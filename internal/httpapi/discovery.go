package httpapi

import (
	"net/http"

	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/session"
)

// registerKube mounts the endpoints that read from a cluster; without Kube nothing is mounted.
func (d Deps) registerKube(mux *http.ServeMux) {
	if d.Kube == nil {
		return
	}
	mux.HandleFunc("GET /api/v1/clusters/{cluster}/discovery", d.discovery)
	d.registerResources(mux, kube.NewResources(d.Kube))
	d.registerAccess(mux, kube.NewAccess(d.Kube, d.Clock, d.sessionRegistry()))
	d.registerOpenAPI(mux, kube.NewOpenAPI(d.Kube, d.Clock))
	d.registerActions(mux)
	d.registerSecrets(mux, kube.NewResources(d.Kube))
}

// discovery answers what the cluster serves.
func (d Deps) discovery(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, disc)
}

func (d Deps) sessionRegistry() *session.Registry {
	if d.Sessions == nil {
		return nil
	}
	return d.Sessions.Registry()
}
