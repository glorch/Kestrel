package executor

import (
	"context"
	"io"
	"time"

	"github.com/glorch/kestrel/pkg/pipeline"
)

// Result captures the execution outcome of a step or job.
type Result struct {
	ExitCode int
	Duration time.Duration
	Error    error
}

// Executor defines the contract for executing job steps in a specific runtime.
type Executor interface {
	Name() string
	ExecuteStep(ctx context.Context, job *pipeline.Job, step *pipeline.Step, env map[string]string, workDir string, out io.Writer) (*Result, error)
	Cleanup(ctx context.Context, job *pipeline.Job) error
}
