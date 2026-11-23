package scheduler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/schedule"
	"github.com/lsegal/aviary/internal/store"
)

func TestHaltStopsSchedulingOnShutdown(t *testing.T) {
	setupSchedulerDataDir(t)
	now := time.Now().Round(0)
	s := newPolicyScheduler(t, &now)
	key := "alpha/t"
	s.Reconcile(singleTask(config.TaskConfig{Name: "t", Every: "1h", Prompt: "p"}))
	s.mu.Lock()
	spec := s.armed[key].spec
	s.mu.Unlock()
	pending, err := s.Queue().Enqueue("alpha/other", "alpha", "p", "", 0, "", "")
	require.NoError(t, err)

	s.Halt()
	s.Halt() // idempotent

	s.mu.Lock()
	assert.Empty(t, s.armed, "every trigger is disarmed")
	assert.Nil(t, s.fireLocked(key, spec, "schedule", now), "timer fires enqueue nothing")
	s.mu.Unlock()
	s.fireFromWatch(key)
	assert.Equal(t, 0, jobCount(t, s, key), "nothing is enqueued after shutdown begins")

	_, err = s.Trigger(key)
	assert.ErrorIs(t, err, errHalted)
	_, err = s.RunJobNow(pending.ID)
	assert.ErrorIs(t, err, errHalted)
	persisted, err := store.ReadJSON[domain.Job](store.JobPath(pending.AgentID, pending.ID))
	require.NoError(t, err)
	assert.Equal(t, domain.JobStatusPending, persisted.Status, "nothing is started after shutdown begins")

	s.Reconcile(singleTask(config.TaskConfig{Name: "t", Every: "5m", Prompt: "p"}))
	s.mu.Lock()
	assert.Empty(t, s.armed, "a config reload during shutdown arms nothing")
	s.mu.Unlock()
}

func TestStrandedRunsReconciledOnStartup(t *testing.T) {
	setupSchedulerDataDir(t)
	s, err := New(agent.NewManager(nil), 1)
	require.NoError(t, err)

	q := s.Queue()
	running, err := q.Enqueue("alpha/running", "alpha", "p", "", 0, "", "")
	require.NoError(t, err)
	claimed, err := q.Claim()
	require.NoError(t, err)
	require.Equal(t, running.ID, claimed.ID)
	done, err := q.Enqueue("alpha/done", "alpha", "p", "", 0, "", "")
	require.NoError(t, err)
	require.NoError(t, q.Complete(done.ID))
	retrying, err := q.Enqueue("alpha/retrying", "alpha", "p", "", 0, "", "")
	require.NoError(t, err)
	fire := time.Now().Round(0)
	for key, run := range map[string]domain.TaskRun{
		"alpha/running":  {JobID: running.ID, Status: domain.TaskRunRunning},
		"alpha/done":     {JobID: done.ID, Status: domain.TaskRunQueued},
		"alpha/retrying": {JobID: retrying.ID, Status: domain.TaskRunRetrying},
		"alpha/gone":     {JobID: "job_missing", Status: domain.TaskRunRunning},
		"alpha/skipped":  {Status: domain.TaskRunSkipped},
	} {
		run.FireTime = fire
		s.state.update(key, func(t *taskState) { t.addRun(run) })
	}
	s.Stop()

	s, err = New(agent.NewManager(nil), 1)
	require.NoError(t, err)
	t.Cleanup(s.Stop)

	assert.Equal(t, domain.TaskRunQueued, lastRun(t, s, "alpha/running").Status, "an interrupted run is requeued")
	assert.Equal(t, domain.TaskRunCompleted, lastRun(t, s, "alpha/done").Status)
	assert.Equal(t, domain.TaskRunRetrying, lastRun(t, s, "alpha/retrying").Status)
	gone := lastRun(t, s, "alpha/gone")
	assert.Equal(t, domain.TaskRunCanceled, gone.Status, "a run whose job is gone is cleared")
	assert.Contains(t, gone.Summary, "interrupted")
	assert.Equal(t, domain.TaskRunSkipped, lastRun(t, s, "alpha/skipped").Status)

	persisted, err := store.ReadJSON[domain.Job](store.JobPath(running.AgentID, running.ID))
	require.NoError(t, err)
	assert.Equal(t, domain.JobStatusPending, persisted.Status)
}

func TestStaleRunNoLongerBlocksOverlapPolicy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		advance time.Duration
		prepare func(*testing.T, *JobQueue, *domain.Job)
	}{
		{"unclaimed queued run", staleQueuedTimeout + time.Minute, nil},
		{"run without heartbeat", lockTimeout + time.Minute, func(t *testing.T, q *JobQueue, j *domain.Job) {
			claimed, err := q.Claim()
			require.NoError(t, err)
			require.Equal(t, j.ID, claimed.ID)
		}},
		{"run past the stale-run timeout", staleRunTimeout + time.Minute, func(t *testing.T, q *JobQueue, j *domain.Job) {
			claimed, err := q.Claim()
			require.NoError(t, err)
			require.Equal(t, j.ID, claimed.ID)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupSchedulerDataDir(t)
			now := time.Now().Round(0)
			s := newPolicyScheduler(t, &now)
			key := "alpha/t"
			s.Reconcile(singleTask(config.TaskConfig{Name: "t", Every: "5m", Overlap: schedule.OverlapSkip, Prompt: "p"}))
			s.mu.Lock()
			spec := s.armed[key].spec
			first := s.fireLocked(key, spec, "schedule", now)
			s.mu.Unlock()
			require.NotNil(t, first)
			if tc.prepare != nil {
				tc.prepare(t, s.Queue(), first)
			}

			s.mu.Lock()
			assert.Nil(t, s.fireLocked(key, spec, "schedule", now), "a live run still blocks")
			now = now.Add(tc.advance)
			if tc.name == "run past the stale-run timeout" {
				// Keep the heartbeat fresh so only the run's age counts.
				job, err := store.ReadJSON[domain.Job](store.JobPath(first.AgentID, first.ID))
				require.NoError(t, err)
				job.LockedAt = &now
				require.NoError(t, store.WriteJSON(store.JobPath(first.AgentID, first.ID), &job))
			}
			assert.NotNil(t, s.fireLocked(key, spec, "schedule", now), "a stale run no longer blocks")
			s.mu.Unlock()
			assert.Equal(t, domain.TaskRunQueued, lastRun(t, s, key).Status)
		})
	}
}

func TestStaleJobHonorsFutureRetry(t *testing.T) {
	now := time.Now()
	old := now.Add(-2 * staleQueuedTimeout)
	retry := now.Add(time.Minute)
	assert.False(t, staleJob(domain.Job{Status: domain.JobStatusPending, UpdatedAt: old, NextRetryAt: &retry}, now))
	assert.True(t, staleJob(domain.Job{Status: domain.JobStatusPending, UpdatedAt: old}, now))
	assert.True(t, staleJob(domain.Job{Status: domain.JobStatusInProgress, CreatedAt: now}, now), "no lock means no live worker")
}
