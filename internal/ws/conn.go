package ws

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/coder/websocket"

	"github.com/bdnix/kubelook/internal/session"
)

// Channel is one logical stream of a connection.
type Channel interface {
	// Handle processes a client op addressed to the channel. An error becomes an error frame; the channel stays open.
	Handle(ctx context.Context, op string, d json.RawMessage) error
	// Close releases the channel. It is called exactly once.
	Close()
}

// Conn is one browser tab's connection.
type Conn struct {
	mux  *Mux
	ws   *websocket.Conn
	sess session.Session
	ctx  context.Context
	stop context.CancelFunc
	q    *queue

	hidden atomic.Bool

	mu        sync.Mutex
	channels  map[int]Channel
	opening   map[int][]item // frames sent by a factory before its channel was announced
	visSubs   map[int]func(bool)
	nextSub   int
	closeOnce sync.Once
	lastPong  atomic.Int64 // unix nanoseconds of the last pong (or of the connection)
}

// Session returns the signed-in session of the connection.
func (c *Conn) Session() session.Session { return c.sess }

// Hidden reports whether the tab said it is not visible.
func (c *Conn) Hidden() bool { return c.hidden.Load() }

// OnVisibility registers fn for visibility changes and returns a function that unregisters it.
func (c *Conn) OnVisibility(fn func(hidden bool)) (cancel func()) {
	c.mu.Lock()
	id := c.nextSub
	c.nextSub++
	c.visSubs[id] = fn
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		delete(c.visSubs, id)
		c.mu.Unlock()
	}
}

func (c *Conn) setHidden(h bool) {
	c.hidden.Store(h)
	c.mu.Lock()
	subs := make([]func(bool), 0, len(c.visSubs))
	for _, f := range c.visSubs {
		subs = append(subs, f)
	}
	c.mu.Unlock()
	for _, f := range subs {
		f(h)
	}
}

func encode(ch int, typ string, d any) (item, error) {
	b, err := json.Marshal(Out{Ch: ch, T: typ, D: d})
	if err != nil {
		return item{}, err
	}
	return item{ch: ch, data: b}, nil
}

// Send queues a frame that must be delivered. It never blocks. With a full queue and nothing
// droppable it returns ErrSlowConsumer and the channel is closed with the reason "slow_consumer".
func (c *Conn) Send(ch int, typ string, d any) error {
	it, err := encode(ch, typ, d)
	if err != nil {
		return err
	}
	return c.enqueue(it, func(it item) error { return c.q.push(it) })
}

// SendDroppable queues a frame that may be replaced under pressure (log lines, list events).
func (c *Conn) SendDroppable(ch int, typ string, d any) {
	it, err := encode(ch, typ, d)
	if err != nil {
		return
	}
	it.droppable = true
	_ = c.enqueue(it, func(it item) error { c.q.pushDroppable(it); return nil })
}

func (c *Conn) enqueue(it item, push func(item) error) error {
	c.mu.Lock()
	if held, opening := c.opening[it.ch]; opening {
		c.opening[it.ch] = append(held, it)
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()
	err := push(it)
	if errors.Is(err, ErrSlowConsumer) {
		c.CloseChannel(it.ch, "slow_consumer")
	}
	return err
}

// Dropped returns how many droppable frames of the channel were discarded.
func (c *Conn) Dropped(ch int) int { return c.q.dropped(ch) }

// CloseChannel ends a channel from the server side and tells the browser why.
func (c *Conn) CloseChannel(ch int, reason string) {
	if c.removeChannel(ch) {
		if it, err := encode(ch, TypeClosed, map[string]string{"reason": reason}); err == nil {
			c.q.pushForce(it)
		}
	}
}

// removeChannel detaches the channel and closes it once. It reports whether there was one.
func (c *Conn) removeChannel(ch int) bool {
	c.mu.Lock()
	chn, ok := c.channels[ch]
	delete(c.channels, ch)
	c.mu.Unlock()
	if ok {
		chn.Close()
	}
	return ok
}

func (c *Conn) sendError(ch int, err error) {
	if it, e := encode(ch, TypeError, ErrorBody(err)); e == nil {
		c.q.pushForce(it)
	}
}

// close ends the connection once: it stops the writer, closes the socket with the code and closes every channel.
func (c *Conn) close(code websocket.StatusCode, reason string) {
	c.closeOnce.Do(func() {
		c.q.close()
		_ = c.ws.Close(code, reason) // the close handshake needs the reader, so cancel only afterwards
		c.stop()
		c.mu.Lock()
		chans := c.channels
		c.channels = map[int]Channel{}
		c.mu.Unlock()
		for _, ch := range chans {
			ch.Close()
		}
	})
}

// writer is the only goroutine that writes to the socket.
func (c *Conn) writer() {
	for {
		it, ok := c.q.next(c.ctx)
		if !ok {
			return
		}
		if err := c.ws.Write(c.ctx, websocket.MessageText, it.data); err != nil {
			c.close(websocket.StatusGoingAway, "write failed")
			return
		}
	}
}
