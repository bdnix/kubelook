package execs

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/ws"
)

// Limits.
const (
	maxOutFrame   = 32 << 10
	maxStdinQueue = 1 << 20
	keepAliveEach = time.Minute
	// DefaultIdleTimeout applies when the configuration sets none.
	DefaultIdleTimeout = 30 * time.Minute
)

// Guard says whether the server refuses new work (the memory guard of Step 6.3).
type Guard interface{ Busy() bool }

type neverBusy struct{}

func (neverBusy) Busy() bool { return false }

// Sink is where a shell sends frames; *ws.Conn implements it.
type Sink interface {
	Send(ch int, typ string, d any) error
	CloseChannel(ch int, reason string)
}

// Deps are the dependencies of the shell channel.
type Deps struct {
	Executor    Executor
	Clock       clock.Clock
	Audit       *audit.Recorder
	IdleTimeout time.Duration                                 // ≤ 0 → DefaultIdleTimeout
	KeepAlive   func(ctx context.Context, sessionHash string) // counts an open shell as activity; nil → none
	Guard       Guard                                         // nil → never refuses
}

type openRequest struct {
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Container string `json:"container"`
	Cols      uint16 `json:"cols"`
	Rows      uint16 `json:"rows"`
}

// NewFactory returns the "exec.open" factory for the WebSocket mux.
func NewFactory(d Deps) ws.Factory {
	return func(ctx context.Context, c *ws.Conn, ch int, raw json.RawMessage) (ws.Channel, error) {
		return open(ctx, d, c, c.Session(), ch, raw)
	}
}

func open(ctx context.Context, d Deps, sink Sink, sess session.Session, ch int, raw json.RawMessage) (ws.Channel, error) {
	if d.Guard == nil {
		d.Guard = neverBusy{}
	}
	if d.IdleTimeout <= 0 {
		d.IdleTimeout = DefaultIdleTimeout
	}
	var req openRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("exec.open needs {cluster, namespace, pod, container, cols, rows}: %w", apperr.ErrInvalid)
	}
	if req.Cluster == "" || req.Namespace == "" || req.Pod == "" {
		return nil, fmt.Errorf("exec.open needs a cluster, a namespace and a pod: %w", apperr.ErrInvalid)
	}
	if d.Guard.Busy() {
		return nil, fmt.Errorf("The server is busy; try again in a moment.: %w", apperr.ErrBusy) //nolint:staticcheck // shown to the user
	}
	runCtx, cancel := context.WithCancel(ctx)
	s := &shell{d: d, sink: sink, sess: sess, ch: ch, req: req, cancel: cancel, done: make(chan struct{}), resize: make(chan Size, 1)}
	s.idle = d.Clock.AfterFunc(d.IdleTimeout, func() { s.sink.CloseChannel(s.ch, "idle") })
	if req.Cols > 0 && req.Rows > 0 {
		s.resize <- Size{Cols: req.Cols, Rows: req.Rows}
	}
	go s.run(runCtx)
	return s, nil
}

// stdinQueue is the process's stdin: writes never block the connection, reads block the exec stream.
type stdinQueue struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    []byte
	closed bool
}

func newStdinQueue() *stdinQueue {
	q := &stdinQueue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func (q *stdinQueue) write(p []byte) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return io.ErrClosedPipe
	}
	if len(q.buf)+len(p) > maxStdinQueue {
		return fmt.Errorf("the shell is not reading its input: %w", apperr.ErrBusy)
	}
	q.buf = append(q.buf, p...)
	q.cond.Signal()
	return nil
}

func (q *stdinQueue) Read(p []byte) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.buf) == 0 && !q.closed {
		q.cond.Wait()
	}
	if len(q.buf) == 0 {
		return 0, io.EOF
	}
	n := copy(p, q.buf)
	q.buf = q.buf[n:]
	return n, nil
}

func (q *stdinQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.cond.Broadcast()
}

// shell is one browser terminal.
type shell struct {
	d      Deps
	sink   Sink
	sess   session.Session
	ch     int
	req    openRequest
	cancel context.CancelFunc
	done   chan struct{}
	resize chan Size
	idle   clock.Timer

	mu        sync.Mutex
	stdin     *stdinQueue
	sentInput bool
	closeOnce sync.Once
}

// Close stops the shell and waits for it to finish.
func (s *shell) Close() {
	s.closeOnce.Do(func() {
		s.idle.Stop()
		s.cancel()
		s.mu.Lock()
		if s.stdin != nil {
			s.stdin.close()
		}
		s.mu.Unlock()
		<-s.done
	})
}

func (s *shell) touch() { s.idle.Reset(s.d.IdleTimeout) }

type stdinOp struct {
	Data string `json:"data"`
}

