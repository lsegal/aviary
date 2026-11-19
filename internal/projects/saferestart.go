package projects

import (
	"context"
	"fmt"

	"github.com/lsegal/aviary/internal/saferestart"
)

// RunningJobs lists the Projects jobs whose agents are running, for a safe
// restart.
func (m *Manager) RunningJobs() []saferestart.Job {
	jobs := []saferestart.Job{}
	for _, job := range m.currentSnapshot().Jobs {
		if job.Status == "active" {
			jobs = append(jobs, saferestart.Job{Source: "project", ID: fmt.Sprintf("%s#%d", job.Target, job.Number), Title: job.Title})
		}
	}
	return jobs
}

// HoldNewJobs stops (true) or allows (false) starting an engine. Releasing a
// hold starts the engine again when a checkpoint stopped it.
func (m *Manager) HoldNewJobs(hold bool) {
	m.mu.Lock()
	m.held = hold
	m.mu.Unlock()
	if !hold {
		_ = m.refresh()
	}
}

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
