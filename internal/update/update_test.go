package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/lsegal/aviary/internal/buildinfo"

	"github.com/stretchr/testify/assert"
)

func TestConfigureEmulationAndCheck(t *testing.T) {
	orig := buildinfo.Version
	buildinfo.Version = "dev"
	t.Cleanup(func() {
		buildinfo.Version = orig
		_ = ConfigureEmulation("")
	})
	err := ConfigureEmulation("1.2.3:1.3.0")
	assert.NoError(t, err)

	check, err := Check(context.Background(), nil)
	assert.NoError(t, err)
	assert.True(t, check.Emulated)
	assert.True(t, check.UpgradeAvailable)
	assert.Equal(t, "1.2.3", check.CurrentVersion)
	assert.Equal(t, "1.3.0", check.LatestVersion)

}

func TestConfigureEmulationRejectsReleaseBuild(t *testing.T) {
	orig := buildinfo.Version
	buildinfo.Version = "1.2.3"
	t.Cleanup(func() { buildinfo.Version = orig })
	err := ConfigureEmulation("1.2.3:1.3.0")
	assert.Error(t, err)

}

func TestInstallNoopWhenEmulated(t *testing.T) {
	orig := buildinfo.Version
	buildinfo.Version = "dev"
	t.Cleanup(func() {
		buildinfo.Version = orig
		_ = ConfigureEmulation("")
	})
	err := ConfigureEmulation("1.2.3:1.3.0")
	assert.NoError(t, err)

	result, err := Install(context.Background(), InstallOptions{
		Version:    "1.3.0",
		TargetPath: filepath.Join(t.TempDir(), "aviary"),
	})
	assert.NoError(t, err)
	assert.True(t, result.Noop)
	assert.True(t, result.Emulated)

}

func TestReplaceFileFallsBackToCopyOnCrossDeviceRename(t *testing.T) {
	originalRename := renameFile
	// One attempt per rename, so each injected failure is final even on
	// Windows, where locked renames are retried.
	originalAttempts := renameAttempts
	renameAttempts = 1
	t.Cleanup(func() { renameAttempts = originalAttempts })
	renameCalls := 0
	renameFile = func(oldpath, newpath string) error {
		renameCalls++
		if renameCalls == 1 {
			return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: syscall.EXDEV}
		}
		return originalRename(oldpath, newpath)
	}
	t.Cleanup(func() { renameFile = originalRename })

	dir := t.TempDir()
	src := filepath.Join(dir, "src-bin")
	target := filepath.Join(dir, "target-bin")
	assert.NoError(t, os.WriteFile(src, []byte("new binary"), 0o755))

	err := replaceFile(src, target)
	assert.NoError(t, err)

	data, readErr := os.ReadFile(target)
	assert.NoError(t, readErr)
	assert.Equal(t, "new binary", string(data))
	_, statErr := os.Stat(src)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestReplaceFileRestoresBackupWhenInstallFails(t *testing.T) {
	originalRename := renameFile
	// One attempt per rename, so each injected failure is final even on
	// Windows, where locked renames are retried.
	originalAttempts := renameAttempts
	renameAttempts = 1
	t.Cleanup(func() { renameAttempts = originalAttempts })
	renameCalls := 0
	renameFile = func(oldpath, newpath string) error {
		renameCalls++
		switch renameCalls {
		case 1:
			return originalRename(oldpath, newpath)
		case 2:
			return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: syscall.EXDEV}
		case 3:
			return errors.New("rename failed")
		default:
			return originalRename(oldpath, newpath)
		}
	}
	t.Cleanup(func() { renameFile = originalRename })

	dir := t.TempDir()
	src := filepath.Join(dir, "src-bin")
	target := filepath.Join(dir, "target-bin")
	assert.NoError(t, os.WriteFile(src, []byte("new binary"), 0o755))
	assert.NoError(t, os.WriteFile(target, []byte("old binary"), 0o755))

	err := replaceFile(src, target)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "rename failed")

	data, readErr := os.ReadFile(target)
	assert.NoError(t, readErr)
	assert.Equal(t, "old binary", string(data))
}

