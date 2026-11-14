package execs_test

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"

	"github.com/bdnix/kubelook/internal/execs"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/testutil"
	"github.com/bdnix/kubelook/internal/testutil/fakekube"
)

var sess = session.Session{Hash: "sess-hash", UserID: "u"}

// syncBuf is a writer that signals every write. It does not embed a bytes.Buffer: io.Copy would use the
// promoted ReadFrom and bypass Write.
type syncBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
	ch  chan struct{}
}

func (b *syncBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	n, err := b.buf.Write(p)
	b.mu.Unlock()
	select {
	case b.ch <- struct{}{}:
	default:
	}
	return n, err
}

func (b *syncBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func kubeExec(t *testing.T, srv *fakekube.ExecServer) execs.Executor {
	t.Helper()
	fk := fakekube.New()
	fk.RESTConfig = &rest.Config{Host: srv.URL}
	return execs.NewKubeExecutor(fk)
}

func TestKubeExecutor_BuildsTheExecURLEchoesAndEnds(t *testing.T) {
	srv := fakekube.NewExecServer()
	t.Cleanup(srv.Close)
	ex := kubeExec(t, srv)
	stdinR, stdinW := newPipe(t)
	out := &syncBuf{ch: make(chan struct{}, 16)}
	resize := make(chan execs.Size, 1)
	resize <- execs.Size{Cols: 120, Rows: 40}
	done := make(chan error, 1)
	go func() {
		done <- ex.Stream(t.Context(), sess, "c", execs.ExecRequest{
			Namespace: "ns", Pod: "web-1", Container: "app", Command: []string{"/bin/bash"}, TTY: true, Stdin: true,
		}, execs.StreamIO{Stdin: stdinR, Stdout: out, Resize: resize})
	}()
	_, err := stdinW.Write([]byte("hello\n"))
	require.NoError(t, err)
	testutil.Recv(t, out.ch)
	_, err = stdinW.Write([]byte("exit\n"))
	require.NoError(t, err)
	require.NoError(t, testutil.Recv(t, done))

	assert.Equal(t, []string{"/api/v1/namespaces/ns/pods/web-1/exec"}, srv.Paths())
	q := srv.Queries()[0]
	assert.Equal(t, "app", q.Get("container"))
	assert.Equal(t, []string{"/bin/bash"}, q["command"])
	assert.Equal(t, "true", q.Get("stdin"))
	assert.Equal(t, "true", q.Get("stdout"))
	assert.Equal(t, "true", q.Get("tty"))
	assert.Contains(t, out.String(), "hello")
	assert.Contains(t, srv.Resizes()[0], `"Width":120`)
}

func TestKubeExecutor_NonZeroExitIsAnExitError(t *testing.T) {
	srv := fakekube.NewExecServer()
	t.Cleanup(srv.Close)
	srv.ExitCode = 3
	ex := kubeExec(t, srv)
	stdinR, stdinW := newPipe(t)
	go func() { _, _ = stdinW.Write([]byte("exit\n")) }()
	err := ex.Stream(t.Context(), sess, "c", execs.ExecRequest{Namespace: "ns", Pod: "p", Command: []string{"sh"}, Stdin: true, TTY: true},
		execs.StreamIO{Stdin: stdinR, Stdout: &bytes.Buffer{}})
	var exit *execs.ExitError
	require.ErrorAs(t, err, &exit)
	assert.Equal(t, 3, exit.Code)
}

func TestKubeExecutor_StartFailureIsAnError(t *testing.T) {
	srv := fakekube.NewExecServer()
	t.Cleanup(srv.Close)
	srv.FailStart = `exec: "/bin/bash": stat /bin/bash: no such file or directory`
	err := kubeExec(t, srv).Stream(t.Context(), sess, "c", execs.ExecRequest{Namespace: "ns", Pod: "p", Command: []string{"/bin/bash"}, Stdin: true, TTY: true},
		execs.StreamIO{Stdin: &bytes.Buffer{}, Stdout: &bytes.Buffer{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no such file or directory")
}

func TestKubeExecutor_CancelEndsTheStream(t *testing.T) {
	srv := fakekube.NewExecServer()
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithCancel(t.Context())
	stdinR, _ := newPipe(t)
	done := make(chan error, 1)
	go func() {
		done <- kubeExec(t, srv).Stream(ctx, sess, "c", execs.ExecRequest{Namespace: "ns", Pod: "p", Command: []string{"sh"}, Stdin: true, TTY: true},
			execs.StreamIO{Stdin: stdinR, Stdout: &bytes.Buffer{}})
	}()
	require.Eventually(t, func() bool { return len(srv.Paths()) == 1 }, 5*time.Second, 10*time.Millisecond)
	cancel()
	assert.Error(t, testutil.Recv(t, done))
}

func TestKubeExecutor_UnknownCluster(t *testing.T) {
	fk := fakekube.New()
	fk.Known = map[string]bool{}
	err := execs.NewKubeExecutor(fk).Stream(t.Context(), sess, "nope", execs.ExecRequest{Namespace: "n", Pod: "p"}, execs.StreamIO{})
	require.Error(t, err)
}
