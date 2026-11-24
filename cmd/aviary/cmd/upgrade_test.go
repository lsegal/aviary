package cmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/buildinfo"
	"github.com/lsegal/aviary/internal/update"
)

func TestResolveUpgradeTarget(t *testing.T) {
	current := update.CheckResult{CurrentVersion: "1.3.0", LatestVersion: "v1.3.0"}
	behind := update.CheckResult{CurrentVersion: "1.2.3", LatestVersion: "v1.3.0", UpgradeAvailable: true}

	target, upToDate := resolveUpgradeTarget(current, "", false)
	assert.True(t, upToDate, "nothing to do when current")
	assert.Empty(t, target)

	for _, requested := range []string{"1.3.0", "v1.3.0"} {
		_, upToDate = resolveUpgradeTarget(current, requested, false)
		assert.True(t, upToDate, "requesting the running version %q is a no-op", requested)
	}

	target, upToDate = resolveUpgradeTarget(behind, "", false)
	assert.False(t, upToDate)
	assert.Equal(t, "v1.3.0", target)

	target, upToDate = resolveUpgradeTarget(behind, "1.2.9", false)
	assert.False(t, upToDate)
	assert.Equal(t, "v1.2.9", target, "versions without a leading v resolve to the release tag")

	_, upToDate = resolveUpgradeTarget(update.CheckResult{CurrentVersion: "1.2.3"}, "", false)
	assert.False(t, upToDate, "an unknown latest version is not reported as up to date")

	dev := update.CheckResult{CurrentVersion: "588e26e2", LatestVersion: "v1.3.0"}
	_, upToDate = resolveUpgradeTarget(dev, "", false)
	assert.True(t, upToDate, "a dev build is not offered a release unless --release forces it")
	target, upToDate = resolveUpgradeTarget(dev, "", true)
	assert.False(t, upToDate)
	assert.Equal(t, "v1.3.0", target, "--release installs the latest release over a dev build")
}

func TestRunUpgradeUpToDateIsNoop(t *testing.T) {
	orig := buildinfo.Version
	buildinfo.Version = "1.4.0"
	update.ClearCache()
	t.Cleanup(func() {
		buildinfo.Version = orig
		update.ClearCache()
	})
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/lsegal/aviary/releases/latest" {
			http.Redirect(w, r, "/lsegal/aviary/releases/tag/v1.4.0", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	t.Setenv(update.DownloadBaseEnv, srv.URL)
	t.Cleanup(func() { upgradeCmd.SilenceUsage = false })

	r, w, err := os.Pipe()
	require.NoError(t, err)
	stdout := os.Stdout
	os.Stdout = w
	runErr := runUpgrade(upgradeCmd, nil)
	os.Stdout = stdout
	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)

	require.NoError(t, runErr)
	assert.Contains(t, string(out), "Aviary v1.4.0 is up to date.")
	assert.Equal(t, []string{"/lsegal/aviary/releases/latest"}, paths, "an up-to-date upgrade downloads nothing")
}

