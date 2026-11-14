package ws_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/store/storetest"
	"github.com/bdnix/kubelook/internal/testutil"
	"github.com/bdnix/kubelook/internal/ws"
)

var t0 = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

// echo is a channel that answers "echo.say" with "said".
type echo struct {
	c      *ws.Conn
	ch     int
	closed *atomic.Int32
}

func (e *echo) Handle(_ context.Context, op string, d json.RawMessage) error {
	if op == "echo.fail" {
		return errors.New("boom")
	}
	return e.c.Send(e.ch, "said", d)
}
func (e *echo) Close() { e.closed.Add(1) }

type env struct {
	srv      *httptest.Server
	mux      *ws.Mux
	mgr      *session.Manager
	clock    *clock.Fake
	sess     session.Session
	id       string
	closed   atomic.Int32
	hidden   chan bool
	sessions *store.Sessions
}

func fail(w http.ResponseWriter, _ *http.Request, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, apperr.ErrUnauthenticated):
		status = http.StatusUnauthorized
	case errors.Is(err, apperr.ErrForbidden):
		status = http.StatusForbidden
	case errors.Is(err, apperr.ErrBusy):
		status = http.StatusServiceUnavailable
	}
	w.WriteHeader(status)
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{clock: clock.NewFake(t0), hidden: make(chan bool, 8)}
	db := storetest.New(t)
	user, err := store.NewUsers(db, e.clock, func() string { return "u1" }).Upsert(t.Context(), "google", "s", "e", "n")
	require.NoError(t, err)
	e.sessions = store.NewSessions(db)
	log := slog.New(slog.DiscardHandler)
	e.mgr = session.NewManager(session.Deps{
		Store: e.sessions, Clock: e.clock, Rand: testutil.Rand("ws"), Log: log, IdleTimeout: 30 * time.Minute, MaxLifetime: 12 * time.Hour,
	})
	e.id, e.sess, err = e.mgr.Create(t.Context(), user.ID)
	require.NoError(t, err)
	e.mux = ws.NewMux(ws.Deps{Clock: e.clock, Log: log, Sessions: e.mgr, PublicHost: "kubelook.example.com", Fail: fail})
	e.mux.Register("echo.open", func(_ context.Context, c *ws.Conn, ch int, d json.RawMessage) (ws.Channel, error) {
		if string(d) == `"reject"` {
			return nil, apperr.ErrForbidden
		}
		if string(d) == `"early"` {
			_ = c.Send(ch, "early", nil) // sent before the factory returns
		}
		if string(d) == `"watchvis"` {
			c.OnVisibility(func(h bool) { e.hidden <- h })
		}
		return &echo{c: c, ch: ch, closed: &e.closed}, nil
	})
	e.srv = httptest.NewServer(e.mgr.Require(func(w http.ResponseWriter, r *http.Request, err error) { fail(w, r, err) })(e.mux))
	t.Cleanup(e.srv.Close)
	return e
}

// dial returns the connection and the HTTP status of the handshake (0 when there was no answer).
func (e *env) dial(t *testing.T, headers map[string]string) (*websocket.Conn, int, error) {
	t.Helper()
	h := http.Header{}
	if headers["Cookie"] == "" {
		h.Set("Cookie", "kubelook_session="+e.id)
	}
	for k, v := range headers {
		if v != "" {
			h.Set(k, v)
		}
	}
	c, resp, err := websocket.Dial(t.Context(), "ws"+strings.TrimPrefix(e.srv.URL, "http")+"/api/v1/ws", &websocket.DialOptions{HTTPHeader: h}) //nolint:bodyclose // the handshake response of a WebSocket has no body to close
	if err == nil {
		t.Cleanup(func() { _ = c.CloseNow() })
	}
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}
	return c, status, err
}

func (e *env) connect(t *testing.T) *websocket.Conn {
	t.Helper()
	c, _, err := e.dial(t, nil)
	require.NoError(t, err)
	return c
}

