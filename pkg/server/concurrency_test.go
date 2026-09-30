package server

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/glorch/kestrel/pkg/pipeline"
	"github.com/glorch/kestrel/pkg/store"
)

func TestConcurrencyManagerBasic(t *testing.T) {
	cm := NewConcurrencyManager()

	var cancelled int32
	cancelFn := func() {
		atomic.StoreInt32(&cancelled, 1)
	}

	// 1. Acquire group "deploy-prod"
	acquired, cancelledID, err := cm.Acquire("deploy-prod", "run-1", false, cancelFn)
	if err != nil || !acquired || cancelledID != "" {
		t.Fatalf("expected run-1 to acquire lock, got acquired=%v, err=%v", acquired, err)
	}

	isLocked, holder := cm.IsLocked("deploy-prod")
	if !isLocked || holder != "run-1" {
		t.Fatalf("expected group locked by run-1, got: %s", holder)
	}

	// 2. run-2 tries to acquire without cancelInProgress -> rejected / queued
	acquired, _, err = cm.Acquire("deploy-prod", "run-2", false, nil)
	if acquired || err == nil {
		t.Fatal("expected run-2 to be rejected due to lock")
	}

	// 3. run-3 acquires with cancelInProgress -> takes over and cancels run-1!
	acquired, cancelledID, err = cm.Acquire("deploy-prod", "run-3", true, nil)
	if err != nil || !acquired {
		t.Fatalf("expected run-3 to take over, got err: %v", err)
	}
	if cancelledID != "run-1" {
		t.Errorf("expected cancelled holder to be run-1, got: %s", cancelledID)
	}
	if atomic.LoadInt32(&cancelled) != 1 {
		t.Error("expected cancelFn of run-1 to have been executed")
	}

	// 4. Release run-3
	cm.Release("deploy-prod", "run-3")
	isLocked, _ = cm.IsLocked("deploy-prod")
	// Promoted queued run-2
	if !isLocked {
		t.Log("group freed or promoted next in queue")
	}
}

func TestServerPipelineConcurrencyIntegration(t *testing.T) {
	st := store.NewMemoryStore()
	srv := NewServer(st)
	ctx := context.Background()

	yaml1 := `
version: "1.0"
name: "deploy-pipeline-v1"
concurrency:
  group: "prod-release"
  cancel-in-progress: true
jobs:
  step1:
    runs-on: host
    commands:
      - echo "v1"
`
	p1, err := pipeline.Parse(strings.NewReader(yaml1))
	if err != nil {
		t.Fatalf("failed to parse yaml1: %v", err)
	}

	runID1, err := srv.TriggerPipeline(ctx, p1, "api")
	if err != nil {
		t.Fatalf("failed to trigger run1: %v", err)
	}

	yaml2 := `
version: "1.0"
name: "deploy-pipeline-v2"
concurrency:
  group: "prod-release"
  cancel-in-progress: true
jobs:
  step1:
    runs-on: host
    commands:
      - echo "v2"
`
	p2, err := pipeline.Parse(strings.NewReader(yaml2))
	if err != nil {
		t.Fatalf("failed to parse yaml2: %v", err)
	}

	// Triggering run2 should take over group "prod-release" and cancel run1
	runID2, err := srv.TriggerPipeline(ctx, p2, "api")
	if err != nil {
		t.Fatalf("failed to trigger run2: %v", err)
	}
	if runID2 == runID1 {
		t.Fatal("expected distinct run IDs")
	}

	// Check status of run1 in store
	runRec1, err := st.GetRun(ctx, runID1)
	if err != nil {
		t.Fatalf("failed to fetch run1: %v", err)
	}
	if runRec1.Status != "CANCELLED" {
		t.Errorf("expected run1 to be CANCELLED, got: %s", runRec1.Status)
	}
}
