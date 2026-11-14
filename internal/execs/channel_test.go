package execs_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/execs"
	"github.com/bdnix/kubelook/internal/store/storetest"
	"github.com/bdnix/kubelook/internal/testutil"
	"github.com/bdnix/kubelook/internal/ws"
)

var t0 = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

type frame struct {
	Type string
	D    map[string]any
}

type fakeSink struct {
	frames chan frame
	closed chan string
}

func (f *fakeSink) Send(_ int, typ string, d any) error {
	b, _ := json.Marshal(d)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	f.frames <- frame{typ, m}
	return nil
}

func (f *fakeSink) CloseChannel(_ int, reason string) { f.closed <- reason }

// script is what a fake process does.
type script func(ctx context.Context, r execs.ExecRequest, sio execs.StreamIO) error

type fakeExec struct {
	mu       sync.Mutex
	scripts  map[string]script
	requests []execs.ExecRequest
}

func (f *fakeExec) Stream(ctx context.Context, _ sessionT, _ string, r execs.ExecRequest, sio execs.StreamIO) error {
	f.mu.Lock()
	f.requests = append(f.requests, r)
	s := f.scripts[r.Command[0]]
	f.mu.Unlock()
	if s == nil {
		return errors.New(`exec: "` + r.Command[0] + `": executable file not found in $PATH`)
	}
	return s(ctx, r, sio)
}

func (f *fakeExec) commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, r := range f.requests {
		out = append(out, r.Command[0])
	}
	return out
}

// echoUntilEOF writes a prompt and then everything it reads, until stdin ends or the context is cancelled.
func echoUntilEOF(exit error) script {
	return func(ctx context.Context, _ execs.ExecRequest, sio execs.StreamIO) error {
		_, _ = sio.Stdout.Write([]byte("$ "))
		buf := make([]byte, 4096)
		for {
			n, err := sio.Stdin.Read(buf)
			if n > 0 {
				_, _ = sio.Stdout.Write(buf[:n])
				if bytes.Contains(buf[:n], []byte("exit")) {
					return exit
				}
			}
			if err != nil || ctx.Err() != nil {
				return ctx.Err()
			}
		}
	}
}

type env struct {
	t     *testing.T
	exec  *fakeExec
	sink  *fakeSink
	clock *clock.Fake
	out   *bytes.Buffer
	deps  execs.Deps
	keeps chan string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{t: t, exec: &fakeExec{scripts: map[string]script{"/bin/bash": echoUntilEOF(nil)}}, clock: clock.NewFake(t0), out: &bytes.Buffer{}, keeps: make(chan string, 8),
		sink: &fakeSink{frames: make(chan frame, 256), closed: make(chan string, 4)}}
	db := storetest.New(t)
	n := 0
	rec := audit.NewRecorder(db, e.clock, e.out, func() string { n++; return fmt.Sprintf("a%d", n) })
	e.deps = execs.Deps{Executor: e.exec, Clock: e.clock, Audit: rec, IdleTimeout: 10 * time.Minute,
		KeepAlive: func(_ context.Context, h string) {
			select {
			case e.keeps <- h:
			default:
			}
		}}
	return e
}

func (e *env) open(cols, rows int) (ws.Channel, error) {
	raw, _ := json.Marshal(map[string]any{"cluster": "c", "namespace": "ns", "pod": "web-1", "container": "app", "cols": cols, "rows": rows})
	ch, err := execs.Open(e.t.Context(), e.deps, e.sink, sess, 1, raw)
	if ch != nil {
		e.t.Cleanup(ch.Close)
	}
	return ch, err
}

func (e *env) next() frame { return testutil.Recv(e.t, e.sink.frames) }

func stdin(s string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"data": base64.StdEncoding.EncodeToString([]byte(s))})
	return b
}

func decode(t *testing.T, f frame) string {
	t.Helper()
	require.Equal(t, "out", f.Type)
	b, err := base64.StdEncoding.DecodeString(f.D["data"].(string))
	require.NoError(t, err)
	return string(b)
}

func (e *env) auditVerbs() []string {
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(e.out.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		_ = json.Unmarshal([]byte(l), &m)
		out = append(out, m["verb"].(string))
	}
	return out
}

