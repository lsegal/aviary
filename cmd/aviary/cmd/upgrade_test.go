package cmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/buildinfo"
	internalmcp "github.com/lsegal/aviary/internal/mcp"
	"github.com/lsegal/aviary/internal/saferestart"
	"github.com/lsegal/aviary/internal/server"
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

// noServer makes runUpgrade find no running server.
func noServer(t *testing.T) {
	t.Helper()
	t.Setenv("AVIARY_PID_FILE", filepath.Join(t.TempDir(), "aviary.pid"))
}

// fakeServer is a running Aviary server's MCP endpoint that records the
// tools aviary upgrade calls.
type fakeServer struct {
	mu    sync.Mutex
	state runningServer
	calls []string
	whens []string
	// onRestart is what the server reports once server_restart ran; nil
	// leaves the state as it was.
	onRestart func(*runningServer)
	// restartErr makes server_restart fail with this error.
	restartErr string
}

func (f *fakeServer) tools() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// startFakeServer serves state as the running server's MCP endpoint and
// points the CLI's dispatcher and PID file at it. A nil state serves a server
// that does not respond.
func startFakeServer(t *testing.T, state *runningServer) *fakeServer {
	t.Helper()
	f := &fakeServer{}
	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	if state != nil {
		f.state = *state
		s := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fake-aviary"}, nil)
		record := func(name string) {
			f.mu.Lock()
			f.calls = append(f.calls, name)
			f.mu.Unlock()
		}
		sdkmcp.AddTool(s, &sdkmcp.Tool{Name: "server_version_check"}, func(context.Context, *sdkmcp.CallToolRequest, struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
			record("server_version_check")
			f.mu.Lock()
			out, err := json.Marshal(f.state)
			f.mu.Unlock()
			require.NoError(t, err)
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(out)}}}, struct{}{}, nil
		})
		type restartArgs struct {
			When string `json:"when,omitempty"`
		}
		sdkmcp.AddTool(s, &sdkmcp.Tool{Name: "server_restart"}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args restartArgs) (*sdkmcp.CallToolResult, struct{}, error) {
			record("server_restart")
			f.mu.Lock()
			defer f.mu.Unlock()
			f.whens = append(f.whens, args.When)
			if f.restartErr != "" {
				return &sdkmcp.CallToolResult{IsError: true, Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: f.restartErr}}}, struct{}{}, nil
			}
			if f.onRestart != nil {
				f.onRestart(&f.state)
			}
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: `{"phase":"waiting"}`}}}, struct{}{}, nil
		})
		sdkmcp.AddTool(s, &sdkmcp.Tool{Name: "server_upgrade"}, func(context.Context, *sdkmcp.CallToolRequest, struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
			record("server_upgrade")
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: `{}`}}}, struct{}{}, nil
		})
		handler = sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return s }, &sdkmcp.StreamableHTTPOptions{JSONResponse: true})
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	pidFile := filepath.Join(t.TempDir(), "aviary.pid")
	require.NoError(t, os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644))
	t.Setenv("AVIARY_PID_FILE", pidFile)
	prev := dispatcher
	dispatcher = internalmcp.NewDispatcher(srv.URL, "test-token")
	internalmcp.SetServerChecker(func() bool { return true })
	origWait, origPoll := serverRestartWait, serverRestartPoll
	serverRestartWait, serverRestartPoll = 2*time.Second, 10*time.Millisecond
	t.Cleanup(func() {
		dispatcher = prev
		internalmcp.SetServerChecker(func() bool { running, _, _ := server.IsRunning(); return running })
		serverRestartWait, serverRestartPoll = origWait, origPoll
	})
	return f
}

// stubInstall records the binaries aviary upgrade installs releases over,
// and the skills updates it runs, instead of downloading anything.
func stubInstall(t *testing.T) (installs, skills *[]string) {
	t.Helper()
	origInstall, origSkills := installRelease, updateSkills
	t.Cleanup(func() { installRelease, updateSkills = origInstall, origSkills })
	installs, skills = &[]string{}, &[]string{}
	installRelease = func(version, target string) (string, error) {
		*installs = append(*installs, version+" "+target)
		return version, nil
	}
	updateSkills = func(_ context.Context, version string) error {
		*skills = append(*skills, version)
		return nil
	}
	return installs, skills
}

