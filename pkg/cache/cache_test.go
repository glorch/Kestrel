package cache

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCacheSaveAndRestore(t *testing.T) {
	cacheRootDir := t.TempDir()
	mgr, err := NewManager(cacheRootDir)
	if err != nil {
		t.Fatalf("failed to create cache manager: %v", err)
	}

	workDir := t.TempDir()
	targetSubDir := filepath.Join(workDir, "build_output")
	_ = os.MkdirAll(targetSubDir, 0755)

	testFile := filepath.Join(targetSubDir, "artifact.bin")
	testContent := []byte("compiled binary data v1.0")
	if err := os.WriteFile(testFile, testContent, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	ctx := context.Background()
	key := "build-v1-linux"

	// 1. Save cache
	entry, err := mgr.Save(ctx, key, workDir, []string{"build_output"})
	if err != nil {
		t.Fatalf("cache save failed: %v", err)
	}
	if entry.Size <= 0 {
		t.Errorf("expected positive archive size, got: %d", entry.Size)
	}

	// 2. Clean workDir and restore cache into a fresh directory
	restoreDir := t.TempDir()
	matchedKey, found, err := mgr.Restore(ctx, key, nil, restoreDir)
	if err != nil {
		t.Fatalf("cache restore failed: %v", err)
	}
	if !found || matchedKey != key {
		t.Fatalf("expected cache found with key %s, got found=%v, matched=%s", key, found, matchedKey)
	}

	// Verify restored file contents
	restoredFile := filepath.Join(restoreDir, "build_output", "artifact.bin")
	content, err := os.ReadFile(restoredFile)
	if err != nil {
		t.Fatalf("restored file missing: %v", err)
	}
	if string(content) != string(testContent) {
		t.Errorf("content mismatch, expected '%s', got '%s'", testContent, content)
	}
}

func TestCacheRestoreKeysFallback(t *testing.T) {
	cacheRootDir := t.TempDir()
	mgr, err := NewManager(cacheRootDir)
	if err != nil {
		t.Fatalf("failed to create cache manager: %v", err)
	}

	workDir := t.TempDir()
	file := filepath.Join(workDir, "pkg.txt")
	_ = os.WriteFile(file, []byte("pkg data"), 0644)

	ctx := context.Background()
	originalKey := "deps-go-1.23-hashabc"

	_, err = mgr.Save(ctx, originalKey, workDir, []string{"pkg.txt"})
	if err != nil {
		t.Fatalf("failed to save cache: %v", err)
	}

	// Try restoring with a non-existent exact key, but with a matching prefix in restore-keys
	restoreDir := t.TempDir()
	newKey := "deps-go-1.23-hashxyz"
	restoreKeys := []string{"deps-go-1.24-", "deps-go-1.23-"}

	matchedKey, found, err := mgr.Restore(ctx, newKey, restoreKeys, restoreDir)
	if err != nil {
		t.Fatalf("restore failed: %v", err)
	}
	if !found {
		t.Fatal("expected restore-keys prefix match, got not found")
	}
	if matchedKey != "deps-go-1.23-" {
		t.Errorf("expected matched prefix 'deps-go-1.23-', got: %s", matchedKey)
	}

	restoredFile := filepath.Join(restoreDir, "pkg.txt")
	if _, err := os.Stat(restoredFile); err != nil {
		t.Errorf("expected restored file to exist: %v", err)
	}
}
