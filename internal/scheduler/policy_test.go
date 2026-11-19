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
			require.NotNil(t, s.fireLocked(key, spec, "schedule", now))
			s.fireLocked(key, spec, "schedule", now.Add(time.Hour))
			s.mu.Unlock()

			assert.Equal(t, tc.wantJobs, jobCount(t, s, key))
			assert.Equal(t, tc.want, lastRun(t, s, key).Status)
		})
	}
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

	// The machine sleeps for three hours; the timer has not fired.
	now = now.Add(3 * time.Hour)
	s.checkOverdue()

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
	job := s.fireLocked(key, s.armed[key].spec, "schedule", now)
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
