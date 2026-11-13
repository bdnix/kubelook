// Package httpapi is the HTTP layer: routing, middleware, the error format and the SPA.
package httpapi

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/config"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/store"
	"github.com/bdnix/kubelook/internal/webui"
)

// Deps are the dependencies of the HTTP handler. Later steps add fields;
// a nil field means "feature not wired yet".
type Deps struct {
	Config *config.Config
	Clock  clock.Clock
	Log    *slog.Logger
	WebFS  fs.FS
	Ready  func(context.Context) error
	NewID  func() string // request IDs; nil → store.NewID

	Sessions *session.Manager // nil → no session or CSRF checks (not wired yet)
}

// New builds the handler.
func New(d Deps) http.Handler {
	if d.NewID == nil {
		d.NewID = store.NewID
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if d.Ready != nil {
			if err := d.Ready(r.Context()); err != nil {
				d.Log.Error("not ready", "error", err.Error())
				writeError(w, r, d.Log, &APIError{Status: http.StatusServiceUnavailable, Code: "internal", Message: "not ready"})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	// API routes are registered above the SPA catch-all; unknown API paths get a JSON 404.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, d.Log, fmt.Errorf("no such API route: %w", ErrNotFound))
	})
	mux.Handle("/", webui.Handler(d.WebFS))

	mws := []middleware{
		recoverPanics(d.Log),
		requestID(d.NewID),
		accessLog(d.Log, d.Clock),
		securityHeaders(d.Config.CookieSecure()),
	}
	if d.Sessions != nil {
		fail := func(w http.ResponseWriter, r *http.Request, err error) { writeError(w, r, d.Log, err) }
		mws = append(mws, d.Sessions.Require(fail), session.CSRF(fail))
	}
	return chain(recordRoute(mux), mws...)
}