func send(t *testing.T, c *websocket.Conn, op string, ch int, d any) {
	t.Helper()
	b, err := json.Marshal(map[string]any{"op": op, "ch": ch, "d": d})
	require.NoError(t, err)
	require.NoError(t, c.Write(t.Context(), websocket.MessageText, b))
}

type frame struct {
	Ch  int            `json:"ch"`
	T   string         `json:"t"`
	D   map[string]any `json:"d"`
	Raw json.RawMessage
}

func recv(t *testing.T, c *websocket.Conn) frame {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, data, err := c.Read(ctx)
	require.NoError(t, err)
	var f frame
	require.NoError(t, json.Unmarshal(data, &f))
	f.Raw = data
	return f
}

func closeCode(t *testing.T, c *websocket.Conn) websocket.StatusCode {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for {
		_, _, err := c.Read(ctx)
		if err != nil {
			code := websocket.CloseStatus(err)
			require.NotEqual(t, websocket.StatusCode(-1), code, "expected a close frame, got %v", err)
			return code
		}
	}
}

func TestUpgrade_SessionAndOrigin(t *testing.T) {
	e := newEnv(t)
	_, resp, err := e.dial(t, map[string]string{"Cookie": "kubelook_session=nope"})
	require.Error(t, err)
	assert.Equal(t, http.StatusUnauthorized, resp)

	_, resp, err = e.dial(t, map[string]string{"Origin": "https://evil.example"})
	require.Error(t, err)
	assert.Equal(t, http.StatusForbidden, resp)

	_, resp, err = e.dial(t, map[string]string{"Origin": "https://kubelook.example.com"})
	require.NoError(t, err)
	assert.Equal(t, http.StatusSwitchingProtocols, resp)

	_, _, err = e.dial(t, nil) // no Origin: not a browser
	require.NoError(t, err)
}

func TestProtocol_UnknownOpKeepsTheConnectionMalformedFramesCloseIt(t *testing.T) {
	e := newEnv(t)
	c := e.connect(t)
	send(t, c, "nope.open", 3, nil)
	f := recv(t, c)
	assert.Equal(t, "error", f.T)
	assert.Equal(t, 3, f.Ch)
	assert.Equal(t, "bad_request", f.D["code"])
	send(t, c, "nope", 0, nil)
	assert.Equal(t, "bad_request", recv(t, c).D["code"])
	send(t, c, "echo.say", 9, nil)
	assert.Equal(t, "no such channel", recv(t, c).D["message"])
	send(t, c, "echo.open", 1, "x") // still alive
	assert.Equal(t, "opened", recv(t, c).T)

	bad := e.connect(t)
	require.NoError(t, bad.Write(t.Context(), websocket.MessageText, []byte("not json")))
	assert.Equal(t, websocket.StatusPolicyViolation, closeCode(t, bad))

	noOp := e.connect(t)
	require.NoError(t, noOp.Write(t.Context(), websocket.MessageText, []byte(`{"ch":1}`)))
	assert.Equal(t, websocket.StatusPolicyViolation, closeCode(t, noOp))

	bin := e.connect(t)
	require.NoError(t, bin.Write(t.Context(), websocket.MessageBinary, []byte{1}))
	assert.Equal(t, websocket.StatusPolicyViolation, closeCode(t, bin))
}

func TestChannels_OpenSayCloseAndReuse(t *testing.T) {
	e := newEnv(t)
	c := e.connect(t)
	send(t, c, "echo.open", 5, "hi")
	f := recv(t, c)
	assert.Equal(t, ws.Out{Ch: 5, T: "opened"}.T, f.T)
	assert.Equal(t, 5, f.Ch)

	send(t, c, "echo.say", 5, map[string]string{"x": "y"})
	f = recv(t, c)
	assert.Equal(t, "said", f.T)
	assert.Equal(t, 5, f.Ch)
	assert.Equal(t, "y", f.D["x"])

	send(t, c, "echo.open", 5, "again")
	f = recv(t, c)
	assert.Equal(t, "error", f.T, "an open channel number cannot be reused")

	send(t, c, "echo.fail", 5, nil)
	assert.Equal(t, "internal", recv(t, c).D["code"], "a Handle error is an error frame, the channel stays")

	send(t, c, "echo.close", 5, nil)
	f = recv(t, c)
	assert.Equal(t, "closed", f.T)
	assert.EqualValues(t, 1, e.closed.Load())

	send(t, c, "echo.open", 5, "reopen")
	assert.Equal(t, "opened", recv(t, c).T)
}

