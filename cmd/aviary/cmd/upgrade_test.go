package cmd

import (
	"context"
	"encoding/json"
	"errors"
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

// A release server already on the target is not reinstalled or restarted:
// only this binary is behind, so only it is upgraded.
func TestRunUpgradeLeavesAServerAlreadyOnTheTarget(t *testing.T) {
	stubLatestRelease(t, "v0.7.3", "v0.7.5")
	installs, _ := stubInstall(t)
	exe := executable(t)
	other := filepath.Join(t.TempDir(), "aviary")
	f := startFakeServer(t, &runningServer{CheckResult: update.CheckResult{InstallType: update.InstallRelease, CurrentVersion: "v0.7.5"}, Executable: other})

	out, err := captureUpgrade(t)
	require.NoError(t, err)
	assert.Equal(t, []string{"v0.7.5 " + exe}, *installs, "only the binary that is behind is replaced")
	assert.Contains(t, out, "Aviary upgraded to v0.7.5.")
	assert.Contains(t, out, "The running Aviary server is already at v0.7.5.")
	assert.NotContains(t, f.tools(), "server_restart")
}

// stubPrompt answers the doctor's upgrade prompt with answer, running
// onPrompt first, and records the prompts asked.
func stubPrompt(t *testing.T, answer bool, onPrompt func()) *[]string {
	t.Helper()
	orig := promptYesNo
	t.Cleanup(func() { promptYesNo = orig })
	asked := &[]string{}
	promptYesNo = func(prompt string) (bool, error) {
		*asked = append(*asked, prompt)
		if onPrompt != nil {
			onPrompt()
		}
		return answer, nil
	}
	return asked
}

// captureStdout runs fn and returns its stdout.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	stdout := os.Stdout
	os.Stdout = w
	runErr := fn()
	os.Stdout = stdout
	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(out), runErr
}

// The doctor judges the install aviary upgrade acts on, so an install
// already on the latest release is reported up to date with no prompt.
func TestDoctorVersionCheckUpToDateDoesNotPrompt(t *testing.T) {
	stubLatestRelease(t, "v0.7.5", "v0.7.5")
	installs, _ := stubInstall(t)
	startFakeServer(t, &runningServer{CheckResult: update.CheckResult{InstallType: update.InstallRelease, CurrentVersion: "v0.7.5"}, Executable: executable(t)})
	asked := stubPrompt(t, true, nil)

	out, err := captureStdout(t, maybeRunDoctorVersionCheck)
	require.NoError(t, err)
	assert.Contains(t, out, "Version check: [OK] v0.7.5")
	assert.Empty(t, *asked)
	assert.Empty(t, *installs)
}

// An older binary than the running server is named in the prompt, and
// accepting upgrades that binary instead of answering "already at" from the
// server's version.
func TestDoctorVersionCheckUpgradesTheBinaryItPromptedFor(t *testing.T) {
	stubLatestRelease(t, "v0.7.3", "v0.7.5")
	installs, _ := stubInstall(t)
	exe := executable(t)
	f := startFakeServer(t, &runningServer{CheckResult: update.CheckResult{InstallType: update.InstallRelease, CurrentVersion: "v0.7.5"}, Executable: filepath.Join(t.TempDir(), "aviary")})
	asked := stubPrompt(t, true, nil)

	out, err := captureStdout(t, maybeRunDoctorVersionCheck)
	require.NoError(t, err)
	assert.Contains(t, out, "Version check: [WARN] upgrade available v0.7.3 -> v0.7.5 for "+exe+" (the running server is at v0.7.5)")
	assert.Equal(t, []string{"Upgrade to v0.7.5 now? [y/N]: "}, *asked)
	assert.Contains(t, out, "Upgrading v0.7.3 → v0.7.5")
	assert.Equal(t, []string{"v0.7.5 " + exe}, *installs)
	assert.Contains(t, out, "Aviary upgraded to v0.7.5.")
	assert.NotContains(t, f.tools(), "server_upgrade")
	assert.NotContains(t, f.tools(), "server_restart")
}

