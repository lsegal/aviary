package projects

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// promptReplyMarker ends every comment glorp posts for the prompt agent in
// answer to a routed comment (issue #842). Comment detection never treats a
// comment carrying it as a trigger, so a reply can never start the routing
// over, whatever its text says.
const promptReplyMarker = "<!-- glorp:prompt-reply -->"

// isPromptReply reports whether body is a reply the prompt agent posted
// through glorp, which comment detection exempts from triggering.
func isPromptReply(body string) bool {
	return strings.Contains(body, promptReplyMarker)
}

// mentionStart matches an @ that GitHub could read as the start of a mention:
// one followed by a login, a team, or a /glorp: identity.
var mentionStart = regexp.MustCompile(`@([A-Za-z0-9/])`)

// neutralizeMentions breaks every @mention in text with a zero-width space,
// so a prompt-agent reply notifies nobody and no comment detection reads it
// as addressed to anyone (issue #842).
func neutralizeMentions(text string) string {
	return mentionStart.ReplaceAllString(text, "@​$1")
}

// routedComment is a comment that comment detection found addressed to an
// issue's job, kept so the prompt agent can be pointed at it (issue #842).
type routedComment struct {
	Kind CommentKind
	// Repository and Number are where the comment was posted: the issue, or
	// a pull request that works on it.
	Repository string
	Number     int
	ID         int64
	Author     string
}

// URL links to the comment on GitHub, or to the thread it is on when its ID
// is unknown.
func (c routedComment) URL() string {
	base := "https://github.com/" + c.Repository
	number := strconv.Itoa(c.Number)
	switch {
	case c.ID == 0 && c.Kind == ConversationComment:
		return base + "/issues/" + number
	case c.ID == 0:
		return base + "/pull/" + number
	case c.Kind == ReviewComment:
		return base + "/pull/" + number + "#discussion_r" + strconv.FormatInt(c.ID, 10)
	case c.Kind == ReviewBody:
		return base + "/pull/" + number + "#pullrequestreview-" + strconv.FormatInt(c.ID, 10)
	}
	return base + "/issues/" + number + "#issuecomment-" + strconv.FormatInt(c.ID, 10)
}

// commentRoute is one comment handed to the prompt agent to decide on: the
// job it concerns, the issue that job works, and the comment.
type commentRoute struct {
	JobKey     string
	Repository string
	Number     int
	Comment    routedComment
	// StaysPaused marks a job paused on its dependencies or by its owner:
	// only the prompt agent's own decision may restart it (issue #844).
	StaysPaused bool
}

// request is what the prompt area shows for a routed comment.
func (r commentRoute) request() string {
	return fmt.Sprintf("look at comment %s on issue %s#%d and determine next steps", r.Comment.URL(), r.Repository, r.Number)
}

// routesCommentsToPrompt reports whether comments on a job's issue go to the
// prompt agent. Without a prompt agent that can run, they restart the job
// as they did before issue #842.
func (w *Glorp) routesCommentsToPrompt() bool {
	if _, err := w.promptSpec(""); err != nil {
		return false
	}
	_, err := w.promptRunner()
	return err == nil
}

// routeCommentToPrompt asks the prompt agent what a new comment on a job's
// issue calls for (issue #842), instead of restarting the job from scratch.
// The agent restarts the job or replies to the comment. When it cannot
// answer at all, the job is retried, so the comment is never dropped.
func (w *Glorp) routeCommentToPrompt(ctx context.Context, route commentRoute) {
	entry, earlier, err := w.startPrompt(route.request())
	if err != nil {
		w.logf("%s failed to route comment %s to the prompt agent: %v", route.JobKey, route.Comment.URL(), err)
		return
	}
	entry = w.answerPrompt(ctx, entry, earlier, "", &route)
	if entry.Status != "failed" || len(entry.Actions) > 0 || ctx.Err() != nil {
		return
	}
	if route.StaysPaused {
		w.logf("%s prompt agent could not decide on comment %s; the job stays paused", route.JobKey, route.Comment.URL())
		return
	}
	w.logf("%s prompt agent could not decide on comment %s; retrying the job instead", route.JobKey, route.Comment.URL())
	w.applyPromptAction(ctx, promptAction{Kind: "retry", Key: route.JobKey}, "", &route)
}

// postPromptReply posts the prompt agent's reply to a routed comment on the
// thread the comment is on, with every mention broken and the reply marker
// that keeps comment detection from routing it again.
func (w *Glorp) postPromptReply(ctx context.Context, route commentRoute, reply string) (string, error) {
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return "", fmt.Errorf("the prompt agent's answer is empty, so there is nothing to post")
	}
	if w.Comments == nil {
		return "", fmt.Errorf("this instance has no GitHub comment client, so it cannot post comments")
	}
	body := neutralizeMentions(reply) + "\n\n" + promptReplyMarker
	repo, number := route.Comment.Repository, route.Comment.Number
	if poster, ok := w.Comments.(interface {
		PostCommentURL(context.Context, string, int, string) (string, error)
	}); ok {
		return poster.PostCommentURL(ctx, repo, number, body)
	}
	if err := w.Comments.PostComment(ctx, repo, number, body); err != nil {
		return "", err
	}
	return fmt.Sprintf("https://github.com/%s/issues/%d", repo, number), nil
}
