// Package helm4 implements helm.Driver with the Helm 4 SDK. Helm 3 stores releases in the same format,
// so releases made by Helm 3 are read the same way.
package helm4

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"helm.sh/helm/v4/pkg/action"
	ri "helm.sh/helm/v4/pkg/release"
	v1 "helm.sh/helm/v4/pkg/release/v1"
	"helm.sh/helm/v4/pkg/storage/driver"
	"sigs.k8s.io/yaml"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/helm"
	"github.com/bdnix/kubelook/internal/redact"
)

// ConfigFunc returns the SDK configuration for a namespace; "" means all namespaces. Helm's storage driver is
// bound to a namespace when it is created, so the adapter asks for a configuration per call.
type ConfigFunc func(namespace string) (*action.Configuration, error)

// Adapter is the Helm 4 implementation of helm.Driver.
type Adapter struct {
	open    ConfigFunc
	workDir string
	locate  Locator
	// registryHTTP is the HTTP client of OCI pulls; nil → the SDK's own.
	registryHTTP *http.Client
}

// New returns an adapter over the configurations `open` produces.
func New(open ConfigFunc) *Adapter { return &Adapter{open: open, locate: defaultLocate} }

// WithWorkDir sets the directory the adapter may write to (chart downloads, registry and repository files).
func (a *Adapter) WithWorkDir(dir string) *Adapter {
	a.workDir = dir
	return a
}

// WithRegistryHTTP sets the HTTP client OCI pulls use (the egress guard's).
func (a *Adapter) WithRegistryHTTP(c *http.Client) *Adapter {
	a.registryHTTP = c
	return a
}

// WorkDir is the directory set by WithWorkDir.
func (a *Adapter) WorkDir() string { return a.workDir }

const (
	applyClientSide = "csa"
	statusMatch     = action.ListAll
)

// errReleaseGone is what the SDK returns for a release or a revision that does not exist.
var errReleaseGone = driver.ErrReleaseNotFound

func notFound(namespace, name string, err error) error {
	if errors.Is(err, errReleaseGone) {
		return fmt.Errorf("release %s/%s: %w", namespace, name, apperr.ErrNotFound)
	}
	return err
}

// releaseOf is the one place that turns what the SDK returns into the concrete release type.
func releaseOf(r ri.Releaser) (*v1.Release, error) {
	switch rel := r.(type) {
	case *v1.Release:
		return rel, nil
	case v1.Release:
		return &rel, nil
	}
	return nil, fmt.Errorf("unexpected release type %T", r)
}

func toRelease(rel *v1.Release) helm.Release {
	out := helm.Release{Name: rel.Name, Namespace: rel.Namespace, Revision: rel.Version, ApplyMethod: applyClientSide}
	if rel.ApplyMethod != "" {
		out.ApplyMethod = rel.ApplyMethod
	}
	if rel.Info != nil {
		out.Status = rel.Info.Status.String()
		out.Updated = rel.Info.LastDeployed
	}
	if rel.Chart != nil && rel.Chart.Metadata != nil {
		out.Chart = rel.Chart.Metadata.Name
		out.ChartVersion = rel.Chart.Metadata.Version
		out.AppVersion = rel.Chart.Metadata.AppVersion
	}
	return out
}

// List implements helm.Driver.
func (a *Adapter) List(_ context.Context, namespace string) ([]helm.Release, error) {
	cfg, err := a.open(namespace)
	if err != nil {
		return nil, err
	}
	l := action.NewList(cfg)
	l.All = true
	l.AllNamespaces = namespace == ""
	l.StateMask = statusMatch
	found, err := l.Run()
	if err != nil {
		return nil, fmt.Errorf("listing releases: %w", err)
	}
	out := make([]helm.Release, 0, len(found))
	for _, f := range found {
		rel, err := releaseOf(f)
		if err != nil {
			return nil, err
		}
		if namespace != "" && rel.Namespace != namespace {
			continue
		}
		out = append(out, toRelease(rel))
	}
	slices.SortFunc(out, func(x, y helm.Release) int {
		return strings.Compare(x.Namespace+"/"+x.Name, y.Namespace+"/"+y.Name)
	})
	return out, nil
}

func toYAML(values map[string]any) (string, error) {
	b, err := yaml.Marshal(values)
	return string(b), err
}

// Get implements helm.Driver.
func (a *Adapter) Get(_ context.Context, namespace, name string, revision int) (*helm.Detail, error) {
	cfg, err := a.open(namespace)
	if err != nil {
		return nil, err
	}
	get := action.NewGet(cfg)
	get.Version = revision
	found, err := get.Run(name)
	if err != nil {
		return nil, notFound(namespace, name, err)
	}
	rel, err := releaseOf(found)
	if err != nil {
		return nil, err
	}
	user := action.NewGetValues(cfg)
	user.Version = revision
	userValues, err := user.Run(name)
	if err != nil {
		return nil, notFound(namespace, name, err)
	}
	all := action.NewGetValues(cfg)
	all.Version = revision
	all.AllValues = true
	computed, err := all.Run(name)
	if err != nil {
		return nil, notFound(namespace, name, err)
	}
	d := &helm.Detail{Release: toRelease(rel), Manifest: redact.Manifest(rel.Manifest), Hooks: []helm.Hook{}}
	if d.UserValues, err = toYAML(userValues); err != nil {
		return nil, err
	}
	if d.ComputedValues, err = toYAML(computed); err != nil {
		return nil, err
	}
	if rel.Info != nil {
		d.Notes = rel.Info.Notes
	}
	for _, h := range rel.Hooks {
		events := make([]string, 0, len(h.Events))
		for _, e := range h.Events {
			events = append(events, string(e))
		}
		d.Hooks = append(d.Hooks, helm.Hook{Name: h.Name, Kind: h.Kind, Events: events})
	}
	return d, nil
}

// History implements helm.Driver.
func (a *Adapter) History(_ context.Context, namespace, name string) ([]helm.Revision, error) {
	cfg, err := a.open(namespace)
	if err != nil {
		return nil, err
	}
	found, err := action.NewHistory(cfg).Run(name)
	if err != nil {
		return nil, notFound(namespace, name, err)
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("release %s/%s: %w", namespace, name, apperr.ErrNotFound)
	}
	out := make([]helm.Revision, 0, len(found))
	for _, f := range found {
		rel, err := releaseOf(f)
		if err != nil {
			return nil, err
		}
		r := toRelease(rel)
		revision := helm.Revision{Revision: r.Revision, Status: r.Status, ChartVersion: r.ChartVersion, Updated: r.Updated}
		if rel.Info != nil {
			revision.Description = rel.Info.Description
		}
		out = append(out, revision)
	}
	slices.SortFunc(out, func(x, y helm.Revision) int { return y.Revision - x.Revision })
	return out, nil
}
