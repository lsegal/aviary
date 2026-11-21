package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/buildinfo"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/saferestart"
	"github.com/lsegal/aviary/internal/store"
	"github.com/lsegal/aviary/internal/update"
)

// fakeParticipant records the safe-restart calls it receives.
type fakeParticipant struct {
	mu      sync.Mutex
	jobs    []saferestart.Job
	held    []bool
	markers []saferestart.Marker
	log     *[]string
}

func (p *fakeParticipant) RunningJobs() []saferestart.Job {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]saferestart.Job(nil), p.jobs...)
}

func (p *fakeParticipant) HoldNewJobs(hold bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.held = append(p.held, hold)
}

func (p *fakeParticipant) Checkpoint(_ context.Context, m saferestart.Marker) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.markers = append(p.markers, m)
	if p.log != nil {
		*p.log = append(*p.log, "checkpoint")
	}
	p.jobs = nil
	return nil
}

func (p *fakeParticipant) setJobs(jobs []saferestart.Job) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.jobs = jobs
}

func (p *fakeParticipant) snapshot() ([]bool, []saferestart.Marker) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]bool(nil), p.held...), append([]saferestart.Marker(nil), p.markers...)
}

func setupSafeRestart(t *testing.T, current string, check update.CheckResult) (*Server, *fakeParticipant, *[]string) {
	t.Helper()
	resetSlogForTest()
	store.SetDataDir(t.TempDir())
	origVersion, origCheck, origInstall, origExe, origPoll := buildinfo.Version, versionCheck, installRelease, executablePath, idlePoll
	origCheckout, origPull := gitCheckout, gitPull
	origPlan, origHotDir, origHotGen, origWait, origHotWait, origRefresh := planDevReload, hotReloadDir, hotReloadGen, waitHotReload, hotReloadWait, refreshAfterHotReload
	buildinfo.Version = current
	gitCheckout = func() string { return "" }
	hotReloadDir = func() string { return "" }
	planDevReload = func(_ context.Context, _, _, _ string, hot bool) (update.DevReloadPlan, error) {
		if hot {
			return update.DevReloadPlan{GoChanged: true}, nil
		}
		return update.DevReloadPlan{GoChanged: true, RestartReason: "forgo hot reload is not active"}, nil
	}
	hotReloadWait = update.HotReloadWait{Poll: 5 * time.Millisecond, Timeout: 2 * time.Second, Answer: time.Second, Settle: 20 * time.Millisecond}
	refreshAfterHotReload = func() {}
	versionCheck = func(context.Context, *http.Client) (update.CheckResult, error) { return check, nil }
	var log []string
	installRelease = func(_ context.Context, opts update.InstallOptions) (update.InstallResult, error) {
		log = append(log, "install "+opts.Version+" "+opts.TargetPath)
		return update.InstallResult{Version: opts.Version}, nil
	}
	executablePath = func() (string, error) { return "/opt/aviary/bin/aviary", nil }
	idlePoll = 10 * time.Millisecond
	p := &fakeParticipant{log: &log}
	saferestart.Register("test", p)
	saferestart.RegisterSkillsUpdater("test", func(_ context.Context, version string) error {
		log = append(log, "skills "+version)
		return nil
	})
	t.Cleanup(func() {
		buildinfo.Version, versionCheck, installRelease, executablePath, idlePoll = origVersion, origCheck, origInstall, origExe, origPoll
		gitCheckout, gitPull = origCheckout, origPull
		planDevReload, hotReloadDir, hotReloadGen, waitHotReload, hotReloadWait, refreshAfterHotReload = origPlan, origHotDir, origHotGen, origWait, origHotWait, origRefresh
		saferestart.Unregister("test")
		saferestart.UnregisterSkillsUpdater("test")
		saferestart.Hold(false)
		store.SetDataDir("")
	})
	srv := New(&config.Config{}, "tok")
	srv.runCtx = context.Background()
	return srv, p, &log
}