func executable(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	return exe
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
	noServer(t)

	out, err := captureUpgrade(t)
	require.Error(t, err, "the download is refused")
	assert.Contains(t, out, "Upgrading v0.7.3 → v0.7.5")
	assert.NotContains(t, out, "checkout")
}

// With a release server running, the CLI downloads and installs the release
// itself, then only has the server restart, and waits for it to come back on
// the new version. The server is never asked to upgrade.
func TestRunUpgradeInstallsInTheCLIAndRestartsAReleaseServer(t *testing.T) {
	stubLatestRelease(t, "v0.7.3", "v0.7.5")
	installs, skills := stubInstall(t)
	exe := executable(t)
	f := startFakeServer(t, &runningServer{CheckResult: update.CheckResult{InstallType: update.InstallRelease, CurrentVersion: "v0.7.3"}, Executable: exe})
	f.onRestart = func(st *runningServer) { st.CurrentVersion = "v0.7.5" }

	out, err := captureUpgrade(t)
	require.NoError(t, err)
	assert.Contains(t, out, "Upgrading v0.7.3 → v0.7.5")
	assert.Equal(t, []string{"v0.7.5 " + exe}, *installs, "the CLI installs the release over the binary the server runs")
	assert.Equal(t, []string{"v0.7.5"}, *skills, "the CLI updates skills, as the server's upgrade did")
	assert.Contains(t, out, "Aviary upgraded to v0.7.5.")
	assert.Contains(t, out, "Restarting the running Aviary server on v0.7.5.")
	assert.Contains(t, out, "The Aviary server is running v0.7.5.")
	assert.Equal(t, []string{"now"}, f.whens)
	assert.NotContains(t, f.tools(), "server_upgrade")
	assert.Contains(t, f.tools(), "server_restart")
}

// A server running from another binary is restarted on that binary, so the
// CLI swaps it too.
func TestRunUpgradeSwapsTheBinaryTheServerRuns(t *testing.T) {
	stubLatestRelease(t, "v0.7.3", "v0.7.5")
	installs, _ := stubInstall(t)
	exe := executable(t)
	other := filepath.Join(t.TempDir(), "aviary")
	f := startFakeServer(t, &runningServer{CheckResult: update.CheckResult{InstallType: update.InstallRelease, CurrentVersion: "v0.7.3"}, Executable: other})
	f.onRestart = func(st *runningServer) { st.CurrentVersion = "v0.7.5" }

	_, err := captureUpgrade(t)
	require.NoError(t, err)
	assert.Equal(t, []string{"v0.7.5 " + exe, "v0.7.5 " + other}, *installs)
	assert.NotContains(t, f.tools(), "server_upgrade")
}

// --when idle swaps the binary now and has the server restart on it once no
// jobs are running.
func TestRunUpgradeWhenIdleRestartsLater(t *testing.T) {
	stubLatestRelease(t, "v0.7.3", "v0.7.5")
	installs, _ := stubInstall(t)
	upgradeWhen = saferestart.WhenIdle
	t.Cleanup(func() { upgradeWhen = saferestart.WhenNow })
	f := startFakeServer(t, &runningServer{CheckResult: update.CheckResult{InstallType: update.InstallRelease, CurrentVersion: "v0.7.3"}, Executable: executable(t)})

	out, err := captureUpgrade(t)
	require.NoError(t, err)
	assert.Len(t, *installs, 1)
	assert.Equal(t, []string{"idle"}, f.whens)
	assert.Contains(t, out, "restarts on v0.7.5 once no jobs are running")
	assert.NotContains(t, f.tools(), "server_upgrade")
}

// With no server running, the CLI upgrades the binary and starts nothing.
func TestRunUpgradeWithoutAServer(t *testing.T) {
	stubLatestRelease(t, "v0.7.3", "v0.7.5")
	installs, _ := stubInstall(t)
	noServer(t)

	out, err := captureUpgrade(t)
	require.NoError(t, err)
	assert.Equal(t, []string{"v0.7.5 " + executable(t)}, *installs)
	assert.Contains(t, out, "Aviary upgraded to v0.7.5.")
	assert.NotContains(t, out, "server")
}

