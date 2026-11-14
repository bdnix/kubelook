package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestOpenAPI_ServedAsJSON(t *testing.T) {
	e := newKubeEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"path":"` + r.URL.Path + `"}`))
	}))
	t.Cleanup(srv.Close)
	e.fakes.RESTConfig = &rest.Config{Host: srv.URL}
	rec := e.get("/api/v1/clusters/v:prod/openapi/apps/v1")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"path":"/openapi/v3/apis/apps/v1"}`, rec.Body.String())
	assert.JSONEq(t, `{"path":"/openapi/v3/api/v1"}`, e.get("/api/v1/clusters/v:prod/openapi/core/v1").Body.String())
}

func TestOpenAPI_Errors(t *testing.T) {
	e := newKubeEnv(t)
	assert.Equal(t, http.StatusBadRequest, e.get("/api/v1/clusters/v:prod/openapi/core/v2").Code)
	rec := e.get("/api/v1/clusters/v:nope/openapi/apps/v1")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "cluster_unavailable", errorCode(t, rec))
	assert.Equal(t, http.StatusUnauthorized, e.doWithSession(http.MethodGet, "/api/v1/clusters/v:prod/openapi/apps/v1", "", "").Code)
}
