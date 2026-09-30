package engine

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/glorch/kestrel/pkg/artifact"
	"github.com/glorch/kestrel/pkg/dag"
	"github.com/glorch/kestrel/pkg/executor"
	"github.com/glorch/kestrel/pkg/logger"
	"github.com/glorch/kestrel/pkg/pipeline"
)

// JobStatus tracks lifecycle state of each job.
type JobStatus string

const (
	StatusPending JobStatus = "PENDING"
	StatusRunning JobStatus = "RUNNING"
	StatusPassed  JobStatus = "PASSED"
	StatusFailed  JobStatus = "FAILED"
	StatusSkipped JobStatus = "SKIPPED"
)

// Options configures the pipeline execution run.
type Options struct {
	WorkDir       string
	ForceExecutor string // "host" or "docker"
	DryRun        bool
	TargetJob     string
}

// Engine coordinates the execution of a pipeline.
type Engine struct {
	pipeline     *pipeline.Pipeline
	opts         Options
	logger       *logger.Logger
	artifactMgr  *artifact.Store
	hostExecutor executor.Executor
	dockExecutor executor.Executor

	statusMu sync.RWMutex
	statuses map[string]JobStatus
	durations map[string]time.Duration
}

// New creates a new execution Engine.
func New(p *pipeline.Pipeline, opts Options, log *logger.Logger) *Engine {
	if log == nil {
		log = logger.Default()
	}
	if opts.WorkDir == "" {
		opts.WorkDir, _ = os.Getwd()
	}

	return &Engine{
		pipeline:    p,
		opts:        opts,
		logger:      log,
		artifactMgr: artifact.NewStore(""),
		statuses:    make(map[string]JobStatus),
		durations:   make(map[string]time.Duration),
	}
}

// Run executes the complete pipeline according to DAG topology.
func (e *Engine) Run(ctx context.Context) error {
	totalStart := time.Now()

	e.logger.Info("Starting pipeline: %s (version %s)", e.pipeline.Name, e.pipeline.Version)

	// 1. Build DAG
	graph, err := dag.BuildGraph(e.pipeline.Jobs)
	if err != nil {
		return fmt.Errorf("failed to build pipeline DAG: %w", err)
	}

	batches, err := graph.ResolveBatches()
	if err != nil {
		return fmt.Errorf("failed to resolve DAG stages: %w", err)
	}

	// Initialize all job statuses to Pending
	for id := range e.pipeline.Jobs {
		e.statuses[id] = StatusPending
	}

	// Initialize executors
	e.hostExecutor = executor.NewHostExecutor()
	dock, err := executor.NewDockerExecutor()
	if err == nil {
		e.dockExecutor = dock
	}

	// 2. Iterate through sequential stages
	for stageIdx, batch := range batches {
		e.logger.Stage(stageIdx+1, len(batches), batch)

		if e.opts.DryRun {
			for _, jobID := range batch {
				e.logger.Info("[DRY-RUN] Job '%s' would run on %s", jobID, e.pipeline.Jobs[jobID].RunsOn)
				e.setJobStatus(jobID, StatusPassed, 0)
			}
			continue
		}

		var wg sync.WaitGroup
		for _, jobID := range batch {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				e.executeJob(ctx, id)
			}(jobID)
		}
		wg.Wait()
	}

	totalDuration := time.Since(totalStart)
	e.printSummary(totalDuration)

	// Determine if overall pipeline succeeded
	e.statusMu.RLock()
	defer e.statusMu.RUnlock()
	for _, status := range e.statuses {
		if status == StatusFailed {
			return fmt.Errorf("pipeline completed with errors")
		}
	}

	return nil
}

