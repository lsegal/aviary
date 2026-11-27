package scheduler

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/saferestart"
	"github.com/lsegal/aviary/internal/store"
)

// startFakeRun claims job and registers it as running until it is canceled,
// then finishes it the way processJob does.
func startFakeRun(t *testing.T, p *WorkerPool, job *domain.Job) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	p.registerActiveJob(job.ID, job.TaskID, job.AgentID, cancel)
	go func() {
		<-ctx.Done()
		p.finishCanceled(job.ID)
		p.unregisterActiveJob(job.ID)
	}()
}

func TestScheduler_CheckpointRequeuesRunningJobsWithRestartNote(t *testing.T) {
	setupSchedulerDataDir(t)
	s, err := New(agent.NewManager(nil), 1)
	require.NoError(t, err)
	t.Cleanup(s.Stop)

	_, err = s.queue.Enqueue("alpha/daily", "alpha", "write the report", "", 1, "", "")
	require.NoError(t, err)
	job, err := s.queue.Claim()
	require.NoError(t, err)
	require.NotNil(t, job)
	startFakeRun(t, s.pool, job)

	assert.Equal(t, []saferestart.Job{{Source: "task", ID: job.ID, Title: "alpha/daily"}}, s.RunningJobs())

	s.HoldNewJobs(true)
	assert.True(t, s.pool.held.Load())

	marker := saferestart.Marker{Reason: "upgrade", FromVersion: "v0.6.2", ToVersion: "v0.7.0"}
	require.NoError(t, s.Checkpoint(context.Background(), marker))
	assert.Empty(t, s.RunningJobs())

	saved, err := store.ReadJSON[domain.Job](store.FindJobPath(job.ID))
	require.NoError(t, err)
	assert.Equal(t, domain.JobStatusPending, saved.Status, "the interrupted job waits to resume, not canceled")
	assert.Equal(t, 0, saved.Attempts, "the interrupted attempt does not count against retries")
	assert.Equal(t, marker.Message(), saved.ResumeNote)

	// After the restart a fresh queue claims it again, even with one retry.
	again, err := NewJobQueue().Claim()
	require.NoError(t, err)
	require.NotNil(t, again)
	assert.Equal(t, job.ID, again.ID)
}

func TestScheduler_StopWithoutCheckpointStillCancels(t *testing.T) {
	setupSchedulerDataDir(t)
	s, err := New(agent.NewManager(nil), 1)
	require.NoError(t, err)

	_, err = s.queue.Enqueue("alpha/daily", "alpha", "p", "", 1, "", "")
	require.NoError(t, err)
	job, err := s.queue.Claim()
	require.NoError(t, err)
	startFakeRun(t, s.pool, job)

	s.pool.StopJobs(nil)
	assert.Eventually(t, func() bool {
		saved, err := store.ReadJSON[domain.Job](store.FindJobPath(job.ID))
		return err == nil && saved.Status == domain.JobStatusCanceled
	}, 2*time.Second, 10*time.Millisecond)
}

func TestWorkerPool_ResumedJobGetsRestartNoteInItsSession(t *testing.T) {
	setupSchedulerDataDir(t)
	mgr := agent.NewManager(nil)
	mgr.Reconcile(&config.Config{Agents: []config.AgentConfig{{Name: "alpha", Model: "m"}}})
	q := NewJobQueue()
	p := NewWorkerPool(q, mgr, 1)

	sess, err := agent.NewSessionManager().GetOrCreateNamed("alpha", "daily")
	require.NoError(t, err)
	queued, err := q.Enqueue("alpha/daily", "alpha", "finish report", "", 1, "", "")
	require.NoError(t, err)
	require.NoError(t, q.SetSession(queued.ID, sess.ID))
	note := saferestart.Marker{Reason: "restart"}.Message()
	require.NoError(t, q.Requeue(queued.ID, note))

	job, err := q.Claim()
	require.NoError(t, err)
	require.NoError(t, p.executeJob(context.Background(), job))

	data, err := os.ReadFile(store.SessionPath("alpha", sess.ID))
	require.NoError(t, err)
	assert.Contains(t, string(data), "Aviary restarted")
	assert.Contains(t, string(data), "Continue the unfinished scheduled task")

	saved, err := store.ReadJSON[domain.Job](store.FindJobPath(job.ID))
	require.NoError(t, err)
	assert.Empty(t, saved.ResumeNote, "the note is delivered once")
}
