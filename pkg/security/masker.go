package security

import (
	"io"
	"sort"
	"strings"
	"sync"
)

const (
	// MaskReplacement is the string used to obfuscate secrets in logs.
	MaskReplacement = "***"
	// MinSecretLength is the minimum length for a secret to be considered for masking
	// (prevents accidentally masking single-character or trivial strings).
	MinSecretLength = 3
)

// Masker safely redacts sensitive strings from log streams and texts.
type Masker struct {
	mu      sync.RWMutex
	secrets []string
}

// NewMasker creates an empty secrets Masker.
func NewMasker() *Masker {
	return &Masker{
		secrets: make([]string, 0),
	}
}

// Register adds one or more secrets to be masked.
func (m *Masker) Register(secrets ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, s := range secrets {
		s = strings.TrimSpace(s)
		if len(s) < MinSecretLength {
			continue
		}

		// Avoid duplicates
		exists := false
		for _, existing := range m.secrets {
			if existing == s {
				exists = true
				break
			}
		}
		if !exists {
			m.secrets = append(m.secrets, s)
		}
	}

	// Sort secrets by descending length so longer matching secrets are replaced first
	sort.Slice(m.secrets, func(i, j int) bool {
		return len(m.secrets[i]) > len(m.secrets[j])
	})
}

// Mask replaces all registered secrets in the input string with the MaskReplacement token.
func (m *Masker) Mask(text string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.secrets) == 0 || len(text) == 0 {
		return text
	}

	res := text
	for _, secret := range m.secrets {
		if strings.Contains(res, secret) {
			res = strings.ReplaceAll(res, secret, MaskReplacement)
		}
	}
	return res
}

// MaskBytes replaces all registered secrets in a byte slice.
func (m *Masker) MaskBytes(b []byte) []byte {
	return []byte(m.Mask(string(b)))
}

// MaskingWriter wraps an io.Writer to ensure that any written bytes have secrets redacted.
type MaskingWriter struct {
	target io.Writer
	masker *Masker
	mu     sync.Mutex
}

// NewMaskingWriter returns an io.Writer that redacts secrets before forwarding to target.
func NewMaskingWriter(target io.Writer, masker *Masker) *MaskingWriter {
	return &MaskingWriter{
		target: target,
		masker: masker,
	}
}

func (mw *MaskingWriter) Write(p []byte) (n int, err error) {
	mw.mu.Lock()
	defer mw.mu.Unlock()

	if mw.masker == nil {
		return mw.target.Write(p)
	}

	masked := mw.masker.MaskBytes(p)
	// Write masked output to target
	_, err = mw.target.Write(masked)
	// Return original length to satisfy io.Writer contract
	return len(p), err
}

// MaskReader wraps an io.Reader to redact secrets when reading.
type MaskReader struct {
	src    io.Reader
	masker *Masker
}

func NewMaskReader(src io.Reader, masker *Masker) *MaskReader {
	return &MaskReader{src: src, masker: masker}
}

func (mr *MaskReader) Read(p []byte) (n int, err error) {
	buf := make([]byte, len(p))
	n, err = mr.src.Read(buf)
	if n > 0 && mr.masker != nil {
		masked := mr.masker.MaskBytes(buf[:n])
		copy(p, masked)
		if len(masked) < len(p) {
			return len(masked), err
		}
	} else if n > 0 {
		copy(p, buf[:n])
	}
	return n, err
}

// SanitizeMap takes an environment map and redacts values whose keys indicate sensitive information.
func SanitizeMap(env map[string]string) map[string]string {
	sensitiveKeyTokens := []string{
		"PASSWORD", "SECRET", "TOKEN", "KEY", "AUTH", "PASSWD", "CREDENTIAL",
	}

	sanitized := make(map[string]string, len(env))
	for k, v := range env {
		upperKey := strings.ToUpper(k)
		isSecret := false
		for _, token := range sensitiveKeyTokens {
			if strings.Contains(upperKey, token) {
				isSecret = true
				break
			}
		}
		if isSecret {
			sanitized[k] = MaskReplacement
		} else {
			sanitized[k] = v
		}
	}
	return sanitized
}
