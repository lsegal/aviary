package projects

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSignAndParseClaim(t *testing.T) {
	body := signComment(startingClaimBody, "AAA111")
	kind, id, ok := parseClaim(body)
	if !ok {
		t.Fatalf("expected claim to parse: %q", body)
	}
	if kind != claimStarting {
		t.Fatalf("kind = %v, want claimStarting", kind)
	}
	if id != "AAA111" {
		t.Fatalf("id = %q, want AAA111", id)
	}
}

func TestParseClaimVariants(t *testing.T) {
	cases := []struct {
		body string
		kind claimKind
		ok   bool
	}{
		{signComment(askClaimBody, "X"), claimAsking, true},
		{signComment(startingClaimBody, "X"), claimStarting, true},
		{signComment(continuingClaimBody, "X"), claimContinuing, true},
		{signComment(presenceClaimBody, "X"), claimPresence, true},
		{"just a regular comment", claimUnknown, false},
		{"Starting work on this issue with no signature", claimUnknown, false},
	}
	for _, tc := range cases {
		kind, _, ok := parseClaim(tc.body)
		if ok != tc.ok || (ok && kind != tc.kind) {
			t.Errorf("parseClaim(%q) = (%v, %v), want (%v, %v)", tc.body, kind, ok, tc.kind, tc.ok)
		}
	}
}

func TestLastCommentMatchesOnlyTheNewestExactComment(t *testing.T) {
	comments := newFakeCommentClient()
	comments.inject("o/r", 1, Comment{Body: claimComment("SELF", false)})
	w := &Engine{Comments: comments}

	matched, err := w.lastCommentMatches(context.Background(), "o/r", 1, claimComment("SELF", false))
	if err != nil || !matched {
		t.Fatalf("lastCommentMatches() = (%v, %v), want (true, nil)", matched, err)
	}
	comments.inject("o/r", 1, Comment{Body: "A newer comment"})
	matched, err = w.lastCommentMatches(context.Background(), "o/r", 1, claimComment("SELF", false))
	if err != nil || matched {
		t.Fatalf("lastCommentMatches() = (%v, %v), want (false, nil)", matched, err)
	}
}

func TestClaimedByOtherIgnoresOwnAndOldComments(t *testing.T) {
	after := time.Now()
	comments := []Comment{
		{Body: signComment(startingClaimBody, "SELF"), CreatedAt: after.Add(time.Second)},
		{Body: signComment(startingClaimBody, "OTHER"), CreatedAt: after.Add(-time.Second)},
	}
	if owner, ok := claimedByOther(comments, after, "SELF"); ok {
		t.Fatalf("expected no other claimant: own comment and a stale comment shouldn't count, got %q", owner)
	}
	comments = append(comments, Comment{Body: signComment(presenceClaimBody, "OTHER"), CreatedAt: after.Add(time.Minute)})
	owner, ok := claimedByOther(comments, after, "SELF")
	if !ok || owner != "OTHER" {
		t.Fatalf("claimedByOther = (%q, %v), want the claiming identity OTHER so logs can name it", owner, ok)
	}
}

func TestClaimedByOtherIgnoresAskComments(t *testing.T) {
	after := time.Now()
	comments := []Comment{
		{Body: signComment(askClaimBody, "OTHER"), CreatedAt: after.Add(time.Second)},
	}
	if _, ok := claimedByOther(comments, after, "SELF"); ok {
		t.Fatalf("an ask comment alone should not count as a claim")
	}
}

type fakeCommentClient struct {
	mu        sync.Mutex
	comments  map[string][]Comment
	posts     int
	lists     int
	postErr   error
	listErr   error
	reactions []fakeReaction
	// reviews holds each pull request's review comments and review bodies.
	reviews  map[string][]Comment
	reactErr error
}

type fakeReaction struct {
	Repo      string
	CommentID int64
	// Kind and NodeID say which reaction endpoint was used.
	Kind    CommentKind
	NodeID  string
	Content string
}

func newFakeCommentClient() *fakeCommentClient {
	return &fakeCommentClient{comments: make(map[string][]Comment), reviews: make(map[string][]Comment)}
}

func (f *fakeCommentClient) key(repo string, number int) string {
	return fmt.Sprintf("%s#%d", repo, number)
}

func (f *fakeCommentClient) PostComment(_ context.Context, repo string, number int, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.postErr != nil {
		return f.postErr
	}
	f.posts++
	key := f.key(repo, number)
	f.comments[key] = append(f.comments[key], Comment{Body: body, CreatedAt: time.Now()})
	return nil
}

func (f *fakeCommentClient) ListComments(_ context.Context, repo string, number int) ([]Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lists++
	if f.listErr != nil {
		return nil, f.listErr
	}
	key := f.key(repo, number)
	out := make([]Comment, len(f.comments[key]))
	copy(out, f.comments[key])
	return out, nil
}

// reset drops every comment on a target, standing in for the ways a reap can
// read a ticket and learn nothing about who owns it.
func (f *fakeCommentClient) reset(repo string, number int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.comments, f.key(repo, number))
}

func (f *fakeCommentClient) inject(repo string, number int, comment Comment) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := f.key(repo, number)
	f.comments[key] = append(f.comments[key], comment)
}

func (f *fakeCommentClient) AddReaction(_ context.Context, repo string, commentID int64, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reactErr != nil {
		return f.reactErr
	}
	f.reactions = append(f.reactions, fakeReaction{Repo: repo, CommentID: commentID, Content: content})
	return nil
}

func (f *fakeCommentClient) reactionsSnapshot() []fakeReaction {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeReaction, len(f.reactions))
	copy(out, f.reactions)
	return out
}

func TestNegotiateOwnershipClaimsWhenUncontested(t *testing.T) {
	comments := newFakeCommentClient()
	w := &Engine{Comments: comments, Identity: "SELF", ownershipWait: func(context.Context) bool { return true }}
	claimed, err := w.negotiateOwnership(context.Background(), ownershipTarget{Repo: "o/r", Number: 1})
	if err != nil {
		t.Fatalf("negotiateOwnership: %v", err)
	}
	if !claimed {
		t.Fatalf("expected to claim uncontested issue")
	}
	posted, _ := comments.ListComments(context.Background(), "o/r", 1)
	if len(posted) != 2 {
		t.Fatalf("expected an ask comment and a starting comment, got %d", len(posted))
	}
	if kind, _, ok := parseClaim(posted[0].Body); !ok || kind != claimAsking {
		t.Fatalf("first comment should be the ask, got %q", posted[0].Body)
	}
	if kind, id, ok := parseClaim(posted[1].Body); !ok || kind != claimStarting || id != "SELF" {
		t.Fatalf("second comment should be our starting claim, got %q", posted[1].Body)
	}
}

func TestNegotiateOwnershipUsesContinuingClaimForDraftPR(t *testing.T) {
	comments := newFakeCommentClient()
	w := &Engine{Comments: comments, Identity: "SELF", ownershipWait: func(context.Context) bool { return true }}
	claimed, err := w.negotiateOwnership(context.Background(), ownershipTarget{Repo: "o/r", Number: 5, Continue: true})
	if err != nil || !claimed {
		t.Fatalf("negotiateOwnership: claimed=%v err=%v", claimed, err)
	}
	posted, _ := comments.ListComments(context.Background(), "o/r", 5)
	if kind, _, ok := parseClaim(posted[len(posted)-1].Body); !ok || kind != claimContinuing {
		t.Fatalf("expected a continuing claim, got %q", posted[len(posted)-1].Body)
	}
}

