package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/saferestart"
	"github.com/lsegal/aviary/internal/update"
)

// ErrReexecRequired is returned by ListenAndServe after a safe restart has
// checkpointed every running job. The caller starts the binary at ReexecPath
// in place of this process.
var ErrReexecRequired = errors.New("server re-exec required")

var (
	versionCheck   = update.Check
	installRelease = update.Install
	gitCheckout    = update.GitCheckout
	gitPull        = update.GitPull
	executablePath = os.Executable
	idlePoll       = 2 * time.Second
	// autoUpdateWait is how long one self-updater round waits for jobs to
	// finish before checking for a newer update.
	autoUpdateWait = update.CheckInterval
)

type restartState struct {
	mu         sync.Mutex
	status     saferestart.Status
	cancel     chan struct{}
	reexecPath string
	auto       *saferestart.AutoUpdate
}

// restartSnapshot is what newly connected dashboards are told about a restart
// in progress.
var restartSnapshot struct {
	sync.Mutex
	status *saferestart.Status
}

func publishRestartStatus(st saferestart.Status) {
	restartSnapshot.Lock()
	if st.Phase == saferestart.PhaseIdle {
		restartSnapshot.status = nil
	} else {
		cp := st
		restartSnapshot.status = &cp
	}
	restartSnapshot.Unlock()
	wsBroadcast(wsEvent{Type: "restart", Restart: &st})
}

func currentRestartSnapshot() *saferestart.Status {
	restartSnapshot.Lock()
	defer restartSnapshot.Unlock()
	return restartSnapshot.status
}

// RestartStatus reports the restart phase and the jobs a restart would stop.
func (s *Server) RestartStatus() saferestart.Status {
	s.restart.mu.Lock()
	st := s.restart.status
	if s.restart.auto != nil {
		auto := *s.restart.auto
		st.AutoUpdate = &auto
	}
	s.restart.mu.Unlock()
	if st.Phase == "" {
		st.Phase = saferestart.PhaseIdle
	}
	st.Jobs = saferestart.RunningJobs()
	st.Held = saferestart.Held()
	return st
}

// ReexecPath is the binary to start after ListenAndServe returns
// ErrReexecRequired. It is resolved before an upgrade replaces the binary,
// because the running executable's own path can point at the moved-aside copy.
func (s *Server) ReexecPath() string {
	s.restart.mu.Lock()
	defer s.restart.mu.Unlock()
	return s.restart.reexecPath
}

// requestRestart starts a safe restart: hold new jobs, then (now, or once no
// jobs are running) install the upgrade, update skills, checkpoint running
// jobs, and restart.
func (s *Server) requestRestart(ctx context.Context, req saferestart.Request) (saferestart.Status, error) {
	if req.Cancel {
		return s.cancelRestart()
	}
	when := strings.TrimSpace(req.When)
	if when == "" {
		when = saferestart.WhenNow
	}
	if when != saferestart.WhenNow && when != saferestart.WhenIdle {
		return saferestart.Status{}, fmt.Errorf("when must be %q or %q", saferestart.WhenNow, saferestart.WhenIdle)
	}

	if req.Upgrade {
		if dir := gitCheckout(); dir != "" {
			return s.gitUpgrade(ctx, dir)
		}
	}

	version := ""
	if req.Upgrade {
		check, err := versionCheck(ctx, &http.Client{Timeout: 15 * time.Second})
		if err != nil && check.LatestVersion == "" {
			return saferestart.Status{}, err
		}
		version = update.NormalizeVersion(req.Version)
		if version == "" {
			if check.NoCheckout {
				return saferestart.Status{}, errors.New(check.Message)
			}
			if !check.UpgradeAvailable {
				st := s.RestartStatus()
				st.Noop = true
				st.Message = fmt.Sprintf("Aviary is already up to date (%s).", check.CurrentVersion)
				return st, nil
			}
			version = update.NormalizeVersion(check.LatestVersion)
		}
		if update.SameVersion(update.CurrentVersion(), version) {
			st := s.RestartStatus()
			st.Noop = true
			st.Message = fmt.Sprintf("Aviary is already at %s.", version)
			return st, nil
		}
		if update.EmulationActive() {
			st := s.RestartStatus()
			st.Emulated = true
			st.Noop = true
			st.Version = version
			st.Message = "Emulated upgrade completed. No files were changed."
			return st, nil
		}
	}

	s.restart.mu.Lock()
	switch s.restart.status.Phase {
	case saferestart.PhaseUpdating:
		s.restart.mu.Unlock()
		return s.RestartStatus(), fmt.Errorf("a restart is already in progress")
	case saferestart.PhaseWaiting:
		// A newer request replaces the waiting one.
		close(s.restart.cancel)
	}
	s.restart.status = saferestart.Status{
		Phase:   saferestart.PhaseWaiting,
		When:    when,
		Upgrade: req.Upgrade,
		Version: version,
	}
	cancel := make(chan struct{})
	s.restart.cancel = cancel
	s.restart.mu.Unlock()

	saferestart.Hold(true)
	st := s.RestartStatus()
	publishRestartStatus(st)
	go s.runRestart(when, cancel)
	return st, nil
}

