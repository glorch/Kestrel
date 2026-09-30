package logger

import (
	"bytes"
	"strings"
	"testing"
)

func TestLoggerMasking(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	l.RegisterSecrets("super-secret-password-123")

	writer := l.JobLineWriter("test-job")
	writer.Write([]byte("Connecting with super-secret-password-123 now\n"))

	output := buf.String()
	if strings.Contains(output, "super-secret-password-123") {
		t.Errorf("expected secret to be masked, but found it in output: %s", output)
	}
	if !strings.Contains(output, "***") {
		t.Errorf("expected output to contain mask '***', got: %s", output)
	}
}
