package httpapi

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/session"
)

const (
	mergePatch     = "application/merge-patch+json"
	strategicPatch = "application/strategic-merge-patch+json"
)

// registerWrites mounts create, replace, patch, delete and apply. Without an audit recorder nothing is mounted.
func (d Deps) registerWrites(mux *http.ServeMux, res *kube.Resources, base string) {
	if d.Audit == nil {
		return
	}
	mux.HandleFunc("POST "+base, d.mutate(func(w http.ResponseWriter, r *http.Request, s session.Session, t target) error {
		return d.createResource(w, r, s, t, res)
	}))
	mux.HandleFunc("PUT "+base+"/{name}", d.mutate(func(w http.ResponseWriter, r *http.Request, s session.Session, t target) error {
		return d.replaceResource(w, r, s, t, res)
	}))
	mux.HandleFunc("PATCH "+base+"/{name}", d.mutate(func(w http.ResponseWriter, r *http.Request, s session.Session, t target) error {
		return d.patchResource(w, r, s, t, res)
	}))
	mux.HandleFunc("DELETE "+base+"/{name}", d.mutate(func(w http.ResponseWriter, r *http.Request, s session.Session, t target) error {
		return d.deleteResource(w, r, s, t, res)
	}))
	mux.HandleFunc("POST /api/v1/clusters/{cluster}/apply", func(w http.ResponseWriter, r *http.Request) {
		s, ok := session.FromContext(r.Context())
		if !ok {
			writeError(w, r, d.Log, ErrUnauthenticated)
			return
		}
		if err := d.apply(w, r, s, res); err != nil {
			writeError(w, r, d.Log, err)
		}
	})
}

// mutate resolves the session and the target; the handler's error becomes the response.
func (d Deps) mutate(fn func(http.ResponseWriter, *http.Request, session.Session, target) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, ok := session.FromContext(r.Context())
		if !ok {
			writeError(w, r, d.Log, ErrUnauthenticated)
			return
		}
		t, err := parseTarget(r)
		if err == nil {
			err = fn(w, r, s, t)
		}
		if err != nil {
			writeError(w, r, d.Log, err)
		}
	}
}

func isDryRun(r *http.Request) bool { return r.URL.Query().Get("dryRun") == "true" }

// readBody reads at most maxBodyBytes.
func readBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, fmt.Errorf("request body exceeds %d bytes: %w", maxBodyBytes, ErrTooLarge)
		}
		return nil, fmt.Errorf("reading the request body: %w", ErrInvalid)
	}
	return b, nil
}

// readObject decodes the body as one Kubernetes object and checks it against the path and discovery.
func (d Deps) readObject(w http.ResponseWriter, r *http.Request, s session.Session, t target, needName bool) (*unstructured.Unstructured, error) {
	b, err := readBody(w, r)
	if err != nil {
		return nil, err
	}
	obj := &unstructured.Unstructured{}
	if err := obj.UnmarshalJSON(b); err != nil {
		return nil, fmt.Errorf("the body is not a valid JSON object with apiVersion and kind: %w", ErrInvalid)
	}
	disc, err := d.Kube.Discovery(r.Context(), s, t.cluster)
	if err != nil {
		return nil, err
	}
	if err := checkObject(disc, t, obj, needName); err != nil {
		return nil, err
	}
	return obj, nil
}

// checkObject makes sure the object belongs where the path says and fills in the namespace.
func checkObject(disc *kube.Discovery, t target, obj *unstructured.Unstructured, needName bool) error {
	entry, ok := disc.Find(t.gvr)
	if !ok {
		return fmt.Errorf("the cluster does not serve %s: %w", t.gvr.String(), ErrNotFound)
	}
	if obj.GetKind() != entry.Kind || obj.GroupVersionKind().GroupVersion() != t.gvr.GroupVersion() {
		return fmt.Errorf("the object's apiVersion and kind must be %s %s: %w", t.gvr.GroupVersion().String(), entry.Kind, ErrInvalid)
	}
	if entry.Namespaced {
		ns := t.ns
		if ns == "" {
			ns = obj.GetNamespace()
		}
		if ns == "" || (obj.GetNamespace() != "" && obj.GetNamespace() != ns) {
			return fmt.Errorf("metadata.namespace must be set and match the namespace in the path: %w", ErrInvalid)
		}
		obj.SetNamespace(ns)
	} else if t.ns != "" || obj.GetNamespace() != "" {
		return fmt.Errorf("%s is not namespaced; remove the namespace: %w", entry.Kind, ErrInvalid)
	}
	switch {
	case t.name != "" && obj.GetName() != t.name:
		return fmt.Errorf("metadata.name must match the name in the path: %w", ErrInvalid)
	case needName && obj.GetName() == "" && obj.GetGenerateName() == "":
		return fmt.Errorf("metadata.name is required: %w", ErrInvalid)
	}
	return nil
}

