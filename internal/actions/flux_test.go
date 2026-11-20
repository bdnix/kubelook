package actions_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/bdnix/kubelook/internal/actions"
	"github.com/bdnix/kubelook/internal/apperr"
)

func fluxTarget(group, version, resource, name string) actions.Target {
	return actions.Target{Group: group, Version: version, Resource: resource, Namespace: "ns", Name: name}
}

func (e *env) serveFlux(group, version, resource, kind string, namespaced bool) {
	e.fakes.AddResources(&metav1.APIResourceList{
		GroupVersion: group + "/" + version,
		APIResources: []metav1.APIResource{{Name: resource, Kind: kind, Namespaced: namespaced, Verbs: metav1.Verbs{"get", "list", "patch"}}},
	})
}

func (e *env) fluxObject(group, version, kind, name string, fields map[string]any) {
	require.NoError(e.t, e.fakes.Dyn.Tracker().Add(obj(group+"/"+version, kind, name, fields)))
}

type family struct {
	group, version, resource, kind string
	suspend, resume                string
}

var fluxFamilies = []family{
	{"kustomize.toolkit.fluxcd.io", "v1", "kustomizations", "Kustomization", `{"spec":{"suspend":true}}`, `{"spec":{"suspend":false}}`},
	{"source.toolkit.fluxcd.io", "v1", "gitrepositories", "GitRepository", `{"spec":{"suspend":true}}`, `{"spec":{"suspend":false}}`},
	{"helm.toolkit.fluxcd.io", "v2", "helmreleases", "HelmRelease", `{"spec":{"suspend":true}}`, `{"spec":{"suspend":false}}`},
	{"source.extensions.fluxcd.io", "v1beta1", "artifactgenerators", "ArtifactGenerator", `{"metadata":{"annotations":{"source.extensions.fluxcd.io/reconcile":"disabled"}}}`, `{"metadata":{"annotations":{"source.extensions.fluxcd.io/reconcile":"enabled"}}}`},
	{"fluxcd.controlplane.io", "v1", "resourcesets", "ResourceSet", `{"metadata":{"annotations":{"fluxcd.controlplane.io/reconcile":"disabled"}}}`, `{"metadata":{"annotations":{"fluxcd.controlplane.io/reconcile":"enabled"}}}`},
}

func TestFlux_ReconcileSuspendResume_SendTheExactPatchOfEachFamily(t *testing.T) {
	for _, f := range fluxFamilies {
		t.Run(f.kind, func(t *testing.T) {
			e := newEnv(t)
			e.serveFlux(f.group, f.version, f.resource, f.kind, true)
			e.fluxObject(f.group, f.version, f.kind, "x", nil)
			tg := fluxTarget(f.group, f.version, f.resource, "x")

			require.NoError(t, e.do("fluxReconcile", tg, nil))
			p := e.lastPatch(t)
			assert.Equal(t, types.MergePatchType, p.PatchType)
			assert.JSONEq(t, `{"metadata":{"annotations":{"reconcile.fluxcd.io/requestedAt":"2026-11-14T12:00:00Z"}}}`, string(p.Patch))

			require.NoError(t, e.do("fluxSuspend", tg, nil))
			assert.JSONEq(t, f.suspend, string(e.lastPatch(t).Patch))
			require.NoError(t, e.do("fluxResume", tg, nil))
			assert.JSONEq(t, f.resume, string(e.lastPatch(t).Patch))

			assert.Equal(t, []string{
				"flux-reconcile:requested", "flux-reconcile:success",
				"flux-suspend:requested", "flux-suspend:success",
				"flux-resume:requested", "flux-resume:success",
			}, e.verbs(t))
		})
	}
}

