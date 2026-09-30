package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/glorch/kestrel/pkg/cd"
	"github.com/glorch/kestrel/pkg/dag"
	"github.com/glorch/kestrel/pkg/pipeline"
	"github.com/glorch/kestrel/pkg/rpc"
	"github.com/glorch/kestrel/pkg/store"
	"github.com/glorch/kestrel/pkg/webhook"
)

// Server represents the Kestrel central control plane coordinating distributed runners.
type Server struct {
	mu            sync.RWMutex
	store         store.Store
	logStore      store.LogStore
	gateMgr       *cd.GateManager
	webhookSecret string
	runners       map[string]*rpc.RunnerInfo
	queue         []*rpc.TaskSpec
	inFlight      map[string]*rpc.TaskSpec
	runPipelines  map[string]*pipeline.Pipeline // runID -> parsed pipeline
	runBatches    map[string][][]string         // runID -> stages
	runStageIdx   map[string]int                // runID -> current stage index
	runJobStatus  map[string]map[string]string  // runID -> jobID -> status
}

// NewServer creates a new Kestrel control plane server.
func NewServer(st store.Store) *Server {
	if st == nil {
		st = store.NewMemoryStore()
	}
	ls, _ := store.NewFileLogStore("")
	return &Server{
		store:        st,
		logStore:     ls,
		gateMgr:      cd.NewGateManager(),
		runners:      make(map[string]*rpc.RunnerInfo),
		queue:        make([]*rpc.TaskSpec, 0),
		inFlight:     make(map[string]*rpc.TaskSpec),
		runPipelines: make(map[string]*pipeline.Pipeline),
		runBatches:   make(map[string][][]string),
		runStageIdx:  make(map[string]int),
		runJobStatus: make(map[string]map[string]string),
	}
}

// RegisterRunner handles runner agent registration.
func (s *Server) RegisterRunner(ctx context.Context, req *rpc.RegisterRequest) (*rpc.RegisterResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	req.Runner.LastSeen = time.Now()
	req.Runner.Connected = true
	s.runners[req.Runner.ID] = &req.Runner

	return &rpc.RegisterResponse{Success: true}, nil
}

// Heartbeat refreshes runner liveness.
func (s *Server) Heartbeat(ctx context.Context, req *rpc.HeartbeatRequest) (*rpc.HeartbeatResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if r, ok := s.runners[req.RunnerID]; ok {
		r.LastSeen = time.Now()
		r.Connected = true
	}
	return &rpc.HeartbeatResponse{Acknowledged: true}, nil
}

// PollTask assigns available queued tasks matching runner capability.
func (s *Server) PollTask(ctx context.Context, req *rpc.PollTaskRequest) (*rpc.PollTaskResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, task := range s.queue {
		if s.matchesRunner(task, req.Tags) {
			// Remove from queue
			s.queue = append(s.queue[:i], s.queue[i+1:]...)
			s.inFlight[task.TaskID] = task
			return &rpc.PollTaskResponse{HasTask: true, Task: task}, nil
		}
	}

	return &rpc.PollTaskResponse{HasTask: false}, nil
}

func (s *Server) matchesRunner(task *rpc.TaskSpec, tags []string) bool {
	if task.RunsOn == "" || task.RunsOn == "host" {
		return true // Any runner can execute host/script tasks
	}
	for _, t := range tags {
		if strings.EqualFold(t, task.RunsOn) {
			return true
		}
	}
	return false
}

// SendLogChunk records streaming logs.
func (s *Server) SendLogChunk(ctx context.Context, req *rpc.LogChunkRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.logStore != nil {
		return s.logStore.Append(req.TaskID, []byte(req.Chunk))
	}
	return nil
}