func TestShell_BashStartsOpenedThenOutput_StdinReachesTheProcess_ExitEndsTheChannel(t *testing.T) {
	e := newEnv(t)
	e.exec.scripts["/bin/bash"] = echoUntilEOF(&execs.ExitError{Code: 3, Message: "command terminated with exit code 3"})
	ch, err := e.open(100, 30)
	require.NoError(t, err)
	f := e.next()
	assert.Equal(t, "opened", f.Type)
	assert.Equal(t, "/bin/bash", f.D["shell"])
	assert.Equal(t, "$ ", decode(t, e.next()))
	require.NoError(t, ch.Handle(t.Context(), "exec.stdin", stdin("ls\n")))
	assert.Equal(t, "ls\n", decode(t, e.next()))
	require.NoError(t, ch.Handle(t.Context(), "exec.stdin", stdin("exit\n")))
	assert.Equal(t, "exit\n", decode(t, e.next()))
	exit := e.next()
	assert.Equal(t, "exit", exit.Type)
	assert.EqualValues(t, 3, exit.D["code"])
	assert.Equal(t, "exited", testutil.Recv(t, e.sink.closed))
	ch.Close()
	assert.Equal(t, []string{"exec-start", "exec-stop"}, e.auditVerbs())
	assert.Contains(t, e.out.String(), `"exitCode":3`)
	assert.Contains(t, e.out.String(), `"durationSeconds":0`)
	assert.Contains(t, e.out.String(), `"shell":"/bin/bash"`)
}

func TestShell_OutputComesInFramesOfAtMost32KiB(t *testing.T) {
	e := newEnv(t)
	e.exec.scripts["/bin/bash"] = func(_ context.Context, _ execs.ExecRequest, sio execs.StreamIO) error {
		_, _ = sio.Stdout.Write(bytes.Repeat([]byte("x"), 70000))
		return nil
	}
	_, err := e.open(80, 24)
	require.NoError(t, err)
	assert.Equal(t, "opened", e.next().Type)
	var sizes []int
	for range 3 {
		sizes = append(sizes, len(decode(t, e.next())))
	}
	assert.Equal(t, []int{32768, 32768, 4464}, sizes)
	assert.Equal(t, "exit", e.next().Type)
}

func TestShell_ResizeReachesTheQueueNewestOnly_InitialSizeFirst(t *testing.T) {
	e := newEnv(t)
	got := make(chan execs.Size, 4)
	started := make(chan struct{})
	release := make(chan struct{})
	e.exec.scripts["/bin/bash"] = func(_ context.Context, _ execs.ExecRequest, sio execs.StreamIO) error {
		<-started
		got <- <-sio.Resize
		<-release
		got <- <-sio.Resize
		return nil
	}
	ch, err := e.open(100, 30)
	require.NoError(t, err)
	close(started)
	assert.Equal(t, execs.Size{Cols: 100, Rows: 30}, testutil.Recv(t, got))
	require.NoError(t, ch.Handle(t.Context(), "exec.resize", json.RawMessage(`{"cols":110,"rows":40}`)))
	require.NoError(t, ch.Handle(t.Context(), "exec.resize", json.RawMessage(`{"cols":120,"rows":50}`)))
	close(release)
	assert.Equal(t, execs.Size{Cols: 120, Rows: 50}, testutil.Recv(t, got))
	assert.ErrorIs(t, ch.Handle(t.Context(), "exec.resize", json.RawMessage(`{"cols":0,"rows":5}`)), apperr.ErrInvalid)
}

func TestShell_FallsBackToShWhenBashCannotStart(t *testing.T) {
	for name, bashErr := range map[string]error{
		"not found":     errors.New(`exec: "/bin/bash": executable file not found in $PATH`),
		"no such file":  errors.New("stat /bin/bash: no such file or directory"),
		"exit code 127": &execs.ExitError{Code: 127, Message: "command terminated with exit code 127"},
		"exit code 126": &execs.ExitError{Code: 126, Message: "command terminated with exit code 126"},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.exec.scripts["/bin/bash"] = func(context.Context, execs.ExecRequest, execs.StreamIO) error { return bashErr }
			e.exec.scripts["/bin/sh"] = echoUntilEOF(nil)
			_, err := e.open(80, 24)
			require.NoError(t, err)
			f := e.next()
			assert.Equal(t, "opened", f.Type)
			assert.Equal(t, "/bin/sh", f.D["shell"])
			assert.Equal(t, []string{"/bin/bash", "/bin/sh"}, e.exec.commands())
		})
	}
}

