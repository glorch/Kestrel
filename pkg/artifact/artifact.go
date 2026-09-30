package artifact

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Store saves artifacts from the job's workspace to the artifact destination directory.
type Store struct {
	BaseDir string
}

// NewStore creates a new artifact store at the given directory (default: .kestrel/artifacts).
func NewStore(baseDir string) *Store {
	if baseDir == "" {
		baseDir = ".kestrel/artifacts"
	}
	return &Store{BaseDir: baseDir}
}

// Collect copies all specified relative file paths/globs from workDir into destination.
func (s *Store) Collect(workDir, jobName string, patterns []string) ([]string, error) {
	if len(patterns) == 0 {
		return nil, nil
	}

	destDir := filepath.Join(s.BaseDir, jobName)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create artifact dir %s: %w", destDir, err)
	}

	var collected []string
	for _, pattern := range patterns {
		fullPattern := filepath.Join(workDir, pattern)
		matches, err := filepath.Glob(fullPattern)
		if err != nil {
			continue
		}

		for _, match := range matches {
			relPath, err := filepath.Rel(workDir, match)
			if err != nil {
				continue
			}

			targetPath := filepath.Join(destDir, relPath)
			if err := copyPath(match, targetPath); err != nil {
				return collected, fmt.Errorf("failed to copy artifact %s: %w", relPath, err)
			}
			collected = append(collected, relPath)
		}
	}

	return collected, nil
}

func copyPath(src, dst string) error {
	fi, err := os.Stat(src)
	if err != nil {
		return err
	}

	if fi.IsDir() {
		return os.MkdirAll(dst, fi.Mode())
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}
