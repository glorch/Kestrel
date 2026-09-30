package cache

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// CacheEntry records metadata about a stored cache archive.
type CacheEntry struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	CreatedAt    time.Time `json:"created_at"`
	LastAccessed time.Time `json:"last_accessed"`
	ArchivePath  string    `json:"archive_path"`
}

// Manager coordinates storage and retrieval of build cache archives.
type Manager struct {
	mu      sync.RWMutex
	rootDir string
}

// NewManager creates a new Cache Manager.
func NewManager(rootDir string) (*Manager, error) {
	if rootDir == "" {
		rootDir = filepath.Join(".kestrel", "cache")
	}
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to initialize cache dir: %w", err)
	}
	return &Manager{
		rootDir: rootDir,
	}, nil
}

// SafeKey converts any user-provided key into a filesystem-safe filename.
func SafeKey(key string) string {
	h := sha256.Sum256([]byte(key))
	safePrefix := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, key)
	if len(safePrefix) > 40 {
		safePrefix = safePrefix[:40]
	}
	return fmt.Sprintf("%s-%s.tar.gz", safePrefix, hex.EncodeToString(h[:8]))
}

// Save archives specified paths into a cache entry indexed by key.
func (m *Manager) Save(ctx context.Context, key string, baseDir string, targetPaths []string) (*CacheEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if key == "" {
		return nil, fmt.Errorf("cache key cannot be empty")
	}

	fileName := SafeKey(key)
	targetFile := filepath.Join(m.rootDir, fileName)
	tempFile := targetFile + ".tmp"

	f, err := os.Create(tempFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create cache temp file: %w", err)
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(tempFile) // clean up if still exists
	}()

	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	var totalBytes int64
	for _, relPath := range targetPaths {
		fullPath := relPath
		if !filepath.IsAbs(relPath) {
			fullPath = filepath.Join(baseDir, relPath)
		}

		info, err := os.Stat(fullPath)
		if err != nil {
			continue // skip paths that do not exist yet
		}

		if info.IsDir() {
			err = filepath.Walk(fullPath, func(p string, fi os.FileInfo, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				rel, err := filepath.Rel(baseDir, p)
				if err != nil {
					rel = filepath.Base(p)
				}
				return addFileToTar(tw, p, rel, fi)
			})
		} else {
			rel, err := filepath.Rel(baseDir, fullPath)
			if err != nil {
				rel = filepath.Base(fullPath)
			}
			err = addFileToTar(tw, fullPath, rel, info)
		}

		if err != nil {
			return nil, fmt.Errorf("failed to archive path '%s': %w", relPath, err)
		}
	}

	_ = tw.Close()
	_ = gw.Close()
	_ = f.Close()

	// Atomically move to target cache file
	if err := os.Rename(tempFile, targetFile); err != nil {
		return nil, fmt.Errorf("failed to finalize cache file: %w", err)
	}

	stat, _ := os.Stat(targetFile)
	if stat != nil {
		totalBytes = stat.Size()
	}

	entry := &CacheEntry{
		Key:          key,
		Size:         totalBytes,
		CreatedAt:    time.Now(),
		LastAccessed: time.Now(),
		ArchivePath:  targetFile,
	}

	return entry, nil
}

// Restore attempts to unpack a cached archive matching key or fallback restore-keys.
func (m *Manager) Restore(ctx context.Context, key string, restoreKeys []string, targetDir string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. Check exact key match
	exactFile := filepath.Join(m.rootDir, SafeKey(key))
	if fi, err := os.Stat(exactFile); err == nil && !fi.IsDir() {
		if err := extractTarGz(exactFile, targetDir); err != nil {
			return "", false, err
		}
		_ = touchFile(exactFile)
		return key, true, nil
	}

	// 2. Check restore-keys by prefix matching
	files, err := os.ReadDir(m.rootDir)
	if err != nil {
		return "", false, nil
	}

	for _, prefix := range restoreKeys {
		if prefix == "" {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".tar.gz") {
				continue
			}
			// Match if file name has prefix
			if strings.HasPrefix(f.Name(), strings.Map(func(r rune) rune {
				if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
					return r
				}
				return '_'
			}, prefix)) {
				archiveFile := filepath.Join(m.rootDir, f.Name())
				if err := extractTarGz(archiveFile, targetDir); err != nil {
					return "", false, err
				}
				_ = touchFile(archiveFile)
				return prefix, true, nil
			}
		}
	}

	return "", false, nil
}

func addFileToTar(tw *tar.Writer, filePath, tarPath string, fi os.FileInfo) error {
	header, err := tar.FileInfoHeader(fi, fi.Name())
	if err != nil {
		return err
	}
	header.Name = filepath.ToSlash(tarPath)

	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	if fi.IsDir() {
		return nil
	}

	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(tw, file)
	return err
}

func extractTarGz(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target := filepath.Join(destDir, header.Name)
		// ZipSlip protection
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)) {
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			outFile, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, header.FileInfo().Mode())
			if err != nil {
				return err
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()
		}
	}

	return nil
}

func touchFile(path string) error {
	now := time.Now()
	return os.Chtimes(path, now, now)
}
