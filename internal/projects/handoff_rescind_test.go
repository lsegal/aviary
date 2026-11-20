package projects

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"
)

func handoffComments(bodies ...string) []Comment {
	now := time.Now()
	comments := make([]Comment, len(bodies))
	for i, body := range bodies {
		comments[i] = Comment{Body: body, CreatedAt: now.Add(time.Duration(i) * time.Second)}
	}
	return comments
}

func TestParseClaimRecognizesStoppingWork(t *testing.T) {
	kind, id, ok := parseClaim(signComment(stoppingClaimBody, "A1B2C3D4"))
	if !ok || kind != claimStopping || id != "A1B2C3D4" {
		t.Fatalf("parseClaim = %v, %q, %v", kind, id, ok)
	}
}

// Claims stack, so the ownership count only frees an issue once every claim
// has a matching stop (issue #420).
func TestOwnershipCountStacksClaimsAgainstStops(t *testing.T) {
	start := signComment(startingClaimBody, "A")
	stop := signComment(stoppingClaimBody, "A")
	twoClaimsOneStop := handoffComments(start, signComment(continuingClaimBody, "B"), stop)
	if count := ownershipCount(twoClaimsOneStop); count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	if ownershipRescinded(twoClaimsOneStop) {
		t.Fatal("an issue with a positive count must stay owned")
	}
	twoStops := append(twoClaimsOneStop, handoffComments(signComment(stoppingClaimBody, "B"))...)
	if count := ownershipCount(twoStops); count != 0 || !ownershipRescinded(twoStops) {
		t.Fatalf("count = %d, rescinded = %v; want a second stop to free the issue", count, ownershipRescinded(twoStops))
	}
	// Asks and presence answers are not claims, and a stop past zero still
	// leaves the issue free.
	extra := handoffComments(start, signComment(askClaimBody, "C"), signComment(presenceClaimBody, "A"), stop, stop)
	if count := ownershipCount(extra); count != -1 || !ownershipRescinded(extra) {
		t.Fatalf("count = %d, want -1 and free", count)
	}
	// A release withdraws a claim too, but without any stop message the
	// older claim rules decide, so a killed instance's claims are unchanged.
	if ownershipRescinded(handoffComments(start, signComment(releaseClaimBody, "A"))) {
		t.Fatal("a ticket with no stop message must not read as rescinded")
	}
	if ownershipRescinded(handoffComments(start)) {
		t.Fatal("a standing claim must not read as rescinded")
	}
}

// A stop that leaves the count positive does not withdraw the claim it
// follows, so the stale-claim rules still see the work as owned.
func TestClaimStandingKeepsAClaimAPartialStopLeavesStanding(t *testing.T) {
	comments := newFakeCommentClient()
	for _, body := range []string{signComment(startingClaimBody, "OTHER"), signComment(startingClaimBody, "OTHER"), signComment(stoppingClaimBody, "OTHER")} {
		comments.inject("o/r", 1, Comment{Body: body, CreatedAt: time.Now()})
	}
	w := &Engine{Comments: comments, Identity: "SELF"}
	standing, err := w.claimStanding(context.Background(), ownershipTarget{Repo: "o/r", Number: 1})
	if err != nil {
		t.Fatal(err)
	}
	if standing.Rescinded || !standing.OwnerClaimed || !standing.OwnerFresh || standing.Owner != "OTHER" {
		t.Fatalf("standing = %+v, want a fresh claim by OTHER", standing)
	}
}

