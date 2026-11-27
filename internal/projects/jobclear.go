package projects

import (
	"fmt"
	"slices"
	"time"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/projects/core"
)

// jobSweepInterval is how often the job store's expired jobs are removed.
var jobSweepInterval = 10 * time.Minute

// Which finished jobs ClearJobs clears.
const (
	ClearDone     = "done"
	ClearFailed   = "failed"
	ClearFinished = "finished"
)

// finishedTTLsLocked maps every one of targets to how long its done and
// failed jobs are kept and where that comes from: "project" for its own
// finished_ttl, else "global". A runtime watch's override wins over the
// saved project's.
func (m *Manager) finishedTTLsLocked(targets []string) (map[string]time.Duration, map[string]string) {
	saved := config.AgentProjectsConfig(m.list).FinishedTTLs()
	global := m.effectiveSettingsLocked()
	ttls := make(map[string]time.Duration, len(targets))
	sources := make(map[string]string, len(targets))
	for _, target := range targets {
		override := saved[target]
		for _, watch := range m.watches {
			if ttl, ok := watch.FinishedTTLs[target]; ok && slices.Contains(watch.Targets, target) {
				override = ttl
			}
		}
		ttls[target], sources[target] = global.FinishedTTLFor(override)
	}
	return ttls, sources
}

// finishedTTL returns the lookup the job store expires finished jobs by. A
// target no longer watched keeps the shared value.
func (m *Manager) finishedTTL() finishedTTL {
	m.mu.Lock()
	settings, _ := m.settingsLocked()
	ttls, _ := m.finishedTTLsLocked(settings.Targets)
	global := m.effectiveSettingsLocked().EffectiveFinishedTTL()
	m.mu.Unlock()
	return func(target string) time.Duration {
		if ttl, ok := ttls[target]; ok {
			return ttl
		}
		return global
	}
}

// ProjectFinishedTTL returns how long target's done and failed jobs are kept,
// as a duration string ("0s" keeps them until cleared), and its source,
// "project" or "global".
func (m *Manager) ProjectFinishedTTL(target string) (string, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ttls, sources := m.finishedTTLsLocked([]string{target})
	return formatFinishedTTL(ttls[target]), sources[target]
}

// projectFinishedTTLs maps every one of targets to its finished_ttl and that
// value's source, and returns the shared value.
func (m *Manager) projectFinishedTTLs(targets []string) (map[string]string, map[string]string, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ttls, sources := m.finishedTTLsLocked(targets)
	rendered := make(map[string]string, len(ttls))
	for target, ttl := range ttls {
		rendered[target] = formatFinishedTTL(ttl)
	}
	return rendered, sources, formatFinishedTTL(m.effectiveSettingsLocked().EffectiveFinishedTTL())
}

// formatFinishedTTL renders a finished_ttl in days or weeks when it is a
// whole number of them, as the settings write it.
func formatFinishedTTL(ttl time.Duration) string {
	const day = 24 * time.Hour
	switch {
	case ttl == 0:
		return "0"
	case ttl%(7*day) == 0:
		return fmt.Sprintf("%dw", ttl/(7*day))
	case ttl%day == 0:
		return fmt.Sprintf("%dd", ttl/day)
	}
	return ttl.String()
}

// sweepJobsInBackground removes the job store's expired jobs, at most once
// per jobSweepInterval, off the caller's goroutine. The engine publishes
// often, so this runs as a background sweep without a timer of its own.
func (m *Manager) sweepJobsInBackground() {
	now := time.Now().UnixNano()
	last := m.lastSweep.Load()
	if now-last < int64(jobSweepInterval) || !m.lastSweep.CompareAndSwap(last, now) {
		return
	}
	go m.jobs.sweep(m.finishedTTL())
}

// display is the state the read APIs show: the live snapshot, with the job
// store filling in every job the engine does not report. Only reads use it;
// every action goes through currentSnapshot, so stored state never starts,
// claims or restarts a job.
func (m *Manager) display() displayState {
	m.mu.Lock()
	settings, _ := m.settingsLocked()
	m.mu.Unlock()
	live := m.snapshot.Load()
	var liveJobs []JobSnapshot
	if live != nil {
		liveJobs = live.Jobs
	}
	return mergeStored(live, m.jobs.view(liveJobs, m.finishedTTL()), settings.Targets)
}

// ClearJobs is project_job_clear: it removes done and failed jobs from the
// board and the job store, with their transcripts, leaving a tombstone so the
// same run is never shown again. Naming target and number clears that one
// job; otherwise which, ClearDone, ClearFailed or ClearFinished (the
// default), picks the jobs of targets, or of every project without targets.
// A schedule's runs are not cleared: its card shows its last run, and Dismiss
// takes a failed one off the Failed state. Running, queued, paused, waiting
// and retrying jobs are never cleared.
func (m *Manager) ClearJobs(targets []string, which, target string, number int) (map[string]any, error) {
	display := m.display()
	var picked []JobSnapshot
	if number > 0 {
		job, err := findJob(display.Jobs, target, number)
		if err != nil {
			return nil, err
		}
		if !jobFinished(job) {
			return nil, fmt.Errorf("%s is %s; only done and failed jobs can be cleared", jobKey(job), job.BoardState())
		}
		if job.Kind == core.JobKindSchedule {
			return nil, fmt.Errorf("%s is a scheduled run; dismiss it instead", jobKey(job))
		}
		picked = append(picked, job)
	} else {
		if which == "" {
			which = ClearFinished
		}
		if which != ClearDone && which != ClearFailed && which != ClearFinished {
			return nil, fmt.Errorf("unknown jobs to clear %q; use %s, %s or %s", which, ClearDone, ClearFailed, ClearFinished)
		}
		for _, job := range display.Jobs {
			if job.Kind == core.JobKindSchedule || !jobFinished(job) {
				continue
			}
			if len(targets) > 0 && !slices.Contains(targets, job.Target) {
				continue
			}
			if which != ClearFinished && job.BoardState() != which {
				continue
			}
			picked = append(picked, job)
		}
	}
	cleared := m.jobs.clear(picked)
	slices.Sort(cleared)
	if cleared == nil {
		cleared = []string{}
	}
	for _, target := range uniqueTargets(picked) {
		m.NotifyUpdated(target)
	}
	return map[string]any{"ok": true, "cleared": cleared, "count": len(cleared)}, nil
}

// uniqueTargets lists the targets of jobs, in order, once each.
func uniqueTargets(jobs []JobSnapshot) []string {
	var targets []string
	for _, job := range jobs {
		if !slices.Contains(targets, job.Target) {
			targets = append(targets, job.Target)
		}
	}
	return targets
}
