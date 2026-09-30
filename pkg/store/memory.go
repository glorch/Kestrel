package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

var (
	ErrNotFound = errors.New("record not found")
)

// MemoryStore provides a thread-safe in-memory implementation of Store.
type MemoryStore struct {
	mu        sync.RWMutex
	runs      map[string]*PipelineRun
	jobs      map[string][]*JobRun // runID -> []JobRun
	artifacts map[string][]*ArtifactMeta
}

// NewMemoryStore creates a new in-memory Store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		runs:      make(map[string]*PipelineRun),
		jobs:      make(map[string][]*JobRun),
		artifacts: make(map[string][]*ArtifactMeta),
	}
}

func (s *MemoryStore) CreateRun(ctx context.Context, run *PipelineRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if run == nil || run.ID == "" {
		return errors.New("invalid run: id is required")
	}

	if _, exists := s.runs[run.ID]; exists {
		return fmt.Errorf("run with id %s already exists", run.ID)
	}

	// Store copy
	clone := *run
	s.runs[run.ID] = &clone
	return nil
}

func (s *MemoryStore) UpdateRun(ctx context.Context, run *PipelineRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if run == nil || run.ID == "" {
		return errors.New("invalid run: id is required")
	}

	if _, exists := s.runs[run.ID]; !exists {
		return ErrNotFound
	}

	clone := *run
	s.runs[run.ID] = &clone
	return nil
}

func (s *MemoryStore) GetRun(ctx context.Context, id string) (*PipelineRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	run, ok := s.runs[id]
	if !ok {
		return nil, ErrNotFound
	}

	clone := *run
	return &clone, nil
}

func (s *MemoryStore) ListRuns(ctx context.Context, filter RunFilter) ([]*PipelineRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*PipelineRun
	for _, r := range s.runs {
		if filter.PipelineName != "" && r.PipelineName != filter.PipelineName {
			continue
		}
		if filter.Status != "" && r.Status != filter.Status {
			continue
		}
		clone := *r
		result = append(result, &clone)
	}

	// Sort by StartedAt descending (newest first)
	sort.Slice(result, func(i, j int) bool {
		return result[i].StartedAt.After(result[j].StartedAt)
	})

	if filter.Limit > 0 && len(result) > filter.Limit {
		result = result[:filter.Limit]
	}

	return result, nil
}

func (s *MemoryStore) SaveJobRun(ctx context.Context, job *JobRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if job == nil || job.RunID == "" {
		return errors.New("invalid job run: run_id is required")
	}

	list := s.jobs[job.RunID]
	// Replace existing or append
	found := false
	clone := *job
	for i, j := range list {
		if j.ID == job.ID || (j.JobID == job.JobID && job.ID == "") {
			list[i] = &clone
			found = true
			break
		}
	}
	if !found {
		list = append(list, &clone)
	}
	s.jobs[job.RunID] = list
	return nil
}

func (s *MemoryStore) GetJobRuns(ctx context.Context, runID string) ([]*JobRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list, ok := s.jobs[runID]
	if !ok {
		return []*JobRun{}, nil
	}

	result := make([]*JobRun, len(list))
	for i, j := range list {
		clone := *j
		result[i] = &clone
	}
	return result, nil
}

func (s *MemoryStore) SaveArtifact(ctx context.Context, art *ArtifactMeta) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if art == nil || art.RunID == "" {
		return errors.New("invalid artifact: run_id is required")
	}

	clone := *art
	s.artifacts[art.RunID] = append(s.artifacts[art.RunID], &clone)
	return nil
}

func (s *MemoryStore) ListArtifacts(ctx context.Context, runID string) ([]*ArtifactMeta, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := s.artifacts[runID]
	result := make([]*ArtifactMeta, len(list))
	for i, a := range list {
		clone := *a
		result[i] = &clone
	}
	return result, nil
}

func (s *MemoryStore) Close() error {
	return nil
}
