package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/glorch/kestrel/pkg/cd"
	"github.com/glorch/kestrel/pkg/pipeline"
	"github.com/glorch/kestrel/pkg/rpc"
	"github.com/glorch/kestrel/pkg/store"
)

func TestServerPipelineProgress(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()
	srv := NewServer(st)

	p := &pipeline.Pipeline{
		Name:    "multi-stage-pipe",
		Version: "1.0",
		Jobs: map[string]*pipeline.Job{
			"build": {
				Name:     "Build",
				RunsOn:   "host",
				Commands: []string{"echo build"},
			},
			"deploy": {
				Name:     "Deploy",
				RunsOn:   "host",
				Needs:    []string{"build"},
				Commands: []string{"echo deploy"},
			},
		},
	}

	runID, err := srv.TriggerPipeline(ctx, p, "test")
	if err != nil {
		t.Fatalf("failed to trigger pipeline: %v", err)
	}

	// 1. Poll Stage 1 task (build)
	pollResp, err := srv.PollTask(ctx, &rpc.PollTaskRequest{
		RunnerID: "r-1",
		Tags:     []string{"host"},
	})
	if err != nil || !pollResp.HasTask {
		t.Fatalf("expected stage 1 task to be available: %v", err)
	}
	if pollResp.Task.JobID != "build" {
		t.Errorf("expected build task, got %s", pollResp.Task.JobID)
	}

	// 2. Complete Stage 1 task
	_, err = srv.CompleteTask(ctx, &rpc.CompleteTaskRequest{
		TaskID:     pollResp.Task.TaskID,
		RunnerID:   "r-1",
		Status:     "PASSED",
		ExitCode:   0,
		DurationMs: 100,
	})
	if err != nil {
		t.Fatalf("failed to complete task: %v", err)
	}

	// 3. Poll Stage 2 task (deploy should now be available!)
	pollResp2, err := srv.PollTask(ctx, &rpc.PollTaskRequest{
		RunnerID: "r-1",
		Tags:     []string{"host"},
	})
	if err != nil || !pollResp2.HasTask {
		t.Fatalf("expected stage 2 task (deploy) to be enqueued after stage 1 passed")
	}
	if pollResp2.Task.JobID != "deploy" {
		t.Errorf("expected deploy task, got %s", pollResp2.Task.JobID)
	}

	// 4. Complete Stage 2 task
	_, err = srv.CompleteTask(ctx, &rpc.CompleteTaskRequest{
		TaskID:     pollResp2.Task.TaskID,
		RunnerID:   "r-1",
		Status:     "PASSED",
		ExitCode:   0,
		DurationMs: 150,
	})
	if err != nil {
		t.Fatalf("failed to complete task: %v", err)
	}

	// 5. Verify overall pipeline status in store is PASSED
	runRec, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("failed to get run from store: %v", err)
	}
	if runRec.Status != "PASSED" {
		t.Errorf("expected final run status PASSED, got: %s", runRec.Status)
	}
}

func TestServerRESTAPIs(t *testing.T) {
	st := store.NewMemoryStore()
	srv := NewServer(st)
	ts := httptest.NewServer(srv.HTTPHandler())
	defer ts.Close()

	// Register a runner first
	_, _ = srv.RegisterRunner(context.Background(), &rpc.RegisterRequest{
		Runner: rpc.RunnerInfo{
			ID:   "runner-rest-1",
			Name: "Rest Runner",
		},
	})

	// 1. GET /api/v1/runners
	resp, err := http.Get(ts.URL + "/api/v1/runners")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("failed to get runners: %v", err)
	}
	var runners []*rpc.RunnerInfo
	_ = json.NewDecoder(resp.Body).Decode(&runners)
	if len(runners) != 1 || runners[0].ID != "runner-rest-1" {
		t.Errorf("unexpected runners: %+v", runners)
	}

	// 2. POST /api/v1/runs/trigger
	pipelineYAML := `
version: "1.0"
name: "rest-triggered"
jobs:
  test:
    commands: ["echo 1"]
`
	postResp, err := http.Post(ts.URL+"/api/v1/runs/trigger", "application/x-yaml", bytes.NewBufferString(pipelineYAML))
	if err != nil || postResp.StatusCode != http.StatusOK {
		t.Fatalf("failed to trigger pipeline via REST: %v", err)
	}
	var result map[string]string
	_ = json.NewDecoder(postResp.Body).Decode(&result)
	if result["run_id"] == "" || result["status"] != "QUEUED" {
		t.Errorf("unexpected trigger response: %+v", result)
	}
}

func TestServerApprovalGate(t *testing.T) {
	st := store.NewMemoryStore()
	srv := NewServer(st)
	ts := httptest.NewServer(srv.HTTPHandler())
	defer ts.Close()

	ctx := context.Background()
	p := &pipeline.Pipeline{
		Name:    "prod-deploy-pipeline",
		Version: "1.0",
		Jobs: map[string]*pipeline.Job{
			"deploy": {
				Name:        "Production Deploy",
				RunsOn:      "host",
				Approval:    true,
				Environment: "production",
				Commands:    []string{"echo deployed to prod"},
			},
		},
	}

	_, err := srv.TriggerPipeline(ctx, p, "test")
	if err != nil {
		t.Fatalf("trigger failed: %v", err)
	}

	// 1. Task should NOT be in queue yet because it requires approval
	pollResp, err := srv.PollTask(ctx, &rpc.PollTaskRequest{RunnerID: "r-1", Tags: []string{"host"}})
	if err != nil || pollResp.HasTask {
		t.Fatal("expected task to be blocked behind approval gate")
	}

	// 2. Query pending approvals via REST API
	resp, err := http.Get(ts.URL + "/api/v1/approvals")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("get approvals failed: %v", err)
	}
	var gates []*cd.GateRequest
	_ = json.NewDecoder(resp.Body).Decode(&gates)
	if len(gates) != 1 {
		t.Fatalf("expected 1 pending gate, got %d", len(gates))
	}

	gateID := gates[0].ID

	// 3. Approve gate via REST API
	approvePayload := map[string]string{
		"gate_id":  gateID,
		"approver": "ops-lead",
		"comment":  "ship it",
	}
	payloadBytes, _ := json.Marshal(approvePayload)
	approveResp, err := http.Post(ts.URL+"/api/v1/approvals/approve", "application/json", bytes.NewReader(payloadBytes))
	if err != nil || approveResp.StatusCode != http.StatusOK {
		t.Fatalf("approve failed: %v", err)
	}

	// Wait briefly for goroutine to release task into queue
	time.Sleep(50 * time.Millisecond)

	// 4. Now runner polls task successfully!
	pollResp2, err := srv.PollTask(ctx, &rpc.PollTaskRequest{RunnerID: "r-1", Tags: []string{"host"}})
	if err != nil || !pollResp2.HasTask {
		t.Fatal("expected task to be released to queue after approval")
	}
	if pollResp2.Task.JobID != "deploy" {
		t.Errorf("expected deploy task, got: %s", pollResp2.Task.JobID)
	}
}
