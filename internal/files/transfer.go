package files

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/execs"
)

var (
	// ErrNoUtilities means the container lacks the commands the browser needs.
	ErrNoUtilities = errors.New("This container has no shell utilities; the file browser can't work here.") //nolint:revive,staticcheck // shown to the user
	// ErrNoTar means directories cannot be transferred.
	ErrNoTar = errors.New("This container has no tar; only single files can be transferred.") //nolint:revive,staticcheck // shown to the user
)

// commandError is a command that ran and failed; its message is what the command printed on stderr.
type commandError struct{ msg string }

func (e *commandError) Error() string { return e.msg }

// capWriter keeps at most `limit` bytes and remembers whether more arrived.
type capWriter struct {
	buf   bytes.Buffer
	limit int
	over  bool
}

func (w *capWriter) Write(p []byte) (int, error) {
	if room := w.limit - w.buf.Len(); room > 0 {
		w.buf.Write(p[:min(len(p), room)])
	}
	if w.buf.Len() >= w.limit && len(p) > 0 {
		w.over = true
	}
	return len(p), nil
}

// strictWriter fails once more than `limit` bytes were written, so the command is stopped.
type strictWriter struct {
	buf   bytes.Buffer
	limit int
}

func (w *strictWriter) Write(p []byte) (int, error) {
	if w.buf.Len()+len(p) > w.limit {
		return 0, fmt.Errorf("the output is larger than %d bytes: %w", w.limit, apperr.ErrTooLarge)
	}
	return w.buf.Write(p)
}

// run executes one command without a TTY. It returns what the command wrote to stderr (capped) and its exit
// code. A command that cannot be started because the utility is missing is ErrNoUtilities.
func (c *channel) run(ctx context.Context, argv []string, stdin io.Reader, stdout io.Writer) (stderr string, code int, err error) {
	errBuf := &capWriter{limit: stderrLimit}
	err = c.d.Executor.Stream(ctx, c.sess, c.req.Cluster,
		execs.ExecRequest{Namespace: c.req.Namespace, Pod: c.req.Pod, Container: c.req.Container, Command: argv, Stdin: stdin != nil},
		execs.StreamIO{Stdin: stdin, Stdout: stdout, Stderr: errBuf})
	text := strings.TrimSpace(errBuf.buf.String())
	var exit *execs.ExitError
	switch {
	case err == nil:
		return text, 0, nil
	case errors.As(err, &exit):
		if exit.Code == 126 || exit.Code == 127 {
			return text, exit.Code, ErrNoUtilities
		}
		return text, exit.Code, nil
	case strings.Contains(err.Error(), "executable file not found") || strings.Contains(err.Error(), "no such file or directory"):
		return text, 127, ErrNoUtilities
	}
	return text, -1, err
}

// check turns a failed command into an error: the files that are missing or forbidden get their own kinds.
func check(stderr string, code int) error {
	if code == 0 {
		return nil
	}
	lower := strings.ToLower(stderr)
	switch {
	case strings.Contains(lower, "no such file"):
		return fmt.Errorf("%s: %w", firstLine(stderr), apperr.ErrNotFound)
	case strings.Contains(lower, "permission denied"), strings.Contains(lower, "read-only file system"):
		return fmt.Errorf("%s: %w", firstLine(stderr), apperr.ErrForbidden)
	case strings.Contains(lower, "is a directory"), strings.Contains(lower, "not a directory"):
		return fmt.Errorf("%s: %w", firstLine(stderr), apperr.ErrInvalid)
	}
	if stderr == "" {
		stderr = fmt.Sprintf("The command failed with exit code %d.", code)
	}
	return &commandError{msg: stderr}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// runCapture runs a command whose whole output is wanted, up to `limit` bytes.
func (c *channel) runCapture(ctx context.Context, argv []string, limit int, stdin io.Reader) (string, error) {
	out := &strictWriter{limit: limit}
	stderr, code, err := c.run(ctx, argv, stdin, out)
	if err != nil {
		return "", err
	}
	if err := check(stderr, code); err != nil {
		return "", err
	}
	return out.buf.String(), nil
}

// chunker forwards what it is given as `chunk` frames of at most ChunkBytes and counts the bytes. Past `limit`
// it fails, which stops the command.
type chunker struct {
	c     *channel
	rid   string
	limit int64
	seq   int
	total int64
	over  bool
	buf   []byte
	mu    sync.Mutex
}

func (k *chunker) Write(p []byte) (int, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.total += int64(len(p))
	if k.total > k.limit {
		k.over = true
		return 0, fmt.Errorf("the transfer is larger than the limit of %d bytes: %w", k.limit, apperr.ErrTooLarge)
	}
	k.buf = append(k.buf, p...)
	for len(k.buf) >= ChunkBytes {
		if err := k.flushLocked(ChunkBytes); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

func (k *chunker) flushLocked(n int) error {
	err := k.c.sink.Send(k.c.ch, "chunk", map[string]any{"rid": k.rid, "seq": k.seq, "data": base64.StdEncoding.EncodeToString(k.buf[:n])})
	k.seq++
	k.buf = k.buf[n:]
	return err
}

// finish sends what is left.
func (k *chunker) finish() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if len(k.buf) == 0 {
		return nil
	}
	return k.flushLocked(len(k.buf))
}
