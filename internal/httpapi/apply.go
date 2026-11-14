package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/session"
)

const (
	maxApplyDocuments = 100
	maxApplyBytes     = 1 << 20
	defaultNamespace  = "default"
)

type applyRequest struct {
	Documents []string `json:"documents"`
	DryRun    bool     `json:"dryRun"`
}

type applyResult struct {
	Index     int    `json:"index"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Action    string `json:"action,omitempty"`
	Error     string `json:"error,omitempty"`
}

// apply creates or replaces every object of the YAML documents. A failing document does not stop the others.
func (d Deps) apply(w http.ResponseWriter, r *http.Request, s session.Session, res *kube.Resources) error {
	var req applyRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	total := 0
	for _, doc := range req.Documents {
		total += len(doc)
	}
	if len(req.Documents) > maxApplyDocuments || total > maxApplyBytes {
		return fmt.Errorf("at most %d documents and 1 MiB in total: %w", maxApplyDocuments, ErrTooLarge)
	}
	cluster := r.PathValue("cluster")
	disc, err := d.Kube.Discovery(r.Context(), s, cluster)
	if err != nil {
		return err
	}
	results := []applyResult{}
	for i, text := range req.Documents {
		dec := yaml.NewYAMLOrJSONDecoder(strings.NewReader(text), 4096)
		for {
			var raw map[string]any
			err := dec.Decode(&raw)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				results = append(results, applyResult{Index: i, Error: "The document is not valid YAML: " + err.Error()})
				break
			}
			if len(raw) == 0 {
				continue
			}
			results = append(results, d.applyOne(r, s, res, disc, cluster, i, &unstructured.Unstructured{Object: raw}, req.DryRun))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
	return nil
}

func (d Deps) applyOne(r *http.Request, s session.Session, res *kube.Resources, disc *kube.Discovery, cluster string, index int, obj *unstructured.Unstructured, dry bool) applyResult {
	out := applyResult{Index: index, Kind: obj.GetKind(), Namespace: obj.GetNamespace(), Name: obj.GetName()}
	entry, ok := disc.FindKind(obj.GroupVersionKind().GroupVersion(), obj.GetKind())
	if !ok {
		out.Error = fmt.Sprintf("The cluster does not serve %s %s.", obj.GetAPIVersion(), obj.GetKind())
		return out
	}
	gvr := schema.GroupVersionResource{Group: entry.Group, Version: entry.Version, Resource: entry.Resource}
	t := target{cluster: cluster, gvr: gvr}
	if entry.Namespaced && obj.GetNamespace() == "" {
		obj.SetNamespace(defaultNamespace)
	}
	out.Namespace = obj.GetNamespace()
	if err := checkObject(disc, t, obj, true); err != nil {
		out.Error = err.Error()
		return out
	}
	ns := obj.GetNamespace()
	existing, err := res.Get(r.Context(), s, cluster, gvr, ns, obj.GetName())
	verb, action := audit.VerbCreate, "created"
	switch {
	case err == nil:
		verb, action = audit.VerbUpdate, "updated"
		obj.SetResourceVersion(existing.GetResourceVersion())
	case !errors.Is(err, ErrNotFound):
		out.Error = err.Error()
		return out
	}
	run := func() error {
		if verb == audit.VerbCreate {
			_, err := res.Create(r.Context(), s, cluster, gvr, ns, obj, dry)
			return err
		}
		_, _, err := res.Update(r.Context(), s, cluster, gvr, ns, obj, dry)
		return err
	}
	if dry {
		err = run()
	} else {
		err = d.audited(r.Context(), d.event(t, ns, verb, obj.GetName()), run)
	}
	if err != nil {
		_, _, msg, _ := classify(err)
		out.Error = msg
		return out
	}
	out.Action = action
	return out
}
