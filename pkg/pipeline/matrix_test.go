package pipeline

import (
	"strings"
	"testing"
)

func TestMatrixExpansion(t *testing.T) {
	yamlData := `
version: "1.0"
name: "matrix-pipeline"
jobs:
  lint:
    commands: ["echo lint"]
  test:
    needs: [lint]
    matrix:
      go: ["1.22", "1.23"]
      os: ["linux", "windows"]
    commands:
      - "echo 'Running on Go ${{ matrix.go }} on ${{ matrix.os }}'"
  deploy:
    needs: [test]
    commands: ["echo deploy"]
`

	p, err := Parse(strings.NewReader(yamlData))
	if err != nil {
		t.Fatalf("unexpected error parsing matrix: %v", err)
	}

	// 1 lint + (2 go * 2 os = 4 test jobs) + 1 deploy = 6 jobs
	if len(p.Jobs) != 6 {
		t.Fatalf("expected 6 total jobs after matrix expansion, got %d", len(p.Jobs))
	}

	// Verify that deploy depends on all 4 expanded test jobs
	deployJob := p.Jobs["deploy"]
	if len(deployJob.Needs) != 4 {
		t.Errorf("expected deploy to have 4 dependencies, got %d: %v", len(deployJob.Needs), deployJob.Needs)
	}

	// Verify one expanded test job
	var sampleJob *Job
	for id, job := range p.Jobs {
		if strings.HasPrefix(id, "test (") {
			sampleJob = job
			break
		}
	}

	if sampleJob == nil {
		t.Fatal("no expanded test job found")
	}

	if len(sampleJob.Needs) != 1 || sampleJob.Needs[0] != "lint" {
		t.Errorf("expected sample matrix job to inherit 'lint' dependency, got: %v", sampleJob.Needs)
	}

	cmd := sampleJob.Commands[0]
	if strings.Contains(cmd, "${{ matrix.") {
		t.Errorf("placeholder not replaced in command: %s", cmd)
	}
}
