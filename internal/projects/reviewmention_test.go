package projects

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestPolledReviewMentionDispatchesClosedIssueOnce(t *testing.T) {
	for _, test := range []struct {
		name     string
		mention  RecentComment
		reaction fakeReaction
	}{
		{
			name:     "inline review comment",
			mention:  RecentComment{ID: 42, Kind: ReviewComment, NodeID: "PRRC_1", Number: 12, Body: "@/aviary:SELF this branch is wrong", Author: "lsegal"},
			reaction: fakeReaction{Repo: "o/r", CommentID: 42, Kind: ReviewComment, Content: "eyes"},
		},
		{
			name:     "review body",
			mention:  RecentComment{ID: 42, Kind: ReviewBody, NodeID: "PRR_1", Number: 12, Body: "@/aviary:SELF please address these", Author: "lsegal"},
			reaction: fakeReaction{Repo: "o/r", Kind: ReviewBody, NodeID: "PRR_1", Content: "eyes"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner, comments, mentions, stop := startPolledMentionRun(t, []string{"lsegal"})
			defer stop()
			mentions.mu.Lock()
			mentions.closing[12] = []int{7}
			mentions.mu.Unlock()
			// A conversation comment sharing the review's ID is a different
			// comment, so handling it must not mark the review handled.
			comments.inject("o/r", 3, Comment{Body: "@/aviary:SELF unrelated", Author: "impersonator", CreatedAt: time.Now()})
			mentions.add("o/r", RecentComment{ID: 42, Number: 3, Body: "@/aviary:SELF unrelated", Author: "impersonator", UpdatedAt: time.Now()})
			waitForScans(t, mentions, 2)
			test.mention.UpdatedAt = time.Now()
			mentions.addReview("o/r", test.mention)
			select {
			case n := <-runner.dispatched:
				if n != 7 {
					t.Fatalf("review mention dispatched #%d, want the issue the pull request closes, #7", n)
				}
			case <-time.After(time.Second):
				t.Fatal("review mention was not picked up by polling")
			}
			waitForScans(t, mentions, 5)
			select {
			case n := <-runner.dispatched:
				t.Fatalf("one review mention dispatched a second run of #%d", n)
			default:
			}
			reactions := comments.reactionsSnapshot()
			if len(reactions) != 2 || reactions[1] != test.reaction {
				t.Fatalf("reactions = %#v, want the review mention reacted to with %#v", reactions, test.reaction)
			}
		})
	}
}

func TestPolledReviewMentionIsSkippedWithAReason(t *testing.T) {
	for _, test := range []struct {
		name    string
		mention RecentComment
		closing []int
		log     string
	}{
		{
			name:    "disallowed commenter",
			mention: RecentComment{ID: 70, Kind: ReviewComment, Number: 12, Body: "@/aviary:SELF go", Author: "impersonator"},
			closing: []int{7},
			log:     "o/r#12 ignoring direct mention of instance SELF in a review comment: impersonator is not an allowed commenter",
		},
		{
			name:    "pull request closing no issue",
			mention: RecentComment{ID: 71, Kind: ReviewBody, NodeID: "PRR_2", Number: 12, Body: "@/aviary:SELF go", Author: "lsegal"},
			log:     "o/r pull request #12 directly mentioned instance SELF in a review, but it closes no issue to run",
		},
		{
			name:    "another instance",
			mention: RecentComment{ID: 72, Kind: ReviewComment, Number: 12, Body: "@/aviary:OTHER go", Author: "lsegal"},
			closing: []int{7},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			src := &fakeSource{batches: [][]Issue{{{Number: 7}}}}
			comments := newFakeCommentClient()
			mentions := newFakeMentionSource()
			mentions.closing[12] = test.closing
			runner := &fakeRunner{release: make(chan struct{}), dispatched: make(chan int, 4)}
			close(runner.release)
			logs := &syncBuffer{}
			w := &Engine{
				Repo: "o/r", Interval: 5 * time.Millisecond, Concurrency: 1, StatePath: filepath.Join(t.TempDir(), "state.json"),
				Issues: src, Runner: runner, Out: logs, Comments: comments, Mentions: mentions, Identity: "SELF", AllowedCommenters: []string{"lsegal"},
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- w.Run(ctx) }()
			defer func() {
				cancel()
				<-done
			}()
			select {
			case <-runner.dispatched:
			case <-time.After(runLoopWait):
				t.Fatal("initial issue was not dispatched")
			}
			test.mention.UpdatedAt = time.Now()
			mentions.addReview("o/r", test.mention)
			waitForScans(t, mentions, 5)
			select {
			case n := <-runner.dispatched:
				t.Fatalf("%s dispatched #%d", test.name, n)
			default:
			}
			if test.log != "" && strings.Count(logs.String(), test.log) != 1 {
				t.Fatalf("logs do not say once why the mention was skipped (%q):\n%s", test.log, logs.String())
			}
		})
	}
}

