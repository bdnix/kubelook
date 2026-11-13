package session_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/session"
)

func failWith(w http.ResponseWriter, _ *http.Request, err error) {
	status, code := http.StatusInternalServerError, "internal"
	var api *apperr.APIError
	switch {
	case errorsAs(err, &api):
		status, code = api.Status, api.Code
	case errorsIs(err, apperr.ErrUnauthenticated):
		status, code = http.StatusUnauthorized, "unauthenticated"
	}
	w.WriteHeader(status)
	_, _ = w.Write([]byte(code))
}

func handlerSeenSession() (http.Handler, *session.Session) {
	var seen session.Session
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s, ok := session.FromContext(r.Context()); ok {
			seen = s
		}
		w.WriteHeader(http.StatusNoContent)
	}), &seen
}

func serve(h http.Handler, method, target, sessionID, csrf string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	if sessionID != "" {
		r.AddCookie(&http.Cookie{Name: "kubelook_session", Value: sessionID})
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestRequire_APIWithoutValidSession_401(t *testing.T) {
	e := newEnv(t, false)
	next, _ := handlerSeenSession()
	h := e.m.Require(failWith)(next)

	rec := serve(h, http.MethodGet, "/api/v1/clusters", "", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, "unauthenticated", rec.Body.String())
	assert.Equal(t, http.StatusUnauthorized, serve(h, http.MethodGet, "/api/v1/clusters", "bogus", "").Code)
}

func TestRequire_ValidSession_HandlerSeesItInContext(t *testing.T) {
	e := newEnv(t, false)
	id, s, _ := e.m.Create(t.Context(), e.userID)
	next, seen := handlerSeenSession()
	h := e.m.Require(failWith)(next)

	assert.Equal(t, http.StatusNoContent, serve(h, http.MethodGet, "/api/v1/clusters", id, "").Code)
	assert.Equal(t, s, *seen)
}

func TestRequire_PublicRoutes_PassWithoutSession(t *testing.T) {
	e := newEnv(t, false)
	next, _ := handlerSeenSession()
	h := e.m.Require(failWith)(next)

	for _, c := range []struct{ method, target string }{
		{"GET", "/healthz"}, {"GET", "/readyz"}, {"GET", "/metrics"},
		{"GET", "/auth/google/start"}, {"GET", "/auth/github/callback"},
		{"GET", "/api/v1/auth/providers"},
		{"GET", "/"}, {"GET", "/assets/app.js"}, {"GET", "/c/v%3Aprod"},
	} {
		assert.Equal(t, http.StatusNoContent, serve(h, c.method, c.target, "", "").Code, c.target)
	}
	assert.Equal(t, http.StatusUnauthorized, serve(h, "POST", "/api/v1/auth/providers", "", "").Code, "only GET providers is public")
	assert.Equal(t, http.StatusUnauthorized, serve(h, "GET", "/api/v1/me", "", "").Code)
}

func TestCSRF_NonSafeMethodsNeedMatchingToken(t *testing.T) {
	e := newEnv(t, false)
	id, s, _ := e.m.Create(t.Context(), e.userID)
	next, _ := handlerSeenSession()
	h := e.m.Require(failWith)(session.CSRF(failWith)(next))

	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		rec := serve(h, method, "/api/v1/x", id, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, method)
		assert.Equal(t, "csrf", rec.Body.String(), method)

		assert.Equal(t, http.StatusForbidden, serve(h, method, "/api/v1/x", id, "wrong").Code, method)
		assert.Equal(t, http.StatusForbidden, serve(h, method, "/api/v1/x", id, s.CSRFToken[:10]).Code, method)
		assert.Equal(t, http.StatusNoContent, serve(h, method, "/api/v1/x", id, s.CSRFToken).Code, method)
	}
	for _, method := range []string{"GET", "HEAD", "OPTIONS"} {
		assert.Equal(t, http.StatusNoContent, serve(h, method, "/api/v1/x", id, "").Code, method)
	}
}

func TestCSRF_NoSessionInContext_PassesThrough(t *testing.T) {
	next, _ := handlerSeenSession()
	h := session.CSRF(failWith)(next)
	require.Equal(t, http.StatusNoContent, serve(h, "POST", "/auth/x", "", "").Code)
}
