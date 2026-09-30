package pipeline

import (
	"fmt"
	"sort"
	"strings"
)

// ExpandMatrix inspects all jobs in the pipeline, and for any job declaring a Matrix,
// computes the Cartesian product of options, creates expanded sub-jobs,
// and rewires downstream job dependencies to wait for all matrix instances.
func ExpandMatrix(p *Pipeline) error {
	if p == nil || len(p.Jobs) == 0 {
		return nil
	}

	expandedJobs := make(map[string]*Job)
	// Track which original job IDs were expanded into what list of new job IDs
	matrixExpansions := make(map[string][]string)

	for jobID, job := range p.Jobs {
		if len(job.Matrix) == 0 {
			expandedJobs[jobID] = job
			continue
		}

		combinations := cartesianProduct(job.Matrix)
		var newJobIDs []string

		for _, comb := range combinations {
			subJobID := generateMatrixJobID(jobID, comb)
			newJobIDs = append(newJobIDs, subJobID)

			subJob := cloneJob(job)
			subJob.Name = subJobID
			subJob.Matrix = nil // clear matrix definition on expanded instance

			// Injected environment variables for this combination
			if subJob.Env == nil {
				subJob.Env = make(map[string]string)
			}
			for k, v := range comb {
				subJob.Env[strings.ToUpper(k)] = v
				subJob.Env[fmt.Sprintf("MATRIX_%s", strings.ToUpper(k))] = v
			}

			// Apply variable substitution (${{ matrix.key }})
			applyMatrixSubstitutions(subJob, comb)

			expandedJobs[subJobID] = subJob
		}

		matrixExpansions[jobID] = newJobIDs
	}

	// Rewire downstream needs
	for _, job := range expandedJobs {
		var newNeeds []string
		for _, dep := range job.Needs {
			if expandedList, wasExpanded := matrixExpansions[dep]; wasExpanded {
				newNeeds = append(newNeeds, expandedList...)
			} else {
				newNeeds = append(newNeeds, dep)
			}
		}
		job.Needs = newNeeds
	}

	p.Jobs = expandedJobs
	return nil
}

// cartesianProduct generates all combinations of key-value assignments.
func cartesianProduct(matrix map[string][]string) []map[string]string {
	keys := make([]string, 0, len(matrix))
	for k := range matrix {
		keys = append(keys, k)
	}
	sort.Strings(keys) // Ensure deterministic order

	var helper func(idx int, current map[string]string) []map[string]string
	helper = func(idx int, current map[string]string) []map[string]string {
		if idx == len(keys) {
			clone := make(map[string]string, len(current))
			for k, v := range current {
				clone[k] = v
			}
			return []map[string]string{clone}
		}

		key := keys[idx]
		values := matrix[key]
		if len(values) == 0 {
			return helper(idx+1, current)
		}

		var result []map[string]string
		for _, val := range values {
			current[key] = val
			result = append(result, helper(idx+1, current)...)
		}
		return result
	}

	return helper(0, make(map[string]string))
}

func generateMatrixJobID(baseID string, comb map[string]string) string {
	keys := make([]string, 0, len(comb))
	for k := range comb {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, comb[k]))
	}
	return fmt.Sprintf("%s (%s)", baseID, strings.Join(parts, ", "))
}

func applyMatrixSubstitutions(job *Job, comb map[string]string) {
	replace := func(s string) string {
		for k, v := range comb {
			placeholder1 := fmt.Sprintf("${{ matrix.%s }}", k)
			placeholder2 := fmt.Sprintf("${{matrix.%s}}", k)
			s = strings.ReplaceAll(s, placeholder1, v)
			s = strings.ReplaceAll(s, placeholder2, v)
		}
		return s
	}

	job.Image = replace(job.Image)

	for i := range job.Commands {
		job.Commands[i] = replace(job.Commands[i])
	}

	for _, step := range job.Steps {
		step.Name = replace(step.Name)
		step.Run = replace(step.Run)
		for i := range step.Commands {
			step.Commands[i] = replace(step.Commands[i])
		}
	}
}

func cloneJob(j *Job) *Job {
	clone := &Job{
		Name:            j.Name,
		RunsOn:          j.RunsOn,
		Image:           j.Image,
		Needs:           append([]string(nil), j.Needs...),
		WorkDir:         j.WorkDir,
		Timeout:         j.Timeout,
		ContinueOnError: j.ContinueOnError,
		Commands:        append([]string(nil), j.Commands...),
		If:              j.If,
	}

	if j.Env != nil {
		clone.Env = make(map[string]string, len(j.Env))
		for k, v := range j.Env {
			clone.Env[k] = v
		}
	}

	if j.Artifacts != nil {
		clone.Artifacts = &ArtifactConfig{
			Paths: append([]string(nil), j.Artifacts.Paths...),
		}
	}

	if len(j.Steps) > 0 {
		clone.Steps = make([]*Step, len(j.Steps))
		for i, s := range j.Steps {
			clone.Steps[i] = &Step{
				Name:     s.Name,
				Run:      s.Run,
				Commands: append([]string(nil), s.Commands...),
			}
			if s.Env != nil {
				clone.Steps[i].Env = make(map[string]string, len(s.Env))
				for k, v := range s.Env {
					clone.Steps[i].Env[k] = v
				}
			}
		}
	}

	return clone
}
