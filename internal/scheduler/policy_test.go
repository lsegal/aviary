package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/schedule"
)

// newPolicyScheduler returns a scheduler whose clock reads *clock and whose
// worker pool is not started, so enqueued jobs stay pending.
func newPolicyScheduler(t *testing.T, clock *time.Time) *Scheduler {
	t.Helper()
	s, err := New(agent.NewManager(nil), 1)
	require.NoError(t, err)
	t.Cleanup(s.Stop)
	s.now = func() time.Time { return *clock }
	return s
}

func singleTask(tc config.TaskConfig) *config.Config {
	return &config.Config{Agents: []config.AgentConfig{{Name: "alpha", Tasks: []config.TaskConfig{tc}}}}
}

func jobCount(t *testing.T, s *Scheduler, key string) int {
	t.Helper()
	jobs, err := s.Queue().List(key)
	require.NoError(t, err)
	return len(jobs)
}

func lastRun(t *testing.T, s *Scheduler, key string) domain.TaskRun {
	t.Helper()
	h := s.state.get(key).History
	require.NotEmpty(t, h)
	return h[len(h)-1]
}

func TestRunHistoryIsCapped(t *testing.T) {
	var st taskState
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 25 {
		st.addRun(domain.TaskRun{FireTime: base.Add(time.Duration(i) * time.Minute), Status: domain.TaskRunCompleted})
	}
	require.Len(t, st.History, maxRunHistory)
	assert.Equal(t, base.Add(5*time.Minute), st.History[0].FireTime)
	assert.Equal(t, base.Add(24*time.Minute), st.History[maxRunHistory-1].FireTime)
}

func TestOverlapPolicy(t *testing.T) {
	for _, tc := range []struct {
		overlap  string
		wantJobs int
		want     domain.TaskRunStatus
	}{
		{schedule.OverlapSkip, 1, domain.TaskRunSkipped},
		{schedule.OverlapQueue, 2, domain.TaskRunQueued},
	} {
		t.Run(tc.overlap, func(t *testing.T) {
			setupSchedulerDataDir(t)
			now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
			s := newPolicyScheduler(t, &now)
			s.Reconcile(singleTask(config.TaskConfig{Name: "t", Every: "1h", Overlap: tc.overlap, Prompt: "p"}))
			key := "alpha/t"

			s.mu.Lock()
			spec := s.armed[key].spec
			require.NotNil(t, s.fireLocked(key, spec, "schedule", now, nil))
			s.fireLocked(key, spec, "schedule", now.Add(time.Hour), nil)
			s.mu.Unlock()

			assert.Equal(t, tc.wantJobs, jobCount(t, s, key))
			assert.Equal(t, tc.want, lastRun(t, s, key).Status)
		})
	}
}

func TestFireRecordsTrigger(t *testing.T) {
	setupSchedulerDataDir(t)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	s := newPolicyScheduler(t, &now)
	s.Reconcile(singleTask(config.TaskConfig{Name: "t", Every: "1h", Prompt: "p"}))
	key := "alpha/t"

	s.mu.Lock()
	spec := s.armed[key].spec
	job := s.fireLocked(key, spec, "missed", now, nil)
	s.mu.Unlock()

	require.NotNil(t, job)
	assert.Equal(t, "missed", job.Trigger)
	jobs, err := s.Queue().List(key)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	assert.Equal(t, "missed", jobs[0].Trigger, "the stored job keeps what fired it")
}

func TestMissedPolicyOnStartup(t *testing.T) {
	for _, tc := range []struct {
		missed   string
		wantJobs int
		want     domain.TaskRunStatus
	}{
		{schedule.MissedSkip, 0, domain.TaskRunMissed},
		{schedule.MissedRunOnce, 1, domain.TaskRunQueued},
	} {
		t.Run(tc.missed, func(t *testing.T) {
			setupSchedulerDataDir(t)
			now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
			// Persisted state from an earlier process that last fired 5h ago.
			prior := loadStateStore(scheduleStatePath())
			prior.update("alpha/t", func(st *taskState) { st.LastFire = now.Add(-5 * time.Hour) })

			s := newPolicyScheduler(t, &now)
			s.Reconcile(singleTask(config.TaskConfig{Name: "t", Every: "1h", Missed: tc.missed, Prompt: "p"}))
			key := "alpha/t"

			// Five fires were missed; at most one catch-up run happens.
			assert.Equal(t, tc.wantJobs, jobCount(t, s, key))
			run := lastRun(t, s, key)
			assert.Equal(t, "missed", run.Reason)
			assert.Equal(t, tc.want, run.Status)
			require.Contains(t, s.armed, key)
			assert.True(t, s.armed[key].next.After(now))

			// A restart right after settles nothing new.
			s.mu.Lock()
			s.armLocked(key, s.armed[key].spec, true)
			s.mu.Unlock()
			assert.Equal(t, tc.wantJobs, jobCount(t, s, key))
		})
	}
}

