package scheduler

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

// putJob writes job to disk as-is, filling in its ID and timestamps.
func putJob(t *testing.T, job domain.Job) domain.Job {
	t.Helper()
	if job.ID == "" {
		job.ID = newID("job")
	}
	if job.AgentID == "" {
		job.AgentID = "alpha"
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
		job.UpdatedAt = job.CreatedAt
	}
	require.NoError(t, store.WriteJSON(store.JobPath(job.AgentID, job.ID), &job))
	return job
}

func readJob(t *testing.T, id string) domain.Job {
	t.Helper()
	path := store.FindJobPath(id)
	require.NotEmpty(t, path, "job %s is on disk", id)
	job, err := store.ReadJSON[domain.Job](path)
	require.NoError(t, err)
	return job
}

// breakJobDir replaces agent's jobs directory with a file, so listing it fails.
func breakJobDir(t *testing.T, agentID string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows reports reading a file as a directory as not found, which listing treats as empty")
	}
	require.NoError(t, os.MkdirAll(store.AgentDir(agentID), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(store.AgentDir(agentID), "jobs"), []byte("x"), 0o600))
}

// Back-to-back IDs never collide, even when the clock has not ticked between
// them, and they still sort in creation order.
func TestNewIDIsUniqueAndOrdered(t *testing.T) {
	ids := make([]string, 1000)
	for i := range ids {
		ids[i] = newID("job")
	}
	seen := map[string]bool{}
	for i, id := range ids {
		require.False(t, seen[id], "duplicate id %s", id)
		seen[id] = true
		if i > 0 {
			require.Less(t, ids[i-1], id)
		}
	}
}

func TestJobQueue_ByIDMethodsReportMissingAndUnreadableJobs(t *testing.T) {
	setupSchedulerDataDir(t)
	q := NewJobQueue()
	const corrupt = "job_corrupt"
	require.NoError(t, os.MkdirAll(filepath.Dir(store.JobPath("alpha", corrupt)), 0o700))
	require.NoError(t, os.WriteFile(store.JobPath("alpha", corrupt), []byte("not-json"), 0o600))

	for _, tc := range []struct {
		name string
		call func(id string) error
	}{
		{"ForceStart", func(id string) error { _, err := q.ForceStart(id); return err }},
		{"Requeue", func(id string) error { return q.Requeue(id, "note") }},
		{"ClearResumeNote", q.ClearResumeNote},
		{"Complete", q.Complete},
		{"Cancel", q.Cancel},
		{"Fail", func(id string) error { return q.Fail(id, errors.New("boom")) }},
		{"Heartbeat", q.Heartbeat},
		{"SetSession", func(id string) error { return q.SetSession(id, "s") }},
		{"SetTrigger", func(id string) error { return q.SetTrigger(id, "manual") }},
		{"SetTriggerContext", func(id string) error { return q.SetTriggerContext(id, &domain.TriggerContext{}) }},
		{"SetDelivery", func(id string) error { return q.SetDelivery(id, &domain.JobDelivery{}) }},
		{"UpdateOutput", func(id string) error { return q.UpdateOutput(id, "out") }},
		{"SetScriptError", func(id string) error { return q.SetScriptError(id, "bad") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.ErrorContains(t, tc.call("job_missing"), "job job_missing not found")
			assert.ErrorContains(t, tc.call(corrupt), "reading job "+corrupt)
		})
	}
}

func TestJobQueue_ClaimSkipsUnclaimableJobs(t *testing.T) {
	future := time.Now().Add(time.Hour)
	fresh := time.Now()
	for _, tc := range []struct {
		name string
		job  domain.Job
	}{
		{"waiting to retry", domain.Job{Status: domain.JobStatusPending, MaxRetries: 3, Attempts: 1, NextRetryAt: &future}},
		{"scheduled later", domain.Job{Status: domain.JobStatusPending, MaxRetries: 3, ScheduledFor: &future}},
		{"running with a live lock", domain.Job{Status: domain.JobStatusInProgress, MaxRetries: 3, Attempts: 1, LockedAt: &fresh}},
		{"finished", domain.Job{Status: domain.JobStatusCompleted, MaxRetries: 3, Attempts: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupSchedulerDataDir(t)
			job := putJob(t, tc.job)
			claimed, err := NewJobQueue().Claim()
			require.NoError(t, err)
			assert.Nil(t, claimed)
			assert.Equal(t, tc.job.Status, readJob(t, job.ID).Status)
		})
	}
}

