package httpapi

import (
	"fmt"
	"net/http"

	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/bdnix/kubelook/internal/actions"
	"github.com/bdnix/kubelook/internal/session"
)

// registerBulk mounts the endpoints that start durable node and bulk jobs. They need the job runner and the
// audit recorder; without either nothing is mounted.
func (d Deps) registerBulk(mux *http.ServeMux) {
	if d.Jobs == nil || d.Audit == nil {
		return
	}
	mux.HandleFunc("POST /api/v1/clusters/{cluster}/bulk", d.startBulk)
	mux.HandleFunc("POST /api/v1/clusters/{cluster}/nodes/{node}/drain", d.startDrain)
}

func (d Deps) startBulk(w http.ResponseWriter, r *http.Request) {
	s, ok := session.FromContext(r.Context())
	if !ok {
		writeError(w, r, d.Log, ErrUnauthenticated)
		return
	}
	var req actions.BulkRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	if err := actions.ValidateBulk(req); err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	cluster := r.PathValue("cluster")
	if _, err := d.Kube.Discovery(r.Context(), s, cluster); err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	noun := "objects"
	if len(req.Targets) == 1 {
		noun = "object"
	}
	target := map[string]any{"name": fmt.Sprintf("%s %d %s", req.Action, len(req.Targets), noun), "action": req.Action, "count": len(req.Targets)}
	id, err := d.Jobs.Submit(r.Context(), s, cluster, "bulk", target, req)
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"jobId": id})
}

func (d Deps) startDrain(w http.ResponseWriter, r *http.Request) {
	s, ok := session.FromContext(r.Context())
	if !ok {
		writeError(w, r, d.Log, ErrUnauthenticated)
		return
	}
	cluster, node := r.PathValue("cluster"), r.PathValue("node")
	if len(validation.IsDNS1123Subdomain(node)) > 0 {
		writeError(w, r, d.Log, fmt.Errorf("%q is not a node name: %w", node, ErrInvalid))
		return
	}
	if _, err := d.Kube.Discovery(r.Context(), s, cluster); err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	id, err := d.Jobs.Submit(r.Context(), s, cluster, "drain", map[string]string{"name": node}, map[string]string{"node": node})
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"jobId": id})
}
