package projects

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/projects/core"
)

// writeAttachment writes a file of size bytes named name under dir.
func writeAttachment(t *testing.T, dir, name string, size int64) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// attachmentCommentClient also posts comments with attachments.
type attachmentCommentClient struct {
	updateCommentClient
	files *[][]string
}

func (c attachmentCommentClient) PostCommentWithAttachments(ctx context.Context, repo string, number int, body string, files []string) (string, error) {
	*c.files = append(*c.files, files)
	return c.PostCommentURL(ctx, repo, number, body)
}

// Attachments reach gh-create as attach: arguments, the result lists them,
// and the same text with different media files a separate issue.
func TestCreateIssuePassesAttachmentsToGhCreate(t *testing.T) {
	dir := t.TempDir()
	shot := writeAttachment(t, dir, "shot.png", 100)
	spaced := writeAttachment(t, dir, "my clip.mp4", 100)
	var prompts []string
	w := &Engine{Identity: "ABCD1234", Registry: clidefs.MustBuiltin(), Runner: CommandRunner{Agent: "claude/opus", Agents: []string{"claude/opus"}}, Out: &syncBuffer{}}
	w.oneShotRun = func(_ context.Context, _ agentSpec, prompt string) (string, error) {
		prompts = append(prompts, prompt)
		return "Filed https://github.com/o/r/issues/12\n", nil
	}
	result, err := w.CreateIssue(context.Background(), core.IssueCreate{Repository: "o/r", Text: "Save crashes", Attachments: []string{shot, shot, spaced}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.Attachments, []string{shot, spaced}) {
		t.Errorf("attachments = %q", result.Attachments)
	}
	if want := "/gh-create o/r Save crashes attach:" + shot + " attach:\"" + spaced + "\" identity:/aviary:ABCD1234"; prompts[0] != want {
		t.Errorf("prompt = %q, want %q", prompts[0], want)
	}
	if _, err := w.CreateIssue(context.Background(), core.IssueCreate{Repository: "o/r", Text: "Save crashes"}); err != nil || len(prompts) != 2 {
		t.Errorf("the same text without media did not file again: %d runs, %v", len(prompts), err)
	}
	if strings.Contains(prompts[1], "attach:") {
		t.Errorf("a filing without media passed attachments: %q", prompts[1])
	}
}

// A file GitHub would not take fails the filing with its name and the reason,
// and gh-create never runs, so no image is dropped silently.
func TestCreateIssueRefusesBadAttachments(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "gone.png")
	notes := writeAttachment(t, dir, "notes.txt", 10)
	huge := writeAttachment(t, dir, "huge.png", maxImageAttachmentBytes+1)
	runs := 0
	w := &Engine{Identity: "ABCD1234", Registry: clidefs.MustBuiltin(), Runner: CommandRunner{Agent: "claude/opus", Agents: []string{"claude/opus"}}, Out: &syncBuffer{}}
	w.oneShotRun = func(context.Context, agentSpec, string) (string, error) {
		runs++
		return "Filed https://github.com/o/r/issues/12\n", nil
	}
	for _, c := range []struct{ file, reason string }{
		{missing, "file does not exist"},
		{notes, "unsupported type text/plain"},
		{huge, "too large"},
		{dir, "not a regular file"},
	} {
		_, err := w.CreateIssue(context.Background(), core.IssueCreate{Repository: "o/r", Text: "x", Attachments: []string{c.file}})
		if err == nil || !strings.Contains(err.Error(), c.file) || !strings.Contains(err.Error(), c.reason) {
			t.Errorf("attaching %s: %v; want its path and %q", c.file, err, c.reason)
		}
	}
	if runs != 0 {
		t.Errorf("gh-create ran %d times for bad attachments", runs)
	}
}

func TestPostIssueUpdateAttachesFiles(t *testing.T) {
	dir := t.TempDir()
	shot := writeAttachment(t, dir, "shot.png", 100)
	var files [][]string
	comments := attachmentCommentClient{updateCommentClient{fakeCommentClient: newFakeCommentClient(), author: "octo"}, &files}
	w := &Engine{Identity: "ABCD1234", Comments: comments, Out: &syncBuffer{}}
	result, err := w.PostIssueUpdate(context.Background(), core.IssueUpdate{Repository: "o/r", Number: 2, Text: "Here it is", Attachments: []string{shot}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.Attachments, []string{shot}) || len(files) != 1 || !slices.Equal(files[0], []string{shot}) {
		t.Errorf("result = %+v, uploaded %q", result, files)
	}
	if posted := comments.comments["o/r#2"]; len(posted) != 1 || posted[0].Body != "@octo @/aviary:ABCD1234\n\nHere it is" {
		t.Errorf("posted %+v", posted)
	}
	// Without attachments it posts the plain way.
	if _, err := w.PostIssueUpdate(context.Background(), core.IssueUpdate{Repository: "o/r", Number: 2, Text: "No media"}); err != nil || len(files) != 1 {
		t.Errorf("a plain update uploaded files: %q, %v", files, err)
	}
	missing := filepath.Join(dir, "gone.png")
	if _, err := w.PostIssueUpdate(context.Background(), core.IssueUpdate{Repository: "o/r", Number: 2, Text: "x", Attachments: []string{missing}}); err == nil || !strings.Contains(err.Error(), missing) {
		t.Errorf("a missing file: %v", err)
	}
	if comments.posts != 2 {
		t.Errorf("posts = %d, want 2", comments.posts)
	}
	plain := &Engine{Identity: "ABCD1234", Comments: comments.updateCommentClient, Out: &syncBuffer{}}
	if _, err := plain.PostIssueUpdate(context.Background(), core.IssueUpdate{Repository: "o/r", Number: 2, Text: "x", Attachments: []string{shot}}); err == nil || !strings.Contains(err.Error(), "cannot upload attachments") {
		t.Errorf("a client that cannot upload: %v", err)
	}
}

func TestGHCLIPostsCommentsWithAttachments(t *testing.T) {
	var calls [][]string
	g := GHCLI{runCommand: func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, args)
		return []byte("https://github.com/o/r/issues/3#issuecomment-4\n"), nil
	}}
	url, err := g.PostCommentWithAttachments(context.Background(), "o/r", 3, "@octo hi", []string{"/a.png", "/b.mp4"})
	if err != nil || url != "https://github.com/o/r/issues/3#issuecomment-4" {
		t.Fatalf("url = %q, %v", url, err)
	}
	want := []string{"issue", "comment", "3", "--repo", "o/r", "--body", "@octo hi", "--attach", "/a.png", "--attach", "/b.mp4"}
	if len(calls) != 1 || !slices.Equal(calls[0], want) {
		t.Errorf("calls = %q, want %q", calls, want)
	}
}
