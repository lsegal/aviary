package server

import (
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/update"
)

// webBuildStampFile is written into webdist by scripts/build-webdist.mjs
// with the build's time, so a backend run from source can tell a web build
// older than the checkout's web sources.
const webBuildStampFile = ".build-stamp"

// webAssetsNotBuilt is the problem a backend reports when the binary embeds
// no web build: a fresh checkout embeds only webdist/.gitkeep.
const webAssetsNotBuilt = "web assets not built: run `pnpm build` or use `pnpm dev` (`pnpm dev:watch` for `aviary watch`)"

// webSourceDirs are the checkout paths a web build is made from.
var webSourceDirs = []string{"web/src", "web/public", "web/index.html"}

var webAssetsProblemOnce = sync.OnceValue(func() string {
	return webAssetsProblem(webDistFS, update.GitCheckout())
})

// WebAssetsProblem describes what is wrong with the embedded web UI, or
// returns "" when nothing is: no web build is embedded, or, for a backend
// run from a source checkout, the build is older than the web sources.
func WebAssetsProblem() string { return webAssetsProblemOnce() }

func webAssetsProblem(dist fs.FS, checkout string) string {
	if _, err := fs.Stat(dist, "webdist/index.html"); err != nil {
		return webAssetsNotBuilt
	}
	if checkout == "" {
		return ""
	}
	stamp, err := fs.ReadFile(dist, "webdist/"+webBuildStampFile)
	if err != nil {
		return ""
	}
	built, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(stamp)))
	if err != nil {
		return ""
	}
	changed := newestModTime(checkout, webSourceDirs)
	if !changed.After(built) {
		return ""
	}
	return fmt.Sprintf("web assets are stale: web sources changed at %s, after the embedded build at %s; run `pnpm build` or use `pnpm dev` (`pnpm dev:watch` for `aviary watch`)",
		changed.Local().Format(time.DateTime), built.Local().Format(time.DateTime))
}

// newestModTime returns the newest modification time of the files under
// the paths, relative to root.
func newestModTime(root string, paths []string) time.Time {
	var newest time.Time
	for _, p := range paths {
		_ = filepath.WalkDir(filepath.Join(root, p), func(_ string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if info, err := d.Info(); err == nil && info.ModTime().After(newest) {
				newest = info.ModTime()
			}
			return nil
		})
	}
	return newest
}

// webAssetsNotBuiltPage answers web UI requests when no web build is
// embedded, instead of a blank page.
func webAssetsNotBuiltPage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = fmt.Fprint(w, `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Aviary: web assets not built</title></head>
<body style="font-family: system-ui, sans-serif; max-width: 40rem; margin: 4rem auto; padding: 0 1rem; line-height: 1.5">
<h1>Web assets not built</h1>
<p>This Aviary binary was built without its web UI, as a build from a fresh source checkout is.</p>
<ul>
<li>Run <code>pnpm build</code> to build the web UI into the binary, or</li>
<li>use <code>pnpm dev</code>, or <code>pnpm dev:watch</code> for <code>aviary watch</code>, to serve it from the Vite dev server.</li>
</ul>
<p>The MCP endpoint at <code>/mcp</code> works either way.</p>
</body>
</html>
`)
}
