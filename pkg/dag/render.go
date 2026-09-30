package dag

import (
	"fmt"
	"sort"
	"strings"
)

// ToMermaid exports the DAG into a GitHub-compatible Mermaid flowchart string.
func (g *Graph) ToMermaid() string {
	var sb strings.Builder
	sb.WriteString("flowchart TD\n")

	// Collect and sort node IDs for deterministic output
	ids := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		node := g.Nodes[id]
		if len(node.Dependencies) == 0 && len(node.Dependents) == 0 {
			// Standalone node
			sb.WriteString(fmt.Sprintf("    %s[\"%s\"]\n", id, id))
			continue
		}
		for _, dep := range node.Dependencies {
			sb.WriteString(fmt.Sprintf("    %s --> %s\n", dep, id))
		}
	}

	return sb.String()
}

// ToASCII exports a readable stage-by-stage ASCII visual representation of the DAG.
func (g *Graph) ToASCII() (string, error) {
	batches, err := g.ResolveBatches()
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString("Pipeline Execution Plan (Stages & Concurrency):\n")

	for i, batch := range batches {
		stageNum := i + 1
		sb.WriteString(fmt.Sprintf("  Stage %d:\n", stageNum))
		for _, job := range batch {
			node := g.Nodes[job]
			if len(node.Dependencies) == 0 {
				sb.WriteString(fmt.Sprintf("    ├── [%s] (roots)\n", job))
			} else {
				sb.WriteString(fmt.Sprintf("    ├── [%s] (needs: %s)\n", job, strings.Join(node.Dependencies, ", ")))
			}
		}
		if i < len(batches)-1 {
			sb.WriteString("    │\n    ▼\n")
		}
	}

	return sb.String(), nil
}
