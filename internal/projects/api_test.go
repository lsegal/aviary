package projects

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/projects/core"
	"github.com/lsegal/aviary/internal/transcript"
)

// apiTestManager is a test manager whose coder agent owns o/r, with a
// stopped engine attached so the action APIs reach it, and the jobs given
// as its live snapshot.
func apiTestManager(t *testing.T, jobs ...core.JobSnapshot) (*Manager, *Engine) {
	t.Helper()
	m := newTestManager(t)
	m.Reconcile(ownedBy(config.ProjectsConfig{}, "o/r"))
	engine := &Engine{Targets: []string{"o/r"}, StatePath: filepath.Join(t.TempDir(), "state.json"), Out: &syncBuffer{}}
	m.running = &engineRun{engine: engine}
	m.snapshot.Store(&core.Snapshot{Targets: []string{"o/r"}, Jobs: jobs, Running: 1, Concurrency: 3})
	return m, engine
}

func TestAPIWithoutEngineReportsNotWatching(t *testing.T) {
	m := newTestManager(t)
	ctx := t.Context()
	calls := map[string]func() error{
		"job action": func() error { _, err := m.JobAction(ctx, "stop", "o/r", 1); return err },
		"refresh":    func() error { _, err := m.Refresh(""); return err },
		"clone list": func() error { _, err := m.CloneList(ctx); return err },
		"clone clear": func() error {
			_, err := m.CloneClear(ctx)
			return err
		},
		"unblock":       func() error { _, err := m.UnblockRun("", 0); return err },
		"issue comment": func() error { _, err := m.IssueComment(ctx, "o/r", 1, "hi", nil); return err },
		"issue create": func() error {
			_, err := m.IssueCreate(ctx, core.IssueCreate{Repository: "o/r", Text: "hi"})
			return err
		},
	}
	for name, call := range calls {
		if err := call(); !errors.Is(err, ErrNotWatching) {
			t.Errorf("%s without an engine = %v, want ErrNotWatching", name, err)
		}
	}
}

func TestInFlightJobsListsOnlyTargetJobsHoldingWork(t *testing.T) {
	m, _ := apiTestManager(t,
		core.JobSnapshot{Target: "o/r", Number: 1, Status: "active"},
		core.JobSnapshot{Target: "o/r", Number: 2, Status: "waiting"},
		core.JobSnapshot{Target: "o/r", Number: 3, Status: "queued"},
		core.JobSnapshot{Target: "o/r", Number: 4, Status: "paused"},
		core.JobSnapshot{Target: "o/r", Number: 5, Status: "complete"},
		core.JobSnapshot{Target: "o/r", Number: 6, Status: "failed"},
		core.JobSnapshot{Target: "x/y", Number: 7, Status: "active"},
	)
	if got := m.InFlightJobs("o/r"); !slices.Equal(got, []int{1, 2, 3, 4}) {
		t.Errorf("in-flight jobs of o/r = %v, want [1 2 3 4]", got)
	}
	if got := m.InFlightJobs("a/b"); got == nil || len(got) != 0 {
		t.Errorf("in-flight jobs of an unwatched target = %#v, want an empty list", got)
	}
}

func TestStatusReportsEngineMergeHoldAndWebhookHealth(t *testing.T) {
	m, engine := apiTestManager(t)
	m.snapshot.Store(&core.Snapshot{Targets: []string{"o/r"}, UseWebhooks: true, WebhookOnline: true})
	engine.noMerge.Store(true)
	status := m.Status(t.Context(), 0, nil)
	// The running engine's live hold wins over the configured setting.
	if status["no_merge"] != true {
		t.Errorf("no_merge = %v, want the engine's true", status["no_merge"])
	}
	if status["webhook_online"] != true {
		t.Errorf("webhook_online = %v, want true", status["webhook_online"])
	}
	engine.noMerge.Store(false)
	m.snapshot.Store(&core.Snapshot{Targets: []string{"o/r"}})
	status = m.Status(t.Context(), 0, nil)
	if status["no_merge"] != false {
		t.Errorf("no_merge = %v after the engine lifted it", status["no_merge"])
	}
	if _, ok := status["webhook_online"]; ok {
		t.Error("webhook_online reported while polling")
	}
}

