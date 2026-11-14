package execs

import (
	"errors"
	"strings"
)

// ErrNoShell is reported when neither /bin/bash nor /bin/sh can be started.
var ErrNoShell = errors.New("This container has no shell (/bin/bash or /bin/sh).") //nolint:revive,staticcheck // the text is shown to the user

// shellCandidates are tried in order.
var shellCandidates = [][]string{{"/bin/bash"}, {"/bin/sh"}}

// notStarted reports whether the error says the command could not even be started, which is when the next
// candidate is worth trying: the runtime could not find the executable, or the shell answered 126 or 127.
func notStarted(err error) bool {
	var exit *ExitError
	if errors.As(err, &exit) && (exit.Code == 126 || exit.Code == 127) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "executable file not found") || strings.Contains(msg, "no such file or directory")
}

// startShell runs the candidates in order until one starts. `run` blocks until the command has ended. A
// candidate that could not start, before any input was sent (`sent` is false), is replaced by the next;
// when none starts the result is ErrNoShell. It returns the candidate that ran and what `run` returned for it.
func startShell(candidates [][]string, sent func() bool, run func(cmd []string) error) (cmd []string, err error) {
	for _, c := range candidates {
		err = run(c)
		if err == nil || !notStarted(err) || sent() {
			return c, err
		}
	}
	return nil, ErrNoShell
}
