package projects

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// heldRunner runs each issue until its test finishes it, so a test controls
// exactly when an agent slot frees.
type heldRunner struct {
	sessions chan Issue
	finish   chan struct{}
}

func (r *heldRunner) AgentName() string                { return "codex" }
func (r *heldRunner) Run(context.Context, Issue) error { return nil }
func (r *heldRunner) RunSession(ctx context.Context, issue Issue, _ AgentSession, update func(AgentSession)) error {
	update(AgentSession{ID: "session-" + issue.Title})
	r.sessions <- issue
	select {
	case <-r.finish:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func startCapacityEngine(t *testing.T, concurrency int, issues []Issue) (*Engine, *heldRunner, *fakeCommentClient, *fakeIssueStatuser, *syncBuffer, string) {
	t.Helper()
	statePath := filepath.Join(t.TempDir(), "state.json")
	runner := &heldRunner{sessions: make(chan Issue, 4), finish: make(chan struct{})}
	comments := newFakeCommentClient()
	statuses := &fakeIssueStatuser{}
	logs := &syncBuffer{}
	w := &Engine{
		Repo: "o/r", Interval: time.Hour, Concurrency: concurrency, StatePath: statePath,
		Issues: &fakeSource{batches: [][]Issue{issues}}, Runner: runner, Out: logs,
		Comments: comments, Identity: "SELF", Status: statuses,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return w, runner, comments, statuses, logs, statePath
}

func claimsOn(t *testing.T, comments *fakeCommentClient, number int) int {
	t.Helper()
	posted, _ := comments.ListComments(context.Background(), "o/r", number)
	claims := 0
	for _, comment := range posted {
		if kind, id, ok := parseClaim(comment.Body); ok && kind == claimStarting && id == "SELF" {
			claims++
		}
	}
	return claims
}

func waitForHeldIssue(t *testing.T, runner *heldRunner, logs *syncBuffer, want int) {
	t.Helper()
	select {
	case issue := <-runner.sessions:
		if issue.Number != want {
			t.Fatalf("dispatched #%d, want #%d:\n%s", issue.Number, want, logs)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("#%d was never dispatched:\n%s", want, logs)
	}
}

// With every agent slot busy, a new issue is left unclaimed: no claim
// comment, no project status, and no work record or board job. Once the slot
// frees, it is claimed and started without waiting for the next tick (issue
// #652).
func TestEngineLeavesNewIssuesUnclaimedWhileSlotsAreFull(t *testing.T) {
	_, runner, comments, statuses, logs, statePath := startCapacityEngine(t, 1, []Issue{{Number: 7, Title: "7"}, {Number: 8, Title: "8"}})
	waitForHeldIssue(t, runner, logs, 7)
	waitForLog(t, logs, "issue #8 not claimed: every agent slot is busy")

	if got := claimsOn(t, comments, 8); got != 0 {
		t.Fatalf("#8 got %d claim comment(s) while every slot was busy:\n%s", got, logs)
	}
	if got := claimsOn(t, comments, 7); got != 1 {
		t.Fatalf("#7 got %d claim comment(s), want 1", got)
	}
	statuses.mu.Lock()
	set := len(statuses.statuses)
	statuses.mu.Unlock()
	if set != 1 {
		t.Fatalf("project status set %d time(s), want only #7's", set)
	}
	saved, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "o/r#8") {
		t.Fatalf("#8 has a work record while it only waits for a slot: %s", saved)
	}
	if strings.Contains(logs.String(), "issue #8 queued") {
		t.Fatalf("#8 was queued as a claimed job:\n%s", logs)
	}

	// Finishing #7 frees its slot; the hour-long interval means only the
	// freed slot can be what dispatches #8.
	runner.finish <- struct{}{}
	waitForHeldIssue(t, runner, logs, 8)
	if got := claimsOn(t, comments, 8); got != 1 {
		t.Fatalf("#8 got %d claim comment(s) once a slot freed, want 1:\n%s", got, logs)
	}
}

// Below the concurrency limit, new issues are claimed and dispatched as
// before.
func TestEngineClaimsNewIssuesBelowTheLimit(t *testing.T) {
	_, runner, comments, _, logs, _ := startCapacityEngine(t, 2, []Issue{{Number: 7, Title: "7"}, {Number: 8, Title: "8"}})
	seen := map[int]bool{}
	for range 2 {
		select {
		case issue := <-runner.sessions:
			seen[issue.Number] = true
		case <-time.After(5 * time.Second):
			t.Fatalf("both issues were not dispatched:\n%s", logs)
		}
	}
	if !seen[7] || !seen[8] {
		t.Fatalf("dispatched %v, want #7 and #8", seen)
	}
	for _, number := range []int{7, 8} {
		if got := claimsOn(t, comments, number); got != 1 {
			t.Fatalf("#%d got %d claim comment(s), want 1", number, got)
		}
	}
	if strings.Contains(logs.String(), "not claimed") {
		t.Fatalf("an issue was left unclaimed below the limit:\n%s", logs)
	}
}

func TestNeedsSlotToClaim(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pending pendingIssue
		want    bool
	}{
		{"fresh pickup", pendingIssue{fresh: true}, true},
		{"contested pickup", pendingIssue{contested: true}, true},
		{"owned resume", pendingIssue{session: AgentSession{ID: "s", Agent: "codex", Resume: true}}, false},
		{"owned retry", pendingIssue{}, false},
		{"contested resume", pendingIssue{contested: true, session: AgentSession{ID: "s", Agent: "codex", Resume: true}}, false},
		{"woken paused work", pendingIssue{woken: true}, false},
	} {
		if got := tc.pending.needsSlotToClaim(); got != tc.want {
			t.Errorf("%s: needsSlotToClaim() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// With no free slot, a contested candidate is not negotiated for: nothing is
// asked or claimed, and it stays seen so a later poll treats it as contested
// again. A claim this instance already holds passes through to dispatch,
// which withdraws it if no slot is free there either.
func TestNegotiateContestedIssuesWithinNoRoomClaimsNothing(t *testing.T) {
	comments := newFakeCommentClient()
	w := &Engine{Comments: comments, Identity: "SELF", Out: io.Discard, ownershipWait: func(context.Context) bool { return true }}
	seen := map[string]bool{}
	pending := []pendingIssue{{issue: Issue{Number: 1, Target: "o/r", Repository: "o/r"}, contested: true}}
	result, full := w.negotiateContestedIssuesWithin(context.Background(), pending, contestedReap{seen: seen, aggressive: true, room: 0})
	w.negotiateWG.Wait()
	if len(result) != 0 || !full {
		t.Fatalf("result = %v, full = %v; want nothing dispatched and the batch reported full", result, full)
	}
	if comments.posts != 0 {
		t.Fatalf("posted %d comment(s) with no free slot", comments.posts)
	}
	if !seen["o/r#1"] {
		t.Fatalf("the skipped candidate lost its seen marker")
	}

	comments.inject("o/r", 1, Comment{Body: claimComment("SELF", false), CreatedAt: time.Now()})
	result, _ = w.negotiateContestedIssuesWithin(context.Background(), pending, contestedReap{seen: seen, aggressive: false, room: 0})
	if len(result) != 1 || result[0].claim == nil {
		t.Fatalf("a claim this instance already holds did not pass through: %+v", result)
	}
}

// Room limits how many contested candidates are negotiated for at once.
func TestNegotiateContestedIssuesWithinLimitsNegotiationsToRoom(t *testing.T) {
	comments := newFakeCommentClient()
	w := &Engine{Comments: comments, Identity: "SELF", Out: io.Discard, ownershipWait: func(context.Context) bool { return true }}
	pending := []pendingIssue{
		{issue: Issue{Number: 1, Target: "o/r", Repository: "o/r"}, contested: true},
		{issue: Issue{Number: 2, Target: "o/r", Repository: "o/r"}, contested: true},
	}
	_, full := w.negotiateContestedIssuesWithin(context.Background(), pending, contestedReap{seen: map[string]bool{}, aggressive: true, room: 1})
	w.negotiateWG.Wait()
	if !full {
		t.Fatalf("two candidates with room for one did not report the batch full")
	}
	asked := 0
	for _, number := range []int{1, 2} {
		if posted, _ := comments.ListComments(context.Background(), "o/r", number); len(posted) > 0 {
			asked++
		}
	}
	if asked != 1 {
		t.Fatalf("negotiated on %d ticket(s), want 1", asked)
	}
}