func TestGetJobReportsDetailScrollbackAndTranscript(t *testing.T) {
	checkout := t.TempDir()
	started := time.Now().Add(-time.Hour)
	m, _ := apiTestManager(t,
		core.JobSnapshot{Target: "o/r", Number: 7, Title: "Fix it", Status: "active", Kind: core.JobKindIssue, Agent: "codex",
			CheckoutDirectory: checkout, SessionID: "sess-1", Effort: "high", Started: started, Log: "one\ntwo\r\nthree\n"},
		core.JobSnapshot{Target: "o/r", Number: 8, Status: "complete", Kind: core.JobKindIssue, CheckoutDirectory: filepath.Join(checkout, "gone")},
	)
	m.transcripts.For("o/r#7").Add(
		transcript.Event{Kind: transcript.KindSystem, Text: "first"},
		transcript.Event{Kind: transcript.KindSystem, Text: "second"},
	)

	detail, err := m.GetJob("", 7, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if detail["key"] != "o/r#7" || detail["session_id"] != "sess-1" || detail["effort"] != "high" {
		t.Errorf("detail = %v", detail)
	}
	if detail["checkout_directory"] != checkout || detail["checkout_state"] != "present" {
		t.Errorf("checkout = %v (%v), want %s present", detail["checkout_directory"], detail["checkout_state"], checkout)
	}
	if detail["scrollback"] != "two\nthree" {
		t.Errorf("scrollback with 2 lines = %q", detail["scrollback"])
	}
	events := detail["events"].([]transcript.Event)
	if len(events) != 1 || events[0].Text != "second" {
		t.Errorf("events with limit 1 = %+v, want the latest", events)
	}
	// The job runs on its project's model, so the detail says where it
	// comes from.
	if detail["model_source"] == nil {
		t.Errorf("model_source missing for a job on its project's model: %v", detail)
	}

	// Negative limits return everything; zero uses the defaults.
	detail, err = m.GetJob("o/r", 7, -1, -1)
	if err != nil {
		t.Fatal(err)
	}
	if detail["scrollback"] != "one\ntwo\r\nthree\n" || len(detail["events"].([]transcript.Event)) != 2 {
		t.Errorf("unlimited detail = %q, %v", detail["scrollback"], detail["events"])
	}
	detail, err = m.GetJob("o/r", 7, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail["events"].([]transcript.Event)) != 2 {
		t.Errorf("default events = %v", detail["events"])
	}

	// A job whose clone is gone from disk says so.
	detail, err = m.GetJob("o/r", 8, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if detail["checkout_state"] != "removed" {
		t.Errorf("checkout_state of a deleted clone = %v", detail["checkout_state"])
	}
	if _, ok := detail["model_source"]; ok {
		t.Errorf("a job with no agent claims its project's model: %v", detail["model_source"])
	}

	if _, err := m.GetJob("o/r", 99, 0, 0); err == nil || err.Error() != "no job o/r#99" {
		t.Errorf("missing job = %v", err)
	}
}

func TestGetJobNamesTheJobThatReusedItsClone(t *testing.T) {
	checkout := t.TempDir()
	now := time.Now()
	m, _ := apiTestManager(t,
		core.JobSnapshot{Target: "o/r", Number: 1, Status: "complete", CheckoutDirectory: checkout, Started: now.Add(-time.Hour)},
		core.JobSnapshot{Target: "o/r", Number: 2, Status: "active", CheckoutDirectory: checkout, Started: now},
	)
	detail, err := m.GetJob("o/r", 1, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if detail["checkout_state"] != "reused" || detail["checkout_reused_by"] != 2 {
		t.Errorf("checkout = %v by %v, want reused by 2", detail["checkout_state"], detail["checkout_reused_by"])
	}
}

func TestExplainJobDescribesEachStatus(t *testing.T) {
	m, _ := apiTestManager(t,
		core.JobSnapshot{Target: "o/r", Number: 1, Status: "active", Started: time.Now().Add(-90 * time.Second), Log: strings.Repeat("line\n", 30) + "last"},
		core.JobSnapshot{Target: "o/r", Number: 2, Status: "waiting", WakeAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), WaitReason: "CI is running"},
		core.JobSnapshot{Target: "o/r", Number: 3, Status: "paused", WaitReason: core.StalledPauseReason},
		core.JobSnapshot{Target: "o/r", Number: 4, Status: "paused", WaitReason: core.ManualPauseReason},
		core.JobSnapshot{Target: "o/r", Number: 5, Status: "paused", WaitReason: "blocked by #4"},
		core.JobSnapshot{Target: "o/r", Number: 6, Status: "queued"},
		core.JobSnapshot{Target: "o/r", Number: 7, Status: "stopping"},
		core.JobSnapshot{Target: "o/r", Number: 8, Status: "failed", WaitReason: "the agent crashed"},
		core.JobSnapshot{Target: "o/r", Number: 9, Status: "complete"},
		core.JobSnapshot{Target: "o/r", Number: 10, Status: "active"},
		core.JobSnapshot{Target: "o/r", Number: 11, Status: "waiting"},
		core.JobSnapshot{Target: "o/r", Number: 12, Status: "paused"},
	)
	for number, want := range map[int]string{
		1:  "Its agent is running, started 1m30s ago.",
		2:  "Its agent scheduled a wakeup and is waiting for it, due 2026-01-02T03:04:05Z: CI is running.",
		3:  "so Code Loop stopped resuming it.",
		4:  "The owner paused it.",
		5:  "It is paused, holding no agent slot: blocked by #4. Code Loop wakes it",
		6:  "It is queued for an agent slot; 1 of 3 slots are running.",
		7:  "It was stopped and Code Loop is waiting for its agent to exit.",
		8:  "Its status is failed: the agent crashed.",
		9:  "Its status is complete.",
		10: "Its agent is running. If its last output",
		11: "Its agent scheduled a wakeup and is waiting for it.",
		12: "It is paused, holding no agent slot. Code Loop",
	} {
		detail, err := m.ExplainJob("o/r", number)
		if err != nil {
			t.Fatal(err)
		}
		if got := detail["explanation"].(string); !strings.Contains(got, want) {
			t.Errorf("job %d explanation = %q, want it to contain %q", number, got, want)
		}
	}
	detail, err := m.ExplainJob("o/r", 1)
	if err != nil {
		t.Fatal(err)
	}
	if output := detail["last_output"].(string); strings.Count(output, "\n") != explainOutputLines-1 || !strings.HasSuffix(output, "last") {
		t.Errorf("last_output = %q, want the last %d lines", output, explainOutputLines)
	}
	if _, err := m.ExplainJob("o/r", 0); err == nil {
		t.Error("explaining job 0 succeeded")
	}
}

func TestExplainJobMarksCachedState(t *testing.T) {
	dir := t.TempDir()
	warmCache(t, dir)
	m := cacheTestManager(t, dir, "o/a", "o/b")
	detail, err := m.ExplainJob("o/a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := detail["explanation"].(string); !strings.HasPrefix(got, "This is its state when Code Loop last ran") || !strings.Contains(got, "Its agent is running") {
		t.Errorf("cached explanation = %q", got)
	}
	if detail["stale"] != true || detail["retry_unavailable"] != "Waiting for the first poll since the restart" {
		t.Errorf("cached job is not marked stale: %v", detail)
	}
}

func TestJobActionChecksStatusAndForwardsToEngine(t *testing.T) {
	m, engine := apiTestManager(t,
		core.JobSnapshot{Target: "o/r", Number: 1, Status: "active", Kind: core.JobKindIssue},
		core.JobSnapshot{Target: "o/r", Number: 2, Status: "paused", Kind: core.JobKindIssue},
		core.JobSnapshot{Target: "o/r", Number: 3, Status: "queued", Kind: core.JobKindIssue},
		core.JobSnapshot{Target: "o/r", Number: 4, Status: "complete", Kind: core.JobKindIssue},
		core.JobSnapshot{Target: "o/r", Number: 5, Status: "failed", Kind: core.JobKindSchedule, ScheduleID: "bot/digest"},
	)
	received := serveJobActions(t, engine, nil)

	for _, tc := range []struct {
		action string
		number int
		sent   string
	}{
		{"stop", 1, "stop"},
		{"pause", 1, "pause"},
		{"resume", 2, "retry"}, // resume clears a pause by retrying
		{"retry", 4, "retry"},
		{"dismiss", 5, "dismiss"},
	} {
		result, err := m.JobAction(t.Context(), tc.action, "o/r", tc.number)
		if err != nil {
			t.Fatalf("%s #%d: %v", tc.action, tc.number, err)
		}
		if result["ok"] != true || result["action"] != tc.sent {
			t.Errorf("%s #%d = %v", tc.action, tc.number, result)
		}
		if job := result["job"].(map[string]any); job["number"] != tc.number {
			t.Errorf("%s #%d reported job %v", tc.action, tc.number, job["number"])
		}
		if got := <-received; got != (core.JobAction{Action: tc.sent, Target: "o/r", Number: tc.number}) {
			t.Errorf("%s #%d sent %+v", tc.action, tc.number, got)
		}
	}

	for _, tc := range []struct {
		action string
		number int
		want   string
	}{
		{"resume", 1, "o/r#1 is active, not paused; there is nothing to resume"},
		{"retry", 3, "o/r#3 cannot be retried: Already queued for a gh-fix run"},
		{"pause", 4, "o/r#4 cannot be paused while complete"},
		{"stop", 4, "o/r#4 cannot be stopped while complete"},
		{"dismiss", 4, "o/r#4 is not a failed scheduled run; only those can be dismissed"},
		{"explode", 1, `unknown job action "explode"`},
		{"stop", 42, "no job o/r#42"},
	} {
		if _, err := m.JobAction(t.Context(), tc.action, "o/r", tc.number); err == nil || err.Error() != tc.want {
			t.Errorf("%s #%d = %v, want %q", tc.action, tc.number, err, tc.want)
		}
	}
	select {
	case got := <-received:
		t.Errorf("a refused action reached the engine: %+v", got)
	default:
	}
}

func TestJobActionReportsEngineFailures(t *testing.T) {
	job := core.JobSnapshot{Target: "o/r", Number: 1, Status: "active", Kind: core.JobKindIssue}

	// An engine that never reaches its dispatch loop is still starting.
	m, engine := apiTestManager(t, job)
	engine.notReadyWait = time.Millisecond
	if _, err := m.JobAction(t.Context(), "stop", "o/r", 1); err == nil || err.Error() != "the engine is still starting; try again" {
		t.Errorf("stop before the engine is ready = %v", err)
	}

	m, engine = apiTestManager(t, job)
	serveJobActions(t, engine, errors.New("job is still stopping"))
	if _, err := m.JobAction(t.Context(), "stop", "o/r", 1); err == nil || err.Error() != "job is still stopping" {
		t.Errorf("stop the engine refused = %v", err)
	}

	// A job that left the snapshot by the time the action finished is
	// reported without its summary.
	m, engine = apiTestManager(t, job)
	close(engine.readyChan())
	go func() {
		request := <-engine.jobActionRequests()
		m.snapshot.Store(&core.Snapshot{Targets: []string{"o/r"}})
		request.done <- nil
	}()
	result, err := m.JobAction(t.Context(), "stop", "o/r", 1)
	if err != nil {
		t.Fatal(err)
	}
	if result["ok"] != true || result["job"] != nil {
		t.Errorf("stop of a job that left the board = %v", result)
	}
}

func TestRefreshNudgesAPollForWatchedTargets(t *testing.T) {
	m, engine := apiTestManager(t)
	result, err := m.Refresh("o/r")
	if err != nil {
		t.Fatal(err)
	}
	if result["requested"] != true || !slices.Equal(result["targets"].([]string), []string{"o/r"}) {
		t.Errorf("refresh = %v", result)
	}
	select {
	case <-engine.negotiatedNudges():
	default:
		t.Fatal("refresh did not nudge the poll loop")
	}
	if _, err := m.Refresh(""); err != nil {
		t.Errorf("refresh of every project = %v", err)
	}
	if _, err := m.Refresh("x/y"); err == nil || err.Error() != "x/y is not watched" {
		t.Errorf("refresh of an unwatched target = %v", err)
	}
}

func TestCloneListAndClearGoThroughTheEngine(t *testing.T) {
	root := t.TempDir()
	m, engine := apiTestManager(t)
	engine.SetTempDirs([]string{root})
	finished := makeClone(t, root, "r-gh-fix-3", "o/r")
	if err := saveScopedWorkState(engine.StatePath, map[string]workState{"o/r#3": {Status: "completed", CheckoutDirectory: finished}}, []string{"o/r"}); err != nil {
		t.Fatal(err)
	}
	engine.recordClone(issueKey(Issue{Number: 3, Target: "o/r"}), "o/r", Issue{Number: 3, Target: "o/r"}, finished)

	listed, err := m.CloneList(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Clones) != 1 || listed.Clones[0].Path != finished || listed.Clones[0].InUse {
		t.Fatalf("clone list = %+v, want only the unused %s", listed.Clones, finished)
	}
	cleared, err := m.CloneClear(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(cleared.Removed) != 1 || cleared.Removed[0].Path != finished || isDir(finished) {
		t.Fatalf("clone clear = %+v, want %s removed", cleared, finished)
	}
}

func TestUnblockRunTriggersTheWatchdog(t *testing.T) {
	m, engine := apiTestManager(t, core.JobSnapshot{Target: "o/r", Number: 7, Status: "active"})

	result, err := m.UnblockRun("", 7)
	if err != nil {
		t.Fatal(err)
	}
	if result["requested"] != true || result["job"] != "o/r#7" || !strings.Contains(result["message"].(string), "for o/r#7") {
		t.Errorf("unblock of #7 = %v", result)
	}
	if key := <-engine.unblockTriggers(); key != "o/r#7" {
		t.Errorf("watchdog asked about %q, want o/r#7", key)
	}

	result, err = m.UnblockRun("", 0)
	if err != nil {
		t.Fatal(err)
	}
	if result["job"] != "" || !strings.Contains(result["message"].(string), "every stuck job") {
		t.Errorf("unblock of every job = %v", result)
	}
	// The round asked for above has not started, so another is refused.
	if _, err := m.UnblockRun("", 0); err == nil || !strings.Contains(err.Error(), "already requested") {
		t.Errorf("second unblock before the first started = %v", err)
	}
	if key := <-engine.unblockTriggers(); key != "" {
		t.Errorf("watchdog asked about %q, want every job", key)
	}

	if _, err := m.UnblockRun("o/r", 0); err == nil || err.Error() != "a target needs a job number" {
		t.Errorf("target without a number = %v", err)
	}
	if _, err := m.UnblockRun("", 8); err == nil || err.Error() != "no job #8" {
		t.Errorf("unblock of a missing job = %v", err)
	}
}

func TestIssueCommentResolvesTheRepositoryAndPosts(t *testing.T) {
	m, engine := apiTestManager(t,
		core.JobSnapshot{Target: "o/r", Number: 2, Status: "active"},
		core.JobSnapshot{Target: "o/r", Number: 5, Status: "active", Repository: "o/r"},
		core.JobSnapshot{Target: "board", Number: 5, Status: "active", Repository: "x/y"},
	)
	comments := updateCommentClient{fakeCommentClient: newFakeCommentClient(), author: "octo"}
	engine.Identity = "ABCD1234"
	engine.Comments = comments

	result, err := m.IssueComment(t.Context(), "", 2, "rebase onto main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result["repository"] != "o/r" || result["number"] != 2 || result["author"] != "octo" || result["url"] != "https://github.com/o/r/issues/2#issuecomment-1" {
		t.Errorf("comment = %v", result)
	}
	if attachments := result["attachments"].([]string); attachments == nil || len(attachments) != 0 {
		t.Errorf("attachments = %#v, want an empty list", attachments)
	}
	if posted := comments.comments["o/r#2"]; len(posted) != 1 || !strings.HasSuffix(posted[0].Body, "rebase onto main") {
		t.Errorf("posted = %+v", posted)
	}

	// An issue with no job goes to the only watched repository.
	if result, err := m.IssueComment(t.Context(), "", 3, "ping", nil); err != nil || result["repository"] != "o/r" {
		t.Errorf("comment on a jobless issue = %v, %v", result, err)
	}
	// Jobs in two repositories share #5, so it needs naming.
	if _, err := m.IssueComment(t.Context(), "", 5, "ping", nil); err == nil || err.Error() != "jobs in o/r and x/y all have #5; name the repository" {
		t.Errorf("ambiguous comment = %v", err)
	}
	if _, err := m.IssueComment(t.Context(), "x/y", 5, "ping", nil); err != nil {
		t.Errorf("comment naming its repository = %v", err)
	}
	if _, err := m.IssueComment(t.Context(), "", 0, "ping", nil); err == nil || err.Error() != "an issue or pull request number is required" {
		t.Errorf("comment without a number = %v", err)
	}
	if _, err := m.IssueComment(t.Context(), "", 2, "  ", nil); err == nil || err.Error() != "the update text is empty" {
		t.Errorf("empty comment = %v", err)
	}
	posts := comments.posts

	// The engine's own failure comes back as is.
	engine.Comments = nil
	if _, err := m.IssueComment(t.Context(), "o/r", 2, "ping", nil); err == nil || !strings.Contains(err.Error(), "no GitHub comment client") {
		t.Errorf("comment without a client = %v", err)
	}
	if comments.posts != posts {
		t.Errorf("refused comments posted: %d posts, want %d", comments.posts, posts)
	}
}

func TestIssueCommentNeedsRepositoryWhenSeveralAreWatched(t *testing.T) {
	m, engine := apiTestManager(t)
	engine.Comments = updateCommentClient{fakeCommentClient: newFakeCommentClient(), author: "octo"}
	m.snapshot.Store(&core.Snapshot{Targets: []string{"o/r", "x/y"}})
	if _, err := m.IssueComment(t.Context(), "", 3, "ping", nil); err == nil || err.Error() != "name the repository of #3" {
		t.Errorf("comment with two watched repositories = %v", err)
	}
}

func TestIssueCreateDefaultsToTheOnlyRepository(t *testing.T) {
	m, engine := apiTestManager(t)
	engine.Identity = "ABCD1234"
	engine.Registry = clidefs.MustBuiltin()
	engine.Runner = CommandRunner{Agent: "claude/opus", Agents: []string{"claude/opus"}}
	var prompts []string
	engine.oneShotRun = func(_ context.Context, _ agentSpec, prompt string) (string, error) {
		prompts = append(prompts, prompt)
		return "Filed https://github.com/o/r/issues/12\n", nil
	}

	result, err := m.IssueCreate(t.Context(), core.IssueCreate{Text: "Add a dark mode"})
	if err != nil {
		t.Fatal(err)
	}
	if result["url"] != "https://github.com/o/r/issues/12" || result["repository"] != "o/r" || result["agent"] != "claude/opus" {
		t.Errorf("create = %v", result)
	}
	if attachments := result["attachments"].([]string); attachments == nil || len(attachments) != 0 {
		t.Errorf("attachments = %#v, want an empty list", attachments)
	}
	if len(prompts) != 1 || !strings.HasPrefix(prompts[0], "/gh-create o/r Add a dark mode") {
		t.Errorf("prompts = %q", prompts)
	}

	if _, err := m.IssueCreate(t.Context(), core.IssueCreate{Text: "  "}); err == nil || err.Error() != "the issue description is empty" {
		t.Errorf("empty create = %v", err)
	}
	if _, err := m.IssueCreate(t.Context(), core.IssueCreate{Repository: "not a repo", Text: "x"}); err == nil || !strings.Contains(err.Error(), "is not OWNER/REPO") {
		t.Errorf("create on a bad repository = %v", err)
	}
	m.snapshot.Store(&core.Snapshot{Targets: []string{"o/r", "x/y"}})
	if _, err := m.IssueCreate(t.Context(), core.IssueCreate{Text: "x"}); err == nil || err.Error() != "name the repository to file the issue on" {
		t.Errorf("create with two watched repositories = %v", err)
	}
	if len(prompts) != 1 {
		t.Errorf("refused creates ran the agent: %q", prompts)
	}
}

func TestFindJobNeedsTargetWhenNumbersCollide(t *testing.T) {
	jobs := []core.JobSnapshot{{Target: "o/r", Number: 5}, {Target: "x/y", Number: 5}}
	if _, err := findJob(jobs, "", 5); err == nil || err.Error() != "more than one project has job #5; name its target" {
		t.Errorf("ambiguous job = %v", err)
	}
	if job, err := findJob(jobs, "x/y", 5); err != nil || job.Target != "x/y" {
		t.Errorf("job in x/y = %+v, %v", job, err)
	}
	if _, err := findJob(jobs, "", -1); err == nil || err.Error() != "a job number is required" {
		t.Errorf("negative job number = %v", err)
	}
}

func TestJobSummaryReportsScheduleQueueAndProgressFields(t *testing.T) {
	finished := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	schedule := jobSummary(core.JobSnapshot{Target: "o/r", Number: 1700000000, Kind: core.JobKindSchedule, ScheduleID: "bot/digest",
		Status: "complete", Summary: "sent the digest", Finished: finished, Fallback: true})
	if schedule["schedule_id"] != "bot/digest" || schedule["summary"] != "sent the digest" || schedule["finished"] != finished || schedule["result"] != "fallback" {
		t.Errorf("schedule summary = %v", schedule)
	}
	if _, ok := schedule["issue_url"]; ok {
		t.Errorf("a scheduled run links an issue: %v", schedule["issue_url"])
	}
	running := jobSummary(core.JobSnapshot{Target: "o/r", Number: 1700000001, Kind: core.JobKindSchedule, ScheduleID: "bot/digest", Status: "active"})
	if _, ok := running["result"]; ok {
		t.Errorf("a running scheduled run has a result: %v", running["result"])
	}

	merge := jobSummary(core.JobSnapshot{Target: "o/r", Number: 9, Kind: core.JobKindMerge, Status: "failed",
		Summary: "merge conflict in go.mod", Finished: finished, QueuePosition: 2})
	if merge["summary"] != "merge conflict in go.mod" || merge["finished"] != finished || merge["queue_position"] != 2 || merge["issue_url"] != "https://github.com/o/r/pull/9" {
		t.Errorf("merge summary = %v", merge)
	}

	issue := jobSummary(core.JobSnapshot{Target: "o/r", Number: 4, Kind: core.JobKindIssue, Status: "waiting", Repository: "o/r",
		PullRequest: 10, PullRequestState: "OPEN", TasksDone: 1, TasksTotal: 4, Progress: 25, WakeAt: finished, WaitReason: "CI"})
	if issue["repository"] != "o/r" || issue["pull_request"] != 10 || issue["pull_request_url"] != "https://github.com/o/r/pull/10" || issue["pull_request_state"] != "OPEN" {
		t.Errorf("issue pull request fields = %v", issue)
	}
	if progress := issue["progress"].(map[string]int); progress["done"] != 1 || progress["total"] != 4 || progress["percent"] != 25 {
		t.Errorf("progress = %v", progress)
	}
	if issue["wake_at"] != finished || issue["wait_reason"] != "CI" {
		t.Errorf("wake fields = %v", issue)
	}
	if issue["actions"].(map[string]any)["retry"] != true {
		t.Errorf("a waiting issue cannot be retried: %v", issue["actions"])
	}
	if _, ok := issue["retry_unavailable"]; ok {
		t.Errorf("retry_unavailable set while retry is available: %v", issue["retry_unavailable"])
	}
}

func TestTranscriptReturnsJobEvents(t *testing.T) {
	m := newTestManager(t)
	m.transcripts.For("o/r#3").Add(transcript.Event{Kind: transcript.KindSystem, Text: "started"})
	events := m.Transcript("o/r", 3)
	if len(events) != 1 || events[0].Text != "started" {
		t.Errorf("transcript = %+v", events)
	}
	if events := m.Transcript("o/r", 4); len(events) != 0 {
		t.Errorf("transcript of a job with none = %+v", events)
	}
}

func TestIssueFilterUsesTargetOverride(t *testing.T) {
	m := newTestManager(t)
	m.Reconcile(&config.Config{
		Agents:   []config.AgentConfig{{Name: "coder", Model: "cli/codex", Projects: config.AgentProjectsConfig{{Target: "o/r", Filter: []string{"label:bug"}}, {Target: "o/s"}}}},
		Projects: config.ProjectsConfig{Filter: []string{"label:ready"}},
	})
	if got := m.IssueFilter("o/r"); got != "label:bug" {
		t.Errorf("filter of o/r = %q, want its override", got)
	}
	if got := m.IssueFilter("o/s"); got != "label:ready" {
		t.Errorf("filter of o/s = %q, want the shared filter", got)
	}
	settings := engineSettings{Filter: "label:ready", TargetFilters: map[string]string{"o/r": "label:bug"}}
	if got := customFilterTargets(settings, []string{"o/r", "o/s"}); !slices.Equal(got, []string{"o/r"}) {
		t.Errorf("custom filter targets = %v", got)
	}
	settings.AllIssues = true
	settings.TargetFilters = nil
	if got := targetIssueFilters(settings, []string{"o/s"}); got["o/s"] != "" {
		t.Errorf("filter with every issue picked up = %q", got["o/s"])
	}
	if got := issueFilter(settings); got != "" {
		t.Errorf("shared filter with every issue picked up = %q", got)
	}
}