func TestShell_NoShellAtAll(t *testing.T) {
	e := newEnv(t)
	delete(e.exec.scripts, "/bin/bash")
	_, err := e.open(80, 24)
	require.NoError(t, err)
	f := e.next()
	assert.Equal(t, "error", f.Type)
	assert.Equal(t, "no_shell", f.D["code"])
	assert.Equal(t, "This container has no shell (/bin/bash or /bin/sh).", f.D["message"])
	assert.Equal(t, "no_shell", testutil.Recv(t, e.sink.closed))
}

func TestShell_NoFallbackAfterInputWasSent(t *testing.T) {
	e := newEnv(t)
	release := make(chan struct{})
	e.exec.scripts["/bin/bash"] = func(_ context.Context, _ execs.ExecRequest, _ execs.StreamIO) error {
		<-release
		return &execs.ExitError{Code: 127, Message: "command terminated with exit code 127"}
	}
	ch, err := e.open(80, 24)
	require.NoError(t, err)
	require.NoError(t, ch.Handle(t.Context(), "exec.stdin", stdin("x")))
	close(release)
	f := e.next()
	assert.Equal(t, "exit", f.Type)
	assert.EqualValues(t, 127, f.D["code"])
	assert.Equal(t, []string{"/bin/bash"}, e.exec.commands())
}

func TestShell_IdleClosesTheChannelOnTheFakeClock(t *testing.T) {
	e := newEnv(t)
	ch, err := e.open(80, 24)
	require.NoError(t, err)
	assert.Equal(t, "opened", e.next().Type)
	e.clock.Advance(9 * time.Minute)
	select {
	case r := <-e.sink.closed:
		t.Fatalf("closed too early: %s", r)
	default:
	}
	require.NoError(t, ch.Handle(t.Context(), "exec.stdin", stdin("a")))
	e.clock.Advance(9 * time.Minute)
	select {
	case r := <-e.sink.closed:
		t.Fatalf("activity must reset the timer: %s", r)
	default:
	}
	e.clock.Advance(2 * time.Minute)
	assert.Equal(t, "idle", testutil.Recv(t, e.sink.closed))
	ch.Close()
}

func TestShell_AuditBeforeStartRefusesWhenItCannotBeWritten(t *testing.T) {
	e := newEnv(t)
	db := storetest.New(t)
	e.deps.Audit = audit.NewRecorder(db, e.clock, e.out, func() string { return "x" })
	require.NoError(t, db.Close())
	_, err := e.open(80, 24)
	require.NoError(t, err)
	f := e.next()
	assert.Equal(t, "error", f.Type)
	assert.Equal(t, "The action was refused because it could not be recorded.", f.D["message"])
	assert.Empty(t, e.exec.commands())
	assert.Equal(t, "refused", testutil.Recv(t, e.sink.closed))
}

func TestShell_NeverPutsShellBytesIntoAuditOrLogs(t *testing.T) {
	e := newEnv(t)
	e.exec.scripts["/bin/bash"] = echoUntilEOF(nil)
	ch, err := e.open(80, 24)
	require.NoError(t, err)
	e.next()
	e.next()
	require.NoError(t, ch.Handle(t.Context(), "exec.stdin", stdin("MARKER-SECRET-123\n")))
	assert.Contains(t, decode(t, e.next()), "MARKER-SECRET-123")
	ch.Close()
	assert.NotContains(t, e.out.String(), "MARKER")
}

func TestShell_KeepAliveEveryMinute(t *testing.T) {
	e := newEnv(t)
	ch, err := e.open(80, 24)
	require.NoError(t, err)
	e.next()
	e.clock.Advance(time.Minute)
	assert.Equal(t, "sess-hash", testutil.Recv(t, e.keeps))
	e.clock.Advance(time.Minute)
	assert.Equal(t, "sess-hash", testutil.Recv(t, e.keeps))
	ch.Close()
}