// CompleteTask handles task completion and progresses pipeline DAG stages.
func (s *Server) CompleteTask(ctx context.Context, req *rpc.CompleteTaskRequest) (*rpc.CompleteTaskResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.inFlight[req.TaskID]
	if !ok {
		return &rpc.CompleteTaskResponse{Acknowledged: true}, nil
	}
	delete(s.inFlight, req.TaskID)

	// Record job run in store
	now := time.Now()
	_ = s.store.SaveJobRun(ctx, &store.JobRun{
		ID:         req.TaskID,
		RunID:      task.RunID,
		JobID:      task.JobID,
		JobName:    task.JobName,
		RunsOn:     task.RunsOn,
		Image:      task.Image,
		Status:     req.Status,
		ExitCode:   req.ExitCode,
		FinishedAt: &now,
		DurationMs: req.DurationMs,
		Error:      req.Error,
	})

	// Track job status in active pipeline run
	if statuses, exists := s.runJobStatus[task.RunID]; exists {
		statuses[task.JobID] = req.Status
		s.checkAndProgressPipeline(ctx, task.RunID)
	}

	return &rpc.CompleteTaskResponse{Acknowledged: true}, nil
}

func (s *Server) checkAndProgressPipeline(ctx context.Context, runID string) {
	batches := s.runBatches[runID]
	stageIdx := s.runStageIdx[runID]
	statuses := s.runJobStatus[runID]
	p := s.runPipelines[runID]

	if stageIdx >= len(batches) {
		return // already finished
	}

	currentBatch := batches[stageIdx]
	stageFinished := true
	stageFailed := false

	for _, jobID := range currentBatch {
		status, finished := statuses[jobID]
		if !finished {
			stageFinished = false
			break
		}
		if status == "FAILED" {
			stageFailed = true
		}
	}

	if !stageFinished {
		return // still waiting for other concurrent jobs in this stage
	}

	if stageFailed {
		// Mark run as FAILED and finalize
		s.finalizeRun(ctx, runID, "FAILED")
		return
	}

	// Progress to next stage
	nextStage := stageIdx + 1
	s.runStageIdx[runID] = nextStage

	if nextStage >= len(batches) {
		// All stages completed successfully!
		s.finalizeRun(ctx, runID, "PASSED")
		return
	}

	// Enqueue jobs for the next stage
	for _, nextJobID := range batches[nextStage] {
		job := p.Jobs[nextJobID]
		s.enqueueJob(runID, nextJobID, job)
	}
}

func (s *Server) finalizeRun(ctx context.Context, runID, status string) {
	runRec, err := s.store.GetRun(ctx, runID)
	if err == nil && runRec != nil {
		now := time.Now()
		runRec.Status = status
		runRec.FinishedAt = &now
		runRec.DurationMs = time.Since(runRec.StartedAt).Milliseconds()
		_ = s.store.UpdateRun(ctx, runRec)
	}
}

func (s *Server) enqueueJob(runID, jobID string, job *pipeline.Job) {
	taskID := fmt.Sprintf("%s-%s", runID, jobID)
	task := &rpc.TaskSpec{
		TaskID:     taskID,
		RunID:      runID,
		JobID:      jobID,
		JobName:    job.Name,
		RunsOn:     job.RunsOn,
		Image:      job.Image,
		Env:        job.Env,
		WorkDir:    job.WorkDir,
		Steps:         job.NormalizedSteps(),
		TimeoutSec:    int(job.ParsedTimeout().Seconds()),
		Retries:       job.Retries,
		RetryInterval: job.RetryInterval,
	}

	// Check if this job requires manual approval
	if job.Approval || job.Environment == "production" {
		gate := s.gateMgr.RequestApproval(runID, jobID, job.Environment, nil, 24*time.Hour)
		go func() {
			status, err := s.gateMgr.WaitForDecision(context.Background(), gate.ID)
			if err != nil || status != cd.ApprovalApproved {
				s.mu.Lock()
				if statuses, exists := s.runJobStatus[runID]; exists {
					statuses[jobID] = "FAILED"
					s.checkAndProgressPipeline(context.Background(), runID)
				}
				s.mu.Unlock()
				return
			}
			s.mu.Lock()
			s.queue = append(s.queue, task)
			s.mu.Unlock()
		}()
		return
	}

	s.queue = append(s.queue, task)
}

