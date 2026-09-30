package version

import (
	"fmt"
	"runtime"
)

var (
	// Version is the current semantic version of Kestrel
	Version = "0.1.0"
	// GitCommit is set by git during build
	GitCommit = "dev"
	// BuildDate is set during build
	BuildDate = "unknown"
)

// Info returns formatted version details
func Info() string {
	return fmt.Sprintf("Kestrel CI/CD Engine v%s (commit: %s, built: %s, %s/%s, %s)",
		Version, GitCommit, BuildDate, runtime.GOOS, runtime.GOARCH, runtime.Version())
}
