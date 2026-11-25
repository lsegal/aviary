package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
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

func TestConfigureEmulationRejectsInvalidValues(t *testing.T) {
	orig := buildinfo.Version
	buildinfo.Version = "dev"
	t.Cleanup(func() {
		buildinfo.Version = orig
		_ = ConfigureEmulation("")
	})

	assert.ErrorContains(t, ConfigureEmulation("1.2.3"), "expected <local>:<remote>")
	assert.ErrorContains(t, ConfigureEmulation("nope:1.3.0"), "invalid emulated local version")
	assert.ErrorContains(t, ConfigureEmulation("1.2.3:nope"), "invalid emulated remote version")
	assert.False(t, EmulationActive(), "a rejected value must leave emulation off")

	assert.NoError(t, ConfigureEmulation(" 1.2.3 : 1.3.0 "))
	assert.True(t, EmulationActive())
	assert.Equal(t, "1.2.3", CurrentVersion(context.Background()))
	assert.NoError(t, ConfigureEmulation("  "))
	assert.False(t, EmulationActive(), "a blank value turns emulation off")
}

func TestCheckMessage(t *testing.T) {
	assert.Equal(t, "unable to determine latest version", CheckMessage(CheckResult{}))
	assert.Equal(t, "offline", CheckMessage(CheckResult{Message: "offline"}))
	assert.Equal(t, "new version available: v1.4.0", CheckMessage(CheckResult{LatestVersion: "v1.4.0", UpgradeAvailable: true}))
	assert.Equal(t, "already up to date", CheckMessage(CheckResult{LatestVersion: "v1.4.0"}))
}

func TestCompareVersions(t *testing.T) {
	out, err := compareVersions("1.4.0", "v1.4.0", false)
	assert.NoError(t, err)
	assert.True(t, out.UpgradeSupported)
	assert.False(t, out.UpgradeAvailable)
	assert.Equal(t, "https://github.com/lsegal/aviary/releases/tag/v1.4.0", out.ReleaseURL)

	// A dev build can compare against nothing, but that is not an error.
	out, err = compareVersions("0123abcd", "v1.4.0", false)
	assert.NoError(t, err)
	assert.False(t, out.UpgradeSupported)
	assert.Equal(t, "current build version is not a semver release", out.Message)

	out, err = compareVersions("1.2.3", "", false)
	assert.Error(t, err)
	assert.False(t, out.UpgradeSupported)
	assert.Empty(t, out.ReleaseURL)
	assert.Equal(t, "latest release version is not valid semver", out.Message)
}

func TestVersionPredicates(t *testing.T) {
	assert.True(t, IsReleaseVersion("v1.2.3"))
	assert.False(t, IsReleaseVersion("dev"))
	assert.False(t, IsReleaseVersion("0123abcd"))
	// Non-semver versions compare as plain strings, ignoring a leading v.
	assert.True(t, SameVersion("dev", "vdev"))
	assert.False(t, SameVersion("dev", "1.2.3"))
}

func TestInstallRequiresTargetPath(t *testing.T) {
	_, err := Install(context.Background(), InstallOptions{Version: "1.2.3", TargetPath: "  "})
	assert.ErrorContains(t, err, "target path is required")
}

func TestInstallFailsWhenLatestCannotBeResolved(t *testing.T) {
	useReleaseBuild(t, "1.2.3")
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	t.Setenv(DownloadBaseEnv, srv.URL)
	target := filepath.Join(t.TempDir(), "aviary")

	_, err := Install(context.Background(), InstallOptions{TargetPath: target, Client: srv.Client()})
	assert.Error(t, err)
	_, statErr := os.Stat(target)
	assert.ErrorIs(t, statErr, os.ErrNotExist, "a failed check must not create the target")
}

func TestDownloadFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("payload"))
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	ctx := context.Background()

	dest := filepath.Join(dir, "ok")
	assert.NoError(t, downloadFile(ctx, srv.Client(), srv.URL+"/asset", dest))
	data, err := os.ReadFile(dest)
	assert.NoError(t, err)
	assert.Equal(t, "payload", string(data))

	missing := filepath.Join(dir, "missing")
	assert.ErrorContains(t, downloadFile(ctx, srv.Client(), srv.URL+"/missing", missing), "HTTP 404")
	_, err = os.Stat(missing)
	assert.ErrorIs(t, err, os.ErrNotExist, "an error response must not leave a file")

	assert.ErrorContains(t, downloadFile(ctx, srv.Client(), "://bad", dest), "creating download request")
	assert.ErrorContains(t, downloadFile(ctx, srv.Client(), srv.URL+"/asset", filepath.Join(dir, "no", "such", "dir")), "creating download file")

	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	assert.ErrorContains(t, downloadFile(ctx, closed.Client(), closed.URL, dest), "downloading release asset")
}

