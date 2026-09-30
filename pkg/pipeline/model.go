package pipeline

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// Pipeline represents a parsed CI/CD pipeline definition.
type Pipeline struct {
	Version     string             `yaml:"version" json:"version"`
	Name        string             `yaml:"name" json:"name"`
	Env         map[string]string  `yaml:"env,omitempty" json:"env,omitempty"`
	Concurrency *ConcurrencyConfig `yaml:"concurrency,omitempty" json:"concurrency,omitempty"`
	Jobs        map[string]*Job    `yaml:"jobs" json:"jobs"`
}

// ConcurrencyConfig controls exclusive execution and in-flight cancellation for a named group.
type ConcurrencyConfig struct {
	Group            string `yaml:"group" json:"group"`
	CancelInProgress bool   `yaml:"cancel-in-progress,omitempty" json:"cancel_in_progress,omitempty"`
}

// UnmarshalYAML supports both string shorthand (concurrency: "prod") and map representation.
func (c *ConcurrencyConfig) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		c.Group = value.Value
		c.CancelInProgress = false
		return nil
	}
	type raw ConcurrencyConfig
	var r raw
	if err := value.Decode(&r); err != nil {
		return err
	}
	c.Group = r.Group
	c.CancelInProgress = r.CancelInProgress
	return nil
}

// Job represents a single job in the pipeline.
type Job struct {
	Name            string             `yaml:"name,omitempty" json:"name,omitempty"`
	RunsOn          string             `yaml:"runs-on,omitempty" json:"runs_on,omitempty"` // "docker" or "host" (default: "docker" if image is set, else "host")
	Image           string             `yaml:"image,omitempty" json:"image,omitempty"`     // Docker image (e.g., "golang:1.23-alpine")
	Needs           []string           `yaml:"needs,omitempty" json:"needs,omitempty"`     // Upstream job dependencies
	Env             map[string]string  `yaml:"env,omitempty" json:"env,omitempty"`
	WorkDir         string             `yaml:"workdir,omitempty" json:"workdir,omitempty"`
	Timeout         string             `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	ContinueOnError bool               `yaml:"continue-on-error,omitempty" json:"continue_on_error,omitempty"`
	Commands        []string           `yaml:"commands,omitempty" json:"commands,omitempty"` // Shorthand for simple sequential commands
	Steps           []*Step            `yaml:"steps,omitempty" json:"steps,omitempty"`       // Detailed step specifications
	Artifacts       *ArtifactConfig    `yaml:"artifacts,omitempty" json:"artifacts,omitempty"`
	Matrix          map[string][]string `yaml:"matrix,omitempty" json:"matrix,omitempty"` // Multi-dimensional matrix options
	If              string              `yaml:"if,omitempty" json:"if,omitempty"`         // Conditional execution expression
	Environment     string              `yaml:"environment,omitempty" json:"environment,omitempty"` // Target deployment environment
	Approval        bool                `yaml:"approval,omitempty" json:"approval,omitempty"`       // Whether manual approval is required before execution
	Concurrency     *ConcurrencyConfig  `yaml:"concurrency,omitempty" json:"concurrency,omitempty"` // Job-level concurrency group
	Retries         int                 `yaml:"retries,omitempty" json:"retries,omitempty"`                 // Number of automatic retries on failure
	RetryInterval   string              `yaml:"retry-interval,omitempty" json:"retry_interval,omitempty"`   // Delay between retries (e.g. "1s", "500ms")
}

// Step represents an individual execution unit within a job.
type Step struct {
	Name          string            `yaml:"name,omitempty" json:"name,omitempty"`
	Run           string            `yaml:"run,omitempty" json:"run,omitempty"`
	Commands      []string          `yaml:"commands,omitempty" json:"commands,omitempty"`
	Env           map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	Uses          string            `yaml:"uses,omitempty" json:"uses,omitempty"` // Action/plugin to execute, e.g. "actions/setup-go"
	With          map[string]string `yaml:"with,omitempty" json:"with,omitempty"` // Action parameters/inputs
	Retries       int               `yaml:"retries,omitempty" json:"retries,omitempty"`                 // Number of automatic step retries on failure
	RetryInterval string            `yaml:"retry-interval,omitempty" json:"retry_interval,omitempty"`   // Delay between step retries
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

// ParsedRetryInterval returns the duration between job retries.
func (j *Job) ParsedRetryInterval() time.Duration {
	if j.RetryInterval == "" {
		return 500 * time.Millisecond
	}
	d, err := time.ParseDuration(j.RetryInterval)
	if err != nil {
		return 500 * time.Millisecond
	}
	return d
}

// ParsedRetryInterval returns the duration between step retries.
func (s *Step) ParsedRetryInterval() time.Duration {
	if s.RetryInterval == "" {
		return 500 * time.Millisecond
	}
	d, err := time.ParseDuration(s.RetryInterval)
	if err != nil {
		return 500 * time.Millisecond
	}
	return d
}
