package cmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/buildinfo"
	"github.com/lsegal/aviary/internal/update"
)

func TestResolveUpgradeTarget(t *testing.T) {
	current := update.CheckResult{CurrentVersion: "1.3.0", LatestVersion: "v1.3.0"}
	behind := update.CheckResult{CurrentVersion: "1.2.3", LatestVersion: "v1.3.0", UpgradeAvailable: true}

	target, upToDate := resolveUpgradeTarget(current, "")
	assert.True(t, upToDate, "nothing to do when current")
	assert.Empty(t, target)

	for _, requested := range []string{"1.3.0", "v1.3.0"} {
		_, upToDate = resolveUpgradeTarget(current, requested)
		assert.True(t, upToDate, "requesting the running version %q is a no-op", requested)
	}

	target, upToDate = resolveUpgradeTarget(behind, "")
	assert.False(t, upToDate)
	assert.Equal(t, "v1.3.0", target)

	target, upToDate = resolveUpgradeTarget(behind, "1.2.9")
	assert.False(t, upToDate)
	assert.Equal(t, "v1.2.9", target, "versions without a leading v resolve to the release tag")

	_, upToDate = resolveUpgradeTarget(update.CheckResult{CurrentVersion: "1.2.3"}, "")
	assert.False(t, upToDate, "an unknown latest version is not reported as up to date")
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
	assert.Contains(t, string(out), "Installed release 1.4.0 at ")
	assert.Contains(t, string(out), "already up to date (1.4.0)")
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

func TestRunUpgradeReleaseFromDevBuildUsesTheRedirect(t *testing.T) {
	orig := buildinfo.Version
	buildinfo.Version = "dev"
	update.ClearCache()
	upgradeRelease, upgradeVerbose = true, true
	t.Cleanup(func() {
		buildinfo.Version = orig
		upgradeRelease, upgradeVerbose = false, false
		upgradeCmd.SilenceUsage = false
		update.ClearCache()
	})
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/lsegal/aviary/releases/latest" {
			http.Redirect(w, r, "/lsegal/aviary/releases/tag/v1.5.0", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	t.Setenv(update.DownloadBaseEnv, srv.URL)

	r, w, err := os.Pipe()
	require.NoError(t, err)
	stdout := os.Stdout
	os.Stdout = w
	runErr := runUpgrade(upgradeCmd, nil)
	os.Stdout = stdout
	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)

	// The fake host has no checksums, so the install stops before touching
	// the test binary.
	require.Error(t, runErr)
	assert.Contains(t, string(out), "Running a dev build")
	assert.Contains(t, string(out), "  - ", "--verbose lists the detection reasons")
	assert.Contains(t, string(out), "The latest release is v1.5.0.")
	require.NotEmpty(t, paths)
	assert.Equal(t, "/lsegal/aviary/releases/latest", paths[0])
	assert.Equal(t, "/lsegal/aviary/releases/download/v1.5.0/checksums.txt", paths[len(paths)-1], "the release is installed, not the checkout pulled")
	for _, p := range paths {
		assert.NotContains(t, p, "/repos/", "no GitHub REST API calls")
	}
}