func waitReexec(t *testing.T, srv *Server) {
	t.Helper()
	select {
	case <-srv.reexecCh:
	case <-time.After(2 * time.Second):
		t.Fatal("safe restart did not request a re-exec")
	}
}

func TestSafeRestart_UpgradeNowCheckpointsRunningJobs(t *testing.T) {
	srv, p, log := setupSafeRestart(t, "1.2.3", update.CheckResult{CurrentVersion: "1.2.3", LatestVersion: "v1.3.0", UpgradeAvailable: true})
	job := saferestart.Job{Source: "projects", ID: "o/r#1", Title: "Fix it"}
	p.setJobs([]saferestart.Job{job})

	st, err := srv.requestRestart(context.Background(), saferestart.Request{Upgrade: true})
	require.NoError(t, err)
	assert.Equal(t, saferestart.PhaseWaiting, st.Phase)
	assert.Equal(t, "v1.3.0", st.Version)
	assert.Equal(t, []saferestart.Job{job}, st.Jobs, "the dialog lists the jobs it will stop")

	waitReexec(t, srv)
	assert.Equal(t, []string{"install v1.3.0 /opt/aviary/bin/aviary", "skills v1.3.0", "checkpoint"}, *log,
		"replace the binary, update skills, then checkpoint")
	assert.Equal(t, "/opt/aviary/bin/aviary", srv.ReexecPath())

	held, markers := p.snapshot()
	assert.Equal(t, []bool{true}, held)
	require.Len(t, markers, 1)
	assert.Equal(t, "upgrade", markers[0].Reason)
	assert.Equal(t, "v1.3.0", markers[0].ToVersion)
	assert.Equal(t, []saferestart.Job{job}, markers[0].Jobs)

	marker, ok := saferestart.TakeMarker()
	require.True(t, ok, "the restart marker survives for the next process")
	assert.Equal(t, []saferestart.Job{job}, marker.Jobs)
	assert.Contains(t, marker.Message(), "Aviary restarted")
}

func TestSafeRestart_IdleHoldsNewJobsUntilJobsFinish(t *testing.T) {
	srv, p, log := setupSafeRestart(t, "1.2.3", update.CheckResult{})
	p.setJobs([]saferestart.Job{{Source: "projects", ID: "o/r#2"}})

	st, err := srv.requestRestart(context.Background(), saferestart.Request{When: saferestart.WhenIdle})
	require.NoError(t, err)
	assert.Equal(t, saferestart.PhaseWaiting, st.Phase)
	assert.True(t, st.Held, "new jobs are held while waiting")

	select {
	case <-srv.reexecCh:
		t.Fatal("restarted while a job was still running")
	case <-time.After(50 * time.Millisecond):
	}
	assert.Empty(t, *log)

	p.setJobs(nil)
	waitReexec(t, srv)
	assert.Equal(t, []string{"checkpoint"}, *log, "a plain restart neither installs nor updates skills")
}

func TestSafeRestart_CancelReleasesHeldJobs(t *testing.T) {
	srv, p, _ := setupSafeRestart(t, "1.2.3", update.CheckResult{})
	p.setJobs([]saferestart.Job{{Source: "projects", ID: "o/r#3"}})

	_, err := srv.requestRestart(context.Background(), saferestart.Request{When: saferestart.WhenIdle})
	require.NoError(t, err)
	st, err := srv.requestRestart(context.Background(), saferestart.Request{Cancel: true})
	require.NoError(t, err)
	assert.Equal(t, saferestart.PhaseIdle, st.Phase)
	assert.False(t, st.Held)

	p.setJobs(nil)
	select {
	case <-srv.reexecCh:
		t.Fatal("a canceled restart still ran")
	case <-time.After(50 * time.Millisecond):
	}
	held, _ := p.snapshot()
	assert.Equal(t, []bool{true, false}, held)
}

