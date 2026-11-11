package version

import "fmt"

// Version and Commit are set at build time with -ldflags -X.
var (
	Version = "dev"
	Commit  = "unknown"
)

func String() string { return fmt.Sprintf("kubelook %s (commit %s)", Version, Commit) }
