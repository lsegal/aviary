package projects

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClaudeJSONOutputWriterRecordsTrailingScheduleWakeup(t *testing.T) {
	var output bytes.Buffer
	w := newClaudeJSONOutputWriter(&output)
	lines := []string{
		`{"type":"assistant","message":{"content":[{"type":"text","text":"The rest is still running; I'll wait for it to finish."}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"ScheduleWakeup","input":{"delaySeconds":240,"prompt":"/gh-fix o/r#7 identity:/aviary:ABC","reason":"watching CI run"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"scheduled"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Waiting on the background check run."}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"Waiting on the background check run."}`,
	}
	for _, line := range lines {
		if _, err := io.WriteString(w, line+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	wakeup, pending := w.PendingWakeup()
	if !pending {
		t.Fatal("a trailing ScheduleWakeup was not recorded")
	}
	want := scheduledWakeup{Delay: 4 * time.Minute, Prompt: "/gh-fix o/r#7 identity:/aviary:ABC", Reason: "watching CI run"}
	if wakeup != want {
		t.Fatalf("PendingWakeup() = %+v, want %+v", wakeup, want)
	}
	if !strings.Contains(output.String(), "Running: ScheduleWakeup /gh-fix o/r#7") {
		t.Fatalf("the wakeup call was not rendered: %q", output.String())
	}
}

func TestClaudeJSONOutputWriterDropsStoppedAndRejectedWakeups(t *testing.T) {
	for name, lines := range map[string][]string{
		"none scheduled": {
			`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"go test ./..."}}]}}`,
		},
		"stopped": {
			`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"ScheduleWakeup","input":{"delaySeconds":60,"prompt":"go on","reason":"r"}}]}}`,
			`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_2","name":"ScheduleWakeup","input":{"stop":true}}]}}`,
		},
		"rejected": {
			`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"ScheduleWakeup","input":{"delaySeconds":60,"prompt":"go on","reason":"r"}}]}}`,
			`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","is_error":true,"content":"not in loop mode"}]}}`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := newClaudeJSONOutputWriter(io.Discard)
			for _, line := range lines {
				if _, err := io.WriteString(w, line+"\n"); err != nil {
					t.Fatal(err)
				}
			}
			if wakeup, pending := w.PendingWakeup(); pending {
				t.Fatalf("PendingWakeup() = %+v, want none", wakeup)
			}
		})
	}
}

func TestParseScheduledWakeupClampsTheDelayLikeClaudeCode(t *testing.T) {
	for _, tc := range []struct {
		seconds float64
		want    time.Duration
	}{
		{seconds: 0, want: time.Minute},
		{seconds: 270, want: 270 * time.Second},
		{seconds: 86400, want: time.Hour},
	} {
		input, _ := json.Marshal(map[string]any{"delaySeconds": tc.seconds, "prompt": "p"})
		if got := parseScheduledWakeup(input); got == nil || got.Delay != tc.want {
			t.Fatalf("parseScheduledWakeup(%v seconds) = %+v, want a delay of %s", tc.seconds, got, tc.want)
		}
	}
}

func TestWakeupPromptUsesTheAgentsOwnPrompt(t *testing.T) {
	issue := Issue{Number: 7}
	if got := wakeupPrompt(issue, scheduledWakeup{Prompt: "/gh-fix o/r#7"}); got != "/gh-fix o/r#7" {
		t.Fatalf("wakeupPrompt() = %q, want the agent's own prompt", got)
	}
	for _, prompt := range []string{"", "<<autonomous-loop-dynamic>>"} {
		got := wakeupPrompt(issue, scheduledWakeup{Prompt: prompt, Reason: "watching CI run"})
		if !strings.Contains(got, "issue #7") || !strings.Contains(got, "watching CI run") {
			t.Fatalf("wakeupPrompt(%q) = %q, want a plain instruction naming the issue and reason", prompt, got)
		}
	}
}

func TestJobWaitLabelSaysWhatTheJobWaitsForAndWhen(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		job  JobSnapshot
		want string
	}{
		{JobSnapshot{WaitReason: "watching CI run", WakeAt: now.Add(3*time.Minute + time.Second)}, "waiting: watching CI run, resumes in 4m"},
		{JobSnapshot{WakeAt: now.Add(20 * time.Second)}, "waiting, resumes in 20s"},
		{JobSnapshot{WaitReason: "r", WakeAt: now.Add(-time.Second)}, "waiting: r, resuming"},
		{JobSnapshot{}, "waiting"},
	} {
		if got := jobWaitLabel(tc.job, now); got != tc.want {
			t.Fatalf("jobWaitLabel(%+v) = %q, want %q", tc.job, got, tc.want)
		}
	}
}

