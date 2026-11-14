package httpapi

import (
	"net/http"

	"github.com/bdnix/kubelook/internal/actions"
	"github.com/bdnix/kubelook/internal/session"
)

// registerActions mounts POST /api/v1/clusters/{cluster}/actions. Without an audit recorder nothing is mounted.
func (d Deps) registerActions(mux *http.ServeMux) {
	if d.Audit == nil {
		return
	}
	svc := actions.New(actions.Deps{Clients: d.Kube, Clock: d.Clock, Audit: d.Audit})
	mux.HandleFunc("POST /api/v1/clusters/{cluster}/actions", func(w http.ResponseWriter, r *http.Request) {
		s, ok := session.FromContext(r.Context())
		if !ok {
			writeError(w, r, d.Log, ErrUnauthenticated)
			return
		}
		var req actions.Request
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, r, d.Log, err)
			return
		}
		if err := svc.Do(r.Context(), s, r.PathValue("cluster"), req); err != nil {
			writeError(w, r, d.Log, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
}
