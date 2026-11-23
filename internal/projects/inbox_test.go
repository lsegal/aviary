package projects

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/projects/core"
	"github.com/lsegal/aviary/internal/transcript"
)

func inboxTestManager(t *testing.T) (*Manager, *Engine) {
	t.Helper()
	m := newTestManager(t)
	engine := &Engine{StatePath: filepath.Join(t.TempDir(), "state.json"), Transcripts: newTranscriptStore("")}
	m.running = &engineRun{engine: engine}
	m.snapshot.Store(&core.Snapshot{Jobs: []core.JobSnapshot{
		{Number: 1, Target: "o/r", Kind: core.JobKindIssue, Status: "active"},
		{Number: 2, Target: "o/r", Kind: core.JobKindIssue, Status: "waiting"},
		{Number: 3, Target: "o/r", Kind: core.JobKindIssue, Status: "paused"},
		{Number: 4, Target: "o/r", Kind: core.JobKindIssue, Status: "complete"},
		{Number: 5, Target: "o/r", Kind: core.JobKindSchedule, ScheduleID: "bot/digest", Status: "complete"},
		{Number: 6, Target: "o/r", Kind: core.JobKindSchedule, ScheduleID: "bot/digest", Status: "active"},
		{Number: 7, Target: "o/r", Kind: core.JobKindSchedule, ScheduleID: "bot/sweep", Status: "failed"},
		{Number: 8, Target: "o/r/discussions", Kind: core.JobKindDiscussion, Status: "active"},
		{Number: 9, Target: "o/r/discussions", Kind: core.JobKindDiscussion, Status: "complete"},
	}})
	return m, engine
}

func TestSendJobMessageByJobState(t *testing.T) {
	m, engine := inboxTestManager(t)
	msg := InboxMessage{Source: "workflow bot/triage", Text: "build is red"}

	// A running or waiting job is woken: its watcher's nudge fires.
	for _, number := range []int{1, 2} {
		nudge := make(chan struct{}, 1)
		key := "o/r#" + strconv.Itoa(number)
		engine.liveNudges.Store(key, nudge)
		outcome, _, err := m.SendJobMessage(context.Background(), "o/r", number, msg, JobDeliveryOptions{})
		if err != nil || outcome != InboxDelivered {
			t.Fatalf("job %d = %q, %v; want delivered", number, outcome, err)
		}
		select {
		case <-nudge:
		default:
			t.Fatalf("job %d was not woken", number)
		}
		if got := engine.jobInbox().pending(key); got != 1 {
			t.Fatalf("job %d has %d pending messages, want 1", number, got)
		}
	}

	// A paused job keeps its state and holds the message for its next run.
	outcome, job, err := m.SendJobMessage(context.Background(), "o/r", 3, msg, JobDeliveryOptions{})
	if err != nil || outcome != InboxQueued || job.Status != "paused" {
		t.Fatalf("paused job = %q, %q, %v; want queued and still paused", outcome, job.Status, err)
	}
	if got := engine.jobInbox().pending("o/r#3"); got != 1 {
		t.Fatalf("paused job has %d pending messages, want 1", got)
	}

	// A completed job rejects the output and queues nothing.
	if _, _, err := m.SendJobMessage(context.Background(), "o/r", 4, msg, JobDeliveryOptions{}); err == nil || !strings.Contains(err.Error(), "is complete") {
		t.Fatalf("complete job err = %v, want it rejected", err)
	}
	if got := engine.jobInbox().pending("o/r#4"); got != 0 {
		t.Fatalf("complete job has %d pending messages, want 0", got)
	}

	// A job that no longer exists is an error the caller can recognize.
	if _, _, err := m.SendJobMessage(context.Background(), "o/r", 99, msg, JobDeliveryOptions{}); !errors.Is(err, ErrJobGone) {
		t.Fatalf("missing job err = %v, want ErrJobGone", err)
	}
}