func writeTarGz(t *testing.T, path string, files map[string]string, order []string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range order {
		body := files[name]
		assert.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body))}))
		_, err := tw.Write([]byte(body))
		assert.NoError(t, err)
	}
	assert.NoError(t, tw.Close())
	assert.NoError(t, gz.Close())
	assert.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))
}

func TestExtractBinary(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "release.tar.gz")
	writeTarGz(t, archive, map[string]string{
		"README.md":                 "docs",
		"aviary_v1.0.0/aviary":      "the binary",
		"aviary_v1.0.0/LICENSE.txt": "license",
	}, []string{"README.md", "aviary_v1.0.0/aviary", "aviary_v1.0.0/LICENSE.txt"})

	out, err := extractBinary(archive, dir, "aviary")
	assert.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "aviary"), out)
	data, err := os.ReadFile(out)
	assert.NoError(t, err)
	assert.Equal(t, "the binary", string(data))
	if runtime.GOOS != "windows" {
		info, err := os.Stat(out)
		assert.NoError(t, err)
		assert.NotZero(t, info.Mode()&0o100, "extracted binary must be executable")
	}

	_, err = extractBinary(archive, dir, "aviary.exe")
	assert.ErrorContains(t, err, `binary "aviary.exe" not found`)

	_, err = extractBinary(filepath.Join(dir, "absent.tar.gz"), dir, "aviary")
	assert.ErrorContains(t, err, "opening archive")

	notGzip := filepath.Join(dir, "plain.tar.gz")
	assert.NoError(t, os.WriteFile(notGzip, []byte("not gzip"), 0o600))
	_, err = extractBinary(notGzip, dir, "aviary")
	assert.ErrorContains(t, err, "reading archive")

	// A gzip stream whose contents are not a tar archive.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, _ = gz.Write(bytes.Repeat([]byte("x"), 1024))
	assert.NoError(t, gz.Close())
	badTar := filepath.Join(dir, "bad.tar.gz")
	assert.NoError(t, os.WriteFile(badTar, buf.Bytes(), 0o600))
	_, err = extractBinary(badTar, dir, "aviary")
	assert.ErrorContains(t, err, "reading archive entry")

	_, err = extractBinary(archive, filepath.Join(dir, "no", "such"), "aviary")
	assert.ErrorContains(t, err, "creating extracted binary")
}

func TestInstallBinaryCrossDeviceErrors(t *testing.T) {
	originalRename, originalAttempts := renameFile, renameAttempts
	renameAttempts = 1
	t.Cleanup(func() { renameFile, renameAttempts = originalRename, originalAttempts })
	exdev := func(oldpath, newpath string) error {
		return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: syscall.EXDEV}
	}
	dir := t.TempDir()

	t.Run("plain rename error is returned", func(t *testing.T) {
		renameFile = func(string, string) error { return errors.New("denied") }
		assert.EqualError(t, installBinary(filepath.Join(dir, "a"), filepath.Join(dir, "b")), "denied")
	})

	t.Run("missing source", func(t *testing.T) {
		renameFile = exdev
		err := installBinary(filepath.Join(dir, "absent"), filepath.Join(dir, "target"))
		assert.ErrorContains(t, err, "stat source binary")
	})

	t.Run("unwritable target dir", func(t *testing.T) {
		renameFile = exdev
		src := filepath.Join(dir, "src")
		assert.NoError(t, os.WriteFile(src, []byte("bin"), 0o755))
		err := installBinary(src, filepath.Join(dir, "no", "such", "target"))
		assert.ErrorContains(t, err, "creating staged binary")
		_, statErr := os.Stat(src)
		assert.NoError(t, statErr, "the source survives a failed install")
	})

	t.Run("final rename fails and staged copy is removed", func(t *testing.T) {
		calls := 0
		renameFile = func(oldpath, newpath string) error {
			calls++
			if calls == 1 {
				return exdev(oldpath, newpath)
			}
			return errors.New("target locked")
		}
		src := filepath.Join(dir, "src2")
		target := filepath.Join(dir, "sub", "target")
		assert.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
		assert.NoError(t, os.WriteFile(src, []byte("bin"), 0o755))
		assert.EqualError(t, installBinary(src, target), "target locked")
		entries, err := os.ReadDir(filepath.Dir(target))
		assert.NoError(t, err)
		assert.Empty(t, entries, "the staged copy must be cleaned up")
	})
}
