package executor

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/glorch/kestrel/pkg/pipeline"
)

func TestHostExecutorSuccess(t *testing.T) {
	exec := NewHostExecutor()
	job := &pipeline.Job{Name: "test-job"}
	step := &pipeline.Step{Name: "echo-step", Run: "echo 'kestrel test'"}

	var buf bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := exec.ExecuteStep(ctx, job, step, nil, "", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", res.ExitCode)
	}

	output := buf.String()
	if !strings.Contains(output, "kestrel test") {
		t.Errorf("expected output to contain 'kestrel test', got: %s", output)
	}
}

func TestHostExecutorFailure(t *testing.T) {
	exec := NewHostExecutor()
	job := &pipeline.Job{Name: "fail-job"}
	// Exit with code 2
	step := &pipeline.Step{Name: "fail-step", Run: "exit 2"}

	var buf bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := exec.ExecuteStep(ctx, job, step, nil, "", &buf)
	if err != nil {
		// On windows powershell, exit 2 might return error from process exit
	}

	if res.ExitCode == 0 {
		t.Errorf("expected non-zero exit code, got 0")
	}
}