// TriggerPipeline triggers a new pipeline execution from a parsed pipeline.
func (s *Server) TriggerPipeline(ctx context.Context, p *pipeline.Pipeline, triggerType string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. Build DAG
	graph, err := dag.BuildGraph(p.Jobs)
	if err != nil {
		return "", fmt.Errorf("DAG error: %w", err)
	}

	batches, err := graph.ResolveBatches()
	if err != nil {
		return "", fmt.Errorf("DAG resolution error: %w", err)
	}

	runID := fmt.Sprintf("run-%d", time.Now().UnixNano()/1e6)
	runRec := &store.PipelineRun{
		ID:           runID,
		PipelineName: p.Name,
		Status:       "RUNNING",
		StartedAt:    time.Now(),
		Trigger:      triggerType,
		Env:          p.Env,
	}
	if err := s.store.CreateRun(ctx, runRec); err != nil {
		return "", err
	}

	s.runPipelines[runID] = p
	s.runBatches[runID] = batches
	s.runStageIdx[runID] = 0
	s.runJobStatus[runID] = make(map[string]string)

	// Enqueue stage 1 jobs
	if len(batches) > 0 {
		for _, jobID := range batches[0] {
			s.enqueueJob(runID, jobID, p.Jobs[jobID])
		}
	}

	return runID, nil
}

// HTTPHandler returns an http.Handler serving RPC and REST API endpoints.
func (s *Server) HTTPHandler() http.Handler {
	rpcMux := rpc.NewHTTPHandler(s)

	mainMux := http.NewServeMux()
	mainMux.Handle("/rpc/", rpcMux)

	// REST API: List runners
	mainMux.HandleFunc("/api/v1/runners", func(w http.ResponseWriter, r *http.Request) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		var list []*rpc.RunnerInfo
		for _, runner := range s.runners {
			list = append(list, runner)
		}
		writeJSONResponse(w, list)
	})

	// REST API: List runs
	mainMux.HandleFunc("/api/v1/runs", func(w http.ResponseWriter, r *http.Request) {
		runs, err := s.store.ListRuns(r.Context(), store.RunFilter{})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSONResponse(w, runs)
	})

	// REST API: Trigger pipeline via YAML in body
	mainMux.HandleFunc("/api/v1/runs/trigger", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		p, err := pipeline.Parse(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid yaml: %v", err), http.StatusBadRequest)
			return
		}
		runID, err := s.TriggerPipeline(r.Context(), p, "api")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSONResponse(w, map[string]string{"run_id": runID, "status": "QUEUED"})
	})

	// REST API: List pending approval gates
	mainMux.HandleFunc("/api/v1/approvals", func(w http.ResponseWriter, r *http.Request) {
		pending := s.gateMgr.ListPending()
		writeJSONResponse(w, pending)
	})

	// REST API: Approve a gate
	mainMux.HandleFunc("/api/v1/approvals/approve", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			GateID   string `json:"gate_id"`
			Approver string `json:"approver"`
			Comment  string `json:"comment"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.gateMgr.Approve(payload.GateID, payload.Approver, payload.Comment); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSONResponse(w, map[string]string{"status": "APPROVED", "gate_id": payload.GateID})
	})

	// Webhook endpoint: handles incoming Git push/PR events
	mainMux.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		event, err := webhook.ParseRequest(r, s.webhookSecret)
		if err != nil {
			http.Error(w, fmt.Sprintf("webhook error: %v", err), http.StatusBadRequest)
			return
		}
		writeJSONResponse(w, map[string]interface{}{
			"status":   "RECEIVED",
			"provider": event.Provider,
			"event":    event.Type,
			"repo":     event.Repo,
			"branch":   event.Branch,
			"commit":   event.Commit,
		})
	})

	// REST API: Get task logs
	mainMux.HandleFunc("/api/v1/tasks/logs", func(w http.ResponseWriter, r *http.Request) {
		taskID := r.URL.Query().Get("task_id")
		if taskID == "" {
			http.Error(w, "missing task_id", http.StatusBadRequest)
			return
		}
		if s.logStore == nil {
			http.Error(w, "log store not initialized", http.StatusInternalServerError)
			return
		}
		data, err := s.logStore.Read(taskID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write(data)
	})

	return mainMux
}

// SetLogStore configures a custom LogStore backend.
func (s *Server) SetLogStore(ls store.LogStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logStore = ls
}

// LogStore returns the server's log store.
func (s *Server) LogStore() store.LogStore {
	return s.logStore
}

// SetWebhookSecret configures the shared HMAC secret for webhook signature validation.
func (s *Server) SetWebhookSecret(secret string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.webhookSecret = secret
}

// GateManager returns the server's approval gate manager.
func (s *Server) GateManager() *cd.GateManager {
	return s.gateMgr
}

func writeJSONResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}
