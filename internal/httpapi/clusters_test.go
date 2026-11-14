package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/cluster"
)

func TestClusters_ListsSourcesOfTheSession(t *testing.T) {
	e := newCredEnv(t)
	e.vault.AddMount("kubernetes/prod/", "kubernetes")
	e.post(http.MethodPost, "/api/v1/credentials/vault", `{"token":"`+apiVaultTok+`"}`)
	b, _ := json.Marshal(map[string]string{"label": "work", "content": apiKubeconfig})
	require.Equal(t, http.StatusOK, e.post(http.MethodPost, "/api/v1/credentials/kubeconfig", string(b)).Code)

	rec := e.doWithSession(http.MethodGet, "/api/v1/clusters", e.id, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct {
		Sources []struct {
			Type, Label, Status string
			Clusters            []struct{ ID, Name, State, Server string }
		}
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Sources, 2)
	assert.Equal(t, "vault", got.Sources[0].Type)
	assert.Equal(t, "ok", got.Sources[0].Status)
	require.Len(t, got.Sources[0].Clusters, 1)
	assert.Equal(t, "v:prod", got.Sources[0].Clusters[0].ID)
	assert.Equal(t, "kubeconfig", got.Sources[1].Type)
	assert.Equal(t, "work", got.Sources[1].Label)
	assert.Equal(t, "k:work/ctx", got.Sources[1].Clusters[0].ID)
	assert.Equal(t, "closed", got.Sources[1].Clusters[0].State)
}

func TestClusters_NoCredentials_EmptySourcesArray(t *testing.T) {
	e := newCredEnv(t)
	rec := e.doWithSession(http.MethodGet, "/api/v1/clusters", e.id, "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"sources":[]}`, rec.Body.String())
}

func TestClusters_NotWired_404AndWithoutSession401(t *testing.T) {
	e := newCredEnv(t)
	e.deps.Clusters = nil
	assert.Equal(t, http.StatusNotFound, e.doWithSession(http.MethodGet, "/api/v1/clusters", e.id, "").Code)
	e.deps.Clusters = cluster.NewRegistry(cluster.RegistryDeps{})
	assert.Equal(t, http.StatusUnauthorized, e.doWithSession(http.MethodGet, "/api/v1/clusters", "", "").Code)
}
