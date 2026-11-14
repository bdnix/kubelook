// Package ws carries one WebSocket per browser tab with tagged channels, a heartbeat and bounded
// queues. Frames are JSON text. Channel 0 is control; any other channel is opened by an op that
// ends in ".open", chosen by the client.
package ws

import (
	"encoding/json"
	"errors"

	"github.com/bdnix/kubelook/internal/apperr"
)

// Close codes (RFC 6455 and application range).
const (
	CloseGoingAway     = 1001
	CloseProtocolError = 1008
	CloseTooBig        = 1009
	CloseSessionEnded  = 4401
	CloseHeartbeat     = 4408
)

// Limits.
const (
	MaxChannels   = 100
	MaxFrameBytes = 1 << 20
	QueueCapacity = 1024
	MaxChannelID  = 1<<31 - 1
)

// Frame types the server sends.
const (
	TypeOpened = "opened"
	TypeClosed = "closed"
	TypeError  = "error"
	TypePing   = "ping"
)

// In is a client → server frame.
type In struct {
	Op string          `json:"op"`
	Ch int             `json:"ch"`
	D  json.RawMessage `json:"d,omitempty"`
}

// Out is a server → client frame.
type Out struct {
	Ch int    `json:"ch"`
	T  string `json:"t"`
	D  any    `json:"d,omitempty"`
}

// ErrSlowConsumer is returned by Send when the outbound queue is full of messages that must not be dropped.
var ErrSlowConsumer = errors.New("ws: the browser is not reading fast enough")

// ErrClosed is returned by Send once the connection is closed.
var ErrClosed = errors.New("ws: connection closed")

// ErrorBody turns an error into the body of an error frame: the machine code and the user-facing text.
func ErrorBody(err error) map[string]string {
	var api *apperr.APIError
	if errors.As(err, &api) {
		return map[string]string{"code": api.Code, "message": api.Message}
	}
	table := []struct {
		target error
		code   string
	}{
		{apperr.ErrClusterUnavailable, "cluster_unavailable"},
		{apperr.ErrNotFound, "not_found"},
		{apperr.ErrInvalid, "bad_request"},
		{apperr.ErrConflict, "conflict"},
		{apperr.ErrForbidden, "forbidden"},
		{apperr.ErrUnauthenticated, "unauthenticated"},
		{apperr.ErrBusy, "server_busy"},
		{apperr.ErrTooLarge, "too_large"},
	}
	for _, e := range table {
		if errors.Is(err, e.target) {
			return map[string]string{"code": e.code, "message": err.Error()}
		}
	}
	return map[string]string{"code": "internal", "message": "internal error"}
}