func TestStandDownPicksUpRescindedWorkAndKeepsPartlyStoppedWork(t *testing.T) {
	comments := newFakeCommentClient()
	now := time.Now()
	comments.inject("o/r", 1, Comment{Body: signComment(startingClaimBody, "OTHER"), CreatedAt: now})
	comments.inject("o/r", 1, Comment{Body: signComment(stoppingClaimBody, "OTHER"), CreatedAt: now})
	comments.inject("o/r", 2, Comment{Body: signComment(startingClaimBody, "OTHER"), CreatedAt: now})
	comments.inject("o/r", 2, Comment{Body: signComment(startingClaimBody, "OTHER"), CreatedAt: now})
	comments.inject("o/r", 2, Comment{Body: signComment(stoppingClaimBody, "OTHER"), CreatedAt: now})
	w := &Engine{Comments: comments, Identity: "SELF", Out: io.Discard}
	pending := []pendingIssue{
		{issue: Issue{Number: 1, Repository: "o/r", Target: "o/r"}},
		{issue: Issue{Number: 2, Repository: "o/r", Target: "o/r"}},
	}
	seen := map[string]bool{}
	result := w.standDownForStandingClaims(context.Background(), nil, pending, seen)
	if len(result) != 1 || result[0].issue.Number != 1 || result[0].contested {
		t.Fatalf("result = %+v, want only the rescinded issue #1, uncontested", result)
	}
}

// A second instance started right after a graceful shutdown claims the
// rescinded work at once: no "Does anyone have this?" ask and no grace wait,
// even though the stopped claim is fresh.
func TestNegotiateContestedIssuesClaimsRescindedWorkWithoutAsking(t *testing.T) {
	comments := newFakeCommentClient()
	now := time.Now()
	comments.inject("o/r", 1, Comment{Body: signComment(startingClaimBody, "OTHER"), CreatedAt: now})
	comments.inject("o/r", 1, Comment{Body: signComment(stoppingClaimBody, "OTHER"), CreatedAt: now})
	w := &Engine{Comments: comments, Identity: "SELF", Out: io.Discard, ownershipWait: func(context.Context) bool {
		t.Error("rescinded work must not wait out the handshake")
		return true
	}}
	pending := []pendingIssue{{issue: Issue{Number: 1, Repository: "o/r", Target: "o/r"}, contested: true}}
	result := w.negotiateContestedIssues(context.Background(), nil, pending, map[string]bool{}, true)
	w.awaitNegotiations()
	if len(result) != 1 || result[0].claim == nil {
		t.Fatalf("result = %+v, want the rescinded issue dispatched with this instance's claim", result)
	}
	posted, _ := comments.ListComments(context.Background(), "o/r", 1)
	if len(posted) != 3 || posted[2].Body != signComment(startingClaimBody, "SELF") {
		t.Fatalf("comments = %+v, want only a starting claim by SELF added", posted)
	}
	if ownershipRescinded(posted) {
		t.Fatal("the new claim must make the issue owned again")
	}
}

func TestNegotiateContestedIssuesStandsDownWhileTheCountIsPositive(t *testing.T) {
	comments := newFakeCommentClient()
	now := time.Now()
	for _, body := range []string{signComment(startingClaimBody, "OTHER"), signComment(startingClaimBody, "OTHER"), signComment(stoppingClaimBody, "OTHER")} {
		comments.inject("o/r", 1, Comment{Body: body, CreatedAt: now})
	}
	w := &Engine{Comments: comments, Identity: "SELF", Out: io.Discard, ownershipWait: func(context.Context) bool { return true }}
	pending := func() []pendingIssue {
		return []pendingIssue{{issue: Issue{Number: 1, Repository: "o/r", Target: "o/r"}, contested: true}}
	}
	if result := settleNegotiation(w, nil, pending, map[string]bool{}, true); len(result) != 0 {
		t.Fatalf("result = %+v, want an issue with count 1 left alone", result)
	}
	if posted, _ := comments.ListComments(context.Background(), "o/r", 1); len(posted) != 3 {
		t.Fatalf("comments = %+v, want nothing posted", posted)
	}
}

