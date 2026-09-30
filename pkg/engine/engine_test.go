package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glorch/kestrel/pkg/logger"
	"github.com/glorch/kestrel/pkg/pipeline"
)

func TestEngineRunSuccess(t *testing.T) {
	p := &pipeline.Pipeline{
		Name:    "test-pipeline",
		Version: "1.0",
		Jobs: map[string]*pipeline.Job{
			"job1": {
				Name:     "job1",
				RunsOn:   "host",
				Commands: []string{"echo 1"},
			},
			"job2": {
				Name:     "job2",
				RunsOn:   "host",
				Needs:    []string{"job1"},
				Commands: []string{"echo 2"},
			},
		},
	}

	eng := New(p, Options{
		WorkDir: os.TempDir(),
	}, logger.New(nil))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := eng.Run(ctx); err != nil {
		t.Fatalf("expected pipeline to succeed, got: %v", err)
	}

	if eng.statuses["job1"] != StatusPassed || eng.statuses["job2"] != StatusPassed {
		t.Errorf("expected both jobs passed, got: %v, %v", eng.statuses["job1"], eng.statuses["job2"])
	}
}

func TestEngineJobFailureAndSkip(t *testing.T) {
	p := &pipeline.Pipeline{
		Name:    "failure-pipeline",
		Version: "1.0",
		Jobs: map[string]*pipeline.Job{
			"failing": {
				Name:     "failing",
				RunsOn:   "host",
				Commands: []string{"exit 1"},
			},
			"dependent": {
				Name:     "dependent",
				RunsOn:   "host",
				Needs:    []string{"failing"},
				Commands: []string{"echo should not run"},
			},
		},
	}

	eng := New(p, Options{
		WorkDir: os.TempDir(),
	}, logger.New(nil))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := eng.Run(ctx)
	if err == nil {
		t.Fatal("expected pipeline to return error on failed job, got nil")
	}

	if eng.statuses["failing"] != StatusFailed {
		t.Errorf("expected failing job status FAILED, got: %s", eng.statuses["failing"])
	}

	if eng.statuses["dependent"] != StatusSkipped {
		t.Errorf("expected dependent job status SKIPPED, got: %s", eng.statuses["dependent"])
	}
}

func TestEngineConditionalExecution(t *testing.T) {
	p := &pipeline.Pipeline{
		Name:    "conditional-pipeline",
		Version: "1.0",
		Jobs: map[string]*pipeline.Job{
			"failing": {
				Name:     "failing",
				RunsOn:   "host",
				Commands: []string{"exit 1"},
			},
			"cleanup": {
				Name:     "cleanup",
				RunsOn:   "host",
				Needs:    []string{"failing"},
				If:       "always()", // Runs even when failing job fails
				Commands: []string{"echo cleanup"},
			},
			"only_on_failure": {
				Name:     "only_on_failure",
				RunsOn:   "host",
				Needs:    []string{"failing"},
				If:       "failure()", // Runs because failing job failed
				Commands: []string{"echo alert"},
			},
		},
	}

	eng := New(p, Options{
		WorkDir: os.TempDir(),
	}, logger.New(nil))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_ = eng.Run(ctx)

	if eng.statuses["cleanup"] != StatusPassed {
		t.Errorf("expected cleanup job with always() to PASS, got: %s", eng.statuses["cleanup"])
	}

	if eng.statuses["only_on_failure"] != StatusPassed {
		t.Errorf("expected only_on_failure job with failure() to PASS, got: %s", eng.statuses["only_on_failure"])
	}
}

func TestEngineStoresRunAndJobs(t *testing.T) {
	p := &pipeline.Pipeline{
		Name:    "store-verify-pipeline",
		Version: "1.0",
		Jobs: map[string]*pipeline.Job{
			"simple": {
				Name:     "simple",
				RunsOn:   "host",
				Commands: []string{"echo hello store"},
			},
		},
	}

	eng := New(p, Options{
		WorkDir: os.TempDir(),
	}, logger.New(nil))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := eng.Run(ctx); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	runID := eng.RunID()
	if runID == "" {
		t.Fatal("expected non-empty run ID")
	}

	runRec, err := eng.Store().GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("failed to get run from store: %v", err)
	}

	if runRec.Status != "PASSED" || runRec.PipelineName != "store-verify-pipeline" {
		t.Errorf("unexpected run record: %+v", runRec)
	}
}

func TestEngineStepRetrySuccess(t *testing.T) {
	tmpDir := t.TempDir()
	markerFile := filepath.Join(tmpDir, "retry_step.marker")
	// Normalize backslashes for PowerShell command
	cleanMarker := strings.ReplaceAll(markerFile, "\\", "/")

	// First attempt creates marker and exits 1; second attempt sees marker and exits 0.
	cmd := fmt.Sprintf("if (Test-Path '%s') { exit 0 } else { New-Item -Path '%s' -ItemType File; exit 1 }", cleanMarker, cleanMarker)

	p := &pipeline.Pipeline{
		Name:    "retry-step-pipeline",
		Version: "1.0",
		Jobs: map[string]*pipeline.Job{
			"flaky-step-job": {
				Name:   "flaky-step-job",
				RunsOn: "host",
				Steps: []*pipeline.Step{
					{
						Name:          "flaky-step",
						Run:           cmd,
						Retries:       2,
						RetryInterval: "50ms",
					},
				},
			},
		},
	}

	eng := New(p, Options{
		WorkDir: tmpDir,
	}, logger.New(nil))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := eng.Run(ctx); err != nil {
		t.Fatalf("expected step retry to succeed, got error: %v", err)
	}

	if eng.statuses["flaky-step-job"] != StatusPassed {
		t.Errorf("expected job status PASSED, got: %s", eng.statuses["flaky-step-job"])
	}
}

func TestEngineJobRetrySuccess(t *testing.T) {
	tmpDir := t.TempDir()
	markerFile := filepath.Join(tmpDir, "retry_job.marker")
	cleanMarker := strings.ReplaceAll(markerFile, "\\", "/")

	cmd := fmt.Sprintf("if (Test-Path '%s') { exit 0 } else { New-Item -Path '%s' -ItemType File; exit 1 }", cleanMarker, cleanMarker)

	p := &pipeline.Pipeline{
		Name:    "retry-job-pipeline",
		Version: "1.0",
		Jobs: map[string]*pipeline.Job{
			"flaky-job": {
				Name:          "flaky-job",
				RunsOn:        "host",
				Retries:       2,
				RetryInterval: "50ms",
				Commands:      []string{cmd},
			},
		},
	}

	eng := New(p, Options{
		WorkDir: tmpDir,
	}, logger.New(nil))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := eng.Run(ctx); err != nil {
		t.Fatalf("expected job retry to succeed, got error: %v", err)
	}

	if eng.statuses["flaky-job"] != StatusPassed {
		t.Errorf("expected job status PASSED, got: %s", eng.statuses["flaky-job"])
	}
}

