package projects

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/projects/core"
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

// The request's images are posted once with the marker and no mentions, so
// no job or agent wakes; a rerun finds the
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
	if len(posted) != 1 || !strings.Contains(posted[0].Body, "originally sent with the request") || !strings.Contains(posted[0].Body, requestMediaMarker) || strings.Contains(posted[0].Body, "@") {
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

// backfillTestManager is a test manager with a running engine whose comment
// client takes attachments, watching targets, in a temp data dir.
func backfillTestManager(t *testing.T, targets ...string) (*Manager, *[][]string) {
	t.Helper()
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir("") })
	var uploads [][]string
	comments := attachmentCommentClient{updateCommentClient: updateCommentClient{fakeCommentClient: newFakeCommentClient(), author: "octo"}, files: &uploads}
	m := newTestManager(t)
	m.running = &engineRun{engine: &Engine{Identity: "ABCD1234", Targets: targets, Comments: comments, Out: &syncBuffer{}}}
	m.snapshot.Store(&core.Snapshot{Targets: targets})
	return m, &uploads
}

// writeFilingSession saves a session in which a user request with media was
// followed by a project_issue_create call that filed url.
func writeFilingSession(t *testing.T, url string, media ...string) {
	t.Helper()
	at := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	record, err := json.Marshal(map[string]any{"id": "toolu_01", "name": "project_issue_create", "args": map[string]any{}, "result": `{"url":"` + url + `"}`})
	if err != nil {
		t.Fatal(err)
	}
	var data []byte
	for _, msg := range []domain.Message{
		{ID: "msg_request", Role: domain.MessageRoleUser, Content: "file this bug", MediaURLs: media, Timestamp: at},
		{ID: "msg_tool", Role: domain.MessageRoleTool, Content: string(record), Timestamp: at.Add(time.Second)},
	} {
		line, err := json.Marshal(msg)
		if err != nil {
			t.Fatal(err)
		}
		data = append(append(data, line...), '\n')
	}
	path := filepath.Join(store.AgentDir("projects"), "sessions", "projects-chat.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// Requests that cannot name their repository and issues are rejected before
// session history is read.
func TestManagerBackfillIssueMediaRejectsBadRequests(t *testing.T) {
	if _, err := newTestManager(t).BackfillIssueMedia(context.Background(), "o/r", []int{1}, true); !errors.Is(err, ErrNotWatching) {
		t.Errorf("no engine: err = %v, want ErrNotWatching", err)
	}
	for name, tc := range map[string]struct {
		targets    []string
		repository string
		numbers    []int
		want       string
	}{
		"no issues":          {[]string{"o/r"}, "o/r", nil, "name the issue numbers"},
		"ambiguous snapshot": {[]string{"o/r", "x/y"}, "", []int{1}, "name the repository"},
		"no repositories":    {nil, "", []int{1}, "name the repository"},
		"invalid repository": {[]string{"o/r"}, "not-a-repo", []int{1}, `repository "not-a-repo" is not OWNER/REPO`},
	} {
		t.Run(name, func(t *testing.T) {
			m, _ := backfillTestManager(t, tc.targets...)
			got, err := m.BackfillIssueMedia(context.Background(), tc.repository, tc.numbers, true)
			if err == nil || !strings.Contains(err.Error(), tc.want) || got != nil {
				t.Errorf("got %v, err = %v, want error containing %q", got, err, tc.want)
			}
		})
	}
}

// Unreadable session history is reported rather than treated as no filings.
func TestManagerBackfillIssueMediaReportsUnreadableHistory(t *testing.T) {
	m, _ := backfillTestManager(t, "o/r")
	if err := os.WriteFile(filepath.Join(store.DataDir(), store.DirAgents), []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.BackfillIssueMedia(context.Background(), "o/r", []int{5}, true); err == nil || !strings.Contains(err.Error(), "read session history") {
		t.Errorf("err = %v, want a session history error", err)
	}
}

// A dry run on the only watched repository matches each issue to the request
// that filed it in session history, reports what would be posted, and posts
// nothing.
func TestManagerBackfillIssueMediaDryRun(t *testing.T) {
	m, uploads := backfillTestManager(t, "o/r")
	writeFilingSession(t, "https://github.com/o/r/issues/5", "data:image/png;base64,iVBORw0KGgo=")

	got, err := m.BackfillIssueMedia(context.Background(), "", []int{5, 7}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got["repository"] != "o/r" || got["dry_run"] != true {
		t.Errorf("got %v", got)
	}
	results, ok := got["issues"].([]BackfillResult)
	if !ok || len(results) != 2 {
		t.Fatalf("issues = %#v", got["issues"])
	}
	if filed := results[0]; filed.Number != 5 || filed.Status != BackfillWouldPost || filed.Prompt != "msg_request" || len(filed.Attachments) != 1 {
		t.Errorf("issue 5 = %+v", filed)
	}
	if unknown := results[1]; unknown.Number != 7 || unknown.Status != BackfillNoMedia || !strings.Contains(unknown.Reason, "no project_issue_create call") {
		t.Errorf("issue 7 = %+v", unknown)
	}
	if len(*uploads) != 0 {
		t.Errorf("dry run uploaded %q", *uploads)
	}
}
