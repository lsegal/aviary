package projects

import (
	"context"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
	"github.com/lsegal/aviary/internal/scheduler"
	"github.com/lsegal/aviary/internal/transcript"
)

// relayRunner reports every session it runs. A fresh session reports its ID
// and blocks until its run is interrupted; a resumed one finishes at once.
// onStart, when set, runs as each session starts.
type relayRunner struct {
	sessions chan AgentSession
	onStart  func(AgentSession)
	block    bool
}

func (r *relayRunner) AgentName() string                { return "relay" }
func (r *relayRunner) Run(context.Context, Issue) error { return nil }
func (r *relayRunner) RunSessionWithOutput(ctx context.Context, _ Issue, session AgentSession, update func(AgentSession), output io.Writer) error {
	r.sessions <- session
	if r.onStart != nil {
		r.onStart(session)
	}
	if session.Resume || !r.block {
		_, _ = io.WriteString(output, "done\n")
		return nil
	}
	update(AgentSession{ID: "relay-session"})
	<-ctx.Done()
	return ctx.Err()
}

func nextSession(t *testing.T, sessions chan AgentSession) AgentSession {
	t.Helper()
	select {
	case session := <-sessions:
		return session
	case <-time.After(5 * time.Second):
		t.Fatal("the agent did not run")
		return AgentSession{}
	}
}

