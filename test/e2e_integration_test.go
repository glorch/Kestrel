package test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/glorch/kestrel/pkg/pipeline"
	"github.com/glorch/kestrel/pkg/runner"
	"github.com/glorch/kestrel/pkg/server"
	"github.com/glorch/kestrel/pkg/store"
)

func TestEndToEndDistributedPipeline(t *testing.T) {
	// 1. Start Server
	memStore := store.NewMemoryStore()
	srv := server.NewServer(memStore)
	ts := httptest.NewServer(srv.HTTPHandler())
	defer ts.Close()

	// 2. Start Runner Daemon
	daemon := runner.NewDaemon(runner.Config{
		ID:                "e2e-runner-1",
		ServerURL:         ts.URL,
		Tags:              []string{"host", "docker"},
		PollInterval:      25 * time.Millisecond,
		HeartbeatInterval: 100 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go func() {
		_ = daemon.Start(ctx)
	}()
	defer daemon.Stop()

	// 3. Define Pipeline YAML with Matrix and Dependencies
	pipelineYAML := `
version: "1.0"
name: "e2e-distributed-pipeline"

jobs:
  lint:
    runs-on: "host"
    commands:
      - "echo 'Linting passed'"

  test:
    runs-on: "host"
    needs: [lint]
    matrix:
      part: ["unit", "integ"]
    commands:
      - "echo 'Running ${{ matrix.part }} tests'"

  build:
    runs-on: "host"
    needs: [test]
    commands:
      - "echo 'Build completed successfully'"
`

	p, err := pipeline.Parse(strings.NewReader(pipelineYAML))
	if err != nil {
		t.Fatalf("failed to parse pipeline: %v", err)
	}

	// 4. Trigger pipeline on Server
	runID, err := srv.TriggerPipeline(ctx, p, "e2e-test")
	if err != nil {
		t.Fatalf("failed to trigger pipeline: %v", err)
	}

	// 5. Poll until pipeline completes or timeout
	deadline := time.Now().Add(8 * time.Second)
	var finalRun *store.PipelineRun
	for time.Now().Before(deadline) {
		r, err := memStore.GetRun(ctx, runID)
		if err == nil && (r.Status == "PASSED" || r.Status == "FAILED") {
			finalRun = r
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if finalRun == nil {
		t.Fatalf("timed out waiting for distributed pipeline execution to complete")
	}

	if finalRun.Status != "PASSED" {
		t.Errorf("expected pipeline to PASS, got status: %s", finalRun.Status)
	}

	// 6. Verify job runs in store
	jobRuns, err := memStore.GetJobRuns(ctx, runID)
	if err != nil {
		t.Fatalf("failed to get job runs: %v", err)
	}

	// 1 lint + 2 matrix test + 1 build = 4 jobs
	if len(jobRuns) != 4 {
		t.Errorf("expected 4 job runs in store, got %d", len(jobRuns))
	}

	for _, jr := range jobRuns {
		if jr.Status != "PASSED" {
			t.Errorf("job %s failed with code %d: %s", jr.JobID, jr.ExitCode, jr.Error)
		}
	}
}
