package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// FileStore provides persistent storage by saving run records as JSON files on disk.
type FileStore struct {
	mu      sync.RWMutex
	baseDir string
	mem     *MemoryStore
}

// NewFileStore initializes a persistent FileStore rooted at baseDir (default: .kestrel/store).
func NewFileStore(baseDir string) (*FileStore, error) {
	if baseDir == "" {
		baseDir = ".kestrel/store"
	}

	runsDir := filepath.Join(baseDir, "runs")
	if err := os.MkdirAll(runsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create store directory: %w", err)
	}

	fs := &FileStore{
		baseDir: baseDir,
		mem:     NewMemoryStore(),
	}

	// Load existing runs from disk into cache
	if err := fs.loadFromDisk(); err != nil {
		return nil, err
	}

	return fs, nil
}

func (fs *FileStore) loadFromDisk() error {
	runsDir := filepath.Join(fs.baseDir, "runs")
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		return nil // directory empty or unreadable
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		filePath := filepath.Join(runsDir, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		var run PipelineRun
		if err := json.Unmarshal(data, &run); err == nil && run.ID != "" {
			_ = fs.mem.CreateRun(context.Background(), &run)
		}
	}
	return nil
}

func (fs *FileStore) saveRunToDisk(run *PipelineRun) error {
	filePath := filepath.Join(fs.baseDir, "runs", fmt.Sprintf("%s.json", run.ID))
	data, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0644)
}

func (fs *FileStore) CreateRun(ctx context.Context, run *PipelineRun) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if err := fs.mem.CreateRun(ctx, run); err != nil {
		return err
	}
	return fs.saveRunToDisk(run)
}

func (fs *FileStore) UpdateRun(ctx context.Context, run *PipelineRun) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if err := fs.mem.UpdateRun(ctx, run); err != nil {
		return err
	}
	return fs.saveRunToDisk(run)
}

func (fs *FileStore) GetRun(ctx context.Context, id string) (*PipelineRun, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return fs.mem.GetRun(ctx, id)
}

func (fs *FileStore) ListRuns(ctx context.Context, filter RunFilter) ([]*PipelineRun, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return fs.mem.ListRuns(ctx, filter)
}

func (fs *FileStore) SaveJobRun(ctx context.Context, job *JobRun) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.mem.SaveJobRun(ctx, job)
}

func (fs *FileStore) GetJobRuns(ctx context.Context, runID string) ([]*JobRun, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return fs.mem.GetJobRuns(ctx, runID)
}

func (fs *FileStore) SaveArtifact(ctx context.Context, art *ArtifactMeta) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.mem.SaveArtifact(ctx, art)
}

func (fs *FileStore) ListArtifacts(ctx context.Context, runID string) ([]*ArtifactMeta, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return fs.mem.ListArtifacts(ctx, runID)
}

func (fs *FileStore) Close() error {
	return nil
}