type resizeOp struct {
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// Handle processes exec.stdin, exec.resize and exec.close.
func (s *shell) Handle(_ context.Context, op string, raw json.RawMessage) error {
	switch op {
	case "exec.stdin":
		var in stdinOp
		if err := json.Unmarshal(raw, &in); err != nil {
			return fmt.Errorf("exec.stdin needs {data}: %w", apperr.ErrInvalid)
		}
		b, err := base64.StdEncoding.DecodeString(in.Data)
		if err != nil {
			return fmt.Errorf("exec.stdin data must be base64: %w", apperr.ErrInvalid)
		}
		s.mu.Lock()
		q := s.stdin
		s.sentInput = true
		s.mu.Unlock()
		s.touch()
		if q == nil {
			return nil // between attempts: the shell has not started yet
		}
		return q.write(b)
	case "exec.resize":
		var r resizeOp
		if err := json.Unmarshal(raw, &r); err != nil || r.Cols == 0 || r.Rows == 0 {
			return fmt.Errorf("exec.resize needs {cols, rows}: %w", apperr.ErrInvalid)
		}
		select {
		case <-s.resize: // keep only the newest size
		default:
		}
		s.resize <- Size(r)
		return nil
	case "exec.close":
		go s.sink.CloseChannel(s.ch, "closed")
		return nil
	}
	return fmt.Errorf("unknown op %q for a shell: %w", op, apperr.ErrInvalid)
}

// out writes process output as frames of at most 32 KiB; the first one announces the shell.
type out struct {
	s    *shell
	cmd  []string
	once sync.Once
}

func (o *out) Write(p []byte) (int, error) {
	o.once.Do(func() { _ = o.s.sink.Send(o.s.ch, "opened", map[string]string{"shell": o.cmd[0]}) })
	o.s.touch()
	written := 0
	for len(p) > 0 {
		n := min(len(p), maxOutFrame)
		if err := o.s.sink.Send(o.s.ch, "out", map[string]string{"data": base64.StdEncoding.EncodeToString(p[:n])}); err != nil {
			return written, err
		}
		written += n
		p = p[n:]
	}
	return written, nil
}

func (s *shell) sent() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sentInput
}

func (s *shell) attempt(ctx context.Context, cmd []string) error {
	ev := audit.Event{Cluster: s.req.Cluster, Namespace: s.req.Namespace, ResourceKind: "Pod", ResourceName: s.req.Pod}
	ev.Verb, ev.Outcome = audit.VerbExecStart, "success"
	ev.Details = map[string]any{"container": s.req.Container, "shell": cmd[0]}
	if err := s.d.Audit.Record(ctx, ev); err != nil {
		return apperr.ErrNotRecorded
	}
	q := newStdinQueue()
	s.mu.Lock()
	s.stdin = q
	s.mu.Unlock()
	defer q.close()
	started := s.d.Clock.Now()
	err := s.d.Executor.Stream(ctx, s.sess, s.req.Cluster,
		ExecRequest{Namespace: s.req.Namespace, Pod: s.req.Pod, Container: s.req.Container, Command: cmd, TTY: true, Stdin: true},
		StreamIO{Stdin: q, Stdout: &out{s: s, cmd: cmd}, Resize: s.resize})
	stop := ev
	stop.Verb, stop.Outcome = audit.VerbExecStop, "success"
	stop.Details = map[string]any{
		"container": s.req.Container, "shell": cmd[0], "durationSeconds": int64(s.d.Clock.Now().Sub(started) / time.Second), "exitCode": exitCode(err),
	}
	if err != nil && !notStarted(err) {
		stop.Outcome = "failure"
	}
	_ = s.d.Audit.Record(context.WithoutCancel(ctx), stop)
	return err
}

func exitCode(err error) int {
	var exit *ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit):
		return exit.Code
	}
	return -1
}

func (s *shell) run(ctx context.Context) {
	keep := s.d.Clock.NewTicker(keepAliveEach)
	stopKeep := make(chan struct{})
	go func() {
		for {
			select {
			case <-keep.C():
				if s.d.KeepAlive != nil {
					s.d.KeepAlive(ctx, s.sess.Hash)
				}
			case <-stopKeep:
				return
			}
		}
	}()
	_, err := startShell(shellCandidates, s.sent, func(cmd []string) error { return s.attempt(ctx, cmd) })
	keep.Stop()
	close(stopKeep)
	reason := s.finish(ctx, err)
	close(s.done)
	if reason != "" {
		// After done is closed: closing the channel calls Close, which waits for done.
		s.sink.CloseChannel(s.ch, reason)
	}
}

// finish tells the browser how the shell ended and returns the reason to close the channel with ("" when the
// channel was closed from outside).
func (s *shell) finish(ctx context.Context, err error) string {
	var exit *ExitError
	switch {
	case ctx.Err() != nil:
		return ""
	case err == nil:
		_ = s.sink.Send(s.ch, "exit", map[string]any{"code": 0})
		return "exited"
	case errors.As(err, &exit):
		_ = s.sink.Send(s.ch, "exit", map[string]any{"code": exit.Code, "message": exit.Message})
		return "exited"
	case errors.Is(err, ErrNoShell):
		_ = s.sink.Send(s.ch, "error", map[string]string{"code": "no_shell", "message": ErrNoShell.Error()})
		return "no_shell"
	case errors.Is(err, apperr.ErrNotRecorded):
		_ = s.sink.Send(s.ch, "error", map[string]string{"code": "internal", "message": apperr.ErrNotRecorded.Message})
		return "refused"
	}
	_ = s.sink.Send(s.ch, "error", map[string]string{"code": "internal", "message": "The shell ended unexpectedly."})
	return "error"
}
