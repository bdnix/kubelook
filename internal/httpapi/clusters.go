package httpapi

import (
	"net/http"

	"github.com/bdnix/kubelook/internal/session"
)

// listClusters answers GET /api/v1/clusters with the session's sources and their clusters.
func (d Deps) listClusters(w http.ResponseWriter, r *http.Request) {
	s, ok := session.FromContext(r.Context())
	if !ok {
		writeError(w, r, d.Log, ErrUnauthenticated)
		return
	}
	sources, err := d.Clusters.List(r.Context(), s)
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": sources})
}
