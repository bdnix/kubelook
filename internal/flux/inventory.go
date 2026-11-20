package flux

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/session"
)

// Entry is one object a Kustomization or a HelmRelease manages.
type Entry struct {
	Group     string `json:"group"`
	Version   string `json:"version"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// Result is the inventory of an object. Note says why a list is empty when the cluster does not tell.
type Result struct {
	Entries []Entry `json:"entries"`
	Note    string  `json:"note,omitempty"`
}

const noteUnavailable = "inventory unavailable"

// ObjectRef names a Flux object.
type ObjectRef struct{ Group, Kind, Namespace, Name string }

// HelmManifests reads the manifest of a Helm release as the user.
type HelmManifests interface {
	Manifest(ctx context.Context, sess session.Session, cluster, namespace, name string) (string, error)
}

// Inventory reads what Flux objects manage.
type Inventory struct {
	Clients kube.Clients
	Helm    HelmManifests
}

// ParseInventoryID splits `<namespace>_<name>_<group>_<kind>` into an entry: the namespace is empty for a
// cluster-scoped object and the group is empty for a core kind, so the ID always has exactly four parts.
func ParseInventoryID(id, version string) (Entry, bool) {
	parts := strings.Split(id, "_")
	if len(parts) != 4 || parts[1] == "" || parts[3] == "" {
		return Entry{}, false
	}
	return Entry{Namespace: parts[0], Name: parts[1], Group: parts[2], Kind: parts[3], Version: version}, true
}

func fromInventory(status map[string]any) ([]Entry, bool) {
	inv, ok := status["inventory"].(map[string]any)
	if !ok {
		return nil, false
	}
	list, ok := inv["entries"].([]any)
	if !ok {
		return nil, false
	}
	out := make([]Entry, 0, len(list))
	for _, item := range list {
		m, _ := item.(map[string]any)
		id, _ := m["id"].(string)
		v, _ := m["v"].(string)
		if e, ok := ParseInventoryID(id, v); ok {
			out = append(out, e)
		}
	}
	return out, true
}

// Entries returns the objects the Kustomization or HelmRelease manages.
func (i *Inventory) Entries(ctx context.Context, sess session.Session, cluster string, ref ObjectRef) (Result, error) {
	if ref.Name == "" {
		return Result{}, fmt.Errorf("the object needs a name: %w", apperr.ErrInvalid)
	}
	isKustomization := ref.Group == "kustomize.toolkit.fluxcd.io" && ref.Kind == "Kustomization"
	isHelmRelease := ref.Group == "helm.toolkit.fluxcd.io" && ref.Kind == "HelmRelease"
	if !isKustomization && !isHelmRelease {
		return Result{}, fmt.Errorf("only a Kustomization or a HelmRelease has an inventory: %w", apperr.ErrInvalid)
	}
	disc, err := i.Clients.Discovery(ctx, sess, cluster)
	if err != nil {
		return Result{}, err
	}
	gvr, err := disc.ResolveKind(ref.Group, ref.Kind)
	if err != nil {
		return Result{}, err
	}
	dyn, err := i.Clients.Dynamic(ctx, sess, cluster)
	if err != nil {
		return Result{}, err
	}
	obj, err := dyn.Resource(gvr).Namespace(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil {
		return Result{}, kube.MapError(err)
	}
	status, _ := obj.Object["status"].(map[string]any)
	if entries, ok := fromInventory(status); ok {
		return Result{Entries: entries}, nil
	}
	if !isHelmRelease {
		return Result{Entries: []Entry{}}, nil
	}
	return i.fromHelm(ctx, sess, cluster, disc, status)
}

func (i *Inventory) fromHelm(ctx context.Context, sess session.Session, cluster string, disc *kube.Discovery, status map[string]any) (Result, error) {
	unavailable := Result{Entries: []Entry{}, Note: noteUnavailable}
	history, _ := status["history"].([]any)
	if len(history) == 0 || i.Helm == nil {
		return unavailable, nil
	}
	first, _ := history[0].(map[string]any)
	name, _ := first["name"].(string)
	namespace, _ := first["namespace"].(string)
	if namespace == "" {
		namespace, _ = status["storageNamespace"].(string)
	}
	if name == "" || namespace == "" {
		return unavailable, nil
	}
	manifest, err := i.Helm.Manifest(ctx, sess, cluster, namespace, name)
	switch {
	case errors.Is(err, apperr.ErrForbidden), errors.Is(err, apperr.ErrNotFound):
		return unavailable, nil
	case err != nil:
		return Result{}, err
	}
	return Result{Entries: entriesOfManifest(manifest, namespace, disc)}, nil
}

var docSeparator = regexp.MustCompile(`(?m)^---\s*$`)

func entriesOfManifest(manifest, releaseNamespace string, disc *kube.Discovery) []Entry {
	out := []Entry{}
	for _, doc := range docSeparator.Split(manifest, -1) {
		var o struct {
			APIVersion string `json:"apiVersion"`
			Kind       string `json:"kind"`
			Metadata   struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
		}
		if yaml.Unmarshal([]byte(doc), &o) != nil || o.Kind == "" || o.Metadata.Name == "" {
			continue
		}
		group, version := "", o.APIVersion
		if g, v, ok := strings.Cut(o.APIVersion, "/"); ok {
			group, version = g, v
		}
		namespace := o.Metadata.Namespace
		if namespace == "" && namespaced(disc, group, o.Kind) {
			namespace = releaseNamespace
		}
		out = append(out, Entry{Group: group, Version: version, Kind: o.Kind, Namespace: namespace, Name: o.Metadata.Name})
	}
	return out
}

func namespaced(disc *kube.Discovery, group, kind string) bool {
	for _, r := range disc.Resources {
		if r.Group == group && r.Kind == kind {
			return r.Namespaced
		}
	}
	return true
}
