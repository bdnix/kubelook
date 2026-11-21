package httpapi

import (
	"net/http"
	"time"
)

// Telemetry is what the HTTP layer needs of the metrics.
type Telemetry interface {
	Handler() http.Handler
	// ObserveHTTP records one request. route is the matched pattern, never the path.
	ObserveHTTP(route, method string, status int, d time.Duration)
}

// registerTelemetry serves GET /metrics. It is outside /api/, so it needs no session; the deployer
// keeps it off the public internet (docs/deployment.md).
func (d Deps) registerTelemetry(mux *http.ServeMux) {
	if d.Telemetry != nil {
		mux.Handle("GET /metrics", d.Telemetry.Handler())
	}
}
