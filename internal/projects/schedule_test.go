package projects

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

// testScheduler is a scheduler on a hand-driven clock that records its
// dispatches instead of starting runs.
type testScheduler struct {
	*scheduler
	clock      time.Time
	dispatched []time.Time
}

func newTestScheduler(t *testing.T, schedules ...core.Schedule) *testScheduler {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".glorp.schedules.json")
	if len(schedules) > 0 {
		if err := saveSchedules(path, schedules); err != nil {
			t.Fatal(err)
		}
	}
	ts := &testScheduler{scheduler: newScheduler(path), clock: time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)}
	ts.now = func() time.Time { return ts.clock }
	ts.dispatch = func(_ core.Schedule, fired time.Time) { ts.dispatched = append(ts.dispatched, fired) }
	if err := ts.load(); err != nil {
		t.Fatal(err)
	}
	return ts
}

func hourly(id string) core.Schedule {
	schedule := core.Schedule{ID: id, Name: id, Repository: "o/r", Prompt: "sweep", Enabled: true, Trigger: core.ScheduleTrigger{Kind: core.TriggerInterval, Every: "1h"}, Created: time.Date(2026, 11, 18, 11, 30, 0, 0, time.UTC)}
	schedule.Normalize()
	return schedule
}

func (ts *testScheduler) history(id string) []core.ScheduleRun {
	return ts.schedules[ts.index(id)].History
}

func TestSchedulerFiresADueScheduleOnTime(t *testing.T) {
	ts := newTestScheduler(t, hourly("a"))
	if next := ts.next["a"]; !next.Equal(time.Date(2026, 11, 18, 12, 30, 0, 0, time.UTC)) {
		t.Fatalf("next = %v", next)
	}
	ts.clock = ts.clock.Add(30*time.Minute + 5*time.Second)
	ts.fireDue()
	if len(ts.dispatched) != 1 {
		t.Fatalf("dispatched %d runs, want 1", len(ts.dispatched))
	}
	if history := ts.history("a"); len(history) != 1 || history[0].Status != core.ScheduleRunRunning {
		t.Fatalf("history = %+v", history)
	}
	if next := ts.next["a"]; !next.Equal(time.Date(2026, 11, 18, 13, 30, 0, 0, time.UTC)) {
		t.Fatalf("next after firing = %v", next)
	}
}

func TestSchedulerOverlapSkipRecordsTheFireAndStartsNothing(t *testing.T) {
	ts := newTestScheduler(t, hourly("a"))
	ts.clock = ts.clock.Add(30 * time.Minute)
	ts.fireDue()
	ts.clock = ts.clock.Add(time.Hour)
	ts.fireDue()
	if len(ts.dispatched) != 1 {
		t.Fatalf("dispatched %d runs while the first was active, want 1", len(ts.dispatched))
	}
	history := ts.history("a")
	if len(history) != 2 || history[1].Status != core.ScheduleRunSkipped {
		t.Fatalf("history = %+v, want the second fire skipped", history)
	}
}

func TestSchedulerOverlapQueueRunsAfterThePreviousRunEnds(t *testing.T) {
	schedule := hourly("a")
	schedule.Overlap = core.OverlapQueue
	ts := newTestScheduler(t, schedule)
	ts.clock = ts.clock.Add(30 * time.Minute)
	ts.fireDue()
	first := ts.dispatched[0]
	ts.clock = ts.clock.Add(time.Hour)
	ts.fireDue()
	if len(ts.dispatched) != 1 {
		t.Fatalf("queued fire started while the previous run was active")
	}
	ts.finish(scheduleFinish{id: "a", fired: first, status: core.ScheduleRunOK, summary: "done", elapsed: time.Minute})
	if len(ts.dispatched) != 2 {
		t.Fatalf("queued fire did not start after the previous run ended")
	}
	if history := ts.history("a"); history[0].Status != core.ScheduleRunOK || history[0].Summary != "done" || history[0].Duration != 60 {
		t.Fatalf("finished run recorded as %+v", history[0])
	}
}

