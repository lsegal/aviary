package projects

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/store"
)

// filingTurn is a project_issue_create call that filed url from a request
// with media.
func filingTurn(url string, at int, media ...string) agent.ToolCallTurn {
	return agent.ToolCallTurn{
		Session:  "/sessions/signal.jsonl",
		CalledAt: time.Unix(int64(at), 0),
		Result:   `{"agent":"claude/opus","repository":"o/r","url":"` + url + `"}`,
		Prompt:   &domain.Message{ID: "msg_" + url[len(url)-1:], Role: domain.MessageRoleUser, MediaURLs: media},
	}
}

// Each issue maps to the earliest call that filed it, on its own repository
// only; results that report no issue are left out.
func TestIssueFilingTurns(t *testing.T) {
	first := filingTurn("https://github.com/o/r/issues/5", 1)
	retry := filingTurn("https://github.com/o/r/issues/5", 2)
	other := filingTurn("https://github.com/o/other/issues/6", 3)
	failed := agent.ToolCallTurn{Session: "s", Result: "gh-create reported no new issue on o/r"}
	filings := issueFilingTurns([]agent.ToolCallTurn{first, retry, other, failed}, "O/R")
	if len(filings) != 1 || !filings[5].CalledAt.Equal(first.CalledAt) {
		t.Errorf("filings = %+v", filings)
	}
}

// The request's images are posted once with the marker; a rerun finds the
// comment and posts nothing, and a voice note is left out.
func TestBackfillIssueMediaPostsOnce(t *testing.T) {
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir("") })
	dir := t.TempDir()
	voice := writeAttachment(t, dir, "voice.m4a", 10)
	var uploads [][]string
	comments := attachmentCommentClient{updateCommentClient: updateCommentClient{fakeCommentClient: newFakeCommentClient(), author: "octo"}, files: &uploads}
	w := &Engine{Identity: "ABCD1234", Comments: comments, Out: &syncBuffer{}}
	filing := filingTurn("https://github.com/o/r/issues/5", 1, "data:image/png;base64,iVBORw0KGgo=", llm.FileURL(voice))

	dry := w.backfillIssueMedia(context.Background(), "o/r", 5, filing, true)
	if dry.Status != BackfillWouldPost || len(dry.Attachments) != 1 || len(uploads) != 0 {
		t.Fatalf("dry run = %+v, uploads %v", dry, uploads)
	}

	result := w.backfillIssueMedia(context.Background(), "o/r", 5, filing, false)
	if result.Status != BackfillPosted || result.URL == "" || result.Prompt != "msg_5" {
		t.Fatalf("result = %+v", result)
	}
	if len(uploads) != 1 || len(uploads[0]) != 1 || filepath.Ext(uploads[0][0]) != ".png" || !strings.HasPrefix(uploads[0][0], agent.TurnMediaDir()) {
		t.Errorf("uploaded %q", uploads)
	}
	if !slices.Equal(result.Skipped, []string{voice}) {
		t.Errorf("skipped = %q", result.Skipped)
	}
	posted := comments.comments["o/r#5"]
	if len(posted) != 1 || !strings.Contains(posted[0].Body, "originally sent with the request") || !strings.Contains(posted[0].Body, requestMediaMarker) {
		t.Fatalf("posted %+v", posted)
	}

	again := w.backfillIssueMedia(context.Background(), "o/r", 5, filing, false)
	if again.Status != BackfillAlreadyPosted || len(comments.comments["o/r#5"]) != 1 || len(uploads) != 1 {
		t.Errorf("rerun = %+v, %d comments", again, len(comments.comments["o/r#5"]))
	}
}

// Issues with nothing to post are reported with the reason and get no comment.
func TestBackfillIssueMediaReportsMissingMedia(t *testing.T) {
	comments := updateCommentClient{fakeCommentClient: newFakeCommentClient(), author: "octo"}
	w := &Engine{Identity: "ABCD1234", Comments: comments, Out: &syncBuffer{}}
	gone := filepath.Join(t.TempDir(), "gone.png")
	voice := writeAttachment(t, t.TempDir(), "voice.m4a", 10)
	noPrompt := filingTurn("https://github.com/o/r/issues/4", 1)
	noPrompt.Prompt = nil
	for name, tc := range map[string]struct {
		filing agent.ToolCallTurn
		reason string
	}{
		"never filed":     {agent.ToolCallTurn{}, "no project_issue_create call"},
		"no prompt":       {noPrompt, "no user message came before"},
		"no media":        {filingTurn("https://github.com/o/r/issues/1", 1), "had no media"},
		"only voice note": {filingTurn("https://github.com/o/r/issues/2", 1, llm.FileURL(voice)), "no images or videos"},
		"remote media":    {filingTurn("https://github.com/o/r/issues/3", 1, "https://example.com/a.png"), "can no longer be read"},
		"deleted file":    {filingTurn("https://github.com/o/r/issues/3", 1, llm.FileURL(gone)), "can no longer be read"},
	} {
		t.Run(name, func(t *testing.T) {
			result := w.backfillIssueMedia(context.Background(), "o/r", 9, tc.filing, false)
			if result.Status != BackfillNoMedia || !strings.Contains(result.Reason, tc.reason) {
				t.Errorf("result = %+v, want reason containing %q", result, tc.reason)
			}
		})
	}
	if len(comments.comments) != 0 {
		t.Errorf("posted %+v", comments.comments)
	}
}