func (e *Engine) executeJob(ctx context.Context, jobID string) {
	job := e.pipeline.Jobs[jobID]

	// Check if upstream dependencies succeeded
	for _, dep := range job.Needs {
		e.statusMu.RLock()
		depStatus := e.statuses[dep]
		e.statusMu.RUnlock()

		if depStatus != StatusPassed {
			e.logger.JobSkipped(jobID, fmt.Sprintf("dependency '%s' did not pass (%s)", dep, depStatus))
			e.setJobStatus(jobID, StatusSkipped, 0)
			return
		}
	}

	// Set timeout context
	jobCtx, cancel := context.WithTimeout(ctx, job.ParsedTimeout())
	defer cancel()

	// Select executor
	execType := job.RunsOn
	if e.opts.ForceExecutor != "" {
		execType = e.opts.ForceExecutor
	}

	var exec executor.Executor
	if execType == "docker" {
		if e.dockExecutor == nil {
			e.logger.Error("[%s] Docker executor requested but Docker daemon is not available. Falling back to host runner.", jobID)
			exec = e.hostExecutor
			execType = "host"
		} else {
			exec = e.dockExecutor
		}
	} else {
		exec = e.hostExecutor
	}

	e.setJobStatus(jobID, StatusRunning, 0)
	e.logger.JobStart(jobID, job.Image, execType)

	// Combine global and job-level environment variables
	combinedEnv := make(map[string]string)
	for k, v := range e.pipeline.Env {
		combinedEnv[k] = v
	}
	for k, v := range job.Env {
		combinedEnv[k] = v
	}
	combinedEnv["KESTREL_JOB"] = jobID
	combinedEnv["KESTREL_PIPELINE"] = e.pipeline.Name

	jobStart := time.Now()
	steps := job.NormalizedSteps()
	logWriter := e.logger.JobLineWriter(jobID)

	var jobErr error
	for _, step := range steps {
		e.logger.StepStart(jobID, step.Name)

		res, err := exec.ExecuteStep(jobCtx, job, step, combinedEnv, e.opts.WorkDir, logWriter)
		if err != nil || (res != nil && res.ExitCode != 0) {
			code := 1
			if res != nil {
				code = res.ExitCode
			}
			jobErr = fmt.Errorf("step '%s' failed with exit code %d", step.Name, code)
			e.logger.JobFailed(jobID, code, jobErr)
			break
		}
	}

	jobDuration := time.Since(jobStart)

	if jobErr != nil {
		if job.ContinueOnError {
			e.logger.Info("[%s] Job failed but continue-on-error is enabled", jobID)
			e.setJobStatus(jobID, StatusPassed, jobDuration)
		} else {
			e.setJobStatus(jobID, StatusFailed, jobDuration)
			return
		}
	} else {
		e.logger.JobSuccess(jobID, jobDuration)
		e.setJobStatus(jobID, StatusPassed, jobDuration)
	}

	// Collect artifacts if defined
	if job.Artifacts != nil && len(job.Artifacts.Paths) > 0 {
		saved, err := e.artifactMgr.Collect(e.opts.WorkDir, jobID, job.Artifacts.Paths)
		if err != nil {
			e.logger.Error("[%s] Artifact collection warning: %v", jobID, err)
		} else if len(saved) > 0 {
			e.logger.Info("[%s] Stored %d artifact(s) to .kestrel/artifacts/%s", jobID, len(saved), jobID)
		}
	}
}

func (e *Engine) setJobStatus(jobID string, status JobStatus, duration time.Duration) {
	e.statusMu.Lock()
	defer e.statusMu.Unlock()
	e.statuses[jobID] = status
	if duration > 0 {
		e.durations[jobID] = duration
	}
}

func (e *Engine) printSummary(total time.Duration) {
	e.statusMu.RLock()
	defer e.statusMu.RUnlock()

	e.logger.Info("--------------------------------------------------")
	e.logger.Info("Pipeline Execution Summary (Total: %s)", total.Round(time.Millisecond))
	for id, status := range e.statuses {
		dur := e.durations[id].Round(time.Millisecond)
		switch status {
		case StatusPassed:
			e.logger.Info("  ✔ %-16s %s (%s)", id, status, dur)
		case StatusFailed:
			e.logger.Error("  ✘ %-16s %s (%s)", id, status, dur)
		case StatusSkipped:
			e.logger.Info("  ⊘ %-16s %s", id, status)
		default:
			e.logger.Info("  • %-16s %s", id, status)
		}
	}
	e.logger.Info("--------------------------------------------------")
}
