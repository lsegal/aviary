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
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir("") })
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

	job, err := q.Enqueue("taskA", "agent_a", "agentA", "hello", "", 0, "", "")
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

func TestJobQueue_Cancel(t *testing.T) {
	setupSchedulerDataDir(t)
	q := NewJobQueue()

	job, err := q.Enqueue("taskCancel", "agent_a", "agentA", "hello", "", 0, "", "")
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := q.Cancel(job.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	persisted, err := store.ReadJSON[domain.Job](store.JobPath(job.AgentID, job.ID))
	if err != nil {
		t.Fatalf("read canceled job: %v", err)
	}
	if persisted.Status != domain.JobStatusCanceled {
		t.Fatalf("expected canceled status, got %s", persisted.Status)
	}
}

func TestJobQueue_FailWithRetryThenFailTerminal(t *testing.T) {
	setupSchedulerDataDir(t)
	q := NewJobQueue()

	job, err := q.Enqueue("taskB", "agent_b", "agentB", "go", "", 2, "", "")
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

func TestScheduler_ListTasksReturnsConfiguredDefinitions(t *testing.T) {
	setupSchedulerDataDir(t)
	mgr := agent.NewManager(nil)

	s, err := New(mgr, 1)
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}
	t.Cleanup(s.Stop)

	startAt := time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339)
	cfg := &config.Config{
		Agents: []config.AgentConfig{{
			Name: "alpha",
			Tasks: []config.TaskConfig{
				{
					Name:     "daily",
					Schedule: "0 0 10 * * *",
					StartAt:  startAt,
					RunOnce:  true,
					Prompt:   "send report",
					Channel:  "last",
				},
				{
					Name:   "watch-docs",
					Watch:  "./docs/**/*.md",
					Prompt: "summarize changes",
				},
			},
		}},
	}
	s.Reconcile(cfg)

	tasks := s.ListTasks()
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}

	if tasks[0].ID != "alpha/daily" || tasks[0].AgentName != "alpha" || tasks[0].Name != "daily" {
		t.Fatalf("unexpected first task identity: %#v", tasks[0])
	}
	if tasks[0].TriggerType != domain.TriggerTypeCron || tasks[0].Schedule != "0 0 10 * * *" {
		t.Fatalf("unexpected cron task trigger data: %#v", tasks[0])
	}
	if tasks[0].StartAt == nil || tasks[0].Channel != "last" || !tasks[0].RunOnce {
		t.Fatalf("expected cron task metadata, got %#v", tasks[0])
	}

	if tasks[1].ID != "alpha/watch-docs" || tasks[1].TriggerType != domain.TriggerTypeWatch || tasks[1].Watch != "./docs/**/*.md" {
		t.Fatalf("unexpected watch task: %#v", tasks[1])
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

func TestWorkerPool_ExecuteJob_RepliesToSessionDelivery(t *testing.T) {
	setupSchedulerDataDir(t)

	mgr := agent.NewManager(nil)
	mgr.Reconcile(&config.Config{Agents: []config.AgentConfig{{Name: "alpha", Model: "m"}}})
	p := NewWorkerPool(NewJobQueue(), mgr, 1)

	const (
		replyAgentID   = "agent_alpha"
		replySessionID = "agent_alpha-signal:+15551234567"
	)

	var delivered string
	agent.RegisterSessionDelivery(replySessionID, "signal", "+15551234567", func(text string) {
		delivered = text
	})

	job := &domain.Job{
		ID:             "job_reply",
		TaskID:         "oneshot/alpha",
		AgentID:        "agent_alpha",
		AgentName:      "alpha",
		Prompt:         "hello",
		ReplyAgentID:   replyAgentID,
		ReplySessionID: replySessionID,
	}
	if err := p.executeJob(context.Background(), job); err != nil {
		t.Fatalf("executeJob: %v", err)
	}
	if delivered == "" {
		t.Fatal("expected scheduled reply to be delivered to session callbacks")
	}

	data, err := os.ReadFile(store.SessionPath(replyAgentID, replySessionID))
	if err != nil {
		t.Fatalf("read reply session: %v", err)
	}
	if !strings.Contains(string(data), "\"role\":\"assistant\"") || !strings.Contains(string(data), "no LLM provider configured") {
		t.Fatalf("expected assistant reply in session file, got: %s", string(data))
	}
}

func TestEnqueueAt(t *testing.T) {
	setupSchedulerDataDir(t)
	q := NewJobQueue()

	at := time.Now().Add(1 * time.Hour)
	job, err := q.EnqueueAt("task1", "agent_alpha", "alpha", "do something", "", 0, at, "", "")
	if err != nil {
		t.Fatalf("EnqueueAt: %v", err)
	}
	if job.ScheduledFor == nil || !job.ScheduledFor.Equal(at) {
		t.Errorf("ScheduledFor = %v; want %v", job.ScheduledFor, at)
	}
}

func TestUpdateOutput(t *testing.T) {
	setupSchedulerDataDir(t)
	q := NewJobQueue()

	job, err := q.Enqueue("task1", "agent_alpha", "alpha", "prompt", "", 0, "", "")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if err := q.UpdateOutput(job.ID, "some output"); err != nil {
		t.Fatalf("UpdateOutput: %v", err)
	}

	// Verify output persisted.
	jobs, err := q.List("")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var found *domain.Job
	for i := range jobs {
		if jobs[i].ID == job.ID {
			found = &jobs[i]
		}
	}
	if found == nil {
		t.Fatal("job not found after UpdateOutput")
	}
	if found.Output != "some output" {
		t.Errorf("Output = %q; want %q", found.Output, "some output")
	}
}

func TestUpdateOutput_NotFound(t *testing.T) {
	setupSchedulerDataDir(t)
	q := NewJobQueue()
	err := q.UpdateOutput("nonexistent_job_id", "output")
	if err == nil {
		t.Error("expected error for nonexistent job")
	}
}

func TestTrigger(t *testing.T) {
	setupSchedulerDataDir(t)

	mgr := agent.NewManager(nil)
	s, err := New(mgr, 1)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Reconcile a config with a task to register it.
	cfg := &config.Config{
		Agents: []config.AgentConfig{
			{
				Name:  "alpha",
				Model: "test/model",
				Tasks: []config.TaskConfig{
					{Name: "daily", Prompt: "run daily", Schedule: "0 9 * * *"},
				},
			},
		},
	}
	mgr.Reconcile(cfg)
	s.Reconcile(cfg)

	job, err := s.Trigger("alpha/daily")
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if job == nil || job.TaskID != "alpha/daily" {
		t.Errorf("job = %+v; want TaskID=alpha/daily", job)
	}
	if job.Status != domain.JobStatusInProgress {
		t.Fatalf("expected immediate job to start in_progress, got %s", job.Status)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		persisted, readErr := store.ReadJSON[domain.Job](store.JobPath(job.AgentID, job.ID))
		if readErr != nil {
			t.Fatalf("read job: %v", readErr)
		}
		if persisted.Status == domain.JobStatusCompleted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected immediate job to complete, got status %s", persisted.Status)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestRunJobNow_ForceStartsPendingJob(t *testing.T) {
	setupSchedulerDataDir(t)

	mgr := agent.NewManager(nil)
	mgr.Reconcile(&config.Config{Agents: []config.AgentConfig{{Name: "alpha", Model: "m"}}})
	s, err := New(mgr, 1)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(s.Stop)

	at := time.Now().Add(1 * time.Hour)
	job, err := s.Queue().EnqueueAt("alpha/daily", "agent_alpha", "alpha", "hello", "", 1, at, "", "")
	if err != nil {
		t.Fatalf("EnqueueAt: %v", err)
	}

	started, err := s.RunJobNow(job.ID)
	if err != nil {
		t.Fatalf("RunJobNow: %v", err)
	}
	if started.Status != domain.JobStatusInProgress {
		t.Fatalf("expected in_progress, got %s", started.Status)
	}
	if started.ScheduledFor != nil {
		t.Fatalf("expected scheduled_for cleared, got %v", started.ScheduledFor)
	}
}

func TestTrigger_NotFound(t *testing.T) {
	setupSchedulerDataDir(t)

	mgr := agent.NewManager(nil)
	s, err := New(mgr, 1)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = s.Trigger("nonexistent/task")
	if err == nil {
		t.Error("expected error for nonexistent task")
	}
}

func TestScheduler_StopJobs_CancelsPending(t *testing.T) {
	setupSchedulerDataDir(t)

	mgr := agent.NewManager(nil)
	s, err := New(mgr, 1)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	job, err := s.Queue().Enqueue("alpha/daily", "agent_alpha", "alpha", "prompt", "", 1, "", "")
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	stopped, err := s.StopJobs("alpha/daily")
	if err != nil {
		t.Fatalf("StopJobs: %v", err)
	}
	if stopped != 1 {
		t.Fatalf("expected 1 stopped job, got %d", stopped)
	}

	persisted, err := store.ReadJSON[domain.Job](store.JobPath(job.AgentID, job.ID))
	if err != nil {
		t.Fatalf("read job: %v", err)
	}
	if persisted.Status != domain.JobStatusCanceled {
		t.Fatalf("expected canceled job, got %s", persisted.Status)
	}
}

func TestJobQueue_ListCorrupted(t *testing.T) {
	setupSchedulerDataDir(t)
	q := NewJobQueue()

	// Enqueue a valid job first.
	job, err := q.Enqueue("task1", "agent_alpha", "alpha", "prompt", "", 0, "", "")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Write a corrupted file alongside it.
	jobsDir := filepath.Join(store.AgentDir("agent_alpha"), "jobs")
	if err := os.WriteFile(filepath.Join(jobsDir, "job_bad.json"), []byte("not-json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// List should skip corrupted file and return the valid job.
	jobs, err := q.List("")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, j := range jobs {
		if j.ID == job.ID {
			found = true
		}
	}
	if !found {
		t.Error("expected valid job in list")
	}
}
