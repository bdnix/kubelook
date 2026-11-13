package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/config"
	"github.com/bdnix/kubelook/internal/httpapi"
	"github.com/bdnix/kubelook/internal/store"
)

var t0 = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

const wantCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'; worker-src 'self' blob:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"

type harness struct {
	db   *store.DB
	deps httpapi.Deps
	logs *bytes.Buffer
	fake *clock.Fake
}

func newHarness(publicURL string) *harness {
	h := &harness{logs: &bytes.Buffer{}, fake: clock.NewFake(t0)}
	n := 0
	h.deps = httpapi.Deps{
		Config: &config.Config{Server: config.Server{PublicURL: publicURL}},
		Clock:  h.fake,
		Log:    slog.New(slog.NewJSONHandler(h.logs, nil)),
		WebFS:  fstest.MapFS{"index.html": {Data: []byte("<html>app</html>")}},
		Ready:  func(context.Context) error { return nil },
		NewID:  func() string { n++; return fmt.Sprintf("generated-%d", n) },
	}
	return h
}

func (h *harness) do(method, target string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	httpapi.New(h.deps).ServeHTTP(rec, req)
	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct{ Code, Message string }
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), rec.Body.String())
	return body.Error.Code
}

func TestHealthz_Returns200Ok(t *testing.T) {
	rec := newHarness("https://k.example.com").do(http.MethodGet, "/healthz")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
}

func TestReadyz_ReflectsReadyFunc(t *testing.T) {
	h := newHarness("https://k.example.com")
	assert.Equal(t, http.StatusOK, h.do(http.MethodGet, "/readyz").Code)

	h.deps.Ready = func(context.Context) error { return errors.New("db down") }
	rec := h.do(http.MethodGet, "/readyz")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "internal", errorCode(t, rec))
	assert.NotContains(t, rec.Body.String(), "db down", "the cause is logged, not returned")
	assert.Contains(t, h.logs.String(), "db down")
}

func TestReadyz_NilReadyFunc_IsReady(t *testing.T) {
	h := newHarness("https://k.example.com")
	h.deps.Ready = nil
	assert.Equal(t, http.StatusOK, h.do(http.MethodGet, "/readyz").Code)
}

func TestUnknownAPIPath_Returns404JSON(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		rec := newHarness("https://k.example.com").do(method, "/api/v1/x")
		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Equal(t, "not_found", errorCode(t, rec))
	}
}

func TestRoot_ServedBySPAHandler(t *testing.T) {
	rec := newHarness("https://k.example.com").do(http.MethodGet, "/")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "<html>app</html>", rec.Body.String())
	assert.NotEqual(t, "no-store", rec.Header().Get("Cache-Control"), "SPA responses are not marked no-store")
}

func TestSecurityHeaders_OnEveryResponse(t *testing.T) {
	h := newHarness("https://k.example.com")
	for _, target := range []string{"/", "/healthz", "/api/v1/x", "/deep/spa/route"} {
		rec := h.do(http.MethodGet, target)
		assert.Equal(t, wantCSP, rec.Header().Get("Content-Security-Policy"), target)
		assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"), target)
		assert.Equal(t, "no-referrer", rec.Header().Get("Referrer-Policy"), target)
		assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"), target)
		assert.Equal(t, "max-age=31536000", rec.Header().Get("Strict-Transport-Security"), target)
	}
}

func TestSecurityHeaders_HSTSOnlyForHTTPSPublicURL(t *testing.T) {
	rec := newHarness("http://localhost:8080").do(http.MethodGet, "/healthz")
	assert.Empty(t, rec.Header().Get("Strict-Transport-Security"))
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
}

func TestAPIResponses_AreNoStore(t *testing.T) {
	h := newHarness("https://k.example.com")
	assert.Equal(t, "no-store", h.do(http.MethodGet, "/api/v1/x").Header().Get("Cache-Control"))
	assert.NotEqual(t, "no-store", h.do(http.MethodGet, "/").Header().Get("Cache-Control"))
}

func TestRequestID_ReusedWhenValidReplacedOtherwise(t *testing.T) {
	h := newHarness("https://k.example.com")
	assert.Equal(t, "abc-123", h.do(http.MethodGet, "/healthz", "X-Request-Id", "abc-123").Header().Get("X-Request-Id"))
	assert.Equal(t, strings.Repeat("a", 64), h.do(http.MethodGet, "/healthz", "X-Request-Id", strings.Repeat("a", 64)).Header().Get("X-Request-Id"))

	for _, bad := range []string{strings.Repeat("a", 65), "has space", "semi;colon", "new\tline", "ünïcode"} {
		got := h.do(http.MethodGet, "/healthz", "X-Request-Id", bad).Header().Get("X-Request-Id")
		assert.NotEqual(t, bad, got, bad)
		assert.Regexp(t, `^generated-\d+$`, got, bad)
	}
	assert.Regexp(t, `^generated-\d+$`, h.do(http.MethodGet, "/healthz").Header().Get("X-Request-Id"))
}

func TestRequestID_DefaultGeneratorProducesValidIDs(t *testing.T) {
	h := newHarness("https://k.example.com")
	h.deps.NewID = nil
	id := h.do(http.MethodGet, "/healthz").Header().Get("X-Request-Id")
	assert.Regexp(t, `^[A-Za-z0-9-]{1,64}$`, id)
}

