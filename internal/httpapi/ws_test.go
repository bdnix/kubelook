package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/httpapi"
	"github.com/bdnix/kubelook/internal/ws"
)

func wsServer(t *testing.T, e *kubeEnv) string {
	t.Helper()
	e.deps.WS = ws.NewMux(ws.Deps{
		Clock: e.fake, Log: e.deps.Log, Sessions: e.deps.Sessions, PublicHost: "localhost:8080", Fail: httpapi.ErrorWriter(e.deps.Log),
	})
	srv := httptest.NewServer(httpapi.New(e.deps))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/ws"
}

func TestWS_UpgradeNeedsASessionAndAllowedOrigin(t *testing.T) {
	e := newKubeEnv(t)
	url := wsServer(t, e)
	ctx := t.Context()

	_, resp, err := websocket.Dial(ctx, url, nil) //nolint:bodyclose // the handshake response of a WebSocket has no body to close
	require.Error(t, err)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "the session middleware answers first")

	h := http.Header{"Cookie": {"kubelook_session=" + e.id}, "Origin": {"https://evil.example"}}
	_, resp, err = websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: h}) //nolint:bodyclose // the handshake response of a WebSocket has no body to close
	require.Error(t, err)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	h.Set("Origin", "http://localhost:8080")
	c, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: h}) //nolint:bodyclose // the handshake response of a WebSocket has no body to close
	require.NoError(t, err)
	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	_ = c.Close(websocket.StatusNormalClosure, "")
}

func TestWS_NotWired_404(t *testing.T) {
	e := newKubeEnv(t)
	assert.Equal(t, http.StatusNotFound, e.get("/api/v1/ws").Code)
}
