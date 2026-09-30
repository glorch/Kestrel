package rpc

import (
	"context"
	"encoding/json"
	"net/http"
)

// ServerHandler specifies the server-side callback interface for RPC operations.
type ServerHandler interface {
	RegisterRunner(ctx context.Context, req *RegisterRequest) (*RegisterResponse, error)
	Heartbeat(ctx context.Context, req *HeartbeatRequest) (*HeartbeatResponse, error)
	PollTask(ctx context.Context, req *PollTaskRequest) (*PollTaskResponse, error)
	SendLogChunk(ctx context.Context, req *LogChunkRequest) error
	CompleteTask(ctx context.Context, req *CompleteTaskRequest) (*CompleteTaskResponse, error)
}

// NewHTTPHandler wraps a ServerHandler into an http.Handler.
func NewHTTPHandler(handler ServerHandler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/rpc/runner/register", func(w http.ResponseWriter, r *http.Request) {
		var req RegisterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp, err := handler.RegisterRunner(r.Context(), &req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, resp)
	})

	mux.HandleFunc("/rpc/runner/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		var req HeartbeatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp, err := handler.Heartbeat(r.Context(), &req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, resp)
	})

	mux.HandleFunc("/rpc/task/poll", func(w http.ResponseWriter, r *http.Request) {
		var req PollTaskRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp, err := handler.PollTask(r.Context(), &req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, resp)
	})

	mux.HandleFunc("/rpc/task/log", func(w http.ResponseWriter, r *http.Request) {
		var req LogChunkRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := handler.SendLogChunk(r.Context(), &req); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/rpc/task/complete", func(w http.ResponseWriter, r *http.Request) {
		var req CompleteTaskRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp, err := handler.CompleteTask(r.Context(), &req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, resp)
	})

	return mux
}

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}
