package httpapi

import "net/http"

// Telemetry is the metrics the HTTP layer needs: the scrape handler and the per-request observer.
type Telemetry interface {
	RequestObserver
	Handler() http.Handler
}

// registerTelemetry mounts GET /metrics, which sits outside /api/ and so needs no session, and
// returns the observer that counts requests (nil when telemetry is not wired).
func (d Deps) registerTelemetry(mux *http.ServeMux) RequestObserver {
	if d.Telemetry == nil {
		return nil
	}
	mux.Handle("GET /metrics", d.Telemetry.Handler())
	return d.Telemetry
}
