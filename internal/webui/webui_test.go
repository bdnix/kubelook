package webui_test

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/webui"
)

func built() fstest.MapFS {
	return fstest.MapFS{
		"index.html":      {Data: []byte("<html>app</html>")},
		"assets/a.js":     {Data: []byte("x")},
		"favicon.svg":     {Data: []byte("<svg/>")},
		"placeholder.txt": {Data: []byte("web UI not built")},
	}
}

func do(fsys fs.FS, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	webui.Handler(fsys).ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestHandler_Root_ServesIndexNoCache(t *testing.T) {
	rec := do(built(), http.MethodGet, "/")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "<html>app</html>", rec.Body.String())
	assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
	assert.True(t, strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html"))
}

func TestHandler_Asset_ServesWithLongCache(t *testing.T) {
	rec := do(built(), http.MethodGet, "/assets/a.js")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "x", rec.Body.String())
	assert.Equal(t, "public, max-age=31536000, immutable", rec.Header().Get("Cache-Control"))
}

func TestHandler_ExistingNonAssetFile_IsServed(t *testing.T) {
	rec := do(built(), http.MethodGet, "/favicon.svg")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "<svg/>", rec.Body.String())
	assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
}

func TestHandler_ClientRoute_FallsBackToIndex(t *testing.T) {
	rec := do(built(), http.MethodGet, "/c/v%3Aprod/ns/default/core/pods/web-1")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "<html>app</html>", rec.Body.String())
}

func TestHandler_MissingAsset_Is404(t *testing.T) {
	assert.Equal(t, http.StatusNotFound, do(built(), http.MethodGet, "/assets/missing.js").Code)
}

func TestHandler_APIAndAuthPaths_Are404(t *testing.T) {
	assert.Equal(t, http.StatusNotFound, do(built(), http.MethodGet, "/api/v1/unknown").Code)
	assert.Equal(t, http.StatusNotFound, do(built(), http.MethodGet, "/auth/callback").Code)
}

func TestHandler_NotBuilt_Is503(t *testing.T) {
	rec := do(fstest.MapFS{"placeholder.txt": {Data: []byte("web UI not built")}}, http.MethodGet, "/")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Contains(t, rec.Body.String(), "web UI not built")
}

func TestHandler_WrongMethod_Is405(t *testing.T) {
	rec := do(built(), http.MethodPost, "/")
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, "GET, HEAD", rec.Header().Get("Allow"))
}

func TestHandler_Head_IsAllowed(t *testing.T) {
	assert.Equal(t, http.StatusOK, do(built(), http.MethodHead, "/").Code)
}

func TestHandler_DotDotPath_DoesNotEscape(t *testing.T) {
	rec := do(built(), http.MethodGet, "/assets/../../etc/passwd")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "<html>app</html>", rec.Body.String())
}

func TestFS_Embedded_ContainsPlaceholder(t *testing.T) {
	fsys := webui.FS()
	require.NotNil(t, fsys)
	_, err := fs.Stat(fsys, "placeholder.txt")
	assert.NoError(t, err)
}