func TestSchedulerMissedRunPolicies(t *testing.T) {
	for _, test := range []struct {
		policy string
		runs   int
	}{{core.MissedRunOnce, 1}, {core.MissedSkip, 0}} {
		schedule := hourly("a")
		schedule.Missed = test.policy
		ts := newTestScheduler(t, schedule)
		// A laptop closed overnight: eight fires pass while asleep.
		ts.clock = ts.clock.Add(8*time.Hour + 10*time.Minute)
		ts.fireDue()
		if len(ts.dispatched) != test.runs {
			t.Errorf("%s: started %d runs after the wake, want %d", test.policy, len(ts.dispatched), test.runs)
		}
		history := ts.history("a")
		if test.policy == core.MissedSkip && (len(history) != 1 || history[0].Status != core.ScheduleRunSkipped || !strings.Contains(history[0].Summary, "missed 8")) {
			t.Errorf("skip: history = %+v", history)
		}
		if !ts.next["a"].After(ts.clock) {
			t.Errorf("%s: next fire %v is not after the wake", test.policy, ts.next["a"])
		}
	}
}

func TestSchedulerAppliesTheMissedPolicyToFiresMissedWhileStopped(t *testing.T) {
	schedule := hourly("a")
	schedule.Missed = core.MissedRunOnce
	schedule.Created = time.Date(2026, 11, 17, 11, 30, 0, 0, time.UTC)
	schedule.LastFire = time.Date(2026, 11, 18, 2, 30, 0, 0, time.UTC)
	ts := newTestScheduler(t, schedule)
	ts.fireDue()
	if len(ts.dispatched) != 1 {
		t.Fatalf("started %d runs for fires missed while glorp was stopped, want 1", len(ts.dispatched))
	}
}

func TestSchedulerNeverFiresADisabledSchedule(t *testing.T) {
	schedule := hourly("a")
	schedule.Enabled = false
	ts := newTestScheduler(t, schedule, hourly("b"))
	ts.clock = ts.clock.Add(30 * time.Minute)
	ts.fireDue()
	for _, run := range ts.upcoming() {
		if run.ScheduleID == "a" {
			t.Fatalf("disabled schedule listed in upcoming: %+v", run)
		}
	}
	if len(ts.history("a")) != 0 {
		t.Fatalf("disabled schedule recorded runs: %+v", ts.history("a"))
	}
	if len(ts.dispatched) != 1 {
		t.Fatalf("dispatched %d runs, want only the enabled schedule's", len(ts.dispatched))
	}
}

func TestSchedulerUpcomingListsTheNextRunsSoonestFirst(t *testing.T) {
	daily := core.Schedule{ID: "d", Name: "d", Repository: "o/r", Prompt: "p", Enabled: true, Trigger: core.ScheduleTrigger{Kind: core.TriggerDaily, Time: "13:00", Timezone: "UTC"}}
	daily.Normalize()
	ts := newTestScheduler(t, hourly("a"), daily)
	runs := ts.upcoming()
	if len(runs) != core.MaxUpcomingRuns {
		t.Fatalf("upcoming has %d runs, want %d", len(runs), core.MaxUpcomingRuns)
	}
	for i := 1; i < len(runs); i++ {
		if runs[i].At.Before(runs[i-1].At) {
			t.Fatalf("upcoming out of order: %+v", runs)
		}
	}
	if runs[0].ScheduleID != "a" || runs[1].ScheduleID != "d" {
		t.Fatalf("upcoming = %+v", runs)
	}
	empty := newTestScheduler(t)
	if runs := empty.upcoming(); len(runs) != 0 {
		t.Fatalf("upcoming without schedules = %+v", runs)
	}
}