// gitUpgrade updates a dev checkout with a fast-forward pull. Hot reload
// relaunches the backend from the pulled sources, so there is no restart.
func (s *Server) gitUpgrade(ctx context.Context, dir string) (saferestart.Status, error) {
	res, err := gitPull(ctx, dir)
	if err != nil {
		return saferestart.Status{}, err
	}
	slog.Info("server: pulled dev checkout", "component", "update", "dir", dir, "from", res.From, "to", res.To, "commits", res.Commits)
	st := s.RestartStatus()
	st.Noop = true
	st.Message = update.GitPullMessage(res)
	return st, nil
}

func (s *Server) cancelRestart() (saferestart.Status, error) {
	s.restart.mu.Lock()
	if s.restart.status.Phase != saferestart.PhaseWaiting {
		phase := s.restart.status.Phase
		s.restart.mu.Unlock()
		if phase == saferestart.PhaseUpdating {
			return s.RestartStatus(), fmt.Errorf("the restart is already in progress and cannot be canceled")
		}
		return s.RestartStatus(), nil
	}
	close(s.restart.cancel)
	s.restart.status = saferestart.Status{Phase: saferestart.PhaseIdle}
	s.restart.mu.Unlock()
	saferestart.Hold(false)
	st := s.RestartStatus()
	publishRestartStatus(st)
	return st, nil
}

func (s *Server) runRestart(when string, cancel <-chan struct{}) {
	if when == saferestart.WhenIdle {
		ticker := time.NewTicker(idlePoll)
		defer ticker.Stop()
		for len(saferestart.RunningJobs()) > 0 {
			select {
			case <-cancel:
				return
			case <-s.runDone():
				return
			case <-ticker.C:
			}
		}
	}

	s.restart.mu.Lock()
	select {
	case <-cancel:
		s.restart.mu.Unlock()
		return
	default:
	}
	s.restart.status.Phase = saferestart.PhaseUpdating
	status := s.restart.status
	s.restart.mu.Unlock()
	publishRestartStatus(s.RestartStatus())

	ctx, done := context.WithTimeout(context.Background(), 10*time.Minute)
	defer done()
	if err := s.performRestart(ctx, status); err != nil {
		slog.Error("server: safe restart failed", "component", "update", "error", err)
		s.restart.mu.Lock()
		s.restart.status = saferestart.Status{Phase: saferestart.PhaseIdle, Error: err.Error()}
		s.restart.mu.Unlock()
		saferestart.Hold(false)
		publishRestartStatus(s.RestartStatus())
	}
}

// performRestart replaces the binary (for an upgrade), updates skills, runs the
// shutdown checkpoint, and asks ListenAndServe to re-exec.
func (s *Server) performRestart(ctx context.Context, status saferestart.Status) error {
	exe, err := executablePath()
	if err != nil {
		return fmt.Errorf("locating executable: %w", err)
	}
	marker := saferestart.Marker{
		Reason:      "restart",
		FromVersion: update.CurrentVersion(),
		At:          time.Now().UTC(),
		Jobs:        saferestart.RunningJobs(),
	}
	if status.Upgrade {
		marker.Reason = "upgrade"
		marker.ToVersion = status.Version
		if _, err := installRelease(ctx, update.InstallOptions{
			Version:    status.Version,
			TargetPath: exe,
			Client:     &http.Client{Timeout: 2 * time.Minute},
		}); err != nil {
			return fmt.Errorf("installing %s: %w", status.Version, err)
		}
		if err := saferestart.UpdateSkills(ctx, status.Version); err != nil {
			slog.Warn("server: updating skills during upgrade", "component", "update", "error", err)
		}
	}
	if err := saferestart.Checkpoint(ctx, marker); err != nil {
		slog.Warn("server: shutdown checkpoint", "component", "update", "error", err)
	}
	if err := saferestart.WriteMarker(marker); err != nil {
		slog.Warn("server: writing restart marker", "component", "update", "error", err)
	}
	slog.Info("server: safe restart", "component", "update", "reason", marker.Reason, "version", marker.ToVersion, "jobs", len(marker.Jobs))

	s.restart.mu.Lock()
	s.restart.reexecPath = exe
	s.restart.mu.Unlock()
	select {
	case s.reexecCh <- struct{}{}:
	default:
	}
	return nil
}

