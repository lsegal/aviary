package projects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/projects/core"
)

// requestMediaMarker marks a comment that posted a filing request's media,
// so a backfill run never posts it twice.
const requestMediaMarker = "<!-- aviary:request-media -->"

// requestMediaText is the comment a backfill posts with the media.
const requestMediaText = "These screenshots were originally sent with the request that filed this issue.\n\n" + requestMediaMarker

// Backfill outcomes for one issue.
const (
	BackfillPosted        = "posted"
	BackfillWouldPost     = "would_post"
	BackfillAlreadyPosted = "already_posted"
	BackfillNoMedia       = "no_media"
	BackfillFailed        = "failed"
)

// BackfillResult is what a media backfill did for one issue.
type BackfillResult struct {
	Number      int      `json:"number"`
	Status      string   `json:"status"`
	Reason      string   `json:"reason,omitempty"`
	URL         string   `json:"url,omitempty"`
	Prompt      string   `json:"prompt,omitempty"` // the ID of the user message that filed it
	Attachments []string `json:"attachments,omitempty"`
	Skipped     []string `json:"skipped,omitempty"` // the request's media GitHub does not take
}

// filedIssueURL matches the issue URL a project_issue_create result reports.
var filedIssueURL = regexp.MustCompile(`^https://github\.com/([^/]+/[^/]+)/issues/(\d+)$`)

// BackfillIssueMedia is project_issue_backfill_media: post the images and
// videos sent with the request that filed each of repository's issues,
// before project_issue_create attached them, as one comment per issue. Each
// issue is matched to the user message whose turn called
// project_issue_create and got its URL back. An issue that already has the
// comment, or whose media cannot be found, is reported and left alone, so a
// rerun posts nothing new. dryRun reports what would be posted.
func (m *Manager) BackfillIssueMedia(ctx context.Context, repository string, numbers []int, dryRun bool) (map[string]any, error) {
	engine := m.engine()
	if engine == nil {
		return nil, ErrNotWatching
	}
	if len(numbers) == 0 {
		return nil, errors.New("name the issue numbers to backfill")
	}
	if repository == "" {
		resolved, err := onlyRepository(m.currentSnapshot())
		if err != nil {
			return nil, errors.New("name the repository the issues are on")
		}
		repository = resolved
	}
	if !core.ValidRepo(repository) {
		return nil, fmt.Errorf("repository %q is not OWNER/REPO", repository)
	}
	turns, err := agent.FindToolCallTurns("project_issue_create")
	if err != nil {
		return nil, fmt.Errorf("read session history: %w", err)
	}
	filings := issueFilingTurns(turns, repository)
	results := make([]BackfillResult, 0, len(numbers))
	for _, number := range numbers {
		results = append(results, engine.backfillIssueMedia(ctx, repository, number, filings[number], dryRun))
	}
	return map[string]any{"repository": repository, "dry_run": dryRun, "issues": results}, nil
}

// issueFilingTurns maps each of repository's issue numbers to the earliest
// project_issue_create call that reported filing it. Later calls returning
// the same URL are retries that waited on the first filing.
func issueFilingTurns(turns []agent.ToolCallTurn, repository string) map[int]agent.ToolCallTurn {
	filings := map[int]agent.ToolCallTurn{}
	for _, turn := range turns {
		var result struct {
			URL string `json:"url"`
		}
		if json.Unmarshal([]byte(turn.Result), &result) != nil {
			continue
		}
		match := filedIssueURL.FindStringSubmatch(strings.TrimSpace(result.URL))
		if match == nil || !strings.EqualFold(match[1], repository) {
			continue
		}
		number, _ := strconv.Atoi(match[2])
		if _, seen := filings[number]; !seen {
			filings[number] = turn
		}
	}
	return filings
}

// backfillIssueMedia posts the media of filing's request on repository#number
// unless it has no media or the issue already has the comment. The comment
// mentions no one.
func (w *Engine) backfillIssueMedia(ctx context.Context, repository string, number int, filing agent.ToolCallTurn, dryRun bool) BackfillResult {
	result := BackfillResult{Number: number}
	fail := func(status, reason string) BackfillResult {
		result.Status, result.Reason = status, reason
		return result
	}
	if filing.Session == "" {
		return fail(BackfillNoMedia, "no project_issue_create call in session history reported filing this issue")
	}
	if filing.Prompt == nil {
		return fail(BackfillNoMedia, "no user message came before the call that filed it in "+filing.Session)
	}
	result.Prompt = filing.Prompt.ID
	if len(filing.Prompt.MediaURLs) == 0 {
		return fail(BackfillNoMedia, "the request that filed it had no media")
	}
	var problems []string
	for _, mediaURL := range filing.Prompt.MediaURLs {
		file, err := agent.MediaFile(mediaURL)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		if _, err := os.Stat(file); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", file, err))
			continue
		}
		contentType := fileContentType(file)
		if githubImageTypes[contentType] || githubVideoTypes[contentType] {
			result.Attachments = append(result.Attachments, file)
		} else {
			result.Skipped = append(result.Skipped, file)
		}
	}
	if len(result.Attachments) == 0 {
		reason := "the request that filed it had no images or videos"
		if len(problems) > 0 {
			reason = "its media can no longer be read: " + strings.Join(problems, "; ")
		}
		return fail(BackfillNoMedia, reason)
	}
	attachments, err := checkAttachments(result.Attachments)
	if err != nil {
		return fail(BackfillFailed, err.Error())
	}
	result.Attachments = attachments
	lister, canList := w.Comments.(core.CommentLister)
	poster, canAttach := w.Comments.(core.AttachmentCommentPoster)
	if !canList || !canAttach {
		return fail(BackfillFailed, fmt.Sprintf("this instance's comment client (%T) cannot list comments and upload attachments; it needs gh installed and signed in (gh auth status)", w.Comments))
	}
	comments, err := lister.ListComments(ctx, repository, number)
	if err != nil {
		return fail(BackfillFailed, err.Error())
	}
	for _, comment := range comments {
		if strings.Contains(comment.Body, requestMediaMarker) {
			return fail(BackfillAlreadyPosted, "the issue already has a comment with its request's media")
		}
	}
	if dryRun {
		result.Status = BackfillWouldPost
		return result
	}
	// The comment mentions no one: it is a record for the issue, not an
	// update for a job, so it must not wake the job or Code Loop agent.
	url, err := poster.PostCommentWithAttachments(ctx, repository, number, requestMediaText, result.Attachments)
	if err != nil {
		return fail(BackfillFailed, err.Error())
	}
	w.logf("posted the request media of %s#%d with %d attachments: %s", repository, number, len(result.Attachments), url)
	result.Status, result.URL = BackfillPosted, url
	return result
}
