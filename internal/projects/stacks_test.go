package projects

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

// fakeStackSource is a closure source whose repository reports whether
// stacked pull requests are enabled.
type fakeStackSource struct {
	*fakeClosureSource
	enabled bool
	probes  int
	mu      sync.Mutex
}

func (f *fakeStackSource) StacksEnabled(context.Context, string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.probes++
	return f.enabled, nil
}

func TestStackableDependencyNeedsExactlyOneOpenBlocker(t *testing.T) {
	for _, test := range []struct {
		name  string
		issue Issue
		want  int
	}{
		{"one open blocker", Issue{DependsOn: []IssueDependency{{Number: 4, State: "open"}, {Number: 5, State: "CLOSED"}}}, 4},
		{"two open blockers", Issue{DependsOn: []IssueDependency{{Number: 4, State: "open"}, {Number: 5, State: "open"}}}, 0},
		{"tracking issue", Issue{HasSubIssues: true, DependsOn: []IssueDependency{{Number: 4, State: "open"}}}, 0},
		{"no open blockers", Issue{DependsOn: []IssueDependency{{Number: 4, State: "closed"}}}, 0},
	} {
		dependency, ok := stackableDependency(test.issue)
		if got := map[bool]int{true: dependency.Number}[ok]; got != test.want {
			t.Errorf("%s: stackableDependency() = %d, %v; want %d", test.name, dependency.Number, ok, test.want)
		}
	}
}

func TestGHCLIStacksEnabled(t *testing.T) {
	for _, test := range []struct {
		name    string
		output  string
		err     error
		want    bool
		wantErr bool
	}{
		{"enabled", `[]`, nil, true, false},
		{"not enabled", `{"message":"Not Found"}` + "\ngh: Not Found (HTTP 404)", errors.New("exit status 1"), false, false},
		{"failure", "gh: Server Error (HTTP 500)", errors.New("exit status 1"), false, true},
	} {
		var args []string
		gh := GHCLI{runCommand: func(_ context.Context, a ...string) ([]byte, error) {
			args = a
			return []byte(test.output), test.err
		}}
		got, err := gh.StacksEnabled(context.Background(), "owner/repo")
		if got != test.want || (err != nil) != test.wantErr {
			t.Errorf("%s: StacksEnabled() = %v, %v; want %v, error %v", test.name, got, err, test.want, test.wantErr)
		}
		if strings.Join(args, " ") != "api repos/owner/repo/stacks?per_page=1" {
			t.Errorf("%s: gh args = %q", test.name, args)
		}
	}
}

