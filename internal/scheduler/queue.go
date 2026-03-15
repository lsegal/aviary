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
	lockHeartbeat  = 30 * time.Second
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

func (q *JobQueue) newJob(taskID, agentID, agentName, prompt, outputChannel string, status domain.JobStatus, maxRetries int, scheduledFor *time.Time, replyAgentID, replySessionID string) *domain.Job {
	if maxRetries <= 0 {
		maxRetries = defaultRetries
	}
	job := &domain.Job{
		ID:             newID("job"),
		TaskID:         taskID,
		AgentID:        agentID,
		AgentName:      agentName,
		Prompt:         prompt,
		OutputChannel:  outputChannel,
		Status:         status,
		MaxRetries:     maxRetries,
		ReplyAgentID:   replyAgentID,
		ReplySessionID: replySessionID,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		ScheduledFor:   scheduledFor,
	}
	if status == domain.JobStatusInProgress {
		now := time.Now()
		job.Attempts = 1
		job.LockedAt = &now
	}
	return job
}

// Enqueue writes a new pending job to disk.
// replyAgentID and replySessionID, if set, identify the session that should
// receive the job's output when it completes (the "call-back" channel).
func (q *JobQueue) Enqueue(taskID, agentID, agentName, prompt, outputChannel string, maxRetries int, replyAgentID, replySessionID string) (*domain.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	job := q.newJob(taskID, agentID, agentName, prompt, outputChannel, domain.JobStatusPending, maxRetries, nil, replyAgentID, replySessionID)
	if err := store.WriteJSON(store.JobPath(agentID, job.ID), job); err != nil {
		return nil, fmt.Errorf("enqueue job: %w", err)
	}
	slog.Info("job enqueued", "id", job.ID, "task", taskID)
	return job, nil
}

// StartImmediate writes a job that is already marked in_progress so it can be
// executed outside the normal queue claim loop.
func (q *JobQueue) StartImmediate(taskID, agentID, agentName, prompt, outputChannel string, replyAgentID, replySessionID string) (*domain.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	job := q.newJob(taskID, agentID, agentName, prompt, outputChannel, domain.JobStatusInProgress, 1, nil, replyAgentID, replySessionID)
	if err := store.WriteJSON(store.JobPath(agentID, job.ID), job); err != nil {
		return nil, fmt.Errorf("start immediate job: %w", err)
	}
	slog.Info("job started immediately", "id", job.ID, "task", taskID)
	return job, nil
}

// ForceStart marks an existing pending job in_progress immediately so it can
// be executed outside the normal queue claim loop.
func (q *JobQueue) ForceStart(id string) (*domain.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	path := store.FindJobPath(id)
	if path == "" {
		return nil, fmt.Errorf("job %s not found", id)
	}
	job, err := store.ReadJSON[domain.Job](path)
	if err != nil {
		return nil, fmt.Errorf("reading job %s: %w", id, err)
	}
	if job.Status != domain.JobStatusPending {
		return nil, fmt.Errorf("job %s is not pending", id)
	}
	if job.Attempts >= job.MaxRetries {
		return nil, fmt.Errorf("job %s has exhausted its %d allowed attempt(s)", id, job.MaxRetries)
	}
	now := time.Now()
	job.Status = domain.JobStatusInProgress
	job.Attempts++
	job.LockedAt = &now
	job.NextRetryAt = nil
	job.ScheduledFor = nil
	job.UpdatedAt = now
	if err := store.WriteJSON(path, &job); err != nil {
		return nil, fmt.Errorf("force starting job %s: %w", id, err)
	}
	slog.Info("job force-started", "id", job.ID, "task", job.TaskID)
	return &job, nil
}

