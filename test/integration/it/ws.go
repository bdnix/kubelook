//go:build integration

package it

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// WS is one WebSocket of the session with tagged channels (internal/ws).
type WS struct {
	t    testing.TB
	conn *websocket.Conn
	mu   sync.Mutex
	next int
	chs  map[int]*Channel
	done chan struct{}
	code int
}

type frame struct {
	Ch int             `json:"ch"`
	T  string          `json:"t"`
	D  json.RawMessage `json:"d"`
}

// WS connects /api/v1/ws with the session cookie and the Origin the server expects.
func (c *Client) WS() *WS {
	c.t.Helper()
	u, _ := url.Parse(BaseURL())
	var cookies []string
	for _, ck := range c.HC.Jar.Cookies(u) {
		cookies = append(cookies, ck.Name+"="+ck.Value)
	}
	conn, resp, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(BaseURL(), "http")+"/api/v1/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {BaseURL()}, "Cookie": {strings.Join(cookies, "; ")}},
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		c.t.Fatalf("opening the WebSocket: %v (HTTP %d)", err, status)
	}
	conn.SetReadLimit(4 << 20)
	w := &WS{t: c.t, conn: conn, chs: map[int]*Channel{}, done: make(chan struct{})}
	go w.read()
	c.t.Cleanup(func() { _ = conn.CloseNow() })
	return w
}

func (w *WS) read() {
	defer close(w.done)
	for {
		_, data, err := w.conn.Read(context.Background())
		if err != nil {
			w.mu.Lock()
			w.code = int(websocket.CloseStatus(err))
			for _, ch := range w.chs {
				ch.wake()
			}
			w.mu.Unlock()
			return
		}
		var f frame
		if json.Unmarshal(data, &f) != nil {
			continue
		}
		w.mu.Lock()
		if ch := w.chs[f.Ch]; ch != nil {
			ch.push(f)
		}
		w.mu.Unlock()
	}
}

// Channel is one tagged channel. Frames are kept until a Next call consumes them.
type Channel struct {
	w      *WS
	ID     int
	mu     sync.Mutex
	frames []*queued
	signal chan struct{}
}

type queued struct {
	f    frame
	used bool
}

func (ch *Channel) push(f frame) {
	ch.mu.Lock()
	ch.frames = append(ch.frames, &queued{f: f})
	ch.mu.Unlock()
	ch.wake()
}

func (ch *Channel) wake() {
	select {
	case ch.signal <- struct{}{}:
	default:
	}
}

// TryOpen sends "<x>.open" on a new channel and returns it without waiting for "opened".
func (w *WS) TryOpen(op string, d any) *Channel {
	w.t.Helper()
	w.mu.Lock()
	w.next++
	ch := &Channel{w: w, ID: w.next, signal: make(chan struct{}, 1)}
	w.chs[ch.ID] = ch
	w.mu.Unlock()
	ch.Send(op, d)
	return ch
}

// Open is TryOpen that waits (up to 30 s) for "opened" and fails the test on an "error" frame.
func (w *WS) Open(op string, d any) *Channel {
	w.t.Helper()
	ch := w.TryOpen(op, d)
	typ, raw := ch.NextOf(30*time.Second, "opened", "error")
	if typ == "error" {
		w.t.Fatalf("opening %s: %s", op, raw)
	}
	return ch
}

// Send sends an op on the channel (a nil d sends no data).
func (ch *Channel) Send(op string, d any) {
	ch.w.t.Helper()
	msg := map[string]any{"op": op, "ch": ch.ID}
	if d != nil {
		msg["d"] = d
	}
	data, err := json.Marshal(msg)
	if err != nil {
		ch.w.t.Fatal(err)
	}
	if err := ch.w.conn.Write(context.Background(), websocket.MessageText, data); err != nil {
		ch.w.t.Fatalf("sending %s: %v", op, err)
	}
}

// Next waits for the next unconsumed frame of that type and returns its data.
func (ch *Channel) Next(typ string, within time.Duration) json.RawMessage {
	ch.w.t.Helper()
	got, raw := ch.NextOf(within, typ)
	if got == "" {
		ch.w.t.Fatalf("channel %d: no %q frame within %s", ch.ID, typ, within)
	}
	return raw
}

// NextOf waits for the next unconsumed frame whose type is one of types. It returns "" on timeout
// or when the socket closed.
func (ch *Channel) NextOf(within time.Duration, types ...string) (string, json.RawMessage) {
	ch.w.t.Helper()
	deadline := time.After(within)
	for {
		ch.mu.Lock()
		for _, q := range ch.frames {
			if q.used {
				continue
			}
			for _, typ := range types {
				if q.f.T == typ {
					q.used = true
					ch.mu.Unlock()
					return typ, q.f.D
				}
			}
		}
		ch.mu.Unlock()
		select {
		case <-ch.signal:
		case <-ch.w.done:
			return "", nil
		case <-deadline:
			return "", nil
		}
	}
}

// Close sends "<x>.close" for the channel's family, e.g. Close("watch.close").
func (ch *Channel) Close(op string) { ch.Send(op, nil) }

// Control sends an op on channel 0 (visibility and the like).
func (w *WS) Control(op string, d any) {
	w.t.Helper()
	msg := map[string]any{"op": op, "ch": 0}
	if d != nil {
		msg["d"] = d
	}
	data, err := json.Marshal(msg)
	if err != nil {
		w.t.Fatal(err)
	}
	if err := w.conn.Write(context.Background(), websocket.MessageText, data); err != nil {
		w.t.Fatalf("sending %s: %v", op, err)
	}
}

// Closed waits for the socket to close and returns the close code (0 when it did not close in time).
func (w *WS) Closed(within time.Duration) (code int) {
	w.t.Helper()
	select {
	case <-w.done:
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.code
	case <-time.After(within):
		return 0
	}
}