// Accepting the prompt installs the version it named, even when a newer
// release appears before the upgrade runs.
func TestDoctorVersionCheckInstallsThePromptedVersion(t *testing.T) {
	stubLatestRelease(t, "v0.7.3", "v0.7.5")
	installs, _ := stubInstall(t)
	noServer(t)
	stubPrompt(t, true, func() { stubLatestRelease(t, "v0.7.3", "v0.7.6") })

	out, err := captureStdout(t, maybeRunDoctorVersionCheck)
	require.NoError(t, err)
	assert.Contains(t, out, "Upgrading v0.7.3 → v0.7.5")
	assert.Equal(t, []string{"v0.7.5 " + executable(t)}, *installs)
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
	assert.Contains(t, otherServerNote(update.CheckResult{InstallType: update.InstallSource}), "dev build from its checkout")
	assert.Contains(t, otherServerNote(update.CheckResult{InstallType: update.InstallRelease, CurrentVersion: "0.7.3"}), "release install (v0.7.3)")
	assert.Equal(t, "The running Aviary server is a different kind of install, so it was not changed.", otherServerNote(update.CheckResult{InstallType: update.InstallDev}))
}

func TestUpgradeMode(t *testing.T) {
	t.Cleanup(func() { upgradeRelease, upgradeSource = false, false })
	tests := []struct {
		release, source bool
		want            update.InstallType
	}{
		{want: ""},
		{release: true, want: update.InstallRelease},
		{source: true, want: update.InstallSource},
	}
	for _, tt := range tests {
		upgradeRelease, upgradeSource = tt.release, tt.source
		assert.Equal(t, tt.want, upgradeMode(), "--release=%v --source=%v", tt.release, tt.source)
	}
}

// stubServer makes findRunningServer report states in turn, repeating the
// last, and returns how many times it was asked.
func stubServer(t *testing.T, states ...func() (runningServer, bool)) *int {
	t.Helper()
	orig := findRunningServer
	t.Cleanup(func() { findRunningServer = orig })
	calls := new(int)
	findRunningServer = func() (runningServer, bool) {
		i := min(*calls, len(states)-1)
		*calls++
		return states[i]()
	}
	return calls
}

func serverState(srv runningServer) func() (runningServer, bool) {
	return func() (runningServer, bool) { return srv, true }
}

func noServerState() (runningServer, bool) { return runningServer{}, false }

// stubRestart records the restarts aviary upgrade asks for.
func stubRestart(t *testing.T) *[]string {
	t.Helper()
	orig := restartServer
	t.Cleanup(func() { restartServer = orig })
	whens := &[]string{}
	restartServer = func(when string) error {
		*whens = append(*whens, when)
		return nil
	}
	return whens
}

func TestApplyUpgradeWithoutACheckoutNeedsAVersion(t *testing.T) {
	installs, _ := stubInstall(t)
	err := applyUpgrade(update.CheckResult{InstallType: update.InstallDev, NoCheckout: true, Message: "no source checkout found"}, "")
	require.EqualError(t, err, "no source checkout found")
	assert.Empty(t, *installs)
}

func TestRunReleaseUpgradeFailures(t *testing.T) {
	behind := update.CheckResult{CurrentVersion: "0.7.3", LatestVersion: "v0.7.5", UpgradeAvailable: true}

	t.Run("no target version", func(t *testing.T) {
		installs, _ := stubInstall(t)
		_, err := captureStdout(t, func() error { return runReleaseUpgrade(update.CheckResult{CurrentVersion: "0.7.3"}, false) })
		require.EqualError(t, err, "unable to determine a target version")
		assert.Empty(t, *installs)
	})

	t.Run("install fails", func(t *testing.T) {
		_, skills := stubInstall(t)
		installRelease = func(string, string) (string, error) { return "", errors.New("checksum mismatch") }
		stubServer(t, noServerState)
		out, err := captureStdout(t, func() error { return runReleaseUpgrade(behind, false) })
		require.EqualError(t, err, "checksum mismatch")
		assert.NotContains(t, out, "Aviary upgraded")
		assert.Empty(t, *skills, "skills are not updated for a failed install")
	})

	t.Run("skills update fails", func(t *testing.T) {
		installs, _ := stubInstall(t)
		updateSkills = func(context.Context, string) error { return errors.New("provider offline") }
		stubServer(t, noServerState)
		out, err := captureStdout(t, func() error { return runReleaseUpgrade(behind, false) })
		require.NoError(t, err, "a skills failure does not fail the upgrade")
		assert.Len(t, *installs, 1)
		assert.Contains(t, out, "Aviary upgraded to v0.7.5.\nUpdating skills for v0.7.5: provider offline\n")
	})
}