// Claim atomically locks a pending job for processing.
// Returns nil, nil if no claimable job is available.
func (q *JobQueue) Claim() (*domain.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	var jobs []domain.Job
	for _, dir := range store.AllJobDirs() {
		batch, err := store.ListJSON[domain.Job](dir, ".json")
		if err != nil {
			return nil, fmt.Errorf("listing jobs: %w", err)
		}
		jobs = append(jobs, batch...)
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
		if j.ScheduledFor != nil && now.Before(*j.ScheduledFor) {
			continue
		}
		if j.Attempts >= j.MaxRetries {
			j.Status = domain.JobStatusFailed
			j.LockedAt = nil
			j.NextRetryAt = nil
			j.UpdatedAt = now
			if err := store.WriteJSON(store.JobPath(j.AgentID, j.ID), j); err != nil {
				return nil, fmt.Errorf("failing exhausted job %s: %w", j.ID, err)
			}
			slog.Warn("job exhausted before claim", "id", j.ID, "attempts", j.Attempts, "max_retries", j.MaxRetries)
			continue
		}
		j.Status = domain.JobStatusInProgress
		j.Attempts++
		j.LockedAt = &now
		j.UpdatedAt = now
		if err := store.WriteJSON(store.JobPath(j.AgentID, j.ID), j); err != nil {
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

// Cancel marks a job as canceled.
func (q *JobQueue) Cancel(id string) error {
	return q.updateStatus(id, domain.JobStatusCanceled)
}

// Fail marks a job as failed and schedules a retry if attempts remain.
func (q *JobQueue) Fail(id string, cause error) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	path := store.FindJobPath(id)
	if path == "" {
		return fmt.Errorf("job %s not found", id)
	}
	job, err := store.ReadJSON[domain.Job](path)
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

	return store.WriteJSON(path, &job)
}

// EnqueueAt writes a new pending job that will not be claimed until at.
// replyAgentID and replySessionID, if set, identify the session to reply to on completion.
func (q *JobQueue) EnqueueAt(taskID, agentID, agentName, prompt, outputChannel string, maxRetries int, at time.Time, replyAgentID, replySessionID string) (*domain.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	job := q.newJob(taskID, agentID, agentName, prompt, outputChannel, domain.JobStatusPending, maxRetries, &at, replyAgentID, replySessionID)
	if err := store.WriteJSON(store.JobPath(agentID, job.ID), job); err != nil {
		return nil, fmt.Errorf("enqueue job: %w", err)
	}
	slog.Info("job scheduled", "id", job.ID, "task", taskID, "at", at)
	return job, nil
}

// List returns all jobs, optionally filtered by task ID.
func (q *JobQueue) List(taskID string) ([]domain.Job, error) {
	var jobs []domain.Job
	for _, dir := range store.AllJobDirs() {
		batch, err := store.ListJSON[domain.Job](dir, ".json")
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, batch...)
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

// RecoverStuck resets any jobs left in progress when the server last exited.
// Called on startup to requeue interrupted work immediately.
func (q *JobQueue) RecoverStuck() {
	q.mu.Lock()
	defer q.mu.Unlock()

	var jobs []domain.Job
	for _, dir := range store.AllJobDirs() {
		batch, err := store.ListJSON[domain.Job](dir, ".json")
		if err != nil {
			slog.Warn("recover stuck: listing jobs failed", "err", err)
			continue
		}
		jobs = append(jobs, batch...)
	}
	now := time.Now()
	for i := range jobs {
		j := &jobs[i]
		if j.Status == domain.JobStatusInProgress {
			if j.Attempts >= j.MaxRetries {
				j.Status = domain.JobStatusFailed
				j.NextRetryAt = nil
			} else {
				j.Status = domain.JobStatusPending
			}
			j.LockedAt = nil
			j.UpdatedAt = now
			if err := store.WriteJSON(store.JobPath(j.AgentID, j.ID), j); err != nil {
				slog.Warn("recover stuck: failed to reset job", "id", j.ID, "err", err)
			} else {
				slog.Info("recovered stuck job", "id", j.ID)
			}
		}
	}
}

// Heartbeat refreshes the lease for an in-progress job.
func (q *JobQueue) Heartbeat(id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	path := store.FindJobPath(id)
	if path == "" {
		return fmt.Errorf("job %s not found", id)
	}
	job, err := store.ReadJSON[domain.Job](path)
	if err != nil {
		return fmt.Errorf("reading job %s: %w", id, err)
	}
	if job.Status != domain.JobStatusInProgress {
		return nil
	}
	now := time.Now()
	job.LockedAt = &now
	job.UpdatedAt = now
	return store.WriteJSON(path, &job)
}

// SetSession associates a durable execution session with a job.
func (q *JobQueue) SetSession(id, sessionID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	path := store.FindJobPath(id)
	if path == "" {
		return fmt.Errorf("job %s not found", id)
	}
	job, err := store.ReadJSON[domain.Job](path)
	if err != nil {
		return fmt.Errorf("reading job %s: %w", id, err)
	}
	job.SessionID = sessionID
	job.UpdatedAt = time.Now()
	return store.WriteJSON(path, &job)
}

// UpdateOutput persists the text output captured during a job's execution.
func (q *JobQueue) UpdateOutput(id, output string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	path := store.FindJobPath(id)
	if path == "" {
		return fmt.Errorf("job %s not found", id)
	}
	job, err := store.ReadJSON[domain.Job](path)
	if err != nil {
		return fmt.Errorf("reading job %s: %w", id, err)
	}
	job.Output = output
	job.UpdatedAt = time.Now()
	return store.WriteJSON(path, &job)
}

func (q *JobQueue) updateStatus(id string, status domain.JobStatus) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	path := store.FindJobPath(id)
	if path == "" {
		return fmt.Errorf("job %s not found", id)
	}
	job, err := store.ReadJSON[domain.Job](path)
	if err != nil {
		return fmt.Errorf("reading job %s: %w", id, err)
	}
	job.Status = status
	job.LockedAt = nil
	job.UpdatedAt = time.Now()
	return store.WriteJSON(path, &job)
}
