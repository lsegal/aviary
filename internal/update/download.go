package update

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var (
	// lookGH finds the gh CLI; tests replace it to force the direct download.
	lookGH = func() (string, error) { return exec.LookPath("gh") }
	// runGH runs a gh command; tests replace it.
	runGH = func(ctx context.Context, gh string, args ...string) error {
		out, err := exec.CommandContext(ctx, gh, args...).CombinedOutput() //nolint:gosec
		if err != nil {
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}

	// renameAttempts is how many times a binary swap is tried. Windows can
	// briefly lock a file that was just written or scanned, so it retries.
	renameAttempts = defaultRenameAttempts()
	renameDelay    = 250 * time.Millisecond
)

func defaultRenameAttempts() int {
	if runtime.GOOS == "windows" {
		return 20
	}
	return 1
}

// downloadAsset fetches a release asset into dest. It uses an installed gh
// CLI first, which works with gh's own auth and proxy settings, and falls
// back to a direct download when gh is missing or fails.
func downloadAsset(ctx context.Context, client *http.Client, repo, version, assetName, assetURL, dest string) error {
	if gh, err := lookGH(); err == nil {
		dir := filepath.Dir(dest)
		ghErr := runGH(ctx, gh, "release", "download", version,
			"--repo", repo, "--pattern", assetName, "--dir", dir, "--clobber")
		if ghErr == nil {
			if _, statErr := os.Stat(filepath.Join(dir, assetName)); statErr == nil {
				if filepath.Join(dir, assetName) != dest {
					return os.Rename(filepath.Join(dir, assetName), dest)
				}
				return nil
			}
		}
	}
	return downloadFile(ctx, client, assetURL, dest)
}

// renameWithRetry renames oldpath to newpath, retrying transient failures on
// platforms that lock files. Cross-device and missing-file errors are final.
func renameWithRetry(oldpath, newpath string) error {
	var err error
	for attempt := 0; attempt < renameAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(renameDelay)
		}
		err = renameFile(oldpath, newpath)
		if err == nil || isCrossDeviceRename(err) || errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return err
}

// CleanupStaging removes what earlier upgrades left behind: the backup of the
// replaced binary next to target, which Windows cannot delete while the old
// process is still running, and stale staging directories in the temp dir.
func CleanupStaging(target string) {
	if strings.TrimSpace(target) != "" {
		_ = os.Remove(target + ".bak")
	}
	matches, _ := filepath.Glob(filepath.Join(os.TempDir(), "aviary-upgrade-*"))
	cutoff := time.Now().Add(-time.Hour)
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.RemoveAll(path)
	}
}
