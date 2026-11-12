// Package webui serves the embedded single-page web application.
package webui

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed all:dist
var dist embed.FS

const (
	indexName    = "index.html"
	cacheNever   = "no-cache"
	cacheForever = "public, max-age=31536000, immutable"
)

// FS returns the embedded web build (the contents of dist/).
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		// "dist" is a constant that the embed directive guarantees; this cannot happen.
		panic(err)
	}
	return sub
}

// Handler serves the SPA from fsys.
//   - GET/HEAD only (405 otherwise)
//   - paths starting with /api/ or /auth/ → 404 (they belong to other handlers)
//   - /assets/* → file or 404, long cache
//   - any other path → the file if it exists, else index.html (no-cache)
//   - 503 "web UI not built" if index.html is missing
func Handler(fsys fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Cleaning a rooted path removes every "..", so the name cannot leave fsys.
		clean := path.Clean("/" + r.URL.Path)
		if strings.HasPrefix(clean, "/api/") || strings.HasPrefix(clean, "/auth/") {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(clean, "/")
		if isAsset(clean) {
			if !serveFile(w, r, fsys, name, cacheForever) {
				http.NotFound(w, r)
			}
			return
		}
		if name != "" && serveFile(w, r, fsys, name, cacheNever) {
			return
		}
		if !serveFile(w, r, fsys, indexName, cacheNever) {
			http.Error(w, "web UI not built", http.StatusServiceUnavailable)
		}
	})
}

func isAsset(cleanPath string) bool { return strings.HasPrefix(cleanPath, "/assets/") }

// serveFile writes the named file and reports whether it could be read.
// A missing file, a directory and any other read error all count as "not served".
func serveFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, name, cacheControl string) bool {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return false
	}
	w.Header().Set("Cache-Control", cacheControl)
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
	return true
}
