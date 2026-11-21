package projects

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeMentionSource returns every comment it holds on every scan, whatever the
// cursor, so the tests prove a mention is acted on once by its ID rather than
// by the scan window happening to move past it.
type fakeMentionSource struct {
	mu       sync.Mutex
	comments map[string][]RecentComment
	reviews  map[string][]RecentComment
	closing  map[int][]int
	scans    int
}

func newFakeMentionSource() *fakeMentionSource {
	return &fakeMentionSource{comments: make(map[string][]RecentComment), reviews: make(map[string][]RecentComment), closing: make(map[int][]int)}
}

func (f *fakeMentionSource) add(repo string, comment RecentComment) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.comments[repo] = append(f.comments[repo], comment)
}

func (f *fakeMentionSource) scanCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.scans
}

func (f *fakeMentionSource) RecentComments(_ context.Context, repo string, _ time.Time) ([]RecentComment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scans++
	return slices.Clone(f.comments[repo]), nil
}

func (f *fakeMentionSource) addReview(repo string, comment RecentComment) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reviews[repo] = append(f.reviews[repo], comment)
}

func (f *fakeMentionSource) RecentReviewComments(_ context.Context, repo string, _ time.Time) ([]RecentComment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.reviews[repo]), nil
}

func (f *fakeMentionSource) ClosingIssues(_ context.Context, _ string, number int) ([]int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closing[number], nil
}

// startPolledMentionRun starts a poll-mode run (no webhooks, as in
// -pollmode=poll and -pollmode=browser) over issue #7, waits for #7's first
// run to finish, and returns the run's pieces for the test to post a mention.
func startPolledMentionRun(t *testing.T, allowed []string) (*fakeRunner, *fakeCommentClient, *fakeMentionSource, func()) {
	t.Helper()
	return startPolledRunAs(t, allowed, "")
}

// startPolledRunAs is startPolledMentionRun for an instance running as the
// GitHub user selfLogin.
func startPolledRunAs(t *testing.T, allowed []string, selfLogin string) (*fakeRunner, *fakeCommentClient, *fakeMentionSource, func()) {
	t.Helper()
	return startPolledRunWith(t, allowed, selfLogin, nil)
}