// rescindOwnership posts one stop on each ticket this instance holds, and
// nothing where it holds no claim or already stopped.
func TestRescindOwnershipPostsOnlyWhereThisInstanceHoldsAClaim(t *testing.T) {
	comments := newFakeCommentClient()
	comments.inject("o/r", 1, Comment{Body: signComment(startingClaimBody, "SELF"), CreatedAt: time.Now()})
	comments.inject("o/r", 2, Comment{Body: signComment(startingClaimBody, "OTHER"), CreatedAt: time.Now()})
	comments.inject("o/r", 3, Comment{Body: signComment(startingClaimBody, "SELF"), CreatedAt: time.Now()})
	comments.inject("o/r", 3, Comment{Body: signComment(releaseClaimBody, "SELF"), CreatedAt: time.Now()})
	w := &Engine{Comments: comments, Identity: "SELF", Out: io.Discard}
	for number := 1; number <= 3; number++ {
		w.rescindOwnership(context.Background(), Issue{Number: number, Repository: "o/r", Target: "o/r"})
	}
	if comments.posts != 1 {
		t.Fatalf("posts = %d, want one stop", comments.posts)
	}
	posted, _ := comments.ListComments(context.Background(), "o/r", 1)
	if last := posted[len(posted)-1].Body; last != signComment(stoppingClaimBody, "SELF") {
		t.Fatalf("last comment = %q", last)
	}
}

// A restarted instance resumes its own sessions without a handshake, so it
// re-claims work its shutdown rescinded rather than leaving it free.
func TestReclaimRescindedReannouncesResumedWork(t *testing.T) {
	comments := newFakeCommentClient()
	comments.inject("o/r", 1, Comment{Body: signComment(startingClaimBody, "OLD"), CreatedAt: time.Now()})
	comments.inject("o/r", 1, Comment{Body: signComment(stoppingClaimBody, "OLD"), CreatedAt: time.Now()})
	comments.inject("o/r", 2, Comment{Body: signComment(startingClaimBody, "OLD"), CreatedAt: time.Now()})
	w := &Engine{Comments: comments, Identity: "NEW", Out: io.Discard}
	w.reclaimRescinded(context.Background(), nil, Issue{Number: 1, Repository: "o/r", Target: "o/r"})
	w.reclaimRescinded(context.Background(), nil, Issue{Number: 2, Repository: "o/r", Target: "o/r"})
	if comments.posts != 1 {
		t.Fatalf("posts = %d, want only the rescinded issue reclaimed", comments.posts)
	}
	posted, _ := comments.ListComments(context.Background(), "o/r", 1)
	if ownershipRescinded(posted) || posted[len(posted)-1].Body != signComment(continuingClaimBody, "NEW") {
		t.Fatalf("comments = %+v", posted)
	}
}

// The shutdown rescinds a run's ownership only after its checkpoint, and
// keeps the claim of a run whose checkpoint failed.
func TestShutdownCheckpointerRescindsAfterTheCheckpoint(t *testing.T) {
	agents := &activeAgents{}
	agents.add(Issue{Target: "o/r", Number: 1}, "")
	agents.add(Issue{Target: "o/r", Number: 2}, t.TempDir())
	var mu sync.Mutex
	var events []string
	record := func(event string) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	}
	s := newShutdownCheckpointer(agents, func(format string, args ...interface{}) {
		if len(args) >= 3 {
			record(fmt.Sprintf("checkpoint #%v %v", args[1], args[2]))
		}
	})
	s.git = func(context.Context, string, ...string) (string, error) { return "", errors.New("not a repository") }
	s.SetRescind(func(_ context.Context, issue Issue) { record(fmt.Sprintf("rescind #%d", issue.Number)) })
	s.Checkpoint()
	want := map[string]int{}
	for i, event := range events {
		want[event] = i
	}
	checkpointed, ok := want["checkpoint #1 "+checkpointNothing]
	rescinded, rescindedOK := want["rescind #1"]
	if !ok || !rescindedOK || rescinded < checkpointed {
		t.Fatalf("events = %q, want #1 rescinded after its checkpoint", events)
	}
	if _, ok := want["rescind #2"]; ok {
		t.Fatalf("events = %q, want the failed checkpoint's claim kept", events)
	}
}
