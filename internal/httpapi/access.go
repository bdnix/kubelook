package httpapi

import (
	"fmt"
	"net/http"

	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/session"
)

type accessRequest struct {
	Checks []kube.Check `json:"checks"`
}

func (d Deps) registerAccess(mux *http.ServeMux, access *kube.Access) {
	mux.HandleFunc("POST /api/v1/clusters/{cluster}/access", func(w http.ResponseWriter, r *http.Request) { d.access(w, r, access) })
}

// access answers what the user may do, for the UI. It is not an authorization layer.
func (d Deps) access(w http.ResponseWriter, r *http.Request, access *kube.Access) {
	s, ok := session.FromContext(r.Context())
	if !ok {
		writeError(w, r, d.Log, ErrUnauthenticated)
		return
	}
	var req accessRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	if len(req.Checks) > kube.MaxChecks {
		writeError(w, r, d.Log, fmt.Errorf("at most %d checks per request: %w", kube.MaxChecks, ErrInvalid))
		return
	}
	for i := range req.Checks {
		c := &req.Checks[i]
		if c.Verb == "" || c.Resource == "" {
			writeError(w, r, d.Log, fmt.Errorf("every check needs a verb and a resource: %w", ErrInvalid))
			return
		}
		if c.Group == "core" {
			c.Group = ""
		}
	}
	results, err := access.CanAll(r.Context(), s, r.PathValue("cluster"), req.Checks)
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}
