package server

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:webdist
var webDistFS embed.FS

// webFileServer returns an http.Handler serving the embedded web/dist files.
// All unknown paths are served index.html for SPA client-side routing.
func webFileServer() http.Handler {
	sub, err := fs.Sub(webDistFS, "webdist")
	if err != nil {
		// Fallback: empty 404 handler.
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "web UI not embedded", http.StatusNotFound)
		})
	}
	return spaHandler{fs: http.FS(sub)}
}

// staticFiles are web/dist files the browser fetches by exact path. Their
// headers matter for installing the app (#942), and a missing one must 404
// rather than be answered with index.html, which a browser would reject as a
// service worker or manifest only after a confusing parse error.
var staticFiles = map[string]struct{ contentType, cacheControl string }{
	// The service worker is revalidated on every update check so a deploy
	// replaces it promptly.
	"/sw.js":                {"text/javascript; charset=utf-8", "no-cache"},
	"/manifest.webmanifest": {"application/manifest+json", "no-cache"},
	"/offline.html":         {"text/html; charset=utf-8", "no-cache"},
}

// staticPrefixes hold build output and icons: never SPA routes.
var staticPrefixes = []string{"/assets/", "/icons/"}

func isStaticPath(p string) bool {
	if _, ok := staticFiles[p]; ok {
		return true
	}
	for _, prefix := range staticPrefixes {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

// spaHandler serves static files; falls back to index.html for missing routes.
type spaHandler struct {
	fs http.FileSystem
}

func (h spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f, err := h.fs.Open(r.URL.Path)
	if err != nil {
		h.serveMissing(w, r)
		return
	}
	stat, err := f.Stat()
	_ = f.Close()
	if err != nil || stat.IsDir() {
		h.serveMissing(w, r)
		return
	}
	if file, ok := staticFiles[r.URL.Path]; ok {
		w.Header().Set("Content-Type", file.contentType)
		w.Header().Set("Cache-Control", file.cacheControl)
	}
	http.FileServer(h.fs).ServeHTTP(w, r)
}

// serveMissing answers a path with no file: a 404 for static files, and
// index.html for everything else so client-side routes load the SPA.
func (h spaHandler) serveMissing(w http.ResponseWriter, r *http.Request) {
	if isStaticPath(r.URL.Path) {
		http.NotFound(w, r)
		return
	}
	h.serveIndex(w, r)
}

func (h spaHandler) serveIndex(w http.ResponseWriter, _ *http.Request) {
	f, err := h.fs.Open("/index.html")
	if err != nil {
		webAssetsNotBuiltPage(w)
		return
	}
	defer f.Close() //nolint:errcheck
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.Copy(w, f) //nolint:errcheck
}
