package logger

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/fatih/color"
)

var (
	cyanBold    = color.New(color.FgCyan, color.Bold).SprintFunc()
	greenBold   = color.New(color.FgGreen, color.Bold).SprintFunc()
	redBold     = color.New(color.FgRed, color.Bold).SprintFunc()
	yellowBold  = color.New(color.FgYellow, color.Bold).SprintFunc()
	magentaBold = color.New(color.FgMagenta, color.Bold).SprintFunc()
	gray        = color.New(color.FgHiBlack).SprintFunc()
	whiteBold   = color.New(color.FgWhite, color.Bold).SprintFunc()
)

// Logger provides thread-safe colored output for jobs and pipeline events.
type Logger struct {
	mu  sync.Mutex
	out io.Writer
}

// New creates a new Logger writing to stdout by default.
func New(out io.Writer) *Logger {
	if out == nil {
		out = os.Stdout
	}
	return &Logger{out: out}
}

// Default returns a standard stdout logger.
func Default() *Logger {
	return New(os.Stdout)
}

func (l *Logger) timestamp() string {
	return gray(time.Now().Format("15:04:05"))
}

// Stage prints a high-level stage announcement.
func (l *Logger) Stage(stageNum int, totalStages int, jobs []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.out, "\n%s %s Stage %d/%d: %s\n",
		l.timestamp(),
		cyanBold("⚡"),
		stageNum,
		totalStages,
		whiteBold(fmt.Sprintf("%v", jobs)),
	)
}

// JobStart logs that a job has begun.
func (l *Logger) JobStart(jobID, image string, runsOn string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	modeInfo := runsOn
	if image != "" {
		modeInfo = fmt.Sprintf("%s (%s)", runsOn, image)
	}
	fmt.Fprintf(l.out, "%s [%s] %s Starting job on %s\n",
		l.timestamp(),
		magentaBold(jobID),
		cyanBold("▶"),
		gray(modeInfo),
	)
}

// JobSuccess logs successful job completion.
func (l *Logger) JobSuccess(jobID string, duration time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.out, "%s [%s] %s Job passed in %s\n",
		l.timestamp(),
		magentaBold(jobID),
		greenBold("✔"),
		gray(duration.Round(time.Millisecond).String()),
	)
}

// JobFailed logs a job failure.
func (l *Logger) JobFailed(jobID string, exitCode int, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	errMsg := ""
	if err != nil {
		errMsg = fmt.Sprintf(": %v", err)
	}
	fmt.Fprintf(l.out, "%s [%s] %s Job failed (exit code %d)%s\n",
		l.timestamp(),
		magentaBold(jobID),
		redBold("✘"),
		exitCode,
		errMsg,
	)
}

// JobSkipped logs a skipped job.
func (l *Logger) JobSkipped(jobID, reason string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.out, "%s [%s] %s Skipped: %s\n",
		l.timestamp(),
		magentaBold(jobID),
		yellowBold("⊘"),
		gray(reason),
	)
}

// StepStart logs step initiation.
func (l *Logger) StepStart(jobID, stepName string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.out, "%s [%s]   %s Running: %s\n",
		l.timestamp(),
		magentaBold(jobID),
		gray("•"),
		whiteBold(stepName),
	)
}

// Info prints general information.
func (l *Logger) Info(format string, a ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	msg := fmt.Sprintf(format, a...)
	fmt.Fprintf(l.out, "%s %s %s\n", l.timestamp(), cyanBold("ℹ"), msg)
}

// Error prints error messages.
func (l *Logger) Error(format string, a ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	msg := fmt.Sprintf(format, a...)
	fmt.Fprintf(l.out, "%s %s %s\n", l.timestamp(), redBold("✘"), msg)
}

// JobLineWriter creates an io.Writer that prepends each line with the job ID prefix.
func (l *Logger) JobLineWriter(jobID string) io.Writer {
	return &prefixWriter{
		l:      l,
		prefix: fmt.Sprintf("[%s]", jobID),
	}
}

type prefixWriter struct {
	l      *Logger
	prefix string
	buf    []byte
}

func (pw *prefixWriter) Write(p []byte) (n int, err error) {
	pw.l.mu.Lock()
	defer pw.l.mu.Unlock()

	for _, b := range p {
		if b == '\n' {
			fmt.Fprintf(pw.l.out, "         %s %s\n",
				gray(pw.prefix),
				string(pw.buf),
			)
			pw.buf = pw.buf[:0]
		} else if b != '\r' {
			pw.buf = append(pw.buf, b)
		}
	}
	return len(p), nil
}

// Flush writes any remaining buffered bytes.
func (pw *prefixWriter) Flush() {
	pw.l.mu.Lock()
	defer pw.l.mu.Unlock()
	if len(pw.buf) > 0 {
		fmt.Fprintf(pw.l.out, "         %s %s\n", gray(pw.prefix), string(pw.buf))
		pw.buf = pw.buf[:0]
	}
}