func TestJobQueue_ClaimRecoversStaleLock(t *testing.T) {
	setupSchedulerDataDir(t)
	stale := time.Now().Add(-2 * lockTimeout)
	job := putJob(t, domain.Job{Status: domain.JobStatusInProgress, MaxRetries: 3, Attempts: 1, LockedAt: &stale})

	claimed, err := NewJobQueue().Claim()
	require.NoError(t, err)
	require.NotNil(t, claimed)
	assert.Equal(t, job.ID, claimed.ID)
	assert.Equal(t, 2, claimed.Attempts)
	saved := readJob(t, job.ID)
	assert.Equal(t, domain.JobStatusInProgress, saved.Status)
	require.NotNil(t, saved.LockedAt)
	assert.True(t, saved.LockedAt.After(stale))
}

func TestJobQueue_ClaimFailsExhaustedJobs(t *testing.T) {
	for _, tc := range []struct {
		name, prior, want string
	}{
		{"without an error", "", "no attempts remain"},
		{"keeps the last error", "boom", "boom"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupSchedulerDataDir(t)
			job := putJob(t, domain.Job{Status: domain.JobStatusPending, MaxRetries: 1, Attempts: 1, Error: tc.prior})

			claimed, err := NewJobQueue().Claim()
			require.NoError(t, err)
			assert.Nil(t, claimed)
			saved := readJob(t, job.ID)
			assert.Equal(t, domain.JobStatusFailed, saved.Status)
			assert.Equal(t, tc.want, saved.Error)
			assert.Nil(t, saved.LockedAt)
		})
	}
}

func TestJobQueue_UnlistableJobDir(t *testing.T) {
	setupSchedulerDataDir(t)
	q := NewJobQueue()
	job := putJob(t, domain.Job{Status: domain.JobStatusInProgress, MaxRetries: 3, Attempts: 1})
	breakJobDir(t, "broken")

	_, err := q.Claim()
	assert.ErrorContains(t, err, "listing jobs")
	_, err = q.List("")
	assert.Error(t, err)

	// Startup recovery skips the unreadable directory and still recovers the
	// rest.
	q.RecoverStuck()
	assert.Equal(t, domain.JobStatusPending, readJob(t, job.ID).Status)
}

func TestJobQueue_StartImmediate(t *testing.T) {
	setupSchedulerDataDir(t)
	q := NewJobQueue()

	job, err := q.StartImmediate("alpha/adhoc", "alpha", "hello", "session:ops", "alpha", "sess")
	require.NoError(t, err)
	saved := readJob(t, job.ID)
	assert.Equal(t, domain.JobStatusInProgress, saved.Status)
	assert.Equal(t, "prompt", saved.TaskType)
	assert.Equal(t, 1, saved.Attempts)
	assert.Equal(t, 1, saved.MaxRetries)
	assert.NotNil(t, saved.LockedAt)
	assert.Equal(t, "session:ops", saved.OutputChannel)
	assert.Equal(t, "sess", saved.ReplySessionID)

	// It is already running, so it cannot be force-started again.
	_, err = q.ForceStart(job.ID)
	assert.ErrorContains(t, err, "is not pending")
}

