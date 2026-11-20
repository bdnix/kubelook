package httpapi

import (
	"net/http"

	"github.com/bdnix/kubelook/internal/modules"
	"github.com/bdnix/kubelook/internal/session"
)

// registerModules mounts the endpoint that tells which ecosystem modules a cluster supports.
func (d Deps) registerModules(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/clusters/{cluster}/modules", d.modules)
}

func (d Deps) modules(w http.ResponseWriter, r *http.Request) {
	s, ok := session.FromContext(r.Context())
	if !ok {
		writeError(w, r, d.Log, ErrUnauthenticated)
		return
	}
	disc, err := d.Kube.Discovery(r.Context(), s, r.PathValue("cluster"))
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"modules": modules.Detect(disc, d.Config.Modules)})
}