func TestNegotiateOwnershipStandsDownWhenAnotherInstanceResponds(t *testing.T) {
	comments := newFakeCommentClient()
	waited := false
	w := &Engine{Comments: comments, Identity: "SELF", ownershipWait: func(context.Context) bool {
		// Simulate another instance answering while we wait.
		comments.inject("o/r", 1, Comment{Body: signComment(presenceClaimBody, "OTHER"), CreatedAt: time.Now()})
		waited = true
		return true
	}}
	claimed, err := w.negotiateOwnership(context.Background(), ownershipTarget{Repo: "o/r", Number: 1})
	if err != nil {
		t.Fatalf("negotiateOwnership: %v", err)
	}
	if !waited {
		t.Fatalf("expected the wait hook to run")
	}
	if claimed {
		t.Fatalf("expected to stand down after another instance responded")
	}
	posted, _ := comments.ListComments(context.Background(), "o/r", 1)
	for _, comment := range posted {
		if kind, id, ok := parseClaim(comment.Body); ok && kind == claimStarting && id == "SELF" {
			t.Fatalf("should not have posted a starting claim after standing down")
		}
	}
}

func TestNegotiateOwnershipStandsDownOnCompetingStartingClaim(t *testing.T) {
	comments := newFakeCommentClient()
	w := &Engine{Comments: comments, Identity: "SELF", ownershipWait: func(context.Context) bool {
		comments.inject("o/r", 1, Comment{Body: signComment(startingClaimBody, "LATER"), CreatedAt: time.Now()})
		return true
	}}
	claimed, err := w.negotiateOwnership(context.Background(), ownershipTarget{Repo: "o/r", Number: 1})
	if err != nil || claimed {
		t.Fatalf("claimed=%v err=%v, want claimed=false", claimed, err)
	}
}

