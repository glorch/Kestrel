package pipeline

import (
	"strings"
	"testing"
)

func TestParseValidYAML(t *testing.T) {
	yamlData := `
version: "1.0"
name: "test-pipeline"
env:
  FOO: "bar"
jobs:
  lint:
    image: "golang:1.23-alpine"
    commands:
      - "go vet ./..."
  test:
    needs: [lint]
    commands:
      - "go test ./..."
`
	p, err := Parse(strings.NewReader(yamlData))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Name != "test-pipeline" {
		t.Errorf("expected name 'test-pipeline', got %s", p.Name)
	}
	if len(p.Jobs) != 2 {
		t.Errorf("expected 2 jobs, got %d", len(p.Jobs))
	}
	if p.Jobs["lint"].RunsOn != "docker" {
		t.Errorf("expected runs-on docker, got %s", p.Jobs["lint"].RunsOn)
	}
	if p.Jobs["test"].RunsOn != "host" {
		t.Errorf("expected runs-on host, got %s", p.Jobs["test"].RunsOn)
	}
	if len(p.Jobs["test"].Needs) != 1 || p.Jobs["test"].Needs[0] != "lint" {
		t.Errorf("expected needs [lint], got %v", p.Jobs["test"].Needs)
	}
}

func TestParseInvalidYAML(t *testing.T) {
	emptyYaml := `
version: "1.0"
jobs: {}
`
	_, err := Parse(strings.NewReader(emptyYaml))
	if err == nil {
		t.Error("expected error for empty jobs, got nil")
	}
}
