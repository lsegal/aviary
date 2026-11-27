package projects

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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
	if want := "/gh-create o/r Save crashes attach:" + shot + " attach:" + strconv.Quote(spaced) + " identity:/aviary:ABCD1234"; prompts[0] != want {
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

// writeAttachmentBytes writes data to a file named name under dir.
func writeAttachmentBytes(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The extension names the type when it is known; otherwise the first bytes
// do, and a file that cannot be read has none.
func TestFileContentType(t *testing.T) {
	dir := t.TempDir()
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	gif := []byte("GIF89a\x01\x00\x01\x00")
	for _, c := range []struct{ name, path, want string }{
		{"png extension", writeAttachmentBytes(t, dir, "shot.png", nil), "image/png"},
		{"upper-case extension", writeAttachmentBytes(t, dir, "SHOT.JPG", nil), "image/jpeg"},
		{"extension wins over content", writeAttachmentBytes(t, dir, "clip.mp4", png), "video/mp4"},
		{"no extension sniffs png", writeAttachmentBytes(t, dir, "screenshot", png), "image/png"},
		{"unknown extension sniffs gif", writeAttachmentBytes(t, dir, "frame.aviaryunknown", gif), "image/gif"},
		{"no extension text drops parameters", writeAttachmentBytes(t, dir, "notes", []byte("hello world")), "text/plain"},
		{"no extension empty file", writeAttachmentBytes(t, dir, "empty", nil), "text/plain"},
		{"missing file without extension", filepath.Join(dir, "gone"), ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := fileContentType(c.path); got != c.want {
				t.Errorf("fileContentType(%s) = %q, want %q", c.path, got, c.want)
			}
		})
	}
}

func TestCheckAttachment(t *testing.T) {
	dir := t.TempDir()
	sniffed := writeAttachmentBytes(t, dir, "screenshot", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"))
	if path, err := checkAttachment("  " + sniffed + "  "); err != nil || path != sniffed {
		t.Errorf("a PNG without an extension, padded: %q, %v", path, err)
	}
	// Videos have their own, larger limit than images.
	video := writeAttachment(t, dir, "clip.mp4", maxImageAttachmentBytes+1)
	if path, err := checkAttachment(video); err != nil || path != video {
		t.Errorf("a video over the image limit: %q, %v", path, err)
	}
	// A relative path resolves against the working directory.
	t.Chdir(dir)
	if path, err := checkAttachment("clip.mp4"); err != nil || path != video {
		t.Errorf("a relative path: %q, %v", path, err)
	}
	for _, blank := range []string{"", "   "} {
		if _, err := checkAttachment(blank); err == nil || !strings.Contains(err.Error(), "not a file path") {
			t.Errorf("checkAttachment(%q) = %v, want not a file path", blank, err)
		}
	}
	text := writeAttachmentBytes(t, dir, "notes", []byte("plain words"))
	if _, err := checkAttachment(text); err == nil || !strings.Contains(err.Error(), "unsupported type text/plain") || !strings.Contains(err.Error(), "GitHub takes only images") {
		t.Errorf("sniffed text: %v", err)
	}
	huge := writeAttachment(t, dir, "huge.png", maxImageAttachmentBytes+1)
	if _, err := checkAttachment(huge); err == nil || !strings.Contains(err.Error(), "too large (11 MB; GitHub takes up to 10 MB for image/png)") {
		t.Errorf("an oversized image: %v", err)
	}
	// A path below a regular file fails to stat; the error names the path
	// whichever way the platform reports it.
	under := filepath.Join(sniffed, "inner.png")
	if _, err := checkAttachment(under); err == nil || !strings.Contains(err.Error(), under) {
		t.Errorf("a path under a file: %v", err)
	}
}

func TestCheckAttachments(t *testing.T) {
	dir := t.TempDir()
	a := writeAttachment(t, dir, "a.png", 1)
	b := writeAttachment(t, dir, "b.gif", 1)
	paths, err := checkAttachments([]string{a, b, a})
	if err != nil || !slices.Equal(paths, []string{a, b}) {
		t.Errorf("paths = %q, %v; want duplicates dropped", paths, err)
	}
	if paths, err := checkAttachments(nil); err != nil || paths != nil {
		t.Errorf("no files: %q, %v", paths, err)
	}
	// Every bad file is named in one error and nothing is returned.
	missing := filepath.Join(dir, "gone.png")
	notes := writeAttachment(t, dir, "notes.txt", 1)
	paths, err = checkAttachments([]string{a, missing, notes})
	if paths != nil || err == nil || !strings.HasPrefix(err.Error(), "cannot attach ") || !strings.Contains(err.Error(), missing) || !strings.Contains(err.Error(), notes) || !strings.Contains(err.Error(), "; ") {
		t.Errorf("bad files: %q, %v", paths, err)
	}
	// More distinct files than gh takes at once is refused; repeats of one
	// file are not counted.
	var many []string
	for i := range maxAttachments + 1 {
		many = append(many, writeAttachment(t, dir, "shot"+strconv.Itoa(i)+".png", 1))
	}
	paths, err = checkAttachments(many)
	if paths != nil || err == nil || !strings.Contains(err.Error(), "51 attachments is more than the 50 GitHub takes at once") {
		t.Errorf("too many files: %d paths, %v", len(paths), err)
	}
	if paths, err := checkAttachments(slices.Repeat([]string{a}, maxAttachments+1)); err != nil || len(paths) != 1 {
		t.Errorf("one file repeated: %q, %v", paths, err)
	}
}

func TestGHCLIPostCommentWithAttachmentsReportsFailure(t *testing.T) {
	g := GHCLI{runCommand: func(context.Context, ...string) ([]byte, error) {
		return []byte("  failed to upload a.png: HTTP 422\n"), errors.New("exit status 1")
	}}
	url, err := g.PostCommentWithAttachments(context.Background(), "o/r", 3, "hi", []string{"/a.png"})
	if url != "" || err == nil || err.Error() != "post comment with attachments on o/r#3: exit status 1: failed to upload a.png: HTTP 422" {
		t.Errorf("url = %q, err = %v", url, err)
	}
	// gh's progress lines before the URL are dropped.
	g.runCommand = func(context.Context, ...string) ([]byte, error) {
		return []byte("Uploading a.png\nhttps://github.com/o/r/issues/3#issuecomment-9\n"), nil
	}
	if url, err := g.PostCommentWithAttachments(context.Background(), "o/r", 3, "hi", nil); err != nil || url != "https://github.com/o/r/issues/3#issuecomment-9" {
		t.Errorf("url = %q, %v", url, err)
	}
}