func TestSafeRestart_UpgradeIsNoopWhenCurrent(t *testing.T) {
	srv, p, log := setupSafeRestart(t, "1.3.0", update.CheckResult{CurrentVersion: "1.3.0", LatestVersion: "v1.3.0"})

	st, err := srv.requestRestart(context.Background(), saferestart.Request{Upgrade: true})
	require.NoError(t, err)
	assert.True(t, st.Noop)
	assert.Contains(t, st.Message, "already up to date")

	for _, version := range []string{"1.3.0", "v1.3.0"} {
		st, err = srv.requestRestart(context.Background(), saferestart.Request{Upgrade: true, Version: version})
		require.NoError(t, err)
		assert.True(t, st.Noop, version)
	}
	assert.Empty(t, *log)
	held, _ := p.snapshot()
	assert.Empty(t, held, "a no-op upgrade does not hold jobs")
}

func TestSafeRestart_InstallFailureResumesWork(t *testing.T) {
	srv, p, _ := setupSafeRestart(t, "1.2.3", update.CheckResult{CurrentVersion: "1.2.3", LatestVersion: "v1.3.0", UpgradeAvailable: true})
	installRelease = func(context.Context, update.InstallOptions) (update.InstallResult, error) {
		return update.InstallResult{}, errors.New("download failed")
	}

	_, err := srv.requestRestart(context.Background(), saferestart.Request{Upgrade: true, Version: "1.3.0"})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return srv.RestartStatus().Error != "" }, 2*time.Second, 10*time.Millisecond)

	st := srv.RestartStatus()
	assert.Equal(t, saferestart.PhaseIdle, st.Phase)
	assert.Contains(t, st.Error, "download failed")
	assert.False(t, st.Held)
	held, markers := p.snapshot()
	assert.Equal(t, []bool{true, false}, held)
	assert.Empty(t, markers, "jobs are not checkpointed when the install fails")
}

func TestSafeRestart_RejectsUnknownWhen(t *testing.T) {
	srv, _, _ := setupSafeRestart(t, "1.2.3", update.CheckResult{})
	_, err := srv.requestRestart(context.Background(), saferestart.Request{When: "later"})
	assert.ErrorContains(t, err, `when must be "now" or "idle"`)
}

// A pull leaves the backend on its old code until it restarts, so under the
// dev supervisor the pull is followed by a safe restart that rebuilds it.
func TestSafeRestart_GitModeRestartsOntoPulledCode(t *testing.T) {
	srv, p, log := setupSafeRestart(t, "dev", update.CheckResult{})
	job := saferestart.Job{Source: "test", ID: "job-1"}
	p.setJobs([]saferestart.Job{job})
	gitCheckout = func() string { return "/src/aviary" }
	gitPull = func(_ context.Context, dir string) (update.GitPullResult, error) {
		*log = append(*log, "pull "+dir)
		return update.GitPullResult{From: "aaa1111", To: "bbb2222", Commits: 1, Supervised: true}, nil
	}

	st, err := srv.requestRestart(context.Background(), saferestart.Request{When: saferestart.WhenNow, Upgrade: true})
	require.NoError(t, err)
	assert.False(t, st.Noop)
	assert.Equal(t, saferestart.PhaseWaiting, st.Phase)
	assert.Equal(t, []saferestart.Job{job}, st.Jobs, "the dialog lists the jobs the restart stops")
	assert.Contains(t, st.Message, "Pulled 1 commit")
	assert.Contains(t, st.Message, "restarts")

	waitReexec(t, srv)
	assert.Equal(t, []string{"pull /src/aviary", "checkpoint"}, *log, "the pull is followed by a checkpointed restart, never a release install")
	_, markers := p.snapshot()
	require.Len(t, markers, 1)
	assert.Equal(t, []saferestart.Job{job}, markers[0].Jobs)
}

