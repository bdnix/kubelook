package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/httpapi"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/store/storetest"
	"github.com/bdnix/kubelook/internal/testutil"
)

func withSessions(t *testing.T) (*harness, string, string) {
	t.Helper()
	h := newHarness("http://localhost:8080")
	db := storetest.New(t)
	h.db = db
	user, err := store.NewUsers(db, h.fake, func() string { return "u1" }).Upsert(t.Context(), "google", "s", "e", "n")
	require.NoError(t, err)
	h.deps.Sessions = session.NewManager(session.Deps{
		Store: store.NewSessions(db), Clock: h.fake, Rand: testutil.Rand("seed"), Log: h.deps.Log,
		IdleTimeout: 30 * time.Minute, MaxLifetime: 12 * time.Hour,
	})
	id, s, err := h.deps.Sessions.Create(t.Context(), user.ID)
	require.NoError(t, err)
	return h, id, s.CSRFToken
}

func (h *harness) doWithSession(method, target, id, csrf string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(""))
	if id != "" {
		req.AddCookie(&http.Cookie{Name: "kubelook_session", Value: id})
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	rec := httptest.NewRecorder()
	httpapi.New(h.deps).ServeHTTP(rec, req)
	return rec
}

func TestSessionMiddleware_APIRequiresSession_PublicRoutesDoNot(t *testing.T) {
	h, id, _ := withSessions(t)

	rec := h.doWithSession(http.MethodGet, "/api/v1/x", "", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, "unauthenticated", errorCode(t, rec))
	assert.Equal(t, wantCSP, rec.Header().Get("Content-Security-Policy"), "security headers wrap the session check")

	// authenticated: reaches the mux, which has no such route
	assert.Equal(t, http.StatusNotFound, h.doWithSession(http.MethodGet, "/api/v1/x", id, "").Code)

	for _, target := range []string{"/healthz", "/readyz", "/"} {
		assert.Equal(t, http.StatusOK, h.doWithSession(http.MethodGet, target, "", "").Code, target)
	}
}

func TestSessionMiddleware_CSRFEnforcedOnWrites(t *testing.T) {
	h, id, csrf := withSessions(t)

	rec := h.doWithSession(http.MethodPost, "/api/v1/x", id, "")
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "csrf", errorCode(t, rec))

	assert.Equal(t, http.StatusNotFound, h.doWithSession(http.MethodPost, "/api/v1/x", id, csrf).Code)
}