func TestJobQueue_Heartbeat(t *testing.T) {
	setupSchedulerDataDir(t)
	q := NewJobQueue()
	old := time.Now().Add(-time.Minute)
	running := putJob(t, domain.Job{Status: domain.JobStatusInProgress, MaxRetries: 3, Attempts: 1, LockedAt: &old})
	pending := putJob(t, domain.Job{Status: domain.JobStatusPending, MaxRetries: 3})

	require.NoError(t, q.Heartbeat(running.ID))
	saved := readJob(t, running.ID)
	require.NotNil(t, saved.LockedAt)
	assert.True(t, saved.LockedAt.After(old), "the lease is renewed")

	require.NoError(t, q.Heartbeat(pending.ID))
	assert.Nil(t, readJob(t, pending.ID).LockedAt, "a job that is not running takes no lease")
}

func TestJobQueue_ClearResumeNote(t *testing.T) {
	setupSchedulerDataDir(t)
	q := NewJobQueue()
	job := putJob(t, domain.Job{Status: domain.JobStatusInProgress, MaxRetries: 3, Attempts: 1})

	require.NoError(t, q.Requeue(job.ID, "resume here"))
	assert.Equal(t, "resume here", readJob(t, job.ID).ResumeNote)
	require.NoError(t, q.ClearResumeNote(job.ID))
	assert.Empty(t, readJob(t, job.ID).ResumeNote)
	require.NoError(t, q.ClearResumeNote(job.ID), "clearing an absent note is a no-op")
}

func TestJobQueue_FailBackoffIsCapped(t *testing.T) {
	setupSchedulerDataDir(t)
	job := putJob(t, domain.Job{Status: domain.JobStatusInProgress, MaxRetries: 20, Attempts: 10})

	before := time.Now()
	require.NoError(t, NewJobQueue().Fail(job.ID, errors.New("boom")))
	saved := readJob(t, job.ID)
	assert.Equal(t, domain.JobStatusPending, saved.Status)
	require.NotNil(t, saved.NextRetryAt)
	assert.WithinDuration(t, before.Add(retryMax), *saved.NextRetryAt, time.Minute)
}

func TestJobMatchesTask(t *testing.T) {
	for _, tc := range []struct {
		job, task string
		want      bool
	}{
		{"alpha/daily", "alpha/daily", true},
		{"alpha/daily", "daily", true},
		{"alpha/daily", "beta/daily", false},
		{"alpha/daily", "nightly", false},
		{"daily", "daily", true},
		{"adhoc", "daily", false},
	} {
		assert.Equal(t, tc.want, jobMatchesTask(tc.job, tc.task), "%s matches %s", tc.job, tc.task)
	}
}

func TestJobQueue_ClaimRenewsLapsedLeaseOfRunningJob(t *testing.T) {
	setupSchedulerDataDir(t)
	q := NewJobQueue()
	stale := time.Now().Add(-2 * lockTimeout)
	job := putJob(t, domain.Job{Status: domain.JobStatusInProgress, MaxRetries: 1, Attempts: 1, LockedAt: &stale})
	done := q.markRunning(job.ID)

	// A worker here is still running the job, so its lapsed lease (the host
	// slept past the heartbeat) neither fails nor re-claims it (#1413).
	claimed, err := q.Claim()
	require.NoError(t, err)
	assert.Nil(t, claimed)
	saved := readJob(t, job.ID)
	assert.Equal(t, domain.JobStatusInProgress, saved.Status)
	assert.Empty(t, saved.Error)
	require.NotNil(t, saved.LockedAt)
	assert.True(t, saved.LockedAt.After(stale))

	// Once no worker runs it, a lapsed lease means the run was abandoned,
	// and the error says so.
	done()
	old := time.Now().Add(-2 * lockTimeout)
	saved.LockedAt = &old
	require.NoError(t, store.WriteJSON(store.JobPath(saved.AgentID, saved.ID), &saved))
	claimed, err = q.Claim()
	require.NoError(t, err)
	assert.Nil(t, claimed)
	saved = readJob(t, job.ID)
	assert.Equal(t, domain.JobStatusFailed, saved.Status)
	assert.Equal(t, abandonedError, saved.Error)
	assert.Contains(t, saved.Error, "run abandoned")
}
