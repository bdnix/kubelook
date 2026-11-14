package httpapi_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clienttesting "k8s.io/client-go/testing"

	"github.com/bdnix/kubelook/internal/testutil/fakekube"
)

type listBody struct {
	Items []struct {
		Metadata struct{ Name, Namespace string }
		Data     map[string]string
	}
	ResourceVersion string
	Continue        string
}

func newResourceEnv(t *testing.T) *kubeEnv {
	t.Helper()
	e := newKubeEnv(t)
	for _, o := range []struct{ ns, name string }{{"ns1", "a"}, {"ns1", "b"}, {"ns2", "c"}} {
		_ = e.fakes.Dyn.Tracker().Add(fakekube.Pod(o.ns, o.name, nil))
	}
	_ = e.fakes.Dyn.Tracker().Add(fakekube.Secret("ns1", "db", map[string]any{"password": "c2VjcmV0"}))
	_ = e.fakes.Dyn.Tracker().Add(fakekube.Secret("ns1", "sh.helm.release.v1.web.v1", map[string]any{"release": "aGVsbQ=="}))
	_ = e.fakes.Dyn.Tracker().Add(fakekube.Namespace("ns1"))
	return e
}

func TestResources_List_NamespacedAndAll(t *testing.T) {
	e := newResourceEnv(t)
	rec := e.get("/api/v1/clusters/v:prod/resources/core/v1/pods?namespace=ns1")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got listBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Len(t, got.Items, 2)

	rec = e.get("/api/v1/clusters/v:prod/resources/core/v1/pods")
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Len(t, got.Items, 3)
	assert.Contains(t, rec.Body.String(), `"continue"`)
}

func TestResources_List_NonCoreGroupAndEmptyListIsAnArray(t *testing.T) {
	e := newResourceEnv(t)
	rec := e.get("/api/v1/clusters/v:prod/resources/apps/v1/deployments")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"items":[]`)
}

func TestResources_List_SecretsAreMaskedAndHelmReleasesAbsent(t *testing.T) {
	e := newResourceEnv(t)
	rec := e.get("/api/v1/clusters/v:prod/resources/core/v1/secrets?namespace=ns1")
	require.Equal(t, http.StatusOK, rec.Code)
	var got listBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Items, 1)
	assert.Equal(t, "********", got.Items[0].Data["password"])
	assert.NotContains(t, rec.Body.String(), "c2VjcmV0")
	assert.NotContains(t, rec.Body.String(), "helm")
}

func TestResources_Get_ObjectAndErrors(t *testing.T) {
	e := newResourceEnv(t)
	rec := e.get("/api/v1/clusters/v:prod/resources/core/v1/pods/a?namespace=ns1")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"name":"a"`)

	rec = e.get("/api/v1/clusters/v:prod/resources/core/v1/pods/nope?namespace=ns1")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "not_found", errorCode(t, rec))

	rec = e.get("/api/v1/clusters/v:prod/resources/core/v1/secrets/sh.helm.release.v1.web.v1?namespace=ns1")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.NotContains(t, rec.Body.String(), "aGVsbQ")

	rec = e.get("/api/v1/clusters/v:prod/resources/core/v1/secrets/db?namespace=ns1")
	assert.NotContains(t, rec.Body.String(), "c2VjcmV0")

	rec = e.get("/api/v1/clusters/v:prod/resources/core/v1/namespaces/ns1")
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestResources_UnknownResourceAndCluster_404(t *testing.T) {
	e := newResourceEnv(t)
	assert.Equal(t, http.StatusNotFound, e.get("/api/v1/clusters/v:prod/resources/core/v1/gadgets").Code)
	rec := e.get("/api/v1/clusters/v:nope/resources/core/v1/pods")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "cluster_unavailable", errorCode(t, rec))
}

func TestResources_EncodedClusterID(t *testing.T) {
	e := newResourceEnv(t)
	rec := e.get("/api/v1/clusters/k%3Aa%2Fb/resources/core/v1/namespaces")
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestResources_ForbiddenShowsTheAPIServerMessage(t *testing.T) {
	e := newResourceEnv(t)
	e.fakes.Dyn.PrependReactor("list", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", errors.New("RBAC says no"))
	})
	rec := e.get("/api/v1/clusters/v:prod/resources/core/v1/pods")
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "forbidden", errorCode(t, rec))
	assert.Contains(t, rec.Body.String(), "RBAC says no")
}

func TestResources_LimitValidation(t *testing.T) {
	e := newResourceEnv(t)
	assert.Equal(t, http.StatusOK, e.get("/api/v1/clusters/v:prod/resources/core/v1/pods?limit=10").Code)
	assert.Equal(t, http.StatusBadRequest, e.get("/api/v1/clusters/v:prod/resources/core/v1/pods?limit=abc").Code)
	assert.Equal(t, http.StatusBadRequest, e.get("/api/v1/clusters/v:prod/resources/core/v1/pods?limit=-1").Code)
}

func TestResources_RequireSession(t *testing.T) {
	e := newResourceEnv(t)
	assert.Equal(t, http.StatusUnauthorized, e.doWithSession(http.MethodGet, "/api/v1/clusters/v:prod/resources/core/v1/pods", "", "").Code)
}
