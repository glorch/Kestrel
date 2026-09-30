package server

import (
	"context"
	"testing"

	"github.com/glorch/kestrel/pkg/pipeline"
	"github.com/glorch/kestrel/pkg/rpc"
)

func TestQuotaManager_Basic(t *testing.T) {
	qm := NewQuotaManager()
	qm.SetQuota("fintech", 2)

	// Acquire 1
	ok, err := qm.TryAcquire("fintech")
	if !ok || err != nil {
		t.Fatalf("expected acquire 1 to succeed, got %v, %v", ok, err)
	}

	// Acquire 2
	ok, err = qm.TryAcquire("fintech")
	if !ok || err != nil {
		t.Fatalf("expected acquire 2 to succeed, got %v, %v", ok, err)
	}

	// Acquire 3 should fail
	ok, err = qm.TryAcquire("fintech")
	if ok || err == nil {
		t.Fatalf("expected acquire 3 to fail due to quota limit, got ok=%v, err=%v", ok, err)
	}

	// Release 1
	qm.Release("fintech")

	// Acquire 3 should now succeed
	ok, err = qm.TryAcquire("fintech")
	if !ok || err != nil {
		t.Fatalf("expected acquire after release to succeed, got %v, %v", ok, err)
	}

	quota, found := qm.GetQuota("fintech")
	if !found || quota.ActiveJobs != 2 {
		t.Fatalf("expected 2 active jobs, got %d", quota.ActiveJobs)
	}

	quotas := qm.ListQuotas()
	if len(quotas) != 1 || quotas["fintech"].MaxConcurrent != 2 {
		t.Fatalf("unexpected list quotas: %+v", quotas)
	}
}

func TestServer_PriorityScheduling(t *testing.T) {
	srv := NewServer(nil)
	ctx := context.Background()

	// Enqueue tasks with varying priorities
	srv.enqueueTask(&rpc.TaskSpec{
		TaskID:   "task-low",
		Priority: 10,
		RunsOn:   "host",
	})
	srv.enqueueTask(&rpc.TaskSpec{
		TaskID:   "task-high",
		Priority: 100,
		RunsOn:   "host",
	})
	srv.enqueueTask(&rpc.TaskSpec{
		TaskID:   "task-mid",
		Priority: 50,
		RunsOn:   "host",
	})

	// Poll 1: should be task-high
	res1, err := srv.PollTask(ctx, &rpc.PollTaskRequest{RunnerID: "r1", Tags: []string{"host"}})
	if err != nil || !res1.HasTask || res1.Task.TaskID != "task-high" {
		t.Fatalf("expected task-high first, got %+v, err: %v", res1, err)
	}

	// Poll 2: should be task-mid
	res2, err := srv.PollTask(ctx, &rpc.PollTaskRequest{RunnerID: "r2", Tags: []string{"host"}})
	if err != nil || !res2.HasTask || res2.Task.TaskID != "task-mid" {
		t.Fatalf("expected task-mid second, got %+v, err: %v", res2, err)
	}

	// Poll 3: should be task-low
	res3, err := srv.PollTask(ctx, &rpc.PollTaskRequest{RunnerID: "r3", Tags: []string{"host"}})
	if err != nil || !res3.HasTask || res3.Task.TaskID != "task-low" {
		t.Fatalf("expected task-low third, got %+v, err: %v", res3, err)
	}
}

func TestServer_TenantQuotaEnforcement(t *testing.T) {
	srv := NewServer(nil)
	ctx := context.Background()

	// Limit team-a to 1 concurrent job
	srv.QuotaManager().SetQuota("team-a", 1)

	// Enqueue 2 jobs for team-a (priorities 100 and 90), 1 job for team-b (priority 50)
	srv.enqueueTask(&rpc.TaskSpec{
		TaskID:   "a-1",
		Tenant:   "team-a",
		Priority: 100,
		RunsOn:   "host",
	})
	srv.enqueueTask(&rpc.TaskSpec{
		TaskID:   "a-2",
		Tenant:   "team-a",
		Priority: 90,
		RunsOn:   "host",
	})
	srv.enqueueTask(&rpc.TaskSpec{
		TaskID:   "b-1",
		Tenant:   "team-b",
		Priority: 50,
		RunsOn:   "host",
	})

	// Runner 1 polls: should get a-1 (quota for team-a now 1/1)
	res1, err := srv.PollTask(ctx, &rpc.PollTaskRequest{RunnerID: "r1", Tags: []string{"host"}})
	if err != nil || !res1.HasTask || res1.Task.TaskID != "a-1" {
		t.Fatalf("expected a-1, got %+v", res1)
	}

	// Runner 2 polls: a-2 cannot be dispatched (team-a quota saturated), so b-1 is dispatched!
	res2, err := srv.PollTask(ctx, &rpc.PollTaskRequest{RunnerID: "r2", Tags: []string{"host"}})
	if err != nil || !res2.HasTask || res2.Task.TaskID != "b-1" {
		t.Fatalf("expected b-1 because team-a reached quota, got %+v", res2)
	}

	// Runner 3 polls: no tasks available (a-2 is blocked by team-a quota)
	res3, err := srv.PollTask(ctx, &rpc.PollTaskRequest{RunnerID: "r3", Tags: []string{"host"}})
	if err != nil || res3.HasTask {
		t.Fatalf("expected no task available, got %+v", res3)
	}

	// Complete a-1 to release team-a quota
	_, err = srv.CompleteTask(ctx, &rpc.CompleteTaskRequest{
		TaskID:   "a-1",
		RunnerID: "r1",
		Status:   "PASSED",
	})
	if err != nil {
		t.Fatalf("complete task failed: %v", err)
	}

	// Runner 3 polls again: now a-2 can be dispatched!
	res4, err := srv.PollTask(ctx, &rpc.PollTaskRequest{RunnerID: "r3", Tags: []string{"host"}})
	if err != nil || !res4.HasTask || res4.Task.TaskID != "a-2" {
		t.Fatalf("expected a-2 after quota release, got %+v", res4)
	}
}

func TestServer_PipelineTenantAndPriorityTrigger(t *testing.T) {
	srv := NewServer(nil)
	ctx := context.Background()

	p := &pipeline.Pipeline{
		Name:   "tenant-pipeline",
		Tenant: "fintech",
		Jobs: map[string]*pipeline.Job{
			"build": {
				Name:     "Build Job",
				Priority: 80,
				Commands: []string{"echo build"},
			},
		},
	}

	runID, err := srv.TriggerPipeline(ctx, p, "manual")
	if err != nil {
		t.Fatalf("trigger failed: %v", err)
	}

	res, err := srv.PollTask(ctx, &rpc.PollTaskRequest{RunnerID: "r1", Tags: []string{"host"}})
	if err != nil || !res.HasTask {
		t.Fatalf("poll failed: %v", err)
	}

	if res.Task.RunID != runID {
		t.Errorf("expected runID %s, got %s", runID, res.Task.RunID)
	}
	if res.Task.Tenant != "fintech" {
		t.Errorf("expected tenant 'fintech', got %s", res.Task.Tenant)
	}
	if res.Task.Priority != 80 {
		t.Errorf("expected priority 80, got %d", res.Task.Priority)
	}
}
