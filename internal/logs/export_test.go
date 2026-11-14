package logs

import (
	"context"
	"encoding/json"

	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/ws"
)

// Open runs the open path with a Sink instead of a connection.
func Open(ctx context.Context, d Deps, sink Sink, sess session.Session, ch int, raw json.RawMessage) (ws.Channel, error) {
	return open(ctx, d, sink, sess, ch, raw)
}