func TestNegotiateOwnershipReturnsFalseWhenContextCancelledDuringWait(t *testing.T) {
	comments := newFakeCommentClient()
	ctx, cancel := context.WithCancel(context.Background())
	w := &Engine{Comments: comments, Identity: "SELF", ownershipWait: func(_ context.Context) bool {
		cancel()
		return false
	}}
	claimed, err := w.negotiateOwnership(ctx, ownershipTarget{Repo: "o/r", Number: 1})
	if claimed {
		t.Fatalf("should not claim when the wait is interrupted")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestNegotiateOwnershipNoopWithoutCommentClient(t *testing.T) {
	w := &Engine{}
	claimed, err := w.negotiateOwnership(context.Background(), ownershipTarget{Repo: "o/r", Number: 1})
	if err != nil || !claimed {
		t.Fatalf("expected an instance without comment support to claim immediately, got claimed=%v err=%v", claimed, err)
	}
}

func TestOwnershipTargetForPrefersOpenPullRequest(t *testing.T) {
	checker := &fakeClosureSource{state: OriginatingWorkState{
		IssueState: "open",
		PullRequests: []PullRequestWorkState{
			{Number: 9, State: "closed", Merged: false},
			{Number: 10, State: "open", Merged: false},
		},
	}}
	target := ownershipTargetFor(context.Background(), checker, Issue{Number: 1, Repository: "o/r", Target: "o/r"})
	if !target.Continue || target.Number != 10 {
		t.Fatalf("target = %+v, want continue on PR #10", target)
	}
}

func TestOwnershipTargetForAdoptsCrossRepoPullRequest(t *testing.T) {
	checker := &fakeClosureSource{state: OriginatingWorkState{
		IssueState:   "open",
		PullRequests: []PullRequestWorkState{{Repository: "fork/r", Number: 4, State: "open"}},
	}}
	target := ownershipTargetFor(context.Background(), checker, Issue{Number: 1, Repository: "o/r", Target: "o/r"})
	if !target.Continue || target.Repo != "fork/r" || target.Number != 4 {
		t.Fatalf("target = %+v, want continue on fork/r#4", target)
	}
}

func TestOwnershipTargetForFallsBackToIssueWithoutOpenPullRequest(t *testing.T) {
	checker := &fakeClosureSource{state: OriginatingWorkState{IssueState: "open"}}
	target := ownershipTargetFor(context.Background(), checker, Issue{Number: 3, Repository: "o/r", Target: "o/r"})
	if target.Continue || target.Number != 3 {
		t.Fatalf("target = %+v, want issue #3 without continue", target)
	}
}

// settleNegotiation runs a reap, waits for the background handshake it hands
// off, then runs the reap again the way the next poll would. The grace window
// no longer runs on the poll loop, so an issue whose handshake is
// won is dispatched by the following poll rather than by the reap that started
// it. pending is a constructor because the reap filters its batch in place.
func settleNegotiation(w *Engine, checker WorkClosureChecker, pending func() []pendingIssue, seen map[string]bool, aggressive bool) []pendingIssue {
	w.negotiateContestedIssues(context.Background(), checker, pending(), seen, aggressive)
	w.awaitNegotiations()
	return w.negotiateContestedIssues(context.Background(), checker, pending(), seen, false)
}

func TestNegotiateContestedIssuesFiltersDeclinedClaims(t *testing.T) {
	comments := newFakeCommentClient()
	// Issue #1 is contested and another instance answers; issue #2 is
	// uncontested so it passes straight through without negotiation.
	comments.inject("o/r", 1, Comment{})
	w := &Engine{Comments: comments, Identity: "SELF", Out: io.Discard, ownershipWait: func(context.Context) bool {
		comments.inject("o/r", 1, Comment{Body: signComment(presenceClaimBody, "OTHER"), CreatedAt: time.Now()})
		return true
	}}
	pending := []pendingIssue{
		{issue: Issue{Number: 1, Repository: "o/r", Target: "o/r"}, contested: true},
		{issue: Issue{Number: 2, Repository: "o/r", Target: "o/r"}},
	}
	declinedKey := issueKey(pending[0].issue)
	seen := map[string]bool{declinedKey: true, issueKey(pending[1].issue): true}
	result := w.negotiateContestedIssues(context.Background(), nil, pending, seen, true)
	w.awaitNegotiations()
	if len(result) != 1 || result[0].issue.Number != 2 {
		t.Fatalf("result = %+v, want only issue #2 to survive", result)
	}
	if !seen[declinedKey] {
		t.Fatalf("declined issue #1 should stay marked as seen so the next poll renegotiates instead of dispatching it")
	}
}

func TestNegotiateContestedIssuesSkipsResumedSessions(t *testing.T) {
	comments := newFakeCommentClient()
	w := &Engine{Comments: comments, Identity: "SELF"}
	pending := []pendingIssue{
		{issue: Issue{Number: 1, Repository: "o/r", Target: "o/r"}, contested: true, session: AgentSession{Resume: true}},
	}
	result := w.negotiateContestedIssues(context.Background(), nil, pending, map[string]bool{}, true)
	if len(result) != 1 {
		t.Fatalf("resumed local sessions should skip negotiation entirely, got %+v", result)
	}
	if posted, _ := comments.ListComments(context.Background(), "o/r", 1); len(posted) != 0 {
		t.Fatalf("expected no comments posted for a resumed session, got %v", posted)
	}
}

func TestLatestClaimByOtherPicksNewestForeignClaim(t *testing.T) {
	now := time.Now()
	comments := []Comment{
		{Body: signComment(startingClaimBody, "OTHER"), CreatedAt: now.Add(-3 * time.Hour)},
		{Body: signComment(askClaimBody, "OTHER"), CreatedAt: now.Add(-time.Minute)},
		{Body: signComment(continuingClaimBody, "OTHER"), CreatedAt: now.Add(-time.Hour)},
		{Body: signComment(startingClaimBody, "SELF"), CreatedAt: now},
		{Body: "unrelated chatter", CreatedAt: now},
	}
	at, _, owner, ok := latestClaimByOther(comments, "SELF")
	if !ok || !at.Equal(now.Add(-time.Hour)) || owner != "OTHER" {
		t.Fatalf("latestClaimByOther = (%v, %q, %v), want the 1h-old continuing claim from OTHER", at, owner, ok)
	}
	if _, _, _, ok := latestClaimByOther(comments[3:], "SELF"); ok {
		t.Fatalf("own claims and non-protocol comments should not count as a foreign claim")
	}
}

func TestClaimStandingHonoursStaleClaimAge(t *testing.T) {
	comments := newFakeCommentClient()
	w := &Engine{Comments: comments, Identity: "SELF", Out: io.Discard}
	target := ownershipTarget{Repo: "o/r", Number: 1}

	if standing, err := w.claimStanding(context.Background(), target); err != nil || standing.OwnerFresh || standing.OwnerClaimed || standing.SelfHolds {
		t.Fatalf("unclaimed work should never look claimed, got %+v err=%v", standing, err)
	}
	comments.inject("o/r", 1, Comment{Body: signComment(startingClaimBody, "OTHER"), CreatedAt: time.Now().Add(-4 * time.Minute)})
	if standing, err := w.claimStanding(context.Background(), target); err != nil || !standing.OwnerFresh || standing.Owner != "OTHER" || standing.OwnerAge < 4*time.Minute {
		t.Fatalf("a 4m-old claim is younger than the 5m staleness window, got %+v err=%v", standing, err)
	}
	w.staleClaim = 3 * time.Minute
	if standing, err := w.claimStanding(context.Background(), target); err != nil || standing.OwnerFresh || standing.Owner != "OTHER" {
		t.Fatalf("claims older than the staleness window should be reapable, got %+v err=%v", standing, err)
	}
}

func TestClaimStandingReportsThisInstanceAsHolderOfItsOwnNewestClaim(t *testing.T) {
	comments := newFakeCommentClient()
	w := &Engine{Comments: comments, Identity: "SELF", Out: io.Discard}
	target := ownershipTarget{Repo: "o/r", Number: 1}

	comments.inject("o/r", 1, Comment{Body: signComment(askClaimBody, "SELF"), CreatedAt: time.Now().Add(-7 * time.Minute)})
	comments.inject("o/r", 1, Comment{Body: signComment(startingClaimBody, "SELF"), CreatedAt: time.Now().Add(-4 * time.Minute)})
	standing, err := w.claimStanding(context.Background(), target)
	if err != nil || !standing.SelfHolds || standing.OwnerClaimed || standing.SelfAge < 4*time.Minute {
		t.Fatalf("this instance's own recent claim should be reported as held by it, got %+v err=%v", standing, err)
	}

	// A foreign claim posted after this instance's own one wins: the last
	// claim always takes the work.
	comments.inject("o/r", 1, Comment{Body: signComment(continuingClaimBody, "OTHER"), CreatedAt: time.Now().Add(-time.Minute)})
	if standing, err := w.claimStanding(context.Background(), target); err != nil || standing.SelfHolds || !standing.OwnerFresh || standing.Owner != "OTHER" {
		t.Fatalf("a newer foreign claim should take ownership away, got %+v err=%v", standing, err)
	}

	// So does age: a claim of this instance's own older than the staleness
	// window no longer holds the work.
	stale := newFakeCommentClient()
	stale.inject("o/r", 1, Comment{Body: signComment(startingClaimBody, "SELF"), CreatedAt: time.Now().Add(-3 * time.Hour)})
	w.Comments = stale
	if standing, err := w.claimStanding(context.Background(), target); err != nil || standing.SelfHolds {
		t.Fatalf("an expired claim of this instance's own should not hold the work, got %+v err=%v", standing, err)
	}
}

// A reap that finds this instance's own claim standing on the target must
// dispatch it without re-running the handshake. Before issue #425 the reap
// only looked at foreign claims, so it re-asked "Does anyone have this?" and
// re-claimed the same ticket on every pass.
func TestNegotiateContestedIssuesDoesNotReAskWorkThisInstanceAlreadyClaimed(t *testing.T) {
	comments := newFakeCommentClient()
	comments.inject("o/r", 1, Comment{Body: signComment(askClaimBody, "SELF"), CreatedAt: time.Now().Add(-7 * time.Minute)})
	comments.inject("o/r", 1, Comment{Body: signComment(startingClaimBody, "SELF"), CreatedAt: time.Now().Add(-4 * time.Minute)})
	w := &Engine{Comments: comments, Identity: "SELF", Out: io.Discard, ownershipWait: func(context.Context) bool { return true }}
	pending := []pendingIssue{{issue: Issue{Number: 1, Repository: "o/r", Target: "o/r"}, contested: true}}
	seen := map[string]bool{issueKey(pending[0].issue): true}

	result := w.negotiateContestedIssues(context.Background(), nil, pending, seen, false)
	if len(result) != 1 || result[0].issue.Number != 1 {
		t.Fatalf("work this instance already claimed should stay in the batch, got %+v", result)
	}
	posted, err := comments.ListComments(context.Background(), "o/r", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(posted) != 2 {
		t.Fatalf("expected no new handoff comments, got %d: %v", len(posted), posted)
	}
}

func TestNegotiateContestedIssuesSkipsFreshlyClaimedWorkWhenNotAggressive(t *testing.T) {
	comments := newFakeCommentClient()
	comments.inject("o/r", 1, Comment{Body: signComment(startingClaimBody, "OTHER"), CreatedAt: time.Now().Add(-time.Minute)})
	comments.inject("o/r", 2, Comment{Body: signComment(startingClaimBody, "OTHER"), CreatedAt: time.Now().Add(-3 * time.Hour)})
	w := &Engine{Comments: comments, Identity: "SELF", Out: io.Discard, ownershipWait: func(context.Context) bool { return true }}
	pending := []pendingIssue{
		{issue: Issue{Number: 1, Repository: "o/r", Target: "o/r"}, contested: true},
		{issue: Issue{Number: 2, Repository: "o/r", Target: "o/r"}, contested: true},
	}
	seen := map[string]bool{issueKey(pending[0].issue): true, issueKey(pending[1].issue): true}
	result := settleNegotiation(w, nil, func() []pendingIssue {
		return []pendingIssue{
			{issue: Issue{Number: 1, Repository: "o/r", Target: "o/r"}, contested: true},
			{issue: Issue{Number: 2, Repository: "o/r", Target: "o/r"}, contested: true},
		}
	}, seen, false)
	if len(result) != 1 || result[0].issue.Number != 2 {
		t.Fatalf("result = %+v, want only the staled issue #2 to be reaped", result)
	}
	posted, _ := comments.ListComments(context.Background(), "o/r", 1)
	if len(posted) != 1 {
		t.Fatalf("a periodic reap must not comment on freshly claimed work, got %v", posted)
	}
	if !seen[issueKey(pending[0].issue)] {
		t.Fatalf("skipped issue #1 should stay marked as seen")
	}
}

// The first reap after startup used to ask about every contested candidate
// regardless of claim age. The ask only reads answers posted after it, and a
// live instance that announced itself minutes ago does not re-announce during
// the grace window, so nothing answered and the freshly started instance
// claimed and dispatched work that was actively running.
func TestNegotiateContestedIssuesAggressiveStandsDownForFreshClaims(t *testing.T) {
	comments := newFakeCommentClient()
	comments.inject("o/r", 1, Comment{Body: signComment(startingClaimBody, "OTHER"), CreatedAt: time.Now().Add(-time.Minute)})
	var logs bytes.Buffer
	w := &Engine{Comments: comments, Identity: "SELF", Out: &logs, ownershipWait: func(context.Context) bool { return true }}
	pending := func() []pendingIssue {
		return []pendingIssue{{issue: Issue{Number: 1, Repository: "o/r", Target: "o/r"}, contested: true}}
	}
	seen := map[string]bool{issueKey(pending()[0].issue): true}
	if result := settleNegotiation(w, nil, pending, seen, true); len(result) != 0 {
		t.Fatalf("a startup reap must not dispatch work another instance claimed a minute ago, got %+v", result)
	}
	posted, _ := comments.ListComments(context.Background(), "o/r", 1)
	if len(posted) != 1 {
		t.Fatalf("a startup reap must not ask about or claim freshly claimed work, got %v", posted)
	}
	if !seen[issueKey(pending()[0].issue)] {
		t.Fatalf("the issue stood down for should stay marked as seen so a later poll renegotiates it")
	}
	requireLogged(t, logs.String(), "issue #1 claimed by instance OTHER")
}

// The reason the first reap is aggressive at all is that it runs before the
// periodic timer: work nobody holds a live claim on must still be picked up
// on startup rather than waiting for the next reap.
func TestNegotiateContestedIssuesAggressiveStillReapsStaleAndUnclaimedWork(t *testing.T) {
	comments := newFakeCommentClient()
	comments.inject("o/r", 1, Comment{Body: signComment(startingClaimBody, "OTHER"), CreatedAt: time.Now().Add(-3 * time.Hour)})
	w := &Engine{Comments: comments, Identity: "SELF", Out: io.Discard, ownershipWait: func(context.Context) bool { return true }}
	pending := func() []pendingIssue {
		return []pendingIssue{
			{issue: Issue{Number: 1, Repository: "o/r", Target: "o/r"}, contested: true},
			{issue: Issue{Number: 2, Repository: "o/r", Target: "o/r"}, contested: true},
		}
	}
	result := settleNegotiation(w, nil, pending, map[string]bool{}, true)
	if len(result) != 2 {
		t.Fatalf("a stale claim and an unclaimed issue should both be reaped on startup, got %+v", result)
	}
	if posted, _ := comments.ListComments(context.Background(), "o/r", 1); len(posted) != 3 {
		t.Fatalf("expected the stale claim plus this instance's ask and claim, got %v", posted)
	}
	if posted, _ := comments.ListComments(context.Background(), "o/r", 2); len(posted) != 2 {
		t.Fatalf("expected this instance's ask and claim on unclaimed work, got %v", posted)
	}
}

// A comment read that fails on the first reap must not strand genuinely
// abandoned work until the next one: the handshake it falls back to is itself
// an ask a live instance can answer.
func TestNegotiateContestedIssuesAggressiveNegotiatesWhenTheClaimReadFails(t *testing.T) {
	comments := newFakeCommentClient()
	comments.listErr = errors.New("boom")
	var logs bytes.Buffer
	w := &Engine{Comments: comments, Identity: "SELF", Out: &logs, ownershipWait: func(context.Context) bool { return true }}
	pending := []pendingIssue{{issue: Issue{Number: 1, Repository: "o/r", Target: "o/r"}, contested: true}}
	w.negotiateContestedIssues(context.Background(), nil, pending, map[string]bool{}, true)
	w.awaitNegotiations()
	requireLogged(t, logs.String(),
		"issue #1 reap check failed; negotiating anyway on the first reap after startup",
		"issue #1 negotiating ownership in the background",
	)
}

func TestReapPollTickOnlyWhenPollingIsSlower(t *testing.T) {
	fast := &Engine{Interval: time.Minute}
	if tick := fast.reapPollTick(); tick != 0 {
		t.Fatalf("tick = %v, want 0 when polling is already faster than the reap interval", tick)
	}
	push := &Engine{Interval: time.Minute, UseWebhooks: true}
	if tick := push.reapPollTick(); tick != reapPollInterval {
		t.Fatalf("tick = %v, want %v in webhook push mode", tick, reapPollInterval)
	}
	slow := &Engine{Interval: time.Hour}
	if tick := slow.reapPollTick(); tick != reapPollInterval {
		t.Fatalf("tick = %v, want %v for slow polling", tick, reapPollInterval)
	}
}

// requireLogged fails the test unless every fragment appears in the captured
// log output, quoting the whole log so a mismatch is diagnosable.
func requireLogged(t *testing.T, logs string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(logs, fragment) {
			t.Fatalf("log is missing %q; got:\n%s", fragment, logs)
		}
	}
}

func TestNegotiateContestedIssuesLogsReapAskAndPickup(t *testing.T) {
	comments := newFakeCommentClient()
	var logs bytes.Buffer
	w := &Engine{Comments: comments, Identity: "SELF", Out: &logs, ownershipWait: func(context.Context) bool { return true }}
	pending := func() []pendingIssue {
		return []pendingIssue{{issue: Issue{Number: 7, Repository: "o/r", Target: "o/r"}, contested: true}}
	}
	if result := settleNegotiation(w, nil, pending, map[string]bool{}, true); len(result) != 1 {
		t.Fatalf("a won handshake should be dispatched by the following poll, got %+v", result)
	}
	requireLogged(t, logs.String(),
		"reaping 1 contested issue(s) as SELF (first reap after startup",
		"issue #7 looks claimed: it reappeared with no local record",
		"issue #7 negotiating ownership in the background; polling continues while it waits",
		"issue o/r#7 asking \"Does anyone have this?\" as SELF",
		"issue o/r#7 unanswered",
		"issue #7 picked up after handoff; dispatching on the next poll",
	)
}

func TestNegotiateContestedIssuesLogsStandDownWithClaimingIdentity(t *testing.T) {
	comments := newFakeCommentClient()
	var logs bytes.Buffer
	w := &Engine{Comments: comments, Identity: "SELF", Out: &logs}
	w.ownershipWait = func(context.Context) bool {
		comments.inject("o/r", 8, Comment{Body: signComment(presenceClaimBody, "OTHER"), CreatedAt: time.Now()})
		return true
	}
	pending := []pendingIssue{{issue: Issue{Number: 8, Repository: "o/r", Target: "o/r"}, contested: true}}
	if result := w.negotiateContestedIssues(context.Background(), nil, pending, map[string]bool{}, true); len(result) != 0 {
		t.Fatalf("an answered ask must drop the issue, got %+v", result)
	}
	w.awaitNegotiations()
	requireLogged(t, logs.String(),
		"issue o/r#8 answered by instance OTHER during the handoff window; letting it go",
		"issue #8 ownership claimed by another instance; standing down",
	)
}

func TestNegotiateContestedIssuesLogsStaleAndFreshClaimAges(t *testing.T) {
	comments := newFakeCommentClient()
	comments.inject("o/r", 1, Comment{Body: signComment(startingClaimBody, "OTHER"), CreatedAt: time.Now().Add(-time.Minute)})
	comments.inject("o/r", 2, Comment{Body: signComment(startingClaimBody, "OTHER"), CreatedAt: time.Now().Add(-3 * time.Hour)})
	var logs bytes.Buffer
	w := &Engine{Comments: comments, Identity: "SELF", Out: &logs, ownershipWait: func(context.Context) bool { return true }}
	pending := []pendingIssue{
		{issue: Issue{Number: 1, Repository: "o/r", Target: "o/r"}, contested: true},
		{issue: Issue{Number: 2, Repository: "o/r", Target: "o/r"}, contested: true},
	}
	seen := map[string]bool{}
	if result := settleNegotiation(w, nil, func() []pendingIssue { return pending }, seen, false); len(result) != 1 {
		t.Fatalf("only the stale issue should be reaped, got %+v", result)
	}
	requireLogged(t, logs.String(),
		"periodic reap, skipping anything claimed within 5m0s",
		"issue #1 claimed by instance OTHER 1m0s ago (within 5m0s); skipping reap",
		"issue #2 last claimed by instance OTHER 3h0m0s ago (older than 5m0s); treating it as abandoned",
	)
}

func TestCommenterAllowed(t *testing.T) {
	if !commenterAllowed("anyone", nil) {
		t.Fatal("an empty allow list should place no restriction on the author")
	}
	if commenterAllowed("", []string{"lsegal"}) {
		t.Fatal("an unknown (empty) login should not be allowed once a list is configured")
	}
	if !commenterAllowed("LSegal", []string{"lsegal"}) {
		t.Fatal("login comparison should be case-insensitive")
	}
	if commenterAllowed("someone-else", []string{"lsegal", "other"}) {
		t.Fatal("a login outside the allow list should not be allowed")
	}
}

func TestAuthorizedDirectMentionRequiresLastCommentMentionAndAllowedAuthor(t *testing.T) {
	ctx := context.Background()
	comments := newFakeCommentClient()
	comments.inject("o/r", 1, Comment{Body: "Please retry @/aviary:SELF", Author: "lsegal", CreatedAt: time.Now()})

	authorized, err := authorizedDirectMention(ctx, comments, "o/r", 1, func(body string) bool { return mentionedIdentity(body, "SELF") }, nil)
	if err != nil {
		t.Fatalf("authorizedDirectMention: %v", err)
	}
	if !authorized {
		t.Fatal("expected the mention to authorize when it is the last comment and no allow list is set")
	}

	authorized, err = authorizedDirectMention(ctx, comments, "o/r", 1, func(body string) bool { return mentionedIdentity(body, "SELF") }, []string{"someone-else"})
	if err != nil {
		t.Fatalf("authorizedDirectMention: %v", err)
	}
	if authorized {
		t.Fatal("expected the mention to be rejected: author is not in the allow list")
	}

	comments.inject("o/r", 1, Comment{Body: "unrelated follow-up", Author: "lsegal", CreatedAt: time.Now()})
	authorized, err = authorizedDirectMention(ctx, comments, "o/r", 1, func(body string) bool { return mentionedIdentity(body, "SELF") }, nil)
	if err != nil {
		t.Fatalf("authorizedDirectMention: %v", err)
	}
	if authorized {
		t.Fatal("expected the mention to be rejected: it is no longer the last comment")
	}
}

func TestNegotiateContestedIssuesLogsProjectItemReasonAndPullRequestTarget(t *testing.T) {
	comments := newFakeCommentClient()
	var logs bytes.Buffer
	w := &Engine{Comments: comments, Identity: "SELF", Out: &logs, ownershipWait: func(context.Context) bool { return true }}
	checker := &fakeClosureSource{state: OriginatingWorkState{PullRequests: []PullRequestWorkState{{Number: 42, State: "open"}}}}
	issue := Issue{Number: 9, Repository: "o/r", Target: "https://github.com/users/o/projects/1", ProjectStatus: "In Progress"}
	pending := func() []pendingIssue { return []pendingIssue{{issue: issue, contested: true}} }
	if result := settleNegotiation(w, checker, pending, map[string]bool{}, true); len(result) != 1 {
		t.Fatalf("the stranded project item should be reclaimed, got %+v", result)
	}
	requireLogged(t, logs.String(),
		"issue #9 looks claimed: it sits at In Progress with no local record",
		"negotiating on pull request o/r#42",
		"pull request o/r#42 asking \"Does anyone have this?\"",
	)
}

func TestActiveIssuesForRepo(t *testing.T) {
	active := map[string]string{
		"o/r#7":     "session-7",
		"o/r#3":     "session-3",
		"o/other#9": "session-9",
		"https://github.com/users/loren/projects/3#11": "session-11",
		"o/r#discussion#4": "discussion",
		"malformed":        "junk",
	}
	if got := activeIssuesForRepo(active, "o/r"); !slices.Equal(got, []int{3, 7}) {
		t.Fatalf("activeIssuesForRepo(o/r) = %v, want [3 7]", got)
	}
	if got := activeIssuesForRepo(active, "o/other"); !slices.Equal(got, []int{9}) {
		t.Fatalf("activeIssuesForRepo(o/other) = %v, want [9]", got)
	}
	// A project-scoped key names the project, not the repository, so it only
	// matches once the key is resolved back to the repository it belongs to.
	if got := activeIssuesForRepo(active, "https://github.com/users/loren/projects/3"); !slices.Equal(got, []int{11}) {
		t.Fatalf("activeIssuesForRepo(project) = %v, want [11]", got)
	}
	if got := activeIssuesForRepo(active, "o/none"); len(got) != 0 {
		t.Fatalf("activeIssuesForRepo(o/none) = %v, want none", got)
	}
}

func TestParseIssueWorkKey(t *testing.T) {
	cases := []struct {
		key    string
		target string
		number int
		ok     bool
	}{
		{"o/r#7", "o/r", 7, true},
		{"https://github.com/users/loren/projects/3#11", "https://github.com/users/loren/projects/3", 11, true},
		{"o/r#discussion#4", "", 0, false},
		{"o/r#0", "", 0, false},
		{"o/r#abc", "", 0, false},
		{"o/r", "", 0, false},
	}
	for _, tc := range cases {
		target, number, ok := parseIssueWorkKey(tc.key)
		if ok != tc.ok || target != tc.target || number != tc.number {
			t.Fatalf("parseIssueWorkKey(%q) = (%q, %d, %v), want (%q, %d, %v)", tc.key, target, number, ok, tc.target, tc.number, tc.ok)
		}
	}
}

func TestParseDiscussionWorkKey(t *testing.T) {
	cases := []struct {
		key    string
		target string
		number int
		ok     bool
	}{
		{"o/r#discussion#4", "o/r", 4, true},
		{"https://github.com/o/r/discussions#discussion#12", "https://github.com/o/r/discussions", 12, true},
		{"o/r#7", "", 0, false},
		{"o/r#discussion#0", "", 0, false},
		{"o/r#discussion#abc", "", 0, false},
		{"o/r", "", 0, false},
	}
	for _, tc := range cases {
		target, number, ok := parseDiscussionWorkKey(tc.key)
		if ok != tc.ok || target != tc.target || number != tc.number {
			t.Fatalf("parseDiscussionWorkKey(%q) = (%q, %d, %v), want (%q, %d, %v)", tc.key, target, number, ok, tc.target, tc.number, tc.ok)
		}
		if tc.ok && discussionWorkKey(tc.target, tc.number) != tc.key {
			t.Fatalf("discussionWorkKey(%q, %d) = %q, want %q", tc.target, tc.number, discussionWorkKey(tc.target, tc.number), tc.key)
		}
	}
}

// The reap's other guards all read the ticket's own comments. When that read
// comes back empty -- the spam of issue #432, where an instance re-opened the
// same negotiation every poll -- the instance's own record of the handshake it
// already ran must still stop it from asking again.
func TestNegotiateContestedIssuesDoesNotReAskAfterItsOwnHandshake(t *testing.T) {
	comments := newFakeCommentClient()
	w := &Engine{Comments: comments, Identity: "SELF", Out: io.Discard, ownershipWait: func(context.Context) bool { return true }}
	pending := func() []pendingIssue {
		return []pendingIssue{{issue: Issue{Number: 1, Repository: "o/r", Target: "o/r"}, contested: true}}
	}
	seen := map[string]bool{issueKey(pending()[0].issue): true}

	if result := settleNegotiation(w, nil, pending, seen, false); len(result) != 1 {
		t.Fatalf("the first handshake should claim the issue, got %+v", result)
	}
	posted, err := comments.ListComments(context.Background(), "o/r", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(posted) != 2 {
		t.Fatalf("expected an ask and a starting claim, got %v", posted)
	}

	// Forget everything the ticket says about who owns it. Only the local
	// record is left, and it alone has to hold the handshake shut.
	comments.reset("o/r", 1)
	for pass := 0; pass < 3; pass++ {
		result := w.negotiateContestedIssues(context.Background(), nil, pending(), seen, false)
		if len(result) != 1 || result[0].issue.Number != 1 {
			t.Fatalf("pass %d: settled work should stay in the batch, got %+v", pass, result)
		}
		posted, err := comments.ListComments(context.Background(), "o/r", 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(posted) != 0 {
			t.Fatalf("pass %d: a settled handshake must not comment again, got %v", pass, posted)
		}
	}
}

// Standing down for another instance is just as settled as winning: the reap
// must not re-ask, and must not quietly pick the work up either.
func TestNegotiateContestedIssuesRemembersStandingDown(t *testing.T) {
	comments := newFakeCommentClient()
	w := &Engine{Comments: comments, Identity: "SELF", Out: io.Discard}
	w.ownershipWait = func(context.Context) bool {
		comments.inject("o/r", 3, Comment{Body: signComment(presenceClaimBody, "OTHER"), CreatedAt: time.Now()})
		return true
	}
	pending := func() []pendingIssue {
		return []pendingIssue{{issue: Issue{Number: 3, Repository: "o/r", Target: "o/r"}, contested: true}}
	}
	seen := map[string]bool{}
	if result := w.negotiateContestedIssues(context.Background(), nil, pending(), seen, false); len(result) != 0 {
		t.Fatalf("an answered ask must drop the issue, got %+v", result)
	}
	w.awaitNegotiations()
	comments.reset("o/r", 3)
	if result := w.negotiateContestedIssues(context.Background(), nil, pending(), seen, false); len(result) != 0 {
		t.Fatalf("work this instance stood down for must stay dropped, got %+v", result)
	}
	if posted, _ := comments.ListComments(context.Background(), "o/r", 3); len(posted) != 0 {
		t.Fatalf("a settled stand-down must not re-ask, got %v", posted)
	}
}

// A record older than the staleness window is not evidence of anything: work
// that really was abandoned has to be renegotiable.
func TestSettledHandshakeExpiresWithTheStalenessWindow(t *testing.T) {
	w := &Engine{Identity: "SELF", Out: io.Discard, staleClaim: time.Hour}
	target := ownershipTarget{Repo: "o/r", Number: 5}
	w.recordHandshake(target, true)
	if record, age, ok := w.settledHandshake(target); !ok || !record.Claimed || age > time.Minute {
		t.Fatalf("a fresh record should hold the work, got %+v age=%v ok=%v", record, age, ok)
	}
	w.handshakeMu.Lock()
	w.handshakes[handshakeKey(target)] = handshakeRecord{At: time.Now().Add(-2 * time.Hour), Claimed: true}
	w.handshakeMu.Unlock()
	if _, _, ok := w.settledHandshake(target); ok {
		t.Fatalf("an expired record should not hold the work")
	}
	// The negotiation on a pull request opened for the issue is a different
	// negotiation, so the issue's record must not settle it.
	w.recordHandshake(target, true)
	if _, _, ok := w.settledHandshake(ownershipTarget{Repo: "o/r", Number: 6, Continue: true}); ok {
		t.Fatalf("a record for one target must not settle another")
	}
}

// syncBuffer is a bytes.Buffer that a goroutine writing log output and the
// test reading that output can share. Tests that pass it as Engine.Out while
// Run works in the background need it to stay race-free.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitForComments blocks until a ticket carries at least want comments, so a
// test does not race a handshake running in the background.
func waitForComments(t *testing.T, comments CommentClient, repo string, number, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		posted, err := comments.ListComments(context.Background(), repo, number)
		if err != nil {
			t.Fatal(err)
		}
		if len(posted) >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d comment(s) on %s#%d, got %v", want, repo, number, posted)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// The handshake's grace window lasts minutes. Running it on the poll loop
// stopped the whole watch for that long -- no repository re-read, no issue
// discovered, nothing dispatched -- which is what issue #437 reported as
// browser mode not refreshing. The reap must hand the wait off and return.
func TestNegotiateContestedIssuesDoesNotBlockThePollOnTheGraceWindow(t *testing.T) {
	comments := newFakeCommentClient()
	release := make(chan struct{})
	logs := &syncBuffer{}
	w := &Engine{Comments: comments, Identity: "SELF", Out: logs, ownershipWait: func(ctx context.Context) bool {
		select {
		case <-release:
			return true
		case <-ctx.Done():
			return false
		}
	}}
	pending := func() []pendingIssue {
		return []pendingIssue{{issue: Issue{Number: 5, Repository: "o/r", Target: "o/r"}, contested: true}}
	}
	seen := map[string]bool{}

	// The reap returns while the handshake is still inside its grace window.
	done := make(chan []pendingIssue, 1)
	go func() { done <- w.negotiateContestedIssues(context.Background(), nil, pending(), seen, true) }()
	select {
	case result := <-done:
		if len(result) != 0 {
			t.Fatalf("an unsettled handshake must not dispatch yet, got %+v", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the reap blocked on the handshake's grace window instead of handing it off")
	}
	if !seen[issueKey(pending()[0].issue)] {
		t.Fatal("an issue being negotiated must stay marked as seen so the next poll renegotiates it")
	}

	// Wait for the handed-off handshake to reach its grace window, which it
	// enters only after posting the ask this test counts.
	waitForComments(t, comments, "o/r", 5, 1)

	// Polls keep running during that window, and none of them re-asks.
	for pass := 0; pass < 3; pass++ {
		if result := w.negotiateContestedIssues(context.Background(), nil, pending(), seen, false); len(result) != 0 {
			t.Fatalf("pass %d: an in-flight handshake must not dispatch, got %+v", pass, result)
		}
	}
	posted, err := comments.ListComments(context.Background(), "o/r", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(posted) != 1 {
		t.Fatalf("expected only the single ask while the handshake waits, got %v", posted)
	}
	if strings.Count(logs.String(), `asking "Does anyone have this?"`) != 1 {
		t.Fatalf("expected exactly one ask to be logged, got:\n%s", logs.String())
	}

	// Once it settles, the following poll dispatches the work it won.
	close(release)
	w.awaitNegotiations()
	result := w.negotiateContestedIssues(context.Background(), nil, pending(), seen, false)
	if len(result) != 1 || result[0].issue.Number != 5 {
		t.Fatalf("the poll after a won handshake should dispatch the issue, got %+v", result)
	}
	if posted, _ = comments.ListComments(context.Background(), "o/r", 5); len(posted) != 2 {
		t.Fatalf("expected the ask and one starting claim, got %v", posted)
	}
}

// A claim this instance withdrew must stop reading as ownership, or the reap
// that follows a released ticket would keep standing down for work nobody is
// doing.
func TestLatestClaimMatchingIgnoresWithdrawnClaims(t *testing.T) {
	claimed := time.Now().Add(-time.Minute)
	comments := []Comment{
		{Body: signComment(startingClaimBody, "OTHER"), CreatedAt: claimed},
		{Body: signComment(releaseClaimBody, "OTHER"), CreatedAt: claimed.Add(time.Second)},
	}
	if at, _, owner, ok := latestClaimByOther(comments, "SELF"); ok {
		t.Fatalf("latestClaimByOther = (%v, %q, true), want no standing claim after the release", at, owner)
	}
	// A claim posted after a release stands again: the withdrawal only
	// retires the claims that came before it.
	comments = append(comments, Comment{Body: signComment(startingClaimBody, "OTHER"), CreatedAt: claimed.Add(2 * time.Second)})
	if _, _, owner, ok := latestClaimByOther(comments, "SELF"); !ok || owner != "OTHER" {
		t.Fatalf("latestClaimByOther = (%q, %v), want OTHER to own the work again after re-claiming", owner, ok)
	}
}

func TestClaimStandingReportsNoOwnerAfterSelfRelease(t *testing.T) {
	comments := newFakeCommentClient()
	comments.inject("o/r", 7, Comment{Body: signComment(startingClaimBody, "SELF"), CreatedAt: time.Now().Add(-time.Minute)})
	comments.inject("o/r", 7, Comment{Body: signComment(releaseClaimBody, "SELF"), CreatedAt: time.Now()})
	w := &Engine{Comments: comments, Identity: "SELF", Out: io.Discard}
	standing, err := w.claimStanding(context.Background(), ownershipTarget{Repo: "o/r", Number: 7})
	if err != nil {
		t.Fatal(err)
	}
	if standing.SelfHolds || standing.OwnerClaimed {
		t.Fatalf("claimStanding = %+v, want a released ticket to look unclaimed", standing)
	}
}

// Releasing must both withdraw the public claim and drop the local handshake
// record, so the next reap renegotiates instead of reusing a decision this
// instance has just walked back.
func TestReleaseOwnershipWithdrawsClaimAndForgetsHandshake(t *testing.T) {
	comments := newFakeCommentClient()
	var logs bytes.Buffer
	w := &Engine{Comments: comments, Identity: "SELF", Out: &logs}
	target := ownershipTarget{Repo: "o/r", Number: 7}
	w.recordHandshake(target, true)
	w.releaseOwnership(context.Background(), target, "the dispatch never happened")

	posted, _ := comments.ListComments(context.Background(), "o/r", 7)
	if len(posted) != 1 {
		t.Fatalf("posted comments = %v, want a single withdrawal", posted)
	}
	if kind, id, ok := parseClaim(posted[0].Body); !ok || kind != claimReleasing || id != "SELF" {
		t.Fatalf("comment = %q, want a release signed by SELF", posted[0].Body)
	}
	if record, _, ok := w.settledHandshake(target); ok {
		t.Fatalf("settledHandshake = %+v, want the record dropped so the target is renegotiated", record)
	}
	requireLogged(t, logs.String(), "issue o/r#7 released by SELF (\"Releasing this issue\"); the dispatch never happened")
}

// A withdrawal that cannot be posted is reported, because the ticket is then
// left reading as claimed and only the log says otherwise.
func TestReleaseOwnershipLogsAFailedWithdrawal(t *testing.T) {
	comments := newFakeCommentClient()
	comments.postErr = errors.New("boom")
	var logs bytes.Buffer
	w := &Engine{Comments: comments, Identity: "SELF", Out: &logs}
	w.releaseOwnership(context.Background(), ownershipTarget{Repo: "o/r", Number: 7}, "the dispatch never happened")
	requireLogged(t, logs.String(), "issue o/r#7 claim not withdrawn after the dispatch never happened", "may still read as claimed by SELF")
}

// A withdrawal posted in the same clock tick as the claim it withdraws must
// still retire that claim. Windows's wall clock resolves to roughly 15ms, so
// the two comments routinely carry one timestamp and ordering on time alone
// let the superseded claim win.
func TestLatestClaimMatchingBreaksTimestampTiesOnCommentOrder(t *testing.T) {
	at := time.Now().Add(-time.Minute)
	comments := []Comment{
		{Body: signComment(askClaimBody, "OTHER"), CreatedAt: at},
		{Body: signComment(startingClaimBody, "OTHER"), CreatedAt: at},
		{Body: signComment(releaseClaimBody, "OTHER"), CreatedAt: at},
	}
	if claimedAt, _, owner, ok := latestClaimByOther(comments, "SELF"); ok {
		t.Fatalf("latestClaimByOther = (%v, %q, true), want the same-timestamp withdrawal to retire the claim", claimedAt, owner)
	}
	// The reverse order must stand: a re-claim posted after a withdrawal in
	// the same tick owns the work again.
	comments = append(comments, Comment{Body: signComment(startingClaimBody, "OTHER"), CreatedAt: at})
	if _, _, owner, ok := latestClaimByOther(comments, "SELF"); !ok || owner != "OTHER" {
		t.Fatalf("latestClaimByOther = (%q, %v), want OTHER to own the work after the same-timestamp re-claim", owner, ok)
	}
}

// Two instances claiming in the same clock tick are ordered by the comments
// themselves, so the last claim posted wins whichever instance signed it.
func TestClaimStandingBreaksTimestampTiesBetweenInstances(t *testing.T) {
	at := time.Now().Add(-time.Minute)
	target := ownershipTarget{Repo: "o/r", Number: 7}

	comments := newFakeCommentClient()
	comments.inject("o/r", 7, Comment{Body: signComment(startingClaimBody, "SELF"), CreatedAt: at})
	comments.inject("o/r", 7, Comment{Body: signComment(startingClaimBody, "OTHER"), CreatedAt: at})
	w := &Engine{Comments: comments, Identity: "SELF", Out: io.Discard}
	standing, err := w.claimStanding(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if standing.SelfHolds || !standing.OwnerClaimed || standing.Owner != "OTHER" {
		t.Fatalf("claimStanding = %+v, want OTHER to hold the ticket it claimed last", standing)
	}

	comments = newFakeCommentClient()
	comments.inject("o/r", 7, Comment{Body: signComment(startingClaimBody, "OTHER"), CreatedAt: at})
	comments.inject("o/r", 7, Comment{Body: signComment(startingClaimBody, "SELF"), CreatedAt: at})
	w = &Engine{Comments: comments, Identity: "SELF", Out: io.Discard}
	standing, err = w.claimStanding(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if !standing.SelfHolds {
		t.Fatalf("claimStanding = %+v, want SELF to hold the ticket it claimed last", standing)
	}
}

// A first-time pickup is decided from local records and the project board
// alone, so before issue #511 an issue another instance had just claimed on
// the ticket was dispatched as uncontested and worked twice. The claim
// standing on the ticket is now read before the pickup.
func TestStandDownForStandingClaimsUsesTheFiveMinuteCooldownThenNegotiates(t *testing.T) {
	comments := newFakeCommentClient()
	comments.inject("o/r", 1, Comment{Body: signComment(startingClaimBody, "OTHER"), CreatedAt: time.Now().Add(-time.Minute)})
	comments.inject("o/r", 2, Comment{Body: signComment(startingClaimBody, "OTHER"), CreatedAt: time.Now().Add(-7 * time.Minute)})
	w := &Engine{Comments: comments, Identity: "SELF", Out: io.Discard, ownershipWait: func(context.Context) bool { return true }}
	pending := []pendingIssue{
		{issue: Issue{Number: 1, Repository: "o/r", Target: "o/r"}},
		{issue: Issue{Number: 2, Repository: "o/r", Target: "o/r"}},
		{issue: Issue{Number: 3, Repository: "o/r", Target: "o/r"}},
	}
	seen := map[string]bool{}
	result := w.standDownForStandingClaims(context.Background(), nil, pending, seen)
	if len(result) != 2 || result[0].issue.Number != 2 || !result[0].contested || result[1].issue.Number != 3 {
		t.Fatalf("result = %+v, want the older claim (#2) routed to negotiation and the unclaimed issue (#3) to survive", result)
	}
	if !seen[issueKey(Issue{Number: 1, Repository: "o/r", Target: "o/r"})] {
		t.Fatalf("the issue stood down for must stay marked as seen so the next poll negotiates for it")
	}
	w.negotiateContestedIssues(context.Background(), nil, result, seen, false)
	w.awaitNegotiations()
	if comments.posts != 2 {
		t.Fatalf("the older claim should receive an ask and a claim, got %d comment(s)", comments.posts)
	}
}

// Standing claims decide first-time pickups only. Work this instance already
// owns, work already routed to the handshake, and an instruction addressed to
// this instance by name must all pass through untouched.
func TestStandDownForStandingClaimsSkipsWorkItDoesNotDecide(t *testing.T) {
	comments := newFakeCommentClient()
	for _, number := range []int{1, 2, 3, 4} {
		comments.inject("o/r", number, Comment{Body: signComment(startingClaimBody, "OTHER"), CreatedAt: time.Now().Add(-time.Minute)})
	}
	comments.inject("o/r", 4, Comment{Body: signComment(continuingClaimBody, "SELF"), CreatedAt: time.Now()})
	w := &Engine{Comments: comments, Identity: "SELF", Out: io.Discard}
	pending := []pendingIssue{
		{issue: Issue{Number: 1, Repository: "o/r", Target: "o/r"}, session: AgentSession{Resume: true}},
		{issue: Issue{Number: 2, Repository: "o/r", Target: "o/r"}, contested: true},
		{issue: Issue{Number: 3, Repository: "o/r", Target: "o/r"}, mentioned: true},
		{issue: Issue{Number: 4, Repository: "o/r", Target: "o/r"}},
	}
	result := w.standDownForStandingClaims(context.Background(), nil, pending, map[string]bool{})
	if len(result) != 4 {
		t.Fatalf("result = %+v, want resumed, contested, mentioned, and self-claimed work all kept", result)
	}
	if comments.lists != 1 {
		t.Fatalf("only the first-time pickup should be checked against the ticket, got %d list(s)", comments.lists)
	}
}

// An unreachable comment API must not stall dispatch: the candidate is picked
// up as it would have been before the check existed.
func TestStandDownForStandingClaimsKeepsCandidatesWhenTheReadFails(t *testing.T) {
	comments := newFakeCommentClient()
	comments.listErr = errors.New("boom")
	w := &Engine{Comments: comments, Identity: "SELF", Out: io.Discard}
	pending := []pendingIssue{{issue: Issue{Number: 1, Repository: "o/r", Target: "o/r"}}}
	if result := w.standDownForStandingClaims(context.Background(), nil, pending, map[string]bool{}); len(result) != 1 {
		t.Fatalf("result = %+v, want the candidate kept when its claims cannot be read", result)
	}
}

// The claim is read on the pull request continuing an issue's work when one is
// open, since that is where the handoff protocol posts for such work.
func TestStandDownForStandingClaimsReadsThePullRequestContinuingTheWork(t *testing.T) {
	comments := newFakeCommentClient()
	comments.inject("o/r", 10, Comment{Body: signComment(continuingClaimBody, "OTHER"), CreatedAt: time.Now().Add(-time.Minute)})
	checker := &fakeClosureSource{state: OriginatingWorkState{IssueState: "open", PullRequests: []PullRequestWorkState{{Number: 10, State: "open"}}}}
	w := &Engine{Comments: comments, Identity: "SELF", Out: io.Discard}
	pending := []pendingIssue{{issue: Issue{Number: 1, Repository: "o/r", Target: "o/r"}}}
	if result := w.standDownForStandingClaims(context.Background(), checker, pending, map[string]bool{}); len(result) != 0 {
		t.Fatalf("result = %+v, want the pickup dropped for the claim standing on its open pull request", result)
	}
}

// The nudge a won handshake sends is what makes the Run loop poll again, and
// that poll is the only one before the next tick. It must find the handshake
// already finished: a poll that still sees it in flight drops the issue as
// "being negotiated", and with nothing left to nudge the loop the reclaimed
// work waits out the whole tick. Nothing here waits for the
// background goroutine to exit; the nudge itself is the signal, exactly as the
// Run loop receives it.
func TestNegotiationNudgeFollowsHandshakeCompletion(t *testing.T) {
	for attempt := 0; attempt < 200; attempt++ {
		w := &Engine{Comments: newFakeCommentClient(), Identity: "SELF", Out: io.Discard, ownershipWait: func(context.Context) bool { return true }}
		pending := func() []pendingIssue {
			return []pendingIssue{{issue: Issue{Number: 7, Repository: "o/r", Target: "o/r"}, contested: true}}
		}
		if result := w.negotiateContestedIssues(context.Background(), nil, pending(), map[string]bool{}, true); len(result) != 0 {
			t.Fatalf("a contested issue should leave the batch while it negotiates, got %+v", result)
		}
		select {
		case <-w.negotiatedNudges():
		case <-time.After(5 * time.Second):
			t.Fatal("a won handshake never nudged the poll loop")
		}
		if result := w.negotiateContestedIssues(context.Background(), nil, pending(), map[string]bool{}, false); len(result) != 1 {
			w.awaitNegotiations()
			t.Fatalf("attempt %d: the poll a won handshake nudged did not dispatch the issue, got %+v", attempt, result)
		}
		w.awaitNegotiations()
	}
}

func (f *fakeCommentClient) AddReviewCommentReaction(_ context.Context, repo string, commentID int64, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reactErr != nil {
		return f.reactErr
	}
	f.reactions = append(f.reactions, fakeReaction{Repo: repo, CommentID: commentID, Kind: ReviewComment, Content: content})
	return nil
}

func (f *fakeCommentClient) AddReviewReaction(_ context.Context, repo string, nodeID string, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reactErr != nil {
		return f.reactErr
	}
	f.reactions = append(f.reactions, fakeReaction{Repo: repo, Kind: ReviewBody, NodeID: nodeID, Content: content})
	return nil
}

func (f *fakeCommentClient) ListReviewComments(_ context.Context, repo string, number int) ([]Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	return slices.Clone(f.reviews[f.key(repo, number)]), nil
}

func (f *fakeCommentClient) injectReview(repo string, number int, comment Comment) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := f.key(repo, number)
	f.reviews[key] = append(f.reviews[key], comment)
}
