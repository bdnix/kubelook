package httpapi

import (
	"fmt"
	"net/http"
	"strconv"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/session"
)

// target is what a resource request addresses.
type target struct {
	cluster string
	gvr     schema.GroupVersionResource
	ns      string
	name    string
}

// parseTarget reads {cluster}, {group}, {version}, {resource}, {name} from the path and
// "namespace" from the query. The group "core" is the empty group. Path values arrive
// percent-decoded, so a cluster ID such as "k:prod/ctx" must be sent as "k%3Aprod%2Fctx".
func parseTarget(r *http.Request) (target, error) {
	t := target{
		cluster: r.PathValue("cluster"),
		gvr:     schema.GroupVersionResource{Group: r.PathValue("group"), Version: r.PathValue("version"), Resource: r.PathValue("resource")},
		ns:      r.URL.Query().Get("namespace"),
		name:    r.PathValue("name"),
	}
	if t.gvr.Group == "core" {
		t.gvr.Group = ""
	}
	if t.cluster == "" || t.gvr.Version == "" || t.gvr.Resource == "" {
		return target{}, fmt.Errorf("cluster, version and resource are required: %w", ErrInvalid)
	}
	return t, nil
}

func (d Deps) registerResources(mux *http.ServeMux, res *kube.Resources) {
	const base = "/api/v1/clusters/{cluster}/resources/{group}/{version}/{resource}"
	mux.HandleFunc("GET "+base, func(w http.ResponseWriter, r *http.Request) { d.listResources(w, r, res) })
	mux.HandleFunc("GET "+base+"/{name}", func(w http.ResponseWriter, r *http.Request) { d.getResource(w, r, res) })
}

func (d Deps) listResources(w http.ResponseWriter, r *http.Request, res *kube.Resources) {
	s, ok := session.FromContext(r.Context())
	if !ok {
		writeError(w, r, d.Log, ErrUnauthenticated)
		return
	}
	t, err := parseTarget(r)
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	q := r.URL.Query()
	opts := kube.ListOptions{LabelSelector: q.Get("labelSelector"), FieldSelector: q.Get("fieldSelector"), Continue: q.Get("continue")}
	if v := q.Get("limit"); v != "" {
		if opts.Limit, err = strconv.ParseInt(v, 10, 64); err != nil || opts.Limit < 0 {
			writeError(w, r, d.Log, fmt.Errorf("limit must be a positive number: %w", ErrInvalid))
			return
		}
	}
	list, err := res.List(r.Context(), s, t.cluster, t.gvr, t.ns, opts)
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	items := make([]map[string]any, len(list.Items))
	for i := range list.Items {
		items[i] = list.Items[i].Object
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "resourceVersion": list.GetResourceVersion(), "continue": list.GetContinue()})
}

func (d Deps) getResource(w http.ResponseWriter, r *http.Request, res *kube.Resources) {
	s, ok := session.FromContext(r.Context())
	if !ok {
		writeError(w, r, d.Log, ErrUnauthenticated)
		return
	}
	t, err := parseTarget(r)
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	obj, err := res.Get(r.Context(), s, t.cluster, t.gvr, t.ns, t.name)
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, obj.Object)
}
