package engine

import (
	"context"
	"os"
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