// startWebhookMentionRun starts a webhook-mode run over issue #7 and waits for
// #7's first run to finish, so the test can deliver a mention.
func startWebhookMentionRun(t *testing.T) (*fakeRunner, *fakeCommentClient, chan<- WebhookEvent, *syncBuffer, func()) {
	t.Helper()
	dir := t.TempDir()
	src := &fakeSource{batches: [][]Issue{{{Number: 7}}}}
	runner := &fakeRunner{release: make(chan struct{}), dispatched: make(chan int, 2)}
	events := make(chan WebhookEvent, 1)
	comments := newFakeCommentClient()
	logs := &syncBuffer{}
	w := &Engine{
		Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: filepath.Join(dir, "state.json"),
		Issues: src, Runner: runner, UseWebhooks: true, Events: events, Out: logs,
		fallbackInterval: time.Hour, Identity: "SELF", Comments: comments, AllowedCommenters: []string{"lsegal"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	select {
	case <-runner.dispatched:
	case <-time.After(runLoopWait):
		t.Fatal("initial issue was not dispatched")
	}
	close(runner.release)
	deadline := time.Now().Add(runLoopWait)
	for {
		state, err := loadWorkState(w.StatePath)
		if err == nil && state[7].Status == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("initial run of #7 did not complete")
		}
		time.Sleep(time.Millisecond)
	}
	stop := func() {
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	return runner, comments, events, logs, stop
}

func TestWebhookReviewMentionDispatchesClosedIssue(t *testing.T) {
	for _, test := range []struct {
		name     string
		event    WebhookEvent
		reaction fakeReaction
	}{
		{
			name:     "inline review comment",
			event:    WebhookEvent{Kind: "pull_request_review_comment", Action: "created", CommentID: 61, CommentNodeID: "PRRC_1"},
			reaction: fakeReaction{Repo: "o/r", CommentID: 61, Kind: ReviewComment, Content: "eyes"},
		},
		{
			name:     "review body",
			event:    WebhookEvent{Kind: "pull_request_review", Action: "submitted", CommentID: 62, CommentNodeID: "PRR_1"},
			reaction: fakeReaction{Repo: "o/r", Kind: ReviewBody, NodeID: "PRR_1", Content: "eyes"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner, comments, events, _, stop := startWebhookMentionRun(t)
			defer stop()
			body := "@/aviary:SELF please handle the empty case"
			comments.injectReview("o/r", 12, Comment{Body: body, Author: "lsegal", CreatedAt: time.Now()})
			event := test.event
			event.Repository, event.IssueNumber, event.CommentBody, event.CommentAuthor = "o/r", 12, body, "lsegal"
			event.OnPullRequest, event.ClosesIssues = true, []int{7}
			events <- event
			select {
			case n := <-runner.dispatched:
				if n != 7 {
					t.Fatalf("review mention dispatched #%d, want #7", n)
				}
			case <-time.After(time.Second):
				t.Fatal("review mention did not dispatch the issue its pull request closes")
			}
			if reactions := comments.reactionsSnapshot(); len(reactions) != 1 || reactions[0] != test.reaction {
				t.Fatalf("reactions = %#v, want %#v", reactions, test.reaction)
			}
		})
	}
}

func TestWebhookReviewMentionIsReverifiedOnGitHub(t *testing.T) {
	const skipped = "pull request #12 ignoring direct mention of instance SELF in a review comment: not from an allowed commenter or not found on GitHub"
	for _, test := range []struct {
		name   string
		author string
		review *Comment
	}{
		{name: "disallowed commenter", author: "impersonator", review: &Comment{Body: "@/aviary:SELF go", Author: "impersonator"}},
		{name: "not on GitHub as delivered", author: "lsegal"},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner, comments, events, logs, stop := startWebhookMentionRun(t)
			defer stop()
			if test.review != nil {
				comments.injectReview("o/r", 12, *test.review)
			}
			events <- WebhookEvent{Kind: "pull_request_review_comment", Action: "created", Repository: "o/r", IssueNumber: 12, CommentID: 63, CommentBody: "@/aviary:SELF go", CommentAuthor: test.author, OnPullRequest: true, ClosesIssues: []int{7}}
			deadline := time.Now().Add(time.Second)
			for !strings.Contains(logs.String(), skipped) {
				if time.Now().After(deadline) {
					t.Fatalf("logs do not say why the mention was skipped:\n%s", logs.String())
				}
				time.Sleep(time.Millisecond)
			}
			select {
			case n := <-runner.dispatched:
				t.Fatalf("%s dispatched #%d", test.name, n)
			case <-time.After(50 * time.Millisecond):
			}
		})
	}
}