func TestFlux_UsesTheVersionTheClusterServes(t *testing.T) {
	old := newEnv(t)
	old.serveFlux("kustomize.toolkit.fluxcd.io", "v1beta2", "kustomizations", "Kustomization", true)
	old.fluxObject("kustomize.toolkit.fluxcd.io", "v1beta2", "Kustomization", "x", nil)
	require.NoError(t, old.do("fluxSuspend", fluxTarget("kustomize.toolkit.fluxcd.io", "v1beta2", "kustomizations", "x"), nil))
	assert.Equal(t, "v1beta2", old.lastPatch(t).GetResource().Version)

	current := newEnv(t)
	current.serveFlux("kustomize.toolkit.fluxcd.io", "v1", "kustomizations", "Kustomization", true)
	current.fluxObject("kustomize.toolkit.fluxcd.io", "v1", "Kustomization", "x", nil)
	require.NoError(t, current.do("fluxSuspend", fluxTarget("kustomize.toolkit.fluxcd.io", "v1", "kustomizations", "x"), nil))
	assert.Equal(t, "v1", current.lastPatch(t).GetResource().Version)
	assert.ErrorIs(t, current.do("fluxSuspend", fluxTarget("kustomize.toolkit.fluxcd.io", "v1beta2", "kustomizations", "x"), nil), apperr.ErrNotFound, "the cluster does not serve v1beta2")
}

func TestFlux_ReconcileOfASuspendedObjectIsRefused(t *testing.T) {
	e := newEnv(t)
	e.serveFlux("kustomize.toolkit.fluxcd.io", "v1", "kustomizations", "Kustomization", true)
	e.fluxObject("kustomize.toolkit.fluxcd.io", "v1", "Kustomization", "x", map[string]any{"spec": map[string]any{"suspend": true}})
	err := e.do("fluxReconcile", fluxTarget("kustomize.toolkit.fluxcd.io", "v1", "kustomizations", "x"), nil)
	var api *apperr.APIError
	require.ErrorAs(t, err, &api)
	assert.Equal(t, 409, api.Status)
	assert.Equal(t, "Resume the object before reconciling.", api.Message)
	assert.Empty(t, e.patch, "nothing was sent")
	assert.Empty(t, e.verbs(t), "a refused request is not audited")

	e.serveFlux("fluxcd.controlplane.io", "v1", "resourcesets", "ResourceSet", true)
	e.fluxObject("fluxcd.controlplane.io", "v1", "ResourceSet", "rs", nil)
	o := obj("fluxcd.controlplane.io/v1", "ResourceSet", "off", nil)
	o.SetAnnotations(map[string]string{"fluxcd.controlplane.io/reconcile": "disabled"})
	require.NoError(t, e.fakes.Dyn.Tracker().Add(o))
	assert.Error(t, e.do("fluxReconcile", fluxTarget("fluxcd.controlplane.io", "v1", "resourcesets", "off"), nil), "an annotation counts as suspended too")
	assert.NoError(t, e.do("fluxReconcile", fluxTarget("fluxcd.controlplane.io", "v1", "resourcesets", "rs"), nil))
}

func TestFlux_FluxReportCannotBeSuspendedButCanBeReconciled(t *testing.T) {
	e := newEnv(t)
	e.serveFlux("fluxcd.controlplane.io", "v1", "fluxreports", "FluxReport", true)
	e.fluxObject("fluxcd.controlplane.io", "v1", "FluxReport", "flux", nil)
	tg := fluxTarget("fluxcd.controlplane.io", "v1", "fluxreports", "flux")
	err := e.do("fluxSuspend", tg, nil)
	var api *apperr.APIError
	require.ErrorAs(t, err, &api)
	assert.Equal(t, 400, api.Status)
	assert.ErrorIs(t, e.do("fluxResume", tg, nil), apperr.ErrInvalid)
	assert.Empty(t, e.verbs(t))
	assert.NoError(t, e.do("fluxReconcile", tg, nil))
}

func TestFlux_ActionsApplyToFluxKindsOnly(t *testing.T) {
	e := newEnv(t)
	assert.ErrorIs(t, e.do("fluxReconcile", depT, nil), apperr.ErrInvalid)
	assert.ErrorIs(t, e.do("fluxSuspend", podT, nil), apperr.ErrInvalid)
}
