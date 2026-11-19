package scheduler

import (
	"context"
	"sort"
	"time"

	"github.com/lsegal/aviary/internal/saferestart"
)

// checkpointPoll is how often Checkpoint checks that interrupted jobs exited.
var checkpointPoll = 50 * time.Millisecond

// RunningJobs lists the task jobs running now, for the safe-restart dialog.
func (s *Scheduler) RunningJobs() []saferestart.Job {
	return s.pool.runningJobs()
}

// HoldNewJobs stops (or resumes) claiming queued task jobs. Jobs that come due
// while held stay queued and run after the restart.
func (s *Scheduler) HoldNewJobs(hold bool) { s.pool.held.Store(hold) }

// Checkpoint interrupts every running task job and requeues it with the
// restart notice, so it resumes in its own session after the restart.
func (s *Scheduler) Checkpoint(ctx context.Context, marker saferestart.Marker) error {
	note := marker.Message()
	s.pool.checkpointNote.Store(&note)
	s.pool.StopJobs(nil)
	ticker := time.NewTicker(checkpointPoll)
	defer ticker.Stop()
	for len(s.pool.runningJobs()) > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	return nil
}

func (p *WorkerPool) runningJobs() []saferestart.Job {
	p.activeMu.Lock()
	defer p.activeMu.Unlock()
	jobs := make([]saferestart.Job, 0, len(p.active))
	for id, job := range p.active {
		jobs = append(jobs, saferestart.Job{Source: "task", ID: id, Title: job.taskID})
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].ID < jobs[j].ID })
	return jobs
}
