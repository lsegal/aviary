package browser

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

// fakeReviewClient is an API client that can also post attachments, react to
// review comments and reviews, and list review comments, recording each call
// so a test can tell the source forwarded it unchanged.
type fakeReviewClient struct {
	*fakeCommentClient
	err       error
	calls     []string
	files     []string
	reviewOut []core.Comment
}

func (f *fakeReviewClient) PostCommentWithAttachments(_ context.Context, repo string, _ int, body string, files []string) (string, error) {
	f.calls = append(f.calls, "attach:"+repo+":"+body)
	f.files = files
	if f.err != nil {
		return "", f.err
	}
	return "https://github.com/" + repo + "/issues/7#issuecomment-9", nil
}

func (f *fakeReviewClient) AddReviewCommentReaction(_ context.Context, repo string, _ int64, content string) error {
	f.calls = append(f.calls, "review-comment:"+repo+":"+content)
	return f.err
}

func (f *fakeReviewClient) AddReviewReaction(_ context.Context, repo string, nodeID string, content string) error {
	f.calls = append(f.calls, "review:"+repo+":"+nodeID+":"+content)
	return f.err
}

func (f *fakeReviewClient) ListReviewComments(_ context.Context, repo string, _ int) ([]core.Comment, error) {
	f.calls = append(f.calls, "list-review:"+repo)
	if f.err != nil {
		return nil, f.err
	}
	return f.reviewOut, nil
}

// Posting a comment with attachments has no page affordance either, so it
// goes through the wrapped API client and reports the URL it returns.
func TestCommentsPostAttachmentsThroughTheAPI(t *testing.T) {
	api := &fakeReviewClient{fakeCommentClient: newFakeCommentClient()}
	source := newTestCommentSource(&fakeCommentPage{}, api, nil)

	url, err := source.PostCommentWithAttachments(context.Background(), "owner/repo", 7, "see shot", []string{"a.png", "b.mp4"})
	if err != nil {
		t.Fatalf("PostCommentWithAttachments: %v", err)
	}
	if want := "https://github.com/owner/repo/issues/7#issuecomment-9"; url != want {
		t.Fatalf("url = %q, want %q", url, want)
	}
	if !reflect.DeepEqual(api.files, []string{"a.png", "b.mp4"}) {
		t.Fatalf("files forwarded = %v", api.files)
	}

	api.err = errors.New("upload failed")
	if _, err := source.PostCommentWithAttachments(context.Background(), "owner/repo", 7, "x", nil); !errors.Is(err, api.err) {
		t.Fatalf("PostCommentWithAttachments error = %v, want %v", err, api.err)
	}

	bare := newTestCommentSource(&fakeCommentPage{}, newFakeCommentClient(), nil)
	if _, err := bare.PostCommentWithAttachments(context.Background(), "owner/repo", 7, "x", nil); err == nil || !strings.Contains(err.Error(), "cannot upload attachments") {
		t.Fatalf("PostCommentWithAttachments without the capability: %v", err)
	}
}

// Review reactions go through the API client when it can make them, surface
// its errors, and are silent no-ops when it cannot.
func TestCommentsReactToReviewsThroughTheAPI(t *testing.T) {
	api := &fakeReviewClient{fakeCommentClient: newFakeCommentClient()}
	source := newTestCommentSource(&fakeCommentPage{}, api, nil)
	ctx := context.Background()

	if err := source.AddReviewCommentReaction(ctx, "owner/repo", 5, "eyes"); err != nil {
		t.Fatalf("AddReviewCommentReaction: %v", err)
	}
	if err := source.AddReviewReaction(ctx, "owner/repo", "PRR_1", "rocket"); err != nil {
		t.Fatalf("AddReviewReaction: %v", err)
	}
	if want := []string{"review-comment:owner/repo:eyes", "review:owner/repo:PRR_1:rocket"}; !reflect.DeepEqual(api.calls, want) {
		t.Fatalf("calls = %v, want %v", api.calls, want)
	}

	api.err = errors.New("forbidden")
	if err := source.AddReviewCommentReaction(ctx, "owner/repo", 5, "eyes"); !errors.Is(err, api.err) {
		t.Fatalf("AddReviewCommentReaction error = %v", err)
	}
	if err := source.AddReviewReaction(ctx, "owner/repo", "PRR_1", "eyes"); !errors.Is(err, api.err) {
		t.Fatalf("AddReviewReaction error = %v", err)
	}

	plain := newFakeCommentClient()
	bare := newTestCommentSource(&fakeCommentPage{}, plain, nil)
	if err := bare.AddReviewCommentReaction(ctx, "owner/repo", 5, "eyes"); err != nil {
		t.Fatalf("AddReviewCommentReaction without the capability: %v", err)
	}
	if err := bare.AddReviewReaction(ctx, "owner/repo", "PRR_1", "eyes"); err != nil {
		t.Fatalf("AddReviewReaction without the capability: %v", err)
	}
	if len(plain.reactions) != 0 {
		t.Fatalf("a client without review reactions took %v", plain.reactions)
	}
}

// Review comments are read through the API client, never the page, and an
// API client that cannot list them yields nothing rather than an error.
func TestCommentsListReviewCommentsThroughTheAPI(t *testing.T) {
	want := []core.Comment{{ID: 3, Author: "alice", Body: "nit"}}
	api := &fakeReviewClient{fakeCommentClient: newFakeCommentClient(), reviewOut: want}
	page := &fakeCommentPage{}
	source := newTestCommentSource(page, api, nil)
	ctx := context.Background()

	got, err := source.ListReviewComments(ctx, "owner/repo", 7)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("ListReviewComments = %+v, %v; want %+v", got, err, want)
	}
	if len(page.navigated) != 0 || page.evals != 0 {
		t.Fatalf("listing review comments touched the page: navigated %v, %d eval(s)", page.navigated, page.evals)
	}

	api.err = errors.New("boom")
	if _, err := source.ListReviewComments(ctx, "owner/repo", 7); !errors.Is(err, api.err) {
		t.Fatalf("ListReviewComments error = %v", err)
	}

	bare := newTestCommentSource(&fakeCommentPage{}, newFakeCommentClient(), nil)
	if got, err := bare.ListReviewComments(ctx, "owner/repo", 7); got != nil || err != nil {
		t.Fatalf("ListReviewComments without the capability = %+v, %v", got, err)
	}
}

// Unset settle knobs fall back to the defaults; explicit ones win.
func TestCommentsSettleDefaults(t *testing.T) {
	var s CommentSource
	if got := s.attempts(); got != defaultCommentsSettleAttempts {
		t.Fatalf("default attempts = %d, want %d", got, defaultCommentsSettleAttempts)
	}
	if got := s.delay(); got != defaultCommentsSettleDelay {
		t.Fatalf("default delay = %v, want %v", got, defaultCommentsSettleDelay)
	}
	s.settleAttempts, s.settleDelay = 3, 7*time.Millisecond
	if s.attempts() != 3 || s.delay() != 7*time.Millisecond {
		t.Fatalf("explicit attempts/delay = %d/%v", s.attempts(), s.delay())
	}
}

// Without the test seam, pause waits out the delay, and stops early when the
// context is cancelled rather than sitting out the wait.
func TestCommentsPauseUsesRealTimer(t *testing.T) {
	s := &CommentSource{settleDelay: time.Millisecond}
	if !s.pause(context.Background()) {
		t.Fatal("pause reported cancellation after the delay elapsed")
	}

	s.settleDelay = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan bool, 1)
	go func() { done <- s.pause(ctx) }()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("pause reported success for a cancelled context")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("pause did not return after the context was cancelled")
	}
}
