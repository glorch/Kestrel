package store

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// LogStore defines the storage contract for streaming task execution logs.
type LogStore interface {
	Append(taskID string, chunk []byte) error
	Read(taskID string) ([]byte, error)
	Reader(taskID string) (io.ReadCloser, error)
	Tail(taskID string, lines int) ([]string, error)
	Close() error
}

// FileLogStore streams logs directly to disk files to prevent in-memory OOM.
type FileLogStore struct {
	baseDir string
	mu      sync.RWMutex
	filesMu map[string]*sync.Mutex
}

// NewFileLogStore initializes a disk-backed log store rooted at baseDir (default: .kestrel/logs).
func NewFileLogStore(baseDir string) (*FileLogStore, error) {
	if baseDir == "" {
		baseDir = ".kestrel/logs"
	}

	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	return &FileLogStore{
		baseDir: baseDir,
		filesMu: make(map[string]*sync.Mutex),
	}, nil
}

func (fls *FileLogStore) getFileMutex(taskID string) *sync.Mutex {
	fls.mu.Lock()
	defer fls.mu.Unlock()

	mu, exists := fls.filesMu[taskID]
	if !exists {
		mu = &sync.Mutex{}
		fls.filesMu[taskID] = mu
	}
	return mu
}

func (fls *FileLogStore) taskLogPath(taskID string) string {
	return filepath.Join(fls.baseDir, fmt.Sprintf("%s.log", taskID))
}

// Append appends log chunks directly to the task's disk file.
func (fls *FileLogStore) Append(taskID string, chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}

	mu := fls.getFileMutex(taskID)
	mu.Lock()
	defer mu.Unlock()

	path := fls.taskLogPath(taskID)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file %s: %w", path, err)
	}
	defer f.Close()

	_, err = f.Write(chunk)
	return err
}

// Read returns the full log contents for the given task.
func (fls *FileLogStore) Read(taskID string) ([]byte, error) {
	mu := fls.getFileMutex(taskID)
	mu.Lock()
	defer mu.Unlock()

	path := fls.taskLogPath(taskID)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return []byte{}, nil
	}
	return os.ReadFile(path)
}

// Reader returns an open io.ReadCloser for the task log.
func (fls *FileLogStore) Reader(taskID string) (io.ReadCloser, error) {
	path := fls.taskLogPath(taskID)
	return os.Open(path)
}

// Tail returns the last n lines of a task log.
func (fls *FileLogStore) Tail(taskID string, n int) ([]string, error) {
	mu := fls.getFileMutex(taskID)
	mu.Lock()
	defer mu.Unlock()

	path := fls.taskLogPath(taskID)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	defer f.Close()

	var allLines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		allLines = append(allLines, scanner.Text())
	}

	if len(allLines) <= n {
		return allLines, nil
	}
	return allLines[len(allLines)-n:], nil
}

func (fls *FileLogStore) Close() error {
	return nil
}