func TestRunReleaseUpgradeEmulated(t *testing.T) {
	orig := buildinfo.Version
	buildinfo.Version = "dev"
	require.NoError(t, update.ConfigureEmulation("0.7.3:0.7.5"))
	t.Cleanup(func() {
		require.NoError(t, update.ConfigureEmulation(""))
		buildinfo.Version = orig
	})
	installs, _ := stubInstall(t)
	calls := stubServer(t, noServerState)

	out, err := captureStdout(t, func() error {
		return runReleaseUpgrade(update.CheckResult{CurrentVersion: "0.7.3", LatestVersion: "0.7.5", UpgradeAvailable: true}, false)
	})
	require.NoError(t, err)
	assert.Contains(t, out, "Emulated upgrade to v0.7.5 completed. No files were changed.")
	assert.Empty(t, *installs)
	assert.Zero(t, *calls, "an emulated upgrade leaves the server alone")
}

func TestRunSourceUpgrade(t *testing.T) {
	tests := []struct {
		name string
		// chdir runs the upgrade from inside the checkout with no checkout in
		// the check result.
		chdir   bool
		dirty   bool
		server  func() (runningServer, bool)
		pulled  bool
		want    string
		wantErr string
	}{
		{
			name:   "falls back to the working directory's checkout",
			chdir:  true,
			server: noServerState,
			pulled: true,
		},
		{
			name:    "refuses a checkout with local changes",
			dirty:   true,
			server:  noServerState,
			wantErr: "has uncommitted changes",
		},
		{
			name:    "reports a server it cannot restart",
			server:  serverState(runningServer{Problem: "did not respond"}),
			pulled:  true,
			wantErr: "the running Aviary server did not respond, so it could not be restarted on the pulled code. " + restartHint,
		},
		{
			name:   "leaves a release server alone",
			server: serverState(runningServer{CheckResult: update.CheckResult{InstallType: update.InstallRelease, CurrentVersion: "0.7.5"}}),
			pulled: true,
			want:   "release install (v0.7.5), so it was not changed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkout := checkoutBehindUpstream(t)
			if tt.dirty {
				require.NoError(t, os.WriteFile(filepath.Join(checkout, "go.mod"), []byte("module github.com/lsegal/aviary\n\n// edited\n"), 0o644))
			}
			check := update.CheckResult{InstallType: update.InstallSource, Git: &update.GitState{Dir: checkout}}
			if tt.chdir {
				t.Chdir(checkout)
				check.Git = nil
			}
			stubServer(t, tt.server)
			restarts := stubRestart(t)

			out, err := captureStdout(t, func() error { return runSourceUpgrade(check) })
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				if tt.dirty {
					assert.Contains(t, err.Error(), "updating the source checkout at "+checkout)
				}
			} else {
				require.NoError(t, err)
			}
			if tt.pulled {
				assert.Contains(t, out, "Pulled 1 commit(s) into "+checkout)
			} else {
				assert.NotContains(t, out, "Pulled")
			}
			assert.Contains(t, out, tt.want)
			assert.Empty(t, *restarts, "a server of another kind is never restarted")
		})
	}
}

// pulledCheckout returns the pull result of bringing an Aviary checkout to a
// fetched upstream commit that writes body to file, without pulling.
func pulledCheckout(t *testing.T, file, body string) update.GitPullResult {
	t.Helper()
	checkout := checkoutBehindUpstreamWith(t, file, body)
	gitRun(t, checkout, "fetch", "-q")
	return update.GitPullResult{Dir: checkout, From: "HEAD", To: "origin/main", Commits: 1}
}

