package session

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/bdnix/kubelook/internal/apperr"
)

// ErrorWriter turns an error into an HTTP response (httpapi supplies it).
type ErrorWriter func(w http.ResponseWriter, r *http.Request, err error)

type ctxKey struct{}

// FromContext returns the session that Require stored in the request context.
func FromContext(ctx context.Context) (Session, bool) {
	s, ok := ctx.Value(ctxKey{}).(Session)
	return s, ok
}

// isPublic reports whether a request may proceed without a session: everything outside
// /api/ (health, metrics, /auth/…, static files) plus GET /api/v1/auth/providers.
func isPublic(r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		return true
	}
	return r.Method == http.MethodGet && r.URL.Path == "/api/v1/auth/providers"
}

// Require rejects API requests that carry no valid session and passes the session to the handler.
func (m *Manager) Require(fail ErrorWriter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isPublic(r) {
				next.ServeHTTP(w, r)
				return
			}
			s, err := m.Load(r.Context(), r)
			if err != nil {
				fail(w, r, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, s)))
		})
	}
}

// CSRF requires a matching X-CSRF-Token on every request that is not GET, HEAD or OPTIONS
// and belongs to a session. Requests without a session (public routes) are not checked.
func CSRF(fail ErrorWriter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			s, ok := FromContext(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(s.CSRFToken)) != 1 {
				fail(w, r, &apperr.APIError{Status: http.StatusForbidden, Code: "csrf", Message: "Missing or invalid CSRF token."})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
