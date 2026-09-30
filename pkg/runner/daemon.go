package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/glorch/kestrel/pkg/executor"
	"github.com/glorch/kestrel/pkg/pipeline"
	"github.com/glorch/kestrel/pkg/rpc"
)

// Config configures a distributed Runner daemon.
type Config struct {
	ID                string
	Name              string
	ServerURL         string
	Tags              []string
	Capacity          int
	WorkDir           string
	PollInterval      time.Duration
	HeartbeatInterval time.Duration
}

// Daemon represents a long-running runner daemon process that executes tasks from the server.
type Daemon struct {
	cfg          Config
	client       *rpc.Client
	hostExec     executor.Executor
	dockExec     executor.Executor
	activeJobsMu sync.Mutex
	activeJobs   int
	stopCh       chan struct{}
}

// NewDaemon initializes a new Runner daemon.
func NewDaemon(cfg Config) *Daemon {
	if cfg.ID == "" {
		cfg.ID = fmt.Sprintf("runner-%s-%d", runtime.GOOS, time.Now().UnixNano()%10000)
	}
	if cfg.Name == "" {
		cfg.Name = cfg.ID
	}
	if cfg.Capacity <= 0 {
		cfg.Capacity = 2
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 1 * time.Second
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 3 * time.Second
	}
	if cfg.WorkDir == "" {
		cfg.WorkDir = os.TempDir()
	}

	d := &Daemon{
		cfg:      cfg,
		client:   rpc.NewClient(cfg.ServerURL),
		hostExec: executor.NewHostExecutor(),
		stopCh:   make(chan struct{}),
	}

	if dock, err := executor.NewDockerExecutor(); err == nil {
		d.dockExec = dock
	}

	return d
}

// Start registers the runner with the server and starts heartbeat and task polling loops.
func (d *Daemon) Start(ctx context.Context) error {
	// 1. Register with Server
	regReq := rpc.RunnerInfo{
		ID:        d.cfg.ID,
		Name:      d.cfg.Name,
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		Tags:      d.cfg.Tags,
		Capacity:  d.cfg.Capacity,
		Version:   "0.2.0",
		LastSeen:  time.Now(),
		Connected: true,
	}

	resp, err := d.client.Register(ctx, regReq)
	if err != nil {
		return fmt.Errorf("failed to register runner: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("registration rejected by server: %s", resp.Message)
	}

	// 2. Start heartbeat goroutine
	go d.heartbeatLoop(ctx)

	// 3. Start task polling loop
	go d.pollLoop(ctx)

	<-ctx.Done()
	return nil
}

func (d *Daemon) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(d.cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		case <-ticker.C:
			d.activeJobsMu.Lock()
			count := d.activeJobs
			d.activeJobsMu.Unlock()

			_, _ = d.client.Heartbeat(ctx, d.cfg.ID, count)
		}
	}
}

func (d *Daemon) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(d.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		case <-ticker.C:
			d.activeJobsMu.Lock()
			current := d.activeJobs
			cap := d.cfg.Capacity
			d.activeJobsMu.Unlock()

			if current >= cap {
				continue // At capacity
			}

			resp, err := d.client.PollTask(ctx, d.cfg.ID, d.cfg.Tags)
			if err != nil || !resp.HasTask || resp.Task == nil {
				continue
			}

			// Spawn task execution concurrently
			d.activeJobsMu.Lock()
			d.activeJobs++
			d.activeJobsMu.Unlock()

			go func(task *rpc.TaskSpec) {
				defer func() {
					d.activeJobsMu.Lock()
					d.activeJobs--
					d.activeJobsMu.Unlock()
				}()
				d.executeTask(ctx, task)
			}(resp.Task)
		}
	}
}

func (d *Daemon) executeTask(ctx context.Context, task *rpc.TaskSpec) {
	start := time.Now()

	// Prepare executor
	var exec executor.Executor
	if task.RunsOn == "docker" && d.dockExec != nil {
		exec = d.dockExec
	} else {
		exec = d.hostExec
	}

	// Create a streaming logger writer to forward logs back to server
	logPipeReader, logPipeWriter := io.Pipe()
	defer logPipeReader.Close()

	// Forward log stream chunks to server
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := logPipeReader.Read(buf)
			if n > 0 {
				_ = d.client.SendLogChunk(context.Background(), task.TaskID, d.cfg.ID, string(buf[:n]))
			}
			if err != nil {
				break
			}
		}
	}()

	// Execute steps
	jobModel := &pipeline.Job{
		Name:    task.JobName,
		RunsOn:  task.RunsOn,
		Image:   task.Image,
		WorkDir: task.WorkDir,
	}

	var exitCode int
	var taskErr error
	status := "PASSED"

	for _, step := range task.Steps {
		res, err := exec.ExecuteStep(ctx, jobModel, step, task.Env, task.WorkDir, logPipeWriter)
		if err != nil || (res != nil && res.ExitCode != 0) {
			status = "FAILED"
			if res != nil {
				exitCode = res.ExitCode
			} else {
				exitCode = 1
			}
			taskErr = err
			break
		}
	}

	logPipeWriter.Close() // Flush and close logs

	duration := time.Since(start)
	errMsg := ""
	if taskErr != nil {
		errMsg = taskErr.Error()
	}

	// Report task completion
	_, _ = d.client.CompleteTask(context.Background(), &rpc.CompleteTaskRequest{
		TaskID:     task.TaskID,
		RunnerID:   d.cfg.ID,
		Status:     status,
		ExitCode:   exitCode,
		DurationMs: duration.Milliseconds(),
		Error:      errMsg,
	})
}

// Stop cleanly terminates runner background activities.
func (d *Daemon) Stop() {
	close(d.stopCh)
}
