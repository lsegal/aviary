package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

// fakeCommentPage stands in for the tab comments are read in. Each canned
// result answers one evaluation, so a test can hand a page that has not drawn
// yet followed by one that has.
type fakeCommentPage struct {
	results   []commentList
	evalErr   error
	navigated []string
	reloads   int
	status    int
	evals     int
	// signIn is the answer the sign-in probe gets. The zero value reports a
	// page that is neither signed in nor signed out, which keeps the
	// diagnosis out of the way of tests that are not about it.
	signIn signInState
}

func (p *fakeCommentPage) Navigate(url string) error {
	p.navigated = append(p.navigated, url)
	return nil
}

func (p *fakeCommentPage) Reload() error {
	p.reloads++
	return nil
}

func (p *fakeCommentPage) HTTPStatus() int { return p.status }

func (p *fakeCommentPage) Eval(_ string, out any) error {
	if p.evalErr != nil {
		return p.evalErr
	}
	if state, ok := out.(*signInState); ok {
		*state = p.signIn
		return nil
	}
	if p.evals >= len(p.results) {
		return fmt.Errorf("unexpected evaluation %d", p.evals+1)
	}
	result := p.results[p.evals]
	p.evals++
	// Round-tripping through JSON mirrors how chromedp decodes the script's
	// value, so a field the script names differently than Go does would fail
	// here rather than silently arriving zeroed.
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, out.(*commentList))
}

// newTestCommentSource builds a source reading every conversation through one
// fake page, with the API client behind it as the fallback.
func newTestCommentSource(page *fakeCommentPage, api core.CommentClient, logf func(string, ...interface{})) *CommentSource {
	if page.status == 0 {
		page.status = 200
	}
	return &CommentSource{
		pageFor:  func(string) (Page, error) { return page, nil },
		api:      api,
		logf:     logf,
		reported: map[string]bool{},
		// One attempt per page load keeps the canned results above matching
		// the reads one-for-one; the settle wait has its own test.
		settleAttempts: 1,
		settleDelay:    time.Millisecond,
		sleep:          func(context.Context, time.Duration) bool { return true },
	}
}

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return parsed
}

// The point of the whole change: a comment read in browser mode comes off the
// rendered page, carries the author, body, and timestamp the handshake reads,
// and costs no API call (issue #441).
func TestCommentsReadFromThePage(t *testing.T) {
	page := &fakeCommentPage{results: []commentList{{
		Recognized: true,
		Comments: []commentRow{
			// GitHub renders its timestamps with a fractional second, so the
			// parse has to take one.
			{ID: "1", Author: "alice", Body: "Does anyone have this? /aviary:AAAA", CreatedAt: "2026-08-30T10:00:00.000Z"},
			{ID: "2", Author: "bob", Body: "Starting work on this issue /aviary:BBBB", CreatedAt: "2026-08-30T10:05:00Z"},
		},
	}}}
	api := newFakeCommentClient()
	source := newTestCommentSource(page, api, nil)

	comments, err := source.ListComments(context.Background(), "owner/repo", 7)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("got %d comments, want 2: %+v", len(comments), comments)
	}
	if comments[0].Author != "alice" || comments[1].Author != "bob" {
		t.Fatalf("authors = %q, %q", comments[0].Author, comments[1].Author)
	}
	if want := mustTime(t, "2026-08-30T10:05:00Z"); !comments[1].CreatedAt.Equal(want) {
		t.Fatalf("second comment created at %v, want %v", comments[1].CreatedAt, want)
	}
	// The handshake parses this body in the root package, so the rendered text
	// has to survive the trip intact, signature and all.
	if want := "Starting work on this issue /aviary:BBBB"; comments[1].Body != want {
		t.Fatalf("second body = %q, want %q", comments[1].Body, want)
	}
	if api.lists != 0 {
		t.Fatalf("the API was called %d time(s) for a page aviary could read", api.lists)
	}
	if want := "https://github.com/owner/repo/issues/7"; len(page.navigated) != 1 || page.navigated[0] != want {
		t.Fatalf("navigated to %v, want [%s]", page.navigated, want)
	}
}

