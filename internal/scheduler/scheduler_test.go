package scheduler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

func setupSchedulerDataDir(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := store.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}
}

func TestNewIDAndTaskKey(t *testing.T) {
	id := newID("job")
	if !strings.HasPrefix(id, "job_") {
		t.Fatalf("expected prefix in id, got %q", id)
	}
	if strings.Contains(id, ".") {
		t.Fatalf("id should not contain dots, got %q", id)
	}

	if got := taskKey("agentA", "task1"); got != "agentA/task1" {
		t.Fatalf("unexpected task key: %q", got)
	}
}

func TestJobQueue_EnqueueClaimCompleteAndList(t *testing.T) {
	setupSchedulerDataDir(t)
	q := NewJobQueue()

	job, err := q.Enqueue("taskA", "agent_a", "agentA", "hello", 0)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if job.Status != domain.JobStatusPending {
		t.Fatalf("expected pending, got %s", job.Status)
	}
	if job.MaxRetries != defaultRetries {
		t.Fatalf("expected default retries %d, got %d", defaultRetries, job.MaxRetries)
	}

	claimed, err := q.Claim()
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claimed == nil {
		t.Fatal("expected claimed job, got nil")
	}
	if claimed.Status != domain.JobStatusInProgress || claimed.Attempts != 1 || claimed.LockedAt == nil {
		t.Fatalf("unexpected claimed state: %+v", claimed)
	}

	if err := q.Complete(claimed.ID); err != nil {
		t.Fatalf("complete: %v", err)
	}

	jobs, err := q.List("taskA")
	if err != nil {
		t.Fatalf("list by task: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Status != domain.JobStatusCompleted {
		t.Fatalf("unexpected list result: %+v", jobs)
	}

	all, err := q.List("")
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected one job in list all, got %d", len(all))
	}
}

func TestJobQueue_FailWithRetryThenFailTerminal(t *testing.T) {
	setupSchedulerDataDir(t)
	q := NewJobQueue()

	job, err := q.Enqueue("taskB", "agent_b", "agentB", "go", 2)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	first, err := q.Claim()
	if err != nil || first == nil {
		t.Fatalf("claim first: %v, job=%v", err, first)
	}
	if err := q.Fail(first.ID, errors.New("boom1")); err != nil {
		t.Fatalf("fail first: %v", err)
	}
	afterFirst, err := store.ReadJSON[domain.Job](store.JobPath(job.AgentID, job.ID))
	if err != nil {
		t.Fatalf("read after first fail: %v", err)
	}
	if afterFirst.Status != domain.JobStatusPending {
		t.Fatalf("expected pending after retryable fail, got %s", afterFirst.Status)
	}
	if afterFirst.NextRetryAt == nil {
		t.Fatal("expected next retry time to be set")
	}

	next := time.Now().Add(-time.Second)
	afterFirst.NextRetryAt = &next
	if err := store.WriteJSON(store.JobPath(job.AgentID, job.ID), &afterFirst); err != nil {
		t.Fatalf("forcing retry due time: %v", err)
	}

	second, err := q.Claim()
	if err != nil || second == nil {
		t.Fatalf("claim second: %v, job=%v", err, second)
	}
	if err := q.Fail(second.ID, errors.New("boom2")); err != nil {
		t.Fatalf("fail second: %v", err)
	}

	afterSecond, err := store.ReadJSON[domain.Job](store.JobPath(job.AgentID, job.ID))
	if err != nil {
		t.Fatalf("read after second fail: %v", err)
	}
	if afterSecond.Status != domain.JobStatusFailed {
		t.Fatalf("expected terminal failed status, got %s", afterSecond.Status)
	}
}