// startPolledRunWith is startPolledRunAs with configure applied to the
// instance before it starts.
func startPolledRunWith(t *testing.T, allowed []string, selfLogin string, configure func(*Engine)) (*fakeRunner, *fakeCommentClient, *fakeMentionSource, func()) {
	t.Helper()
	dir := t.TempDir()
	src := &fakeSource{batches: [][]Issue{{{Number: 7}}}}
	runner := &fakeRunner{release: make(chan struct{}), dispatched: make(chan int, 4)}
	comments := newFakeCommentClient()
	mentions := newFakeMentionSource()
	w := &Engine{
		Repo: "o/r", Interval: 5 * time.Millisecond, Concurrency: 1, StatePath: filepath.Join(dir, "state.json"),
		Issues: src, Runner: runner, Identity: "SELF", AllowedCommenters: allowed, SelfLogin: selfLogin,
		Comments: comments, Mentions: mentions,
	}
	if configure != nil {
		configure(w)
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
	return runner, comments, mentions, stop
}

// waitForScans lets the run poll a few more times, so a test asserting that
// nothing was dispatched has given the scan a real chance to act.
func waitForScans(t *testing.T, mentions *fakeMentionSource, more int) {
	t.Helper()
	target := mentions.scanCount() + more
	deadline := time.Now().Add(runLoopWait)
	for mentions.scanCount() < target {
		if time.Now().After(deadline) {
			t.Fatalf("mention scans = %d, want at least %d", mentions.scanCount(), target)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestPolledDirectMentionOnIssueDispatchesOnce(t *testing.T) {
	runner, comments, mentions, stop := startPolledMentionRun(t, []string{"lsegal"})
	defer stop()

	body := "Please revisit this @/aviary:SELF"
	comments.inject("o/r", 7, Comment{Body: body, Author: "lsegal", CreatedAt: time.Now()})
	mentions.add("o/r", RecentComment{ID: 42, Number: 7, Body: body, Author: "lsegal", UpdatedAt: time.Now()})
	select {
	case n := <-runner.dispatched:
		if n != 7 {
			t.Fatalf("direct mention dispatched issue #%d, want #7", n)
		}
	case <-time.After(time.Second):
		t.Fatal("direct mention on the issue was not picked up by polling")
	}

	// The same comment keeps coming back from later scans; it must not start
	// another run.
	waitForScans(t, mentions, 5)
	select {
	case n := <-runner.dispatched:
		t.Fatalf("one mention dispatched a second run of #%d", n)
	default:
	}
	if reactions := comments.reactionsSnapshot(); len(reactions) != 1 || reactions[0] != (fakeReaction{Repo: "o/r", CommentID: 42, Content: "eyes"}) {
		t.Fatalf("reactions = %#v, want a single eyes reaction on comment 42", reactions)
	}
}

func TestPolledDirectMentionOnPullRequestDispatchesClosedIssue(t *testing.T) {
	runner, comments, mentions, stop := startPolledMentionRun(t, []string{"lsegal"})
	defer stop()

	body := "@/aviary:SELF the CI failure needs another look"
	mentions.mu.Lock()
	mentions.closing[12] = []int{7}
	mentions.mu.Unlock()
	comments.inject("o/r", 12, Comment{Body: body, Author: "lsegal", CreatedAt: time.Now()})
	mentions.add("o/r", RecentComment{ID: 43, Number: 12, Body: body, Author: "lsegal", UpdatedAt: time.Now()})
	select {
	case n := <-runner.dispatched:
		if n != 7 {
			t.Fatalf("pull request mention dispatched #%d, want the issue it closes, #7", n)
		}
	case <-time.After(time.Second):
		t.Fatal("direct mention on the pull request was not picked up by polling")
	}
	if reactions := comments.reactionsSnapshot(); len(reactions) != 1 || reactions[0].CommentID != 43 {
		t.Fatalf("reactions = %#v, want an eyes reaction on the pull request comment", reactions)
	}
}

func TestPolledDirectMentionIgnoresUnauthorizedMentions(t *testing.T) {
	for _, test := range []struct {
		name     string
		comments []Comment
		mention  RecentComment
	}{
		{
			name:     "disallowed commenter",
			comments: []Comment{{Body: "@/aviary:SELF go", Author: "impersonator"}},
			mention:  RecentComment{ID: 50, Number: 7, Body: "@/aviary:SELF go", Author: "impersonator"},
		},
		{
			name:     "not the newest comment",
			comments: []Comment{{Body: "@/aviary:SELF go", Author: "lsegal"}, {Body: "actually never mind", Author: "lsegal"}},
			mention:  RecentComment{ID: 51, Number: 7, Body: "@/aviary:SELF go", Author: "lsegal"},
		},
		{
			name:     "another instance",
			comments: []Comment{{Body: "@/aviary:OTHER go", Author: "lsegal"}},
			mention:  RecentComment{ID: 52, Number: 7, Body: "@/aviary:OTHER go", Author: "lsegal"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner, comments, mentions, stop := startPolledMentionRun(t, []string{"lsegal"})
			defer stop()
			for _, comment := range test.comments {
				comments.inject("o/r", 7, comment)
			}
			mentions.add("o/r", test.mention)
			waitForScans(t, mentions, 5)
			select {
			case n := <-runner.dispatched:
				t.Fatalf("%s dispatched #%d", test.name, n)
			default:
			}
		})
	}
}

func TestWebhookDirectMentionOnPullRequestDispatchesClosedIssue(t *testing.T) {
	dir := t.TempDir()
	src := &fakeSource{batches: [][]Issue{{{Number: 7}}}}
	runner := &fakeRunner{release: make(chan struct{}), dispatched: make(chan int, 2)}
	events := make(chan WebhookEvent, 1)
	comments := newFakeCommentClient()
	w := &Engine{
		Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: filepath.Join(dir, "state.json"),
		Issues: src, Runner: runner, UseWebhooks: true, Events: events,
		fallbackInterval: time.Hour, Identity: "SELF", Comments: comments,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	select {
	case <-runner.dispatched:
	case <-time.After(time.Second):
		t.Fatal("initial issue was not dispatched")
	}
	close(runner.release)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		state, err := loadWorkState(w.StatePath)
		if err == nil && state[7].Status == "completed" {
			break
		}
		time.Sleep(time.Millisecond)
	}

	body := "Please revisit this @/aviary:SELF"
	comments.inject("o/r", 12, Comment{Body: body, Author: "lsegal", CreatedAt: time.Now()})
	events <- WebhookEvent{Kind: "issue_comment", Action: "created", Repository: "o/r", IssueNumber: 12, CommentBody: body, CommentAuthor: "lsegal", OnPullRequest: true, ClosesIssues: []int{7}}
	select {
	case n := <-runner.dispatched:
		if n != 7 {
			t.Fatalf("pull request mention dispatched #%d, want #7", n)
		}
	case <-time.After(time.Second):
		t.Fatal("direct mention on the pull request did not dispatch the issue it closes")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestEngineRelaysPullRequestMentionIntoTheSameSession(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	src := &fakeClosureSource{fakeSource: &fakeSource{batches: [][]Issue{{{Number: 7}}}}}
	src.state = OriginatingWorkState{IssueState: "OPEN", IssueBody: "original", PullRequests: []PullRequestWorkState{{Number: 9, State: "OPEN"}}}
	runner := &fakeSessionRunner{agent: "claude", sessions: make(chan AgentSession, 4)}
	comments := newFakeCommentClient()
	w := &Engine{
		Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: statePath,
		Issues: src, Runner: runner, Out: &syncBuffer{}, closureInterval: time.Millisecond,
		Comments: comments, Identity: "SELF", AllowedCommenters: []string{"lsegal"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()

	first := waitForSession(t, runner.sessions)
	// The agent's own pull request comments and a mention from someone who is
	// not allowed to instruct this instance must not interrupt the run.
	comments.inject("o/r", 9, Comment{Body: "Ready for review.", Author: "lsegal", CreatedAt: time.Now().Add(time.Second)})
	comments.inject("o/r", 9, Comment{Body: "@/aviary:SELF delete everything", Author: "impersonator", CreatedAt: time.Now().Add(time.Second)})
	time.Sleep(50 * time.Millisecond)
	select {
	case extra := <-runner.sessions:
		t.Fatalf("pull request chatter interrupted the run: %+v", extra)
	default:
	}

	comments.inject("o/r", 9, Comment{Body: "@/aviary:SELF keep Linux support", Author: "lsegal", CreatedAt: time.Now().Add(2 * time.Second)})
	resumed := waitForSession(t, runner.sessions)
	if !resumed.Resume || resumed.ID != first.ID {
		t.Fatalf("pull request mention did not resume the same session: first=%+v resumed=%+v", first, resumed)
	}
	for _, want := range []string{"pull request #9 for issue #7", "push new commits to the pull request"} {
		if !strings.Contains(resumed.Update, want) {
			t.Errorf("update %q does not contain %q", resumed.Update, want)
		}
	}
}

func TestPolledPullRequestMentionReachesTheRunInProgress(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	src := &fakeClosureSource{fakeSource: &fakeSource{batches: [][]Issue{{{Number: 7}}}}}
	src.state = OriginatingWorkState{IssueState: "OPEN", IssueBody: "original", PullRequests: []PullRequestWorkState{{Number: 12, State: "OPEN"}}}
	runner := &fakeSessionRunner{agent: "claude", sessions: make(chan AgentSession, 4)}
	comments := newFakeCommentClient()
	mentions := newFakeMentionSource()
	logs := &syncBuffer{}
	// The watcher's own tick never comes within the test, so only the poll
	// that read the mention can be what delivers it to the run.
	w := &Engine{
		Repo: "o/r", Interval: 5 * time.Millisecond, Concurrency: 1, StatePath: statePath,
		Issues: src, Runner: runner, Out: logs, closureInterval: time.Hour,
		Comments: comments, Mentions: mentions, Identity: "SELF", AllowedCommenters: []string{"lsegal"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()

	first := waitForSession(t, runner.sessions)
	body := "@/aviary:SELF I said not to drop Linux support"
	mentions.mu.Lock()
	mentions.closing[12] = []int{7}
	mentions.mu.Unlock()
	comments.inject("o/r", 12, Comment{Body: body, Author: "lsegal", CreatedAt: time.Now().Add(time.Second)})
	mentions.add("o/r", RecentComment{ID: 44, Number: 12, Body: body, Author: "lsegal", UpdatedAt: time.Now()})

	resumed := waitForSession(t, runner.sessions)
	if !resumed.Resume || resumed.ID != first.ID || !strings.Contains(resumed.Update, "pull request #12 for issue #7") {
		t.Fatalf("pull request mention was not relayed into the run in progress: first=%+v resumed=%+v", first, resumed)
	}
	// The relayed mention is not also queued for a fresh run.
	waitForScans(t, mentions, 5)
	select {
	case extra := <-runner.sessions:
		t.Fatalf("one mention launched another session: %+v", extra)
	default:
	}
	if !strings.Contains(logs.String(), "issue #7 direct mention of instance SELF concerns the run in progress; relaying it into that run") {
		t.Fatalf("relay was not logged:\n%s", logs.String())
	}
	if reactions := comments.reactionsSnapshot(); len(reactions) != 1 || reactions[0].CommentID != 44 {
		t.Fatalf("reactions = %#v, want one eyes reaction on the pull request comment", reactions)
	}
}

func TestPolledDirectMentionLogsWhyItWaits(t *testing.T) {
	src := &fakeSource{batches: [][]Issue{{}}}
	comments := newFakeCommentClient()
	mentions := newFakeMentionSource()
	logs := &syncBuffer{}
	w := &Engine{
		Repo: "o/r", Interval: 5 * time.Millisecond, Concurrency: 1, StatePath: filepath.Join(t.TempDir(), "state.json"),
		Issues: src, Runner: &fakeRunner{}, Out: logs, Comments: comments, Mentions: mentions, Identity: "SELF",
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()

	body := "@/aviary:SELF please look"
	comments.inject("o/r", 99, Comment{Body: body, Author: "lsegal", CreatedAt: time.Now()})
	mentions.add("o/r", RecentComment{ID: 45, Number: 99, Body: body, Author: "lsegal", UpdatedAt: time.Now()})
	waitForScans(t, mentions, 5)
	if got := strings.Count(logs.String(), "o/r#99 direct mention of instance SELF is waiting: it is not among the open issues this instance watches"); got != 1 {
		t.Fatalf("waiting mention logged %d time(s), want once:\n%s", got, logs.String())
	}
}

func TestDecodeWebhookEventMarksPullRequestComments(t *testing.T) {
	event := decodeWebhookEvent("issue_comment", []byte(`{"action":"created","repository":{"full_name":"o/r"},"issue":{"number":12,"body":"Fixes the parser.\n\nCloses #7\nAlso see other/repo#9 and closes o/r#8","pull_request":{"url":"https://api.github.com/repos/o/r/pulls/12"}},"comment":{"id":5,"body":"@/aviary:SELF","user":{"login":"lsegal"}}}`))
	if !event.OnPullRequest || !slices.Equal(event.ClosesIssues, []int{7, 8}) {
		t.Fatalf("event = %+v, want a pull request comment closing #7 and #8", event)
	}
	event = decodeWebhookEvent("issue_comment", []byte(`{"action":"created","repository":{"full_name":"o/r"},"issue":{"number":7,"body":"Closes #3"},"comment":{"id":5,"body":"@/aviary:SELF"}}`))
	if event.OnPullRequest || len(event.ClosesIssues) != 0 {
		t.Fatalf("event = %+v, want an issue comment with no closing references", event)
	}
}

func TestClosingIssueNumbers(t *testing.T) {
	got := closingIssueNumbers("Closes #7\nfixes #7, resolved O/R#9, closes other/repo#10, see #11, Fix #12", "o/r")
	if want := []int{7, 9, 12}; !slices.Equal(got, want) {
		t.Fatalf("closingIssueNumbers = %v, want %v", got, want)
	}
}

func TestMentionScanReposCoversTargetsAndListedRepositories(t *testing.T) {
	got := mentionScanRepos([]string{"o/r", "o/r/discussions", "users/o/projects/1"}, []Issue{{Repository: "o/other"}, {Repository: "o/r"}})
	if want := []string{"o/other", "o/r"}; !slices.Equal(got, want) {
		t.Fatalf("mentionScanRepos = %v, want %v", got, want)
	}
}

func TestGHCLIRecentCommentsAndClosingIssues(t *testing.T) {
	var calls [][]string
	gh := GHCLI{runCommand: func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, args)
		switch {
		case strings.Contains(args[1], "issues/comments?"):
			return []byte(`[{"id":42,"body":"@/aviary:SELF","issue_url":"https://api.github.com/repos/o/r/issues/12","updated_at":"2026-10-06T07:00:00Z","user":{"login":"lsegal"}}]`), nil
		case args[1] == "repos/o/r/issues/12":
			return []byte(`{"body":"Closes #7","pull_request":{"url":"x"}}`), nil
		default:
			return []byte(`{"body":"Closes #3"}`), nil
		}
	}}
	since := time.Date(2026, 10, 6, 6, 0, 0, 0, time.UTC)
	comments, err := gh.RecentComments(context.Background(), "o/r", since)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 || comments[0].ID != 42 || comments[0].Number != 12 || comments[0].Author != "lsegal" {
		t.Fatalf("comments = %#v", comments)
	}
	if !strings.Contains(calls[0][1], "since=2026-10-06T06%3A00%3A00Z") || !slices.Contains(calls[0], "--paginate") {
		t.Fatalf("recent comments request = %v, want a paginated listing since the cursor", calls[0])
	}
	closing, err := gh.ClosingIssues(context.Background(), "o/r", 12)
	if err != nil || !slices.Equal(closing, []int{7}) {
		t.Fatalf("ClosingIssues(#12) = %v, %v; want [7]", closing, err)
	}
	closing, err = gh.ClosingIssues(context.Background(), "o/r", 3)
	if err != nil || len(closing) != 0 {
		t.Fatalf("ClosingIssues(issue #3) = %v, %v; want none for an issue", closing, err)
	}
}

func TestMentionedLoginMatchesOrdinaryMentionsOnly(t *testing.T) {
	for _, body := range []string{"@lsegal please look", "cc @LSegal.", "thoughts, (@lsegal)?", "first line\n@lsegal"} {
		if !mentionedLogin(body, "lsegal") {
			t.Errorf("mentionedLogin(%q) = false, want true", body)
		}
	}
	for _, body := range []string{"no mention", "lsegal without the at", "@lsegal2 is someone else", "@my-lsegal too", "@lsegal/team is a team", "user@lsegal.com", "`@lsegal` in code", "```\n@lsegal\n```", "> @lsegal wrote this"} {
		if mentionedLogin(body, "lsegal") {
			t.Errorf("mentionedLogin(%q) = true, want false", body)
		}
	}
	if mentionedLogin("@lsegal", "") {
		t.Error("an unknown login must never match")
	}
}

func TestCommentRequestsRecheckRequiresAllowedAuthorAndLoginMention(t *testing.T) {
	w := &Engine{Identity: "SELF", SelfLogin: "bot", AllowedCommenters: []string{"lsegal", "bot"}}
	if !w.commentRequestsRecheck("@bot please look", "LSegal") {
		t.Error("an allowed commenter mentioning the login must request a recheck")
	}
	for _, c := range []struct{ body, author string }{
		{"please look", "lsegal"},
		{"@bot please look", "stranger"},
		{"@bot and @/aviary:OTHER please look", "lsegal"},
		{"Starting work on this issue for @bot /aviary:OTHER", "bot"},
	} {
		if w.commentRequestsRecheck(c.body, c.author) {
			t.Errorf("commentRequestsRecheck(%q, %q) = true, want false", c.body, c.author)
		}
	}
	w.AllowedCommenters = nil
	if w.commentRequestsRecheck("@bot please look", "lsegal") {
		t.Error("an empty allowed list must not let everyone request a recheck")
	}
}

func TestPolledLoginMentionDispatchesOnce(t *testing.T) {
	runner, comments, mentions, stop := startPolledRunAs(t, []string{"lsegal"}, "bot")
	defer stop()

	mentions.add("o/r", RecentComment{ID: 1, Number: 7, Body: "history @bot", Author: "lsegal", CreatedAt: time.Now().Add(-time.Hour), UpdatedAt: time.Now()})
	waitForScans(t, mentions, 2)
	mentions.add("o/r", RecentComment{ID: 42, Number: 7, Body: "@bot please revisit this", Author: "lsegal", CreatedAt: time.Now(), UpdatedAt: time.Now()})
	select {
	case n := <-runner.dispatched:
		if n != 7 {
			t.Fatalf("login mention dispatched #%d, want #7", n)
		}
	case <-time.After(time.Second):
		t.Fatal("login mention was not picked up by polling")
	}
	waitForScans(t, mentions, 5)
	select {
	case n := <-runner.dispatched:
		t.Fatalf("one comment dispatched a second run of #%d", n)
	default:
	}
	if reactions := comments.reactionsSnapshot(); len(reactions) != 1 || reactions[0].CommentID != 42 {
		t.Fatalf("reactions = %#v, want a single eyes reaction on comment 42", reactions)
	}
}

func TestPolledCommentsThatDoNotQualifyChangeNothing(t *testing.T) {
	runner, comments, mentions, stop := startPolledRunAs(t, []string{"lsegal"}, "bot")
	defer stop()

	now := time.Now()
	mentions.add("o/r", RecentComment{ID: 50, Number: 7, Body: "please revisit this", Author: "lsegal", CreatedAt: now, UpdatedAt: now})
	mentions.add("o/r", RecentComment{ID: 51, Number: 7, Body: "@bot please revisit this", Author: "stranger", CreatedAt: now, UpdatedAt: now})
	mentions.add("o/r", RecentComment{ID: 52, Number: 99, Body: "@bot this issue is not watched", Author: "lsegal", CreatedAt: now, UpdatedAt: now})
	waitForScans(t, mentions, 5)
	select {
	case n := <-runner.dispatched:
		t.Fatalf("comment dispatched a run of #%d", n)
	default:
	}
	if reactions := comments.reactionsSnapshot(); len(reactions) != 0 {
		t.Fatalf("reactions = %#v, want none", reactions)
	}
}

func TestPolledLoginMentionOnPullRequestDispatchesClosedIssue(t *testing.T) {
	runner, _, mentions, stop := startPolledRunAs(t, []string{"lsegal"}, "bot")
	defer stop()

	mentions.mu.Lock()
	mentions.closing[12] = []int{7}
	mentions.mu.Unlock()
	mentions.addReview("o/r", RecentComment{ID: 43, Kind: ReviewComment, Number: 12, Body: "@bot this line is wrong", Author: "lsegal", CreatedAt: time.Now(), UpdatedAt: time.Now()})
	select {
	case n := <-runner.dispatched:
		if n != 7 {
			t.Fatalf("pull request comment dispatched #%d, want the issue it closes, #7", n)
		}
	case <-time.After(time.Second):
		t.Fatal("login mention in a review comment was not picked up by polling")
	}
}

func TestPolledCommentFromOwnLoginDuringAgentRunDoesNotDispatch(t *testing.T) {
	runner, _, mentions, stop := startPolledRunAs(t, []string{"bot"}, "bot")
	defer stop()

	// The first run has only just finished, so this reads as that agent's.
	mentions.add("o/r", RecentComment{ID: 60, Number: 7, Body: "@bot this needs a human", Author: "bot", CreatedAt: time.Now(), UpdatedAt: time.Now()})
	waitForScans(t, mentions, 5)
	select {
	case n := <-runner.dispatched:
		t.Fatalf("the agent's own comment dispatched a run of #%d", n)
	default:
	}
	// Past the grace period the same login is a human again.
	later := time.Now().Add(2 * agentCommentGrace)
	mentions.add("o/r", RecentComment{ID: 61, Number: 7, Body: "@bot please revisit this", Author: "bot", CreatedAt: later, UpdatedAt: later})
	select {
	case <-runner.dispatched:
	case <-time.After(time.Second):
		t.Fatal("a later comment from the login was not picked up")
	}
}

func TestPolledLoginMentionRespectsAnotherInstancesClaim(t *testing.T) {
	runner, comments, mentions, stop := startPolledRunAs(t, []string{"lsegal"}, "bot")
	defer stop()

	comments.reset("o/r", 7)
	comments.inject("o/r", 7, Comment{Body: signComment(startingClaimBody, "OTHER"), Author: "bot", CreatedAt: time.Now()})
	mentions.add("o/r", RecentComment{ID: 70, Number: 7, Body: "@bot please revisit this", Author: "lsegal", CreatedAt: time.Now(), UpdatedAt: time.Now()})
	waitForScans(t, mentions, 5)
	select {
	case n := <-runner.dispatched:
		t.Fatalf("a comment not addressed to this instance overrode another's claim on #%d", n)
	default:
	}
}

func TestScanSeedsCommentsVisibleWhenWatchingStarts(t *testing.T) {
	start := time.Now()
	mentions := newFakeMentionSource()
	w := &Engine{Identity: "SELF", SelfLogin: "bot", AllowedCommenters: []string{"lsegal"}, Mentions: mentions}
	scan := newMentionScan(start)
	listed := map[string]bool{"o/r#7": true}
	queue := map[string]bool{}
	scanned := func() { w.scanDirectMentions(context.Background(), scan, []string{"o/r"}, listed, queue) }

	// Timestamped after start, but already there on the first scan.
	mentions.add("o/r", RecentComment{ID: 1, Number: 7, Body: "@bot old", Author: "lsegal", CreatedAt: start.Add(time.Second), UpdatedAt: start.Add(time.Second)})
	scanned()
	scanned()
	if len(queue) != 0 {
		t.Fatalf("queue = %v, want the first scan's comments treated as history", queue)
	}
	// An edit returns an older comment to the listing under an unseen ID.
	mentions.add("o/r", RecentComment{ID: 2, Number: 7, Body: "@bot edited", Author: "lsegal", CreatedAt: start.Add(-time.Hour), UpdatedAt: start.Add(time.Minute)})
	scanned()
	if len(queue) != 0 {
		t.Fatalf("queue = %v, want an edited old comment treated as history", queue)
	}
	mentions.add("o/r", RecentComment{ID: 3, Number: 7, Body: "@bot new", Author: "lsegal", CreatedAt: start.Add(time.Minute), UpdatedAt: start.Add(time.Minute)})
	scanned()
	if !queue["o/r#7"] || !scan.rechecks["o/r#7"] {
		t.Fatalf("queue = %v, rechecks = %v, want #7 queued as a recheck", queue, scan.rechecks)
	}
}

func TestPullRequestMentionsRelaysLoginMentionFromAllowedCommenter(t *testing.T) {
	since := time.Now()
	comments := newFakeCommentClient()
	w := &Engine{Identity: "SELF", SelfLogin: "bot", AllowedCommenters: []string{"lsegal"}, Comments: comments}
	issue := Issue{Number: 7}
	pulls := []PullRequestWorkState{{Number: 12}}
	seen := map[string]bool{}
	comments.inject("o/r", 12, Comment{Body: "status update", Author: "lsegal", CreatedAt: since.Add(time.Second)})
	comments.inject("o/r", 12, Comment{Body: "@bot from a stranger", Author: "stranger", CreatedAt: since.Add(time.Second)})
	if got := w.pullRequestMentions(context.Background(), "o/r", issue, pulls, since, seen); len(got) != 0 {
		t.Fatalf("relayed %v, want nothing", got)
	}
	comments.inject("o/r", 12, Comment{Body: "@bot use the other API", Author: "lsegal", CreatedAt: since.Add(time.Second)})
	if got := w.pullRequestMentions(context.Background(), "o/r", issue, pulls, since, seen); !slices.Equal(got, []int{12}) {
		t.Fatalf("relayed %v, want pull request #12", got)
	}
}

func TestWebhookLoginMentionDispatchesWithoutInstanceMention(t *testing.T) {
	src := &fakeSource{batches: [][]Issue{{{Number: 7}}}}
	runner := &fakeRunner{release: make(chan struct{}), dispatched: make(chan int, 4)}
	comments := newFakeCommentClient()
	events := make(chan WebhookEvent)
	w := &Engine{
		Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: filepath.Join(t.TempDir(), "state.json"), UseWebhooks: true, Events: events,
		Issues: src, Runner: runner, Identity: "SELF", SelfLogin: "bot", AllowedCommenters: []string{"lsegal"}, Comments: comments,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}()
	select {
	case <-runner.dispatched:
	case <-time.After(time.Second):
		t.Fatal("initial issue was not dispatched")
	}
	close(runner.release)
	deadline := time.Now().Add(time.Second)
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

	deliver := func(id int64, body, author string) {
		comments.inject("o/r", 7, Comment{Body: body, Author: author, CreatedAt: time.Now()})
		events <- WebhookEvent{Kind: "issue_comment", Action: "created", Repository: "o/r", IssueNumber: 7, CommentID: id, CommentBody: body, CommentAuthor: author}
	}
	deliver(80, "no mention at all", "lsegal")
	deliver(81, "@bot from a stranger", "stranger")
	select {
	case n := <-runner.dispatched:
		t.Fatalf("an unqualified comment dispatched #%d", n)
	case <-time.After(50 * time.Millisecond):
	}
	deliver(82, "@bot please revisit this", "lsegal")
	select {
	case n := <-runner.dispatched:
		if n != 7 {
			t.Fatalf("dispatched #%d, want #7", n)
		}
	case <-time.After(time.Second):
		t.Fatal("login mention delivered by webhook did not dispatch a run")
	}
}

func TestPolledDirectMentionOnFinishedJobGoesToTheProjectsAgent(t *testing.T) {
	prompts := make(chan string, 4)
	runner, comments, mentions, stop := startPolledRunWith(t, []string{"lsegal"}, "", func(w *Engine) {
		w.RouteMention = func(_ context.Context, prompt string) error {
			prompts <- prompt
			return nil
		}
	})
	defer stop()

	body := "Please revisit this @/aviary:SELF"
	comments.inject("o/r", 7, Comment{Body: body, Author: "lsegal", CreatedAt: time.Now()})
	mentions.add("o/r", RecentComment{ID: 42, Number: 7, Body: body, Author: "lsegal", UpdatedAt: time.Now()})
	select {
	case prompt := <-prompts:
		if want := "look at comment https://github.com/o/r/issues/7#issuecomment-42 on issue o/r#7 and determine next steps"; prompt != want {
			t.Fatalf("prompt = %q, want %q", prompt, want)
		}
	case n := <-runner.dispatched:
		t.Fatalf("mention on the finished job restarted #%d instead of reaching the Projects agent", n)
	case <-time.After(time.Second):
		t.Fatal("mention on the finished job never reached the Projects agent")
	}

	// The job restarts only when the agent asks for it.
	waitForScans(t, mentions, 5)
	select {
	case n := <-runner.dispatched:
		t.Fatalf("a routed mention restarted #%d", n)
	case prompt := <-prompts:
		t.Fatalf("one mention reached the Projects agent twice: %q", prompt)
	default:
	}
}

func TestPolledDirectMentionRestartsTheJobWhenTheProjectsAgentIsUnavailable(t *testing.T) {
	runner, comments, mentions, stop := startPolledRunWith(t, []string{"lsegal"}, "", func(w *Engine) {
		w.RouteMention = func(context.Context, string) error { return errors.New("no agent") }
	})
	defer stop()

	body := "Please revisit this @/aviary:SELF"
	comments.inject("o/r", 7, Comment{Body: body, Author: "lsegal", CreatedAt: time.Now()})
	mentions.add("o/r", RecentComment{ID: 42, Number: 7, Body: body, Author: "lsegal", UpdatedAt: time.Now()})
	select {
	case n := <-runner.dispatched:
		if n != 7 {
			t.Fatalf("direct mention dispatched issue #%d, want #7", n)
		}
	case <-time.After(time.Second):
		t.Fatal("direct mention was dropped when the Projects agent could not take it")
	}
}

func TestMentionPromptLinksTheComment(t *testing.T) {
	issue := Issue{Repository: "o/r", Number: 7}
	for _, test := range []struct {
		comment mentionComment
		link    string
	}{
		{mentionComment{Kind: ConversationComment, Repository: "o/r", Number: 12, ID: 5}, "https://github.com/o/r/issues/12#issuecomment-5"},
		{mentionComment{Kind: ReviewComment, Repository: "o/r", Number: 12, ID: 6}, "https://github.com/o/r/pull/12#discussion_r6"},
		{mentionComment{Kind: ReviewBody, Repository: "o/r", Number: 12, ID: 7}, "https://github.com/o/r/pull/12#pullrequestreview-7"},
		{mentionComment{}, "https://github.com/o/r/issues/7"},
	} {
		want := "look at comment " + test.link + " on issue o/r#7 and determine next steps"
		if got := mentionPrompt(issue, test.comment); got != want {
			t.Errorf("mentionPrompt(%+v) = %q, want %q", test.comment, got, want)
		}
	}
}
