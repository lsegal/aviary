package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenameAttemptsFor(t *testing.T) {
	assert.Equal(t, 20, renameAttemptsFor("windows"))
	assert.Equal(t, 1, renameAttemptsFor("linux"))
	assert.Equal(t, 1, renameAttemptsFor("darwin"))
	assert.Equal(t, renameAttemptsFor(runtime.GOOS), defaultRenameAttempts())
}

func TestDefaultLookGHMatchesPathLookup(t *testing.T) {
	want, wantErr := exec.LookPath("gh")
	got, err := lookGH()
	assert.Equal(t, want, got)
	assert.Equal(t, wantErr == nil, err == nil)
}

func TestDefaultRunGHReportsCommandOutput(t *testing.T) {
	// The test binary stands in for gh: it exits 0 when asked to run no
	// tests and fails with a usage message for an unknown flag.
	self, err := os.Executable()
	require.NoError(t, err)

	assert.NoError(t, runGH(context.Background(), self, "-test.run=^$"))

	err = runGH(context.Background(), self, "-test.no-such-flag")
	require.Error(t, err)
	var exitErr *exec.ExitError
	assert.ErrorAs(t, err, &exitErr)
	assert.Contains(t, err.Error(), "flag provided but not defined")
}

// stubGH replaces the gh lookup and runner for the duration of a test.
func stubGH(t *testing.T, look func() (string, error), run func(context.Context, string, ...string) error) {
	t.Helper()
	origLook, origRun := lookGH, runGH
	t.Cleanup(func() { lookGH, runGH = origLook, origRun })
	lookGH, runGH = look, run
}

func TestDownloadAssetRenamesGHDownloadToDest(t *testing.T) {
	stubGH(t, func() (string, error) { return "gh", nil }, func(_ context.Context, _ string, args ...string) error {
		dir := args[len(args)-2]
		return os.WriteFile(filepath.Join(dir, "asset.tar.gz"), []byte("from gh"), 0o600)
	})

	dir := t.TempDir()
	dest := filepath.Join(dir, "archive.tar.gz")
	require.NoError(t, downloadAsset(context.Background(), nil, "lsegal/aviary", "v1.3.0", "asset.tar.gz", "http://127.0.0.1:1/unused", dest))

	data, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, "from gh", string(data))
	_, err = os.Stat(filepath.Join(dir, "asset.tar.gz"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestDownloadAssetFallsBackWhenGHWritesNothing(t *testing.T) {
	stubGH(t, func() (string, error) { return "gh", nil }, func(context.Context, string, ...string) error { return nil })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("direct"))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "asset.tar.gz")
	require.NoError(t, downloadAsset(context.Background(), srv.Client(), "lsegal/aviary", "v1.3.0", "asset.tar.gz", srv.URL, dest))
	data, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, "direct", string(data))
}

func TestDownloadAssetWithoutGHDownloadsDirectly(t *testing.T) {
	ran := false
	stubGH(t, func() (string, error) { return "", errors.New("gh not found") }, func(context.Context, string, ...string) error {
		ran = true
		return nil
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("direct"))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "asset.tar.gz")
	require.NoError(t, downloadAsset(context.Background(), srv.Client(), "lsegal/aviary", "v1.3.0", "asset.tar.gz", srv.URL, dest))
	assert.False(t, ran, "gh must not run when it is not installed")
	data, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, "direct", string(data))
}

func TestDownloadAssetReportsNonSuccessStatus(t *testing.T) {
	stubGH(t, func() (string, error) { return "", errors.New("gh not found") }, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "asset.tar.gz")
	err := downloadAsset(context.Background(), srv.Client(), "lsegal/aviary", "v1.3.0", "asset.tar.gz", srv.URL, dest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "downloading release asset")
	assert.Contains(t, err.Error(), "404")
	_, statErr := os.Stat(dest)
	assert.ErrorIs(t, statErr, os.ErrNotExist, "no file is written for a failed response")
}

func TestDownloadAssetReportsWriteFailure(t *testing.T) {
	stubGH(t, func() (string, error) { return "", errors.New("gh not found") }, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("direct"))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "missing-dir", "asset.tar.gz")
	err := downloadAsset(context.Background(), srv.Client(), "lsegal/aviary", "v1.3.0", "asset.tar.gz", srv.URL, dest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating download file")
}

// useTempDir points os.TempDir at a fresh directory on every platform.
func useTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	t.Setenv("TEMP", dir)
	t.Setenv("TMP", dir)
	require.Equal(t, filepath.Clean(dir), filepath.Clean(os.TempDir()))
	return dir
}

func TestCleanupStagingRemovesOnlyStaleStagingDirs(t *testing.T) {
	tmp := useTempDir(t)
	old := time.Now().Add(-2 * time.Hour)

	stale := filepath.Join(tmp, "aviary-upgrade-stale")
	require.NoError(t, os.MkdirAll(filepath.Join(stale, "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(stale, "nested", "aviary"), []byte("x"), 0o600))
	require.NoError(t, os.Chtimes(stale, old, old))

	fresh := filepath.Join(tmp, "aviary-upgrade-fresh")
	require.NoError(t, os.MkdirAll(fresh, 0o755))

	unrelated := filepath.Join(tmp, "other-stale")
	require.NoError(t, os.MkdirAll(unrelated, 0o755))
	require.NoError(t, os.Chtimes(unrelated, old, old))

	target := filepath.Join(t.TempDir(), "aviary")
	require.NoError(t, os.WriteFile(target, []byte("current"), 0o755))
	require.NoError(t, os.WriteFile(target+".bak", []byte("old"), 0o755))

	CleanupStaging(target)

	_, err := os.Stat(stale)
	assert.ErrorIs(t, err, os.ErrNotExist, "stale staging dir is removed")
	assert.DirExists(t, fresh, "fresh staging dir is kept")
	assert.DirExists(t, unrelated, "non-staging dirs are never touched")
	_, err = os.Stat(target + ".bak")
	assert.ErrorIs(t, err, os.ErrNotExist, "backup is removed")
	assert.FileExists(t, target, "the binary itself is kept")
}

func TestCleanupStagingWithEmptyTargetSkipsBackup(t *testing.T) {
	useTempDir(t)
	cwd := t.TempDir()
	t.Chdir(cwd)
	require.NoError(t, os.WriteFile(".bak", []byte("keep"), 0o600))
	require.NoError(t, os.WriteFile(" .bak", []byte("keep"), 0o600))

	CleanupStaging("")
	CleanupStaging(" ")

	assert.FileExists(t, filepath.Join(cwd, ".bak"))
	assert.FileExists(t, filepath.Join(cwd, " .bak"))
}
