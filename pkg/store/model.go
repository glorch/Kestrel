package store

import (
	"context"
	"time"
)

// PipelineRun represents a record of a pipeline execution.
type PipelineRun struct {
	ID           string            `json:"id"`
	PipelineName string            `json:"pipeline_name"`
	Repo         string            `json:"repo,omitempty"`
	Commit       string            `json:"commit,omitempty"`
	Branch       string            `json:"branch,omitempty"`
	Author       string            `json:"author,omitempty"`
	Status       string            `json:"status"` // PENDING, RUNNING, PASSED, FAILED, CANCELED
	StartedAt    time.Time         `json:"started_at"`
	FinishedAt   *time.Time        `json:"finished_at,omitempty"`
	DurationMs   int64             `json:"duration_ms"`
	Trigger      string            `json:"trigger"` // manual, webhook, cli, schedule
	Env          map[string]string `json:"env,omitempty"`
}

// JobRun represents an execution record of a single job within a pipeline run.
type JobRun struct {
	ID         string     `json:"id"`
	RunID      string     `json:"run_id"`
	JobID      string     `json:"job_id"`
	JobName    string     `json:"job_name"`
	RunsOn     string     `json:"runs_on"`
	Image      string     `json:"image,omitempty"`
	Status     string     `json:"status"`
	ExitCode   int        `json:"exit_code"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	DurationMs int64      `json:"duration_ms"`
	Error      string     `json:"error,omitempty"`
}

// ArtifactMeta records metadata about a generated artifact.
type ArtifactMeta struct {
	ID        string    `json:"id"`
	RunID     string    `json:"run_id"`
	JobID     string    `json:"job_id"`
	Path      string    `json:"path"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

// RunFilter contains query criteria for listing pipeline runs.
type RunFilter struct {
	PipelineName string
	Status       string
	Limit        int
}

// Store defines the persistent storage interface for Kestrel pipeline history.
type Store interface {
	CreateRun(ctx context.Context, run *PipelineRun) error
	UpdateRun(ctx context.Context, run *PipelineRun) error
	GetRun(ctx context.Context, id string) (*PipelineRun, error)
	ListRuns(ctx context.Context, filter RunFilter) ([]*PipelineRun, error)

	SaveJobRun(ctx context.Context, job *JobRun) error
	GetJobRuns(ctx context.Context, runID string) ([]*JobRun, error)

	SaveArtifact(ctx context.Context, art *ArtifactMeta) error
	ListArtifacts(ctx context.Context, runID string) ([]*ArtifactMeta, error)

	Close() error
}
