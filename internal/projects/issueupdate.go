package projects

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/lsegal/aviary/internal/projects/core"
)

// GHCLI posts updates directly; browser mode wraps it and passes them through.
var _ core.IssueUpdatePoster = GHCLI{}

// IssueAuthor reads who opened repo#number. The issues endpoint answers for
// pull requests too, since every pull request is also an issue.
func (g GHCLI) IssueAuthor(ctx context.Context, repo string, number int) (string, error) {
	output, err := g.run(ctx, "api", "repos/"+repo+"/issues/"+strconv.Itoa(number), "--jq", ".user.login")
	if err != nil {
		return "", fmt.Errorf("read the author of %s#%d: %w: %s", repo, number, err, strings.TrimSpace(string(output)))
	}
	login := strings.TrimSpace(string(output))
	if login == "" {
		return "", fmt.Errorf("read the author of %s#%d: GitHub named none", repo, number)
	}
	return login, nil
}

// PostCommentURL posts body on repo#number and reports the comment's URL.
func (g GHCLI) PostCommentURL(ctx context.Context, repo string, number int, body string) (string, error) {
	output, err := g.run(ctx, "api", "repos/"+repo+"/issues/"+strconv.Itoa(number)+"/comments", "-f", "body="+body, "--jq", ".html_url")
	if err != nil {
		return "", fmt.Errorf("post comment on %s#%d: %w: %s", repo, number, err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

// PostIssueUpdate posts update as a comment on its issue or pull request that
// mentions the author and this instance, the way the stuck-job watchdog's
// unblock agent reaches a job: aviary's mention path delivers it
// to the owning job. It reports the comment's URL.
func (w *Engine) PostIssueUpdate(ctx context.Context, update core.IssueUpdate) (core.IssueUpdateResult, error) {
	text := strings.TrimSpace(update.Text)
	switch {
	case !core.ValidRepo(update.Repository):
		return core.IssueUpdateResult{}, fmt.Errorf("repository %q is not OWNER/REPO", update.Repository)
	case update.Number <= 0:
		return core.IssueUpdateResult{}, fmt.Errorf("an issue or pull request number is required")
	case text == "":
		return core.IssueUpdateResult{}, fmt.Errorf("the update text is empty")
	}
	if w.Comments == nil {
		return core.IssueUpdateResult{}, fmt.Errorf("this instance has no GitHub comment client, so it cannot post updates; it needs gh installed and signed in (gh auth status)")
	}
	client, ok := w.Comments.(core.IssueUpdatePoster)
	if !ok {
		return core.IssueUpdateResult{}, fmt.Errorf("this instance's comment client (%T) cannot read an issue's author or report a posted comment's URL, which posting an update needs", w.Comments)
	}
	author, err := client.IssueAuthor(ctx, update.Repository, update.Number)
	if err != nil {
		return core.IssueUpdateResult{}, err
	}
	body := fmt.Sprintf("@%s @/aviary:%s\n\n%s", author, w.Identity, text)
	url, err := client.PostCommentURL(ctx, update.Repository, update.Number, body)
	if err != nil {
		return core.IssueUpdateResult{}, err
	}
	w.logf("posted an update on %s#%d for @%s: %s", update.Repository, update.Number, author, url)
	return core.IssueUpdateResult{URL: url, Author: author}, nil
}
