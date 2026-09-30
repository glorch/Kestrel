package dag

import (
	"fmt"
	"sort"

	"github.com/glorch/kestrel/pkg/pipeline"
)

// Graph represents a directed acyclic graph for pipeline jobs.
type Graph struct {
	Nodes map[string]*Node
}

// Node represents a single job node in the DAG.
type Node struct {
	ID           string
	Dependencies []string // Upstream jobs this job needs
	Dependents   []string // Downstream jobs that need this job
	InDegree     int
}

// BuildGraph constructs and validates a DAG from a job map.
func BuildGraph(jobs map[string]*pipeline.Job) (*Graph, error) {
	g := &Graph{
		Nodes: make(map[string]*Node),
	}

	// 1. Initialize nodes
	for id := range jobs {
		g.Nodes[id] = &Node{
			ID:           id,
			Dependencies: make([]string, 0),
			Dependents:   make([]string, 0),
			InDegree:     0,
		}
	}

	// 2. Build edges and in-degrees
	for id, job := range jobs {
		for _, dep := range job.Needs {
			if _, exists := g.Nodes[dep]; !exists {
				return nil, fmt.Errorf("job '%s' depends on non-existent job '%s'", id, dep)
			}
			g.Nodes[id].Dependencies = append(g.Nodes[id].Dependencies, dep)
			g.Nodes[dep].Dependents = append(g.Nodes[dep].Dependents, id)
			g.Nodes[id].InDegree++
		}
	}

	return g, nil
}

// ResolveBatches returns sequential execution stages using Kahn's algorithm.
// Jobs within each stage can be executed concurrently.
// Returns an error if a circular dependency is detected.
func (g *Graph) ResolveBatches() ([][]string, error) {
	// Clone in-degree map to keep graph immutable
	inDegree := make(map[string]int, len(g.Nodes))
	for id, node := range g.Nodes {
		inDegree[id] = node.InDegree
	}

	var batches [][]string
	processedCount := 0

	for len(inDegree) > 0 {
		var currentBatch []string
		for id, deg := range inDegree {
			if deg == 0 {
				currentBatch = append(currentBatch, id)
			}
		}

		if len(currentBatch) == 0 {
			// No nodes with in-degree 0, but nodes remain => cycle detected
			var remaining []string
			for id := range inDegree {
				remaining = append(remaining, id)
			}
			sort.Strings(remaining)
			return nil, fmt.Errorf("circular dependency detected among jobs: %v", remaining)
		}

		// Sort batch deterministically
		sort.Strings(currentBatch)
		batches = append(batches, currentBatch)
		processedCount += len(currentBatch)

		// Remove current batch and reduce downstream in-degrees
		for _, id := range currentBatch {
			delete(inDegree, id)
			for _, downstream := range g.Nodes[id].Dependents {
				if _, ok := inDegree[downstream]; ok {
					inDegree[downstream]--
				}
			}
		}
	}

	return batches, nil
}
