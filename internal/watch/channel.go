package watch

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/ws"
)

// snapshotChunk is how many objects one snapshot frame carries.
const snapshotChunk = 500

// openRequest is the "d" of a watch.open op.
type openRequest struct {
	Cluster   string `json:"cluster"`
	Group     string `json:"group"`
	Version   string `json:"version"`
	Resource  string `json:"resource"`
	Namespace string `json:"namespace"`
}

// Factory returns the "watch.open" factory for the WebSocket mux.
func (h *Hub) Factory() ws.Factory {
	return func(ctx context.Context, c *ws.Conn, ch int, d json.RawMessage) (ws.Channel, error) {
		var req openRequest
		if err := json.Unmarshal(d, &req); err != nil {
			return nil, fmt.Errorf("watch.open needs {cluster, group, version, resource, namespace}: %w", apperr.ErrInvalid)
		}
		if req.Group == "core" {
			req.Group = ""
		}
		if req.Cluster == "" || req.Version == "" || req.Resource == "" {
			return nil, fmt.Errorf("watch.open needs a cluster, a version and a resource: %w", apperr.ErrInvalid)
		}
		sess := c.Session()
		key := Key{
			SessionHash: sess.Hash, UserID: sess.UserID, Cluster: req.Cluster, Namespace: req.Namespace,
			GVR: schema.GroupVersionResource{Group: req.Group, Version: req.Version, Resource: req.Resource},
		}
		wc := &channel{conn: c, ch: ch}
		unsub, err := h.Subscribe(ctx, sess, key, wc)
		if err != nil {
			return nil, err
		}
		wc.unsub = unsub
		wc.cancelVis = c.OnVisibility(func(bool) { h.ActivityChanged(key) })
		return wc, nil
	}
}

// channel connects one WebSocket channel to a hub subscription.
type channel struct {
	conn      *ws.Conn
	ch        int
	unsub     func()
	cancelVis func()
}

func objects(items []*unstructured.Unstructured) []map[string]any {
	out := make([]map[string]any, len(items))
	for i, o := range items {
		out[i] = o.Object
	}
	return out
}

// Snapshot sends the list in frames of 500 objects; all but the last carry more=true.
func (c *channel) Snapshot(items []*unstructured.Unstructured, rv string) {
	if len(items) == 0 {
		_ = c.conn.Send(c.ch, "snapshot", map[string]any{"items": []any{}, "resourceVersion": rv, "more": false})
		return
	}
	for start := 0; start < len(items); start += snapshotChunk {
		end := min(start+snapshotChunk, len(items))
		err := c.conn.Send(c.ch, "snapshot", map[string]any{
			"items": objects(items[start:end]), "resourceVersion": rv, "more": end < len(items),
		})
		if err != nil {
			return
		}
	}
}

// Event sends one change. Events are never dropped: a missed one would leave the list wrong, so a
// browser that cannot keep up loses the channel and reloads instead.
func (c *channel) Event(typ string, obj *unstructured.Unstructured) {
	_ = c.conn.Send(c.ch, "event", map[string]any{"type": typ, "object": obj.Object})
}

// Paused tells the browser that the upstream was closed for idleness.
func (c *channel) Paused() { _ = c.conn.Send(c.ch, "paused", map[string]any{}) }

// Failed reports a permanent failure and closes the channel.
func (c *channel) Failed(code, message string) {
	_ = c.conn.Send(c.ch, "error", map[string]string{"code": code, "message": message})
	c.conn.CloseChannel(c.ch, "error")
}

// Active reports whether the tab is visible.
func (c *channel) Active() bool { return !c.conn.Hidden() }

// Handle refuses every op: a watch channel is driven by the server.
func (c *channel) Handle(_ context.Context, op string, _ json.RawMessage) error {
	return fmt.Errorf("a watch channel takes no op %q: %w", op, apperr.ErrInvalid)
}

// Close detaches from the hub.
func (c *channel) Close() {
	if c.unsub != nil {
		c.unsub()
	}
	if c.cancelVis != nil {
		c.cancelVis()
	}
}