// A server that does not respond cannot stop the upgrade: the binary is
// swapped and the user is told the server still runs the old version.
func TestRunUpgradeWithAnUnresponsiveServer(t *testing.T) {
	stubLatestRelease(t, "v0.7.3", "v0.7.5")
	installs, _ := stubInstall(t)
	startFakeServer(t, nil)

	out, err := captureUpgrade(t)
	assert.Equal(t, []string{"v0.7.5 " + executable(t)}, *installs)
	assert.Contains(t, out, "Aviary upgraded to v0.7.5.")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not respond, so it could not be restarted and still runs the old version")
	assert.Contains(t, err.Error(), "aviary stop, then aviary serve")
}

// A server that refuses the restart, or comes back on another version, is
// reported after the binary was swapped.
func TestRunUpgradeReportsAFailedRestart(t *testing.T) {
	stubLatestRelease(t, "v0.7.3", "v0.7.5")
	installs, _ := stubInstall(t)
	f := startFakeServer(t, &runningServer{CheckResult: update.CheckResult{InstallType: update.InstallRelease, CurrentVersion: "v0.7.3"}, Executable: executable(t)})
	f.restartErr = "a restart is already in progress"

	_, err := captureUpgrade(t)
	assert.Len(t, *installs, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "a restart is already in progress")

	f.restartErr = ""
	_, err = captureUpgrade(t)
	require.Error(t, err, "the server came back on the old binary")
	assert.Contains(t, err.Error(), "came back on another version")
	assert.NotContains(t, f.tools(), "server_upgrade")
}

func TestRunUpgradeLeavesADevServerAlone(t *testing.T) {
	chdirIntoCheckout(t)
	stubLatestRelease(t, "v0.7.3", "v0.7.5")
	installs, _ := stubInstall(t)
	f := startFakeServer(t, &runningServer{CheckResult: update.CheckResult{InstallType: update.InstallSource, Git: &update.GitState{Dir: "/src/aviary"}}})

	out, err := captureUpgrade(t)
	require.NoError(t, err)
	assert.Contains(t, out, "Upgrading v0.7.3 → v0.7.5")
	assert.Len(t, *installs, 1, "the release binary installs itself")
	assert.Contains(t, out, "dev build from the source checkout at /src/aviary, so it was not changed")
	assert.Equal(t, []string{"server_version_check"}, f.tools(), "a release binary never restarts a dev server")
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

// checkoutBehindUpstream returns an Aviary git checkout one commit behind its
// upstream.
func checkoutBehindUpstream(t *testing.T) string {
	t.Helper()
	return checkoutBehindUpstreamWith(t, "a.txt", "a")
}

// checkoutBehindUpstreamWith returns an Aviary git checkout one commit behind
// its upstream, whose commit writes body to file.
func checkoutBehindUpstreamWith(t *testing.T, file, body string) string {
	t.Helper()
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
	require.NoError(t, os.WriteFile(filepath.Join(other, file), []byte(body), 0o644))
	gitRun(t, other, "add", file)
	gitRun(t, other, "commit", "-q", "-m", file)
	gitRun(t, other, "push", "-q")
	checkout, err := filepath.EvalSymlinks(filepath.Join(root, "checkout"))
	require.NoError(t, err)
	return checkout
}

func TestRunUpgradeSourceOnAReleaseBinaryPullsTheCheckout(t *testing.T) {
	checkout := checkoutBehindUpstream(t)
	t.Chdir(checkout)

	stubLatestRelease(t, "v0.7.3", "v0.7.5")
	noServer(t)
	upgradeSource = true
	t.Cleanup(func() { upgradeSource = false })

	out, err := captureUpgrade(t)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(out, "Installed release v0.7.3 at "), "the first line names the install: %q", out)
	assert.Contains(t, out, "Fetched origin/main: local ")
	assert.Contains(t, out, "(0 ahead, 1 behind).")
	assert.Contains(t, out, "Pulled 1 commit(s) into "+checkout)
	assert.NotContains(t, out, "v0.7.5", "--source never looks at releases")
	assert.NotContains(t, out, "aviary upgrade --release", "a release binary needs no hint to switch to releases")
}

func TestRunUpgradeSourceOnADevBuildReportsTheBinary(t *testing.T) {
	checkout := checkoutBehindUpstream(t)
	t.Chdir(checkout)

	stubLatestRelease(t, "dev", "v0.7.5")
	noServer(t)
	upgradeSource, upgradeVerbose = true, true
	t.Cleanup(func() { upgradeSource, upgradeVerbose = false, false })

	out, err := captureUpgrade(t)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(out, "Running a dev build"), "the first line names the install: %q", out)
	assert.Contains(t, out, "  - the binary's stamped version \"dev\" is not a release")
	assert.Contains(t, out, "working directory is never used")
	assert.Contains(t, out, "Fetched origin/main: local ")
	assert.Contains(t, out, "Pulled 1 commit(s) into "+checkout)
	assert.Contains(t, out, "cannot be compared with the checkout's HEAD", "a test binary embeds no VCS revision")
	assert.Contains(t, out, "To install the latest release binary instead, run: aviary upgrade --release")
}

