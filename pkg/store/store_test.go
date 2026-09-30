package store

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestMemoryStore(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	run := &PipelineRun{
		ID:           "run-100",
		PipelineName: "my-pipe",
		Status:       "RUNNING",
		StartedAt:    time.Now(),
		Trigger:      "cli",
	}

	if err := s.CreateRun(ctx, run); err != nil {
		t.Fatalf("failed to create run: %v", err)
	}

	// Fetch run
	fetched, err := s.GetRun(ctx, "run-100")
	if err != nil {
		t.Fatalf("failed to get run: %v", err)
	}
	if fetched.PipelineName != "my-pipe" || fetched.Status != "RUNNING" {
		t.Errorf("unexpected run data: %+v", fetched)
	}

	// Update run
	now := time.Now()
	fetched.Status = "PASSED"
	fetched.FinishedAt = &now
	if err := s.UpdateRun(ctx, fetched); err != nil {
		t.Fatalf("failed to update run: %v", err)
	}

	updated, _ := s.GetRun(ctx, "run-100")
	if updated.Status != "PASSED" {
		t.Errorf("expected status PASSED, got: %s", updated.Status)
	}

	// Job runs
	job := &JobRun{
		ID:       "j-1",
		RunID:    "run-100",
		JobID:    "lint",
		Status:   "PASSED",
		ExitCode: 0,
	}
	if err := s.SaveJobRun(ctx, job); err != nil {
		t.Fatalf("failed to save job run: %v", err)
	}

	jobs, err := s.GetJobRuns(ctx, "run-100")
	if err != nil || len(jobs) != 1 {
		t.Fatalf("expected 1 job run, got %d (err: %v)", len(jobs), err)
	}
}

func TestFileStorePersistence(t *testing.T) {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "kestrel-store-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	fs1, err := NewFileStore(tempDir)
	if err != nil {
		t.Fatalf("failed to create file store: %v", err)
	}

	run := &PipelineRun{
		ID:           "run-persist-1",
		PipelineName: "persisted-pipe",
		Status:       "PASSED",
		StartedAt:    time.Now(),
		Trigger:      "webhook",
	}

	if err := fs1.CreateRun(ctx, run); err != nil {
		t.Fatalf("failed to create run: %v", err)
	}

	// Reopen file store from same dir to verify disk reloading
	fs2, err := NewFileStore(tempDir)
	if err != nil {
		t.Fatalf("failed to reopen file store: %v", err)
	}

	reloaded, err := fs2.GetRun(ctx, "run-persist-1")
	if err != nil {
		t.Fatalf("failed to get reloaded run: %v", err)
	}

	if reloaded.PipelineName != "persisted-pipe" || reloaded.Status != "PASSED" {
		t.Errorf("data mismatch in reloaded run: %+v", reloaded)
	}
}
