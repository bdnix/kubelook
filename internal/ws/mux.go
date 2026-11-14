package ws

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/session"
)

const (
	pingInterval     = 20 * time.Second
	heartbeatTimeout = 60 * time.Second
)

// Factory opens a channel for an "<x>.open" op. Frames it sends before returning are delivered after "opened".
type Factory func(ctx context.Context, c *Conn, ch int, d json.RawMessage) (Channel, error)

// Deps are the dependencies of Mux.
type Deps struct {
	Clock      clock.Clock
	Log        *slog.Logger
	Sessions   *session.Manager
	PublicHost string                                                  // host[:port] of server.publicURL; the only accepted Origin
	Fail       func(w http.ResponseWriter, r *http.Request, err error) // writes the standard error response
}

// Mux upgrades requests and routes channel ops to factories.
type Mux struct {
	d         Deps
	mu        sync.Mutex
	factories map[string]Factory
	conns     map[*Conn]struct{}
	down      bool
	wg        sync.WaitGroup
}

// NewMux returns a Mux with no factories.
func NewMux(d Deps) *Mux {
	return &Mux{d: d, factories: map[string]Factory{}, conns: map[*Conn]struct{}{}}
}

// Register adds the factory for an op that ends in ".open", e.g. "watch.open".
func (m *Mux) Register(openOp string, f Factory) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.factories[openOp] = f
}

// Shutdown closes every connection with 1001 and waits for them. Later upgrades are refused.
func (m *Mux) Shutdown() {
	m.mu.Lock()
	m.down = true
	conns := make([]*Conn, 0, len(m.conns))
	for c := range m.conns {
		conns = append(conns, c)
	}
	m.mu.Unlock()
	for _, c := range conns {
		c.close(websocket.StatusGoingAway, "server shutting down")
	}
	m.wg.Wait()
}

func (m *Mux) shuttingDown() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.down
}

func (m *Mux) originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // not a browser
	}
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, m.d.PublicHost)
}

// ServeHTTP upgrades the request. The session middleware has already run; browsers cannot set
// headers on WebSocket upgrades, so authentication is the cookie plus the Origin check.
func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sess, ok := session.FromContext(r.Context())
	if !ok {
		m.d.Fail(w, r, apperr.ErrUnauthenticated)
		return
	}
	if !m.originAllowed(r) {
		m.d.Fail(w, r, apperr.ErrForbidden)
		return
	}
	if m.shuttingDown() {
		m.d.Fail(w, r, apperr.ErrBusy)
		return
	}
	wsConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{m.d.PublicHost}})
	if err != nil {
		return // Accept has answered
	}
	wsConn.SetReadLimit(MaxFrameBytes)

	ctx, stop := context.WithCancel(context.WithoutCancel(r.Context()))
	c := &Conn{
		mux: m, ws: wsConn, sess: sess, ctx: ctx, stop: stop, q: newQueue(QueueCapacity),
		channels: map[int]Channel{}, opening: map[int][]item{}, visSubs: map[int]func(bool){},
	}
	c.lastPong.Store(m.d.Clock.Now().UnixNano())
	m.mu.Lock()
	m.conns[c] = struct{}{}
	m.wg.Add(1)
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.conns, c)
		m.mu.Unlock()
		m.wg.Done()
	}()

	unregister := m.d.Sessions.Registry().Register(sess.Hash, "ws", func() error {
		if m.shuttingDown() {
			c.close(websocket.StatusGoingAway, "server shutting down")
		} else {
			c.close(CloseSessionEnded, "session ended")
		}
		return nil
	})
	defer unregister()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); c.writer() }()
	go func() { defer wg.Done(); c.heartbeat() }()
	c.reader()
	c.close(websocket.StatusNormalClosure, "")
	wg.Wait()
}

// heartbeat pings every 20 s and closes the connection after 60 s without a pong.
func (c *Conn) heartbeat() {
	t := c.mux.d.Clock.NewTicker(pingInterval)
	defer t.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-t.C():
			last := time.Unix(0, c.lastPong.Load())
			if c.mux.d.Clock.Now().Sub(last) >= heartbeatTimeout {
				c.close(CloseHeartbeat, "heartbeat timeout")
				return
			}
			if it, err := encode(0, TypePing, nil); err == nil {
				c.q.pushForce(it)
			}
		}
	}
}