func TestJobQueue_RecoverStuck(t *testing.T) {
	setupSchedulerDataDir(t)
	q := NewJobQueue()

	job := domain.Job{
		ID:         newID("job"),
		TaskID:     "taskC",
		AgentID:    "agent_c",
		AgentName:  "agentC",
		Prompt:     "run",
		Status:     domain.JobStatusInProgress,
		Attempts:   1,
		MaxRetries: 3,
		CreatedAt:  time.Now().Add(-10 * time.Minute),
		UpdatedAt:  time.Now().Add(-10 * time.Minute),
	}
	lockedAt := time.Now().Add(-lockTimeout - time.Second)
	job.LockedAt = &lockedAt
	if err := store.WriteJSON(store.JobPath(job.AgentID, job.ID), &job); err != nil {
		t.Fatalf("write stuck job: %v", err)
	}

	q.RecoverStuck()

	recovered, err := store.ReadJSON[domain.Job](store.JobPath(job.AgentID, job.ID))
	if err != nil {
		t.Fatalf("read recovered job: %v", err)
	}
	if recovered.Status != domain.JobStatusPending || recovered.LockedAt != nil {
		t.Fatalf("expected recovered pending unlocked job, got %+v", recovered)
	}
}

func TestCronRunner_AddRemove(t *testing.T) {
	r := NewCronRunner()
	called := make(chan struct{}, 1)

	if err := r.Add("task", "*/1 * * * * *", func() {
		select {
		case called <- struct{}{}:
		default:
		}
	}); err != nil {
		t.Fatalf("add cron: %v", err)
	}
	if _, ok := r.ids["task"]; !ok {
		t.Fatal("expected cron id to be tracked")
	}

	r.Start()
	defer r.Stop()

	select {
	case <-called:
	case <-time.After(2500 * time.Millisecond):
		t.Fatal("expected cron callback to run")
	}

	r.Remove("task")
	if _, ok := r.ids["task"]; ok {
		t.Fatal("expected cron id removed")
	}
}

func TestFileWatcher_AddStartAndRemove(t *testing.T) {
	fw, err := NewFileWatcher()
	if err != nil {
		t.Fatalf("new file watcher: %v", err)
	}

	tmp := t.TempDir()
	glob := filepath.Join(tmp, "*.txt")
	triggered := make(chan string, 1)

	if err := fw.Add("watch1", glob, func(path string) {
		select {
		case triggered <- path:
		default:
		}
	}); err != nil {
		t.Fatalf("add watcher: %v", err)
	}
	if _, ok := fw.handlers["watch1"]; !ok {
		t.Fatal("expected handler to be registered")
	}

	go fw.Start()
	t.Cleanup(fw.Stop)

	path := filepath.Join(tmp, "event.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write watched file: %v", err)
	}

	select {
	case got := <-triggered:
		if got != path {
			t.Fatalf("unexpected path in callback: got %q want %q", got, path)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("expected file watcher callback")
	}

	fw.Remove("watch1")
	if _, ok := fw.handlers["watch1"]; ok {
		t.Fatal("expected handler removed")
	}
}

func TestScheduler_NewStartStopAndReconcile(t *testing.T) {
	setupSchedulerDataDir(t)
	mgr := agent.NewManager(nil)

	s, err := New(mgr, 1)
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}
	if s.Queue() == nil {
		t.Fatal("expected queue to be initialized")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	t.Cleanup(s.Stop)

	watchDir := t.TempDir()
	cfg := &config.Config{
		Agents: []config.AgentConfig{
			{
				Name: "alpha",
				Tasks: []config.TaskConfig{
					{Name: "cronTask", Schedule: "*/1 * * * * *", Prompt: "tick"},
					{Name: "watchTask", Watch: filepath.Join(watchDir, "*.md"), Prompt: "watch"},
				},
			},
		},
	}
	s.Reconcile(cfg)

	if len(s.tasks) != 2 {
		t.Fatalf("expected 2 registered tasks, got %d", len(s.tasks))
	}
	if _, ok := s.cron.ids["alpha/cronTask"]; !ok {
		t.Fatal("expected cron task to be registered")
	}
	if _, ok := s.watch.handlers["alpha/watchTask"]; !ok {
		t.Fatal("expected watch task to be registered")
	}

	s.Reconcile(&config.Config{})
	if len(s.tasks) != 0 || len(s.cron.ids) != 0 || len(s.watch.handlers) != 0 {
		t.Fatalf("expected all tasks removed, got tasks=%d cron=%d watch=%d", len(s.tasks), len(s.cron.ids), len(s.watch.handlers))
	}
}