func TestChannels_FactoryErrorsAndInvalidNumbers(t *testing.T) {
	e := newEnv(t)
	c := e.connect(t)
	send(t, c, "echo.open", 1, "reject")
	f := recv(t, c)
	assert.Equal(t, "error", f.T)
	assert.Equal(t, "forbidden", f.D["code"])
	send(t, c, "echo.open", 1, "ok")
	assert.Equal(t, "opened", recv(t, c).T, "a failed open leaves the number free")

	for _, ch := range []int{-1, 1 << 31} {
		send(t, c, "echo.open", ch, "x")
		assert.Equal(t, "bad_request", recv(t, c).D["code"])
	}
}

func TestChannels_FramesSentWhileOpeningFollowTheOpenedFrame(t *testing.T) {
	e := newEnv(t)
	c := e.connect(t)
	send(t, c, "echo.open", 2, "early")
	assert.Equal(t, "opened", recv(t, c).T)
	assert.Equal(t, "early", recv(t, c).T)
}

func TestChannels_MoreThanHundredOpenChannels_ServerBusy(t *testing.T) {
	e := newEnv(t)
	c := e.connect(t)
	for i := 1; i <= ws.MaxChannels; i++ {
		send(t, c, "echo.open", i, "x")
		require.Equal(t, "opened", recv(t, c).T)
	}
	send(t, c, "echo.open", 101, "x")
	f := recv(t, c)
	assert.Equal(t, "error", f.T)
	assert.Equal(t, "server_busy", f.D["code"])
}

func TestHeartbeat_PingEvery20sAndCloseAfter60sWithoutPong(t *testing.T) {
	e := newEnv(t)
	c := e.connect(t)
	e.clock.BlockUntil(1)
	e.clock.Advance(20 * time.Second)
	f := recv(t, c)
	assert.Equal(t, "ping", f.T)
	assert.Equal(t, 0, f.Ch)
	e.clock.Advance(20 * time.Second)
	assert.Equal(t, "ping", recv(t, c).T)
	e.clock.Advance(20 * time.Second)
	assert.Equal(t, websocket.StatusCode(ws.CloseHeartbeat), closeCode(t, c))
}

// roundTrip proves the server has processed every frame sent so far (the reader handles them in order).
func roundTrip(t *testing.T, c *websocket.Conn, ch int) {
	t.Helper()
	send(t, c, "echo.open", ch, "x")
	for {
		if f := recv(t, c); f.T == "opened" && f.Ch == ch {
			return
		}
	}
}

func TestHeartbeat_PongResetsTheTimer(t *testing.T) {
	e := newEnv(t)
	c := e.connect(t)
	e.clock.BlockUntil(1)
	e.clock.Advance(20 * time.Second)
	require.Equal(t, "ping", recv(t, c).T)
	send(t, c, "pong", 0, nil)
	roundTrip(t, c, 90)
	e.clock.Advance(20 * time.Second)
	require.Equal(t, "ping", recv(t, c).T)
	e.clock.Advance(20 * time.Second) // 60 s after connecting, 40 s after the pong
	require.Equal(t, "ping", recv(t, c).T)
	e.clock.Advance(20 * time.Second) // 60 s since the pong
	assert.Equal(t, websocket.StatusCode(ws.CloseHeartbeat), closeCode(t, c))
}

func TestVisibility_UpdatesTheConnectionAndNotifiesSubscribers(t *testing.T) {
	e := newEnv(t)
	c := e.connect(t)
	send(t, c, "echo.open", 1, "watchvis")
	require.Equal(t, "opened", recv(t, c).T)
	send(t, c, "visibility", 0, map[string]bool{"hidden": true})
	assert.True(t, testutil.Recv(t, e.hidden))
	send(t, c, "visibility", 0, map[string]bool{"hidden": false})
	assert.False(t, testutil.Recv(t, e.hidden))
	send(t, c, "visibility", 0, "garbage")
	assert.Equal(t, "bad_request", recv(t, c).D["code"])
}

