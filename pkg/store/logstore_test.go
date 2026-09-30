package store

import (
	"os"
	"strings"
	"sync"
	"testing"
)

func TestFileLogStore(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "kestrel-log-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	ls, err := NewFileLogStore(tempDir)
	if err != nil {
		t.Fatalf("failed to create log store: %v", err)
	}

	taskID := "task-oom-safe-1"

	// 1. Concurrent appends
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = ls.Append(taskID, []byte("log line chunk\n"))
		}(i)
	}
	wg.Wait()

	// 2. Read full content
	data, err := ls.Read(taskID)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}

	count := strings.Count(string(data), "log line chunk")
	if count != 5 {
		t.Errorf("expected 5 chunks written, found %d", count)
	}

	// 3. Append distinct lines and test Tail
	_ = ls.Append(taskID, []byte("tail line 1\n"))
	_ = ls.Append(taskID, []byte("tail line 2\n"))

	tailLines, err := ls.Tail(taskID, 2)
	if err != nil {
		t.Fatalf("tail failed: %v", err)
	}
	if len(tailLines) != 2 || tailLines[0] != "tail line 1" || tailLines[1] != "tail line 2" {
		t.Errorf("unexpected tail lines: %v", tailLines)
	}

	// 4. Non-existent task returns empty
	emptyData, err := ls.Read("non-existent-task")
	if err != nil || len(emptyData) != 0 {
		t.Errorf("expected empty data for non-existent task, got %v", emptyData)
	}
}