func TestSafeRestart_GitModeUnsupervisedSaysRestartNeeded(t *testing.T) {
	srv, _, log := setupSafeRestart(t, "dev", update.CheckResult{})
	gitCheckout = func() string { return "/src/aviary" }
	gitPull = func(_ context.Context, dir string) (update.GitPullResult, error) {
		*log = append(*log, "pull "+dir)
		return update.GitPullResult{From: "aaa1111", To: "bbb2222", Commits: 1}, nil
	}

	st, err := srv.requestRestart(context.Background(), saferestart.Request{When: saferestart.WhenNow, Upgrade: true})
	require.NoError(t, err)
	assert.True(t, st.Noop)
	assert.Contains(t, st.Message, "Restart Aviary to run the new code (forgo hot reload is not active).")
	assert.Equal(t, []string{"pull /src/aviary"}, *log, "re-executing would only run the old binary again")
	assert.Equal(t, saferestart.PhaseIdle, srv.RestartStatus().Phase)
	assert.False(t, saferestart.Held())
	select {
	case <-srv.reexecCh:
		t.Fatal("an unsupervised git-mode update requested a re-exec")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSafeRestart_GitModeUpToDateDoesNotRestart(t *testing.T) {
	srv, _, _ := setupSafeRestart(t, "dev", update.CheckResult{})
	gitCheckout = func() string { return "/src/aviary" }
	gitPull = func(context.Context, string) (update.GitPullResult, error) {
		return update.GitPullResult{From: "bbb2222", To: "bbb2222", Supervised: true}, nil
	}

	st, err := srv.requestRestart(context.Background(), saferestart.Request{Upgrade: true})
	require.NoError(t, err)
	assert.True(t, st.Noop)
	assert.Contains(t, st.Message, "already up to date")
	assert.Equal(t, saferestart.PhaseIdle, srv.RestartStatus().Phase)
}

func TestSafeRestart_GitModeRefusalIsReported(t *testing.T) {
	srv, _, log := setupSafeRestart(t, "dev", update.CheckResult{})
	gitCheckout = func() string { return "/src/aviary" }
	gitPull = func(context.Context, string) (update.GitPullResult, error) {
		return update.GitPullResult{}, errors.New("not updating: the checkout at /src/aviary has uncommitted changes")
	}

	_, err := srv.requestRestart(context.Background(), saferestart.Request{Upgrade: true})
	if err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("err = %v, want the dirty-tree reason", err)
	}
	if len(*log) != 0 || saferestart.Held() {
		t.Fatalf("log = %v held = %v, want nothing done", *log, saferestart.Held())
	}
}

// runUpdater runs the self-updater until the test ends.
func runUpdater(t *testing.T, srv *Server, auto bool) {
	t.Helper()
	srv.cfg.Server.AutoUpdate = &auto
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		srv.runSelfUpdater(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

func gitModeCheck(behind int) update.CheckResult {
	return update.CheckResult{
		CurrentVersion:   "9af6a62",
		LatestVersion:    "b9a9522",
		UpgradeAvailable: behind > 0,
		UpgradeSupported: true,
		Git:              &update.GitState{Dir: "/src/aviary", Branch: "main", Upstream: "origin/main", Behind: behind},
	}
}

func TestSelfUpdater_GitModePullsWithJobsRunning(t *testing.T) {
	srv, p, _ := setupSafeRestart(t, "dev", update.CheckResult{})
	p.setJobs([]saferestart.Job{{Source: "test", ID: "job-1"}, {Source: "test", ID: "job-2"}})
	gitCheckout = func() string { return "/src/aviary" }
	var mu sync.Mutex
	behind, pulls := 2, 0
	versionCheck = func(context.Context, *http.Client) (update.CheckResult, error) {
		mu.Lock()
		defer mu.Unlock()
		return gitModeCheck(behind), nil
	}
	gitPull = func(context.Context, string) (update.GitPullResult, error) {
		mu.Lock()
		defer mu.Unlock()
		pulls++
		behind = 0
		return update.GitPullResult{From: "9af6a62", To: "b9a9522", Commits: 2}, nil
	}
	runUpdater(t, srv, true)

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return pulls == 1
	}, 2*time.Second, 5*time.Millisecond, "the update pulls while jobs are still running")
	require.Eventually(t, func() bool { return srv.RestartStatus().AutoUpdate == nil }, 2*time.Second, 5*time.Millisecond)
	assert.Len(t, p.RunningJobs(), 2, "no job had to finish first")
}

func TestSelfUpdater_ReleaseModeRestartsWithJobsRunning(t *testing.T) {
	check := update.CheckResult{CurrentVersion: "1.2.3", LatestVersion: "v1.3.0", UpgradeAvailable: true, UpgradeSupported: true}
	srv, p, log := setupSafeRestart(t, "1.2.3", check)
	jobs := make([]saferestart.Job, 8)
	for i := range jobs {
		jobs[i] = saferestart.Job{Source: "projects", ID: fmt.Sprintf("o/r#%d", i+1)}
	}
	p.setJobs(jobs)
	runUpdater(t, srv, true)

	waitReexec(t, srv)
	assert.Equal(t, []string{"install v1.3.0 /opt/aviary/bin/aviary", "skills v1.3.0", "checkpoint"}, *log,
		"the update installs and checkpoints the running jobs instead of waiting for them")
	_, markers := p.snapshot()
	require.Len(t, markers, 1)
	assert.Equal(t, jobs, markers[0].Jobs)
}

func TestSelfUpdater_ReplacesRestartWaitingForJobs(t *testing.T) {
	srv, p, _ := setupSafeRestart(t, "dev", update.CheckResult{})
	p.setJobs([]saferestart.Job{{Source: "test", ID: "job-1"}})
	gitCheckout = func() string { return "/src/aviary" }
	var mu sync.Mutex
	pulls := 0
	versionCheck = func(context.Context, *http.Client) (update.CheckResult, error) {
		mu.Lock()
		defer mu.Unlock()
		return gitModeCheck(2 - 2*pulls), nil
	}
	gitPull = func(context.Context, string) (update.GitPullResult, error) {
		mu.Lock()
		defer mu.Unlock()
		pulls++
		return update.GitPullResult{From: "9af6a62", To: "b9a9522", Commits: 2}, nil
	}
	_, err := srv.requestRestart(context.Background(), saferestart.Request{When: saferestart.WhenIdle})
	require.NoError(t, err)
	runUpdater(t, srv, true)

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return pulls == 1
	}, 2*time.Second, 5*time.Millisecond, "a restart waiting for jobs does not hold the update back")
}