func TestMissedFireKeepsFiring(t *testing.T) {
	setupSchedulerDataDir(t)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	prior := loadStateStore(scheduleStatePath())
	prior.update("alpha/t", func(st *taskState) { st.LastFire = now.Add(-time.Hour) })

	s := newPolicyScheduler(t, &now)
	s.Reconcile(singleTask(config.TaskConfig{Name: "t", Every: "5m", Overlap: "skip", Missed: "skip", Prompt: "p"}))
	key := "alpha/t"
	require.Equal(t, domain.TaskRunMissed, lastRun(t, s, key).Status)
	require.Contains(t, s.armed, key)

	// The next two timers each fire and re-arm.
	for range 2 {
		a := s.armed[key]
		now = a.next
		s.onTimer(key, a.gen)
		require.Contains(t, s.armed, key)
		assert.Equal(t, now.Add(5*time.Minute), s.armed[key].next)
	}
	assert.Equal(t, 2, len(s.state.get(key).History)-1, "two fires after the missed one")
	assert.Equal(t, "schedule", lastRun(t, s, key).Reason)
}

func TestResumingPausedTaskDoesNotRecordMissedFire(t *testing.T) {
	setupSchedulerDataDir(t)
	now := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	s := newPolicyScheduler(t, &now)
	tc := config.TaskConfig{Name: "t", Every: "5m", Missed: "run_once", Prompt: "p"}
	s.Reconcile(singleTask(tc))
	key := "alpha/t"
	s.mu.Lock()
	s.fireLocked(key, s.armed[key].spec, "schedule", now, nil)
	s.mu.Unlock()

	paused := tc
	paused.Enabled = new(bool)
	s.Reconcile(singleTask(paused))
	require.NotContains(t, s.armed, key)

	// Hours later the task is resumed: nothing was missed while it was
	// paused, so it neither catches up nor records a missed fire, and its
	// next fire is armed.
	now = now.Add(7 * time.Hour)
	s.Reconcile(singleTask(tc))
	assert.Equal(t, 1, jobCount(t, s, key))
	assert.Equal(t, domain.TaskRunQueued, lastRun(t, s, key).Status)
	require.Contains(t, s.armed, key)
	assert.Equal(t, now.Add(5*time.Minute), s.armed[key].next)
}

func TestNewTaskDoesNotCatchUp(t *testing.T) {
	setupSchedulerDataDir(t)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	s := newPolicyScheduler(t, &now)
	s.Reconcile(singleTask(config.TaskConfig{Name: "t", At: "09:00", Timezone: "UTC", Missed: "run_once", Prompt: "p"}))
	assert.Equal(t, 0, jobCount(t, s, "alpha/t"))
	assert.Equal(t, time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC), s.armed["alpha/t"].next)
}

func TestWakeDetectionSettlesOverdueFire(t *testing.T) {
	setupSchedulerDataDir(t)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	s := newPolicyScheduler(t, &now)
	s.Reconcile(singleTask(config.TaskConfig{Name: "t", Every: "30m", Missed: "run_once", Prompt: "p"}))
	key := "alpha/t"
	require.Equal(t, now.Add(30*time.Minute), s.armed[key].next)

	// The machine sleeps for three hours; the timer has not fired, and the
	// shared wake detector reports the wake.
	now = now.Add(3 * time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	wakes := make(chan time.Duration)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.wakeLoop(ctx, wakes)
	}()
	wakes <- 3 * time.Hour
	cancel()
	<-done

	assert.Equal(t, 1, jobCount(t, s, key))
	assert.Equal(t, "missed", lastRun(t, s, key).Reason)
	assert.Equal(t, now.Add(30*time.Minute), s.armed[key].next)

	// A late timer for the old fire is ignored.
	s.onTimer(key, s.armed[key].gen-1)
	assert.Equal(t, 1, jobCount(t, s, key))
}

func TestRunOnceStaysDoneAcrossRestarts(t *testing.T) {
	setupSchedulerDataDir(t)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	prior := loadStateStore(scheduleStatePath())
	prior.update("alpha/t", func(st *taskState) { st.LastFire = now.Add(-time.Minute) })

	s := newPolicyScheduler(t, &now)
	s.Reconcile(singleTask(config.TaskConfig{Name: "t", Schedule: "*/5 * * * *", RunOnce: true, Prompt: "p"}))
	assert.NotContains(t, s.armed, "alpha/t")
	fires, err := s.Preview("t", 3)
	require.NoError(t, err)
	assert.Empty(t, fires)
}