type busy struct{}

func (busy) Busy() bool { return true }

func TestShell_BusyServerRefusesNewShells(t *testing.T) {
	e := newEnv(t)
	e.deps.Guard = busy{}
	_, err := e.open(80, 24)
	assert.ErrorIs(t, err, apperr.ErrBusy)
	assert.Empty(t, e.exec.commands())
}

func TestShell_CloseCancelsTheStreamAndIsIdempotent(t *testing.T) {
	e := newEnv(t)
	ctxDone := make(chan struct{})
	e.exec.scripts["/bin/bash"] = func(ctx context.Context, _ execs.ExecRequest, sio execs.StreamIO) error {
		_, _ = sio.Stdout.Write([]byte("hi"))
		<-ctx.Done()
		close(ctxDone)
		return ctx.Err()
	}
	ch, err := e.open(80, 24)
	require.NoError(t, err)
	e.next()
	ch.Close()
	testutil.Recv(t, ctxDone)
	ch.Close()
	select {
	case r := <-e.sink.closed:
		t.Fatalf("a channel closed from outside is not closed again: %s", r)
	default:
	}
}

func TestShell_ExecCloseFromTheBrowser(t *testing.T) {
	e := newEnv(t)
	ch, err := e.open(80, 24)
	require.NoError(t, err)
	e.next()
	require.NoError(t, ch.Handle(t.Context(), "exec.close", nil))
	assert.Equal(t, "closed", testutil.Recv(t, e.sink.closed))
	ch.Close()
}

func TestShell_BadOpsAndOpenRequests(t *testing.T) {
	e := newEnv(t)
	ch, err := e.open(80, 24)
	require.NoError(t, err)
	assert.ErrorIs(t, ch.Handle(t.Context(), "exec.stdin", json.RawMessage(`{`)), apperr.ErrInvalid)
	assert.ErrorIs(t, ch.Handle(t.Context(), "exec.stdin", json.RawMessage(`{"data":"!!"}`)), apperr.ErrInvalid)
	assert.ErrorIs(t, ch.Handle(t.Context(), "exec.nope", nil), apperr.ErrInvalid)
	ch.Close()
	_, err = execs.Open(t.Context(), e.deps, e.sink, sess, 2, json.RawMessage(`"x"`))
	assert.ErrorIs(t, err, apperr.ErrInvalid)
	_, err = execs.Open(t.Context(), e.deps, e.sink, sess, 2, json.RawMessage(`{"cluster":"c"}`))
	assert.ErrorIs(t, err, apperr.ErrInvalid)
}

func TestShell_UnexpectedStreamErrorIsReportedInGeneralWords(t *testing.T) {
	e := newEnv(t)
	e.exec.scripts["/bin/bash"] = func(context.Context, execs.ExecRequest, execs.StreamIO) error {
		return errors.New("connection reset by peer")
	}
	_, err := e.open(80, 24)
	require.NoError(t, err)
	f := e.next()
	assert.Equal(t, "error", f.Type)
	assert.Equal(t, "The shell ended unexpectedly.", f.D["message"])
	assert.Equal(t, "error", testutil.Recv(t, e.sink.closed))
}

func TestStdinQueue_RefusesToBufferWithoutLimit(t *testing.T) {
	e := newEnv(t)
	hold := make(chan struct{})
	e.exec.scripts["/bin/bash"] = func(_ context.Context, _ execs.ExecRequest, sio execs.StreamIO) error {
		_, _ = sio.Stdout.Write([]byte("up"))
		<-hold
		return nil
	}
	ch, err := e.open(80, 24)
	require.NoError(t, err)
	e.next()
	big := strings.Repeat("a", 600<<10)
	require.NoError(t, ch.Handle(t.Context(), "exec.stdin", stdin(big)))
	assert.ErrorIs(t, ch.Handle(t.Context(), "exec.stdin", stdin(big)), apperr.ErrBusy)
	close(hold)
}