func TestSendJobMessageToDiscussionJobs(t *testing.T) {
	m, engine := inboxTestManager(t)
	msg := InboxMessage{Source: "task bot/digest", Text: "new release notes"}
	nudge := make(chan struct{}, 1)
	engine.liveNudges.Store("o/r/discussions#8", nudge)
	outcome, job, err := m.SendJobMessage(context.Background(), "o/r/discussions", 8, msg, JobDeliveryOptions{})
	if err != nil || outcome != InboxDelivered || job.Kind != core.JobKindDiscussion {
		t.Fatalf("active discussion = %q, %+v, %v; want delivered", outcome, job, err)
	}
	select {
	case <-nudge:
	default:
		t.Fatal("the active discussion was not woken")
	}
	if got := engine.jobInbox().pending("o/r/discussions#8"); got != 1 {
		t.Fatalf("active discussion has %d pending messages, want 1", got)
	}
	if _, _, err := m.SendJobMessage(context.Background(), "o/r/discussions", 9, msg, JobDeliveryOptions{}); err == nil || !strings.Contains(err.Error(), "is complete") {
		t.Fatalf("answered discussion err = %v, want it rejected", err)
	}
	if _, _, err := m.SendJobMessage(context.Background(), "o/r/discussions", 9, msg, JobDeliveryOptions{Reopen: true}); err == nil || !strings.Contains(err.Error(), "is complete") {
		t.Fatalf("answered discussion with reopen err = %v, want it rejected", err)
	}
}

func TestSendJobMessageToScheduledRuns(t *testing.T) {
	m, engine := inboxTestManager(t)
	msg := InboxMessage{Source: "workflow bot/triage", Text: "three new issues"}

	// Any run of a schedule names it: an earlier, finished run reaches the
	// schedule's run in flight, which is woken.
	nudge := make(chan struct{}, 1)
	engine.liveNudges.Store("o/r#6", nudge)
	outcome, job, err := m.SendJobMessage(context.Background(), "o/r", 5, msg, JobDeliveryOptions{})
	if err != nil || outcome != InboxDelivered || job.Number != 6 {
		t.Fatalf("schedule with a run in flight = %q, run %d, %v; want delivered to run 6", outcome, job.Number, err)
	}
	select {
	case <-nudge:
	default:
		t.Fatal("the schedule's run in flight was not woken")
	}
	if got := engine.jobInbox().pending("o/r#6"); got != 1 {
		t.Fatalf("run in flight has %d pending messages, want 1", got)
	}

	// Between runs, output waits for the schedule's next run, even when the
	// last run failed.
	outcome, job, err = m.SendJobMessage(context.Background(), "o/r", 7, msg, JobDeliveryOptions{})
	if err != nil || outcome != InboxQueued || job.Number != 7 {
		t.Fatalf("schedule between runs = %q, run %d, %v; want queued", outcome, job.Number, err)
	}
	if got := engine.jobInbox().pending(scheduleInboxKey("o/r", "bot/sweep")); got != 1 {
		t.Fatalf("schedule queue has %d pending messages, want 1", got)
	}

	// A run from before a restart is no longer on the board, but core's run
	// history still names its schedule.
	fire := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	m.SetScheduleSource(func() []domain.ScheduledTask {
		return []domain.ScheduledTask{{ID: "bot/nightly", Name: "nightly", Project: "o/r", History: []domain.TaskRun{{FireTime: fire}}}}
	})
	outcome, job, err = m.SendJobMessage(context.Background(), "o/r", int(fire.Unix()), msg, JobDeliveryOptions{})
	if err != nil || outcome != InboxQueued || job.ScheduleID != "bot/nightly" {
		t.Fatalf("schedule run from history = %q, %+v, %v; want queued for bot/nightly", outcome, job, err)
	}
	if got := engine.jobInbox().pending(scheduleInboxKey("o/r", "bot/nightly")); got != 1 {
		t.Fatalf("schedule queue has %d pending messages, want 1", got)
	}
}

