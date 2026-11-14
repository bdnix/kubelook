package ws

import (
	"context"
	"encoding/json"
)

// DetachedConn is a Conn without a socket, for testing the outbound queue.
func DetachedConn(capacity int) *Conn {
	ctx, stop := context.WithCancel(context.Background())
	return &Conn{ctx: ctx, stop: stop, q: newQueue(capacity), channels: map[int]Channel{}, opening: map[int][]item{}, visSubs: map[int]func(bool){}}
}

// AddChannel registers a channel on a detached connection.
func (c *Conn) AddChannel(ch int, chn Channel) {
	c.mu.Lock()
	c.channels[ch] = chn
	c.mu.Unlock()
}

// Pending returns the queued frames, oldest first, as decoded objects.
func (c *Conn) Pending() []map[string]any {
	var out []map[string]any
	c.q.mu.Lock()
	defer c.q.mu.Unlock()
	for e := c.q.items.Front(); e != nil; e = e.Next() {
		var m map[string]any
		_ = json.Unmarshal(e.Value.(item).data, &m)
		out = append(out, m)
	}
	return out
}
