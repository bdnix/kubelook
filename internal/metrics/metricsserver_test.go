package metrics_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clienttesting "k8s.io/client-go/testing"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/config"
	"github.com/bdnix/kubelook/internal/metrics"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/testutil/fakekube"
)

var (
	sess = session.Session{Hash: "s", UserID: "u"}
	t0   = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
)

func servesMetrics(fk *fakekube.Clients) {
	list := func(name, kind string, ns bool) *metav1.APIResourceList {
		return &metav1.APIResourceList{GroupVersion: "metrics.k8s.io/v1beta1", APIResources: []metav1.APIResource{
			{Name: name, Namespaced: ns, Kind: kind, Verbs: metav1.Verbs{"get", "list"}},
		}}
	}
	fk.AddResources(list("pods", "PodMetrics", true), list("nodes", "NodeMetrics", false))
}

func container(name, cpu, mem string) map[string]any {
	return map[string]any{"name": name, "usage": map[string]any{"cpu": cpu, "memory": mem}}
}

func podMetrics(ns, name string, containers ...map[string]any) *unstructured.Unstructured {
	cs := make([]any, len(containers))
	for i, c := range containers {
		cs[i] = c
	}
	return fakekube.Object("metrics.k8s.io/v1beta1", "PodMetrics", ns, name, map[string]any{"containers": cs})
}

func nodeMetrics(name, cpu, mem string) *unstructured.Unstructured {
	return fakekube.Object("metrics.k8s.io/v1beta1", "NodeMetrics", "", name, map[string]any{"usage": map[string]any{"cpu": cpu, "memory": mem}})
}

func newService(fk *fakekube.Clients, prom config.Prometheus) *metrics.Service {
	return metrics.New(metrics.Deps{Clients: fk, Clock: clock.NewFake(t0), Prometheus: prom})
}

func TestPods_NotServed_IsUnavailableWithoutAnError(t *testing.T) {
	got, err := newService(fakekube.New(), config.Prometheus{}).Pods(t.Context(), sess, "c", "")
	require.NoError(t, err)
	assert.False(t, got.Available)
	assert.Empty(t, got.Items)
}

func TestPods_SumsTheContainersAndConvertsQuantities(t *testing.T) {
	fk := fakekube.New()
	servesMetrics(fk)
	for _, o := range []*unstructured.Unstructured{
		podMetrics("ns2", "b", container("app", "250m", "128Mi")),
		podMetrics("ns1", "web", container("app", "250m", "128Mi"), container("side", "100000000n", "1024Ki")),
		podMetrics("ns1", "a", container("app", "2", "1G")),
	} {
		require.NoError(t, fk.Dyn.Tracker().Add(o))
	}
	got, err := newService(fk, config.Prometheus{}).Pods(t.Context(), sess, "c", "")
	require.NoError(t, err)
	require.True(t, got.Available)
	require.Len(t, got.Items, 3)
	assert.Equal(t, "ns1", got.Items[0].Namespace, "sorted by namespace and name")
	assert.Equal(t, "a", got.Items[0].Name)
	assert.EqualValues(t, 2000, got.Items[0].CPUMillicores)
	assert.EqualValues(t, 1_000_000_000, got.Items[0].MemoryBytes)
	web := got.Items[1]
	assert.Equal(t, "web", web.Name)
	assert.EqualValues(t, 350, web.CPUMillicores, "250m + 100000000n (a tenth of a core)")
	assert.EqualValues(t, 134217728+1048576, web.MemoryBytes)
	assert.Equal(t, []metrics.Container{
		{Name: "app", CPUMillicores: 250, MemoryBytes: 134217728},
		{Name: "side", CPUMillicores: 100, MemoryBytes: 1048576},
	}, web.Containers)
	assert.Equal(t, "ns2", got.Items[2].Namespace)
}

func TestPods_NamespaceNarrowsTheList(t *testing.T) {
	fk := fakekube.New()
	servesMetrics(fk)
	require.NoError(t, fk.Dyn.Tracker().Add(podMetrics("ns1", "a", container("c", "1m", "1Ki"))))
	require.NoError(t, fk.Dyn.Tracker().Add(podMetrics("ns2", "b", container("c", "1m", "1Ki"))))
	got, err := newService(fk, config.Prometheus{}).Pods(t.Context(), sess, "c", "ns2")
	require.NoError(t, err)
	require.Len(t, got.Items, 1)
	assert.Equal(t, "b", got.Items[0].Name)
}

func TestNodes_ReadsTheUsageOfEachNode(t *testing.T) {
	fk := fakekube.New()
	servesMetrics(fk)
	require.NoError(t, fk.Dyn.Tracker().Add(nodeMetrics("n2", "3500m", "8Gi")))
	require.NoError(t, fk.Dyn.Tracker().Add(nodeMetrics("n1", "1500000000n", "512Mi")))
	got, err := newService(fk, config.Prometheus{}).Nodes(t.Context(), sess, "c")
	require.NoError(t, err)
	require.True(t, got.Available)
	require.Len(t, got.Items, 2)
	assert.Equal(t, metrics.Item{Name: "n1", CPUMillicores: 1500, MemoryBytes: 536870912}, got.Items[0])
	assert.Equal(t, metrics.Item{Name: "n2", CPUMillicores: 3500, MemoryBytes: 8 << 30}, got.Items[1])
}

func TestUnreadableQuantities_CountAsZero(t *testing.T) {
	fk := fakekube.New()
	servesMetrics(fk)
	require.NoError(t, fk.Dyn.Tracker().Add(nodeMetrics("n1", "lots", "plenty")))
	got, err := newService(fk, config.Prometheus{}).Nodes(t.Context(), sess, "c")
	require.NoError(t, err)
	assert.Equal(t, metrics.Item{Name: "n1"}, got.Items[0])
}

func TestErrors_AreMappedAndTheClusterMustExist(t *testing.T) {
	fk := fakekube.New()
	servesMetrics(fk)
	fk.Dyn.PrependReactor("list", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "metrics.k8s.io", Resource: "pods"}, "", nil)
	})
	_, err := newService(fk, config.Prometheus{}).Pods(t.Context(), sess, "c", "")
	assert.ErrorIs(t, err, apperr.ErrForbidden)

	fk.Known = map[string]bool{}
	_, err = newService(fk, config.Prometheus{}).Nodes(t.Context(), sess, "c")
	assert.ErrorIs(t, err, apperr.ErrClusterUnavailable)
}