func TestPanickingHandler_Returns500JSON_LogsOnce_ServerKeepsServing(t *testing.T) {
	h := newHarness("https://k.example.com")
	calls := 0
	h.deps.Ready = func(context.Context) error {
		calls++
		if calls == 1 {
			panic("kaboom")
		}
		return nil
	}
	rec := h.do(http.MethodGet, "/readyz")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "internal", errorCode(t, rec))
	assert.NotContains(t, rec.Body.String(), "kaboom")
	assert.Equal(t, 1, strings.Count(h.logs.String(), "kaboom"), "the panic is logged once")
	assert.Equal(t, wantCSP, rec.Header().Get("Content-Security-Policy"))

	assert.Equal(t, http.StatusOK, h.do(http.MethodGet, "/readyz").Code, "the server keeps serving")
}

func TestPanicErrAbortHandler_IsNotSwallowed(t *testing.T) {
	h := newHarness("https://k.example.com")
	h.deps.Ready = func(context.Context) error { panic(http.ErrAbortHandler) }
	assert.PanicsWithValue(t, http.ErrAbortHandler, func() { h.do(http.MethodGet, "/readyz") })
}

func TestAccessLog_OneRecordPerRequest_NoQueryString(t *testing.T) {
	h := newHarness("https://k.example.com")
	h.deps.Ready = func(context.Context) error { h.fake.Advance(250 * time.Millisecond); return nil }
	h.do(http.MethodGet, "/readyz?token=supersecret", "X-Request-Id", "rid-1")

	var lines []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(h.logs.String()), "\n") {
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(l), &m))
		lines = append(lines, m)
	}
	require.Len(t, lines, 1)
	l := lines[0]
	assert.Equal(t, "request", l["msg"])
	assert.Equal(t, "GET", l["method"])
	assert.Equal(t, "GET /readyz", l["route"])
	assert.EqualValues(t, 200, l["status"])
	assert.EqualValues(t, 250, l["durationMs"])
	assert.Equal(t, "rid-1", l["requestId"])
	assert.NotContains(t, h.logs.String(), "supersecret")
	assert.NotContains(t, h.logs.String(), "token")
}

func TestAccessLog_UnmatchedAndSPARoutes(t *testing.T) {
	h := newHarness("https://k.example.com")
	h.do(http.MethodGet, "/api/v1/nothing")
	h.do(http.MethodGet, "/some/page")
	out := h.logs.String()
	assert.Contains(t, out, `"route":"/api/"`)
	assert.Contains(t, out, `"route":"/"`)
	assert.NotContains(t, out, "/some/page", "the raw path is not logged, only the route pattern")
}

func TestWriteError_MapsSentinelsAndAPIErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		status     int
		code       string
		wantInBody string
	}{
		{"not found", fmt.Errorf("pod x: %w", apperr.ErrNotFound), 404, "not_found", "pod x"},
		{"invalid", apperr.ErrInvalid, 400, "bad_request", ""},
		{"conflict", apperr.ErrConflict, 409, "conflict", ""},
		{"forbidden", apperr.ErrForbidden, 403, "forbidden", ""},
		{"unauthenticated", apperr.ErrUnauthenticated, 401, "unauthenticated", ""},
		{"busy", apperr.ErrBusy, 503, "server_busy", ""},
		{"too large", apperr.ErrTooLarge, 413, "too_large", ""},
		{"api error", &httpapi.APIError{Status: 409, Code: "key_changed", Message: "The key changed."}, 409, "key_changed", "The key changed."},
		{"wrapped api error", fmt.Errorf("x: %w", &httpapi.APIError{Status: 503, Code: "vault_unreachable", Message: "Vault is down."}), 503, "vault_unreachable", "Vault is down."},
		{"unknown", errors.New("pq: password authentication failed for user admin"), 500, "internal", "internal error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			rec := httptest.NewRecorder()
			httpapi.WriteError(rec, httptest.NewRequest(http.MethodGet, "/", nil), slog.New(slog.NewJSONHandler(&logs, nil)), tt.err)
			assert.Equal(t, tt.status, rec.Code)
			assert.Equal(t, tt.code, errorCode(t, rec))
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
			assert.Contains(t, rec.Body.String(), tt.wantInBody)
			if tt.code == "internal" {
				assert.NotContains(t, rec.Body.String(), "password", "raw error text never reaches the client")
				assert.Contains(t, logs.String(), "password", "but it is logged")
			}
		})
	}
}

type payload struct {
	Name string `json:"name"`
}

func decode(body string) (payload, error) {
	var p payload
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	err := httpapi.DecodeJSON(rec, req, &p)
	return p, err
}

func TestDecodeJSON(t *testing.T) {
	p, err := decode(`{"name":"x"}`)
	require.NoError(t, err)
	assert.Equal(t, "x", p.Name)

	_, err = decode(`{"name":"x","extra":1}`)
	assert.ErrorIs(t, err, apperr.ErrInvalid, "unknown fields are rejected")

	_, err = decode(`{"name":`)
	assert.ErrorIs(t, err, apperr.ErrInvalid)

	_, err = decode(`{"name":"x"} {"name":"y"}`)
	assert.ErrorIs(t, err, apperr.ErrInvalid, "trailing data is rejected")

	_, err = decode(`{"name":"` + strings.Repeat("a", 1<<20) + `"}`)
	assert.ErrorIs(t, err, apperr.ErrTooLarge)

	rec := httptest.NewRecorder()
	httpapi.WriteError(rec, httptest.NewRequest(http.MethodGet, "/", nil), slog.New(slog.DiscardHandler), err)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestWriteJSON_SetsStatusAndContentType(t *testing.T) {
	rec := httptest.NewRecorder()
	httpapi.WriteJSON(rec, http.StatusCreated, map[string]int{"a": 1})
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.JSONEq(t, `{"a":1}`, rec.Body.String())
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
}
