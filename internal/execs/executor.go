// Package execs runs commands in containers over the exec subresource: browser shells, the debug toolkit
// and the file browser. Everything goes through the Executor interface, so tests need no cluster.
package execs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"

	"github.com/bdnix/kubelook/internal/egress"
	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/session"
)

// ExecRequest says what to run where.
type ExecRequest struct {
	Namespace, Pod, Container string
	Command                   []string
	TTY, Stdin                bool
}

// Size is a terminal size.
type Size struct{ Cols, Rows uint16 }

// StreamIO connects the process to the caller. Stderr is used only without a TTY.
type StreamIO struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	Resize         <-chan Size
}

// ExitError is returned by Stream when the command ended with a non-zero exit code.
type ExitError struct {
	Code    int
	Message string
}

func (e *ExitError) Error() string { return e.Message }

// Executor runs one command and returns when it has ended.
type Executor interface {
	Stream(ctx context.Context, sess session.Session, clusterID string, r ExecRequest, sio StreamIO) error
}

type kubeExecutor struct {
	clients kube.Clients
	tunnel  *egress.Proxy
}

// NewKubeExecutor returns the executor that talks to the cluster over WebSocket exec (v5 channel protocol). It
// refuses to exec through a cluster that is under the egress guard: use NewGuardedKubeExecutor for that.
func NewKubeExecutor(c kube.Clients) Executor { return &kubeExecutor{clients: c} }

// NewGuardedKubeExecutor is NewKubeExecutor for a server whose kubeconfig clusters are under the egress guard.
// client-go's WebSocket exec does not dial through rest.Config.Dial, so the connections of such a cluster go
// through the tunnel, which dials through the guard.
func NewGuardedKubeExecutor(c kube.Clients, tunnel *egress.Proxy) Executor {
	return &kubeExecutor{clients: c, tunnel: tunnel}
}

// errNoTunnel is the answer for a guarded cluster when the executor has no tunnel: failing closed beats
// connecting unguarded.
var errNoTunnel = errors.New("this cluster is under the egress guard and the executor has no guarded tunnel")

type sizeQueue struct {
	ctx context.Context
	ch  <-chan Size
}

// Next implements remotecommand.TerminalSizeQueue; nil ends the queue.
func (q sizeQueue) Next() *remotecommand.TerminalSize {
	select {
	case s, ok := <-q.ch:
		if !ok {
			return nil
		}
		return &remotecommand.TerminalSize{Width: s.Cols, Height: s.Rows}
	case <-q.ctx.Done():
		return nil
	}
}

func (k *kubeExecutor) Stream(ctx context.Context, sess session.Session, clusterID string, r ExecRequest, sio StreamIO) error {
	cfg, err := k.clients.REST(ctx, sess, clusterID)
	if err != nil {
		return err
	}
	typed, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("building the client: %w", err)
	}
	req := typed.CoreV1().RESTClient().Post().Resource("pods").Namespace(r.Namespace).Name(r.Pod).SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: r.Container, Command: r.Command, Stdin: r.Stdin, Stdout: true, Stderr: !r.TTY, TTY: r.TTY,
		}, scheme.ParameterCodec)
	wsCfg := cfg
	if cfg.Dial != nil { // the egress guard is on for this cluster
		if k.tunnel == nil {
			return errNoTunnel
		}
		wsCfg = rest.CopyConfig(cfg)
		wsCfg.Dial = nil // the tunnel dials; the proxy must be reachable from here
		wsCfg.Proxy = k.tunnel.ProxyFunc()
	}
	exec, err := remotecommand.NewWebSocketExecutor(wsCfg, "GET", req.URL().String())
	if err != nil {
		return fmt.Errorf("preparing the exec request: %w", err)
	}
	opts := remotecommand.StreamOptions{Stdout: sio.Stdout, Tty: r.TTY}
	if r.Stdin {
		opts.Stdin = sio.Stdin
	}
	if !r.TTY {
		opts.Stderr = sio.Stderr
	}
	if r.TTY && sio.Resize != nil {
		opts.TerminalSizeQueue = sizeQueue{ctx: ctx, ch: sio.Resize}
	}
	err = exec.StreamWithContext(ctx, opts)
	if err != nil && strings.Contains(err.Error(), egress.RefusalReason) {
		return egress.ErrBlocked
	}
	var code utilexec.CodeExitError
	if errors.As(err, &code) {
		return &ExitError{Code: code.Code, Message: err.Error()}
	}
	return err
}
