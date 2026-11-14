package httpapi

import "net/http"

// registerWS mounts the WebSocket endpoint; without a Mux nothing is mounted.
func (d Deps) registerWS(mux *http.ServeMux) {
	if d.WS == nil {
		return
	}
	mux.Handle("GET /api/v1/ws", d.WS)
}