func TestNormalizeVersionAcceptsLeadingV(t *testing.T) {
	assert.Equal(t, "v1.2.3", NormalizeVersion("1.2.3"))
	assert.Equal(t, "v1.2.3", NormalizeVersion(" v1.2.3 "))
	assert.Equal(t, "", NormalizeVersion(""))
	assert.True(t, SameVersion("1.2.3", "v1.2.3"))
	assert.False(t, SameVersion("1.2.3", "v1.2.4"))
	assert.Equal(t, "https://github.com/lsegal/aviary/releases/tag/v1.2.3", ReleaseURL("", "1.2.3"))
}

func TestReplaceFileRetriesLockedRename(t *testing.T) {
	originalRename, originalAttempts, originalDelay := renameFile, renameAttempts, renameDelay
	renameAttempts, renameDelay = 5, 0
	failures := 2
	renameFile = func(oldpath, newpath string) error {
		if failures > 0 {
			failures--
			return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: errors.New("being used by another process")}
		}
		return originalRename(oldpath, newpath)
	}
	t.Cleanup(func() { renameFile, renameAttempts, renameDelay = originalRename, originalAttempts, originalDelay })

	dir := t.TempDir()
	src := filepath.Join(dir, "src-bin")
	target := filepath.Join(dir, "target-bin")
	assert.NoError(t, os.WriteFile(src, []byte("new binary"), 0o755))
	assert.NoError(t, os.WriteFile(target, []byte("old binary"), 0o755))

	assert.NoError(t, replaceFile(src, target))
	data, err := os.ReadFile(target)
	assert.NoError(t, err)
	assert.Equal(t, "new binary", string(data))
}

func TestDownloadAssetPrefersGH(t *testing.T) {
	origLook, origRun := lookGH, runGH
	t.Cleanup(func() { lookGH, runGH = origLook, origRun })
	lookGH = func() (string, error) { return "gh", nil }
	var got []string
	runGH = func(_ context.Context, _ string, args ...string) error {
		got = args
		dir := args[len(args)-2]
		return os.WriteFile(filepath.Join(dir, "aviary_v1.3.0_linux_amd64.tar.gz"), []byte("archive"), 0o600)
	}

	dest := filepath.Join(t.TempDir(), "aviary_v1.3.0_linux_amd64.tar.gz")
	err := downloadAsset(context.Background(), nil, "lsegal/aviary", "v1.3.0", "aviary_v1.3.0_linux_amd64.tar.gz", "http://127.0.0.1:1/unused", dest)
	assert.NoError(t, err)
	assert.Equal(t, []string{"release", "download", "v1.3.0", "--repo", "lsegal/aviary", "--pattern", "aviary_v1.3.0_linux_amd64.tar.gz", "--dir", filepath.Dir(dest), "--clobber"}, got)
	data, _ := os.ReadFile(dest)
	assert.Equal(t, "archive", string(data))
}

func TestDownloadAssetFallsBackToDirectDownload(t *testing.T) {
	origLook, origRun := lookGH, runGH
	t.Cleanup(func() { lookGH, runGH = origLook, origRun })
	lookGH = func() (string, error) { return "gh", nil }
	runGH = func(context.Context, string, ...string) error { return errors.New("gh: not logged in") }

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("direct"))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "asset.tar.gz")
	assert.NoError(t, downloadAsset(context.Background(), srv.Client(), "lsegal/aviary", "v1.3.0", "asset.tar.gz", srv.URL, dest))
	data, _ := os.ReadFile(dest)
	assert.Equal(t, "direct", string(data))
}

func TestCleanupStagingRemovesBackup(t *testing.T) {
	target := filepath.Join(t.TempDir(), "aviary")
	assert.NoError(t, os.WriteFile(target+".bak", []byte("old"), 0o755))
	CleanupStaging(target)
	_, err := os.Stat(target + ".bak")
	assert.ErrorIs(t, err, os.ErrNotExist)
}
