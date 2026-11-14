package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/ws"
)

// DefaultMaxLinesPerSecond is the per-stream rate cap when the configuration sets none.
const DefaultMaxLinesPerSecond = 1000

var errNoOps = fmt.Errorf("a log channel takes no ops: %w", apperr.ErrInvalid)

var errAuditFailed = &apperr.APIError{Status: http.StatusServiceUnavailable, Code: "server_busy",
	Message: "Logs can't be opened right now because the audit record could not be written."}

// Deps are the dependencies of the log channel.
type Deps struct {
	Source            Source
	Clock             clock.Clock
	Audit             *audit.Recorder
	KeepAlive         func(ctx context.Context, sessionHash string) // counts an open stream as activity; nil → none
	MaxLinesPerSecond int                                           // ≤ 0 → 1000
}

// openRequest is the "d" of a logs.open op.
type openRequest struct {
	Cluster      string `json:"cluster"`
	Namespace    string `json:"namespace"`
	Pod          string `json:"pod"`
	Container    string `json:"container"`
	Follow       bool   `json:"follow"`
	TailLines    *int64 `json:"tailLines"`
	SinceSeconds *int64 `json:"sinceSeconds"`
	SinceTime    string `json:"sinceTime"`
	Previous     bool   `json:"previous"`
}

// options builds the log request. The stream always asks for timestamps.
func (r openRequest) options() (*corev1.PodLogOptions, error) {
	o := &corev1.PodLogOptions{
		Container: r.Container, Follow: r.Follow, Timestamps: true, Previous: r.Previous,
		TailLines: r.TailLines, SinceSeconds: r.SinceSeconds,
	}
	if (r.TailLines != nil && *r.TailLines < 0) || (r.SinceSeconds != nil && *r.SinceSeconds <= 0) {
		return nil, fmt.Errorf("tailLines and sinceSeconds must be positive: %w", apperr.ErrInvalid)
	}
	if r.SinceTime != "" {
		t, err := time.Parse(time.RFC3339, r.SinceTime)
		if err != nil {
			return nil, fmt.Errorf("sinceTime must be an RFC 3339 time: %w", apperr.ErrInvalid)
		}
		mt := metav1.NewTime(t)
		o.SinceTime = &mt
	}
	return o, nil
}

// NewFactory returns the "logs.open" factory for the WebSocket mux.
func NewFactory(d Deps) ws.Factory {
	return func(ctx context.Context, c *ws.Conn, ch int, raw json.RawMessage) (ws.Channel, error) {
		return open(ctx, d, c, c.Session(), ch, raw)
	}
}

func open(ctx context.Context, d Deps, sink Sink, sess session.Session, ch int, raw json.RawMessage) (ws.Channel, error) {
	var req openRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("logs.open needs {cluster, namespace, pod, container, ...}: %w", apperr.ErrInvalid)
	}
	if req.Cluster == "" || req.Namespace == "" || req.Pod == "" {
		return nil, fmt.Errorf("logs.open needs a cluster, a namespace and a pod: %w", apperr.ErrInvalid)
	}
	opts, err := req.options()
	if err != nil {
		return nil, err
	}
	event := audit.Event{
		Cluster: req.Cluster, Namespace: req.Namespace, ResourceKind: "Pod", ResourceName: req.Pod, Outcome: "success",
		Details: details(req),
	}
	event.Verb = audit.VerbLogViewOpen
	if err := d.Audit.Record(ctx, event); err != nil {
		return nil, errAuditFailed // no record, no stream
	}
	rc, err := d.Source.Stream(ctx, sess, req.Cluster, req.Namespace, req.Pod, opts)
	if err != nil {
		closeEvent := event
		closeEvent.Verb, closeEvent.Outcome = audit.VerbLogViewClose, "failure"
		_ = d.Audit.Record(ctx, closeEvent)
		return nil, err
	}
	sd := streamDeps{
		clock: d.Clock, maxPerSec: d.MaxLinesPerSecond,
		onEnd: func(ctx context.Context) {
			closeEvent := event
			closeEvent.Verb = audit.VerbLogViewClose
			_ = d.Audit.Record(context.WithoutCancel(ctx), closeEvent)
		},
	}
	if sd.maxPerSec <= 0 {
		sd.maxPerSec = DefaultMaxLinesPerSecond
	}
	if d.KeepAlive != nil {
		sd.keepAlive = func(ctx context.Context) { d.KeepAlive(ctx, sess.Hash) }
	}
	return start(ctx, sd, sink, ch, req.Follow, rc), nil
}

// details are what the audit record says about the stream: where and how much, never what.
func details(r openRequest) map[string]any {
	m := map[string]any{"container": r.Container, "follow": r.Follow, "previous": r.Previous}
	if r.TailLines != nil {
		m["tailLines"] = *r.TailLines
	}
	if r.SinceSeconds != nil {
		m["sinceSeconds"] = *r.SinceSeconds
	}
	if r.SinceTime != "" {
		m["sinceTime"] = r.SinceTime
	}
	return m
}
