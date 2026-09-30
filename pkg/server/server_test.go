package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestServerChangeFreezeBlocked(t *testing.T) {
	st := store.NewMemoryStore()
	srv := NewServer(st)
	ts := httptest.NewServer(srv.HTTPHandler())
	defer ts.Close()

	// 1. Add active freeze rule via REST API
	freezeRule := cd.FreezeRule{
		ID:           "emergency-lockdown",
		Name:         "Security Lockdown",
		Type:         cd.FreezeDateRange,
		Environments: []string{"production"},
		StartTime:    time.Now().Add(-1 * time.Hour),
		EndTime:      time.Now().Add(1 * time.Hour),
	}
	body, _ := json.Marshal(freezeRule)
	resp, err := http.Post(ts.URL+"/api/v1/freeze/rules", "application/json", bytes.NewReader(body))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("failed to add freeze rule via REST: %v", err)
	}

	// 2. Query freeze rules
	getResp, err := http.Get(ts.URL + "/api/v1/freeze/rules")
	if err != nil || getResp.StatusCode != http.StatusOK {
		t.Fatalf("failed to get freeze rules: %v", err)
	}
	var rules []*cd.FreezeRule
	_ = json.NewDecoder(getResp.Body).Decode(&rules)
	if len(rules) != 1 || rules[0].ID != "emergency-lockdown" {
		t.Fatalf("unexpected rules list: %+v", rules)
	}

	// 3. Trigger deployment pipeline targeting production
	pipelineYAML := `
version: "1.0"
name: "deploy-pipeline"
jobs:
  prod-deploy:
    runs-on: host
    environment: production
    commands:
      - echo "deploying"
`
	p, err := pipeline.Parse(strings.NewReader(pipelineYAML))
	if err != nil {
		t.Fatalf("failed to parse pipeline: %v", err)
	}

	ctx := context.Background()
	runID, err := srv.TriggerPipeline(ctx, p, "manual")
	if err != nil {
		t.Fatalf("failed to trigger pipeline: %v", err)
	}

	// Let background pipeline progression run
	time.Sleep(100 * time.Millisecond)

	// Check that queue has no tasks for runner because production is frozen
	pollResp, err := srv.PollTask(ctx, &rpc.PollTaskRequest{RunnerID: "r-1", Tags: []string{"host"}})
	if err != nil || pollResp.HasTask {
		t.Fatal("expected no tasks to be dispatched due to change freeze")
	}

	runRec, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("failed to get run: %v", err)
	}
	if runRec.Status != "FAILED" {
		t.Errorf("expected run status FAILED due to change freeze blockage, got: %s", runRec.Status)
	}
}

func TestServer_OIDC_HTTPHandlers(t *testing.T) {
	srv := NewServer(nil)
	handler := srv.HTTPHandler()

	// 1. Test .well-known/openid-configuration
	reqDisc := httptest.NewRequest("GET", "/.well-known/openid-configuration", nil)
	rrDisc := httptest.NewRecorder()
	handler.ServeHTTP(rrDisc, reqDisc)

	if rrDisc.Code != http.StatusOK {
		t.Fatalf("expected 200 for oidc discovery, got %d", rrDisc.Code)
	}
	var disc map[string]interface{}
	if err := json.NewDecoder(rrDisc.Body).Decode(&disc); err != nil {
		t.Fatalf("failed to decode discovery doc: %v", err)
	}
	if disc["issuer"] != "https://kestrel.ci" {
		t.Errorf("expected issuer https://kestrel.ci, got %v", disc["issuer"])
	}

	// 2. Test /api/v1/oidc/token
	tokenReqBody, _ := json.Marshal(map[string]interface{}{
		"run_id":     "run-1234",
		"job_id":     "build-and-push",
		"audience":   "https://vault.hashicorp.com",
		"repository": "glorch/kestrel",
		"tenant":     "infra",
	})
	reqToken := httptest.NewRequest("POST", "/api/v1/oidc/token", bytes.NewReader(tokenReqBody))
	rrToken := httptest.NewRecorder()
	handler.ServeHTTP(rrToken, reqToken)

	if rrToken.Code != http.StatusOK {
		t.Fatalf("expected 200 for oidc token, got %d", rrToken.Code)
	}
	var tokenResp map[string]interface{}
	if err := json.NewDecoder(rrToken.Body).Decode(&tokenResp); err != nil {
		t.Fatalf("failed to decode token response: %v", err)
	}
	rawToken, ok := tokenResp["token"].(string)
	if !ok || rawToken == "" {
		t.Fatalf("expected valid token in response, got %+v", tokenResp)
	}

	// 3. Test /api/v1/oidc/verify
	verifyReqBody, _ := json.Marshal(map[string]interface{}{
		"token":    rawToken,
		"audience": "https://vault.hashicorp.com",
	})
	reqVerify := httptest.NewRequest("POST", "/api/v1/oidc/verify", bytes.NewReader(verifyReqBody))
	rrVerify := httptest.NewRecorder()
	handler.ServeHTTP(rrVerify, reqVerify)

	if rrVerify.Code != http.StatusOK {
		t.Fatalf("expected 200 for token verification, got %d", rrVerify.Code)
	}
	var verifyResp map[string]interface{}
	if err := json.NewDecoder(rrVerify.Body).Decode(&verifyResp); err != nil {
		t.Fatalf("failed to decode verify response: %v", err)
	}
	if verifyResp["valid"] != true {
		t.Errorf("expected valid=true, got %+v", verifyResp)
	}
}

