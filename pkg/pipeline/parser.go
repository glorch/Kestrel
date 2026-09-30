package pipeline

import (
	"errors"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// Parse reads pipeline configuration from an io.Reader and validates it.
func Parse(r io.Reader) (*Pipeline, error) {
	var p Pipeline
	dec := yaml.NewDecoder(r)
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("failed to parse yaml: %w", err)
	}

	if err := Validate(&p); err != nil {
		return nil, err
	}

	// Apply defaults
	applyDefaults(&p)

	return &p, nil
}

// ParseFile loads and validates a pipeline from a file path.
func ParseFile(path string) (*Pipeline, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open pipeline file %s: %w", path, err)
	}
	defer f.Close()

	return Parse(f)
}

// Validate checks the structural correctness of the pipeline.
func Validate(p *Pipeline) error {
	if p == nil {
		return errors.New("pipeline is nil")
	}

	if len(p.Jobs) == 0 {
		return errors.New("pipeline must contain at least one job in 'jobs'")
	}

	for id, job := range p.Jobs {
		if job == nil {
			return fmt.Errorf("job '%s' is empty", id)
		}

		if len(job.Commands) == 0 && len(job.Steps) == 0 {
			return fmt.Errorf("job '%s' must declare either 'commands' or 'steps'", id)
		}

		// Ensure steps have run commands
		for sIdx, s := range job.Steps {
			if s.Run == "" && len(s.Commands) == 0 {
				return fmt.Errorf("job '%s' step #%d has no 'run' or 'commands'", id, sIdx+1)
			}
		}
	}

	return nil
}

func applyDefaults(p *Pipeline) {
	if p.Version == "" {
		p.Version = "1.0"
	}
	if p.Name == "" {
		p.Name = "unnamed-pipeline"
	}

	for id, job := range p.Jobs {
		if job.Name == "" {
			job.Name = id
		}
		// If runs-on is not specified, infer from image presence
		if job.RunsOn == "" {
			if job.Image != "" {
				job.RunsOn = "docker"
			} else {
				job.RunsOn = "host"
			}
		}
	}
}