func TestRecordAttemptUpdatesHistory(t *testing.T) {
	setupSchedulerDataDir(t)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	s := newPolicyScheduler(t, &now)
	s.Reconcile(singleTask(config.TaskConfig{Name: "t", Every: "1h", Prompt: "p"}))
	key := "alpha/t"
	s.mu.Lock()
	job := s.fireLocked(key, s.armed[key].spec, "schedule", now, nil)
	s.mu.Unlock()
	require.NotNil(t, job)

	job.Status = domain.JobStatusCompleted
	job.Output = "\n  Sent the digest to #general.\nmore detail"
	s.recordAttempt(*job, 1500*time.Millisecond)

	run := lastRun(t, s, key)
	assert.Equal(t, domain.TaskRunCompleted, run.Status)
	assert.Equal(t, "Sent the digest to #general.", run.Summary)
	assert.Equal(t, int64(1500), run.DurationMS)

	tasks := s.ListTasks()
	require.Len(t, tasks, 1)
	assert.Len(t, tasks[0].History, 1)
	require.NotNil(t, tasks[0].LastFire)
	assert.Equal(t, now, *tasks[0].LastFire)
}

func TestUpcomingLimitsPerTask(t *testing.T) {
	setupSchedulerDataDir(t)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	s := newPolicyScheduler(t, &now)
	s.Reconcile(&config.Config{Agents: []config.AgentConfig{{Name: "alpha", Tasks: []config.TaskConfig{
		{Name: "fast", Every: "1m", Prompt: "p"},
		{Name: "slow", Every: "2m", Prompt: "p"},
	}}}})

	runs := s.Upcoming(5, 3)
	require.Len(t, runs, 5)
	perTask := map[string]int{}
	for i, r := range runs {
		perTask[r.TaskID]++
		if i > 0 {
			assert.False(t, r.At.Before(runs[i-1].At))
		}
	}
	assert.Equal(t, 3, perTask["alpha/fast"])
	assert.Equal(t, 2, perTask["alpha/slow"])
}

// listedTask returns key's entry in ListTasks.
func listedTask(t *testing.T, s *Scheduler, key string) domain.ScheduledTask {
	t.Helper()
	for _, task := range s.ListTasks() {
		if task.ID == key {
			return task
		}
	}
	t.Fatalf("task %q not listed", key)
	return domain.ScheduledTask{}
}

// fireArmed runs key's armed timer as if it went off on time.
func fireArmed(t *testing.T, s *Scheduler, key string) {
	t.Helper()
	s.mu.Lock()
	a := s.armed[key]
	s.mu.Unlock()
	require.NotNil(t, a)
	s.onTimer(key, a.gen)
}

func TestListTasksNextRunAroundAFire(t *testing.T) {
	t.Run("once", func(t *testing.T) {
		setupSchedulerDataDir(t)
		now := time.Date(2026, 11, 21, 19, 0, 0, 0, time.UTC)
		s := newPolicyScheduler(t, &now)
		at := now.Add(11 * time.Minute)
		s.Reconcile(singleTask(config.TaskConfig{Name: "t", RunOnce: true, StartAt: at.Format(time.RFC3339), Prompt: "p"}))
		key := "alpha/t"

		before := listedTask(t, s, key)
		require.NotNil(t, before.NextRun)
		assert.True(t, at.Equal(*before.NextRun))

		now = at
		fireArmed(t, s, key)

		// The fired one-time trigger has no next run; its run in flight is
		// in the history, which is what the board shows while it runs.
		during := listedTask(t, s, key)
		assert.Nil(t, during.NextRun)
		require.NotEmpty(t, during.History)
		assert.Equal(t, domain.TaskRunQueued, during.History[len(during.History)-1].Status)
		assert.NotContains(t, s.armed, key)
	})

	t.Run("recurring", func(t *testing.T) {
		setupSchedulerDataDir(t)
		now := time.Date(2026, 11, 21, 19, 0, 0, 0, time.UTC)
		s := newPolicyScheduler(t, &now)
		s.Reconcile(singleTask(config.TaskConfig{Name: "t", Every: "1h", Prompt: "p"}))
		key := "alpha/t"

		before := listedTask(t, s, key)
		require.NotNil(t, before.NextRun)
		fired := *before.NextRun

		now = fired
		fireArmed(t, s, key)

		// A recurring trigger re-arms as it fires, so its next fire time is
		// listed while the run it just started is still in flight.
		during := listedTask(t, s, key)
		require.NotNil(t, during.NextRun)
		assert.True(t, fired.Add(time.Hour).Equal(*during.NextRun), "next run %s", during.NextRun)
		require.NotEmpty(t, during.History)
		assert.Equal(t, domain.TaskRunQueued, during.History[len(during.History)-1].Status)
	})
}

