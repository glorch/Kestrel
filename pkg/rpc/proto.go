package rpc

import (
	"time"

	"github.com/glorch/kestrel/pkg/pipeline"
)

// RunnerInfo contains registration metadata about a runner agent.
type RunnerInfo struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	OS        string    `json:"os"`
	Arch      string    `json:"arch"`
	Tags      []string  `json:"tags"`     // e.g. ["docker", "host", "windows", "gpu"]
	Capacity  int       `json:"capacity"` // maximum concurrent jobs
	Version   string    `json:"version"`
	LastSeen  time.Time `json:"last_seen"`
	Connected bool      `json:"connected"`
}

// RegisterRequest is sent by runner on startup.
type RegisterRequest struct {
	Runner RunnerInfo `json:"runner"`
}

// RegisterResponse acknowledges runner registration.
type RegisterResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// HeartbeatRequest is sent periodically by runners to prove liveness.
type HeartbeatRequest struct {
	RunnerID   string    `json:"runner_id"`
	Timestamp  time.Time `json:"timestamp"`
	ActiveJobs int       `json:"active_jobs"`
}

// HeartbeatResponse is returned by server.
type HeartbeatResponse struct {
	Acknowledged bool `json:"acknowledged"`
}

// TaskSpec defines a unit of work assigned to a runner.
type TaskSpec struct {
	TaskID        string            `json:"task_id"`
	RunID         string            `json:"run_id"`
	JobID         string            `json:"job_id"`
	JobName       string            `json:"job_name"`
	RunsOn        string            `json:"runs_on"`
	Image         string            `json:"image,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	WorkDir       string            `json:"workdir,omitempty"`
	Steps         []*pipeline.Step  `json:"steps,omitempty"`
	ArtifactPaths []string          `json:"artifact_paths,omitempty"`
	TimeoutSec    int               `json:"timeout_sec"`
}

// PollTaskRequest asks server if any matching task is available for execution.
type PollTaskRequest struct {
	RunnerID string   `json:"runner_id"`
	Tags     []string `json:"tags"`
}

// PollTaskResponse contains assigned task if available.
type PollTaskResponse struct {
	HasTask bool      `json:"has_task"`
	Task    *TaskSpec `json:"task,omitempty"`
}

// LogChunkRequest streams stdout/stderr from runner to server.
type LogChunkRequest struct {
	TaskID    string    `json:"task_id"`
	RunnerID  string    `json:"runner_id"`
	Chunk     string    `json:"chunk"`
	Timestamp time.Time `json:"timestamp"`
}

// CompleteTaskRequest notifies server that a task finished.
type CompleteTaskRequest struct {
	TaskID     string   `json:"task_id"`
	RunnerID   string   `json:"runner_id"`
	Status     string   `json:"status"` // PASSED, FAILED
	ExitCode   int      `json:"exit_code"`
	DurationMs int64    `json:"duration_ms"`
	Error      string   `json:"error,omitempty"`
	Artifacts  []string `json:"artifacts,omitempty"`
}

// CompleteTaskResponse acknowledges completion report.
type CompleteTaskResponse struct {
	Acknowledged bool `json:"acknowledged"`
}
