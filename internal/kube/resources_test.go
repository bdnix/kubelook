package kube_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	clienttesting "k8s.io/client-go/testing"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/redact"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/testutil/fakekube"
)

var (
	podsGVR    = schema.GroupVersionResource{Version: "v1", Resource: "pods"}
	secretsGVR = schema.GroupVersionResource{Version: "v1", Resource: "secrets"}
	nsGVR      = schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}
)

func fixture() *fakekube.Clients {
	return fakekube.New(
		fakekube.Pod("ns1", "a", nil), fakekube.Pod("ns1", "b", nil), fakekube.Pod("ns2", "c", nil),
		fakekube.Secret("ns1", "db", map[string]any{"password": "c2VjcmV0"}),
		fakekube.Secret("ns1", "sh.helm.release.v1.web.v1", map[string]any{"release": "aGVsbQ=="}),
		fakekube.Namespace("ns1"),
	)
}

func names(l *unstructured.UnstructuredList) []string {
	var out []string
	for _, i := range l.Items {
		out = append(out, i.GetNamespace()+"/"+i.GetName())
	}
	return out
}

func TestList_NamespacedWithAndWithoutNamespaceAndClusterScoped(t *testing.T) {
	r := kube.NewResources(fixture())
	l, err := r.List(t.Context(), sessA, "c", podsGVR, "ns1", kube.ListOptions{})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"ns1/a", "ns1/b"}, names(l))

	l, err = r.List(t.Context(), sessA, "c", podsGVR, "", kube.ListOptions{})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"ns1/a", "ns1/b", "ns2/c"}, names(l))

	l, err = r.List(t.Context(), sessA, "c", nsGVR, "", kube.ListOptions{})
	require.NoError(t, err)
	assert.Equal(t, []string{"/ns1"}, names(l))
}

func labelled(name string) map[string]any {
	p := fakekube.Pod("ns1", name, nil)
	p.SetLabels(map[string]string{"app": "web"})
	return p.Object
}

func TestList_SelectorsContinueAndOrderArePassedThrough(t *testing.T) {
	fk := fixture()
	var seen clienttesting.ListRestrictions
	fk.Dyn.PrependReactor("list", "pods", func(a clienttesting.Action) (bool, runtime.Object, error) {
		seen = a.(clienttesting.ListAction).GetListRestrictions()
		l := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1", "kind": "PodList",
			"metadata": map[string]any{"resourceVersion": "42", "continue": "tok"},
			"items":    []any{labelled("z"), labelled("a")},
		}}
		return true, l, nil
	})
	l, err := kube.NewResources(fk).List(t.Context(), sessA, "c", podsGVR, "ns1", kube.ListOptions{LabelSelector: "app=web", FieldSelector: "status.phase=Running"})
	require.NoError(t, err)
	assert.Equal(t, "app=web", seen.Labels.String())
	assert.Equal(t, "status.phase=Running", seen.Fields.String())
	assert.Equal(t, []string{"ns1/z", "ns1/a"}, names(l), "API order is kept")
	assert.Equal(t, "42", l.GetResourceVersion())
	assert.Equal(t, "tok", l.GetContinue())
}

// spy records the options of every list.
type spy struct {
	dynamic.Interface
	opts *[]metav1.ListOptions
}

func (s spy) Resource(gvr schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
	return spyRes{s.Interface.Resource(gvr), s.opts}
}

type spyRes struct {
	dynamic.NamespaceableResourceInterface
	opts *[]metav1.ListOptions
}

func (s spyRes) List(ctx context.Context, o metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	*s.opts = append(*s.opts, o)
	return s.NamespaceableResourceInterface.List(ctx, o)
}

type spyClients struct {
	*fakekube.Clients
	opts []metav1.ListOptions
}

func (s *spyClients) Dynamic(ctx context.Context, sess session.Session, id string) (dynamic.Interface, error) {
	d, err := s.Clients.Dynamic(ctx, sess, id)
	return spy{d, &s.opts}, err
}

func TestList_LimitDefaultAndMax(t *testing.T) {
	sc := &spyClients{Clients: fixture()}
	r := kube.NewResources(sc)
	for _, in := range []int64{0, -5, 10, 5000, 99999} {
		_, err := r.List(t.Context(), sessA, "c", nsGVR, "", kube.ListOptions{Limit: in, Continue: "x"})
		require.NoError(t, err)
	}
	var got []int64
	for _, o := range sc.opts {
		got = append(got, o.Limit)
		assert.Equal(t, "x", o.Continue)
	}
	assert.Equal(t, []int64{500, 500, 10, 5000, 5000}, got)
}

