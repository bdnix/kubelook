// Package version holds the build version.
package version

import "fmt"

// Version and Commit are set at build time with -ldflags -X.
var (
	Version = "dev"
	Commit  = "unknown"
)

// String returns the human-readable version line.
func String() string { return fmt.Sprintf("kubelook %s (commit %s)", Version, Commit) }