func TestSelfUpdater_WaitsForRestartInProgress(t *testing.T) {
	srv, _, _ := setupSafeRestart(t, "dev", update.CheckResult{})
	gitCheckout = func() string { return "/src/aviary" }
	versionCheck = func(context.Context, *http.Client) (update.CheckResult, error) { return gitModeCheck(2), nil }
	srv.restart.mu.Lock()
	srv.restart.status = saferestart.Status{Phase: saferestart.PhaseUpdating}
	srv.restart.mu.Unlock()
	runUpdater(t, srv, true)

	require.Eventually(t, func() bool {
		auto := srv.RestartStatus().AutoUpdate
		return auto != nil && auto.State == saferestart.AutoWaitingRestart
	}, 2*time.Second, 5*time.Millisecond)
}

func TestSelfUpdater_ReportsFailedPull(t *testing.T) {
	srv, _, _ := setupSafeRestart(t, "dev", update.CheckResult{})
	gitCheckout = func() string { return "/src/aviary" }
	versionCheck = func(context.Context, *http.Client) (update.CheckResult, error) { return gitModeCheck(2), nil }
	gitPull = func(context.Context, string) (update.GitPullResult, error) {
		return update.GitPullResult{}, errors.New("git pull --ff-only --quiet: exit status 1: fatal: unable to access remote")
	}
	runUpdater(t, srv, true)

	require.Eventually(t, func() bool {
		auto := srv.RestartStatus().AutoUpdate
		return auto != nil && strings.Contains(auto.Error, "unable to access remote")
	}, 2*time.Second, 5*time.Millisecond, "a failed pull is reported with its error")

	// The failure stays visible while the next attempt waits on a restart.
	srv.setAutoUpdate(nil)
	srv.setAutoUpdate(&saferestart.AutoUpdate{Version: "b9a9522", Error: "boom"})
	srv.setAutoUpdateState("b9a9522", saferestart.AutoWaitingRestart)
	auto := srv.RestartStatus().AutoUpdate
	require.NotNil(t, auto)
	assert.Equal(t, "boom", auto.Error)
	assert.Equal(t, saferestart.AutoWaitingRestart, auto.State)
}