// serveJobActions answers the engine's job action requests the way its
// dispatch loop does, recording each and replying with reply.
func serveJobActions(t *testing.T, engine *Engine, reply error) <-chan core.JobAction {
	t.Helper()
	close(engine.readyChan())
	seen := make(chan core.JobAction, 4)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		for {
			select {
			case request := <-engine.jobActionRequests():
				seen <- request.action
				request.done <- reply
			case <-ctx.Done():
				return
			}
		}
	}()
	return seen
}

func TestSendJobMessageResumesPausedJob(t *testing.T) {
	m, engine := inboxTestManager(t)
	actions := serveJobActions(t, engine, nil)
	msg := InboxMessage{Source: "task bot/digest", Text: "new results"}

	outcome, _, err := m.SendJobMessage(context.Background(), "o/r", 3, msg, JobDeliveryOptions{Resume: true})
	if err != nil || outcome != InboxResumed {
		t.Fatalf("paused job with resume = %q, %v; want resumed", outcome, err)
	}
	// Resume is the dashboard's resume: a retry of the paused job, whose run
	// relays the queued message on its first turn.
	if got := <-actions; got.Action != "retry" || got.Target != "o/r" || got.Number != 3 {
		t.Fatalf("job action = %+v, want retry of o/r#3", got)
	}
	if got := engine.jobInbox().pending("o/r#3"); got != 1 {
		t.Fatalf("resumed job has %d pending messages, want 1", got)
	}

	// Resume only applies to paused jobs; a completed job still rejects.
	if _, _, err := m.SendJobMessage(context.Background(), "o/r", 4, msg, JobDeliveryOptions{Resume: true}); err == nil || !strings.Contains(err.Error(), "is complete") {
		t.Fatalf("complete job with resume err = %v, want it rejected", err)
	}
}

func TestSendJobMessageQueuesWhenResumeFails(t *testing.T) {
	m, engine := inboxTestManager(t)
	serveJobActions(t, engine, errors.New("no slot"))
	outcome, _, err := m.SendJobMessage(context.Background(), "o/r", 3, InboxMessage{Text: "x"}, JobDeliveryOptions{Resume: true})
	if err != nil || outcome != InboxQueued {
		t.Fatalf("failed resume = %q, %v; want queued", outcome, err)
	}
	if got := engine.jobInbox().pending("o/r#3"); got != 1 {
		t.Fatalf("job has %d pending messages, want 1", got)
	}
}

func TestSendJobMessageReopensCompletedJob(t *testing.T) {
	m, engine := inboxTestManager(t)
	actions := serveJobActions(t, engine, nil)
	msg := InboxMessage{Source: "workflow bot/triage", Text: "regression is back"}

	outcome, _, err := m.SendJobMessage(context.Background(), "o/r", 4, msg, JobDeliveryOptions{Reopen: true})
	if err != nil || outcome != InboxReopened {
		t.Fatalf("complete job with reopen = %q, %v; want reopened", outcome, err)
	}
	if got := <-actions; got.Action != "retry" || got.Number != 4 {
		t.Fatalf("job action = %+v, want retry of o/r#4", got)
	}
	if got := engine.jobInbox().pending("o/r#4"); got != 1 {
		t.Fatalf("reopened job has %d pending messages, want 1", got)
	}

	// Reopen leaves a paused job queued as before.
	outcome, _, err = m.SendJobMessage(context.Background(), "o/r", 3, msg, JobDeliveryOptions{Reopen: true})
	if err != nil || outcome != InboxQueued {
		t.Fatalf("paused job with reopen = %q, %v; want queued", outcome, err)
	}
	select {
	case got := <-actions:
		t.Fatalf("paused job with reopen ran action %+v", got)
	default:
	}
}

