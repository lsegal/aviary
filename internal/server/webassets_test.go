package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// webCheckout makes a checkout whose web sources were last changed at mod.
func webCheckout(t *testing.T, mod time.Time) string {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join(root, "web", "src", "App.vue")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("<template/>"), 0o600))
	require.NoError(t, os.Chtimes(src, mod, mod))
	return root
}

func TestWebAssetsProblem(t *testing.T) {
	built := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	dist := fstest.MapFS{
		"webdist/index.html":   {Data: []byte("<html></html>")},
		"webdist/.build-stamp": {Data: []byte(built.Format(time.RFC3339Nano) + "\n")},
	}
	unstamped := fstest.MapFS{"webdist/index.html": {Data: []byte("<html></html>")}}

	t.Run("not built", func(t *testing.T) {
		empty := fstest.MapFS{"webdist/.gitkeep": {}}
		assert.Equal(t, webAssetsNotBuilt, webAssetsProblem(empty, ""))
		assert.Equal(t, webAssetsNotBuilt, webAssetsProblem(empty, webCheckout(t, built)))
	})
	t.Run("stale in a checkout", func(t *testing.T) {
		problem := webAssetsProblem(dist, webCheckout(t, built.Add(time.Hour)))
		assert.Contains(t, problem, "web assets are stale")
		assert.Contains(t, problem, "pnpm dev:watch")
	})
	t.Run("current in a checkout", func(t *testing.T) {
		assert.Empty(t, webAssetsProblem(dist, webCheckout(t, built.Add(-time.Hour))))
	})
	t.Run("not run from a checkout", func(t *testing.T) {
		assert.Empty(t, webAssetsProblem(dist, ""))
	})
	t.Run("no build stamp", func(t *testing.T) {
		assert.Empty(t, webAssetsProblem(unstamped, webCheckout(t, built.Add(time.Hour))))
	})
}

func TestSPAHandlerNotBuiltPage(t *testing.T) {
	h := spaHandler{fs: http.FS(fstest.MapFS{".gitkeep": {}})}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/agents/watch/projects", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
	assert.Equal(t, "text/html; charset=utf-8", rr.Header().Get("Content-Type"))
	assert.Contains(t, rr.Body.String(), "Web assets not built")
	assert.Contains(t, rr.Body.String(), "pnpm dev:watch")
}