func TestScheduler_RunOnceStartAt_EnqueuesSingleJob(t *testing.T) {
	setupSchedulerDataDir(t)
	mgr := agent.NewManager(nil)

	s, err := New(mgr, 1)
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}
	t.Cleanup(s.Stop)

	startAt := time.Now().UTC().Add(250 * time.Millisecond).Format(time.RFC3339)
	cfg := &config.Config{
		Agents: []config.AgentConfig{{
			Name: "alpha",
			Tasks: []config.TaskConfig{{
				Name:    "once",
				RunOnce: true,
				StartAt: startAt,
				Prompt:  "ping",
			}},
		}},
	}
	s.Reconcile(cfg)

	time.Sleep(1200 * time.Millisecond)
	jobs, err := s.Queue().List("alpha/once")
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected exactly one enqueued job, got %d", len(jobs))
	}

	time.Sleep(1200 * time.Millisecond)
	jobs, err = s.Queue().List("alpha/once")
	if err != nil {
		t.Fatalf("list jobs after wait: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected one-time task to remain single-run, got %d jobs", len(jobs))
	}
}

func TestScheduler_DelayedCronStart(t *testing.T) {
	setupSchedulerDataDir(t)
	mgr := agent.NewManager(nil)

	s, err := New(mgr, 1)
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}
	s.cron.Start()
	t.Cleanup(s.Stop)

	startAt := time.Now().UTC().Add(2 * time.Second).Format(time.RFC3339)
	cfg := &config.Config{
		Agents: []config.AgentConfig{{
			Name: "alpha",
			Tasks: []config.TaskConfig{{
				Name:     "delayed",
				Schedule: "*/1 * * * * *",
				StartAt:  startAt,
				Prompt:   "tick",
			}},
		}},
	}
	s.Reconcile(cfg)

	time.Sleep(1200 * time.Millisecond)
	before, err := s.Queue().List("alpha/delayed")
	if err != nil {
		t.Fatalf("list jobs before start_at: %v", err)
	}
	if len(before) != 0 {
		t.Fatalf("expected no jobs before delayed start, got %d", len(before))
	}

	time.Sleep(2200 * time.Millisecond)
	after, err := s.Queue().List("alpha/delayed")
	if err != nil {
		t.Fatalf("list jobs after start_at: %v", err)
	}
	if len(after) == 0 {
		t.Fatal("expected jobs after delayed cron start")
	}
}

func TestScheduler_RunOnceCron_OnlyEnqueuesOneJob(t *testing.T) {
	setupSchedulerDataDir(t)
	mgr := agent.NewManager(nil)

	s, err := New(mgr, 1)
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}
	s.cron.Start()
	t.Cleanup(s.Stop)

	cfg := &config.Config{
		Agents: []config.AgentConfig{{
			Name: "alpha",
			Tasks: []config.TaskConfig{{
				Name:     "oncecron",
				Schedule: "*/1 * * * * *",
				RunOnce:  true,
				Prompt:   "tick",
			}},
		}},
	}
	s.Reconcile(cfg)

	time.Sleep(3200 * time.Millisecond)
	jobs, err := s.Queue().List("alpha/oncecron")
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected run_once cron task to enqueue once, got %d", len(jobs))
	}
}

func TestWorkerPool_ExecuteJob(t *testing.T) {
	setupSchedulerDataDir(t)
	mgr := agent.NewManager(nil)
	mgr.Reconcile(&config.Config{Agents: []config.AgentConfig{{Name: "alpha", Model: "m", Tasks: []config.TaskConfig{{Name: "t", Schedule: "*/1 * * * * *", Prompt: "p"}}}}})

	p := NewWorkerPool(NewJobQueue(), mgr, 1)

	err := p.executeJob(context.Background(), &domain.Job{AgentName: "missing", Prompt: "hello"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found error, got %v", err)
	}

	err = p.executeJob(context.Background(), &domain.Job{AgentName: "alpha", Prompt: "hello"})
	if err != nil {
		t.Fatalf("expected nil error for stubbed no-provider path, got %v", err)
	}
}
