package projects

import (
	"context"
	"fmt"
	"strconv"

	"github.com/lsegal/aviary/internal/saferestart"
)

// restartSource is the Source of the jobs this package lists for a safe
// restart.
const restartSource = "projects"

// RunningJobs lists the Code Loop jobs whose agents are running, for a safe
// restart. Waiting and paused jobs are left out: a restart leaves them alone.
func (m *Manager) RunningJobs() []saferestart.Job {
	jobs := []saferestart.Job{}
	for _, job := range m.currentSnapshot().Jobs {
		if job.Status != "active" {
			continue
		}
		repo := job.Repository
		if repo == "" {
			repo = job.Target
		}
		jobs = append(jobs, saferestart.Job{Source: restartSource, ID: fmt.Sprintf("%s#%d", repo, job.Number), Title: job.Title})
	}
	return jobs
}

// HoldNewJobs stops (true) or allows (false) dispatching new jobs. While held,
// the engine's polls start nothing and leave waiting jobs as they are, and no
// engine starts. Releasing a hold starts the engine again when a checkpoint
// stopped it, and polls at once for the work the hold kept back.
func (m *Manager) HoldNewJobs(hold bool) {
	m.held.Store(hold)
	if hold {
		return
	}
	_ = m.refresh()
	if engine := m.engine(); engine != nil {
		engine.nudgePoll()
	}
}

// heldForRestart reports whether a safe restart is holding new jobs.
func (m *Manager) heldForRestart() bool { return m.held.Load() }

// Checkpoint commits and pushes every running job's work and stops the
// engine. The work state keeps each job's session, so the jobs resume in the
// same sessions once Aviary is back.
func (m *Manager) Checkpoint(context.Context, saferestart.Marker) error {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	m.mu.Lock()
	run := m.running
	m.running = nil
	m.lastKey = ""
	m.mu.Unlock()
	m.stopRun(run)
	return nil
}

// ResumeAfterRestart records the notice each job a safe restart stopped is
// resumed with. The engine resumes those jobs in their own sessions from the
// work state, as after any restart, and prompts each with marker.Message()
// instead of a bare "continue". Call it before the engine starts.
func (m *Manager) ResumeAfterRestart(marker saferestart.Marker) {
	notices := map[string]string{}
	for _, job := range marker.Jobs {
		if job.Source == restartSource {
			notices[job.ID] = marker.Message()
		}
	}
	m.noticeMu.Lock()
	m.restartNotices = notices
	m.noticeMu.Unlock()
}

// restartNotice returns, once, the notice for the job a safe restart stopped
// on issue, or "" when it stopped none there.
func (m *Manager) restartNotice(issue Issue) string {
	id := issueRepository(issue.Target, issue) + "#" + strconv.Itoa(issue.Number)
	m.noticeMu.Lock()
	defer m.noticeMu.Unlock()
	notice := m.restartNotices[id]
	delete(m.restartNotices, id)
	return notice
}