// reader handles client frames until the connection ends.
func (c *Conn) reader() {
	for {
		typ, data, err := c.ws.Read(c.ctx)
		if err != nil {
			return // closed, cancelled or too big (the library already answered 1009)
		}
		if typ != websocket.MessageText {
			c.close(CloseProtocolError, "text frames only")
			return
		}
		var in In
		if err := json.Unmarshal(data, &in); err != nil || in.Op == "" {
			c.close(CloseProtocolError, "malformed frame")
			return
		}
		if in.Op == "pong" && in.Ch == 0 {
			c.lastPong.Store(c.mux.d.Clock.Now().UnixNano())
			continue
		}
		c.mux.d.Sessions.KeepAlive(c.ctx, c.sess.Hash)
		c.dispatch(in)
	}
}

func (c *Conn) dispatch(in In) {
	switch {
	case in.Ch == 0:
		c.control(in)
	case strings.HasSuffix(in.Op, ".open"):
		c.open(in)
	default:
		c.route(in)
	}
}

func badRequest(msg string) error {
	return &apperr.APIError{Status: http.StatusBadRequest, Code: "bad_request", Message: msg}
}

func (c *Conn) control(in In) {
	if in.Op != "visibility" {
		c.sendError(0, badRequest("unknown op "+in.Op))
		return
	}
	var d struct {
		Hidden bool `json:"hidden"`
	}
	if err := json.Unmarshal(in.D, &d); err != nil {
		c.sendError(0, badRequest("visibility needs {\"hidden\": true|false}"))
		return
	}
	c.setHidden(d.Hidden)
}

func (c *Conn) open(in In) {
	if in.Ch < 1 || in.Ch > MaxChannelID {
		c.sendError(in.Ch, badRequest("channel numbers run from 1 to 2147483647"))
		return
	}
	c.mux.mu.Lock()
	f, known := c.mux.factories[in.Op]
	c.mux.mu.Unlock()
	if !known {
		c.sendError(in.Ch, badRequest("unknown op "+in.Op))
		return
	}
	c.mu.Lock()
	_, used := c.channels[in.Ch]
	_, opening := c.opening[in.Ch]
	full := len(c.channels)+len(c.opening) >= MaxChannels
	if used || opening || full {
		c.mu.Unlock()
		if full && !used && !opening {
			c.sendError(in.Ch, &apperr.APIError{Status: http.StatusServiceUnavailable, Code: "server_busy", Message: "Too many open channels on this connection."})
		} else {
			c.sendError(in.Ch, badRequest("that channel number is already in use"))
		}
		return
	}
	c.opening[in.Ch] = nil
	c.mu.Unlock()

	chn, err := f(c.ctx, c, in.Ch, in.D)

	c.mu.Lock()
	held := c.opening[in.Ch]
	delete(c.opening, in.Ch)
	if err == nil {
		c.channels[in.Ch] = chn
	}
	c.mu.Unlock()
	if err != nil {
		c.sendError(in.Ch, err)
		return
	}
	if it, e := encode(in.Ch, TypeOpened, nil); e == nil {
		c.q.pushForce(it)
	}
	for _, it := range held {
		if it.droppable {
			c.q.pushDroppable(it)
		} else if perr := c.q.push(it); errors.Is(perr, ErrSlowConsumer) {
			c.CloseChannel(in.Ch, "slow_consumer")
			return
		}
	}
}

func (c *Conn) route(in In) {
	c.mu.Lock()
	chn, ok := c.channels[in.Ch]
	c.mu.Unlock()
	if !ok {
		c.sendError(in.Ch, badRequest("no such channel"))
		return
	}
	if strings.HasSuffix(in.Op, ".close") {
		c.CloseChannel(in.Ch, "closed")
		return
	}
	if err := chn.Handle(c.ctx, in.Op, in.D); err != nil {
		c.sendError(in.Ch, err)
	}
}