func TestSendJobMessageReopenFailureDropsOutput(t *testing.T) {
	m, engine := inboxTestManager(t)
	serveJobActions(t, engine, errors.New("no slot"))
	if _, _, err := m.SendJobMessage(context.Background(), "o/r", 4, InboxMessage{Text: "x"}, JobDeliveryOptions{Reopen: true}); err == nil || !strings.Contains(err.Error(), "reopening") {
		t.Fatalf("failed reopen err = %v, want a delivery error", err)
	}
	if got := engine.jobInbox().pending("o/r#4"); got != 0 {
		t.Fatalf("failed reopen left %d pending messages, want 0", got)
	}
}

func TestSendJobMessageWithoutEngine(t *testing.T) {
	m := newTestManager(t)
	if _, _, err := m.SendJobMessage(context.Background(), "o/r", 1, InboxMessage{Text: "x"}, JobDeliveryOptions{}); !errors.Is(err, ErrNotWatching) {
		t.Fatalf("err = %v, want ErrNotWatching", err)
	}
}

func TestJobInboxSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inbox.json")
	first := &jobInbox{path: path}
	if err := first.add("o/r#3", InboxMessage{Source: "task bot/digest", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	second := &jobInbox{path: path}
	got := second.take("o/r#3")
	if len(got) != 1 || got[0].Text != "hello" {
		t.Fatalf("reloaded inbox = %+v, want the queued message", got)
	}
	if again := (&jobInbox{path: path}).take("o/r#3"); len(again) != 0 {
		t.Fatalf("taken message came back: %+v", again)
	}
}

func TestRelayInboxInterruptsRunWithLabelledMessage(t *testing.T) {
	_, engine := inboxTestManager(t)
	issue := Issue{Number: 1, Target: "o/r", Repository: "o/r"}
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	if engine.relayInbox(issue, cancel) {
		t.Fatal("relayed with nothing queued")
	}
	at := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	if err := engine.jobInbox().add("o/r#1", InboxMessage{Source: "workflow bot/triage", SourceURL: "/agents/bot/tasks/triage/runs/job_1", Text: "build is red", At: at}); err != nil {
		t.Fatal(err)
	}
	if !engine.relayInbox(issue, cancel) {
		t.Fatal("queued message was not relayed")
	}
	update, ok := workUpdateFor(context.Cause(ctx))
	if !ok {
		t.Fatalf("cause = %v, want a workUpdate", context.Cause(ctx))
	}
	if !strings.Contains(update.instruction, "workflow bot/triage") || !strings.Contains(update.instruction, "build is red") {
		t.Fatalf("instruction = %q, want the source and the text", update.instruction)
	}
	events := engine.Transcripts.For("o/r#1").Events(0)
	if len(events) != 1 || events[0].Kind != transcript.KindCommentRelay || events[0].Source != "workflow bot/triage" || events[0].URL != "/agents/bot/tasks/triage/runs/job_1" {
		t.Fatalf("transcript = %+v, want the relayed output labelled with its run", events)
	}
	if engine.jobInbox().pending("o/r#1") != 0 {
		t.Fatal("relayed message is still pending")
	}
}

func TestWatcherRelaysQueuedOutputOnResume(t *testing.T) {
	_, engine := inboxTestManager(t)
	engine.Comments = &fakeCommentClient{}
	issue := Issue{Number: 3, Target: "o/r", Repository: "o/r"}
	if err := engine.jobInbox().add("o/r#3", InboxMessage{Source: "task bot/digest", Text: "queued while paused"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	ready := make(chan struct{})
	done := make(chan struct{})
	go func() {
		engine.watchForIssueUpdates(ctx, nil, issue, issueWatch{since: time.Now(), resumable: func() bool { return true }}, cancel, ready)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("watcher did not relay the queued output")
	}
	<-ready
	update, ok := workUpdateFor(context.Cause(ctx))
	if !ok || !strings.Contains(update.instruction, "queued while paused") {
		t.Fatalf("cause = %v, want the queued output relayed", context.Cause(ctx))
	}
}
