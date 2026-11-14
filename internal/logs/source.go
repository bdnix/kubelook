// Package logs follows pod logs through a WebSocket channel with batching, a rate cap, a bounded
// buffer and audit records. Log text never reaches the audit record or the server's own log.
package logs

import (
	"context"
	"io"

	corev1 "k8s.io/api/core/v1"

	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/session"
)

// Source opens the byte stream of one container's log.
type Source interface {
	Stream(ctx context.Context, sess session.Session, clusterID, ns, pod string, o *corev1.PodLogOptions) (io.ReadCloser, error)
}

type kubeSource struct{ clients kube.Clients }

// NewKubeSource reads logs through the user's typed client.
func NewKubeSource(c kube.Clients) Source { return kubeSource{c} }

func (k kubeSource) Stream(ctx context.Context, sess session.Session, clusterID, ns, pod string, o *corev1.PodLogOptions) (io.ReadCloser, error) {
	typed, err := k.clients.Typed(ctx, sess, clusterID)
	if err != nil {
		return nil, err
	}
	rc, err := typed.CoreV1().Pods(ns).GetLogs(pod, o).Stream(ctx)
	if err != nil {
		return nil, kube.MapError(err)
	}
	return rc, nil
}