func TestRunUpgradeErrorIsShortAndHidesIP(t *testing.T) {
	orig := buildinfo.Version
	buildinfo.Version = "1.2.3"
	update.ClearCache()
	t.Cleanup(func() {
		buildinfo.Version = orig
		update.ClearCache()
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded for 203.0.113.7."}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv(update.DownloadBaseEnv, srv.URL)
	t.Cleanup(func() { upgradeCmd.SilenceUsage = false })

	err := runUpgrade(upgradeCmd, nil)
	require.Error(t, err)
	assert.True(t, upgradeCmd.SilenceUsage, "runtime errors print without the usage block")
	assert.Contains(t, err.Error(), "GitHub rate limited the request; retry in a few minutes")
	assert.NotContains(t, err.Error(), "203.0.113.7")
}

// stubLatestRelease serves tag as the latest release and refuses downloads.
func stubLatestRelease(t *testing.T, version, tag string) {
	t.Helper()
	orig := buildinfo.Version
	buildinfo.Version = version
	update.ClearCache()
	t.Cleanup(func() {
		buildinfo.Version = orig
		update.ClearCache()
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/lsegal/aviary/releases/latest" {
			http.Redirect(w, r, "/lsegal/aviary/releases/tag/"+tag, http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	t.Setenv(update.DownloadBaseEnv, srv.URL)
	t.Cleanup(func() { upgradeCmd.SilenceUsage = false })
}

// stubServer makes runUpgrade see a running server with install check, and
// records the server_upgrade calls it makes.
func stubServer(t *testing.T, check *update.CheckResult) *[]map[string]any {
	t.Helper()
	origInstall, origUpgrade := runningServerInstall, upgradeThroughServer
	t.Cleanup(func() { runningServerInstall, upgradeThroughServer = origInstall, origUpgrade })
	runningServerInstall = func() (update.CheckResult, bool) {
		if check == nil {
			return update.CheckResult{}, false
		}
		return *check, true
	}
	var calls []map[string]any
	upgradeThroughServer = func(args map[string]any, _ func()) error {
		calls = append(calls, args)
		return nil
	}
	return &calls
}

// captureUpgrade runs runUpgrade and returns its stdout.
func captureUpgrade(t *testing.T) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	stdout := os.Stdout
	os.Stdout = w
	runErr := runUpgrade(upgradeCmd, nil)
	os.Stdout = stdout
	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(out), runErr
}

// chdirIntoCheckout runs the test from inside a fresh Aviary git checkout.
func chdirIntoCheckout(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-q", dir)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	require.NoError(t, cmd.Run())
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/lsegal/aviary\n"), 0o644))
	t.Chdir(dir)
	return dir
}

func TestRunUpgradeReleaseBinaryInsideACheckoutUsesReleases(t *testing.T) {
	chdirIntoCheckout(t)
	stubLatestRelease(t, "v0.7.3", "v0.7.5")
	calls := stubServer(t, nil)

	out, err := captureUpgrade(t)
	require.Error(t, err, "the download is refused")
	assert.Contains(t, out, "Upgrading v0.7.3 → v0.7.5")
	assert.NotContains(t, out, "checkout")
	assert.Empty(t, *calls)
}

func TestRunUpgradeGoesThroughAReleaseServer(t *testing.T) {
	stubLatestRelease(t, "v0.7.3", "v0.7.5")
	calls := stubServer(t, &update.CheckResult{InstallType: update.InstallRelease, CurrentVersion: "v0.7.3"})

	out, err := captureUpgrade(t)
	require.NoError(t, err)
	assert.Contains(t, out, "Upgrading v0.7.3 → v0.7.5")
	require.Len(t, *calls, 1)
	assert.Equal(t, "v0.7.5", (*calls)[0]["version"])
}

func TestRunUpgradeLeavesADevServerAlone(t *testing.T) {
	chdirIntoCheckout(t)
	stubLatestRelease(t, "v0.7.3", "v0.7.5")
	calls := stubServer(t, &update.CheckResult{InstallType: update.InstallSource, Git: &update.GitState{Dir: "/src/aviary"}})

	out, err := captureUpgrade(t)
	require.Error(t, err, "the release binary installs itself; the download is refused")
	assert.Contains(t, out, "Upgrading v0.7.3 → v0.7.5")
	assert.Empty(t, *calls, "a release binary never asks a dev server to pull its checkout")
}

func TestRunUpgradeSourceRejectsVersion(t *testing.T) {
	upgradeSource, upgradeVersion = true, "v0.7.5"
	t.Cleanup(func() { upgradeSource, upgradeVersion = false, "" })
	t.Cleanup(func() { upgradeCmd.SilenceUsage = false })
	err := runUpgrade(upgradeCmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--source")
}

// gitRun runs git in dir with an isolated config.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

func TestRunUpgradeSourceOnAReleaseBinaryPullsTheCheckout(t *testing.T) {
	root := t.TempDir()
	gitRun(t, root, "init", "-q", "--bare", "-b", "main", "remote.git")
	gitRun(t, root, "clone", "-q", "remote.git", "other")
	other := filepath.Join(root, "other")
	gitRun(t, other, "checkout", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(other, "go.mod"), []byte("module github.com/lsegal/aviary\n"), 0o644))
	gitRun(t, other, "add", "go.mod")
	gitRun(t, other, "commit", "-q", "-m", "init")
	gitRun(t, other, "push", "-q", "-u", "origin", "main")
	gitRun(t, root, "clone", "-q", "remote.git", "checkout")
	require.NoError(t, os.WriteFile(filepath.Join(other, "a.txt"), []byte("a"), 0o644))
	gitRun(t, other, "add", "a.txt")
	gitRun(t, other, "commit", "-q", "-m", "a")
	gitRun(t, other, "push", "-q")
	checkout, err := filepath.EvalSymlinks(filepath.Join(root, "checkout"))
	require.NoError(t, err)
	t.Chdir(checkout)

	stubLatestRelease(t, "v0.7.3", "v0.7.5")
	calls := stubServer(t, nil)
	upgradeSource = true
	t.Cleanup(func() { upgradeSource = false })

	out, err := captureUpgrade(t)
	require.NoError(t, err)
	assert.Contains(t, out, "Pulled 1 commit(s) into "+checkout)
	assert.NotContains(t, out, "v0.7.5", "--source never looks at releases")
	assert.Empty(t, *calls)
}

func TestOtherServerNote(t *testing.T) {
	assert.Contains(t, otherServerNote(update.CheckResult{InstallType: update.InstallSource, Git: &update.GitState{Dir: "/src/aviary"}}), "dev build from the source checkout at /src/aviary")
	assert.Contains(t, otherServerNote(update.CheckResult{InstallType: update.InstallRelease, CurrentVersion: "0.7.3"}), "release install (v0.7.3)")
}