// The page renders comments oldest first, but the order is what tells the
// newest claim from an older one, so it is sorted rather than assumed.
func TestCommentsSortIntoCreationOrder(t *testing.T) {
	page := &fakeCommentPage{results: []commentList{{
		Recognized: true,
		Comments: []commentRow{
			{ID: "2", Author: "bob", Body: "second", CreatedAt: "2026-08-30T10:05:00Z"},
			{ID: "1", Author: "alice", Body: "first", CreatedAt: "2026-08-30T10:00:00Z"},
		},
	}}}
	source := newTestCommentSource(page, newFakeCommentClient(), nil)

	comments, err := source.ListComments(context.Background(), "owner/repo", 7)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if comments[0].Body != "first" || comments[1].Body != "second" {
		t.Fatalf("comments out of order: %+v", comments)
	}
}

// Comments sharing a timestamp are ordered by their position in the list, which
// is how the handshake breaks a tie between a claim and the withdrawal posted
// moments after it (issue #443), so the page's own order has to survive the
// sort rather than being shuffled by it.
func TestCommentsKeepPageOrderForTiedTimestamps(t *testing.T) {
	page := &fakeCommentPage{results: []commentList{{
		Recognized: true,
		Comments: []commentRow{
			{ID: "1", Author: "bob", Body: "Starting work on this issue /aviary:BBBB", CreatedAt: "2026-08-30T10:00:00Z"},
			{ID: "2", Author: "bob", Body: "Releasing this issue /aviary:BBBB", CreatedAt: "2026-08-30T10:00:00Z"},
		},
	}}}
	source := newTestCommentSource(page, newFakeCommentClient(), nil)

	comments, err := source.ListComments(context.Background(), "owner/repo", 7)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	// The root package's handshake takes the last claim in the list, so the
	// withdrawal has to still be behind the claim it withdraws.
	if comments[0].Body != "Starting work on this issue /aviary:BBBB" || comments[1].Body != "Releasing this issue /aviary:BBBB" {
		t.Fatalf("the withdrawal is no longer last: %+v", comments)
	}
}

// A conversation that rendered with nothing on it is an empty comment list,
// not a failure, and must not cost an API call either.
func TestCommentsEmptyConversationIsNotAFallback(t *testing.T) {
	page := &fakeCommentPage{results: []commentList{{Recognized: true}}}
	api := newFakeCommentClient()
	source := newTestCommentSource(page, api, nil)

	comments, err := source.ListComments(context.Background(), "owner/repo", 7)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(comments) != 0 {
		t.Fatalf("got %d comments, want none: %+v", len(comments), comments)
	}
	if api.lists != 0 {
		t.Fatalf("the API was called %d time(s) for a conversation that rendered empty", api.lists)
	}
}

// A page Aviary could not read must never be reported as a conversation with no
// comments: the handshake would read an owned ticket as unclaimed and two
// instances would claim the same work. Every such read goes to the API instead.
func TestCommentsFallBackToTheAPI(t *testing.T) {
	apiComments := []core.Comment{{Body: "Starting work on this issue /aviary:BBBB", Author: "bob", CreatedAt: time.Now()}}
	for name, page := range map[string]*fakeCommentPage{
		"unrecognized markup": {results: []commentList{{}}},
		"evaluation failed":   {evalErr: fmt.Errorf("target closed")},
		"http error":          {status: 404, results: []commentList{{Recognized: true}}},
		"signed out": {
			results: []commentList{{Recognized: true}},
			signIn:  signInState{SignedOut: true},
		},
		"unreadable timestamp": {results: []commentList{{
			Recognized: true,
			Comments:   []commentRow{{ID: "1", Author: "bob", Body: "hi", CreatedAt: ""}},
		}}},
	} {
		t.Run(name, func(t *testing.T) {
			api := newFakeCommentClient()
			api.comments["owner/repo#7"] = apiComments
			source := newTestCommentSource(page, api, nil)

			comments, err := source.ListComments(context.Background(), "owner/repo", 7)
			if err != nil {
				t.Fatalf("ListComments: %v", err)
			}
			if api.lists != 1 {
				t.Fatalf("the API was called %d time(s), want exactly one fallback", api.lists)
			}
			if len(comments) != 1 || comments[0].Author != "bob" {
				t.Fatalf("got %+v, want the API's answer", comments)
			}
		})
	}
}

