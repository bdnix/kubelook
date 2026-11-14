package httpapi

import (
	"net/http"

	"github.com/bdnix/kubelook/internal/execs"
	"github.com/bdnix/kubelook/internal/session"
)

type debugPodRequest struct {
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Target    string `json:"target"`
	Profile   string `json:"profile"`
}

type debugNodeRequest struct {
	Node string `json:"node"`
}

// registerDebug mounts the debug toolkit. Without a Debugger nothing is mounted.
func (d Deps) registerDebug(mux *http.ServeMux) {
	if d.Debugger == nil {
		return
	}
	mux.HandleFunc("POST /api/v1/clusters/{cluster}/debug/pod", func(w http.ResponseWriter, r *http.Request) {
		s, ok := session.FromContext(r.Context())
		if !ok {
			writeError(w, r, d.Log, ErrUnauthenticated)
			return
		}
		var req debugPodRequest
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, r, d.Log, err)
			return
		}
		if req.Namespace == "" || req.Pod == "" {
			writeError(w, r, d.Log, ErrInvalid)
			return
		}
		res, err := d.Debugger.AddContainer(r.Context(), s, r.PathValue("cluster"), execs.PodRequest(req))
		if err != nil {
			writeError(w, r, d.Log, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	})
	mux.HandleFunc("POST /api/v1/clusters/{cluster}/debug/node", func(w http.ResponseWriter, r *http.Request) {
		s, ok := session.FromContext(r.Context())
		if !ok {
			writeError(w, r, d.Log, ErrUnauthenticated)
			return
		}
		var req debugNodeRequest
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, r, d.Log, err)
			return
		}
		res, err := d.Debugger.CreateNodeShell(r.Context(), s, r.PathValue("cluster"), req.Node)
		if err != nil {
			writeError(w, r, d.Log, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	})
	mux.HandleFunc("DELETE /api/v1/clusters/{cluster}/debug/node/{pod}", func(w http.ResponseWriter, r *http.Request) {
		s, ok := session.FromContext(r.Context())
		if !ok {
			writeError(w, r, d.Log, ErrUnauthenticated)
			return
		}
		if err := d.Debugger.DeleteNodeShell(r.Context(), s, r.PathValue("cluster"), r.PathValue("pod")); err != nil {
			writeError(w, r, d.Log, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
}
