// Package execs runs commands in containers over the exec subresource: browser shells, the debug toolkit
// and the file browser. Everything goes through the Executor interface, so tests need no cluster.
package execs

import (
	"context"
	"errors"
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"

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

type kubeExecutor struct{ clients kube.Clients }

// NewKubeExecutor returns the executor that talks to the cluster over WebSocket exec (v5 channel protocol).
func NewKubeExecutor(c kube.Clients) Executor { return &kubeExecutor{clients: c} }

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
	exec, err := remotecommand.NewWebSocketExecutor(cfg, "GET", req.URL().String())
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
	var code utilexec.CodeExitError
	if errors.As(err, &code) {
		return &ExitError{Code: code.Code, Message: err.Error()}
	}
	return err
}