func TestApplyPulledCode(t *testing.T) {
	failedReload := update.HotReloadResult{Generation: 1, Reason: "a type changed shape"}
	tests := []struct {
		name         string
		res          func(t *testing.T) update.GitPullResult
		git          update.GitState
		hotDir       bool
		reload       *update.HotReloadResult
		want         string
		wantErr      string
		wantRestarts []string
	}{
		{
			name: "nothing pulled",
			res: func(t *testing.T) update.GitPullResult {
				return update.GitPullResult{Dir: t.TempDir(), To: "abc1234"}
			},
			git:  update.GitState{Supervised: true, HotReload: true},
			want: "is already up to date (abc1234).",
		},
		{
			name: "listing the changes fails under the supervisor",
			res: func(t *testing.T) update.GitPullResult {
				return update.GitPullResult{Dir: t.TempDir(), From: "a", To: "b", Commits: 1}
			},
			git:          update.GitState{Supervised: true, HotReload: true},
			want:         "listing the pulled changes failed",
			wantRestarts: []string{saferestart.WhenNow},
		},
		{
			name: "a restart without the supervisor is left to the user",
			res: func(t *testing.T) update.GitPullResult {
				return update.GitPullResult{Dir: t.TempDir(), From: "a", To: "b", Commits: 1}
			},
			git:  update.GitState{HotReload: true},
			want: "listing the pulled changes failed",
		},
		{
			name: "go changes without a hot reload directory",
			res:  func(t *testing.T) update.GitPullResult { return pulledCheckout(t, "main.go", "package main\n") },
			git:  update.GitState{Supervised: true, HotReload: true},
			want: "The backend hot-reloads the new code",
		},
		{
			name:    "a failed hot reload without the supervisor",
			res:     func(t *testing.T) update.GitPullResult { return pulledCheckout(t, "main.go", "package main\n") },
			git:     update.GitState{HotReload: true},
			hotDir:  true,
			reload:  &failedReload,
			want:    "could not hot-reload the pulled code: a type changed shape.",
			wantErr: "the running Aviary server still runs the old code. " + restartHint,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := waitHotReload
			t.Cleanup(func() { waitHotReload = orig })
			waits := 0
			waitHotReload = func(context.Context, string, int, update.HotReloadWait) update.HotReloadResult {
				waits++
				require.NotNil(t, tt.reload, "no hot reload is awaited")
				return *tt.reload
			}
			restarts := stubRestart(t)
			stubServer(t, serverState(runningServer{}))
			hotDir := ""
			if tt.hotDir {
				hotDir = t.TempDir()
			}
			git := tt.git
			srv := runningServer{CheckResult: update.CheckResult{InstallType: update.InstallSource, Git: &git}}

			out, err := captureStdout(t, func() error {
				return applyPulledCode(context.Background(), srv, tt.res(t), hotDir, 0)
			})
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Contains(t, out, tt.want)
			assert.Equal(t, tt.reload != nil, waits == 1, "hot reload awaited")
			if tt.wantRestarts == nil {
				assert.Empty(t, *restarts)
			} else {
				assert.Equal(t, tt.wantRestarts, *restarts)
			}
		})
	}
}

func TestAwaitServer(t *testing.T) {
	origWait, origPoll := serverRestartWait, serverRestartPoll
	serverRestartWait, serverRestartPoll = 50*time.Millisecond, time.Millisecond
	t.Cleanup(func() { serverRestartWait, serverRestartPoll = origWait, origPoll })
	restarting := runningServer{Restart: saferestart.Status{Phase: saferestart.PhaseUpdating}}
	back := runningServer{Restart: saferestart.Status{Phase: saferestart.PhaseIdle}}

	tests := []struct {
		name      string
		states    []func() (runningServer, bool)
		wantErr   string
		wantPolls int
	}{
		{
			name:      "waits out the restart",
			states:    []func() (runningServer, bool){serverState(restarting), noServerState, serverState(runningServer{Problem: "did not respond"}), serverState(back)},
			wantPolls: 4,
		},
		{
			name:    "a failed restart",
			states:  []func() (runningServer, bool){serverState(runningServer{Restart: saferestart.Status{Phase: saferestart.PhaseIdle, Error: "build failed"}})},
			wantErr: "the running Aviary server failed to restart: build failed",
		},
		{
			name:    "never comes back",
			states:  []func() (runningServer, bool){noServerState},
			wantErr: "the running Aviary server did not come back within 50ms",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			polls := stubServer(t, tt.states...)
			err := awaitServer(func(runningServer) bool { return true })
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantPolls, *polls)
		})
	}
}