func TestSelfUpdater_OffLeavesUpdateAlone(t *testing.T) {
	srv, _, _ := setupSafeRestart(t, "dev", update.CheckResult{})
	gitCheckout = func() string { return "/src/aviary" }
	checked := make(chan struct{}, 1)
	versionCheck = func(context.Context, *http.Client) (update.CheckResult, error) {
		select {
		case checked <- struct{}{}:
		default:
		}
		return gitModeCheck(2), nil
	}
	gitPull = func(context.Context, string) (update.GitPullResult, error) {
		t.Error("pulled with auto_update off")
		return update.GitPullResult{}, nil
	}
	runUpdater(t, srv, false)

	<-checked
	time.Sleep(50 * time.Millisecond)
	assert.Nil(t, srv.RestartStatus().AutoUpdate)
}

func TestAutoUpdateOnDefaultsOn(t *testing.T) {
	on, off := true, false
	srv := &Server{cfg: &config.Config{}}
	if !srv.autoUpdateOn() {
		t.Fatal("a config without auto_update should update automatically")
	}
	srv.cfg.Server.AutoUpdate = &on
	if !srv.autoUpdateOn() {
		t.Fatal("auto_update: true should update automatically")
	}
	srv.cfg.Server.AutoUpdate = &off
	if srv.autoUpdateOn() {
		t.Fatal("auto_update: false should not update automatically")
	}
}

// syncBuffer collects log output written from several goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func captureLogs(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	t.Cleanup(resetSlogForTest)
	return buf
}

// setupHotReload runs the dev update under a fake forgo control directory
// whose next reload generation answers with the files in reply.
func setupHotReload(t *testing.T, log *[]string, reply map[string]string) {
	t.Helper()
	hot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(hot, "agent"), []byte("pid 1\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(hot, "link-3"), []byte("patch 1 2\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(hot, "resp-3"), []byte("ok\n"), 0o600))
	t.Setenv(update.HotReloadEnv, hot)
	hotReloadDir, hotReloadGen, waitHotReload = update.HotReloadDir, update.HotReloadGeneration, update.WaitHotReload
	refreshAfterHotReload = func() { *log = append(*log, "refresh") }
	gitCheckout = func() string { return "/src/aviary" }
	gitPull = func(_ context.Context, dir string) (update.GitPullResult, error) {
		*log = append(*log, "pull "+dir)
		// forgo reloads once the pull changes the sources.
		go func() {
			time.Sleep(20 * time.Millisecond)
			for name, body := range reply {
				_ = os.WriteFile(filepath.Join(hot, name), []byte(body), 0o600)
			}
		}()
		return update.GitPullResult{From: "aaa1111", To: "bbb2222", Commits: 2, Supervised: true, HotReload: true}, nil
	}
}