func TestFrames_OverOneMiB_Close1009(t *testing.T) {
	e := newEnv(t)
	c := e.connect(t)
	c.SetReadLimit(4 << 20)
	big := strings.Repeat("a", ws.MaxFrameBytes+1)
	require.NoError(t, c.Write(t.Context(), websocket.MessageText, []byte(`{"op":"echo.say","ch":1,"d":"`+big+`"}`)))
	assert.Equal(t, websocket.StatusMessageTooBig, closeCode(t, c))
}

func TestSessionEnd_Closes4401AndEveryChannelOnce(t *testing.T) {
	e := newEnv(t)
	c := e.connect(t)
	for i := 1; i <= 2; i++ {
		send(t, c, "echo.open", i, "x")
		require.Equal(t, "opened", recv(t, c).T)
	}
	ended := make(chan error, 1)
	go func() { ended <- e.mgr.End(t.Context(), e.sess.Hash) }()
	assert.Equal(t, websocket.StatusCode(ws.CloseSessionEnded), closeCode(t, c))
	require.NoError(t, testutil.Recv(t, ended))
	assert.EqualValues(t, 2, e.closed.Load(), "every channel is closed once")
}

func TestShutdown_Closes1001AndRefusesNewConnections(t *testing.T) {
	e := newEnv(t)
	c := e.connect(t)
	send(t, c, "echo.open", 1, "x")
	require.Equal(t, "opened", recv(t, c).T)
	done := make(chan struct{})
	go func() { e.mux.Shutdown(); close(done) }()
	assert.Equal(t, websocket.StatusGoingAway, closeCode(t, c))
	testutil.Recv(t, done)
	assert.EqualValues(t, 1, e.closed.Load())
	_, resp, err := e.dial(t, nil)
	require.Error(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, resp)
}

func TestKeepAlive_AnyFrameExceptPongCountsAsActivity(t *testing.T) {
	e := newEnv(t)
	c := e.connect(t)
	e.clock.BlockUntil(1)
	e.clock.Advance(50 * time.Second)
	send(t, c, "pong", 0, nil)
	send(t, c, "nope", 0, nil) // not a pong → keep-alive
	for f := recv(t, c); f.T != "error"; f = recv(t, c) {
		require.Equal(t, "ping", f.T)
	}
	row, err := e.sessions.Get(t.Context(), e.sess.Hash)
	require.NoError(t, err)
	assert.Equal(t, e.clock.Now().UnixMilli(), row.LastSeenAt.UnixMilli())
}

func TestErrorBody_MapsSentinelsAndAPIErrors(t *testing.T) {
	assert.Equal(t, "forbidden", ws.ErrorBody(apperr.ErrForbidden)["code"])
	assert.Equal(t, "cluster_unavailable", ws.ErrorBody(apperr.ErrClusterUnavailable)["code"])
	assert.Equal(t, "not_found", ws.ErrorBody(apperr.ErrNotFound)["code"])
	assert.Equal(t, "bad_request", ws.ErrorBody(apperr.ErrInvalid)["code"])
	assert.Equal(t, "conflict", ws.ErrorBody(apperr.ErrConflict)["code"])
	assert.Equal(t, "unauthenticated", ws.ErrorBody(apperr.ErrUnauthenticated)["code"])
	assert.Equal(t, "server_busy", ws.ErrorBody(apperr.ErrBusy)["code"])
	assert.Equal(t, "too_large", ws.ErrorBody(apperr.ErrTooLarge)["code"])
	body := ws.ErrorBody(&apperr.APIError{Code: "vault_unreachable", Message: "down"})
	assert.Equal(t, map[string]string{"code": "vault_unreachable", "message": "down"}, body)
	assert.Equal(t, map[string]string{"code": "internal", "message": "internal error"}, ws.ErrorBody(errors.New("secret detail")))
}
