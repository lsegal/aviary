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

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/mcp"
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
	planDevReload  = update.PlanDevReload
	hotReloadDir   = update.HotReloadDir
	hotReloadGen   = update.HotReloadGeneration
	waitHotReload  = update.WaitHotReload
	hotReloadWait  = update.DefaultHotReloadWait
	// refreshAfterHotReload reruns the startup registrations a hot reload
	// does not repeat, so reloaded code's MCP tools go live too.
	refreshAfterHotReload = mcp.RefreshLiveServer
	executablePath        = os.Executable
	idlePoll              = 2 * time.Second
	// autoUpdateWait is how long one self-updater round waits for a restart
	// already in progress before checking for a newer update.
	autoUpdateWait = update.CheckInterval
)

type restartState struct {
	mu         sync.Mutex
	status     saferestart.Status
	cancel     chan struct{}
	reexecPath string
	auto       *saferestart.AutoUpdate
	// stopHotReloadWait stops waiting on forgo for an earlier dev update.
	stopHotReloadWait context.CancelFunc
}

// restartSnapshot is what newly connected dashboards are told about a restart
// in progress.
var restartSnapshot struct {
	sync.Mutex
	status *saferestart.Status
}

// configureRestartGate applies the configured restart gate timings.
func configureRestartGate(cfg *config.Config) {
	var srv config.ServerConfig
	if cfg != nil {
		srv = cfg.Server
	}
	saferestart.MCPCalls.SetTimings(srv.EffectiveRestartCallGrace(), srv.EffectiveRestartCallMaxWait(), 0)
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
	if st.Phase == saferestart.PhaseDraining {
		st.MCPCalls = saferestart.MCPCalls.Active()
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
			return s.gitUpgrade(ctx, dir, when)
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

	return s.startRestart(when, req.Upgrade, version, req.Force)
}

// startRestart holds new jobs and restarts now, or once no jobs are running,
// installing version first when upgrade is set. Either way it first waits for
// in-flight MCP calls unless force is set.
func (s *Server) startRestart(when string, upgrade bool, version string, force bool) (saferestart.Status, error) {
	s.restart.mu.Lock()
	switch s.restart.status.Phase {
	case saferestart.PhaseUpdating:
		s.restart.mu.Unlock()
		return s.RestartStatus(), fmt.Errorf("a restart is already in progress")
	case saferestart.PhaseWaiting, saferestart.PhaseDraining:
		// A newer request replaces the waiting one.
		close(s.restart.cancel)
	}
	s.restart.status = saferestart.Status{
		Phase:   saferestart.PhaseWaiting,
		When:    when,
		Upgrade: upgrade,
		Version: version,
		Force:   force,
	}
	cancel := make(chan struct{})
	s.restart.cancel = cancel
	s.restart.mu.Unlock()

	saferestart.Hold(true)
	st := s.RestartStatus()
	publishRestartStatus(st)
	// Read the poll interval here, not in the goroutine, which can outlive
	// whoever started the restart.
	go s.runRestart(when, cancel, idlePoll)
	return st, nil
}

// gitUpgrade updates a dev checkout with a fast-forward pull. Under forgo's
// hot reload the backend picks up pulled Go changes in place, keeping running
// jobs, the Projects engine and MCP/chat sessions; gitUpgrade only waits for
// forgo's verdict. It falls back to a safe restart only when hot reload cannot
// run the pulled code: go.mod, go.sum or an embedded file changed, forgo is
// not active, or forgo refuses or fails the reload. That restart rebuilds from
// the checkout only under scripts/dev-go.mjs; otherwise re-executing would run
// the old binary again, so the status says a restart is still needed instead.
func (s *Server) gitUpgrade(ctx context.Context, dir, when string) (saferestart.Status, error) {
	hotDir := hotReloadDir()
	since := 0
	if hotDir != "" {
		since = hotReloadGen(hotDir)
	}
	res, err := gitPull(ctx, dir)
	if err != nil {
		return saferestart.Status{}, err
	}
	slog.Info("server: pulled dev checkout", "component", "update", "dir", dir, "from", res.From, "to", res.To, "commits", res.Commits, "supervised", res.Supervised, "hot_reload", hotDir != "")

	var plan update.DevReloadPlan
	if res.Commits > 0 {
		if plan, err = planDevReload(ctx, dir, res.From, res.To, hotDir != ""); err != nil {
			plan = update.DevReloadPlan{GoChanged: true, RestartReason: "listing the pulled changes failed: " + err.Error()}
		}
	}
	msg := update.GitPullMessage(res, plan)
	switch {
	case res.Commits == 0:
	case plan.RestartReason != "":
		slog.Info("server: dev update needs a full restart", "component", "update", "reason", plan.RestartReason, "to", res.To)
		if res.Supervised {
			st, err := s.startRestart(when, false, res.To, false)
			st.Message = msg
			return st, err
		}
	case plan.GoChanged:
		slog.Info("server: dev update waiting for hot reload", "component", "update", "to", res.To)
		s.awaitHotReload(hotDir, since, when, res)
	default:
		slog.Info("server: dev update applied without a backend change", "component", "update", "to", res.To)
	}
	st := s.RestartStatus()
	st.Noop = true
	st.Message = msg
	return st, nil
}

// awaitHotReload waits in the background for forgo to reload the code a pull
// brought in, replacing any earlier wait. When the reload goes live nothing
// restarts; otherwise the existing safe restart runs (when supervised).
func (s *Server) awaitHotReload(dir string, since int, when string, res update.GitPullResult) {
	ctx, cancel := context.WithCancel(context.Background())
	s.restart.mu.Lock()
	if s.restart.stopHotReloadWait != nil {
		s.restart.stopHotReloadWait()
	}
	s.restart.stopHotReloadWait = cancel
	s.restart.mu.Unlock()
	opts := hotReloadWait
	done := s.runDone()
	go func() {
		defer cancel()
		go func() {
			select {
			case <-done:
				cancel()
			case <-ctx.Done():
			}
		}()
		result := waitHotReload(ctx, dir, since, opts)
		if ctx.Err() != nil {
			return // a newer pull or shutdown took over
		}
		if result.Applied {
			refreshAfterHotReload()
			slog.Info("server: dev update applied via hot reload", "component", "update", "to", res.To, "generation", result.Generation)
			return
		}
		slog.Warn("server: dev update needs a full restart", "component", "update", "reason", result.Reason, "to", res.To)
		if !res.Supervised {
			return
		}
		if _, err := s.startRestart(when, false, res.To, false); err != nil {
			slog.Warn("server: restarting after a failed hot reload", "component", "update", "error", err)
		}
	}()
}

func (s *Server) cancelRestart() (saferestart.Status, error) {
	s.restart.mu.Lock()
	if phase := s.restart.status.Phase; phase != saferestart.PhaseWaiting && phase != saferestart.PhaseDraining {
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

func (s *Server) runRestart(when string, cancel <-chan struct{}, poll time.Duration) {
	if when == saferestart.WhenIdle {
		ticker := time.NewTicker(poll)
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

	unlock := s.drainMCPCalls(cancel)
	if unlock == nil {
		return
	}

	s.restart.mu.Lock()
	select {
	case <-cancel:
		s.restart.mu.Unlock()
		unlock()
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
		unlock()
		saferestart.Hold(false)
		publishRestartStatus(s.RestartStatus())
	}
}

// drainMCPCalls waits for in-flight MCP calls to finish, then takes the
// exclusive restart lock so no new call starts while the restart runs. Unless
// the restart is forced, it defers the restart while calls are active, logging
// why and publishing the count; new calls are refused after the grace period,
// and the restart stops waiting after the max wait. It returns the lock's
// unlock, or nil when the restart was canceled or the server stopped while it
// waited.
func (s *Server) drainMCPCalls(cancel <-chan struct{}) (unlock func()) {
	s.restart.mu.Lock()
	select {
	case <-cancel:
		s.restart.mu.Unlock()
		return nil
	default:
	}
	s.restart.status.Phase = saferestart.PhaseDraining
	force := s.restart.status.Force
	s.restart.mu.Unlock()

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() {
		select {
		case <-s.runDone():
			stop()
		case <-ctx.Done():
		}
	}()
	unlock, abandoned, err := saferestart.MCPCalls.Lock(ctx, saferestart.LockOptions{
		Force:  force,
		Cancel: cancel,
		OnWait: func(active int) {
			slog.Info("server: restart deferred", "component", "update", "reason", "waiting for in-flight MCP calls", "active_mcp_calls", active)
			publishRestartStatus(s.RestartStatus())
		},
	})
	if err != nil {
		return nil
	}
	if abandoned > 0 {
		reason := "max wait elapsed"
		if force {
			reason = "forced"
		}
		slog.Warn("server: restarting with MCP calls still in flight", "component", "update", "reason", reason, "active_mcp_calls", abandoned)
	}
	return unlock
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
// applies it when auto_update is on. Running jobs do not delay it: the restart
// checkpoints them and they resume afterwards.
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
			// whose restart is still checkpointing jobs.
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
	return cfg != nil && config.EffectiveServerAutoUpdate(cfg)
}

// autoApply applies check's update, restarting now and checkpointing any
// running jobs, and reports whether it applied. Only a restart already in
// progress delays it, for at most autoUpdateWait, so the next round re-checks
// for a newer update; a restart still waiting for jobs to finish is replaced.
// A failed attempt stays published until one succeeds.
func (s *Server) autoApply(ctx context.Context, check update.CheckResult) bool {
	version := check.LatestVersion
	deadline := time.NewTimer(autoUpdateWait)
	defer deadline.Stop()
	poll := time.NewTicker(idlePoll)
	defer poll.Stop()
	for s.RestartStatus().Phase == saferestart.PhaseUpdating {
		if !s.autoUpdateOn() {
			s.setAutoUpdate(nil)
			return false
		}
		s.setAutoUpdateState(version, saferestart.AutoWaitingRestart)
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		case <-poll.C:
		}
	}
	if !s.autoUpdateOn() {
		s.setAutoUpdate(nil)
		return false
	}

	slog.Info("server: applying automatic update", "component", "update", "version", version)
	s.setAutoUpdateState(version, saferestart.AutoApplying)
	st, err := s.requestRestart(ctx, saferestart.Request{When: saferestart.WhenNow, Upgrade: true, Version: version})
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
func (s *Server) setAutoUpdateState(version, state string) {
	s.restart.mu.Lock()
	next := saferestart.AutoUpdate{Version: version, State: state}
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
