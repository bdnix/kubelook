package httpapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/bdnix/kubelook/internal/flux"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/testutil/fakekube"
)

type noHelm struct{}

func (noHelm) Manifest(context.Context, session.Session, string, string, string) (string, error) {
	return "", nil
}

func newFluxEnv(t *testing.T) *writeEnv {
	t.Helper()
	e := newWriteEnv(t)
	e.fakes.AddResources(&metav1.APIResourceList{
		GroupVersion: "kustomize.toolkit.fluxcd.io/v1",
		APIResources: []metav1.APIResource{{Name: "kustomizations", Kind: "Kustomization", Namespaced: true, Verbs: metav1.Verbs{"get", "list"}}},
	})
	k := fakekube.Object("kustomize.toolkit.fluxcd.io/v1", "Kustomization", "flux-system", "apps", map[string]any{
		"status": map[string]any{"inventory": map[string]any{"entries": []any{
			map[string]any{"id": "default_web_apps_Deployment", "v": "v1"},
			map[string]any{"id": "default_web__Service", "v": "v1"},
		}}},
	})
	require.NoError(t, e.fakes.Dyn.Tracker().Add(k))
	e.deps.Flux = &flux.Inventory{Clients: e.fakes, Helm: noHelm{}}
	return e
}

const fluxPath = "/api/v1/clusters/v:prod/flux/inventory"

func TestFluxInventory_ListsTheManagedObjects(t *testing.T) {
	e := newFluxEnv(t)
	rec := e.sendAs(http.MethodGet, fluxPath+"?group=kustomize.toolkit.fluxcd.io&kind=Kustomization&namespace=flux-system&name=apps", "", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"entries":[
		{"group":"apps","version":"v1","kind":"Deployment","namespace":"default","name":"web"},
		{"group":"","version":"v1","kind":"Service","namespace":"default","name":"web"}]}`, rec.Body.String())
}

func TestFluxInventory_Errors(t *testing.T) {
	e := newFluxEnv(t)
	q := "?group=kustomize.toolkit.fluxcd.io&kind=Kustomization&namespace=flux-system"
	assert.Equal(t, http.StatusNotFound, e.sendAs(http.MethodGet, fluxPath+q+"&name=nope", "", "").Code)
	assert.Equal(t, http.StatusBadRequest, e.sendAs(http.MethodGet, fluxPath+q, "", "").Code, "no name")
	assert.Equal(t, http.StatusBadRequest, e.sendAs(http.MethodGet, fluxPath+"?group=source.toolkit.fluxcd.io&kind=GitRepository&name=x", "", "").Code)
	assert.Equal(t, http.StatusNotFound, e.sendAs(http.MethodGet, "/api/v1/clusters/v:nope/flux/inventory"+q+"&name=apps", "", "").Code)
	assert.Equal(t, http.StatusUnauthorized, e.doWithSession(http.MethodGet, fluxPath+q+"&name=apps", "", "").Code)
}

func TestFluxInventory_NotMountedWithoutTheInventory(t *testing.T) {
	e := newFluxEnv(t)
	e.deps.Flux = nil
	assert.Equal(t, http.StatusNotFound, e.sendAs(http.MethodGet, fluxPath+"?name=apps", "", "").Code)
}
