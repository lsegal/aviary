package projects

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/projects/core"
)

// updateCommentClient is a comment client that also posts updates.
type updateCommentClient struct {
	*fakeCommentClient
	author string
}

func (c updateCommentClient) IssueAuthor(context.Context, string, int) (string, error) {
	return c.author, nil
}

func (c updateCommentClient) PostCommentURL(ctx context.Context, repo string, number int, body string) (string, error) {
	if err := c.PostComment(ctx, repo, number, body); err != nil {
		return "", err
	}
	return "https://github.com/" + repo + "/issues/2#issuecomment-1", nil
}

func TestPostIssueUpdateMentionsTheAuthorAndThisInstance(t *testing.T) {
	comments := updateCommentClient{fakeCommentClient: newFakeCommentClient(), author: "octo"}
	logs := &syncBuffer{}
	w := &Engine{Identity: "ABCD1234", Comments: comments, Out: logs}
	result, err := w.PostIssueUpdate(context.Background(), core.IssueUpdate{Repository: "o/r", Number: 2, Text: "  The blocker merged; rebase onto main.  "})
	if err != nil {
		t.Fatal(err)
	}
	if result.URL != "https://github.com/o/r/issues/2#issuecomment-1" || result.Author != "octo" {
		t.Errorf("result = %+v", result)
	}
	posted := comments.comments["o/r#2"]
	if len(posted) != 1 || posted[0].Body != "@octo @/aviary:ABCD1234\n\nThe blocker merged; rebase onto main." {
		t.Fatalf("posted %+v", posted)
	}
	// The comment reaches the owning job through the mention path.
	if !mentionedIdentity(posted[0].Body, "ABCD1234") {
		t.Errorf("the update does not mention this instance: %q", posted[0].Body)
	}
	if !strings.Contains(logs.String(), "posted an update on o/r#2 for @octo") {
		t.Errorf("log = %q", logs.String())
	}
	for _, bad := range []core.IssueUpdate{{Repository: "nope", Number: 2, Text: "x"}, {Repository: "o/r", Text: "x"}, {Repository: "o/r", Number: 2, Text: " "}} {
		if _, err := w.PostIssueUpdate(context.Background(), bad); err == nil {
			t.Errorf("%+v was posted", bad)
		}
	}
	if comments.posts != 1 {
		t.Errorf("invalid updates posted comments: %d posts", comments.posts)
	}
}

// An instance that genuinely cannot post says what it is missing.
func TestPostIssueUpdateNamesTheMissingCapability(t *testing.T) {
	update := core.IssueUpdate{Repository: "o/r", Number: 2, Text: "x"}
	_, err := (&Engine{Out: &syncBuffer{}}).PostIssueUpdate(context.Background(), update)
	if err == nil || !strings.Contains(err.Error(), "gh auth status") {
		t.Errorf("no comment client: %v", err)
	}
	_, err = (&Engine{Comments: newFakeCommentClient(), Out: &syncBuffer{}}).PostIssueUpdate(context.Background(), update)
	if err == nil || !strings.Contains(err.Error(), "cannot read an issue's author") {
		t.Errorf("comment client without update support: %v", err)
	}
}

func TestGHCLIPostsUpdatesWithoutEditingBodies(t *testing.T) {
	var calls [][]string
	g := GHCLI{runCommand: func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, args)
		if slices.Contains(args, ".user.login") {
			return []byte("octo\n"), nil
		}
		return []byte("https://github.com/o/r/pull/3#issuecomment-4\n"), nil
	}}
	author, err := g.IssueAuthor(context.Background(), "o/r", 3)
	if err != nil || author != "octo" {
		t.Fatalf("author = %q, %v", author, err)
	}
	url, err := g.PostCommentURL(context.Background(), "o/r", 3, "@octo hi")
	if err != nil || url != "https://github.com/o/r/pull/3#issuecomment-4" {
		t.Fatalf("url = %q, %v", url, err)
	}
	want := [][]string{
		{"api", "repos/o/r/issues/3", "--jq", ".user.login"},
		{"api", "repos/o/r/issues/3/comments", "-f", "body=@octo hi", "--jq", ".html_url"},
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %q", calls)
	}
	for i := range want {
		if !slices.Equal(calls[i], want[i]) {
			t.Errorf("call %d = %q, want %q", i, calls[i], want[i])
		}
	}
}
