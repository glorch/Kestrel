package plugin

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glorch/kestrel/pkg/pipeline"
)

func TestBuiltInActions(t *testing.T) {
	ctx := context.Background()

	// 1. Test actions/echo
	var buf bytes.Buffer
	actCtx := &ActionContext{
		Workspace: ".",
		Out:       &buf,
	}

	stepEcho := &pipeline.Step{
		Uses: "actions/echo",
		With: map[string]string{"message": "Hello from plugin test"},
	}

	executed, err := ExecuteStepAction(ctx, stepEcho, actCtx)
	if err != nil || !executed {
		t.Fatalf("failed to execute actions/echo: %v", err)
	}

	if !strings.Contains(buf.String(), "Hello from plugin test") {
		t.Errorf("expected output to contain message, got: %s", buf.String())
	}

	// 2. Test actions/setup-go
	buf.Reset()
	stepGo := &pipeline.Step{
		Uses: "actions/setup-go",
		With: map[string]string{"version": "1.23", "goproxy": "https://goproxy.cn"},
	}
	executed, err = ExecuteStepAction(ctx, stepGo, actCtx)
	if err != nil || !executed {
		t.Fatalf("failed to execute actions/setup-go: %v", err)
	}
	if !strings.Contains(buf.String(), "Configured Go environment") {
		t.Errorf("expected go configured message, got: %s", buf.String())
	}

	// 3. Test Unknown action returns error
	stepUnknown := &pipeline.Step{
		Uses: "actions/unknown-nonexistent",
	}
	executed, err = ExecuteStepAction(ctx, stepUnknown, actCtx)
	if err == nil || !executed {
		t.Errorf("expected error for unknown action, got nil")
	}
}

func TestActionsCache(t *testing.T) {
	ctx := context.Background()
	ws := t.TempDir()

	testFile := filepath.Join(ws, "cached_file.txt")
	_ = os.WriteFile(testFile, []byte("data to be cached"), 0644)

	var buf bytes.Buffer
	actCtx := &ActionContext{
		Workspace: ws,
		Out:       &buf,
	}

	// 1. Save cache step
	saveStep := &pipeline.Step{
		Uses: "actions/cache",
		With: map[string]string{
			"path": "cached_file.txt",
			"key":  "test-cache-key-1",
			"mode": "save",
		},
	}
	executed, err := ExecuteStepAction(ctx, saveStep, actCtx)
	if err != nil || !executed {
		t.Fatalf("actions/cache save failed: %v", err)
	}

	// 2. Remove the original file
	_ = os.Remove(testFile)
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Fatal("expected file to be removed")
	}

	// 3. Restore cache step
	buf.Reset()
	restoreStep := &pipeline.Step{
		Uses: "actions/cache",
		With: map[string]string{
			"path": "cached_file.txt",
			"key":  "test-cache-key-1",
			"mode": "restore",
		},
	}
	executed, err = ExecuteStepAction(ctx, restoreStep, actCtx)
	if err != nil || !executed {
		t.Fatalf("actions/cache restore failed: %v", err)
	}

	// Verify file is back!
	restoredData, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("restored file missing: %v", err)
	}
	if string(restoredData) != "data to be cached" {
		t.Errorf("unexpected restored content: %s", string(restoredData))
	}
}

