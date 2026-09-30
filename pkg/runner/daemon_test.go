package runner

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/glorch/kestrel/pkg/pipeline"
	"github.com/glorch/kestrel/pkg/rpc"
)

type mockTestServer struct {
	registered   bool
	taskPolled   bool
	taskFinished chan *rpc.CompleteTaskRequest
	logsReceived []string
}

func (m *mockTestServer) RegisterRunner(ctx context.Context, req *rpc.RegisterRequest) (*rpc.RegisterResponse, error) {
	m.registered = true
	return &rpc.RegisterResponse{Success: true}, nil
}

func (m *mockTestServer) Heartbeat(ctx context.Context, req *rpc.HeartbeatRequest) (*rpc.HeartbeatResponse, error) {
	return &rpc.HeartbeatResponse{Acknowledged: true}, nil
}

func (m *mockTestServer) PollTask(ctx context.Context, req *rpc.PollTaskRequest) (*rpc.PollTaskResponse, error) {
	if !m.taskPolled {
		m.taskPolled = true
		return &rpc.PollTaskResponse{
			HasTask: true,
			Task: &rpc.TaskSpec{
				TaskID:  "task-test-1",
				RunID:   "run-test-1",
				JobID:   "echo-job",
				JobName: "Echo Step",
				RunsOn:  "host",
				Steps: []*pipeline.Step{
					{Name: "Echo", Run: "echo 'hello from daemon'"},
				},
			},
		}, nil
	}
	return &rpc.PollTaskResponse{HasTask: false}, nil
}

func (m *mockTestServer) SendLogChunk(ctx context.Context, req *rpc.LogChunkRequest) error {
	m.logsReceived = append(m.logsReceived, req.Chunk)
	return nil
}

func (m *mockTestServer) CompleteTask(ctx context.Context, req *rpc.CompleteTaskRequest) (*rpc.CompleteTaskResponse, error) {
	m.taskFinished <- req
	return &rpc.CompleteTaskResponse{Acknowledged: true}, nil
}

func TestRunnerDaemon(t *testing.T) {
	mockServer := &mockTestServer{
		taskFinished: make(chan *rpc.CompleteTaskRequest, 1),
	}

	ts := httptest.NewServer(rpc.NewHTTPHandler(mockServer))
	defer ts.Close()

	daemon := NewDaemon(Config{
		ID:                "test-runner-1",
		ServerURL:         ts.URL,
		Tags:              []string{"host"},
		PollInterval:      50 * time.Millisecond,
		HeartbeatInterval: 100 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = daemon.Start(ctx)
	}()

	// Wait for task completion
	select {
	case comp := <-mockServer.taskFinished:
		if comp.TaskID != "task-test-1" {
			t.Errorf("expected task-test-1, got %s", comp.TaskID)
		}
		if comp.Status != "PASSED" {
			t.Errorf("expected task to pass, got %s", comp.Status)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("timed out waiting for task completion from runner daemon")
	}

	if !mockServer.registered {
		t.Error("expected runner to be registered")
	}
}