func runBlockedIssuePoll(t *testing.T, source IssueSource) (int, string) {
	t.Helper()
	runner := &fakeRunner{release: make(chan struct{}), dispatched: make(chan int, 1)}
	logs := &syncBuffer{}
	w := &Engine{
		Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: filepath.Join(t.TempDir(), "state.json"),
		Issues: source, Runner: runner, Out: logs,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	got := 0
	select {
	case got = <-runner.dispatched:
	case <-time.After(200 * time.Millisecond):
	}
	close(runner.release)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	return got, logs.String()
}

func blockedIssueSource(enabled bool, pullRequests ...PullRequestWorkState) *fakeStackSource {
	return &fakeStackSource{
		fakeClosureSource: &fakeClosureSource{
			fakeSource: &fakeSource{batches: [][]Issue{{
				{Number: 7, Repository: "o/r", DependsOn: []IssueDependency{{Number: 12, State: "open"}}},
			}}},
			state: OriginatingWorkState{IssueState: "open", PullRequests: pullRequests},
		},
		enabled: enabled,
	}
}

func TestEngineStacksBlockedIssueOnBlockerPullRequest(t *testing.T) {
	got, logs := runBlockedIssuePoll(t, blockedIssueSource(true, PullRequestWorkState{Number: 13, State: "closed"}, PullRequestWorkState{Number: 14, State: "open"}))
	if got != 7 {
		t.Fatalf("dispatched #%d, want blocked issue #7 stacked on its blocker\n%s", got, logs)
	}
	if !strings.Contains(logs, "issue #7 depends on #12 (open); stacking it on pull request #14") {
		t.Fatalf("stacked dispatch was not logged:\n%s", logs)
	}
}

func TestEngineWaitsForBlockerWithoutStackedPullRequests(t *testing.T) {
	for _, test := range []struct {
		name   string
		source *fakeStackSource
	}{
		{"stacks disabled", blockedIssueSource(false, PullRequestWorkState{Number: 14, State: "open"})},
		{"blocker has no open pull request", blockedIssueSource(true, PullRequestWorkState{Number: 14, State: "closed", Merged: false})},
	} {
		got, logs := runBlockedIssuePoll(t, test.source)
		if got != 0 {
			t.Errorf("%s: dispatched blocked issue #%d", test.name, got)
		}
		if !strings.Contains(logs, "issue #7 not picked up: depends on #12 (open)") {
			t.Errorf("%s: blocked issue was not logged:\n%s", test.name, logs)
		}
	}
}

func TestEngineCachesStackedPullRequestSetting(t *testing.T) {
	source := blockedIssueSource(false)
	w := &Engine{Issues: source}
	for range 3 {
		if w.stacksEnabled(context.Background(), "o/r") {
			t.Fatal("stacksEnabled() = true, want false")
		}
	}
	if source.probes != 1 {
		t.Fatalf("StacksEnabled probes = %d, want 1 cached probe", source.probes)
	}
}

func TestOriginatingWorkStateReadsStackedPullRequestBase(t *testing.T) {
	for _, test := range []struct {
		name string
		pull string
		want bool
	}{
		{"stacked on a blocker branch", `{"state":"open","draft":false,"base":{"ref":"fix/issue-12-blocker","repo":{"default_branch":"main"}}}`, true},
		{"targets the default branch", `{"state":"open","draft":false,"base":{"ref":"main","repo":{"default_branch":"main"}}}`, false},
	} {
		responses := [][]byte{
			[]byte(`{"state":"open"}`),
			[]byte(`[{"event":"cross-referenced","source":{"issue":{"number":9,"body":"Closes #7","pull_request":{"merged_at":null}}}}]`),
			[]byte(test.pull),
		}
		call := 0
		gh := GHCLI{runCommand: func(_ context.Context, _ ...string) ([]byte, error) {
			call++
			return responses[call-1], nil
		}}
		state, err := gh.OriginatingWorkState(context.Background(), "owner/repo", 7)
		if err != nil || len(state.PullRequests) != 1 || state.PullRequests[0].Stacked != test.want {
			t.Errorf("%s: OriginatingWorkState() = (%#v, %v), want Stacked %v", test.name, state, err, test.want)
		}
	}
}

// fakePausingSource answers per issue number, so a dependent issue's stacked
// pull request and its blocker's pull request can be told apart.
type fakePausingSource struct {
	*fakeSource
	mu     sync.Mutex
	states map[int]OriginatingWorkState
}

func (f *fakePausingSource) OriginatingWorkState(_ context.Context, _ string, number int) (OriginatingWorkState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.states[number], nil
}

func (f *fakePausingSource) StacksEnabled(context.Context, string) (bool, error) { return true, nil }

// mergeBlocker makes every later poll report issue #7's blocker as closed. The
// test calls it only after seeing the issue paused: with the merge scripted
// into a fixed batch, a poll can unpause the issue before the test reads the
// paused state, and the state never shows it again.
func (f *fakePausingSource) mergeBlocker() {
	f.fakeSource.mu.Lock()
	defer f.fakeSource.mu.Unlock()
	f.batches = [][]Issue{blockedOn12("closed")}
}

func pausingSource(batches ...[]Issue) *fakePausingSource {
	return &fakePausingSource{
		fakeSource: &fakeSource{batches: batches},
		states: map[int]OriginatingWorkState{
			7:  {IssueState: "open", PullRequests: []PullRequestWorkState{{Number: 15, State: "open", Stacked: true}}},
			12: {IssueState: "open", PullRequests: []PullRequestWorkState{{Number: 14, State: "open"}}},
		},
	}
}

func blockedOn12(state string) []Issue {
	return []Issue{{Number: 7, Repository: "o/r", DependsOn: []IssueDependency{{Number: 12, State: state}}}}
}

func waitForWorkStatus(t *testing.T, statePath string, number int, status string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		state, err := loadWorkState(statePath)
		if err == nil && state[number].Status == status {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("issue #%d was not recorded as %s, state=%v err=%v", number, status, state, err)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestEnginePausesAStackedPullRequestWaitingOnItsBlocker(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	src := pausingSource(blockedOn12("open"))
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
	waitForWorkStatus(t, statePath, 7, "paused")
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(logs.String(), "issue #7 paused: depends on #12 (open); its stacked pull request waits for that to merge") {
		if time.Now().After(deadline) {
			t.Fatalf("a later poll did not leave the paused issue waiting:\n%s", logs)
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case extra := <-runner.sessions:
		t.Fatalf("a paused stacked pull request relaunched its agent while its blocker is open: %+v", extra)
	default:
	}
	if !strings.Contains(logs.String(), "issue #7 paused: its stacked pull request is ready and waits on its blocker to merge; freeing its slot") {
		t.Fatalf("pausing was not logged:\n%s", logs)
	}
}

func TestEnginePausesAHeldPullRequestThatIsNotStackedOnItsPullRequest(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	src := pausingSource(blockedOn12("open"))
	// Merge withheld on a pull request that already targets the default
	// branch: a donotmerge hold, not a paused stack.
	src.states[7] = OriginatingWorkState{IssueState: "open", PullRequests: []PullRequestWorkState{{Number: 15, State: "open"}}}
	runner := &finishingSessionRunner{agent: "claude", sessions: make(chan AgentSession, 4), finish: 1}
	w := &Engine{
		Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: statePath,
		Issues: src, Runner: runner, Out: &syncBuffer{}, closureInterval: time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()

	waitForSession(t, runner.sessions)
	waitForPaused(t, statePath, 7, core.PausedPullRequest)
}

func TestEngineRedispatchesAPausedIssueOnceItsBlockerMerges(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	src := pausingSource(blockedOn12("open"))
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

	first := waitForSession(t, runner.sessions)
	waitForWorkStatus(t, statePath, 7, "paused")
	// The blocker merged: its issue closed and the stacked pull request was
	// retargeted, so the dependent issue is dispatched again to finish it.
	src.mu.Lock()
	src.states[7] = OriginatingWorkState{IssueState: "open", PullRequests: []PullRequestWorkState{{Number: 15, State: "open", IsDraft: true}}}
	src.mu.Unlock()
	src.mergeBlocker()
	resumed := waitForSession(t, runner.sessions)
	if !resumed.Resume || resumed.ID != first.ID || !strings.Contains(resumed.Update, "The blocker is no longer open") {
		t.Fatalf("paused issue was not resumed with its blocker merged: first=%+v resumed=%+v", first, resumed)
	}
	if !strings.Contains(logs.String(), "issue #7 unpaused: its blocker is no longer open; resuming its stacked pull request") {
		t.Fatalf("unpausing was not logged:\n%s", logs)
	}
}
