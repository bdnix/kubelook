package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/testutil/fakekube"
)

// kubeEnv is a session-protected server whose clusters are the fakes.
type kubeEnv struct {
	*harness
	id, csrf string
	fakes    *fakekube.Clients
}

func newKubeEnv(t *testing.T) *kubeEnv {
	t.Helper()
	h, id, csrf := withSessions(t)
	fk := fakekube.New()
	fk.Known = map[string]bool{"v:prod": true, "k:a/b": true}
	h.deps.Kube = fk
	return &kubeEnv{harness: h, id: id, csrf: csrf, fakes: fk}
}

func (e *kubeEnv) get(path string) *httptest.ResponseRecorder {
	return e.doWithSession(http.MethodGet, path, e.id, "")
}

func TestDiscovery_ListsResourcesSortedAndSubresourcesOut(t *testing.T) {
	e := newKubeEnv(t)
	rec := e.get("/api/v1/clusters/v:prod/discovery")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct {
		ServerVersion string
		Resources     []struct {
			Group, Version, Resource, Kind string
			Namespaced, Preferred          bool
			Verbs, ServedVersions          []string
		}
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.NotEmpty(t, got.Resources)
	assert.Equal(t, "configmaps", got.Resources[0].Resource)
	assert.Equal(t, "v1", got.Resources[0].Version)
	assert.True(t, got.Resources[0].Preferred)
	assert.True(t, got.Resources[0].Namespaced)
	last := got.Resources[len(got.Resources)-1]
	assert.Equal(t, "apps", last.Group)
	assert.NotContains(t, rec.Body.String(), "pods/exec")
}

func TestDiscovery_UnknownCluster_404(t *testing.T) {
	e := newKubeEnv(t)
	rec := e.get("/api/v1/clusters/v:nope/discovery")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "cluster_unavailable", errorCode(t, rec))
}

func TestDiscovery_EncodedClusterIDAndSession(t *testing.T) {
	e := newKubeEnv(t)
	assert.Equal(t, http.StatusOK, e.get("/api/v1/clusters/k%3Aa%2Fb/discovery").Code)
	assert.Equal(t, http.StatusUnauthorized, e.doWithSession(http.MethodGet, "/api/v1/clusters/v:prod/discovery", "", "").Code)
}

func TestDiscovery_NotWired_404(t *testing.T) {
	e := newKubeEnv(t)
	e.deps.Kube = nil
	assert.Equal(t, http.StatusNotFound, e.get("/api/v1/clusters/v:prod/discovery").Code)
}
