package browser

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

// The conversation page is a client-rendered React app, so an extraction that
// runs the instant navigation finishes can land before the comments exist in
// the DOM. These bound the wait for it, mirroring the issues-list reader's.
const (
	defaultCommentsSettleAttempts = 60
	defaultCommentsSettleDelay    = 250 * time.Millisecond
)

// commentsTab is the name of the one tab every comment read is made in.
// Comments are read per issue rather than per target, so a tab per issue would
// leak a renderer process for every ticket the run ever negotiated; the single
// tab is navigated to each conversation in turn and reads are serialized on it.
const commentsTab = "comments"

// CommentSource reads an issue's or pull request's comments off its
// rendered GitHub conversation page instead of calling the API. The handoff
// handshake polls comments on every active issue -- once per contested
// candidate per reap, and again on a timer for the whole time an agent is
// running (see watchForCompetingClaim) -- which is the heaviest API user left
// in browser mode and exactly the kind of read browser mode exists to move onto
// a page (issue #441).
//
// Only the read moves. Posting a comment has no page affordance Aviary could
// drive, so PostComment still goes through the API client this wraps, and that
// same client answers any read the page could not produce: the handshake
// decides who owns a ticket, so a page that failed to load, came back signed
// out, or rendered markup the extractor does not recognise falls back to the
// API rather than reporting a conversation as having no comments and letting
// two instances claim the same work.
type CommentSource struct {
	// pageFor opens (or returns the already-open) tab comments are read in.
	pageFor func(name string) (Page, error)
	// api posts every comment and answers reads the page could not.
	api  core.CommentClient
	logf func(string, ...interface{})
	// settleAttempts and settleDelay bound the wait for the client-rendered
	// conversation to draw. sleep is a test seam for that wait.
	settleAttempts int
	settleDelay    time.Duration
	sleep          func(context.Context, time.Duration) bool

	// tab serializes reads: the reaps that negotiate contested issues run
	// concurrently, and they would otherwise navigate the one shared tab out
	// from under each other.
	tab sync.Mutex

	mu sync.Mutex
	// reported remembers which conversation URLs have already had a fallback
	// logged, so a page Aviary cannot read is reported once rather than on
	// every poll of a ticket an agent is working.
	reported map[string]bool
	// lastURL is the URL the tab was last pointed at, so a repeated read of
	// the same conversation reloads the tab instead of navigating it again.
	lastURL string
}

