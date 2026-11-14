package execs_test

import (
	"io"
	"testing"

	"github.com/bdnix/kubelook/internal/session"
)

type sessionT = session.Session

// newPipe returns a stdin pipe that is closed with the test, so the executor's copy goroutine ends.
func newPipe(t *testing.T) (*io.PipeReader, *io.PipeWriter) {
	t.Helper()
	r, w := io.Pipe()
	t.Cleanup(func() { _ = w.Close() })
	return r, w
}
