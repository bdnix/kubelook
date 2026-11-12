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

func untested(a int) int {
	if a > 0 {
		return a + 1
	}
	if a < -10 {
		return a - 1
	}
	return a * 2
}

var _ = untested
