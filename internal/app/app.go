package app

import (
	"fmt"
	"io"

	"github.com/bdnix/kubelook/internal/version"
)

const usage = "usage: kubelook <serve|migrate|audit-cleanup|version>"

// Main is the whole program. args excludes the program name. It returns the exit code.
func Main(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, version.String())
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n%s\n", args[0], usage)
		return 2
	}
}
