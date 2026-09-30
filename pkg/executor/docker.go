package executor

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/glorch/kestrel/pkg/pipeline"
)

// DockerExecutor runs job steps inside Docker containers.
type DockerExecutor struct {
	cli *client.Client
}

// NewDockerExecutor instantiates a DockerExecutor using Docker daemon environment.
func NewDockerExecutor() (*DockerExecutor, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	// Verify connection to Docker daemon
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		return nil, fmt.Errorf("docker daemon is unreachable (%w). Is Docker running?", err)
	}

	return &DockerExecutor{cli: cli}, nil
}

func (d *DockerExecutor) Name() string {
	return "docker"
}

// ensureImage pulls the image if it is not already available locally.
func (d *DockerExecutor) ensureImage(ctx context.Context, imgName string, out io.Writer) error {
	_, _, err := d.cli.ImageInspectWithRaw(ctx, imgName)
	if err == nil {
		return nil // Cached locally
	}

	fmt.Fprintf(out, "Pulling container image: %s...\n", imgName)
	reader, err := d.cli.ImagePull(ctx, imgName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull image %s: %w", imgName, err)
	}
	defer reader.Close()

	// Drain reader so pull completes
	io.Copy(io.Discard, reader)
	return nil
}

func (d *DockerExecutor) ExecuteStep(ctx context.Context, job *pipeline.Job, step *pipeline.Step, env map[string]string, workDir string, out io.Writer) (*Result, error) {
	start := time.Now()

	img := job.Image
	if img == "" {
		img = "alpine:latest" // Default fallback image if none specified
	}

	if err := d.ensureImage(ctx, img, out); err != nil {
		return &Result{ExitCode: 1, Duration: time.Since(start), Error: err}, err
	}

	// Determine command
	var cmdStr string
	if step.Run != "" {
		cmdStr = step.Run
	} else if len(step.Commands) > 0 {
		cmdStr = strings.Join(step.Commands, " && ")
	} else {
		return &Result{ExitCode: 0, Duration: 0}, nil
	}

	// Prepare environment
	var envList []string
	for k, v := range env {
		envList = append(envList, fmt.Sprintf("%s=%s", k, v))
	}
	for k, v := range step.Env {
		envList = append(envList, fmt.Sprintf("%s=%s", k, v))
	}

	// Convert host workDir to absolute path for Docker bind mount
	absWorkDir, err := filepath.Abs(workDir)
	if err != nil {
		absWorkDir = workDir
	}

	containerConfig := &container.Config{
		Image:        img,
		Cmd:          []string{"sh", "-c", cmdStr},
		WorkingDir:   "/workspace",
		Env:          envList,
		Tty:          false,
		AttachStdout: true,
		AttachStderr: true,
	}

	hostConfig := &container.HostConfig{
		Binds: []string{
			fmt.Sprintf("%s:/workspace", absWorkDir),
		},
	}

	containerName := fmt.Sprintf("kestrel-%s-%d", job.Name, time.Now().UnixNano()%100000)

	resp, err := d.cli.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, containerName)
	if err != nil {
		return &Result{ExitCode: 1, Duration: time.Since(start), Error: err}, fmt.Errorf("container create failed: %w", err)
	}

	// Ensure cleanup of the container
	defer func() {
		_ = d.cli.ContainerRemove(context.Background(), resp.ID, container.RemoveOptions{Force: true})
	}()

	if err := d.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return &Result{ExitCode: 1, Duration: time.Since(start), Error: err}, fmt.Errorf("container start failed: %w", err)
	}

	// Stream logs
	logsReader, err := d.cli.ContainerLogs(ctx, resp.ID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err == nil {
		defer logsReader.Close()
		_, _ = stdcopy.StdCopy(out, out, logsReader)
	}

	// Wait for completion
	statusCh, errCh := d.cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case waitErr := <-errCh:
		if waitErr != nil {
			return &Result{ExitCode: 1, Duration: time.Since(start), Error: waitErr}, waitErr
		}
	case status := <-statusCh:
		duration := time.Since(start)
		if status.StatusCode != 0 {
			return &Result{
				ExitCode: int(status.StatusCode),
				Duration: duration,
				Error:    fmt.Errorf("step exited with code %d", status.StatusCode),
			}, nil
		}
		return &Result{
			ExitCode: 0,
			Duration: duration,
		}, nil
	}

	return &Result{ExitCode: 0, Duration: time.Since(start)}, nil
}

func (d *DockerExecutor) Cleanup(ctx context.Context, job *pipeline.Job) error {
	return nil
}
