package watch_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/store/storetest"
	"github.com/bdnix/kubelook/internal/testutil"
	"github.com/bdnix/kubelook/internal/ws"
)

type chanEnv struct {
	*hubEnv
	srv *httptest.Server
	id  string
}

func newChanEnv(t *testing.T, mutate ...func(*hubEnv)) *chanEnv {
	t.Helper()
	e := newHubEnv(t, mutate...)
	db := storetest.New(t)
	wsClock := clock.NewFake(t0)
	user, err := store.NewUsers(db, wsClock, func() string { return "u1" }).Upsert(t.Context(), "google", "s", "e", "n")
	require.NoError(t, err)
	mgr := session.NewManager(session.Deps{
		Store: store.NewSessions(db), Clock: wsClock, Rand: testutil.Rand("watch"), Log: slog.New(slog.DiscardHandler),
		IdleTimeout: 30 * time.Minute, MaxLifetime: 12 * time.Hour,
	})
	id, _, err := mgr.Create(t.Context(), user.ID)
	require.NoError(t, err)
	mux := ws.NewMux(ws.Deps{Clock: wsClock, Log: slog.New(slog.DiscardHandler), Sessions: mgr, PublicHost: "kubelook.example.com",
		Fail: func(w http.ResponseWriter, _ *http.Request, _ error) { w.WriteHeader(http.StatusUnauthorized) }})
	mux.Register("watch.open", e.hub.Factory())
	srv := httptest.NewServer(mgr.Require(func(w http.ResponseWriter, _ *http.Request, _ error) { w.WriteHeader(http.StatusUnauthorized) })(mux))
	t.Cleanup(srv.Close)
	t.Cleanup(mux.Shutdown)
	return &chanEnv{hubEnv: e, srv: srv, id: id}
}

func (e *chanEnv) connect(t *testing.T) *websocket.Conn {
	t.Helper()
	h := http.Header{"Cookie": {"kubelook_session=" + e.id}}
	c, _, err := websocket.Dial(t.Context(), "ws"+strings.TrimPrefix(e.srv.URL, "http")+"/api/v1/ws", &websocket.DialOptions{HTTPHeader: h}) //nolint:bodyclose // a WebSocket handshake response has no body to close
	require.NoError(t, err)
	c.SetReadLimit(16 << 20)
	t.Cleanup(func() { _ = c.CloseNow() })
	return c
}

type wsFrame struct {
	Ch int            `json:"ch"`
	T  string         `json:"t"`
	D  map[string]any `json:"d"`
}

func recvFrame(t *testing.T, c *websocket.Conn) wsFrame {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, data, err := c.Read(ctx)
	require.NoError(t, err)
	var f wsFrame
	require.NoError(t, json.Unmarshal(data, &f))
	return f
}

func sendOp(t *testing.T, c *websocket.Conn, op string, ch int, d any) {
	t.Helper()
	b, err := json.Marshal(map[string]any{"op": op, "ch": ch, "d": d})
	require.NoError(t, err)
	require.NoError(t, c.Write(t.Context(), websocket.MessageText, b))
}

func openReq(ns string) map[string]string {
	return map[string]string{"cluster": "c", "group": "core", "version": "v1", "resource": "pods", "namespace": ns}
}

func TestChannel_OpenSendsOpenedThenSnapshotThenEvents(t *testing.T) {
	e := newChanEnv(t)
	c := e.connect(t)
	sendOp(t, c, "watch.open", 1, openReq(""))
	assert.Equal(t, "opened", recvFrame(t, c).T)
	snap := recvFrame(t, c)
	assert.Equal(t, "snapshot", snap.T)
	assert.Equal(t, 1, snap.Ch)
	assert.Equal(t, "100", snap.D["resourceVersion"])
	assert.Equal(t, false, snap.D["more"])
	assert.Len(t, snap.D["items"], 2)

	w := e.watcher(t)
	w.Add(withRV(pod("c"), "101"))
	ev := recvFrame(t, c)
	assert.Equal(t, "event", ev.T)
	assert.Equal(t, "ADDED", ev.D["type"])
	assert.Equal(t, "c", ev.D["object"].(map[string]any)["metadata"].(map[string]any)["name"])
}

func TestChannel_LargeSnapshotIsSplitIntoFramesOfFiveHundred(t *testing.T) {
	e := newChanEnv(t, func(e *hubEnv) {
		for i := range 1200 {
			e.items = append(e.items, pod(fmt.Sprintf("p%04d", i)))
		}
	})
	c := e.connect(t)
	sendOp(t, c, "watch.open", 1, openReq(""))
	assert.Equal(t, "opened", recvFrame(t, c).T)
	var sizes []int
	var more []any
	for len(sizes) < 3 {
		f := recvFrame(t, c)
		require.Equal(t, "snapshot", f.T)
		sizes = append(sizes, len(f.D["items"].([]any)))
		more = append(more, f.D["more"])
	}
	assert.Equal(t, []int{500, 500, 202}, sizes)
	assert.Equal(t, []any{true, true, false}, more)
}

