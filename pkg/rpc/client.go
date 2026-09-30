package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Client handles RPC communication with a Kestrel server.
type Client struct {
	serverURL  string
	httpClient *http.Client
}

// NewClient creates an RPC client targeting serverURL (e.g. "http://localhost:8080").
func NewClient(serverURL string) *Client {
	serverURL = strings.TrimSuffix(serverURL, "/")
	if !strings.HasPrefix(serverURL, "http://") && !strings.HasPrefix(serverURL, "https://") {
		serverURL = "http://" + serverURL
	}
	return &Client{
		serverURL: serverURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *Client) post(ctx context.Context, path string, in interface{}, out interface{}) error {
	data, err := json.Marshal(in)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s%s", c.serverURL, path)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("server returned error HTTP %d", resp.StatusCode)
	}

	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// Register registers the runner with the server.
func (c *Client) Register(ctx context.Context, info RunnerInfo) (*RegisterResponse, error) {
	var resp RegisterResponse
	err := c.post(ctx, "/rpc/runner/register", &RegisterRequest{Runner: info}, &resp)
	return &resp, err
}

// Heartbeat sends a liveness ping.
func (c *Client) Heartbeat(ctx context.Context, runnerID string, activeJobs int) (*HeartbeatResponse, error) {
	var resp HeartbeatResponse
	err := c.post(ctx, "/rpc/runner/heartbeat", &HeartbeatRequest{
		RunnerID:   runnerID,
		Timestamp:  time.Now(),
		ActiveJobs: activeJobs,
	}, &resp)
	return &resp, err
}

// PollTask queries the server for pending jobs.
func (c *Client) PollTask(ctx context.Context, runnerID string, tags []string) (*PollTaskResponse, error) {
	var resp PollTaskResponse
	err := c.post(ctx, "/rpc/task/poll", &PollTaskRequest{
		RunnerID: runnerID,
		Tags:     tags,
	}, &resp)
	return &resp, err
}

// SendLogChunk pushes streamed log output to the server.
func (c *Client) SendLogChunk(ctx context.Context, taskID, runnerID, chunk string) error {
	return c.post(ctx, "/rpc/task/log", &LogChunkRequest{
		TaskID:    taskID,
		RunnerID:  runnerID,
		Chunk:     chunk,
		Timestamp: time.Now(),
	}, nil)
}

// CompleteTask reports task execution completion.
func (c *Client) CompleteTask(ctx context.Context, req *CompleteTaskRequest) (*CompleteTaskResponse, error) {
	var resp CompleteTaskResponse
	err := c.post(ctx, "/rpc/task/complete", req, &resp)
	return &resp, err
}
