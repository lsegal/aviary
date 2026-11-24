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
	"github.com/lsegal/aviary/internal/saferestart"
	"github.com/lsegal/aviary/internal/schedule"
	"github.com/lsegal/aviary/internal/store"
)

// fallbackRun is a running fallback model run of a precompiled project task:
// its script failed and the model run waits until it is released or stopped.
type fallbackRun struct {
	s        *Scheduler
	key      string
	job      *domain.Job
	fellBack chan struct{}
	release  chan struct{}
	stopped  chan error
	done     chan struct{}
}

// startFallbackRun fires a scheduled project task every 5 minutes with
// precompile on, under overlap, and runs it until its model run is going.
func startFallbackRun(t *testing.T, now *time.Time, overlap string) *fallbackRun {
	t.Helper()
	setupSchedulerDataDir(t)
	precompile := true
	cfg := &config.Config{Agents: []config.AgentConfig{{Name: "alpha", Model: "m", Tasks: []config.TaskConfig{{
		Name: "release", Type: "project", Every: "5m", Overlap: overlap, Project: "o/r",
		Prompt: "Trigger release.yml when main moved", Precompile: &precompile,
	}}}}}
	mgr := agent.NewManager(nil)
	mgr.Reconcile(cfg)
	s, err := New(mgr, 1)
	require.NoError(t, err)
	t.Cleanup(s.Stop)
	s.now = func() time.Time { return *now }
	s.Reconcile(cfg)

	r := &fallbackRun{s: s, key: "alpha/release", fellBack: make(chan struct{}), release: make(chan struct{}), stopped: make(chan error, 1), done: make(chan struct{})}
	s.pool.taskCompiler = func(context.Context, string, string) (string, error) {
		return `error("failed to read main SHA: gh: Not Found (HTTP 404)")`, nil
	}
	checkout := t.TempDir()
	s.pool.projectRunner = func(ctx context.Context, run ProjectRun) (string, error) {
		_, err := run.RunCompiled(ctx, func(context.Context) (string, error) { return checkout, nil }, func(string, ...any) {})
		require.Error(t, err)
		close(r.fellBack)
		select {
		case <-r.release:
			return "Triggered release.yml", nil
		case <-ctx.Done():
			r.stopped <- ctx.Err()
			return "", ctx.Err()
		}
	}

	s.mu.Lock()
	fired := s.fireLocked(r.key, s.armed[r.key].spec, "schedule", *now, nil)
	s.mu.Unlock()
	require.NotNil(t, fired)
	r.job, err = s.queue.Claim()
	require.NoError(t, err)
	require.Equal(t, fired.ID, r.job.ID)
	go func() {
		defer close(r.done)
		s.pool.processJob(context.Background(), r.job)
	}()
	select {
	case <-r.fellBack:
	case <-time.After(5 * time.Second):
		t.Fatal("the precompiled script never fell back to the model")
	}
	return r
}

func (r *fallbackRun) saved(t *testing.T) domain.Job {
	t.Helper()
	job, err := store.ReadJSON[domain.Job](store.FindJobPath(r.job.ID))
	require.NoError(t, err)
	return job
}

func (r *fallbackRun) wait(t *testing.T) {
	t.Helper()
	select {
	case <-r.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the fallback run never finished")
	}
}

func TestNextFireDoesNotCancelARunningFallbackRun(t *testing.T) {
	for _, tc := range []struct {
		overlap string
		next    domain.TaskRunStatus
	}{
		{"", domain.TaskRunQueued},
		{schedule.OverlapSkip, domain.TaskRunSkipped},
	} {
		t.Run("overlap="+tc.overlap, func(t *testing.T) {
			now := time.Date(2026, 11, 23, 17, 52, 56, 0, time.UTC)
			r := startFallbackRun(t, &now, tc.overlap)

			now = now.Add(5 * time.Minute)
			r.s.mu.Lock()
			r.s.fireLocked(r.key, r.s.armed[r.key].spec, "schedule", now, nil)
			r.s.mu.Unlock()
			assert.Equal(t, tc.next, lastRun(t, r.s, r.key).Status)

			select {
			case err := <-r.stopped:
				t.Fatalf("the next fire stopped the running fallback run: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			assert.Equal(t, domain.JobStatusInProgress, r.saved(t).Status)

			close(r.release)
			r.wait(t)
			saved := r.saved(t)
			assert.Equal(t, domain.JobStatusCompleted, saved.Status)
			assert.Contains(t, saved.Output, "Triggered release.yml")
			assert.Contains(t, saved.Output, "fell back to the model")
		})
	}
}

func TestCheckpointRequeuesARunningFallbackRun(t *testing.T) {
	now := time.Date(2026, 11, 23, 17, 52, 56, 0, time.UTC)
	r := startFallbackRun(t, &now, "")

	marker := saferestart.Marker{Reason: "restart"}
	require.NoError(t, r.s.Checkpoint(context.Background(), marker))
	r.wait(t)
	saved := r.saved(t)
	assert.Equal(t, domain.JobStatusPending, saved.Status, "a restart requeues the fallback run instead of canceling it")
	assert.Equal(t, marker.Message(), saved.ResumeNote)
}
