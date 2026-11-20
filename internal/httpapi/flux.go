package httpapi

import (
	"net/http"

	"github.com/bdnix/kubelook/internal/flux"
	"github.com/bdnix/kubelook/internal/session"
)

// registerFlux mounts the Flux inventory endpoint; without the inventory reader nothing is mounted.
func (d Deps) registerFlux(mux *http.ServeMux) {
	if d.Flux == nil {
		return
	}
	mux.HandleFunc("GET /api/v1/clusters/{cluster}/flux/inventory", d.fluxInventory)
}

// fluxInventory answers the objects a Kustomization or a HelmRelease manages.
func (d Deps) fluxInventory(w http.ResponseWriter, r *http.Request) {
	s, ok := session.FromContext(r.Context())
	if !ok {
		writeError(w, r, d.Log, ErrUnauthenticated)
		return
	}
	q := r.URL.Query()
	res, err := d.Flux.Entries(r.Context(), s, r.PathValue("cluster"), flux.ObjectRef{
		Group: q.Get("group"), Kind: q.Get("kind"), Namespace: q.Get("namespace"), Name: q.Get("name"),
	})
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
