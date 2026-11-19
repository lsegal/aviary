package projects

import (
	"context"
	"io"
	"reflect"
	"testing"
	"time"
)

type stateChecker map[int]OriginatingWorkState

func (c stateChecker) OriginatingWorkState(_ context.Context, _ string, number int) (OriginatingWorkState, error) {
	return c[number], nil
}

func TestLinkedPullRequestRecorderKeepsOnlyOpenPullRequestsInOtherRepositories(t *testing.T) {
	linked := newLinkedPullRequests()
	checker := recordLinkedPullRequests(stateChecker{7: {IssueState: "open", PullRequests: []PullRequestWorkState{
		{Repository: "o/r", Number: 8, State: "open"},
		{Number: 9, State: "open"},
		{Repository: "me/r", Number: 3, State: "open"},
		{Repository: "me/r", Number: 4, State: "closed", Merged: true},
		{Repository: "other/r", Number: 5, State: "closed"},
	}}}, linked)
	if _, err := checker.OriginatingWorkState(context.Background(), "o/r", 7); err != nil {
		t.Fatal(err)
	}
	if got := linked.issues("me/r", 3); !reflect.DeepEqual(got, []string{"o/r#7"}) {
		t.Errorf("issues linked to me/r#3 = %q, want o/r#7", got)
	}
	for _, pull := range []struct {
		repo   string
		number int
	}{{"o/r", 8}, {"o/r", 9}, {"me/r", 4}, {"other/r", 5}} {
		if got := linked.issues(pull.repo, pull.number); got != nil {
			t.Errorf("issues linked to %s#%d = %q, want none", pull.repo, pull.number, got)
		}
	}
	if got := linked.repos(); !reflect.DeepEqual(got, []string{"me/r"}) {
		t.Errorf("linked repos = %q, want only me/r", got)
	}

	linked.record("o/r", 7, OriginatingWorkState{IssueState: "open", PullRequests: []PullRequestWorkState{{Repository: "me/r", Number: 3, State: "closed"}}})
	if got := linked.repos(); got != nil {
		t.Errorf("linked repos after the pull request closed = %q, want none", got)
	}
}

// linkedScan is a poll scan of o/r whose issue #7 has an open pull request,
// me/r#3, hosted in a repository aviary does not watch.
func linkedScan() (*Engine, *mentionScan, *fakeMentionSource) {
	mentions := newFakeMentionSource()
	w := &Engine{Identity: "SELF", AllowedCommenters: []string{"lsegal"}, Mentions: mentions, Out: io.Discard}
	scan := newMentionScan(time.Now().Add(-time.Hour))
	scan.linked.record("o/r", 7, OriginatingWorkState{IssueState: "open", PullRequests: []PullRequestWorkState{{Repository: "me/r", Number: 3, State: "open"}}})
	return w, scan, mentions
}

func TestScannedMentionOnLinkedPullRequestAddressesItsIssue(t *testing.T) {
	w, scan, mentions := linkedScan()
	now := time.Now()
	mentions.add("me/r", RecentComment{ID: 1, Number: 3, Body: "Please fix @/aviary:SELF", Author: "lsegal", CreatedAt: now, UpdatedAt: now})
	mentions.addReview("me/r", RecentComment{ID: 2, Kind: ReviewComment, Number: 3, Body: "And this @/aviary:SELF", Author: "lsegal", CreatedAt: now, UpdatedAt: now})
	// me/r's own issue #5 is not linked to anything, so a mention on it is
	// never acted on.
	mentions.add("me/r", RecentComment{ID: 3, Number: 5, Body: "Unrelated @/aviary:SELF", Author: "lsegal", CreatedAt: now, UpdatedAt: now})
	mentions.closing[3] = []int{3}

	directMentions := make(map[string]bool)
	w.scanDirectMentions(context.Background(), scan, []string{"o/r"}, map[string]bool{"o/r#7": true}, directMentions)
	if want := map[string]bool{"o/r#7": true}; !reflect.DeepEqual(directMentions, want) {
		t.Fatalf("direct mentions = %v, want %v", directMentions, want)
	}
}