func (d Deps) event(t target, ns, verb, name string) audit.Event {
	return audit.Event{Cluster: t.cluster, Namespace: ns, Verb: verb, ResourceKind: t.gvr.Resource, ResourceName: name,
		Details: map[string]any{"group": t.gvr.Group, "version": t.gvr.Version}}
}

func (d Deps) createResource(w http.ResponseWriter, r *http.Request, s session.Session, t target, res *kube.Resources) error {
	obj, err := d.readObject(w, r, s, t, true)
	if err != nil {
		return err
	}
	dry := isDryRun(r)
	var out *unstructured.Unstructured
	run := func() (err error) {
		out, err = res.Create(r.Context(), s, t.cluster, t.gvr, obj.GetNamespace(), obj, dry)
		return err
	}
	if dry {
		err = run()
	} else {
		err = d.audited(r.Context(), d.event(t, obj.GetNamespace(), audit.VerbCreate, obj.GetName()), run)
	}
	if err != nil {
		return err
	}
	writeObject(w, http.StatusCreated, out)
	return nil
}

func (d Deps) replaceResource(w http.ResponseWriter, r *http.Request, s session.Session, t target, res *kube.Resources) error {
	obj, err := d.readObject(w, r, s, t, true)
	if err != nil {
		return err
	}
	if obj.GetResourceVersion() == "" {
		return fmt.Errorf("resourceVersion is required: %w", ErrInvalid)
	}
	dry := isDryRun(r)
	var live, out *unstructured.Unstructured
	run := func() (err error) {
		live, out, err = res.Update(r.Context(), s, t.cluster, t.gvr, obj.GetNamespace(), obj, dry)
		return err
	}
	if dry {
		err = run()
	} else {
		err = d.audited(r.Context(), d.event(t, obj.GetNamespace(), audit.VerbUpdate, obj.GetName()), run)
	}
	if err != nil {
		return err
	}
	if dry {
		writeJSON(w, http.StatusOK, map[string]any{"live": objectOrNil(live), "result": objectOrNil(out)})
		return nil
	}
	writeObject(w, http.StatusOK, out)
	return nil
}

func (d Deps) patchResource(w http.ResponseWriter, r *http.Request, s session.Session, t target, res *kube.Resources) error {
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	var pt types.PatchType
	switch ct {
	case mergePatch:
		pt = types.MergePatchType
	case strategicPatch:
		pt = types.StrategicMergePatchType
	default:
		return fmt.Errorf("Content-Type must be %s or %s: %w", mergePatch, strategicPatch, ErrInvalid)
	}
	body, err := readBody(w, r)
	if err != nil {
		return err
	}
	dry := isDryRun(r)
	var out *unstructured.Unstructured
	run := func() (err error) {
		out, err = res.Patch(r.Context(), s, t.cluster, t.gvr, t.ns, t.name, pt, body, dry)
		return err
	}
	if dry {
		err = run()
	} else {
		err = d.audited(r.Context(), d.event(t, t.ns, audit.VerbPatch, t.name), run)
	}
	if err != nil {
		return err
	}
	writeObject(w, http.StatusOK, out)
	return nil
}

func (d Deps) deleteResource(w http.ResponseWriter, r *http.Request, s session.Session, t target, res *kube.Resources) error {
	policy := metav1.DeletionPropagation(r.URL.Query().Get("propagationPolicy"))
	err := d.audited(r.Context(), d.event(t, t.ns, audit.VerbDelete, t.name), func() error {
		return res.Delete(r.Context(), s, t.cluster, t.gvr, t.ns, t.name, policy)
	})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

func objectOrNil(o *unstructured.Unstructured) any {
	if o == nil {
		return nil
	}
	return o.Object
}

func writeObject(w http.ResponseWriter, status int, o *unstructured.Unstructured) {
	writeJSON(w, status, objectOrNil(o))
}