func TestCommandRunnerReportsAPendingWakeupWhenClaudeExits(t *testing.T) {
	output := strings.Join([]string{
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"ScheduleWakeup","input":{"delaySeconds":300,"prompt":"check CI","reason":"watching CI run"}}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"Waiting."}`,
	}, "\n")
	binary, _ := writeFakeAgent(t, output, 0)
	runner := CommandRunner{Agent: "claude", ClaudeBinary: binary, Repo: "o/r"}
	session := AgentSession{ID: "session-7", Agent: "claude", Resume: true}
	err := runner.RunSession(context.Background(), Issue{Number: 7}, session, func(AgentSession) {})
	var wakeup *scheduledWakeup
	if !errors.As(err, &wakeup) {
		t.Fatalf("RunSession() error = %v, want the scheduled wakeup", err)
	}
	if wakeup.Delay != 5*time.Minute || wakeup.Prompt != "check CI" || wakeup.Reason != "watching CI run" {
		t.Fatalf("scheduled wakeup = %+v", wakeup)
	}
}

// wakeupRunner returns the wakeups it is given, one per run, the way a Claude
// session that ends its turn on ScheduleWakeup does, and then blocks until its
// run is cancelled.
type wakeupRunner struct {
	sessions chan AgentSession
	mu       sync.Mutex
	wakeups  []*scheduledWakeup
	always   *scheduledWakeup
	calls    int
}

func (r *wakeupRunner) AgentName() string                { return "claude" }
func (r *wakeupRunner) Run(context.Context, Issue) error { return nil }
func (r *wakeupRunner) RunSession(ctx context.Context, _ Issue, session AgentSession, _ func(AgentSession)) error {
	select {
	case r.sessions <- session:
	default:
	}
	r.mu.Lock()
	r.calls++
	var next *scheduledWakeup
	if len(r.wakeups) > 0 {
		next, r.wakeups = r.wakeups[0], r.wakeups[1:]
	} else if r.always != nil {
		copied := *r.always
		next = &copied
	}
	r.mu.Unlock()
	if next != nil {
		return next
	}
	<-ctx.Done()
	return nil
}

func (r *wakeupRunner) RunSessionWithOutput(ctx context.Context, issue Issue, session AgentSession, update func(AgentSession), _ io.Writer) error {
	return r.RunSession(ctx, issue, session, update)
}

func (r *wakeupRunner) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func (r *snapshotReporter) job(number int) (JobSnapshot, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.snapshots) == 0 {
		return JobSnapshot{}, false
	}
	for _, job := range r.snapshots[len(r.snapshots)-1].Jobs {
		if job.Number == number {
			return job, true
		}
	}
	return JobSnapshot{}, false
}

func waitForJobStatus(t *testing.T, reporter *snapshotReporter, number int, status string) JobSnapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if job, ok := reporter.job(number); ok && job.Status == status {
			return job
		}
		time.Sleep(time.Millisecond)
	}
	job, _ := reporter.job(number)
	t.Fatalf("job #%d never reached %q; last seen %+v", number, status, job)
	return JobSnapshot{}
}

type wakeupHarness struct {
	w         *Engine
	src       *fakeClosureSource
	runner    *wakeupRunner
	reporter  *snapshotReporter
	out       *syncBuffer
	statePath string
	ctx       context.Context
}

func startWakeupHarness(t *testing.T, runner *wakeupRunner, delay time.Duration) *wakeupHarness {
	t.Helper()
	h := &wakeupHarness{
		src:       &fakeClosureSource{fakeSource: &fakeSource{batches: [][]Issue{{{Number: 7}}}}},
		runner:    runner,
		reporter:  &snapshotReporter{},
		out:       &syncBuffer{},
		statePath: filepath.Join(t.TempDir(), "state.json"),
	}
	h.src.state = OriginatingWorkState{IssueState: "OPEN", IssueBody: "original"}
	h.w = &Engine{
		Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: h.statePath,
		Issues: h.src, Runner: runner, Active: &activeAgents{}, Out: h.out, UI: h.reporter,
		closureInterval: time.Millisecond, wakeupDelayOverride: delay,
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.ctx = ctx
	done := make(chan error, 1)
	go func() { done <- h.w.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return h
}

func TestEngineResumesAScheduledWakeupWithItsOwnPrompt(t *testing.T) {
	runner := &wakeupRunner{
		sessions: make(chan AgentSession, 4),
		wakeups:  []*scheduledWakeup{{Delay: 4 * time.Minute, Prompt: "/gh-fix o/r#7 identity:/aviary:ABC", Reason: "watching CI run"}},
	}
	h := startWakeupHarness(t, runner, 200*time.Millisecond)

	first := waitForSession(t, runner.sessions)
	waiting := waitForJobStatus(t, h.reporter, 7, "waiting")
	if waiting.WaitReason != "watching CI run" || waiting.WakeAt.IsZero() {
		t.Fatalf("waiting job = %+v, want its reason and wake time", waiting)
	}
	resumed := waitForSession(t, runner.sessions)
	if !resumed.Resume || resumed.ID != first.ID {
		t.Fatalf("wakeup did not resume the same session: first=%+v resumed=%+v", first, resumed)
	}
	if resumed.Update != "/gh-fix o/r#7 identity:/aviary:ABC" {
		t.Fatalf("wakeup resumed the session with %q, want the agent's own prompt", resumed.Update)
	}
	job := waitForJobStatus(t, h.reporter, 7, "active")
	if job.WaitReason != "" || !job.WakeAt.IsZero() {
		t.Fatalf("resumed job still carries its wait: %+v", job)
	}
	if strings.Contains(h.out.String(), "completed") || strings.Contains(h.out.String(), "keepalive") {
		t.Fatalf("a run waiting on its wakeup was treated as finished:\n%s", h.out.String())
	}
	state, err := loadWorkState(h.statePath)
	if err != nil || state[7].Status != "active" {
		t.Fatalf("waiting work was not left active, state=%v err=%v", state, err)
	}
}

func TestEngineStopsAJobWaitingOnItsWakeup(t *testing.T) {
	runner := &wakeupRunner{
		sessions: make(chan AgentSession, 4),
		wakeups:  []*scheduledWakeup{{Delay: time.Hour, Prompt: "check CI", Reason: "watching CI run"}},
	}
	h := startWakeupHarness(t, runner, time.Hour)

	waitForSession(t, runner.sessions)
	waitForJobStatus(t, h.reporter, 7, "waiting")
	if err := h.w.handleJobAction(h.ctx, jobAction{Action: "stop", Target: "o/r", Number: 7}); err != nil {
		t.Fatalf("stopping a waiting job failed: %v", err)
	}
	waitForJobStatus(t, h.reporter, 7, "failed")
	waitForWorkStatus(t, h.statePath, 7, "failed")
	if calls := runner.callCount(); calls != 1 {
		t.Fatalf("the stopped wakeup still resumed the agent: %d runs", calls)
	}
}

func TestEngineCancelsTheWakeupWhenTheIssueCloses(t *testing.T) {
	runner := &wakeupRunner{
		sessions: make(chan AgentSession, 4),
		wakeups:  []*scheduledWakeup{{Delay: time.Hour, Prompt: "check CI", Reason: "watching CI run"}},
	}
	h := startWakeupHarness(t, runner, time.Hour)

	waitForSession(t, runner.sessions)
	waitForJobStatus(t, h.reporter, 7, "waiting")
	h.src.mu.Lock()
	h.src.state = OriginatingWorkState{IssueState: "CLOSED", IssueBody: "original", PullRequests: []PullRequestWorkState{{Number: 8, State: "MERGED", Merged: true}}}
	h.src.mu.Unlock()
	waitForJobStatus(t, h.reporter, 7, "complete")
	waitForWorkStatus(t, h.statePath, 7, "completed")
	if calls := runner.callCount(); calls != 1 {
		t.Fatalf("the cancelled wakeup still resumed the agent: %d runs", calls)
	}
	if !strings.Contains(h.out.String(), "cancelling the wakeup") {
		t.Fatalf("the cancelled wakeup was not logged:\n%s", h.out.String())
	}
}

func TestEngineCapsConsecutiveWakeups(t *testing.T) {
	runner := &wakeupRunner{
		sessions: make(chan AgentSession, 1),
		always:   &scheduledWakeup{Delay: time.Minute, Prompt: "check CI", Reason: "watching CI run"},
	}
	h := startWakeupHarness(t, runner, time.Millisecond)

	waitForWorkStatus(t, h.statePath, 7, "failed")
	if calls := runner.callCount(); calls != maxConsecutiveWakeups+1 {
		t.Fatalf("agent ran %d times, want the first run plus %d wakeups", calls, maxConsecutiveWakeups)
	}
	// The failed state is saved before the run's error is logged (issue
	// #785), so the log is waited on rather than read once the state fails.
	waitForLog(t, h.out, "wakeups in a row")
}