func TestScannedReviewOnLinkedPullRequestAddressesItsIssue(t *testing.T) {
	w, scan, mentions := linkedScan()
	now := time.Now()
	mentions.addReview("me/r", RecentComment{ID: 9, Kind: ReviewBody, NodeID: "R_1", Number: 3, Body: "Changes needed @/aviary:SELF", Author: "lsegal", CreatedAt: now, UpdatedAt: now})

	directMentions := make(map[string]bool)
	w.scanDirectMentions(context.Background(), scan, []string{"o/r"}, map[string]bool{"o/r#7": true}, directMentions)
	if want := map[string]bool{"o/r#7": true}; !reflect.DeepEqual(directMentions, want) {
		t.Fatalf("direct mentions = %v, want %v", directMentions, want)
	}
}

func TestLinkedPullRequestScanSkipsRepositoriesAlreadyCovered(t *testing.T) {
	w, scan, mentions := linkedScan()
	now := time.Now()
	mentions.add("me/r", RecentComment{ID: 1, Number: 3, Body: "Please fix @/aviary:SELF", Author: "lsegal", CreatedAt: now, UpdatedAt: now})

	directMentions := make(map[string]bool)
	w.scanLinkedPullRequests(context.Background(), scan, []string{"o/r", "me/r"}, nil, directMentions)
	if mentions.scanCount() != 0 || len(directMentions) != 0 {
		t.Fatalf("scans = %d, direct mentions = %v; want a covered repository left to its own scan or webhook", mentions.scanCount(), directMentions)
	}
}

func TestMentionOnCoveredLinkedPullRequestAddressesItsIssue(t *testing.T) {
	// A pull request in another watched repository is scanned in full there,
	// but still addresses the issue it is linked to rather than the issue its
	// own closing keywords would name in that repository.
	w, scan, mentions := linkedScan()
	now := time.Now()
	mentions.add("me/r", RecentComment{ID: 1, Number: 3, Body: "Please fix @/aviary:SELF", Author: "lsegal", CreatedAt: now, UpdatedAt: now})

	directMentions := make(map[string]bool)
	w.scanDirectMentions(context.Background(), scan, []string{"me/r", "o/r"}, nil, directMentions)
	if want := map[string]bool{"o/r#7": true}; !reflect.DeepEqual(directMentions, want) {
		t.Fatalf("direct mentions = %v, want %v", directMentions, want)
	}
	if got := scan.linkedIssueKeys("me/r", 3, []string{"me/r#3"}); !reflect.DeepEqual(got, []string{"o/r#7"}) {
		t.Errorf("webhook keys for me/r#3 = %q, want o/r#7", got)
	}
	if got := scan.linkedIssueKeys("me/r", 4, []string{"me/r#1"}); !reflect.DeepEqual(got, []string{"me/r#1"}) {
		t.Errorf("webhook keys for unlinked me/r#4 = %q, want them unchanged", got)
	}
}

func TestLoginMentionOnLinkedPullRequestRechecksItsIssue(t *testing.T) {
	w, scan, mentions := linkedScan()
	w.SelfLogin = "aviarybot"
	listed := map[string]bool{"o/r#7": true}
	directMentions := make(map[string]bool)
	// The first scan seeds the cursor, so only comments posted after it are
	// new.
	w.scanDirectMentions(context.Background(), scan, []string{"o/r"}, listed, directMentions)
	now := time.Now()
	mentions.add("me/r", RecentComment{ID: 1, Number: 3, Body: "@aviarybot please look again", Author: "lsegal", CreatedAt: now, UpdatedAt: now})
	w.scanDirectMentions(context.Background(), scan, []string{"o/r"}, listed, directMentions)
	if want := map[string]bool{"o/r#7": true}; !reflect.DeepEqual(directMentions, want) {
		t.Fatalf("direct mentions = %v, want %v", directMentions, want)
	}
}
