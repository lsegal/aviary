package core

import (
	"context"
	"time"
)

// Comment is a single issue or pull request comment relevant to the
// cooperative handoff protocol.
type Comment struct {
	Body      string
	Author    string
	CreatedAt time.Time
}

// CommentPoster posts a comment to an issue or pull request.
type CommentPoster interface {
	PostComment(ctx context.Context, repo string, number int, body string) error
}

// CommentLister lists the comments on an issue or pull request.
type CommentLister interface {
	ListComments(ctx context.Context, repo string, number int) ([]Comment, error)
}

// CommentClient is the combined capability needed to run the handoff
// handshake described in issue #214.
type CommentClient interface {
	CommentPoster
	CommentLister
}

// CommentReactor adds an emoji reaction to a specific comment, identified by
// its GitHub comment ID. It is kept separate from CommentClient so a driver
// with no reaction affordance (a fake in a test, or a future comment source
// with no API to react through) is not forced to implement it (issue #581).
type CommentReactor interface {
	AddReaction(ctx context.Context, repo string, commentID int64, content string) error
}

// ReviewReactor reacts to pull request review comments and review bodies,
// which the issue comment reaction endpoint does not reach (issue #695). A
// review body has no REST reaction endpoint, so it is named by its GraphQL
// node ID.
type ReviewReactor interface {
	AddReviewCommentReaction(ctx context.Context, repo string, commentID int64, content string) error
	AddReviewReaction(ctx context.Context, repo string, nodeID string, content string) error
}

// ReviewCommentLister lists the inline review comments and submitted review
// bodies on a pull request, oldest first, so a run in progress hears a
// mention posted in a review of the pull request it opened (issue #695).
type ReviewCommentLister interface {
	ListReviewComments(ctx context.Context, repo string, number int) ([]Comment, error)
}

// IssueUpdatePoster is the GitHub access the MCP update-issue tool and the
// prompt agent's update action need (issues #806 and #832): the author of an
// issue or pull request, and posting a comment that reports its URL. Every
// comment client glorp wraps another in must pass it through, or updates stop
// working in the mode that wraps it.
type IssueUpdatePoster interface {
	IssueAuthor(ctx context.Context, repo string, number int) (string, error)
	PostCommentURL(ctx context.Context, repo string, number int, body string) (string, error)
}
