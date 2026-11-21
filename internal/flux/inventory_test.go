package flux_test

import (
	"context"
	"errors"
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/flux"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/testutil/fakekube"
)

func TestParseInventoryID(t *testing.T) {
	tests := []struct {
		id, version string
		want        flux.Entry
		ok          bool
	}{
		{"default_web_apps_Deployment", "v1", flux.Entry{Group: "apps", Version: "v1", Kind: "Deployment", Namespace: "default", Name: "web"}, true},
		{"default_web__Service", "v1", flux.Entry{Group: "", Version: "v1", Kind: "Service", Namespace: "default", Name: "web"}, true},
		{"_cluster-admin_rbac.authorization.k8s.io_ClusterRole", "v1", flux.Entry{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole", Name: "cluster-admin"}, true},
		{"_flux-system__Namespace", "v1", flux.Entry{Version: "v1", Kind: "Namespace", Name: "flux-system"}, true},
		{"too_few_parts", "v1", flux.Entry{}, false},
		{"a_b_c_d_e", "v1", flux.Entry{}, false},
		{"", "v1", flux.Entry{}, false},
	}
	for _, tc := range tests {
		got, ok := flux.ParseInventoryID(tc.id, tc.version)
		assert.Equal(t, tc.ok, ok, tc.id)
		if tc.ok {
			assert.Equal(t, tc.want, got, tc.id)
		}
	}
}

type fakeHelm struct {
	manifest string
	err      error
	asked    []string
}

func (f *fakeHelm) Manifest(_ context.Context, _ session.Session, _, namespace, name string) (string, error) {
	f.asked = append(f.asked, namespace+"/"+name)
	return f.manifest, f.err
}

func kustomization(entries ...string) map[string]any {
	var list []any
	for _, e := range entries {
		list = append(list, map[string]any{"id": e, "v": "v1"})
	}
	return map[string]any{"status": map[string]any{"inventory": map[string]any{"entries": list}}}
}

func newInventory(t *testing.T, h flux.HelmManifests, objs ...map[string]any) *flux.Inventory {
	t.Helper()
	fk := fakekube.New()
	fk.AddResources(
		&metav1.APIResourceList{GroupVersion: "kustomize.toolkit.fluxcd.io/v1", APIResources: []metav1.APIResource{{Name: "kustomizations", Kind: "Kustomization", Namespaced: true, Verbs: metav1.Verbs{"get", "list"}}}},
		&metav1.APIResourceList{GroupVersion: "helm.toolkit.fluxcd.io/v2", APIResources: []metav1.APIResource{{Name: "helmreleases", Kind: "HelmRelease", Namespaced: true, Verbs: metav1.Verbs{"get", "list"}}}},
		&metav1.APIResourceList{GroupVersion: "apps/v1", APIResources: []metav1.APIResource{{Name: "deployments", Kind: "Deployment", Namespaced: true, Verbs: metav1.Verbs{"get", "list"}}}},
		&metav1.APIResourceList{GroupVersion: "rbac.authorization.k8s.io/v1", APIResources: []metav1.APIResource{{Name: "clusterroles", Kind: "ClusterRole", Namespaced: false, Verbs: metav1.Verbs{"get", "list"}}}},
	)
	for i, o := range objs {
		o = maps.Clone(o)
		u := fakekube.Object(o["apiVersion"].(string), o["kind"].(string), "flux-system", o["name"].(string), nil)
		delete(o, "apiVersion")
		delete(o, "kind")
		delete(o, "name")
		for k, v := range o {
			u.Object[k] = v
		}
		require.NoError(t, fk.Dyn.Tracker().Add(u), i)
	}
	return &flux.Inventory{Clients: fk, Helm: h}
}

var sess = session.Session{Hash: "s", UserID: "u"}

func ref(group, kind, name string) flux.ObjectRef {
	return flux.ObjectRef{Group: group, Kind: kind, Namespace: "flux-system", Name: name}
}

func TestInventory_Kustomization_ParsesTheEntries(t *testing.T) {
	obj := kustomization("default_web_apps_Deployment", "default_web__Service", "_cluster-admin_rbac.authorization.k8s.io_ClusterRole", "garbage")
	obj["apiVersion"], obj["kind"], obj["name"] = "kustomize.toolkit.fluxcd.io/v1", "Kustomization", "apps"
	inv := newInventory(t, nil, obj)
	got, err := inv.Entries(t.Context(), sess, "c", ref("kustomize.toolkit.fluxcd.io", "Kustomization", "apps"))
	require.NoError(t, err)
	assert.Equal(t, []flux.Entry{
		{Group: "apps", Version: "v1", Kind: "Deployment", Namespace: "default", Name: "web"},
		{Group: "", Version: "v1", Kind: "Service", Namespace: "default", Name: "web"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole", Name: "cluster-admin"},
	}, got.Entries, "an entry that does not parse is left out")
	assert.Empty(t, got.Note)
}

func TestInventory_HelmReleaseWithItsOwnInventory_UsesIt(t *testing.T) {
	obj := kustomization("default_web_apps_Deployment")
	obj["apiVersion"], obj["kind"], obj["name"] = "helm.toolkit.fluxcd.io/v2", "HelmRelease", "web"
	h := &fakeHelm{}
	got, err := newInventory(t, h, obj).Entries(t.Context(), sess, "c", ref("helm.toolkit.fluxcd.io", "HelmRelease", "web"))
	require.NoError(t, err)
	require.Len(t, got.Entries, 1)
	assert.Empty(t, h.asked, "the Helm release is not read")
}

func TestInventory_HelmReleaseWithoutInventory_ListsTheObjectsOfTheHelmManifest(t *testing.T) {
	obj := map[string]any{
		"apiVersion": "helm.toolkit.fluxcd.io/v2", "kind": "HelmRelease", "name": "web",
		"status": map[string]any{"history": []any{map[string]any{"name": "web", "namespace": "apps-ns"}}},
	}
	h := &fakeHelm{manifest: "---\n# Source: web/templates/d.yaml\napiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\n---\napiVersion: v1\nkind: Secret\nmetadata:\n  name: web-secret\n  namespace: other\n---\napiVersion: rbac.authorization.k8s.io/v1\nkind: ClusterRole\nmetadata:\n  name: web-reader\n"}
	got, err := newInventory(t, h, obj).Entries(t.Context(), sess, "c", ref("helm.toolkit.fluxcd.io", "HelmRelease", "web"))
	require.NoError(t, err)
	assert.Equal(t, []string{"apps-ns/web"}, h.asked, "the release named by the history, in its storage namespace")
	assert.Equal(t, []flux.Entry{
		{Group: "apps", Version: "v1", Kind: "Deployment", Namespace: "apps-ns", Name: "web"},
		{Group: "", Version: "v1", Kind: "Secret", Namespace: "other", Name: "web-secret"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole", Name: "web-reader"},
	}, got.Entries)
}

func TestInventory_HelmReleaseWithoutHistory_IsUnavailable(t *testing.T) {
	obj := map[string]any{"apiVersion": "helm.toolkit.fluxcd.io/v2", "kind": "HelmRelease", "name": "web"}
	got, err := newInventory(t, &fakeHelm{}, obj).Entries(t.Context(), sess, "c", ref("helm.toolkit.fluxcd.io", "HelmRelease", "web"))
	require.NoError(t, err)
	assert.Empty(t, got.Entries)
	assert.Equal(t, "inventory unavailable", got.Note)
}

func TestInventory_TheHelmReleaseCannotBeRead_IsUnavailable(t *testing.T) {
	obj := map[string]any{
		"apiVersion": "helm.toolkit.fluxcd.io/v2", "kind": "HelmRelease", "name": "web",
		"status": map[string]any{"history": []any{map[string]any{"name": "web", "namespace": "apps-ns"}}},
	}
	h := &fakeHelm{err: apperr.ErrForbidden}
	got, err := newInventory(t, h, obj).Entries(t.Context(), sess, "c", ref("helm.toolkit.fluxcd.io", "HelmRelease", "web"))
	require.NoError(t, err, "a release the user cannot read is not an error of the page")
	assert.Equal(t, "inventory unavailable", got.Note)

	h.err = errors.New("boom")
	_, err = newInventory(t, h, obj).Entries(t.Context(), sess, "c", ref("helm.toolkit.fluxcd.io", "HelmRelease", "web"))
	assert.Error(t, err, "other failures are returned")
}

func TestInventory_Refusals(t *testing.T) {
	inv := newInventory(t, nil)
	_, err := inv.Entries(t.Context(), sess, "c", ref("kustomize.toolkit.fluxcd.io", "Kustomization", "missing"))
	assert.ErrorIs(t, err, apperr.ErrNotFound)
	_, err = inv.Entries(t.Context(), sess, "c", ref("source.toolkit.fluxcd.io", "GitRepository", "x"))
	assert.ErrorIs(t, err, apperr.ErrInvalid, "only Kustomization and HelmRelease have an inventory")
	_, err = inv.Entries(t.Context(), sess, "c", flux.ObjectRef{Group: "kustomize.toolkit.fluxcd.io", Kind: "Kustomization"})
	assert.ErrorIs(t, err, apperr.ErrInvalid, "no name")
}
