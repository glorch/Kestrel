package rpc

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

type mockServerHandler struct {
	registeredRunner *RunnerInfo
	heartbeatCount   int
	assignedTask     *TaskSpec
	receivedLogs     []string
	completedTask    *CompleteTaskRequest
}

func (m *mockServerHandler) RegisterRunner(ctx context.Context, req *RegisterRequest) (*RegisterResponse, error) {
	m.registeredRunner = &req.Runner
	return &RegisterResponse{Success: true}, nil
}

func (m *mockServerHandler) Heartbeat(ctx context.Context, req *HeartbeatRequest) (*HeartbeatResponse, error) {
	m.heartbeatCount++
	return &HeartbeatResponse{Acknowledged: true}, nil
}

func (m *mockServerHandler) PollTask(ctx context.Context, req *PollTaskRequest) (*PollTaskResponse, error) {
	if m.assignedTask != nil {
		task := m.assignedTask
		m.assignedTask = nil // Return once
		return &PollTaskResponse{HasTask: true, Task: task}, nil
	}
	return &PollTaskResponse{HasTask: false}, nil
}

func (m *mockServerHandler) SendLogChunk(ctx context.Context, req *LogChunkRequest) error {
	m.receivedLogs = append(m.receivedLogs, req.Chunk)
	return nil
}

func (m *mockServerHandler) CompleteTask(ctx context.Context, req *CompleteTaskRequest) (*CompleteTaskResponse, error) {
	m.completedTask = req
	return &CompleteTaskResponse{Acknowledged: true}, nil
}

func TestRPCRoundTrip(t *testing.T) {
	mockHandler := &mockServerHandler{
		assignedTask: &TaskSpec{
			TaskID:  "task-123",
			RunID:   "run-123",
			JobID:   "build",
			JobName: "Build Application",
			RunsOn:  "host",
		},
	}

	server := httptest.NewServer(NewHTTPHandler(mockHandler))
	defer server.Close()

	client := NewClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. Test Register
	info := RunnerInfo{
		ID:       "runner-node-1",
		Name:     "Test Runner",
		OS:       "linux",
		Arch:     "amd64",
		Tags:     []string{"docker", "host"},
		Capacity: 2,
	}
	regResp, err := client.Register(ctx, info)
	if err != nil || !regResp.Success {
		t.Fatalf("register failed: %v", err)
	}
	if mockHandler.registeredRunner == nil || mockHandler.registeredRunner.ID != "runner-node-1" {
		t.Errorf("runner registration not recorded properly")
	}

	// 2. Test Heartbeat
	hbResp, err := client.Heartbeat(ctx, "runner-node-1", 0)
	if err != nil || !hbResp.Acknowledged {
		t.Fatalf("heartbeat failed: %v", err)
	}
	if mockHandler.heartbeatCount != 1 {
		t.Errorf("expected 1 heartbeat, got %d", mockHandler.heartbeatCount)
	}

	// 3. Test PollTask
	pollResp, err := client.PollTask(ctx, "runner-node-1", []string{"host"})
	if err != nil || !pollResp.HasTask {
		t.Fatalf("poll task failed or empty: %v", err)
	}
	if pollResp.Task.TaskID != "task-123" {
		t.Errorf("expected task-123, got: %s", pollResp.Task.TaskID)
	}

	// 4. Test SendLogChunk
	err = client.SendLogChunk(ctx, "task-123", "runner-node-1", "compiling sources...\n")
	if err != nil {
		t.Fatalf("send log chunk failed: %v", err)
	}
	if len(mockHandler.receivedLogs) != 1 || mockHandler.receivedLogs[0] != "compiling sources...\n" {
		t.Errorf("log chunk mismatch: %v", mockHandler.receivedLogs)
	}

	// 5. Test CompleteTask
	completeReq := &CompleteTaskRequest{
		TaskID:     "task-123",
		RunnerID:   "runner-node-1",
		Status:     "PASSED",
		ExitCode:   0,
		DurationMs: 1500,
	}
	compResp, err := client.CompleteTask(ctx, completeReq)
	if err != nil || !compResp.Acknowledged {
		t.Fatalf("complete task failed: %v", err)
	}
	if mockHandler.completedTask == nil || mockHandler.completedTask.Status != "PASSED" {
		t.Errorf("completed task not recorded properly")
	}
}
