package kube_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/testutil/fakekube"
)

func TestBuildDiscovery_SubresourcesAreRecordedNotListed(t *testing.T) {
	d, err := kube.BuildDiscovery(fakekube.New().Disc)
	require.NoError(t, err)
	for _, r := range d.Resources {
		assert.NotContains(t, r.Resource, "/")
	}
	assert.True(t, d.Has("pods", "exec"))
	assert.True(t, d.Has("pods", "log"))
	assert.False(t, d.Has("pods", "attach"))
	assert.False(t, d.Has("secrets", "exec"))
}

func TestBuildDiscovery_ResultIsSortedByGroupThenResource(t *testing.T) {
	d, err := kube.BuildDiscovery(fakekube.New().Disc)
	require.NoError(t, err)
	var keys []string
	for _, r := range d.Resources {
		keys = append(keys, r.Group+"/"+r.Resource)
	}
	assert.Equal(t, []string{"/configmaps", "/namespaces", "/nodes", "/pods", "/secrets", "/services", "apps/deployments"}, keys)
	assert.Equal(t, []string{"create", "delete", "get", "list", "patch", "update", "watch"}, d.Resources[3].Verbs, "verbs are sorted")
}

func withVersions(t *testing.T) *kube.Discovery {
	t.Helper()
	fk := fakekube.New()
	fk.Disc.Resources = append(fk.Disc.Resources,
		&metav1.APIResourceList{GroupVersion: "example.io/v1", APIResources: []metav1.APIResource{{Name: "widgets", Kind: "Widget", Namespaced: true, Verbs: metav1.Verbs{"get"}}}},
		&metav1.APIResourceList{GroupVersion: "example.io/v1beta1", APIResources: []metav1.APIResource{{Name: "widgets", Kind: "Widget", Namespaced: true, Verbs: metav1.Verbs{"get"}}}},
	)
	d, err := kube.BuildDiscovery(fk.Disc)
	require.NoError(t, err)
	return d
}

func TestResolve_PreferredVersionWinsAndAllVersionsAreListed(t *testing.T) {
	d := withVersions(t)
	gvr, err := d.Resolve("example.io", "widgets")
	require.NoError(t, err)
	assert.Equal(t, schema.GroupVersionResource{Group: "example.io", Version: "v1", Resource: "widgets"}, gvr)
	gvr, err = d.ResolveKind("example.io", "Widget")
	require.NoError(t, err)
	assert.Equal(t, "v1", gvr.Version)
	assert.Equal(t, "widgets", gvr.Resource)

	var preferred, other kube.Resource
	for _, r := range d.Resources {
		switch {
		case r.Group == "example.io" && r.Version == "v1":
			preferred = r
		case r.Group == "example.io":
			other = r
		}
	}
	assert.True(t, preferred.Preferred)
	assert.False(t, other.Preferred)
	assert.Equal(t, []string{"v1", "v1beta1"}, preferred.ServedVersions)

	found, ok := d.Find(schema.GroupVersionResource{Group: "example.io", Version: "v1beta1", Resource: "widgets"})
	require.True(t, ok)
	assert.Equal(t, "v1beta1", found.Version)
	_, ok = d.Find(schema.GroupVersionResource{Group: "example.io", Version: "v9", Resource: "widgets"})
	assert.False(t, ok)
}

func TestResolve_CoreGroupAndUnknown(t *testing.T) {
	d := withVersions(t)
	gvr, err := d.Resolve("", "pods")
	require.NoError(t, err)
	assert.Equal(t, schema.GroupVersionResource{Version: "v1", Resource: "pods"}, gvr)

	_, err = d.Resolve("", "nope")
	require.ErrorIs(t, err, apperr.ErrNotFound)
	assert.Contains(t, err.Error(), "nope")
	_, err = d.Resolve("example.io", "nope")
	assert.Contains(t, err.Error(), "nope.example.io")
	_, err = d.ResolveKind("", "Nope")
	require.ErrorIs(t, err, apperr.ErrNotFound)
}

// partial answers like an API server where one group is down.
type partial struct{ discovery.DiscoveryInterface }

func (p partial) ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
	groups, lists, _ := p.DiscoveryInterface.ServerGroupsAndResources()
	return groups, lists[:1], &discovery.ErrGroupDiscoveryFailed{Groups: map[schema.GroupVersion]error{{Group: "apps", Version: "v1"}: errors.New("down")}}
}

func TestBuildDiscovery_PartialFailureKeepsTheOtherGroups(t *testing.T) {
	d, err := kube.BuildDiscovery(partial{fakekube.New().Disc})
	require.NoError(t, err)
	_, err = d.Resolve("", "pods")
	require.NoError(t, err)
	_, err = d.Resolve("apps", "deployments")
	require.ErrorIs(t, err, apperr.ErrNotFound)
}

type broken struct{ discovery.DiscoveryInterface }

func (broken) ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
	return nil, nil, errors.New("connection refused")
}

func TestBuildDiscovery_OtherErrorsFail(t *testing.T) {
	_, err := kube.BuildDiscovery(broken{fakekube.New().Disc})
	require.ErrorContains(t, err, "connection refused")
}

func TestBuildDiscovery_ServerVersionIsReported(t *testing.T) {
	fk := fakekube.New()
	fk.Disc.FakedServerVersion = nil
	d, err := kube.BuildDiscovery(fk.Disc)
	require.NoError(t, err)
	assert.NotNil(t, d)
}

func TestDiscovery_FindKind(t *testing.T) {
	d, err := kube.BuildDiscovery(fakekube.New().Disc)
	require.NoError(t, err)
	r, ok := d.FindKind(schema.GroupVersion{Group: "apps", Version: "v1"}, "Deployment")
	require.True(t, ok)
	assert.Equal(t, "deployments", r.Resource)
	_, ok = d.FindKind(schema.GroupVersion{Version: "v2"}, "Pod")
	assert.False(t, ok)
	_, ok = d.FindKind(schema.GroupVersion{Version: "v1"}, "Gadget")
	assert.False(t, ok)
}
