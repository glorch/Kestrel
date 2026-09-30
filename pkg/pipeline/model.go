package pipeline

import (
	"fmt"
	"time"
)

// Pipeline represents a parsed CI/CD pipeline definition.
type Pipeline struct {
	Version string            `yaml:"version" json:"version"`
	Name    string            `yaml:"name" json:"name"`
	Env     map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	Jobs    map[string]*Job   `yaml:"jobs" json:"jobs"`
}

// Job represents a single job in the pipeline.
type Job struct {
	Name            string            `yaml:"name,omitempty" json:"name,omitempty"`
	RunsOn          string            `yaml:"runs-on,omitempty" json:"runs_on,omitempty"` // "docker" or "host" (default: "docker" if image is set, else "host")
	Image           string            `yaml:"image,omitempty" json:"image,omitempty"`     // Docker image (e.g., "golang:1.23-alpine")
	Needs           []string          `yaml:"needs,omitempty" json:"needs,omitempty"`     // Upstream job dependencies
	Env             map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	WorkDir         string            `yaml:"workdir,omitempty" json:"workdir,omitempty"`
	Timeout         string            `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	ContinueOnError bool              `yaml:"continue-on-error,omitempty" json:"continue_on_error,omitempty"`
	Commands        []string          `yaml:"commands,omitempty" json:"commands,omitempty"` // Shorthand for simple sequential commands
	Steps           []*Step           `yaml:"steps,omitempty" json:"steps,omitempty"`       // Detailed step specifications
	Artifacts       *ArtifactConfig   `yaml:"artifacts,omitempty" json:"artifacts,omitempty"`
}

// Step represents an individual execution unit within a job.
type Step struct {
	Name     string            `yaml:"name,omitempty" json:"name,omitempty"`
	Run      string            `yaml:"run,omitempty" json:"run,omitempty"`
	Commands []string          `yaml:"commands,omitempty" json:"commands,omitempty"`
	Env      map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
}

// ArtifactConfig defines files or directories to preserve after job execution.
type ArtifactConfig struct {
	Paths []string `yaml:"paths" json:"paths"`
}

// NormalizedSteps returns the effective list of steps for the job.
// If Commands is provided, it converts them into individual steps.
func (j *Job) NormalizedSteps() []*Step {
	if len(j.Steps) > 0 {
		return j.Steps
	}
	steps := make([]*Step, 0, len(j.Commands))
	for i, cmd := range j.Commands {
		steps = append(steps, &Step{
			Name: fmt.Sprintf("Step %d", i+1),
			Run:  cmd,
		})
	}
	return steps
}

// ParsedTimeout returns the duration if specified, or a sensible default (30m).
func (j *Job) ParsedTimeout() time.Duration {
	if j.Timeout == "" {
		return 30 * time.Minute
	}
	d, err := time.ParseDuration(j.Timeout)
	if err != nil {
		return 30 * time.Minute
	}
	return d
}