func TestDecodeWebhookEventReadsReviewMentions(t *testing.T) {
	event := decodeWebhookEvent("pull_request_review_comment", []byte(`{"action":"created","repository":{"full_name":"o/r"},"pull_request":{"number":12,"title":"Fix it","body":"Closes #7"},"comment":{"id":5,"node_id":"PRRC_5","body":"@/aviary:SELF here","user":{"login":"lsegal"}}}`))
	if event.IssueNumber != 12 || !event.OnPullRequest || !slices.Equal(event.ClosesIssues, []int{7}) || event.CommentID != 5 || event.CommentNodeID != "PRRC_5" || event.CommentBody != "@/aviary:SELF here" || event.CommentAuthor != "lsegal" {
		t.Fatalf("review comment event = %+v", event)
	}
	if kind, ok := webhookMentionKind(event); !ok || kind != ReviewComment {
		t.Fatalf("webhookMentionKind = %v, %v; want a review comment", kind, ok)
	}
	event = decodeWebhookEvent("pull_request_review", []byte(`{"action":"submitted","repository":{"full_name":"o/r"},"pull_request":{"number":12,"body":"Fixes #7 and closes #8"},"review":{"id":6,"node_id":"PRR_6","body":"@/aviary:SELF changes","state":"changes_requested","user":{"login":"lsegal"}}}`))
	if event.IssueNumber != 12 || !event.OnPullRequest || !slices.Equal(event.ClosesIssues, []int{7, 8}) || event.CommentID != 6 || event.CommentNodeID != "PRR_6" || event.CommentBody != "@/aviary:SELF changes" || event.CommentAuthor != "lsegal" {
		t.Fatalf("review event = %+v", event)
	}
	if kind, ok := webhookMentionKind(event); !ok || kind != ReviewBody {
		t.Fatalf("webhookMentionKind = %v, %v; want a review body", kind, ok)
	}
	if _, ok := webhookMentionKind(WebhookEvent{Kind: "pull_request_review", Action: "dismissed"}); ok {
		t.Fatal("a dismissed review is not a newly posted mention")
	}
	for _, kind := range []string{"pull_request_review", "pull_request_review_comment"} {
		if webhookEventNeedsRefresh(WebhookEvent{Kind: kind, Action: "created"}) {
			t.Errorf("%s delivery should not cost a refresh on its own", kind)
		}
	}
}

