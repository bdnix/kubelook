package helm4_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/helm/helm4"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/testutil/fakekube"
)

// secretsServer is an API server whose cluster holds no release Secrets.
func secretsServer(t *testing.T, paths *[]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/secrets") {
			*paths = append(*paths, r.URL.Path)
			_ = json.NewEncoder(w).Encode(map[string]any{"kind": "SecretList", "apiVersion": "v1", "items": []any{}})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFactory_DriverUsesTheUsersConnectionAndTheNamespace(t *testing.T) {
	var paths []string
	srv := secretsServer(t, &paths)
	fk := fakekube.New()
	fk.Known = map[string]bool{"v:prod": true}
	fk.RESTConfig = &rest.Config{Host: srv.URL}

	d, err := (&helm4.Factory{Clients: fk}).Driver(context.Background(), session.Session{}, "v:prod", "team-a", t.TempDir())
	require.NoError(t, err)
	got, err := d.List(context.Background(), "team-a")
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Equal(t, []string{"/api/v1/namespaces/team-a/secrets"}, paths, "the release Secrets of the namespace, with the user's connection")
}

func TestFactory_AllNamespacesListsClusterWide(t *testing.T) {
	var paths []string
	srv := secretsServer(t, &paths)
	fk := fakekube.New()
	fk.RESTConfig = &rest.Config{Host: srv.URL}
	d, err := (&helm4.Factory{Clients: fk}).Driver(context.Background(), session.Session{}, "v:prod", "", t.TempDir())
	require.NoError(t, err)
	_, err = d.List(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, []string{"/api/v1/secrets"}, paths)
}

func TestFactory_EachDriverHasItsOwnWorkDir(t *testing.T) {
	fk := fakekube.New()
	f := &helm4.Factory{Clients: fk}
	a, err := f.Driver(context.Background(), session.Session{}, "v:prod", "ns", "/work/a")
	require.NoError(t, err)
	b, err := f.Driver(context.Background(), session.Session{}, "v:prod", "ns", "/work/b")
	require.NoError(t, err)
	assert.Equal(t, "/work/a", a.(*helm4.Adapter).WorkDir())
	assert.Equal(t, "/work/b", b.(*helm4.Adapter).WorkDir())
}

func TestFactory_UnknownCluster(t *testing.T) {
	fk := fakekube.New()
	fk.Known = map[string]bool{}
	_, err := (&helm4.Factory{Clients: fk}).Driver(context.Background(), session.Session{}, "v:nope", "ns", t.TempDir())
	assert.ErrorIs(t, err, apperr.ErrClusterUnavailable)
}