func (s *Server) runDone() <-chan struct{} {
	if s.runCtx == nil {
		return nil
	}
	return s.runCtx.Done()
}

// runSelfUpdater checks for a new release (or, in git mode, new upstream
// commits) every update.CheckInterval, tells every dashboard about it, and
// applies it when auto_update is on, as soon as no jobs are running.
func (s *Server) runSelfUpdater(ctx context.Context) {
	if update.IsDevBuild() && !update.EmulationActive() && gitCheckout() == "" {
		return // dev builds outside a checkout have nothing to compare against
	}
	var lastNotified string
	notify := func(check update.CheckResult) {
		latest := ""
		if check.UpgradeAvailable {
			latest = check.LatestVersion
		}
		if latest != lastNotified {
			lastNotified = latest
			wsBroadcast(wsEvent{Type: "version", Check: &check})
		}
	}
	tick := func() {
		update.ClearCache()
		check, err := versionCheck(ctx, &http.Client{Timeout: 30 * time.Second})
		if err != nil {
			return
		}
		notify(check)
		if !check.UpgradeAvailable || !s.autoUpdateOn() || !check.UpgradeSupported {
			s.setAutoUpdate(nil)
			return
		}
		if s.autoApply(ctx, check) {
			// Tell dashboards the update is gone, e.g. after a git pull
			// that hot reload has not yet restarted onto.
			update.ClearCache()
			if check, err := versionCheck(ctx, &http.Client{Timeout: 30 * time.Second}); err == nil {
				notify(check)
			}
		}
	}
	ticker := time.NewTicker(update.CheckInterval)
	defer ticker.Stop()
	for {
		tick()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) autoUpdateOn() bool {
	cfg := s.cfg
	return cfg != nil && cfg.Server.AutoUpdate
}

// autoApply applies check's update once no restart is pending and no jobs
// are running, publishing what it waits for meanwhile, and reports whether
// it applied. It waits at most autoUpdateWait, so the next round re-checks
// for a newer update. A failed attempt stays published until one succeeds.
func (s *Server) autoApply(ctx context.Context, check update.CheckResult) bool {
	version := check.LatestVersion
	deadline := time.NewTimer(autoUpdateWait)
	defer deadline.Stop()
	poll := time.NewTicker(idlePoll)
	defer poll.Stop()
	for {
		if !s.autoUpdateOn() {
			s.setAutoUpdate(nil)
			return false
		}
		state, jobs := "", 0
		if s.RestartStatus().Phase != saferestart.PhaseIdle {
			state = saferestart.AutoWaitingRestart
		} else if n := len(saferestart.RunningJobs()); n > 0 {
			state, jobs = saferestart.AutoWaitingJobs, n
		}
		if state == "" {
			break
		}
		s.setAutoUpdateState(version, state, jobs)
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		case <-poll.C:
		}
	}

	slog.Info("server: applying automatic update", "component", "update", "version", version)
	s.setAutoUpdateState(version, saferestart.AutoApplying, 0)
	st, err := s.requestRestart(ctx, saferestart.Request{When: saferestart.WhenIdle, Upgrade: true, Version: version})
	if err != nil {
		slog.Warn("server: automatic update", "component", "update", "error", err)
		s.setAutoUpdate(&saferestart.AutoUpdate{Version: version, Error: err.Error()})
		return false
	}
	if st.Message != "" {
		slog.Info("server: automatic update applied", "component", "update", "message", st.Message)
	}
	s.setAutoUpdate(nil)
	return true
}

// setAutoUpdateState records what a pending automatic update waits for,
// keeping the last attempt's error.
func (s *Server) setAutoUpdateState(version, state string, jobs int) {
	s.restart.mu.Lock()
	next := saferestart.AutoUpdate{Version: version, State: state, Jobs: jobs}
	if prev := s.restart.auto; prev != nil && prev.Version == version {
		next.Error = prev.Error
	}
	s.restart.mu.Unlock()
	s.setAutoUpdate(&next)
}

// setAutoUpdate records the automatic update state, or clears it for nil,
// and tells every dashboard when it changed.
func (s *Server) setAutoUpdate(auto *saferestart.AutoUpdate) {
	s.restart.mu.Lock()
	prev := s.restart.auto
	if (prev == nil && auto == nil) || (prev != nil && auto != nil && *prev == *auto) {
		s.restart.mu.Unlock()
		return
	}
	s.restart.auto = auto
	s.restart.mu.Unlock()
	publishRestartStatus(s.RestartStatus())
}