func TestSchedulerRequestsPersistAcrossARestart(t *testing.T) {
	ts := newTestScheduler(t)
	created, err := ts.handle(core.ScheduleRequest{Op: core.ScheduleOpCreate, Schedule: core.Schedule{Name: "Weekly flaky triage", Repository: "o/r", Prompt: "triage", Enabled: true, Trigger: core.ScheduleTrigger{Kind: core.TriggerCron, Cron: "0 9 * * mon"}}})
	if err != nil {
		t.Fatal(err)
	}
	id := created.Schedule.ID
	if !strings.HasPrefix(id, "weekly-flaky-triage-") || created.Schedule.Next.IsZero() {
		t.Fatalf("created %+v", created.Schedule)
	}
	enabled := false
	if _, err := ts.handle(core.ScheduleRequest{Op: core.ScheduleOpUpdate, ID: id, Update: core.ScheduleUpdate{Enabled: &enabled}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ts.handle(core.ScheduleRequest{Op: core.ScheduleOpRun, ID: id}); err != nil {
		t.Fatal(err)
	}
	if len(ts.dispatched) != 1 {
		t.Fatal("run now did not start a run of a disabled schedule")
	}
	restarted := newScheduler(ts.path)
	restarted.now = ts.now
	if err := restarted.load(); err != nil {
		t.Fatal(err)
	}
	list, _ := restarted.handle(core.ScheduleRequest{Op: core.ScheduleOpList})
	if len(list.Schedules) != 1 || list.Schedules[0].ID != id || list.Schedules[0].Enabled {
		t.Fatalf("after restart: %+v", list.Schedules)
	}
	history := list.Schedules[0].History
	if len(history) != 1 || history[0].Status != core.ScheduleRunFailed || !strings.Contains(history[0].Summary, "interrupted") {
		t.Fatalf("a run cut off by the restart reads %+v", history)
	}
	if _, err := restarted.handle(core.ScheduleRequest{Op: core.ScheduleOpDelete, ID: id}); err != nil {
		t.Fatal(err)
	}
	if schedules, _ := loadSchedules(ts.path); len(schedules) != 0 {
		t.Fatalf("deleted schedule still stored: %+v", schedules)
	}
}

func TestSchedulerRejectsAnInvalidSchedule(t *testing.T) {
	ts := newTestScheduler(t)
	if _, err := ts.handle(core.ScheduleRequest{Op: core.ScheduleOpCreate, Schedule: core.Schedule{Name: "x", Repository: "o/r", Prompt: "p", Trigger: core.ScheduleTrigger{Kind: core.TriggerInterval, Every: "1s"}}}); err == nil {
		t.Fatal("accepted a one-second interval")
	}
	if _, err := ts.handle(core.ScheduleRequest{Op: core.ScheduleOpRun, ID: "missing"}); err == nil || !strings.HasSuffix(err.Error(), "not found") {
		t.Fatalf("run of a missing schedule: %v", err)
	}
}

func TestSchedulerPreviewListsTheNextThreeFires(t *testing.T) {
	ts := newTestScheduler(t)
	response, err := ts.handle(core.ScheduleRequest{Op: core.ScheduleOpPreview, Trigger: core.ScheduleTrigger{Kind: core.TriggerDaily, Time: "09:00", Timezone: "UTC"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Fires) != 3 || !response.Fires[0].Equal(time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("fires = %v", response.Fires)
	}
}

func TestSchedulerReloadPicksUpHandEditsAndRunRequests(t *testing.T) {
	ts := newTestScheduler(t, hourly("a"))
	schedules, _ := loadSchedules(ts.path)
	schedules[0].RunNow = true
	schedules = append(schedules, hourly("b"))
	if err := saveSchedules(ts.path, schedules); err != nil {
		t.Fatal(err)
	}
	ts.reload()
	if len(ts.schedules) != 2 || ts.next["b"].IsZero() {
		t.Fatalf("reload did not add the new schedule: %+v", ts.schedules)
	}
	if len(ts.dispatched) != 1 {
		t.Fatalf("a run requested in the store started %d runs, want 1", len(ts.dispatched))
	}
	stored, _ := loadSchedules(ts.path)
	if stored[0].RunNow {
		t.Fatal("the run request was not cleared from the store")
	}
}

func TestSchedulerGivesEveryRunOfAScheduleItsOwnSecond(t *testing.T) {
	ts := newTestScheduler(t, hourly("a"))
	for range 3 {
		if _, err := ts.handle(core.ScheduleRequest{Op: core.ScheduleOpRun, ID: "a"}); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	for _, fired := range ts.dispatched {
		key := scheduleJobKey("a", fired)
		if seen[key] {
			t.Fatalf("two runs share job key %s", key)
		}
		seen[key] = true
	}
}

func TestCommandArgsGiveAScheduledRunItsPrompt(t *testing.T) {
	runner := CommandRunner{Agent: "claude", Identity: "ABCD1234"}
	args := commandArgsForSession(runner, Issue{Title: "Nightly", Repository: "o/r", Target: "o/r", Prompt: "Find flaky tests and file issues.", ScheduleID: "nightly"}, AgentSession{RunDir: t.TempDir()})
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "/gh-fix") {
		t.Fatalf("scheduled run was given gh-fix: %q", joined)
	}
	for _, want := range []string{"Find flaky tests and file issues.", "Repository: o/r", "/glorp:ABCD1234", "clone directory"} {
		if !strings.Contains(joined, want) {
			t.Errorf("prompt is missing %q: %q", want, joined)
		}
	}
}

// promptRecordingRunner records each run's issue and blocks until released or
// cancelled.
type promptRecordingRunner struct {
	mu      sync.Mutex
	issues  []Issue
	started chan Issue
	release chan struct{}
}

func (r *promptRecordingRunner) AgentName() string { return "codex" }
func (r *promptRecordingRunner) Run(ctx context.Context, issue Issue) error {
	return r.RunWithOutput(ctx, issue, io.Discard)
}
func (r *promptRecordingRunner) RunWithOutput(ctx context.Context, issue Issue, output io.Writer) error {
	r.mu.Lock()
	r.issues = append(r.issues, issue)
	r.mu.Unlock()
	_, _ = io.WriteString(output, "swept 3 dependencies\n")
	r.started <- issue
	select {
	case <-r.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestGlorpRunsAScheduleAsAJobAndStopsIt(t *testing.T) {
	dir := t.TempDir()
	runner := &promptRecordingRunner{started: make(chan Issue, 4), release: make(chan struct{})}
	reporter := &snapshotReporter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &Glorp{Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: filepath.Join(dir, ".glorp.json"), Issues: &fakeSource{batches: [][]Issue{{}}}, Runner: runner, UI: reporter, Out: io.Discard}
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	created, err := w.HandleSchedule(ctx, core.ScheduleRequest{Op: core.ScheduleOpCreate, Schedule: core.Schedule{ID: "nightly", Name: "Nightly sweep", Repository: "o/r", Prompt: "Sweep dependencies.", Enabled: true, Trigger: core.ScheduleTrigger{Kind: core.TriggerDaily, Time: "03:00"}}})
	if err != nil {
		t.Fatal(err)
	}
	if created.Schedule == nil || created.Schedule.Next.IsZero() {
		t.Fatalf("created %+v", created)
	}
	if _, err := os.Stat(filepath.Join(dir, ".glorp.schedules.json")); err != nil {
		t.Fatalf("schedule store not written: %v", err)
	}
	if _, err := w.HandleSchedule(ctx, core.ScheduleRequest{Op: core.ScheduleOpRun, ID: "nightly"}); err != nil {
		t.Fatal(err)
	}
	var issue Issue
	select {
	case issue = <-runner.started:
	case <-time.After(5 * time.Second):
		t.Fatal("run now started no run")
	}
	if issue.Prompt != "Sweep dependencies." || issue.ScheduleID != "nightly" {
		t.Fatalf("run got %+v", issue)
	}
	job := waitForJob(t, reporter, func(job JobSnapshot) bool { return job.ScheduleID == "nightly" && job.Status == "active" })
	if job.Kind != core.JobKindSchedule || job.Target != "schedule:nightly" || !strings.Contains(job.Log, "swept 3") {
		t.Fatalf("scheduled job card = %+v", job)
	}
	if err := w.handleJobAction(ctx, core.JobAction{Action: "stop", Target: job.Target, Number: job.Number}); err != nil {
		t.Fatalf("stop: %v", err)
	}
	waitForJob(t, reporter, func(job JobSnapshot) bool { return job.ScheduleID == "nightly" && job.Status == "failed" })
	list, err := w.HandleSchedule(ctx, core.ScheduleRequest{Op: core.ScheduleOpList})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		history := list.Schedules[0].History
		if len(history) == 1 && history[0].Status == core.ScheduleRunFailed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("history after stop = %+v", history)
		}
		time.Sleep(10 * time.Millisecond)
		list, _ = w.HandleSchedule(ctx, core.ScheduleRequest{Op: core.ScheduleOpList})
	}
	snapshot := latestSnapshot(reporter)
	if len(snapshot.Upcoming) == 0 || snapshot.Upcoming[0].ScheduleID != "nightly" {
		t.Fatalf("snapshot upcoming = %+v", snapshot.Upcoming)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func latestSnapshot(reporter *snapshotReporter) EngineSnapshot {
	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	if len(reporter.snapshots) == 0 {
		return EngineSnapshot{}
	}
	return reporter.snapshots[len(reporter.snapshots)-1]
}

func waitForJob(t *testing.T, reporter *snapshotReporter, match func(JobSnapshot) bool) JobSnapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, job := range latestSnapshot(reporter).Jobs {
			if match(job) {
				return job
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no matching job; last jobs: %+v", latestSnapshot(reporter).Jobs)
	return JobSnapshot{}
}
