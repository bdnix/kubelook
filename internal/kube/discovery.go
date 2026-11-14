package kube

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"

	"github.com/bdnix/kubelook/internal/apperr"
)

// Resource describes one resource a cluster serves in one version.
type Resource struct {
	Group          string   `json:"group"`
	Version        string   `json:"version"`
	Resource       string   `json:"resource"`
	Kind           string   `json:"kind"`
	Namespaced     bool     `json:"namespaced"`
	Verbs          []string `json:"verbs"`
	Preferred      bool     `json:"preferred"`
	ServedVersions []string `json:"servedVersions"`
}

// Discovery is what a cluster serves. Subresources (names with a "/") are not in Resources.
type Discovery struct {
	ServerVersion string     `json:"serverVersion"`
	Resources     []Resource `json:"resources"`

	subresources map[string]bool // "pods/exec"
}

// Has reports whether the cluster serves the subresource of the resource, e.g. Has("pods", "exec").
func (d *Discovery) Has(resource, subresource string) bool {
	return d.subresources[resource+"/"+subresource]
}

// Resolve returns the preferred served version of the resource of the group.
func (d *Discovery) Resolve(group, resource string) (schema.GroupVersionResource, error) {
	for _, r := range d.Resources {
		if r.Preferred && r.Group == group && r.Resource == resource {
			return schema.GroupVersionResource{Group: r.Group, Version: r.Version, Resource: r.Resource}, nil
		}
	}
	return schema.GroupVersionResource{}, fmt.Errorf("the cluster does not serve %s: %w", qualified(resource, group), apperr.ErrNotFound)
}

// ResolveKind returns the preferred served version of the kind of the group.
func (d *Discovery) ResolveKind(group, kind string) (schema.GroupVersionResource, error) {
	for _, r := range d.Resources {
		if r.Preferred && r.Group == group && r.Kind == kind {
			return schema.GroupVersionResource{Group: r.Group, Version: r.Version, Resource: r.Resource}, nil
		}
	}
	return schema.GroupVersionResource{}, fmt.Errorf("the cluster does not serve the kind %s: %w", qualified(kind, group), apperr.ErrNotFound)
}

// Find returns the entry for an exact group, version and resource.
func (d *Discovery) Find(gvr schema.GroupVersionResource) (Resource, bool) {
	for _, r := range d.Resources {
		if r.Group == gvr.Group && r.Version == gvr.Version && r.Resource == gvr.Resource {
			return r, true
		}
	}
	return Resource{}, false
}

func qualified(name, group string) string {
	if group == "" {
		return name
	}
	return name + "." + group
}

// BuildDiscovery asks the cluster what it serves. A group that fails to load is left out; the
// rest is still returned.
func BuildDiscovery(c discovery.DiscoveryInterface) (*Discovery, error) {
	groups, lists, err := c.ServerGroupsAndResources()
	if err != nil && !discovery.IsGroupDiscoveryFailedError(err) {
		return nil, fmt.Errorf("discovering the cluster's API: %w", err)
	}
	d := &Discovery{subresources: map[string]bool{}}
	if v, verr := c.ServerVersion(); verr == nil && v != nil {
		d.ServerVersion = v.GitVersion
	}
	preferred := map[string]string{}
	served := map[string][]string{} // group → versions
	for _, g := range groups {
		preferred[g.Name] = g.PreferredVersion.Version
		for _, v := range g.Versions {
			served[g.Name] = append(served[g.Name], v.Version)
		}
	}
	for _, l := range lists {
		gv, perr := schema.ParseGroupVersion(l.GroupVersion)
		if perr != nil {
			continue
		}
		for _, r := range l.APIResources {
			if strings.Contains(r.Name, "/") {
				d.subresources[r.Name] = true
				continue
			}
			d.Resources = append(d.Resources, toResource(gv, r, preferred[gv.Group] == gv.Version, served[gv.Group]))
		}
	}
	sort.SliceStable(d.Resources, func(i, j int) bool {
		a, b := d.Resources[i], d.Resources[j]
		switch {
		case a.Group != b.Group:
			return a.Group < b.Group
		case a.Resource != b.Resource:
			return a.Resource < b.Resource
		}
		return a.Version < b.Version
	})
	return d, nil
}

func toResource(gv schema.GroupVersion, r metav1.APIResource, preferred bool, served []string) Resource {
	versions := slices.Clone(served)
	if len(versions) == 0 {
		versions = []string{gv.Version}
	}
	verbs := slices.Clone(r.Verbs)
	sort.Strings(verbs)
	return Resource{
		Group: gv.Group, Version: gv.Version, Resource: r.Name, Kind: r.Kind, Namespaced: r.Namespaced,
		Verbs: verbs, Preferred: preferred, ServedVersions: versions,
	}
}