// startRelayEngine runs an engine on targets with runner and discussions.
func startRelayEngine(t *testing.T, runner *relayRunner, targets []string, discussions DiscussionSource) (*Engine, context.Context) {
	t.Helper()
	w := &Engine{
		Targets: targets, Interval: time.Hour, Concurrency: 2,
		StatePath: filepath.Join(t.TempDir(), "state.json"),
		Issues:    &fakeSource{batches: [][]Issue{{}}}, Discussions: discussions,
		Runner: runner, UI: &snapshotReporter{}, Transcripts: newTranscriptStore(""),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	return w, ctx
}

func waitForJob(t *testing.T, w *Engine, target string, number int, ok func(JobSnapshot) bool) JobSnapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if snapshot := w.published.Load(); snapshot != nil {
			for _, job := range snapshot.Jobs {
				if job.Target == target && job.Number == number && ok(job) {
					return job
				}
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %s#%d never reached the expected state", target, number)
	return JobSnapshot{}
}

func assertRelayed(t *testing.T, w *Engine, key string, resumed AgentSession, firstID string) {
	t.Helper()
	if !resumed.Resume || resumed.ID != firstID {
		t.Fatalf("resumed session = %+v, want session %s resumed", resumed, firstID)
	}
	if !strings.Contains(resumed.Update, "workflow bot/triage") || !strings.Contains(resumed.Update, "build is red") {
		t.Fatalf("resumed prompt = %q, want the source and the text", resumed.Update)
	}
	events := w.Transcripts.For(key).Events(0)
	for _, event := range events {
		if event.Kind == transcript.KindCommentRelay && event.Source == "workflow bot/triage" && event.Text == "build is red" {
			return
		}
	}
	t.Fatalf("transcript = %+v, want the output labelled with its run", events)
}

func TestEngineRelaysOutputIntoARunningDiscussionJob(t *testing.T) {
	target := "https://github.com/o/r/discussions"
	runner := &relayRunner{sessions: make(chan AgentSession, 4), block: true}
	w, _ := startRelayEngine(t, runner, []string{target}, &fakeDiscussionSource{batches: [][]Discussion{{{Number: 8, Title: "How do I deploy?"}}}})

	first := nextSession(t, runner.sessions)
	if first.Resume || first.ID == "" {
		t.Fatalf("first session = %+v, want a fresh session with an ID", first)
	}
	job := waitForJob(t, w, target, 8, func(j JobSnapshot) bool { return j.Status == "active" && j.SessionID == "relay-session" })
	if job.Kind != core.JobKindDiscussion || job.Title != "How do I deploy?" {
		t.Fatalf("discussion job = %+v", job)
	}
	if actions := core.JobActionsFor(job); !actions.Stop || actions.Retry || actions.Pause {
		t.Fatalf("discussion actions = %+v, want stop only", actions)
	}

	key := target + "#8"
	if err := w.jobInbox().add(key, InboxMessage{Source: "workflow bot/triage", Text: "build is red", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	w.nudgeJob(key)
	assertRelayed(t, w, key, nextSession(t, runner.sessions), "relay-session")
	waitForJob(t, w, target, 8, func(j JobSnapshot) bool { return j.Status == "complete" })
}

func TestEngineRelaysOutputIntoARunningScheduledRun(t *testing.T) {
	runner := &relayRunner{sessions: make(chan AgentSession, 4), block: true}
	w, ctx := startRelayEngine(t, runner, []string{"o/r"}, nil)
	fire := time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)
	errs := make(chan error, 1)
	go func() {
		_, err := w.runProjectTask(ctx, scheduler.ProjectRun{TaskID: "bot/watch", Name: "watch", Project: "o/r", Prompt: "Watch CI", Fire: fire}, "o/r", AgentSession{})
		errs <- err
	}()
	if first := nextSession(t, runner.sessions); first.Resume {
		t.Fatalf("first session = %+v, want a fresh session", first)
	}
	key := "o/r#" + strconv.Itoa(int(fire.Unix()))
	waitForJob(t, w, "o/r", int(fire.Unix()), func(j JobSnapshot) bool { return j.SessionID == "relay-session" })
	if err := w.jobInbox().add(key, InboxMessage{Source: "workflow bot/triage", Text: "build is red", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	w.nudgeJob(key)
	// The resumed session is the one the agent reported.
	assertRelayed(t, w, key, nextSession(t, runner.sessions), "relay-session")
	if err := <-errs; err != nil {
		t.Fatalf("run error = %v", err)
	}
}

func TestEngineStartsTheNextScheduledRunWithOutputQueuedBetweenRuns(t *testing.T) {
	runner := &relayRunner{sessions: make(chan AgentSession, 4)}
	w, ctx := startRelayEngine(t, runner, []string{"o/r"}, nil)
	if err := w.jobInbox().add(scheduleInboxKey("o/r", "bot/digest"), InboxMessage{Source: "workflow bot/triage", Text: "build is red", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	compiledRan := false
	run := scheduler.ProjectRun{TaskID: "bot/digest", Name: "digest", Project: "o/r", Prompt: "Summarize", Fire: time.Now(),
		RunCompiled: func(context.Context, func(context.Context) (string, error), func(string, ...any)) (string, error) {
			compiledRan = true
			return "ran the script", nil
		}}
	if _, err := w.runProjectTask(ctx, run, "o/r", AgentSession{}); err != nil {
		t.Fatal(err)
	}
	if compiledRan {
		t.Fatal("the precompiled script ran, so the queued output was never read")
	}
	first := nextSession(t, runner.sessions)
	if first.Resume || !strings.Contains(first.Update, "build is red") || !strings.Contains(first.Update, "workflow bot/triage") {
		t.Fatalf("first session = %+v, want a fresh run that starts with the queued output", first)
	}
	if got := w.jobInbox().pending(scheduleInboxKey("o/r", "bot/digest")); got != 0 {
		t.Fatalf("schedule queue still has %d messages", got)
	}
}

func TestEngineCarriesOutputAScheduledRunMissedToTheNextRun(t *testing.T) {
	fire := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	key := "o/r#" + strconv.Itoa(int(fire.Unix()))
	var w *Engine
	runner := &relayRunner{sessions: make(chan AgentSession, 4)}
	// The output arrives as the run ends, too late for it to be relayed.
	runner.onStart = func(AgentSession) {
		if err := w.jobInbox().add(key, InboxMessage{Source: "task bot/late", Text: "too late", At: time.Now()}); err != nil {
			t.Error(err)
		}
	}
	w, ctx := startRelayEngine(t, runner, []string{"o/r"}, nil)
	if _, err := w.runProjectTask(ctx, scheduler.ProjectRun{TaskID: "bot/sweep", Name: "sweep", Project: "o/r", Prompt: "Sweep", Fire: fire}, "o/r", AgentSession{}); err != nil {
		t.Fatal(err)
	}
	if got := w.jobInbox().pending(key); got != 0 {
		t.Fatalf("finished run still holds %d messages", got)
	}
	if got := w.jobInbox().pending(scheduleInboxKey("o/r", "bot/sweep")); got != 1 {
		t.Fatalf("schedule queue has %d messages, want the missed output carried over", got)
	}
}

func TestJobPromptStartsAFreshRunWithItsQueuedOutput(t *testing.T) {
	r := CommandRunner{Repo: "o/r"}
	discussion := jobPrompt(r, Issue{Number: 8, Target: "https://github.com/o/r/discussions"}, AgentSession{Update: "New output was sent"})
	if !strings.HasPrefix(discussion, "/gh-discuss 8") || !strings.HasSuffix(discussion, "\n\nNew output was sent") {
		t.Fatalf("discussion prompt = %q", discussion)
	}
	scheduled := jobPrompt(r, Issue{Number: 1, Target: "o/r", Prompt: "Summarize", ScheduleID: "bot/digest"}, AgentSession{Update: "New output was sent"})
	if !strings.HasPrefix(scheduled, "Summarize") || !strings.HasSuffix(scheduled, "\n\nNew output was sent") {
		t.Fatalf("scheduled prompt = %q", scheduled)
	}
	if plain := jobPrompt(r, Issue{Number: 1, Target: "o/r", Prompt: "Summarize"}, AgentSession{}); strings.Contains(plain, "New output") {
		t.Fatalf("prompt without queued output = %q", plain)
	}
}

func TestEngineStopsADiscussionJobAndRefusesItsOtherActions(t *testing.T) {
	target := "https://github.com/o/r/discussions"
	runner := &relayRunner{sessions: make(chan AgentSession, 4), block: true}
	w, ctx := startRelayEngine(t, runner, []string{target}, &fakeDiscussionSource{batches: [][]Discussion{{{Number: 4, Title: "Q"}}}})
	nextSession(t, runner.sessions)
	waitForJob(t, w, target, 4, func(j JobSnapshot) bool { return j.Status == "active" })
	for _, action := range []string{"retry", "pause"} {
		if err := w.handleJobAction(ctx, jobAction{Action: action, Target: target, Number: 4}); err == nil || !strings.Contains(err.Error(), "can only be stopped") {
			t.Fatalf("%s = %v, want it refused", action, err)
		}
	}
	if err := w.handleJobAction(ctx, jobAction{Action: "stop", Target: target, Number: 4}); err != nil {
		t.Fatalf("stop: %v", err)
	}
	waitForJob(t, w, target, 4, func(j JobSnapshot) bool { return j.Status == "failed" })
}
