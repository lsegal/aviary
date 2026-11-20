package server

import (
	"context"
	"errors"
	"net/http"
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
	buildinfo.Version = current
	gitCheckout = func() string { return "" }
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

func TestSafeRestart_GitModePullsWithoutRestart(t *testing.T) {
	srv, p, log := setupSafeRestart(t, "dev", update.CheckResult{})
	p.setJobs([]saferestart.Job{{Source: "test", ID: "job-1"}})
	gitCheckout = func() string { return "/src/aviary" }
	gitPull = func(_ context.Context, dir string) (update.GitPullResult, error) {
		*log = append(*log, "pull "+dir)
		return update.GitPullResult{From: "aaa1111", To: "bbb2222", Commits: 1, HotReload: true}, nil
	}

	st, err := srv.requestRestart(context.Background(), saferestart.Request{When: saferestart.WhenNow, Upgrade: true})
	if err != nil {
		t.Fatalf("requestRestart: %v", err)
	}
	if !st.Noop || !strings.Contains(st.Message, "Pulled 1 commit") || !strings.Contains(st.Message, "Hot reload") {
		t.Fatalf("status = %+v, want a no-restart pull message", st)
	}
	if got := strings.Join(*log, "|"); got != "pull /src/aviary" {
		t.Fatalf("log = %q, want only the pull (no install, checkpoint, or restart)", got)
	}
	if srv.RestartStatus().Phase != saferestart.PhaseIdle || saferestart.Held() {
		t.Fatalf("restart status = %+v, want idle and not holding jobs", srv.RestartStatus())
	}
	select {
	case <-srv.reexecCh:
		t.Fatal("git-mode update requested a re-exec")
	case <-time.After(50 * time.Millisecond):
	}
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
