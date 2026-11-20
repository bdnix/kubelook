package helm_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/helm"
	"github.com/bdnix/kubelook/internal/testutil/helmtest"
)

// repoServer serves index.yaml and the chart archives of web 1.0.0, 1.1.0 and 2.0.0-rc1, optionally behind basic auth.
func repoServer(t *testing.T, auth [2]string, hits *[]string) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	for _, v := range []string{"1.0.0", "1.1.0", "2.0.0-rc1"} {
		helmtest.Save(t, helmtest.ChartVersion("web", v, "x"), dir)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			*hits = append(*hits, r.URL.Path)
		}
		if auth[0] != "" {
			if u, p, ok := r.BasicAuth(); !ok || u != auth[0] || p != auth[1] {
				http.Error(w, "no", http.StatusUnauthorized)
				return
			}
		}
		if r.URL.Path == "/index.yaml" {
			fmt.Fprint(w, "apiVersion: v1\nentries:\n  web:\n    - {version: 1.0.0, urls: [web-1.0.0.tgz]}\n    - {version: 2.0.0-rc1, urls: [web-2.0.0-rc1.tgz]}\n    - {version: 1.1.0, urls: [web-1.1.0.tgz]}\n  other:\n    - {version: 3.0.0, urls: ['http://elsewhere.invalid/other-3.0.0.tgz']}\n")
			return
		}
		http.ServeFile(w, r, filepath.Join(dir, filepath.Base(r.URL.Path)))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetch_NoVersion_TakesTheNewestRelease(t *testing.T) {
	srv := repoServer(t, [2]string{}, nil)
	dest := t.TempDir()
	path, err := (&helm.Fetcher{MaxBytes: 1 << 20}).Fetch(t.Context(), helm.Repository{URL: srv.URL}, "web", "", dest)
	require.NoError(t, err)
	assert.Equal(t, dest, filepath.Dir(path), "the file lands in the job's directory")
	require.FileExists(t, path)
	chart, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotEmpty(t, chart)
}

func TestFetch_ExactVersion(t *testing.T) {
	var hits []string
	srv := repoServer(t, [2]string{}, &hits)
	_, err := (&helm.Fetcher{MaxBytes: 1 << 20}).Fetch(t.Context(), helm.Repository{URL: srv.URL}, "web", "1.0.0", t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, []string{"/index.yaml", "/web-1.0.0.tgz"}, hits)
}

func TestFetch_PrereleaseOnlyWhenAsked(t *testing.T) {
	var hits []string
	srv := repoServer(t, [2]string{}, &hits)
	_, err := (&helm.Fetcher{MaxBytes: 1 << 20}).Fetch(t.Context(), helm.Repository{URL: srv.URL}, "web", "2.0.0-rc1", t.TempDir())
	require.NoError(t, err)
	assert.Contains(t, hits, "/web-2.0.0-rc1.tgz")
}

func TestFetch_UnknownChartOrVersion_NotFound(t *testing.T) {
	srv := repoServer(t, [2]string{}, nil)
	f := &helm.Fetcher{MaxBytes: 1 << 20}
	_, err := f.Fetch(t.Context(), helm.Repository{URL: srv.URL}, "nope", "", t.TempDir())
	assert.ErrorIs(t, err, apperr.ErrNotFound)
	_, err = f.Fetch(t.Context(), helm.Repository{URL: srv.URL}, "web", "9.9.9", t.TempDir())
	assert.ErrorIs(t, err, apperr.ErrNotFound)
}

func TestFetch_SendsBasicAuthFromTheRepositoryEntry(t *testing.T) {
	srv := repoServer(t, [2]string{"alice", "s3cret"}, nil)
	f := &helm.Fetcher{MaxBytes: 1 << 20}
	_, err := f.Fetch(t.Context(), helm.Repository{URL: srv.URL, Username: "alice", Password: "s3cret"}, "web", "1.1.0", t.TempDir())
	require.NoError(t, err)
	_, err = f.Fetch(t.Context(), helm.Repository{URL: srv.URL}, "web", "1.1.0", t.TempDir())
	assert.Error(t, err, "without credentials the repository refuses")
}

func TestFetch_ChartLargerThanTheLimit_TooLarge_AndTheFileIsRemoved(t *testing.T) {
	srv := repoServer(t, [2]string{}, nil)
	dest := t.TempDir()
	_, err := (&helm.Fetcher{MaxBytes: 100}).Fetch(t.Context(), helm.Repository{URL: srv.URL}, "web", "1.0.0", dest)
	assert.ErrorIs(t, err, apperr.ErrTooLarge)
	left, _ := os.ReadDir(dest)
	assert.Empty(t, left, "nothing is left behind")
}

func TestFetch_AChartOnAnotherHostGetsNoCredentials(t *testing.T) {
	var hits []string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := r.BasicAuth(); ok {
			hits = append(hits, "credentials leaked")
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(other.Close)
	repo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "apiVersion: v1\nentries:\n  web:\n    - {version: 1.0.0, urls: ['%s/web-1.0.0.tgz']}\n", other.URL)
	}))
	t.Cleanup(repo.Close)
	_, _ = (&helm.Fetcher{MaxBytes: 1 << 20}).Fetch(t.Context(), helm.Repository{URL: repo.URL, Username: "a", Password: "b"}, "web", "", t.TempDir())
	assert.Empty(t, hits)
}

func TestFetch_RefusesAnInvalidChartName(t *testing.T) {
	srv := repoServer(t, [2]string{}, nil)
	_, err := (&helm.Fetcher{MaxBytes: 1 << 20}).Fetch(t.Context(), helm.Repository{URL: srv.URL}, "../etc", "", t.TempDir())
	assert.ErrorIs(t, err, apperr.ErrInvalid)
}

func TestFetch_RefusesARepositoryThatIsNotHTTP(t *testing.T) {
	_, err := (&helm.Fetcher{MaxBytes: 1 << 20}).Fetch(t.Context(), helm.Repository{URL: "file:///etc"}, "web", "", t.TempDir())
	assert.ErrorIs(t, err, apperr.ErrInvalid)
}
