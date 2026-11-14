package httpapi

import (
	"net/http"

	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/session"
)

func (d Deps) registerOpenAPI(mux *http.ServeMux, api *kube.OpenAPI) {
	mux.HandleFunc("GET /api/v1/clusters/{cluster}/openapi/{group}/{version}", func(w http.ResponseWriter, r *http.Request) {
		s, ok := session.FromContext(r.Context())
		if !ok {
			writeError(w, r, d.Log, ErrUnauthenticated)
			return
		}
		body, err := api.Get(r.Context(), s, r.PathValue("cluster"), r.PathValue("group"), r.PathValue("version"))
		if err != nil {
			writeError(w, r, d.Log, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body) //nolint:gosec // G705: the body is the cluster's JSON, served as application/json with nosniff
	})
}
