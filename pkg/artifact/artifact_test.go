package artifact

import (
	"os"
	"path/filepath"
	"testing"
)

func TestArtifactStore(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "kestrel-artifact-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Create dummy test file
	testFile := filepath.Join(tempDir, "sample.txt")
	if err := os.WriteFile(testFile, []byte("hello kestrel artifact"), 0644); err != nil {
		t.Fatal(err)
	}

	storeDir := filepath.Join(tempDir, "store")
	store := NewStore(storeDir)

	collected, err := store.Collect(tempDir, "job1", []string{"sample.txt"})
	if err != nil {
		t.Fatalf("failed to collect artifacts: %v", err)
	}

	if len(collected) != 1 || collected[0] != "sample.txt" {
		t.Errorf("expected collected ['sample.txt'], got %v", collected)
	}

	// Verify target exists
	target := filepath.Join(storeDir, "job1", "sample.txt")
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("artifact not found at target: %v", err)
	}
	if string(content) != "hello kestrel artifact" {
		t.Errorf("content mismatch, got: %s", string(content))
	}
}
