package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/bdnix/kubelook/internal/config"
)

func TestModules_ListsWhatTheClusterServes(t *testing.T) {
	e := newKubeEnv(t)
	e.fakes.AddResources(&metav1.APIResourceList{
		GroupVersion: "keda.sh/v1alpha1",
		APIResources: []metav1.APIResource{{Name: "scaledobjects", Kind: "ScaledObject", Namespaced: true, Verbs: metav1.Verbs{"get", "list"}}},
	})
	rec := e.get("/api/v1/clusters/v:prod/modules")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct {
		Modules []struct {
			Name   string
			Active bool
			Reason string
			Kinds  []struct{ Group, Version, Resource, Kind string }
		}
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	byName := map[string]int{}
	for i, m := range got.Modules {
		byName[m.Name] = i
	}
	keda := got.Modules[byName["keda"]]
	assert.True(t, keda.Active)
	require.Len(t, keda.Kinds, 1)
	assert.Equal(t, "scaledobjects", keda.Kinds[0].Resource)
	assert.False(t, got.Modules[byName["trivy"]].Active)
	assert.True(t, got.Modules[byName["permissions"]].Active)
}

func TestModules_HonoursTheConfiguration(t *testing.T) {
	e := newKubeEnv(t)
	e.deps.Config.Modules = map[string]config.Module{"keda": {Enabled: "false"}}
	e.fakes.AddResources(&metav1.APIResourceList{
		GroupVersion: "keda.sh/v1alpha1",
		APIResources: []metav1.APIResource{{Name: "scaledobjects", Kind: "ScaledObject", Namespaced: true, Verbs: metav1.Verbs{"list"}}},
	})
	rec := e.get("/api/v1/clusters/v:prod/modules")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"reason":"disabled"`)
}

func TestModules_UnknownClusterAndNoSession(t *testing.T) {
	e := newKubeEnv(t)
	assert.Equal(t, http.StatusNotFound, e.get("/api/v1/clusters/v:nope/modules").Code)
	assert.Equal(t, http.StatusUnauthorized, e.doWithSession(http.MethodGet, "/api/v1/clusters/v:prod/modules", "", "").Code)
}
