package executor

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/glorch/kestrel/pkg/pipeline"
	"github.com/glorch/kestrel/pkg/plugin"
)

// HostExecutor runs job steps directly on the host machine.
type HostExecutor struct{}

// NewHostExecutor creates an instance of HostExecutor.
func NewHostExecutor() *HostExecutor {
	return &HostExecutor{}
}

func (h *HostExecutor) Name() string {
	return "host"
}

func (h *HostExecutor) ExecuteStep(ctx context.Context, job *pipeline.Job, step *pipeline.Step, env map[string]string, workDir string, out io.Writer) (*Result, error) {
	start := time.Now()

	// If step uses an Action, execute it via the action plugin registry
	if step.Uses != "" {
		actCtx := &plugin.ActionContext{
			Workspace: workDir,
			Env:       env,
			Out:       out,
		}
		if executed, err := plugin.ExecuteStepAction(ctx, step, actCtx); executed {
			if err != nil {
				return &Result{ExitCode: 1, Duration: time.Since(start), Error: err}, err
			}
			return &Result{ExitCode: 0, Duration: time.Since(start)}, nil
		}
	}

	// Consolidate commands to execute
	var cmdString string
	if step.Run != "" {
		cmdString = step.Run
	} else if len(step.Commands) > 0 {
		cmdString = strings.Join(step.Commands, " && ")
	} else {
		return &Result{ExitCode: 0, Duration: 0}, nil
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		// Use PowerShell on Windows for consistency
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", cmdString)
	} else {
		// Use sh on Unix systems
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", cmdString)
	}

	if workDir != "" {
		cmd.Dir = workDir
	}

	// Inherit current environment and apply custom envs
	cmdEnv := os.Environ()
	for k, v := range env {
		cmdEnv = append(cmdEnv, fmt.Sprintf("%s=%s", k, v))
	}
	for k, v := range step.Env {
		cmdEnv = append(cmdEnv, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = cmdEnv

	cmd.Stdout = out
	cmd.Stderr = out

	err := cmd.Run()
	duration := time.Since(start)

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return &Result{
				ExitCode: exitErr.ExitCode(),
				Duration: duration,
				Error:    exitErr,
			}, nil
		}
		return &Result{
			ExitCode: 1,
			Duration: duration,
			Error:    err,
		}, err
	}

	return &Result{
		ExitCode: 0,
		Duration: duration,
	}, nil
}

func (h *HostExecutor) Cleanup(ctx context.Context, job *pipeline.Job) error {
	// No persistent resources to clean up for host execution
	return nil
}