func TestDoctorVersionCheckWithoutAnUpgradeToOffer(t *testing.T) {
	tests := []struct {
		name    string
		stub    func(t *testing.T)
		want    string
		wantOut bool
	}{
		{
			name: "check fails",
			stub: func(t *testing.T) {
				orig := buildinfo.Version
				buildinfo.Version = "1.2.3"
				update.ClearCache()
				t.Cleanup(func() {
					buildinfo.Version = orig
					update.ClearCache()
				})
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}))
				t.Cleanup(srv.Close)
				t.Setenv(update.DownloadBaseEnv, srv.URL)
			},
			want: "\nVersion check: [WARN] ",
		},
		{
			name: "latest release is not comparable",
			stub: func(t *testing.T) { stubLatestRelease(t, "v0.7.3", "nightly") },
			want: "\nVersion check: [INFO] ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.stub(t)
			asked := stubPrompt(t, true, nil)
			installs, _ := stubInstall(t)
			out, err := captureStdout(t, maybeRunDoctorVersionCheck)
			require.NoError(t, err, "the doctor reports a version problem without failing")
			assert.True(t, strings.HasPrefix(out, tt.want), "output: %q", out)
			assert.Empty(t, *asked)
			assert.Empty(t, *installs)
		})
	}
}

func TestDoctorVersionCheckPromptAnswers(t *testing.T) {
	tests := []struct {
		name      string
		answer    bool
		promptErr error
	}{
		{name: "declined"},
		{name: "prompt fails", promptErr: errors.New("no console")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubLatestRelease(t, "v0.7.3", "v0.7.5")
			installs, _ := stubInstall(t)
			stubServer(t, noServerState)
			orig := promptYesNo
			t.Cleanup(func() { promptYesNo = orig })
			asked := 0
			promptYesNo = func(string) (bool, error) {
				asked++
				return tt.answer, tt.promptErr
			}

			out, err := captureStdout(t, maybeRunDoctorVersionCheck)
			if tt.promptErr != nil {
				require.ErrorIs(t, err, tt.promptErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, 1, asked)
			assert.Contains(t, out, "Version check: [WARN] upgrade available v0.7.3 -> v0.7.5\n", "no running server, so no binary is named")
			assert.Empty(t, *installs)
			assert.NotContains(t, out, "Upgrading")
		})
	}
}

func TestDoctorUpgradeTargetNamesNothing(t *testing.T) {
	release := update.CheckResult{InstallType: update.InstallRelease, CurrentVersion: "v0.7.3"}
	tests := []struct {
		name   string
		check  update.CheckResult
		server func() (runningServer, bool)
		polls  int
	}{
		{name: "not a release install", check: update.CheckResult{InstallType: update.InstallSource, CurrentVersion: "abc"}, server: serverState(runningServer{CheckResult: update.CheckResult{CurrentVersion: "v0.7.5"}})},
		{name: "no server", check: release, server: noServerState, polls: 1},
		{name: "server with a problem", check: release, server: serverState(runningServer{Problem: "did not respond"}), polls: 1},
		{name: "server at the same version", check: release, server: serverState(runningServer{CheckResult: update.CheckResult{CurrentVersion: "0.7.3"}}), polls: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			polls := stubServer(t, tt.server)
			assert.Empty(t, doctorUpgradeTarget(tt.check))
			assert.Equal(t, tt.polls, *polls)
		})
	}
}
