package httpapi

import (
	"fmt"
	"net/http"

	"github.com/bdnix/kubelook/internal/session"
)

type openRequest struct {
	Role string `json:"role"`
}

type ackRequest struct {
	Labels []string `json:"labels"`
}

// registerOpen mounts the endpoints that open clusters and hand out pending credential copies.
func (d Deps) registerOpen(mux *http.ServeMux) {
	if d.Opener == nil {
		return
	}
	mux.HandleFunc("POST /api/v1/clusters/{cluster}/open", d.openCluster)
	mux.HandleFunc("GET /api/v1/credentials/pending", d.pendingCredentials)
	mux.HandleFunc("POST /api/v1/credentials/pending/ack", d.ackPendingCredentials)
}

func (d Deps) openCluster(w http.ResponseWriter, r *http.Request) {
	s, ok := session.FromContext(r.Context())
	if !ok {
		writeError(w, r, d.Log, ErrUnauthenticated)
		return
	}
	var req openRequest
	if r.ContentLength != 0 {
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, r, d.Log, err)
			return
		}
	}
	st, err := d.Opener.Open(r.Context(), s, r.PathValue("cluster"), req.Role)
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (d Deps) pendingCredentials(w http.ResponseWriter, r *http.Request) {
	s, ok := session.FromContext(r.Context())
	if !ok {
		writeError(w, r, d.Log, ErrUnauthenticated)
		return
	}
	items, err := d.Opener.Pending(r.Context(), s)
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (d Deps) ackPendingCredentials(w http.ResponseWriter, r *http.Request) {
	s, ok := session.FromContext(r.Context())
	if !ok {
		writeError(w, r, d.Log, ErrUnauthenticated)
		return
	}
	var req ackRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	if len(req.Labels) > 100 {
		writeError(w, r, d.Log, fmt.Errorf("too many labels: %w", ErrInvalid))
		return
	}
	if err := d.Opener.AckPending(r.Context(), s, req.Labels); err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
