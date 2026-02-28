package server

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
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

// spaHandler serves static files; falls back to index.html for missing routes.
type spaHandler struct {
	fs http.FileSystem
}

func (h spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f, err := h.fs.Open(r.URL.Path)
	if err != nil {
		// Path not found — serve index.html directly for SPA client-side routing.
		h.serveIndex(w, r)
		return
	}
	stat, err := f.Stat()
	f.Close()
	if err != nil || stat.IsDir() {
		// Directory or stat error — serve index.html.
		h.serveIndex(w, r)
		return
	}
	http.FileServer(h.fs).ServeHTTP(w, r)
}

func (h spaHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	f, err := h.fs.Open("/index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.Copy(w, f) //nolint:errcheck
}