// A ticket an agent is working has its conversation re-read on a timer for the
// whole run, so a page that cannot be read must be reported once rather than on
// every check.
func TestCommentsReportFallbackOncePerConversation(t *testing.T) {
	page := &fakeCommentPage{results: []commentList{{}, {}, {}}}
	var mu sync.Mutex
	var logged []string
	source := newTestCommentSource(page, newFakeCommentClient(), func(format string, args ...interface{}) {
		mu.Lock()
		defer mu.Unlock()
		logged = append(logged, fmt.Sprintf(format, args...))
	})

	for i := 0; i < 3; i++ {
		if _, err := source.ListComments(context.Background(), "owner/repo", 7); err != nil {
			t.Fatalf("ListComments: %v", err)
		}
	}
	if len(logged) != 1 {
		t.Fatalf("logged %d line(s), want 1: %v", len(logged), logged)
	}
	if !strings.Contains(logged[0], "owner/repo/issues/7") {
		t.Fatalf("log line %q does not name the conversation", logged[0])
	}
}

// The conversation draws client-side, so a read that lands before it has
// rendered is retried rather than failed: an issue whose page was merely slow
// must not be reported as unreadable and sent to the API.
func TestCommentsWaitForTheConversationToRender(t *testing.T) {
	page := &fakeCommentPage{results: []commentList{
		{},
		{},
		{Recognized: true, Comments: []commentRow{{ID: "1", Author: "bob", Body: "hi", CreatedAt: "2026-08-30T10:00:00Z"}}},
	}}
	api := newFakeCommentClient()
	source := newTestCommentSource(page, api, nil)
	source.settleAttempts = 5

	comments, err := source.ListComments(context.Background(), "owner/repo", 7)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("got %d comments, want 1", len(comments))
	}
	if page.evals != 3 {
		t.Fatalf("evaluated %d time(s), want 3", page.evals)
	}
	if api.lists != 0 {
		t.Fatalf("a page that rendered late still cost %d API call(s)", api.lists)
	}
}

// Comments are read per issue, not per target, so the reads share one tab: a
// second conversation navigates it, and re-reading the same one reloads.
func TestCommentsShareOneTab(t *testing.T) {
	page := &fakeCommentPage{results: []commentList{
		{Recognized: true}, {Recognized: true}, {Recognized: true},
	}}
	source := newTestCommentSource(page, newFakeCommentClient(), nil)

	for _, number := range []int{7, 8, 8} {
		if _, err := source.ListComments(context.Background(), "owner/repo", number); err != nil {
			t.Fatalf("ListComments(#%d): %v", number, err)
		}
	}
	want := []string{"https://github.com/owner/repo/issues/7", "https://github.com/owner/repo/issues/8"}
	if len(page.navigated) != len(want) {
		t.Fatalf("navigated %v, want %v", page.navigated, want)
	}
	for i, url := range want {
		if page.navigated[i] != url {
			t.Fatalf("navigation %d = %q, want %q", i, page.navigated[i], url)
		}
	}
	if page.reloads != 1 {
		t.Fatalf("reloaded %d time(s), want 1 for the repeated conversation", page.reloads)
	}
}

// Posting has no page affordance to drive, so it stays on the API client.
func TestCommentsPostThroughTheAPI(t *testing.T) {
	api := newFakeCommentClient()
	source := newTestCommentSource(&fakeCommentPage{}, api, nil)

	if err := source.PostComment(context.Background(), "owner/repo", 7, "Starting work on this issue /aviary:AAAA"); err != nil {
		t.Fatalf("PostComment: %v", err)
	}
	if api.posts != 1 {
		t.Fatalf("the API took %d post(s), want 1", api.posts)
	}
}

// The MCP update-issue tool and the prompt agent's update action read the
// author and post through the wrapped API client, so browser mode can post
// updates like any other instance (issue #832).
func TestCommentsPostUpdatesThroughTheAPI(t *testing.T) {
	api := &fakeUpdateClient{fakeCommentClient: newFakeCommentClient(), author: "octo"}
	source := newTestCommentSource(&fakeCommentPage{}, api, nil)

	author, err := source.IssueAuthor(context.Background(), "owner/repo", 7)
	if err != nil || author != "octo" {
		t.Fatalf("IssueAuthor = %q, %v", author, err)
	}
	url, err := source.PostCommentURL(context.Background(), "owner/repo", 7, "@octo update")
	if err != nil || url != "https://github.com/owner/repo/issues/7#issuecomment-1" {
		t.Fatalf("PostCommentURL = %q, %v", url, err)
	}
	if api.posts != 1 {
		t.Fatalf("the API took %d post(s), want 1", api.posts)
	}

	bare := newTestCommentSource(&fakeCommentPage{}, newFakeCommentClient(), nil)
	if _, err := bare.IssueAuthor(context.Background(), "owner/repo", 7); err == nil || !strings.Contains(err.Error(), "cannot read an issue's author") {
		t.Fatalf("IssueAuthor without the capability: %v", err)
	}
	if _, err := bare.PostCommentURL(context.Background(), "owner/repo", 7, "x"); err == nil || !strings.Contains(err.Error(), "cannot report a posted comment's URL") {
		t.Fatalf("PostCommentURL without the capability: %v", err)
	}
}