func TestList_UnknownResourceAndUnknownCluster(t *testing.T) {
	fk := fixture()
	fk.Known = map[string]bool{"c": true}
	r := kube.NewResources(fk)
	_, err := r.List(t.Context(), sessA, "c", schema.GroupVersionResource{Version: "v1", Resource: "gadgets"}, "", kube.ListOptions{})
	require.ErrorIs(t, err, apperr.ErrNotFound)
	_, err = r.List(t.Context(), sessA, "c", schema.GroupVersionResource{Version: "v9", Resource: "pods"}, "", kube.ListOptions{})
	require.ErrorIs(t, err, apperr.ErrNotFound, "the version must be served too")
	_, err = r.List(t.Context(), sessA, "nope", podsGVR, "", kube.ListOptions{})
	require.ErrorIs(t, err, apperr.ErrClusterUnavailable)
	_, err = r.Get(t.Context(), sessA, "nope", podsGVR, "ns1", "a")
	require.ErrorIs(t, err, apperr.ErrClusterUnavailable)
}

func TestList_ForbiddenKeepsTheServerMessage(t *testing.T) {
	fk := fixture()
	fk.Dyn.PrependReactor("list", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(podsGVR.GroupResource(), "", errors.New("RBAC: no"))
	})
	_, err := kube.NewResources(fk).List(t.Context(), sessA, "c", podsGVR, "", kube.ListOptions{})
	require.ErrorIs(t, err, apperr.ErrForbidden)
	assert.Contains(t, err.Error(), `pods is forbidden`)
	assert.NotContains(t, err.Error(), ": forbidden")
}

func TestList_SecretsAreMaskedAndHelmReleasesHidden(t *testing.T) {
	l, err := kube.NewResources(fixture()).List(t.Context(), sessA, "c", secretsGVR, "ns1", kube.ListOptions{})
	require.NoError(t, err)
	require.Equal(t, []string{"ns1/db"}, names(l))
	data, _, _ := unstructured.NestedMap(l.Items[0].Object, "data")
	assert.Equal(t, map[string]any{"password": redact.Mask}, data)
}

func TestGet_ObjectsAreRedactedAndHiddenOnesAreNotFound(t *testing.T) {
	r := kube.NewResources(fixture())
	o, err := r.Get(t.Context(), sessA, "c", secretsGVR, "ns1", "db")
	require.NoError(t, err)
	data, _, _ := unstructured.NestedMap(o.Object, "data")
	assert.Equal(t, redact.Mask, data["password"])

	_, err = r.Get(t.Context(), sessA, "c", secretsGVR, "ns1", "sh.helm.release.v1.web.v1")
	require.ErrorIs(t, err, apperr.ErrNotFound)

	_, err = r.Get(t.Context(), sessA, "c", podsGVR, "ns1", "nope")
	require.ErrorIs(t, err, apperr.ErrNotFound)

	ns, err := r.Get(t.Context(), sessA, "c", nsGVR, "", "ns1")
	require.NoError(t, err)
	assert.Equal(t, "ns1", ns.GetName())
}

func TestMapError_Table(t *testing.T) {
	gr := podsGVR.GroupResource()
	for name, tc := range map[string]struct {
		in   error
		want error
	}{
		"forbidden":    {apierrors.NewForbidden(gr, "x", errors.New("no")), apperr.ErrForbidden},
		"unauthorized": {apierrors.NewUnauthorized("expired"), apperr.ErrForbidden},
		"not found":    {apierrors.NewNotFound(gr, "x"), apperr.ErrNotFound},
		"conflict":     {apierrors.NewConflict(gr, "x", errors.New("rv")), apperr.ErrConflict},
		"exists":       {apierrors.NewAlreadyExists(gr, "x"), apperr.ErrConflict},
		"invalid":      {apierrors.NewBadRequest("bad"), apperr.ErrInvalid},
		"busy":         {apierrors.NewTooManyRequests("slow down", 1), apperr.ErrBusy},
		"unavailable":  {apierrors.NewServiceUnavailable("down"), apperr.ErrBusy},
		"too large":    {apierrors.NewRequestEntityTooLargeError("big"), apperr.ErrTooLarge},
	} {
		got := kube.MapError(tc.in)
		require.ErrorIs(t, got, tc.want, name)
		assert.NotEmpty(t, got.Error(), name)
	}
	plain := errors.New("plain")
	assert.Same(t, plain, kube.MapError(plain))
	other := apierrors.NewInternalError(errors.New("boom"))
	assert.Equal(t, other, kube.MapError(other))
	assert.NotErrorIs(t, kube.MapError(apierrors.NewNotFound(gr, "x")), apperr.ErrForbidden)
}
