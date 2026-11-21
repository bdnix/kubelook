package helm4_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"

	"github.com/bdnix/kubelook/internal/helm/helm4"
)

// apiServer answers discovery for the core group only.
func apiServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api":
			_ = json.NewEncoder(w).Encode(map[string]any{"kind": "APIVersions", "versions": []string{"v1"}})
		case "/apis":
			_ = json.NewEncoder(w).Encode(map[string]any{"kind": "APIGroupList", "apiVersion": "v1", "groups": []any{}})
		case "/api/v1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"kind": "APIResourceList", "groupVersion": "v1",
				"resources": []map[string]any{{"name": "pods", "kind": "Pod", "namespaced": true, "verbs": []string{"get", "list"}}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRESTClientGetter_ServesTheConfigDiscoveryAndMapper(t *testing.T) {
	srv := apiServer(t)
	g := helm4.NewRESTClientGetter(&rest.Config{Host: srv.URL}, "team-a")

	cfg, err := g.ToRESTConfig()
	require.NoError(t, err)
	assert.Equal(t, srv.URL, cfg.Host)

	disc, err := g.ToDiscoveryClient()
	require.NoError(t, err)
	_, lists, err := disc.ServerGroupsAndResources()
	require.NoError(t, err)
	require.Len(t, lists, 1)
	assert.Equal(t, "pods", lists[0].APIResources[0].Name)

	mapper, err := g.ToRESTMapper()
	require.NoError(t, err)
	m, err := mapper.RESTMapping(schema.GroupKind{Kind: "Pod"}, "v1")
	require.NoError(t, err)
	assert.Equal(t, "pods", m.Resource.Resource)

	ns, _, err := g.ToRawKubeConfigLoader().Namespace()
	require.NoError(t, err)
	assert.Equal(t, "team-a", ns)
}

func TestRESTClientGetter_HandsOutACopyOfTheConfig(t *testing.T) {
	base := &rest.Config{Host: "https://x"}
	g := helm4.NewRESTClientGetter(base, "n")
	c, err := g.ToRESTConfig()
	require.NoError(t, err)
	c.Host = "changed"
	assert.Equal(t, "https://x", base.Host)
}

func TestRESTClientGetter_AnUnusableConnectionIsAnError(t *testing.T) {
	g := helm4.NewRESTClientGetter(&rest.Config{Host: "https://x.invalid", TLSClientConfig: rest.TLSClientConfig{CAData: []byte("not a certificate")}}, "n")
	_, err := g.ToDiscoveryClient()
	assert.Error(t, err)
	_, err = g.ToRESTMapper()
	assert.Error(t, err)
}