func TestChannel_OpenPassesTheFieldSelectorToTheUpstream(t *testing.T) {
	e := newChanEnv(t)
	c := e.connect(t)
	req := openReq("ns1")
	req["fieldSelector"] = "metadata.name=a"
	sendOp(t, c, "watch.open", 1, req)
	require.Equal(t, "opened", recvFrame(t, c).T)
	require.Equal(t, "snapshot", recvFrame(t, c).T)
	e.spy.mu.Lock()
	defer e.spy.mu.Unlock()
	require.NotEmpty(t, e.spy.lists)
	assert.Equal(t, "metadata.name=a", e.spy.lists[0].FieldSelector)
}

func TestChannel_EmptySnapshotIsOneFrame(t *testing.T) {
	e := newChanEnv(t, func(e *hubEnv) { e.items = nil })
	c := e.connect(t)
	sendOp(t, c, "watch.open", 1, openReq("ns1"))
	require.Equal(t, "opened", recvFrame(t, c).T)
	f := recvFrame(t, c)
	assert.Equal(t, "snapshot", f.T)
	assert.Empty(t, f.D["items"])
	assert.Equal(t, false, f.D["more"])
}

func TestChannel_BadOpenRequests(t *testing.T) {
	e := newChanEnv(t)
	c := e.connect(t)
	sendOp(t, c, "watch.open", 1, "not an object")
	f := recvFrame(t, c)
	assert.Equal(t, "error", f.T)
	assert.Equal(t, "bad_request", f.D["code"])
	sendOp(t, c, "watch.open", 2, map[string]string{"cluster": "c"})
	assert.Equal(t, "bad_request", recvFrame(t, c).D["code"])
}

func TestChannel_ForbiddenList_ErrorFrameThenClosed(t *testing.T) {
	e := newChanEnv(t, func(e *hubEnv) {
		e.listErr = apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", fmt.Errorf("RBAC"))
	})
	c := e.connect(t)
	sendOp(t, c, "watch.open", 1, openReq(""))
	assert.Equal(t, "opened", recvFrame(t, c).T)
	f := recvFrame(t, c)
	assert.Equal(t, "error", f.T)
	assert.Equal(t, "forbidden", f.D["code"])
}

func TestChannel_ClusterNotInTheSession_ErrorFrame(t *testing.T) {
	e := newChanEnv(t)
	e.spy.Known = map[string]bool{}
	c := e.connect(t)
	sendOp(t, c, "watch.open", 1, openReq(""))
	assert.Equal(t, "opened", recvFrame(t, c).T)
	f := recvFrame(t, c)
	assert.Equal(t, "error", f.T)
	assert.Equal(t, "cluster_unavailable", f.D["code"])
}

func TestChannel_HiddenTab_PausesAfterTheIdleTime(t *testing.T) {
	e := newChanEnv(t)
	c := e.connect(t)
	sendOp(t, c, "watch.open", 1, openReq(""))
	require.Equal(t, "opened", recvFrame(t, c).T)
	require.Equal(t, "snapshot", recvFrame(t, c).T)
	e.watcher(t)

	sendOp(t, c, "visibility", 0, map[string]bool{"hidden": true})
	e.clock.BlockUntil(1)
	e.clock.Advance(30 * time.Second)
	f := recvFrame(t, c)
	assert.Equal(t, "paused", f.T)
	assert.Equal(t, 1, f.Ch)
}

func TestChannel_CloseOp_DetachesFromTheHub(t *testing.T) {
	e := newChanEnv(t)
	c := e.connect(t)
	sendOp(t, c, "watch.open", 1, openReq(""))
	require.Equal(t, "opened", recvFrame(t, c).T)
	require.Equal(t, "snapshot", recvFrame(t, c).T)
	w := e.watcher(t)
	sendOp(t, c, "watch.close", 1, nil)
	assert.Equal(t, "closed", recvFrame(t, c).T)
	e.clock.BlockUntil(1) // the last subscriber left: the idle timer runs
	e.clock.Advance(30 * time.Second)
	e.hub.Close()
	assert.True(t, w.IsStopped())
}

func TestChannel_OpsAreRefused(t *testing.T) {
	e := newChanEnv(t)
	c := e.connect(t)
	sendOp(t, c, "watch.open", 1, openReq(""))
	require.Equal(t, "opened", recvFrame(t, c).T)
	require.Equal(t, "snapshot", recvFrame(t, c).T)
	sendOp(t, c, "watch.poke", 1, nil)
	f := recvFrame(t, c)
	assert.Equal(t, "error", f.T)
	assert.Equal(t, "bad_request", f.D["code"])
}