// commentRow is one comment as the page script reports it.
type commentRow struct {
	ID        string `json:"id"`
	Author    string `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
}

// commentList is the page script's result: the comments it found and
// whether it recognised the page as a conversation at all.
type commentList struct {
	Comments   []commentRow `json:"comments"`
	Recognized bool         `json:"recognized"`
}

// NewCommentSource builds the comment client browser mode runs the handoff
// handshake through, reading conversations in one tab of the shared browser and
// keeping api for writes and for reads the page could not answer.
func NewCommentSource(browser *Browser, api core.CommentClient, logf func(string, ...interface{})) *CommentSource {
	return &CommentSource{
		pageFor: func(name string) (Page, error) {
			tab, err := browser.Tab(name)
			if err != nil {
				return nil, err
			}
			return tab, nil
		},
		api:      api,
		logf:     logf,
		reported: map[string]bool{},
	}
}

// PostComment writes through the API client: GitHub's conversation page offers
// no affordance Aviary could drive to post one.
func (s *CommentSource) PostComment(ctx context.Context, repo string, number int, body string) error {
	return s.api.PostComment(ctx, repo, number, body)
}

// IssueAuthor reads through the API client: the MCP update-issue tool and the
// prompt agent's update action mention the author in the comment they post,
// and that mention is what delivers the update to the owning job (issue #832).
func (s *CommentSource) IssueAuthor(ctx context.Context, repo string, number int) (string, error) {
	poster, ok := s.api.(core.IssueUpdatePoster)
	if !ok {
		return "", fmt.Errorf("the GitHub API client (%T) cannot read an issue's author", s.api)
	}
	return poster.IssueAuthor(ctx, repo, number)
}

// PostCommentURL posts through the API client, same as PostComment, and
// reports the posted comment's URL (issue #832).
func (s *CommentSource) PostCommentURL(ctx context.Context, repo string, number int, body string) (string, error) {
	poster, ok := s.api.(core.IssueUpdatePoster)
	if !ok {
		return "", fmt.Errorf("the GitHub API client (%T) cannot report a posted comment's URL", s.api)
	}
	return poster.PostCommentURL(ctx, repo, number, body)
}

// PostCommentWithAttachments writes through the API client, same as
// PostCommentURL.
func (s *CommentSource) PostCommentWithAttachments(ctx context.Context, repo string, number int, body string, files []string) (string, error) {
	poster, ok := s.api.(core.AttachmentCommentPoster)
	if !ok {
		return "", fmt.Errorf("the GitHub API client (%T) cannot upload attachments", s.api)
	}
	return poster.PostCommentWithAttachments(ctx, repo, number, body, files)
}

// AddReaction writes through the API client, same as PostComment: the
// conversation page offers no affordance Aviary could drive to react with. It
// is a no-op when the wrapped client has no reaction capability.
func (s *CommentSource) AddReaction(ctx context.Context, repo string, commentID int64, content string) error {
	reactor, ok := s.api.(core.CommentReactor)
	if !ok {
		return nil
	}
	return reactor.AddReaction(ctx, repo, commentID, content)
}

// AddReviewCommentReaction writes through the API client, same as
// AddReaction, and is a no-op when it cannot react to review comments.
func (s *CommentSource) AddReviewCommentReaction(ctx context.Context, repo string, commentID int64, content string) error {
	reactor, ok := s.api.(core.ReviewReactor)
	if !ok {
		return nil
	}
	return reactor.AddReviewCommentReaction(ctx, repo, commentID, content)
}

// AddReviewReaction writes through the API client, same as AddReaction, and
// is a no-op when it cannot react to reviews.
func (s *CommentSource) AddReviewReaction(ctx context.Context, repo string, nodeID string, content string) error {
	reactor, ok := s.api.(core.ReviewReactor)
	if !ok {
		return nil
	}
	return reactor.AddReviewReaction(ctx, repo, nodeID, content)
}

// ListReviewComments reads a pull request's review comments and review bodies
// through the API client. The conversation page this source reads folds
// inline comments into collapsed review threads, so the API is the reliable
// reader. It returns nothing when the API client cannot list them.
func (s *CommentSource) ListReviewComments(ctx context.Context, repo string, number int) ([]core.Comment, error) {
	lister, ok := s.api.(core.ReviewCommentLister)
	if !ok {
		return nil, nil
	}
	return lister.ListReviewComments(ctx, repo, number)
}

// commentsURL is the conversation page for an issue or a pull request.
// GitHub redirects /issues/N to /pull/N for a pull request, so the issue form
// reaches both, which matters because the handoff protocol negotiates ownership
// on whichever of the two is the live conversation.
func commentsURL(repo string, number int) string {
	return "https://github.com/" + repo + "/issues/" + strconv.Itoa(number)
}

// ListComments reads repo#number's conversation page and returns its comments
// in creation order. A conversation that rendered with no comments yields an
// empty slice; anything else falls back to the API client.
func (s *CommentSource) ListComments(ctx context.Context, repo string, number int) ([]core.Comment, error) {
	pageURL := commentsURL(repo, number)
	comments, err := s.readComments(ctx, pageURL)
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		s.reportFallback(pageURL, err)
		return s.api.ListComments(ctx, repo, number)
	}
	return comments, nil
}

// readComments drives the shared tab through one conversation page.
func (s *CommentSource) readComments(ctx context.Context, pageURL string) ([]core.Comment, error) {
	s.tab.Lock()
	defer s.tab.Unlock()
	page, err := s.pageFor(commentsTab)
	if err != nil {
		return nil, err
	}
	if err := s.visit(page, pageURL); err != nil {
		return nil, fmt.Errorf("load conversation at %s: %w", pageURL, err)
	}
	if status := page.HTTPStatus(); status >= 400 {
		return nil, fmt.Errorf("load conversation at %s: GitHub returned HTTP %d", pageURL, status)
	}
	// The conversation draws client-side, so the first evaluation after a
	// navigation usually lands before React has rendered anything: no comments
	// and no conversation marker, which is indistinguishable from markup Aviary
	// cannot read. The read is retried until the page recognises itself, so a
	// page that has drawn costs a single evaluation.
	var list commentList
	for attempt := 0; ; attempt++ {
		var read commentList
		if err := page.Eval(commentsScript, &read); err != nil {
			return nil, fmt.Errorf("read conversation at %s: %w", pageURL, err)
		}
		list = read
		if list.Recognized || attempt >= s.attempts()-1 {
			break
		}
		if !s.pause(ctx) {
			return nil, ctx.Err()
		}
	}
	if !list.Recognized {
		return nil, fmt.Errorf("read conversation at %s: it did not render within %s (GitHub may be failing to serve the page, or its markup may have changed)", pageURL, time.Duration(s.attempts())*s.delay())
	}
	// A conversation that rendered nothing is where a signed-out profile
	// hides: a private repository's page is a login wall, and the handshake
	// must not read that as an unclaimed ticket.
	if len(list.Comments) == 0 && signedOutPage(page) {
		return nil, fmt.Errorf("read conversation at %s: the browser profile is signed out", pageURL)
	}
	return commentsFromRows(pageURL, list.Comments)
}

// commentsFromRows converts the extracted rows into Comments, in
// creation order. A row whose timestamp did not parse fails the whole read: the
// handshake compares comment times to decide who owns a ticket, and a comment
// silently carrying the zero time reads as older than every claim, which is a
// wrong answer rather than a missing one.
func commentsFromRows(pageURL string, rows []commentRow) ([]core.Comment, error) {
	comments := make([]core.Comment, 0, len(rows))
	for _, row := range rows {
		createdAt, err := time.Parse(time.RFC3339, row.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("read conversation at %s: comment %s carried no readable timestamp (%q)", pageURL, row.ID, row.CreatedAt)
		}
		comment := core.Comment{Body: row.Body, Author: row.Author, CreatedAt: createdAt}
		if id, err := strconv.ParseInt(row.ID, 10, 64); err == nil {
			comment.ID = id
			comment.URL = strings.SplitN(pageURL, "#", 2)[0] + "#issuecomment-" + row.ID
		}
		comments = append(comments, comment)
	}
	// The page renders comments oldest first, as the API returns them, but the
	// order is what tells the newest claim from an older one, so it is sorted
	// rather than assumed.
	sort.SliceStable(comments, func(i, j int) bool { return comments[i].CreatedAt.Before(comments[j].CreatedAt) })
	return comments, nil
}

// visit navigates the shared tab, or reloads it when it already shows the
// conversation being read.
func (s *CommentSource) visit(page Page, pageURL string) error {
	s.mu.Lock()
	unchanged := s.lastURL == pageURL
	s.lastURL = pageURL
	s.mu.Unlock()
	if unchanged {
		return page.Reload()
	}
	return page.Navigate(pageURL)
}

// attempts is how many times one page load is read before it is given up on.
func (s *CommentSource) attempts() int {
	if s.settleAttempts > 0 {
		return s.settleAttempts
	}
	return defaultCommentsSettleAttempts
}

// delay is how long the reader waits between those attempts.
func (s *CommentSource) delay() time.Duration {
	if s.settleDelay > 0 {
		return s.settleDelay
	}
	return defaultCommentsSettleDelay
}

// pause waits between settle attempts, reporting false when the run is being
// shut down so a cancelled read stops instead of sitting out the whole wait.
func (s *CommentSource) pause(ctx context.Context) bool {
	if s.sleep != nil {
		return s.sleep(ctx, s.delay())
	}
	timer := time.NewTimer(s.delay())
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// reportFallback logs a page read that went to the API instead, once per
// conversation URL, so a ticket an agent is working does not repeat the same
// line on every claim check.
func (s *CommentSource) reportFallback(pageURL string, cause error) {
	s.mu.Lock()
	report := !s.reported[pageURL]
	s.reported[pageURL] = true
	s.mu.Unlock()
	if report && s.logf != nil {
		s.logf("reading comments from %s through the API instead: %v", pageURL, cause)
	}
}