func waitForLog(t *testing.T, logs *syncBuffer, line string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(logs.String(), line) {
		if time.Now().After(deadline) {
			t.Fatalf("log never showed %q:\n%s", line, logs.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Under forgo, a pull of Go changes goes live by hot reload: running jobs keep
// running, nothing is held or checkpointed, and the backend never re-execs.
func TestSafeRestart_GitModeHotReloadDoesNotRestart(t *testing.T) {
	srv, p, log := setupSafeRestart(t, "dev", update.CheckResult{})
	logs := captureLogs(t)
	job := saferestart.Job{Source: "projects", ID: "o/r#1"}
	p.setJobs([]saferestart.Job{job})
	setupHotReload(t, log, map[string]string{"link-4": "patch 1 2\n", "resp-4": "ok\n"})

	st, err := srv.requestRestart(context.Background(), saferestart.Request{When: saferestart.WhenNow, Upgrade: true})
	require.NoError(t, err)
	assert.True(t, st.Noop)
	assert.Contains(t, st.Message, "hot-reloads the new code")
	assert.False(t, saferestart.Held(), "a hot reload holds no jobs")

	waitForLog(t, logs, "dev update applied via hot reload")
	assert.Contains(t, logs.String(), "generation=4", "the verdict is about the reload after the pull, not an earlier one")
	assert.NotContains(t, logs.String(), "needs a full restart")
	assert.NotContains(t, logs.String(), "safe restart")
	select {
	case <-srv.reexecCh:
		t.Fatal("a hot-reloaded dev update requested a re-exec")
	case <-time.After(50 * time.Millisecond):
	}
	assert.Equal(t, []string{"pull /src/aviary", "refresh"}, *log, "no checkpoint; MCP tools are registered again")
	held, markers := p.snapshot()
	assert.Empty(t, held)
	assert.Empty(t, markers)
	assert.Equal(t, saferestart.PhaseIdle, srv.RestartStatus().Phase)
}

// When forgo refuses the reload, the backend would keep running old code, so
// the existing safe restart runs and the log says why.
func TestSafeRestart_GitModeRefusedHotReloadRestarts(t *testing.T) {
	srv, p, log := setupSafeRestart(t, "dev", update.CheckResult{})
	logs := captureLogs(t)
	job := saferestart.Job{Source: "projects", ID: "o/r#1"}
	p.setJobs([]saferestart.Job{job})
	setupHotReload(t, log, map[string]string{"link-4": "refuse the type server.Server changed shape\n"})

	st, err := srv.requestRestart(context.Background(), saferestart.Request{When: saferestart.WhenNow, Upgrade: true})
	require.NoError(t, err)
	assert.True(t, st.Noop)

	waitReexec(t, srv)
	assert.Contains(t, logs.String(), "dev update needs a full restart")
	assert.Contains(t, logs.String(), "the type server.Server changed shape")
	assert.NotContains(t, logs.String(), "applied via hot reload")
	assert.Equal(t, []string{"pull /src/aviary", "checkpoint"}, *log)
	_, markers := p.snapshot()
	require.Len(t, markers, 1)
	assert.Equal(t, []saferestart.Job{job}, markers[0].Jobs)
}

// A change hot reload cannot apply, such as go.mod, restarts right away
// without waiting for forgo.
func TestSafeRestart_GitModeGoModChangeRestarts(t *testing.T) {
	srv, _, log := setupSafeRestart(t, "dev", update.CheckResult{})
	logs := captureLogs(t)
	setupHotReload(t, log, nil)
	planDevReload = func(context.Context, string, string, string, bool) (update.DevReloadPlan, error) {
		return update.DevReloadPlan{GoChanged: true, RestartReason: "go.mod changed"}, nil
	}

	st, err := srv.requestRestart(context.Background(), saferestart.Request{When: saferestart.WhenNow, Upgrade: true})
	require.NoError(t, err)
	assert.Contains(t, st.Message, "restarts to build and run the new code (go.mod changed)")
	waitReexec(t, srv)
	assert.Contains(t, logs.String(), `dev update needs a full restart" component=update reason="go.mod changed"`)
}

// A pull that changes no Go code needs neither a reload nor a restart.
func TestSafeRestart_GitModeNoBackendChange(t *testing.T) {
	srv, _, log := setupSafeRestart(t, "dev", update.CheckResult{})
	setupHotReload(t, log, nil)
	planDevReload = func(context.Context, string, string, string, bool) (update.DevReloadPlan, error) {
		return update.DevReloadPlan{}, nil
	}

	st, err := srv.requestRestart(context.Background(), saferestart.Request{When: saferestart.WhenNow, Upgrade: true})
	require.NoError(t, err)
	assert.True(t, st.Noop)
	assert.Contains(t, st.Message, "No backend code changed.")
	select {
	case <-srv.reexecCh:
		t.Fatal("a pull without Go changes requested a re-exec")
	case <-time.After(100 * time.Millisecond):
	}
	assert.Equal(t, []string{"pull /src/aviary"}, *log)
}