func TestRecordAttemptStatusAndObserver(t *testing.T) {
	setupSchedulerDataDir(t)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	s := newPolicyScheduler(t, &now)
	s.Reconcile(singleTask(config.TaskConfig{Name: "t", Every: "1h", Prompt: "p"}))
	key := "alpha/t"
	type observed struct {
		status  domain.TaskRunStatus
		summary string
	}
	var seen []observed
	s.SetRunObserver(func(_ domain.Job, status domain.TaskRunStatus, summary string) {
		seen = append(seen, observed{status, summary})
	})

	for _, tc := range []struct {
		status      domain.JobStatus
		output, err string
		want        domain.TaskRunStatus
		summary     string
	}{
		{domain.JobStatusFailed, "partial work", "agent exited 1", domain.TaskRunFailed, "agent exited 1"},
		{domain.JobStatusPending, "partial work", "rate limited", domain.TaskRunRetrying, "rate limited"},
		{domain.JobStatusFailed, "partial work", "", domain.TaskRunFailed, "partial work"},
		{domain.JobStatusCanceled, "stopped early", "ignored", domain.TaskRunCanceled, "stopped early"},
	} {
		s.mu.Lock()
		job := s.fireLocked(key, s.armed[key].spec, "schedule", now, nil)
		s.mu.Unlock()
		require.NotNil(t, job)
		job.Status, job.Output, job.Error = tc.status, tc.output, tc.err
		s.recordAttempt(*job, time.Second)

		run := lastRun(t, s, key)
		assert.Equal(t, tc.want, run.Status)
		assert.Equal(t, tc.summary, run.Summary)
		require.NotEmpty(t, seen)
		assert.Equal(t, observed{tc.want, tc.summary}, seen[len(seen)-1])
	}

	s.SetRunObserver(nil)
	seen = nil
	s.recordAttempt(domain.Job{TaskID: key, Status: domain.JobStatusCompleted}, time.Second)
	assert.Empty(t, seen, "a cleared observer hears nothing")
}

func TestTaskRunStatus(t *testing.T) {
	for status, want := range map[domain.JobStatus]domain.TaskRunStatus{
		domain.JobStatusPending:    domain.TaskRunQueued,
		domain.JobStatusInProgress: domain.TaskRunRunning,
		domain.JobStatusFailed:     domain.TaskRunFailed,
		domain.JobStatusCanceled:   domain.TaskRunCanceled,
		domain.JobStatusCompleted:  domain.TaskRunCompleted,
	} {
		assert.Equal(t, want, taskRunStatus(status), string(status))
	}
}

func TestForgetDropsTaskHistory(t *testing.T) {
	setupSchedulerDataDir(t)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	s := newPolicyScheduler(t, &now)
	s.Reconcile(singleTask(config.TaskConfig{Name: "t", Every: "1h", Prompt: "p"}))
	fireArmed(t, s, "alpha/t")
	require.NotEmpty(t, listedTask(t, s, "alpha/t").History)

	s.Forget("alpha/t")
	task := listedTask(t, s, "alpha/t")
	assert.Empty(t, task.History)
	assert.Nil(t, task.LastFire)
}

func TestPreviewWithoutTimeTrigger(t *testing.T) {
	setupSchedulerDataDir(t)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	s := newPolicyScheduler(t, &now)
	s.Reconcile(&config.Config{Agents: []config.AgentConfig{{Name: "alpha", Tasks: []config.TaskConfig{
		{Name: "flow", Trigger: config.TriggerMessage, Channel: "team", Prompt: "p"},
		{Name: "watcher", Watch: "*.txt", Prompt: "p"},
		{Name: "broken", Schedule: "not a cron", Prompt: "p"},
	}}}})

	_, err := s.Preview("missing", 3)
	assert.ErrorContains(t, err, `task "missing" not found`)
	for _, name := range []string{"flow", "alpha/watcher"} {
		fires, err := s.Preview(name, 3)
		require.NoError(t, err, name)
		assert.Empty(t, fires, "%s has no time trigger", name)
	}
	_, err = s.Preview("broken", 3)
	assert.Error(t, err, "an invalid schedule cannot be previewed")
}
