// Package scheduler drives task execution via cron, file-watch, and job queue.
package scheduler

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

const (
	lockTimeout    = 5 * time.Minute
	retryBase      = 30 * time.Second
	retryMax       = 1 * time.Hour
	defaultRetries = 3
)

// newID generates a time-based unique identifier with the given prefix.
func newID(prefix string) string {
	ts := time.Now().UTC().Format("20060102_150405.000000000")
	return prefix + "_" + strings.ReplaceAll(ts, ".", "_")
}

// JobQueue is a file-backed, re-entrant job queue.
// Each job is stored as a JSON file under ~/.config/aviary/jobs/.
type JobQueue struct {
	mu sync.Mutex
}

// NewJobQueue creates a JobQueue.
func NewJobQueue() *JobQueue { return &JobQueue{} }

// Enqueue writes a new pending job to disk.
func (q *JobQueue) Enqueue(taskID, agentID, agentName, prompt string, maxRetries int) (*domain.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if maxRetries <= 0 {
		maxRetries = defaultRetries
	}
	job := &domain.Job{
		ID:         newID("job"),
		TaskID:     taskID,
		AgentID:    agentID,
		AgentName:  agentName,
		Prompt:     prompt,
		Status:     domain.JobStatusPending,
		MaxRetries: maxRetries,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if err := store.WriteJSON(store.JobPath(job.ID), job); err != nil {
		return nil, fmt.Errorf("enqueue job: %w", err)
	}
	slog.Info("job enqueued", "id", job.ID, "task", taskID)
	return job, nil
}

// Claim atomically locks a pending job for processing.
// Returns nil, nil if no claimable job is available.
func (q *JobQueue) Claim() (*domain.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	jobs, err := store.ListJSON[domain.Job](store.SubDir(store.DirJobs), ".json")
	if err != nil {
		return nil, fmt.Errorf("listing jobs: %w", err)
	}

	now := time.Now()
	for i := range jobs {
		j := &jobs[i]
		switch {
		case j.Status == domain.JobStatusPending:
			// Ready to claim.
		case j.Status == domain.JobStatusInProgress && j.LockedAt != nil && now.Sub(*j.LockedAt) > lockTimeout:
			// Stale lock — recover it.
			slog.Warn("recovering stale job", "id", j.ID)
		default:
			continue
		}
		if j.NextRetryAt != nil && now.Before(*j.NextRetryAt) {
			continue
		}
		j.Status = domain.JobStatusInProgress
		j.Attempts++
		j.LockedAt = &now
		j.UpdatedAt = now
		if err := store.WriteJSON(store.JobPath(j.ID), j); err != nil {
			return nil, fmt.Errorf("claiming job %s: %w", j.ID, err)
		}
		return j, nil
	}
	return nil, nil
}

// Complete marks a job as completed.
func (q *JobQueue) Complete(id string) error {
	return q.updateStatus(id, domain.JobStatusCompleted)
}

// Fail marks a job as failed and schedules a retry if attempts remain.
func (q *JobQueue) Fail(id string, cause error) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	job, err := store.ReadJSON[domain.Job](store.JobPath(id))
	if err != nil {
		return fmt.Errorf("reading job %s: %w", id, err)
	}

	now := time.Now()
	job.UpdatedAt = now
	job.LockedAt = nil

	if job.Attempts >= job.MaxRetries {
		job.Status = domain.JobStatusFailed
		slog.Warn("job failed (no retries left)", "id", id, "err", cause)
	} else {
		// Exponential backoff: 30s, 60s, 120s … capped at 1h.
		backoff := retryBase * (1 << (job.Attempts - 1))
		if backoff > retryMax {
			backoff = retryMax
		}
		next := now.Add(backoff)
		job.NextRetryAt = &next
		job.Status = domain.JobStatusPending
		slog.Info("job will retry", "id", id, "at", next, "err", cause)
	}

	return store.WriteJSON(store.JobPath(id), &job)
}

// List returns all jobs, optionally filtered by task ID.
func (q *JobQueue) List(taskID string) ([]domain.Job, error) {
	jobs, err := store.ListJSON[domain.Job](store.SubDir(store.DirJobs), ".json")
	if err != nil {
		return nil, err
	}
	if taskID == "" {
		return jobs, nil
	}
	out := jobs[:0]
	for _, j := range jobs {
		if j.TaskID == taskID {
			out = append(out, j)
		}
	}
	return out, nil
}

// RecoverStuck resets any jobs stuck in in_progress beyond lockTimeout.
// Called on startup to handle jobs that were interrupted by a crash.
func (q *JobQueue) RecoverStuck() {
	q.mu.Lock()
	defer q.mu.Unlock()

	jobs, err := store.ListJSON[domain.Job](store.SubDir(store.DirJobs), ".json")
	if err != nil {
		slog.Warn("recover stuck: listing jobs failed", "err", err)
		return
	}
	now := time.Now()
	for i := range jobs {
		j := &jobs[i]
		if j.Status == domain.JobStatusInProgress && j.LockedAt != nil && now.Sub(*j.LockedAt) > lockTimeout {
			j.Status = domain.JobStatusPending
			j.LockedAt = nil
			j.UpdatedAt = now
			if err := store.WriteJSON(store.JobPath(j.ID), j); err != nil {
				slog.Warn("recover stuck: failed to reset job", "id", j.ID, "err", err)
			} else {
				slog.Info("recovered stuck job", "id", j.ID)
			}
		}
	}
}

func (q *JobQueue) updateStatus(id string, status domain.JobStatus) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	job, err := store.ReadJSON[domain.Job](store.JobPath(id))
	if err != nil {
		return fmt.Errorf("reading job %s: %w", id, err)
	}
	job.Status = status
	job.LockedAt = nil
	job.UpdatedAt = time.Now()
	return store.WriteJSON(store.JobPath(id), &job)
}
