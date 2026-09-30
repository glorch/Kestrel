package dag

import (
	"reflect"
	"testing"

	"github.com/glorch/kestrel/pkg/pipeline"
)

func TestBuildGraphAndResolveBatches(t *testing.T) {
	// Diamond pattern:
	//      lint
	//     /    \
	//   test   sec
	//     \    /
	//     build
	jobs := map[string]*pipeline.Job{
		"lint":  {},
		"test":  {Needs: []string{"lint"}},
		"sec":   {Needs: []string{"lint"}},
		"build": {Needs: []string{"test", "sec"}},
	}

	g, err := BuildGraph(jobs)
	if err != nil {
		t.Fatalf("failed to build graph: %v", err)
	}

	batches, err := g.ResolveBatches()
	if err != nil {
		t.Fatalf("failed to resolve batches: %v", err)
	}

	expected := [][]string{
		{"lint"},
		{"sec", "test"},
		{"build"},
	}

	if !reflect.DeepEqual(batches, expected) {
		t.Errorf("expected batches %v, got %v", expected, batches)
	}
}

func TestCircularDependency(t *testing.T) {
	// A -> B -> C -> A
	jobs := map[string]*pipeline.Job{
		"A": {Needs: []string{"C"}},
		"B": {Needs: []string{"A"}},
		"C": {Needs: []string{"B"}},
	}

	g, err := BuildGraph(jobs)
	if err != nil {
		t.Fatalf("failed to build graph: %v", err)
	}

	_, err = g.ResolveBatches()
	if err == nil {
		t.Fatal("expected circular dependency error, got nil")
	}
}

func TestMissingDependency(t *testing.T) {
	jobs := map[string]*pipeline.Job{
		"build": {Needs: []string{"nonexistent"}},
	}

	_, err := BuildGraph(jobs)
	if err == nil {
		t.Fatal("expected error for non-existent dependency, got nil")
	}
}