// devServerUpgrade runs aviary upgrade --source on a checkout whose upstream
// commit writes body to file, under a running dev server from that checkout.
func devServerUpgrade(t *testing.T, file, body string, git update.GitState) (*fakeServer, string, error) {
	t.Helper()
	checkout := checkoutBehindUpstreamWith(t, file, body)
	t.Chdir(checkout)
	stubLatestRelease(t, "dev", "v0.7.5")
	upgradeSource = true
	t.Cleanup(func() { upgradeSource = false })
	git.Dir = checkout
	f := startFakeServer(t, &runningServer{CheckResult: update.CheckResult{InstallType: update.InstallSource, Git: &git}})
	out, err := captureUpgrade(t)
	return f, out, err
}

// The CLI pulls a dev server's checkout itself, and restarts the server only
// when hot reload cannot run the pulled code.
func TestRunUpgradeSourcePullsInTheCLIAndRestartsADevServer(t *testing.T) {
	f, out, err := devServerUpgrade(t, "go.mod", "module github.com/lsegal/aviary\n\ngo 1.22\n", update.GitState{Supervised: true, HotReload: true, HotReloadDir: t.TempDir()})
	require.NoError(t, err)
	assert.Contains(t, out, "Pulled 1 commit(s) into ")
	assert.Contains(t, out, "Aviary restarts to build and run the new code (go.mod changed).")
	assert.Contains(t, out, "Restarting the running Aviary server on ")
	assert.Equal(t, []string{"now"}, f.whens)
	assert.NotContains(t, f.tools(), "server_upgrade")
}

// Pulled Go code a dev server hot-reloads needs no restart.
func TestRunUpgradeSourceWaitsForHotReload(t *testing.T) {
	orig := waitHotReload
	t.Cleanup(func() { waitHotReload = orig })
	hotDir := t.TempDir()
	var waited string
	waitHotReload = func(_ context.Context, dir string, _ int, _ update.HotReloadWait) update.HotReloadResult {
		waited = dir
		return update.HotReloadResult{Applied: true, Generation: 1}
	}

	f, out, err := devServerUpgrade(t, "main.go", "package main\n", update.GitState{Supervised: true, HotReload: true, HotReloadDir: hotDir})
	require.NoError(t, err)
	assert.Equal(t, hotDir, waited)
	assert.Contains(t, out, "The backend hot-reloads the new code")
	assert.Contains(t, out, "The running Aviary server hot-reloaded the pulled code.")
	assert.Equal(t, []string{"server_version_check"}, f.tools())
}

// A hot reload forgo refuses falls back to a restart under the dev supervisor.
func TestRunUpgradeSourceRestartsWhenHotReloadFails(t *testing.T) {
	orig := waitHotReload
	t.Cleanup(func() { waitHotReload = orig })
	waitHotReload = func(context.Context, string, int, update.HotReloadWait) update.HotReloadResult {
		return update.HotReloadResult{Generation: 1, Reason: "forgo cannot reload without restarting: a type changed shape"}
	}

	f, out, err := devServerUpgrade(t, "main.go", "package main\n", update.GitState{Supervised: true, HotReload: true, HotReloadDir: t.TempDir()})
	require.NoError(t, err)
	assert.Contains(t, out, "could not hot-reload the pulled code: forgo cannot reload without restarting: a type changed shape.")
	assert.Equal(t, []string{"now"}, f.whens)
	assert.NotContains(t, f.tools(), "server_upgrade")
}

func TestOtherServerNote(t *testing.T) {
	assert.Contains(t, otherServerNote(update.CheckResult{InstallType: update.InstallSource, Git: &update.GitState{Dir: "/src/aviary"}}), "dev build from the source checkout at /src/aviary")
	assert.Contains(t, otherServerNote(update.CheckResult{InstallType: update.InstallRelease, CurrentVersion: "0.7.3"}), "release install (v0.7.3)")
}
