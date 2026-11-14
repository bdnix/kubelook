package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"strings"
	"sync/atomic"

	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/cluster"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/store"
)

const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; font-src 'self' data:; connect-src 'self'; worker-src 'self' blob:; " +
	"frame-ancestors 'none'; base-uri 'self'; form-action 'self'"

var requestIDRE = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

type middleware func(http.Handler) http.Handler

// chain composes middleware so the first one is outermost.
func chain(h http.Handler, mws ...middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// statusWriter records the status code. Unwrap lets http.ResponseController reach
// Flush and Hijack on the underlying writer (streams and WebSockets).
type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusWriter) WriteHeader(code int) {
	if !s.wrote {
		s.status, s.wrote = code, true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if !s.wrote {
		s.status, s.wrote = http.StatusOK, true
	}
	return s.ResponseWriter.Write(b)
}

func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// recoverPanics turns a panic in a handler into a 500 and keeps the server serving.
func recoverPanics(log *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sw := &statusWriter{ResponseWriter: w}
			defer func() {
				p := recover()
				if p == nil {
					return
				}
				if p == http.ErrAbortHandler { //nolint:errorlint // the net/http contract compares the value itself
					panic(p)
				}
				log.Error("panic in handler", "panic", p, "stack", string(debug.Stack()), "requestId", w.Header().Get("X-Request-Id"))
				if !sw.wrote {
					writeJSON(w, http.StatusInternalServerError, errorBody{Error: errorDetail{Code: "internal", Message: "internal error"}})
				}
			}()
			next.ServeHTTP(sw, r)
		})
	}
}

// requestID reuses a well-formed incoming X-Request-Id and otherwise generates one.
func requestID(newID func() string) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-Id")
			if !requestIDRE.MatchString(id) {
				id = newID()
			}
			w.Header().Set("X-Request-Id", id)
			next.ServeHTTP(w, r)
		})
	}
}

// routeHolder carries the matched route pattern from the mux back out to the access log.
type routeHolder struct{ pattern atomic.Pointer[string] }

type routeKey struct{}

// recordRoute wraps the mux and stores the matched pattern for the access log.
func recordRoute(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		if h, ok := r.Context().Value(routeKey{}).(*routeHolder); ok {
			p := r.Pattern
			h.pattern.Store(&p)
		}
	})
}

// accessLog writes one record per request: method, route pattern, status, duration and
// request ID. Neither the path nor the query string is logged.
func accessLog(log *slog.Logger, c clock.Clock) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := c.Now()
			holder := &routeHolder{}
			sw := &statusWriter{ResponseWriter: w}
			next.ServeHTTP(sw, r.WithContext(context.WithValue(r.Context(), routeKey{}, holder)))
			route := ""
			if p := holder.pattern.Load(); p != nil {
				route = *p
			}
			status := sw.status
			if !sw.wrote {
				status = http.StatusOK
			}
			log.Info("request",
				"method", r.Method, "route", route, "status", status,
				"durationMs", c.Now().Sub(start).Milliseconds(), "requestId", w.Header().Get("X-Request-Id"))
		})
	}
}

// securityHeaders sets the headers every response carries; API responses are never cached.
func securityHeaders(hsts bool) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", contentSecurityPolicy)
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("X-Frame-Options", "DENY")
			if hsts {
				h.Set("Strict-Transport-Security", "max-age=31536000")
			}
			if strings.HasPrefix(r.URL.Path, "/api/") {
				h.Set("Cache-Control", "no-store")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// auditIdentity attaches the signed-in user's identity to the request context, so every
// audit record written while handling the request names who acted. A request whose user row
// cannot be read carries no identity; the handler decides what that means.
//
// When the session holds a Vault token (creds != nil), the Vault entity ID and display name are added.
func auditIdentity(users *store.Users, creds *cluster.Credentials) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s, ok := session.FromContext(r.Context()); ok {
				if u, err := users.Get(r.Context(), s.UserID); err == nil {
					id := audit.UserIdentity(u)
					if creds != nil {
						id.VaultEntityID, id.VaultDisplayName = creds.VaultIdentity(r.Context(), s)
					}
					r = r.WithContext(audit.WithIdentity(r.Context(), id))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
