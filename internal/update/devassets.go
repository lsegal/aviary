package update

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// liveAssetDirs are the checkout-relative directories whose embedded files a
// dev build reads from its checkout through DevAssets. A pull that changes only
// these goes live without a restart (see PlanDevReload).
var liveAssetDirs = []string{".agents/skills", "skills"}

// DevAssets returns the files embedded from the checkout-relative directory
// dir. In git mode (see GitCheckout) they are read from the checkout, so edits
// take effect without a rebuild; otherwise they come from embedded. Callers
// read only files under liveAssetDirs from the result.
func DevAssets(embedded fs.FS, dir string) fs.FS {
	checkout := GitCheckout()
	if checkout == "" {
		return embedded
	}
	return os.DirFS(filepath.Join(checkout, filepath.FromSlash(dir)))
}

// liveAsset reports whether the checkout-relative file f is read live by a
// dev build, rather than from what the binary embeds.
func liveAsset(f string) bool {
	if strings.HasSuffix(f, ".go") || strings.HasSuffix(f, ".fgo") {
		return false
	}
	for _, dir := range liveAssetDirs {
		if strings.HasPrefix(f, dir+"/") {
			return true
		}
	}
	return false
}
