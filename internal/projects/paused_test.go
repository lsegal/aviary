package projects

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

// waitForPaused waits until issue number is recorded as paused work of kind,
// and returns its paused record.
func waitForPaused(t *testing.T, statePath string, number int, kind string) core.PausedWork {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		state, err := loadWorkState(statePath)
		if record := state[number]; err == nil && record.Status == "paused" && record.Paused != nil && record.Paused.Kind == kind {
			return *record.Paused
		}
		if time.Now().After(deadline) {
			t.Fatalf("issue #%d was not paused as %s, state=%v err=%v", number, kind, state, err)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestEnginePausesADependencyBlockedIssueUntilItsBlockerCloses(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	src := pausingSource(blockedOn12("open"))
	// The blocker has no pull request to stack on, so the issue can only wait.
	src.states[12] = OriginatingWorkState{IssueState: "open"}
	runner := &finishingSessionRunner{agent: "claude", sessions: make(chan AgentSession, 4), finish: 1}
	logs := &syncBuffer{}
	w := &Engine{
		Repo: "o/r", Interval: 5 * time.Millisecond, Concurrency: 1, StatePath: statePath,
		Issues: src, Runner: runner, Out: logs, closureInterval: time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()

	paused := waitForPaused(t, statePath, 7, core.PausedBlocked)
	if !slices.Equal(paused.WaitingOn, []int{12}) {
		t.Fatalf("paused record = %+v, want it waiting on #12", paused)
	}
	select {
	case extra := <-runner.sessions:
		t.Fatalf("a paused blocked issue took a slot while its blocker is open: %+v", extra)
	case <-time.After(50 * time.Millisecond):
	}
	if count := strings.Count(logs.String(), "issue #7 not picked up: depends on #12 (open)"); count != 1 {
		t.Fatalf("pausing was logged %d time(s), want once:\n%s", count, logs)
	}

	src.mergeBlocker()
	waitForSession(t, runner.sessions)
	if !strings.Contains(logs.String(), "issue #7 unpaused: its blockers are closed; dispatching it") {
		t.Fatalf("waking was not logged:\n%s", logs)
	}
}

func TestEngineForgetsAPausedBlockedIssueThatClosed(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	src := pausingSource(blockedOn12("open"))
	src.states[12] = OriginatingWorkState{IssueState: "open"}
	runner := &finishingSessionRunner{agent: "claude", sessions: make(chan AgentSession, 4), finish: 1}
	w := &Engine{
		Repo: "o/r", Interval: 5 * time.Millisecond, Concurrency: 1, StatePath: statePath,
		Issues: src, Runner: runner, Out: io.Discard, closureInterval: time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()

	waitForPaused(t, statePath, 7, core.PausedBlocked)
	// The issue was closed: it drops out of the open issues the poll lists,
	// and reading it back says so.
	src.mu.Lock()
	src.states[7] = OriginatingWorkState{IssueState: "closed"}
	src.mu.Unlock()
	src.fakeSource.mu.Lock()
	src.batches = [][]Issue{{}}
	src.fakeSource.mu.Unlock()
	deadline := time.Now().Add(5 * time.Second)
	for {
		state, err := loadWorkState(statePath)
		if _, ok := state[7]; err == nil && !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("closed paused issue was not forgotten, state=%v err=%v", state, err)
		}
		time.Sleep(time.Millisecond)
	}
}

// Woken paused work goes back through the claim protocol even though its
// session resumes, so when two instances see the same wake, the one that
// claimed first keeps it and the other stands down.
func TestStandDownForStandingClaimsChecksWokenPausedWork(t *testing.T) {
	comments := newFakeCommentClient()
	comments.inject("o/r", 1, Comment{Body: signComment(startingClaimBody, "OTHER"), CreatedAt: time.Now().Add(-time.Minute)})
	comments.inject("o/r", 2, Comment{Body: signComment(startingClaimBody, "OTHER"), CreatedAt: time.Now().Add(-2 * time.Minute)})
	comments.inject("o/r", 2, Comment{Body: signComment(startingClaimBody, "SELF"), CreatedAt: time.Now().Add(-time.Minute)})
	w := &Engine{Comments: comments, Identity: "SELF", Out: io.Discard}
	pending := []pendingIssue{
		{issue: Issue{Number: 1, Repository: "o/r", Target: "o/r"}, session: AgentSession{Resume: true}, woken: true},
		{issue: Issue{Number: 2, Repository: "o/r", Target: "o/r"}, session: AgentSession{Resume: true}, woken: true},
	}
	result := w.standDownForStandingClaims(context.Background(), nil, pending, map[string]bool{})
	if len(result) != 1 || result[0].issue.Number != 2 {
		t.Fatalf("result = %+v, want the issue another instance claimed first (#1) stood down for and this instance's own claim (#2) kept", result)
	}
}

// A state file saved while paused work was called parked is read as paused,
// with its record kept.
func TestLoadWorkStateReadsParkedWorkAsPaused(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	legacy := `{
  "acme/app#7": {"status": "parked", "parked": {"kind": "blocked", "waitingOn": [3]}},
  "acme/app#8": {"status": "parked"}
}
`
	if err := os.WriteFile(statePath, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	state, err := loadScopedWorkState(statePath, []string{"acme/app"})
	if err != nil {
		t.Fatal(err)
	}
	blocked := state["acme/app#7"]
	if blocked.Status != "paused" || blocked.Paused == nil || blocked.Paused.Kind != core.PausedBlocked || !slices.Equal(blocked.Paused.WaitingOn, []int{3}) {
		t.Fatalf("blocked state = %+v, want paused blocked work waiting on #3", blocked)
	}
	if stacked := state["acme/app#8"]; stacked.Status != "paused" || pausedRecord(stacked).Kind != core.PausedStacked {
		t.Fatalf("stacked state = %+v, want paused stacked work", stacked)
	}
	if err := saveScopedWorkState(statePath, state, []string{"acme/app"}); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "parked") {
		t.Fatalf("saved state still says parked:\n%s", saved)
	}
}

// The nupp#5 shape: a pull-request record saved while some pull
// request was held, for an issue that now has no pull request, no sub-issues,
// and only closed dependencies. Nothing is left for it to wait on, so the
// first poll dispatches it instead of leaving it paused forever.
func TestEngineDispatchesPausedPullRequestWorkWithNothingToWaitOn(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	saved := `{"o/r#7": {"status": "paused", "paused": {"kind": "pull-request", "pullRequests": [15]}}}`
	if err := os.WriteFile(statePath, []byte(saved), 0600); err != nil {
		t.Fatal(err)
	}
	src := pausingSource(blockedOn12("closed"))
	src.states[7] = OriginatingWorkState{IssueState: "open"}
	runner := &finishingSessionRunner{agent: "claude", sessions: make(chan AgentSession, 4), finish: 1}
	logs := &syncBuffer{}
	w := &Engine{
		Repo: "o/r", Interval: 5 * time.Millisecond, Concurrency: 1, StatePath: statePath,
		Issues: src, Runner: runner, Out: logs, closureInterval: time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()

	waitForSession(t, runner.sessions)
	if !strings.Contains(logs.String(), "issue #7 unpaused: it has no open pull request and nothing blocks it; dispatching it") {
		t.Fatalf("waking was not logged:\n%s", logs)
	}
}

// Retrying a paused job from the web UI clears its pause and reruns gh-fix,
// whatever kind of work it was waiting on, leaving its session as a failed
// run's is.
func TestEngineWebUIRetriesPausedWork(t *testing.T) {
	cases := []struct {
		name   string
		paused string
		issues []Issue
		own    OriginatingWorkState
	}{
		{"blocked", `{"kind": "blocked", "waitingOn": [12]}`, blockedOn12("open"), OriginatingWorkState{IssueState: "open"}},
		{"stacked", `{"kind": "stacked", "waitingOn": [12], "pullRequests": [15]}`, blockedOn12("open"), OriginatingWorkState{IssueState: "open", PullRequests: []PullRequestWorkState{{Number: 15, State: "open", Stacked: true}}}},
		{"pull request", `{"kind": "pull-request", "pullRequests": [15]}`, []Issue{{Number: 7, Repository: "o/r"}}, OriginatingWorkState{IssueState: "open", PullRequests: []PullRequestWorkState{{Number: 15, State: "open"}}}},
		// A manual pause ends only when the owner retries it.
		{"manual", `{"kind": "manual"}`, []Issue{{Number: 7, Repository: "o/r"}}, OriginatingWorkState{IssueState: "open"}},
		{"legacy", ``, blockedOn12("open"), OriginatingWorkState{IssueState: "open", PullRequests: []PullRequestWorkState{{Number: 15, State: "open", Stacked: true}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			statePath := filepath.Join(t.TempDir(), "state.json")
			record := `"status": "paused", "sessionId": "session-7", "agent": "claude"`
			if tc.paused != "" {
				record += `, "paused": ` + tc.paused
			}
			if err := os.WriteFile(statePath, []byte(`{"o/r#7": {`+record+`}}`), 0600); err != nil {
				t.Fatal(err)
			}
			src := pausingSource(tc.issues)
			src.states[7] = tc.own
			// The blocker has no pull request to stack on, so only the retry
			// can move the job.
			src.states[12] = OriginatingWorkState{IssueState: "open"}
			// The rerun keeps running, so its active state can be read back.
			runner := &finishingSessionRunner{agent: "claude", sessions: make(chan AgentSession, 4)}
			logs := &syncBuffer{}
			w := &Engine{
				Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: statePath,
				Issues: src, Runner: runner, Out: logs, closureInterval: time.Millisecond,
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- w.Run(ctx) }()
			defer func() {
				cancel()
				<-done
			}()

			deadline := time.Now().Add(5 * time.Second)
			for !strings.Contains(logs.String(), "issue #7 paused: ") {
				if time.Now().After(deadline) {
					t.Fatalf("the restored work was not left paused:\n%s", logs)
				}
				time.Sleep(time.Millisecond)
			}
			select {
			case extra := <-runner.sessions:
				t.Fatalf("paused work ran before it was retried: %+v", extra)
			default:
			}

			if err := w.handleJobAction(ctx, jobAction{Action: "retry", Target: "o/r", Number: 7}); err != nil {
				t.Fatalf("retrying paused work failed: %v\n%s", err, logs)
			}
			session := waitForSession(t, runner.sessions)
			if !session.Resume || session.ID != "session-7" {
				t.Fatalf("session = %+v, want the paused work's session resumed", session)
			}
			waitForWorkStatus(t, statePath, 7, "active")
			if !strings.Contains(logs.String(), "issue #7 retry requested from web UI; clearing its pause and rerunning gh-fix") {
				t.Fatalf("the retry was not logged:\n%s", logs)
			}
		})
	}
}

// A retry that cannot requeue the paused job keeps it paused and says so,
// rather than reporting success for a retry that changed nothing.
func TestEngineWebUIRetryKeepsPauseWhenNothingIsRequeued(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(statePath, []byte(`{"o/r#7": {"status": "paused", "sessionId": "session-7", "agent": "claude", "paused": {"kind": "blocked", "waitingOn": [12]}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	src := pausingSource(blockedOn12("open"), []Issue{})
	src.states[12] = OriginatingWorkState{IssueState: "open"}
	runner := &finishingSessionRunner{agent: "claude", sessions: make(chan AgentSession, 4), finish: 1}
	logs := &syncBuffer{}
	w := &Engine{
		Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: statePath,
		Issues: src, Runner: runner, Out: logs, closureInterval: time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(logs.String(), "issue #7 paused: ") {
		if time.Now().After(deadline) {
			t.Fatalf("the restored work was not left paused:\n%s", logs)
		}
		time.Sleep(time.Millisecond)
	}
	// The retry's poll no longer lists the issue, so nothing can rerun it.
	err := w.handleJobAction(ctx, jobAction{Action: "retry", Target: "o/r", Number: 7})
	if err == nil || !strings.Contains(err.Error(), "stays paused") {
		t.Fatalf("retry error = %v, want it to report the job stays paused", err)
	}
	waitForPaused(t, statePath, 7, core.PausedBlocked)
	select {
	case extra := <-runner.sessions:
		t.Fatalf("a retry that was refused still ran: %+v", extra)
	default:
	}
}

// hydratingPausingSource also reads single issues back, as the paused watch
// does for paused work the poll does not list.
type hydratingPausingSource struct {
	*fakePausingSource
	issueMu   sync.Mutex
	issues    map[int]Issue
	hydrated  int
	hydrateCh chan struct{}
}

func (f *hydratingPausingSource) HydrateIssue(_ context.Context, _ string, issue *Issue) error {
	f.issueMu.Lock()
	read := f.issues[issue.Number]
	f.hydrated++
	f.issueMu.Unlock()
	issue.State, issue.DependsOn = read.State, read.DependsOn
	if f.hydrateCh != nil {
		select {
		case f.hydrateCh <- struct{}{}:
		default:
		}
	}
	return nil
}

func (f *hydratingPausingSource) setIssue(issue Issue) {
	f.issueMu.Lock()
	defer f.issueMu.Unlock()
	f.issues[issue.Number] = issue
}

// unlistedBlockedSource lists no issues at all, while issue #7 is paused on
// its open blocker #12.
func unlistedBlockedSource(t *testing.T, statePath string) *hydratingPausingSource {
	t.Helper()
	saved := `{"o/r#7": {"status": "paused", "paused": {"kind": "blocked", "waitingOn": [12]}}}`
	if err := os.WriteFile(statePath, []byte(saved), 0600); err != nil {
		t.Fatal(err)
	}
	src := &hydratingPausingSource{fakePausingSource: pausingSource([]Issue{}), issues: map[int]Issue{}, hydrateCh: make(chan struct{}, 1)}
	src.states[7] = OriginatingWorkState{IssueState: "open"}
	src.states[12] = OriginatingWorkState{IssueState: "open"}
	src.setIssue(Issue{Number: 7, State: "open", DependsOn: []IssueDependency{{Number: 12, State: "open"}}})
	return src
}

// A paused issue the poll no longer lists is still watched: once its blocker
// closes, the paused watch wakes it and dispatches it.
func TestEngineWakesPausedWorkTheDispatchListNoLongerHas(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	src := unlistedBlockedSource(t, statePath)
	runner := &finishingSessionRunner{agent: "claude", sessions: make(chan AgentSession, 4), finish: 1}
	logs := &syncBuffer{}
	w := &Engine{
		Repo: "o/r", Interval: 5 * time.Millisecond, Concurrency: 1, StatePath: statePath,
		Issues: src, Runner: runner, Out: logs, closureInterval: time.Millisecond, pausedInterval: 20 * time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()

	select {
	case <-src.hydrateCh:
	case <-time.After(5 * time.Second):
		t.Fatal("unlisted paused issue was never read back")
	}
	select {
	case extra := <-runner.sessions:
		t.Fatalf("paused issue was dispatched while its blocker is open: %+v", extra)
	case <-time.After(50 * time.Millisecond):
	}
	if !strings.Contains(logs.String(), "issue #7 paused: depends on #12 (open)") {
		t.Fatalf("unlisted paused issue was not re-checked:\n%s", logs)
	}

	src.setIssue(Issue{Number: 7, State: "open", DependsOn: []IssueDependency{{Number: 12, State: "closed"}}})
	waitForSession(t, runner.sessions)
	if !strings.Contains(logs.String(), "issue #7 unpaused: its blockers are closed; dispatching it") {
		t.Fatalf("waking was not logged:\n%s", logs)
	}
}

// In push mode the fallback poll can be far apart, so the paused watch runs
// on its own interval: paused work is re-checked within it even when nothing
// else in the run happens.
func TestEngineWatchesPausedWorkOnItsOwnInterval(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	src := unlistedBlockedSource(t, statePath)
	runner := &finishingSessionRunner{agent: "claude", sessions: make(chan AgentSession, 4), finish: 1}
	w := &Engine{
		Repo: "o/r", Interval: 5 * time.Millisecond, Concurrency: 1, StatePath: statePath,
		Issues: src, Runner: runner, Out: io.Discard, closureInterval: time.Millisecond,
		UseWebhooks: true, fallbackInterval: time.Hour, reapInterval: time.Hour, pausedInterval: 20 * time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()

	for range 3 {
		select {
		case <-src.hydrateCh:
		case <-time.After(5 * time.Second):
			t.Fatal("paused work was not re-checked within its paused interval")
		}
	}
	src.setIssue(Issue{Number: 7, State: "open", DependsOn: []IssueDependency{{Number: 12, State: "closed"}}})
	waitForSession(t, runner.sessions)
}

func TestPausedWatchIsNeverFasterThanThePoll(t *testing.T) {
	w := &Engine{Interval: time.Minute}
	if got := w.pausedWatchEvery(); got != pausedWatchInterval {
		t.Fatalf("pausedWatchEvery() = %s, want %s", got, pausedWatchInterval)
	}
	if got := w.pausedWatchTick(); got != 0 {
		t.Fatalf("pausedWatchTick() = %s in poll mode, want every poll to carry the watch", got)
	}
	w.Interval = time.Hour
	if got := w.pausedWatchEvery(); got != time.Hour {
		t.Fatalf("pausedWatchEvery() = %s, want the slower poll interval", got)
	}
	push := &Engine{Interval: time.Minute, UseWebhooks: true}
	if got := push.pausedWatchTick(); got != pausedWatchInterval {
		t.Fatalf("pausedWatchTick() = %s in push mode, want %s", got, pausedWatchInterval)
	}
}