func TestGHCLIRecentReviewComments(t *testing.T) {
	var calls [][]string
	gh := GHCLI{runCommand: func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, args)
		switch {
		case strings.Contains(args[1], "pulls/comments?"):
			return []byte(`[{"id":42,"node_id":"PRRC_42","body":"@/aviary:SELF inline","pull_request_url":"https://api.github.com/repos/o/r/pulls/12","updated_at":"2026-10-06T07:00:00Z","user":{"login":"lsegal"}}]`), nil
		case strings.HasPrefix(args[1], "repos/o/r/pulls?"):
			return []byte(`[{"number":12,"updated_at":"2026-10-06T07:30:00Z"},{"number":11,"updated_at":"2026-10-06T05:00:00Z"}]`), nil
		case strings.HasPrefix(args[1], "repos/o/r/pulls/12/reviews"):
			return []byte(`[{"id":5,"node_id":"PRR_5","body":"old","state":"COMMENTED","submitted_at":"2026-10-06T05:00:00Z","user":{"login":"lsegal"}},` +
				`{"id":6,"node_id":"PRR_6","body":"@/aviary:SELF review","state":"CHANGES_REQUESTED","submitted_at":"2026-10-06T06:30:00Z","user":{"login":"lsegal"}},` +
				`{"id":7,"node_id":"PRR_7","body":"","state":"APPROVED","submitted_at":"2026-10-06T07:10:00Z","user":{"login":"lsegal"}},` +
				`{"id":8,"node_id":"PRR_8","body":"draft","state":"PENDING","user":{"login":"lsegal"}}]`), nil
		default:
			t.Fatalf("unexpected request %v", args)
			return nil, nil
		}
	}}
	since := time.Date(2026, 10, 6, 6, 0, 0, 0, time.UTC)
	comments, err := gh.RecentReviewComments(context.Background(), "o/r", since)
	if err != nil {
		t.Fatal(err)
	}
	want := []RecentComment{
		{ID: 6, Kind: ReviewBody, NodeID: "PRR_6", Number: 12, Body: "@/aviary:SELF review", Author: "lsegal", UpdatedAt: time.Date(2026, 10, 6, 6, 30, 0, 0, time.UTC)},
		{ID: 42, Kind: ReviewComment, NodeID: "PRRC_42", Number: 12, Body: "@/aviary:SELF inline", Author: "lsegal", UpdatedAt: time.Date(2026, 10, 6, 7, 0, 0, 0, time.UTC)},
	}
	if !slices.EqualFunc(comments, want, func(a, b RecentComment) bool {
		return a.ID == b.ID && a.Kind == b.Kind && a.NodeID == b.NodeID && a.Number == b.Number && a.Body == b.Body && a.Author == b.Author && a.UpdatedAt.Equal(b.UpdatedAt)
	}) {
		t.Fatalf("comments = %#v, want %#v", comments, want)
	}
	if !strings.Contains(calls[0][1], "since=2026-10-06T06%3A00%3A00Z") || !slices.Contains(calls[0], "--paginate") {
		t.Fatalf("review comments request = %v, want a paginated listing since the cursor", calls[0])
	}
	for _, call := range calls {
		if strings.Contains(call[1], "pulls/11/") {
			t.Fatalf("read reviews of pull request #11, which was not updated since the cursor: %v", call)
		}
	}
}

func TestGHCLIListReviewCommentsAndReviewReactions(t *testing.T) {
	var calls [][]string
	gh := GHCLI{runCommand: func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, args)
		switch {
		case args[1] == "repos/o/r/pulls/12/comments":
			return []byte(`[{"body":"@/aviary:SELF inline","created_at":"2026-10-06T07:00:00Z","user":{"login":"lsegal"}}]`), nil
		case strings.HasPrefix(args[1], "repos/o/r/pulls/12/reviews"):
			return []byte(`[{"id":6,"body":"@/aviary:SELF review","state":"COMMENTED","submitted_at":"2026-10-06T06:00:00Z","user":{"login":"lsegal"}}]`), nil
		default:
			return []byte(`{}`), nil
		}
	}}
	comments, err := gh.ListReviewComments(context.Background(), "o/r", 12)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 2 || comments[0].Body != "@/aviary:SELF review" || comments[1].Body != "@/aviary:SELF inline" || comments[1].Author != "lsegal" {
		t.Fatalf("comments = %#v, want the review then the inline comment", comments)
	}
	calls = nil
	if err := gh.AddReviewCommentReaction(context.Background(), "o/r", 42, "eyes"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"api", "repos/o/r/pulls/comments/42/reactions", "-f", "content=eyes"}; !slices.Equal(calls[0], want) {
		t.Fatalf("review comment reaction = %v, want %v", calls[0], want)
	}
	if err := gh.AddReviewReaction(context.Background(), "o/r", "PRR_6", "eyes"); err != nil {
		t.Fatal(err)
	}
	if call := calls[1]; call[1] != "graphql" || !slices.Contains(call, "subject=PRR_6") || !slices.Contains(call, "content=EYES") || !strings.Contains(strings.Join(call, " "), "addReaction") {
		t.Fatalf("review reaction = %v, want an addReaction mutation on PRR_6", call)
	}
}
