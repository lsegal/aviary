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
	"github.com/lsegal/aviary/internal/store"
)

// #1028: a project task run the Code Loop engine interrupts by restarting is
// requeued with a note, and runs again with that note; a task_stop still
// cancels it.
func TestProjectRunInterruptedByEngineRestartIsRequeued(t *testing.T) {
	for _, tc := range []struct {
		name string
		// stop ends the running job: the engine restarting under it, or the
		// owner stopping it.
		stop   func(s *Scheduler, interrupt chan struct{})
		status domain.JobStatus
	}{
		{"engine restart", func(_ *Scheduler, interrupt chan struct{}) { close(interrupt) }, domain.JobStatusPending},
		{"task_stop", func(s *Scheduler, _ chan struct{}) { _, _ = s.StopJobs("") }, domain.JobStatusCanceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupSchedulerDataDir(t)
			cfg := &config.Config{Agents: []config.AgentConfig{{Name: "alpha", Model: "m", Tasks: []config.TaskConfig{{
				Name: "triage", Type: "project", Every: "5m", Project: "o/r", Prompt: "Triage new issues",
			}}}}}
			mgr := agent.NewManager(nil)
			mgr.Reconcile(cfg)
			s, err := New(mgr, 1)
			require.NoError(t, err)
			t.Cleanup(s.Stop)
			now := time.Date(2026, 11, 23, 18, 0, 0, 0, time.UTC)
			s.now = func() time.Time { return now }
			s.Reconcile(cfg)

			started := make(chan struct{}, 2)
			prompts := make(chan string, 2)
			interrupt := make(chan struct{})
			s.pool.projectRunner = func(ctx context.Context, run ProjectRun) (string, error) {
				prompts <- run.Prompt
				started <- struct{}{}
				select {
				case <-interrupt:
					return "", ErrProjectRunInterrupted
				case <-ctx.Done():
					return "", ctx.Err()
				}
			}

			s.mu.Lock()
			fired := s.fireLocked("alpha/triage", s.armed["alpha/triage"].spec, "schedule", now, nil)
			s.mu.Unlock()
			require.NotNil(t, fired)
			job, err := s.queue.Claim()
			require.NoError(t, err)
			require.Equal(t, fired.ID, job.ID)
			done := make(chan struct{})
			go func() {
				defer close(done)
				s.pool.processJob(context.Background(), job)
			}()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("the project task never started")
			}
			assert.Equal(t, "Triage new issues", <-prompts)
			tc.stop(s, interrupt)
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("the project task never finished")
			}

			saved, err := store.ReadJSON[domain.Job](store.FindJobPath(job.ID))
			require.NoError(t, err)
			assert.Equal(t, tc.status, saved.Status)
			if tc.status != domain.JobStatusPending {
				assert.Empty(t, saved.ResumeNote)
				return
			}
			assert.Equal(t, projectRunInterruptedNote, saved.ResumeNote)
			assert.Equal(t, job.Attempts-1, saved.Attempts, "the interrupted attempt does not count")

			// The requeued job runs again, told it was interrupted.
			again, err := s.queue.Claim()
			require.NoError(t, err)
			require.NotNil(t, again)
			require.Equal(t, job.ID, again.ID)
			againDone := make(chan struct{})
			go func() {
				defer close(againDone)
				s.pool.processJob(context.Background(), again)
			}()
			select {
			case prompt := <-prompts:
				assert.Equal(t, projectRunInterruptedNote+"\n\nTriage new issues", prompt)
			case <-time.After(5 * time.Second):
				t.Fatal("the requeued project task never ran again")
			}
			_, _ = s.StopJobs("")
			// Wait for the canceled run to persist its state before the
			// TempDir cleanup removes the scheduler directory (#1076).
			select {
			case <-againDone:
			case <-time.After(5 * time.Second):
				t.Fatal("the requeued project task never finished")
			}
		})
	}
}