// fakeUpdateClient is an API client that can also post updates.
type fakeUpdateClient struct {
	*fakeCommentClient
	author string
}

func (f *fakeUpdateClient) IssueAuthor(context.Context, string, int) (string, error) {
	return f.author, nil
}

func (f *fakeUpdateClient) PostCommentURL(ctx context.Context, repo string, number int, body string) (string, error) {
	if err := f.PostComment(ctx, repo, number, body); err != nil {
		return "", err
	}
	return fmt.Sprintf("https://github.com/%s/issues/%d#issuecomment-1", repo, number), nil
}

// Reacting has the same "no page affordance" shape as posting: GitHub's
// conversation page offers nothing to drive it, so it always goes through
// the wrapped API client (issue #581).
func TestCommentsReactThroughTheAPI(t *testing.T) {
	api := newFakeCommentClient()
	source := newTestCommentSource(&fakeCommentPage{}, api, nil)

	if err := source.AddReaction(context.Background(), "owner/repo", 42, "eyes"); err != nil {
		t.Fatalf("AddReaction: %v", err)
	}
	if len(api.reactions) != 1 || api.reactions[0] != "42:eyes" {
		t.Fatalf("reactions = %v, want [42:eyes]", api.reactions)
	}
}

// The reaps that negotiate contested issues run concurrently, and they all read
// through the one shared tab: without serialization they navigate it out from
// under each other.
func TestCommentsSerializeConcurrentReads(t *testing.T) {
	results := make([]commentList, 16)
	for i := range results {
		results[i] = commentList{Recognized: true}
	}
	page := &fakeCommentPage{results: results}
	source := newTestCommentSource(page, newFakeCommentClient(), nil)

	var wg sync.WaitGroup
	for i := 0; i < len(results); i++ {
		wg.Add(1)
		go func(number int) {
			defer wg.Done()
			if _, err := source.ListComments(context.Background(), "owner/repo", number); err != nil {
				t.Errorf("ListComments(#%d): %v", number, err)
			}
		}(i + 1)
	}
	wg.Wait()
	if page.evals != len(results) {
		t.Fatalf("evaluated %d time(s), want %d", page.evals, len(results))
	}
}

// A read cancelled with the run must stop rather than sitting out the settle
// budget, and must not be laundered into an API call on the way out.
func TestCommentsCancelledReadStops(t *testing.T) {
	page := &fakeCommentPage{results: []commentList{{}, {}}}
	api := newFakeCommentClient()
	source := newTestCommentSource(page, api, nil)
	source.settleAttempts = 5
	source.sleep = func(context.Context, time.Duration) bool { return false }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := source.ListComments(ctx, "owner/repo", 7); err == nil {
		t.Fatal("a cancelled read returned no error")
	}
	if api.lists != 0 {
		t.Fatalf("a cancelled read cost %d API call(s)", api.lists)
	}
}

// fakeCommentClient stands in for the API client the reader falls back to when
// a conversation cannot be read off the page, counting the reads and posts that
// reached it so a test can tell a page read from a fallback.
type fakeCommentClient struct {
	mu        sync.Mutex
	comments  map[string][]core.Comment
	posts     int
	lists     int
	postErr   error
	listErr   error
	reactions []string
}

func newFakeCommentClient() *fakeCommentClient {
	return &fakeCommentClient{comments: make(map[string][]core.Comment)}
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
	f.comments[key] = append(f.comments[key], core.Comment{Body: body, CreatedAt: time.Now()})
	return nil
}

func (f *fakeCommentClient) ListComments(_ context.Context, repo string, number int) ([]core.Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lists++
	if f.listErr != nil {
		return nil, f.listErr
	}
	key := f.key(repo, number)
	out := make([]core.Comment, len(f.comments[key]))
	copy(out, f.comments[key])
	return out, nil
}

func (f *fakeCommentClient) AddReaction(_ context.Context, _ string, commentID int64, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reactions = append(f.reactions, fmt.Sprintf("%d:%s", commentID, content))
	return nil
}
