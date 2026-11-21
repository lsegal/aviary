package projects

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestListenForWebhooksAssignsRandomPort(t *testing.T) {
	listener, err := listenForWebhooks("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close() //nolint:errcheck

	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if port == "0" || port == "" {
		t.Fatalf("listener address = %q, want an assigned port", listener.Addr())
	}
}

func TestNgrokTargetAddrRewritesWildcardHostToLoopback(t *testing.T) {
	cases := map[string]string{
		"[::]:51587":         "127.0.0.1:51587",
		"0.0.0.0:51587":      "127.0.0.1:51587",
		":51587":             "127.0.0.1:51587",
		"127.0.0.1:51587":    "127.0.0.1:51587",
		"192.168.1.5:51587":  "192.168.1.5:51587",
		"not-a-valid-target": "not-a-valid-target",
	}
	for input, want := range cases {
		if got := ngrokTargetAddr(input); got != want {
			t.Errorf("ngrokTargetAddr(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestOriginatingWorkStateLoadsLinkedPullRequest(t *testing.T) {
	responses := [][]byte{
		[]byte(`{"state":"OPEN"}`),
		[]byte(`[{"event":"cross-referenced","source":{"issue":{"number":9,"body":"Closes #7","pull_request":{"merged_at":null}}}}]`),
		[]byte(`{"state":"closed","merged_at":null}`),
	}
	var calls [][]string
	gh := GHCLI{runCommand: func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		return responses[len(calls)-1], nil
	}}
	state, err := gh.OriginatingWorkState(context.Background(), "owner/repo", 7)
	if err != nil || state.IssueState != "OPEN" || len(state.PullRequests) != 1 || state.PullRequests[0] != (PullRequestWorkState{Repository: "owner/repo", Number: 9, State: "closed"}) {
		t.Fatalf("OriginatingWorkState() = (%#v, %v)", state, err)
	}
	want := []string{"api", "repos/owner/repo/pulls/9"}
	if !reflect.DeepEqual(calls[2], want) {
		t.Fatalf("pull request state call = %#v, want %#v", calls[2], want)
	}
}

func TestOriginatingWorkStateReadsPullRequestDraftStatus(t *testing.T) {
	responses := [][]byte{
		[]byte(`{"state":"OPEN"}`),
		[]byte(`[{"event":"cross-referenced","source":{"issue":{"number":9,"body":"Closes #7","pull_request":{"merged_at":null}}}}]`),
		[]byte(`{"state":"open","merged_at":null,"draft":false}`),
	}
	var calls [][]string
	gh := GHCLI{runCommand: func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		return responses[len(calls)-1], nil
	}}
	state, err := gh.OriginatingWorkState(context.Background(), "owner/repo", 7)
	if err != nil || len(state.PullRequests) != 1 || state.PullRequests[0] != (PullRequestWorkState{Repository: "owner/repo", Number: 9, State: "open", IsDraft: false}) {
		t.Fatalf("OriginatingWorkState() = (%#v, %v)", state, err)
	}
}

// A pull request's diff size is read from the same pull request response,
// so Usage can report the code a project's jobs changed (issue #787).
func TestOriginatingWorkStateReadsPullRequestDiffSize(t *testing.T) {
	responses := [][]byte{
		[]byte(`{"state":"CLOSED"}`),
		[]byte(`[{"event":"cross-referenced","source":{"issue":{"number":9,"body":"Closes #7","pull_request":{}}}}]`),
		[]byte(`{"state":"closed","merged_at":"2026-11-09T12:00:00Z","additions":120,"deletions":45}`),
	}
	var calls int
	gh := GHCLI{runCommand: func(context.Context, ...string) ([]byte, error) {
		calls++
		return responses[calls-1], nil
	}}
	state, err := gh.OriginatingWorkState(context.Background(), "owner/repo", 7)
	if err != nil || len(state.PullRequests) != 1 || state.PullRequests[0].Additions != 120 || state.PullRequests[0].Deletions != 45 {
		t.Fatalf("OriginatingWorkState() = (%#v, %v), want 120 additions and 45 deletions", state, err)
	}
	if calls != len(responses) {
		t.Fatalf("API calls = %d, want %d (no extra read for the diff size)", calls, len(responses))
	}
}

// A same-repo pull request keeps being matched by the plain #N closing form,
// even when the timeline event names its repository.
func TestOriginatingWorkStateRecognizesSameRepoPlainReference(t *testing.T) {
	for _, fullName := range []string{"owner/repo", "Owner/Repo"} {
		t.Run(fullName, func(t *testing.T) {
			responses := [][]byte{
				[]byte(`{"state":"OPEN"}`),
				[]byte(`[{"event":"cross-referenced","source":{"issue":{"number":9,"body":"Fixes #7","repository":{"full_name":"` + fullName + `"},"pull_request":{}}}}]`),
				[]byte(`{"state":"open","draft":true}`),
			}
			var calls [][]string
			gh := GHCLI{runCommand: func(_ context.Context, args ...string) ([]byte, error) {
				calls = append(calls, append([]string(nil), args...))
				return responses[len(calls)-1], nil
			}}
			state, err := gh.OriginatingWorkState(context.Background(), "owner/repo", 7)
			want := PullRequestWorkState{Repository: fullName, Number: 9, State: "open", IsDraft: true}
			if err != nil || len(state.PullRequests) != 1 || state.PullRequests[0] != want {
				t.Fatalf("OriginatingWorkState() = (%#v, %v), want pull request %#v", state, err, want)
			}
			if len(calls) != 3 || !reflect.DeepEqual(calls[2], []string{"api", "repos/" + fullName + "/pulls/9"}) {
				t.Fatalf("calls = %#v, want the pull request read from its own repo with no visibility probe", calls)
			}
		})
	}
}

func TestOriginatingWorkStateRecognizesCrossRepoPullRequest(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		pull   string
		want   PullRequestWorkState
	}{
		{
			name:   "merged with qualified reference",
			source: `{"number":3,"body":"Closes owner/repo#7","repository":{"full_name":"fork/repo"},"pull_request":{}}`,
			pull:   `{"state":"closed","merged_at":"2026-11-09T12:00:00Z"}`,
			want:   PullRequestWorkState{Repository: "fork/repo", Number: 3, State: "closed", Merged: true, MergedAt: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)},
		},
		{
			name:   "open draft with issue URL",
			source: `{"number":3,"body":"Fixes https://github.com/owner/repo/issues/7","repository_url":"https://api.github.com/repos/fork/repo","pull_request":{}}`,
			pull:   `{"state":"open","draft":true}`,
			want:   PullRequestWorkState{Repository: "fork/repo", Number: 3, State: "open", IsDraft: true},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			responses := [][]byte{
				[]byte(`{"state":"OPEN"}`),
				[]byte(`[{"event":"cross-referenced","source":{"issue":` + test.source + `}}]`),
				[]byte(test.pull),
			}
			var calls [][]string
			gh := GHCLI{runCommand: func(_ context.Context, args ...string) ([]byte, error) {
				calls = append(calls, append([]string(nil), args...))
				return responses[len(calls)-1], nil
			}}
			state, err := gh.OriginatingWorkState(context.Background(), "owner/repo", 7)
			if err != nil || len(state.PullRequests) != 1 || state.PullRequests[0] != test.want {
				t.Fatalf("OriginatingWorkState() = (%#v, %v), want pull request %#v", state, err, test.want)
			}
			if want := []string{"api", "repos/fork/repo/pulls/3"}; !reflect.DeepEqual(calls[2], want) {
				t.Fatalf("pull request state call = %#v, want %#v", calls[2], want)
			}
		})
	}
}

func TestOriginatingWorkStateIgnoresCrossRepoBareReference(t *testing.T) {
	responses := [][]byte{
		[]byte(`{"state":"OPEN"}`),
		[]byte(`[{"event":"cross-referenced","source":{"issue":{"number":3,"body":"Closes #7","repository":{"full_name":"other/repo"},"pull_request":{}}}}]`),
	}
	call := 0
	gh := GHCLI{runCommand: func(_ context.Context, _ ...string) ([]byte, error) {
		response := responses[call]
		call++
		return response, nil
	}}
	state, err := gh.OriginatingWorkState(context.Background(), "owner/repo", 7)
	if err != nil || len(state.PullRequests) != 0 || call != 2 {
		t.Fatalf("OriginatingWorkState() = (%#v, %v) after %d calls, want no pull requests", state, err, call)
	}
}

func TestClosesIssue(t *testing.T) {
	for _, test := range []struct {
		body, pullRepo string
		want           bool
	}{
		{body: "Closes #7", pullRepo: "owner/repo", want: true},
		{body: "Fix it.\n\nfixes #7", pullRepo: "owner/repo", want: true},
		{body: "Resolved #7", pullRepo: "Owner/Repo", want: true},
		{body: "Closes #70", pullRepo: "owner/repo", want: false},
		{body: "See #7", pullRepo: "owner/repo", want: false},
		{body: "fixes: owner/repo#7", pullRepo: "owner/repo", want: true},
		{body: "Closes #7", pullRepo: "fork/repo", want: false},
		{body: "Closes owner/repo#7", pullRepo: "fork/repo", want: true},
		{body: "Resolves https://github.com/Owner/Repo/issues/7", pullRepo: "fork/repo", want: true},
		{body: "Closes owner/repo#70", pullRepo: "fork/repo", want: false},
		{body: "Closes other/repo#7", pullRepo: "fork/repo", want: false},
	} {
		if got := closesIssue(test.body, test.pullRepo, "owner/repo", 7); got != test.want {
			t.Errorf("closesIssue(%q, %q) = %v, want %v", test.body, test.pullRepo, got, test.want)
		}
	}
}

func TestClosedWorkReasonDistinguishesManualIssueClosureFromMerge(t *testing.T) {
	for _, test := range []struct {
		name       string
		issue      string
		timeline   string
		pull       string
		wantReason string
	}{
		{name: "manual closure", issue: `{"state":"CLOSED"}`, timeline: `[]`, wantReason: "issue #7 was closed without a merge"},
		{name: "merged pull request", issue: `{"state":"CLOSED"}`, timeline: `[{"event":"cross-referenced","source":{"issue":{"number":9,"body":"Closes #7","pull_request":{}}}}]`, pull: `{"state":"closed","merged_at":"2026-07-20T12:00:00Z"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			responses := [][]byte{[]byte(test.issue)}
			responses = append(responses, []byte(test.timeline))
			if test.pull != "" {
				responses = append(responses, []byte(test.pull))
			}
			call := 0
			gh := GHCLI{runCommand: func(_ context.Context, _ ...string) ([]byte, error) {
				response := responses[call]
				call++
				return response, nil
			}}
			state, err := gh.OriginatingWorkState(context.Background(), "owner/repo", 7)
			reason := closedWorkReason(OriginatingWorkState{IssueState: "OPEN"}, state, 7)
			if err != nil || reason != test.wantReason {
				t.Fatalf("closure state = (%#v, %v), reason=%q, want %q", state, err, reason, test.wantReason)
			}
		})
	}
}

func TestGHCLIPostComment(t *testing.T) {
	var calls [][]string
	gh := GHCLI{runCommand: func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		return []byte(`{}`), nil
	}}
	if err := gh.PostComment(context.Background(), "owner/repo", 7, "Starting work on this issue /aviary:AAA"); err != nil {
		t.Fatalf("PostComment: %v", err)
	}
	want := []string{"api", "repos/owner/repo/issues/7/comments", "-f", "body=Starting work on this issue /aviary:AAA"}
	if !reflect.DeepEqual(calls[0], want) {
		t.Fatalf("call = %#v, want %#v", calls[0], want)
	}
}

func TestGHCLIPostCommentReturnsError(t *testing.T) {
	gh := GHCLI{runCommand: func(_ context.Context, _ ...string) ([]byte, error) {
		return []byte("boom"), errors.New("exit status 1")
	}}
	if err := gh.PostComment(context.Background(), "owner/repo", 7, "hi"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestGHCLIAddReaction(t *testing.T) {
	var calls [][]string
	gh := GHCLI{runCommand: func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		return []byte(`{}`), nil
	}}
	if err := gh.AddReaction(context.Background(), "owner/repo", 42, "eyes"); err != nil {
		t.Fatalf("AddReaction: %v", err)
	}
	want := []string{"api", "repos/owner/repo/issues/comments/42/reactions", "-f", "content=eyes"}
	if !reflect.DeepEqual(calls[0], want) {
		t.Fatalf("call = %#v, want %#v", calls[0], want)
	}
}

func TestGHCLIAddReactionReturnsError(t *testing.T) {
	gh := GHCLI{runCommand: func(_ context.Context, _ ...string) ([]byte, error) {
		return []byte("boom"), errors.New("exit status 1")
	}}
	if err := gh.AddReaction(context.Background(), "owner/repo", 42, "eyes"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestGHCLIListComments(t *testing.T) {
	gh := GHCLI{runCommand: func(_ context.Context, _ ...string) ([]byte, error) {
		return []byte(`[{"body":"Starting work on this issue /aviary:AAA","created_at":"2026-07-20T12:00:00Z"}]`), nil
	}}
	comments, err := gh.ListComments(context.Background(), "owner/repo", 7)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(comments) != 1 || comments[0].Body != "Starting work on this issue /aviary:AAA" {
		t.Fatalf("comments = %#v", comments)
	}
}

func TestGHCLIListCommentsReturnsError(t *testing.T) {
	gh := GHCLI{runCommand: func(_ context.Context, _ ...string) ([]byte, error) {
		return []byte("boom"), errors.New("exit status 1")
	}}
	if _, err := gh.ListComments(context.Background(), "owner/repo", 7); err == nil {
		t.Fatal("expected an error")
	}
}

func TestValidRepo(t *testing.T) {
	for _, s := range []string{"owner/repo", "a/b"} {
		if !validRepo(s) {
			t.Errorf("%q", s)
		}
	}
	for _, s := range []string{"repo", "a/b/c", "a b/c", "/repo"} {
		if validRepo(s) {
			t.Errorf("accepted %q", s)
		}
	}
}

func TestVersionDefaultsToDevelopment(t *testing.T) {
	if version != "dev" {
		t.Fatalf("version = %q, want dev", version)
	}
}

func TestParseTargetURLs(t *testing.T) {
	for _, input := range []string{
		"https://github.com/lsegal/aviary",
		"https://github.com/lsegal/aviary/",
	} {
		got, err := parseTarget(input)
		if err != nil || got.Repo != "lsegal/aviary" || got.IsProject {
			t.Fatalf("parseTarget(%q) = %#v, %v", input, got, err)
		}
	}
	got, err := parseTarget("https://github.com/users/lsegal/projects/3")
	if err != nil || !got.IsProject || got.Owner != "lsegal" || got.ProjectID != "3" || got.ProjectOwnerType != "users" {
		t.Fatalf("project target = %#v, %v", got, err)
	}
	got, err = parseTarget("https://github.com/orgs/example/projects/4")
	if err != nil || !got.IsProject || got.Owner != "example" || got.ProjectID != "4" || got.ProjectOwnerType != "orgs" {
		t.Fatalf("organization project target = %#v, %v", got, err)
	}
	for _, input := range []string{
		"https://github.com/lsegal/aviary/discussions",
		"https://github.com/lsegal/aviary/discussions/",
	} {
		got, err := parseTarget(input)
		if err != nil || !got.IsDiscussion || got.Repo != "lsegal/aviary" || got.IsProject || got.DiscussionCategory != "" {
			t.Fatalf("parseTarget(%q) = %#v, %v", input, got, err)
		}
	}
	got, err = parseTarget("https://github.com/lsegal/aviary/discussions/categories/q-a")
	if err != nil || !got.IsDiscussion || got.Repo != "lsegal/aviary" || got.DiscussionCategory != "q-a" {
		t.Fatalf("discussion category target = %#v, %v", got, err)
	}
	if _, err := parseTarget("https://github.com/lsegal/aviary/discussions/categories/"); err == nil {
		t.Fatal("expected an error for an empty discussion category")
	}
}

func TestExpandTargetShorthand(t *testing.T) {
	origin := func() (string, bool) { return "lsegal/aviary", true }
	for _, tt := range []struct{ in, want string }{
		{"projects:3", "https://github.com/lsegal/aviary/projects/3"},
		{"projects:other/repo/4", "https://github.com/other/repo/projects/4"},
		{"discussions:q-a", "https://github.com/lsegal/aviary/discussions/categories/q-a"},
		{"discussions:other/repo/q-a", "https://github.com/other/repo/discussions/categories/q-a"},
		{"discussions:other/repo", "https://github.com/other/repo/discussions"},
		{"discussions:/other/repo/q-a/", "https://github.com/other/repo/discussions/categories/q-a"},
		{"owner/repo", "owner/repo"},
		{"https://github.com/users/lsegal/projects/3", "https://github.com/users/lsegal/projects/3"},
	} {
		got, err := expandTargetShorthand(tt.in, origin)
		if err != nil || got != tt.want {
			t.Fatalf("expandTargetShorthand(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
		if _, err := parseTarget(got); err != nil {
			t.Fatalf("parseTarget(%q) = %v", got, err)
		}
	}
	for _, in := range []string{"projects:", "projects:lsegal/3", "projects:abc", "projects:other/repo/abc", "discussions:a/b/c/d"} {
		if got, err := expandTargetShorthand(in, origin); err == nil {
			t.Fatalf("expandTargetShorthand(%q) = %q, want an error", in, got)
		}
	}
}

func TestExpandTargetShorthandWithoutOriginRemote(t *testing.T) {
	none := func() (string, bool) { return "", false }
	for _, in := range []string{"projects:3", "discussions:q-a"} {
		if _, err := expandTargetShorthand(in, none); err == nil {
			t.Fatalf("expandTargetShorthand(%q) without an origin remote should fail", in)
		}
	}
	if got, err := expandTargetShorthand("owner/repo", none); err != nil || got != "owner/repo" {
		t.Fatalf("expandTargetShorthand(owner/repo) = %q, %v", got, err)
	}
}

func TestGHCLIListUnansweredDiscussions(t *testing.T) {
	var calls [][]string
	gh := GHCLI{runCommand: func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		return []byte(`{"data":{"repository":{"discussions":{"nodes":[
			{"number":5,"title":"Unanswered","body":"question","createdAt":"2026-08-01T00:00:00Z","comments":{"totalCount":0}},
			{"number":3,"title":"Answered","body":"question","createdAt":"2026-07-01T00:00:00Z","comments":{"totalCount":2}}
		]}}}}`), nil
	}}
	discussions, err := gh.ListUnansweredDiscussions(context.Background(), "https://github.com/owner/repo/discussions")
	if err != nil {
		t.Fatalf("ListUnansweredDiscussions: %v", err)
	}
	if len(discussions) != 1 || discussions[0].Number != 5 || discussions[0].Title != "Unanswered" {
		t.Fatalf("discussions = %#v", discussions)
	}
	if len(calls) != 1 || calls[0][0] != "api" || calls[0][1] != "graphql" {
		t.Fatalf("call = %#v", calls)
	}
}

func TestGHCLIListUnansweredDiscussionsFiltersByCategory(t *testing.T) {
	gh := GHCLI{runCommand: func(_ context.Context, _ ...string) ([]byte, error) {
		return []byte(`{"data":{"repository":{"discussions":{"nodes":[
			{"number":5,"title":"Question","body":"q","createdAt":"2026-08-01T00:00:00Z","comments":{"totalCount":0},"category":{"slug":"q-a","name":"Q&A"}},
			{"number":6,"title":"Idea","body":"i","createdAt":"2026-08-02T00:00:00Z","comments":{"totalCount":0},"category":{"slug":"ideas","name":"Ideas"}}
		]}}}}`), nil
	}}
	for _, target := range []string{
		"https://github.com/owner/repo/discussions/categories/q-a",
		"https://github.com/owner/repo/discussions/categories/Q-A",
		"https://github.com/owner/repo/discussions/categories/Q&A",
	} {
		discussions, err := gh.ListUnansweredDiscussions(context.Background(), target)
		if err != nil {
			t.Fatalf("ListUnansweredDiscussions(%q): %v", target, err)
		}
		if len(discussions) != 1 || discussions[0].Number != 5 {
			t.Fatalf("discussions for %q = %#v", target, discussions)
		}
	}
	discussions, err := gh.ListUnansweredDiscussions(context.Background(), "https://github.com/owner/repo/discussions")
	if err != nil || len(discussions) != 2 {
		t.Fatalf("uncategorized target = %#v, %v", discussions, err)
	}
}

func TestGHCLIListUnansweredDiscussionsRejectsNonDiscussionTarget(t *testing.T) {
	gh := GHCLI{runCommand: func(_ context.Context, _ ...string) ([]byte, error) {
		t.Fatal("runCommand should not be called for a non-discussion target")
		return nil, nil
	}}
	if _, err := gh.ListUnansweredDiscussions(context.Background(), "owner/repo"); err == nil {
		t.Fatal("expected an error for a non-discussion target")
	}
}

func TestGHCLIListUnansweredDiscussionsReturnsError(t *testing.T) {
	gh := GHCLI{runCommand: func(_ context.Context, _ ...string) ([]byte, error) {
		return []byte("boom"), errors.New("exit status 1")
	}}
	if _, err := gh.ListUnansweredDiscussions(context.Background(), "https://github.com/owner/repo/discussions"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestParseGitHubRemote(t *testing.T) {
	for _, input := range []string{
		"https://github.com/lsegal/aviary",
		"https://github.com/lsegal/aviary.git",
		"git@github.com:lsegal/aviary.git",
		"ssh://git@github.com/lsegal/aviary.git",
	} {
		repo, ok := parseGitHubRemote(input)
		if !ok || repo != "lsegal/aviary" {
			t.Fatalf("parseGitHubRemote(%q) = %q, %v", input, repo, ok)
		}
	}
	for _, input := range []string{
		"",
		"https://gitlab.com/lsegal/aviary",
		"git@gitlab.com:lsegal/aviary.git",
	} {
		if _, ok := parseGitHubRemote(input); ok {
			t.Fatalf("parseGitHubRemote(%q) unexpectedly succeeded", input)
		}
	}
}

func TestOriginRemoteTarget(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("remote", "add", "origin", "https://github.com/lsegal/aviary.git")

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd) //nolint:errcheck

	repo, ok := originRemoteTarget()
	if !ok || repo != "lsegal/aviary" {
		t.Fatalf("originRemoteTarget() = %q, %v", repo, ok)
	}
}

func TestIssueRepositoryUsesProjectItemRepository(t *testing.T) {
	issue := Issue{Number: 32, Repository: "lsegal/aviary"}
	if got := issueRepository("https://github.com/users/lsegal/projects/3", issue); got != "lsegal/aviary" {
		t.Fatalf("issue repository = %q", got)
	}
}

func TestIssueRepositoryNormalizesRepositoryURL(t *testing.T) {
	issue := Issue{Number: 32}
	if got := issueRepository("https://github.com/lsegal/aviary", issue); got != "lsegal/aviary" {
		t.Fatalf("issue repository = %q", got)
	}
}

func TestProjectListArgs(t *testing.T) {
	got := projectListArgs(target{Owner: "lsegal", ProjectID: "3", IsProject: true}, "label:other status=closed", false)
	want := []string{"project", "item-list", "3", "--owner", "lsegal", "--format", "json", "--limit", "1000", "--query", "is:issue is:open label:other status=closed"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestProjectListArgsOmitsDefaultFilter(t *testing.T) {
	got := projectListArgs(target{Owner: "lsegal", ProjectID: "3", IsProject: true}, defaultIssueFilter, false)
	want := []string{"project", "item-list", "3", "--owner", "lsegal", "--format", "json", "--limit", "1000", "--query", "is:issue is:open"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestUserProjectListUsesURLOwnerType(t *testing.T) {
	var calls [][]string
	gh := GHCLI{runCommand: func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		return []byte(`{"data":{"user":{"projectV2":{"items":{"nodes":[{"id":"PVTI_item","fieldValueByName":{"name":"Todo"},"content":{"__typename":"Issue","number":171,"title":"bug","body":"details","state":"OPEN","createdAt":"2026-07-20T17:38:43Z","repository":{"nameWithOwner":"lsegal/aviary"},"labels":{"nodes":[{"name":"bug"}]}}}],"pageInfo":{"hasNextPage":false,"endCursor":"cursor"}}}}}}`), nil
	}}
	issues, err := gh.ListIssues(context.Background(), "https://github.com/users/lsegal/projects/3")
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || len(calls[0]) < 4 || calls[0][0] != "api" || calls[0][1] != "graphql" || !strings.Contains(calls[0][3], "user(login:$login)") {
		t.Fatalf("gh calls = %#v, want typed user GraphQL query", calls)
	}
	if len(issues) != 1 || issues[0].Number != 171 || issues[0].Repository != "lsegal/aviary" || issues[0].ProjectStatus != "Todo" || issues[0].ProjectItemID != "PVTI_item" || len(issues[0].Labels) != 1 {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestOrganizationProjectListUsesURLOwnerTypeAndPaginates(t *testing.T) {
	responses := [][]byte{
		[]byte(`{"data":{"organization":{"projectV2":{"items":{"nodes":[],"pageInfo":{"hasNextPage":true,"endCursor":"next"}}}}}}`),
		[]byte(`{"data":{"organization":{"projectV2":{"items":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":"done"}}}}}}`),
	}
	var calls [][]string
	gh := GHCLI{runCommand: func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		return responses[len(calls)-1], nil
	}}
	if _, err := gh.ListIssues(context.Background(), "https://github.com/orgs/example/projects/4"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || !strings.Contains(calls[0][3], "organization(login:$login)") || !slices.Contains(calls[1], "after=next") {
		t.Fatalf("gh calls = %#v, want paginated organization GraphQL queries", calls)
	}
}

func TestDecodeProjectIssues(t *testing.T) {
	got, err := decodeProjectIssues([]byte(`{"items":[{"id":"PVTI_item","status":"In Progress","content":{"number":7,"title":"bug","repository":"owner/repo","type":"Issue"}},{"content":{"number":8,"type":"PullRequest"}}]}`), nil)
	if err != nil || len(got) != 1 || got[0].Number != 7 || got[0].Repository != "owner/repo" || got[0].ProjectStatus != "In Progress" || got[0].ProjectItemID != "PVTI_item" {
		t.Fatalf("decode project issues = %#v, %v", got, err)
	}
}

func TestDecodeProjectItemsArray(t *testing.T) {
	items, err := decodeProjectItems([]byte(`[{"id":"PVTI_item","content":{"number":7,"type":"Issue"}}]`), nil)
	if err != nil || len(items) != 1 || items[0].ID != "PVTI_item" || items[0].Content.Number != 7 {
		t.Fatalf("decode project items = %#v, %v", items, err)
	}
}

func TestDecodeProjectFields(t *testing.T) {
	fields, err := decodeProjectFields([]byte(`{"fields":[{"id":"PVTF_status","name":"Status","options":[{"id":"opt_progress","name":"In Progress"}]}]}`), nil)
	if err != nil || len(fields) != 1 || fields[0].ID != "PVTF_status" || fields[0].Options[0].ID != "opt_progress" {
		t.Fatalf("decode project fields = %#v, %v", fields, err)
	}
}

func TestDecodeProjectFieldsIncludesOutputDetailOnFailure(t *testing.T) {
	_, err := decodeProjectFields([]byte("missing required scopes [project]"), errors.New("exit status 1"))
	if err == nil || !strings.Contains(err.Error(), "missing required scopes [project]") {
		t.Fatalf("decodeProjectFields() error = %v, want it to include the gh output detail", err)
	}
}

func TestDecodeRepositoryProjectItemsPage(t *testing.T) {
	var page repositoryProjectItemsPage
	err := json.Unmarshal([]byte(`{"data":{"repository":{"issue":{"projectItems":{"nodes":[{"id":"PVTI_item","project":{"id":"PVT_project","number":3,"owner":{"login":"owner"}}}],"pageInfo":{"hasNextPage":true,"endCursor":"cursor"}}}}}}`), &page)
	items := page.Data.Repository.Issue.ProjectItems
	if err != nil || len(items.Nodes) != 1 || items.Nodes[0].ID != "PVTI_item" || items.Nodes[0].Project.ID != "PVT_project" || items.Nodes[0].Project.Number != 3 || items.Nodes[0].Project.Owner.Login != "owner" {
		t.Fatalf("repository project items = %#v, %v", items.Nodes, err)
	}
	if !items.PageInfo.HasNextPage || items.PageInfo.EndCursor != "cursor" {
		t.Fatalf("repository project page info = %#v", items.PageInfo)
	}
}

func TestSetIssueStatusProjectItemLookupSurfacesFailureDetail(t *testing.T) {
	var calls [][]string
	gh := GHCLI{runCommand: func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		return []byte("some other project error"), errors.New("exit status 1")
	}}
	err := gh.SetIssueStatus(context.Background(), "https://github.com/users/owner/projects/3", Issue{Number: 7}, "In Progress")
	if err == nil || !strings.Contains(err.Error(), "some other project error") {
		t.Fatalf("SetIssueStatus() error = %v, want it to include the gh output detail", err)
	}
	want := projectListArgs(target{Owner: "owner", ProjectID: "3", IsProject: true}, "", true)
	if len(calls) != 1 || !reflect.DeepEqual(calls[0], want) {
		t.Fatalf("gh calls = %#v, want single call %#v", calls, want)
	}
}

func TestRepositoryIssueStatusUpdatesAttachedProject(t *testing.T) {
	responses := [][]byte{
		[]byte(`{"data":{"repository":{"issue":{"projectItems":{"nodes":[{"id":"PVTI_item","project":{"id":"PVT_project","number":3,"owner":{"login":"owner"}}}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}}`),
		[]byte(`{"id":"PVT_project"}`),
		[]byte(`{"fields":[{"id":"PVTSSF_status","name":"Status","options":[{"id":"opt_progress","name":"In Progress"}]}]}`),
		nil,
	}
	var calls [][]string
	gh := GHCLI{runCommand: func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		if len(calls) > len(responses) {
			t.Fatalf("unexpected gh call: %#v", args)
		}
		return responses[len(calls)-1], nil
	}}
	if err := gh.SetIssueStatus(context.Background(), "owner/repo", Issue{Number: 148}, "In Progress"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 4 {
		t.Fatalf("gh calls = %#v, want GraphQL lookup plus three project update calls", calls)
	}
	if got := calls[0][:2]; !reflect.DeepEqual(got, []string{"api", "graphql"}) {
		t.Fatalf("project lookup call = %#v", calls[0])
	}
	wantEdit := []string{"project", "item-edit", "--id", "PVTI_item", "--field-id", "PVTSSF_status", "--project-id", "PVT_project", "--single-select-option-id", "opt_progress"}
	if !reflect.DeepEqual(calls[3], wantEdit) {
		t.Fatalf("project edit call = %#v, want %#v", calls[3], wantEdit)
	}
}

func TestRepositoryIssueStatusReportsProjectViewFailureDetail(t *testing.T) {
	responses := [][]byte{
		[]byte(`{"data":{"repository":{"issue":{"projectItems":{"nodes":[{"id":"PVTI_item","project":{"id":"PVT_project","number":3,"owner":{"login":"owner"}}}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}}`),
		[]byte("missing required scopes [read:project]"),
	}
	call := 0
	gh := GHCLI{runCommand: func(_ context.Context, _ ...string) ([]byte, error) {
		response := responses[call]
		call++
		if call == 2 {
			return response, errors.New("exit status 1")
		}
		return response, nil
	}}
	err := gh.SetIssueStatus(context.Background(), "owner/repo", Issue{Number: 148}, "In Progress")
	if err == nil || !strings.Contains(err.Error(), "missing required scopes [read:project]") {
		t.Fatalf("SetIssueStatus() error = %v, want it to include the gh output detail", err)
	}
}

func TestProjectStatusOptionMatchesReadyStates(t *testing.T) {
	fields := []projectField{{
		ID:   "status-field",
		Name: "Status",
		Options: []projectFieldOption{
			{ID: "backlog-option", Name: "Backlog"},
			{ID: "ready-option", Name: "READY"},
			{ID: "custom-option", Name: "Agent Queue"},
		},
	}}
	for _, test := range []struct {
		status        string
		allowFallback bool
		wantOption    string
	}{
		{status: "ready", wantOption: "ready-option"},
		{status: "Todo", allowFallback: true, wantOption: "ready-option"},
		{status: "agent queue", wantOption: "custom-option"},
		{status: "Todo", wantOption: ""},
	} {
		fieldID, optionID := projectStatusOption(fields, test.status, test.allowFallback)
		if fieldID != "status-field" || optionID != test.wantOption {
			t.Errorf("projectStatusOption(%q, %v) = (%q, %q), want (%q, %q)", test.status, test.allowFallback, fieldID, optionID, "status-field", test.wantOption)
		}
	}
}

func TestDecodeProjectIssuesReportsMissingScope(t *testing.T) {
	_, err := decodeProjectIssues([]byte("error: your authentication token is missing required scopes [read:project]"), errors.New("exit status 1"))
	if err == nil || !strings.Contains(err.Error(), "gh auth refresh -s read:project") {
		t.Fatalf("missing scope error = %v", err)
	}
}

func TestProjectStatusErrorReportsWriteScope(t *testing.T) {
	detail := "error: your authentication token is missing required scopes [project]"
	if !strings.Contains(projectStatusError(45, errors.New("exit status 1"), detail).Error(), "gh auth refresh -s project") {
		t.Fatal("project status error did not report the project scope")
	}
}

func TestFilterFlagAccumulatesValues(t *testing.T) {
	got := filterFlag{values: []string{defaultIssueFilter}}
	if err := got.Set("label:bug"); err != nil {
		t.Fatal(err)
	}
	if err := got.Set("author:lsegal"); err != nil {
		t.Fatal(err)
	}
	if got.String() != "label:bug author:lsegal" {
		t.Fatalf("filter = %q", got.String())
	}
}

func TestFilterFlagDefaultsToMyOpenIssues(t *testing.T) {
	got := filterFlag{values: []string{defaultIssueFilter}}
	if got.String() != defaultIssueFilter {
		t.Fatalf("filter = %q, want %q", got.String(), defaultIssueFilter)
	}
}

func TestSplitAllowedCommenters(t *testing.T) {
	if got := splitAllowedCommenters(""); got != nil {
		t.Fatalf("splitAllowedCommenters(%q) = %#v, want nil", "", got)
	}
	got := splitAllowedCommenters(" lsegal, other , ,third")
	want := []string{"lsegal", "other", "third"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitAllowedCommenters = %#v, want %#v", got, want)
	}
}

func TestAgentFlagAccumulatesValues(t *testing.T) {
	got := agentFlag{values: []agentSpec{{Name: "codex"}}}
	if err := got.Set("claude"); err != nil {
		t.Fatal(err)
	}
	if err := got.Set("codex"); err != nil {
		t.Fatal(err)
	}
	// Each accumulated spec carries the managed default its definition
	// declares, so the flag's string form is what a resumed run parses back.
	if want := "claude/" + claudeDefaultModel + ",codex/" + codexDefaultModel; got.String() != want {
		t.Fatalf("agents = %q, want %q", got.String(), want)
	}
}

func TestAgentFlagRejectsUnknownAgent(t *testing.T) {
	got := agentFlag{values: []agentSpec{{Name: "codex"}}}
	if err := got.Set("nosuchagent"); err == nil {
		t.Fatal("expected error for unknown agent")
	}
}

func TestBugReportURL(t *testing.T) {
	got, err := bugReportURL("owner/repo", Issue{Number: 12}, []string{"agent", "--flag"}, "panic: boom\nexit status 2")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(got)
	if err != nil || u.Path != "/owner/repo/issues/new" {
		t.Fatalf("URL = %q, %v", got, err)
	}
	if !strings.Contains(got, "bug_report.md") {
		t.Fatalf("URL is missing the bug report template: %s", got)
	}
	body := u.Query().Get("body")
	if !strings.Contains(body, "panic: boom\nexit status 2") {
		t.Fatalf("body omits the agent output: %q", body)
	}
	if strings.Contains(body, "robot output omitted") {
		t.Fatalf("body still uses the placeholder: %q", body)
	}
}

func TestBugReportURLWithoutOutput(t *testing.T) {
	got, err := bugReportURL("owner/repo", Issue{Number: 12}, []string{"agent"}, "   \n  ")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if body := u.Query().Get("body"); !strings.Contains(body, "[no agent output was captured]") {
		t.Fatalf("body = %q, want the no-output note", body)
	}
}

func TestAgentOutputTailWriterKeepsTailAndPassesThrough(t *testing.T) {
	var seen bytes.Buffer
	w := &agentOutputTailWriter{output: &seen, limit: 8}
	for _, chunk := range []string{"abcdef", "ghij", "klmno"} {
		if n, err := w.Write([]byte(chunk)); err != nil || n != len(chunk) {
			t.Fatalf("Write(%q) = %d, %v", chunk, n, err)
		}
	}
	if got := seen.String(); got != "abcdefghijklmno" {
		t.Fatalf("passthrough = %q, want the full stream", got)
	}
	if got := w.Tail(); got != "hijklmno" {
		t.Fatalf("Tail() = %q, want the last 8 bytes", got)
	}
}

func TestAgentOutputTailWriterStripsControlSequences(t *testing.T) {
	w := &agentOutputTailWriter{output: io.Discard, limit: agentOutputTailLimit}
	if _, err := w.Write([]byte("\x1b[31mfatal:\x1b[0m no such file\r\n")); err != nil {
		t.Fatal(err)
	}
	if got := w.Tail(); got != "fatal: no such file" {
		t.Fatalf("Tail() = %q, want the escape sequences removed", got)
	}
}

func TestBugReportOutputSectionNotesTruncation(t *testing.T) {
	got := bugReportOutputSection(strings.Repeat("x", agentOutputTailLimit))
	if !strings.HasPrefix(got, "Last ") {
		t.Fatalf("section = %q, want a truncation note", got)
	}
	if short := bugReportOutputSection("boom"); strings.HasPrefix(short, "Last ") {
		t.Fatalf("section = %q, want no truncation note for short output", short)
	}
}

func TestProjectStateFingerprintIgnoresItemOrder(t *testing.T) {
	items := []projectItem{
		{ID: "a", Status: "Todo", Content: &projectContent{Issue: Issue{Number: 1, Repository: "o/r", State: "OPEN"}, Type: "Issue"}},
		{ID: "b", Status: "Done", Content: &projectContent{Issue: Issue{Number: 2, Repository: "o/r", State: "OPEN"}, Type: "Issue"}},
	}
	reordered := []projectItem{items[1], items[0]}
	if projectItemsFingerprint(items) != projectItemsFingerprint(reordered) {
		t.Fatal("reordered board items produced a different fingerprint")
	}
	moved := []projectItem{items[0], {ID: "b", Status: "Todo", Content: items[1].Content}}
	if projectItemsFingerprint(items) == projectItemsFingerprint(moved) {
		t.Fatal("moving a card between columns did not change the fingerprint")
	}
	added := append(append([]projectItem(nil), items...), projectItem{ID: "c", Status: "Todo", Content: &projectContent{Issue: Issue{Number: 3, Repository: "o/r", State: "OPEN"}, Type: "Issue"}})
	if projectItemsFingerprint(items) == projectItemsFingerprint(added) {
		t.Fatal("dragging a new issue onto the board did not change the fingerprint")
	}
}

func TestProjectStateQueriesOnlyBoardKeyFields(t *testing.T) {
	var calls [][]string
	gh := GHCLI{runCommand: func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		return []byte(`{"data":{"user":{"projectV2":{"items":{"nodes":[{"id":"PVTI_item","fieldValueByName":{"name":"Todo"},"content":{"__typename":"Issue","number":171,"state":"OPEN","repository":{"nameWithOwner":"lsegal/aviary"}}}],"pageInfo":{"hasNextPage":false,"endCursor":"cursor"}}}}}}`), nil
	}}
	state, err := gh.ProjectState(context.Background(), "https://github.com/users/lsegal/projects/3")
	if err != nil {
		t.Fatal(err)
	}
	if state == "" {
		t.Fatal("project state fingerprint is empty")
	}
	if len(calls) != 1 {
		t.Fatalf("gh calls = %#v, want a single request", calls)
	}
	query := calls[0][3]
	for _, unwanted := range []string{"body", "labels(", "createdAt", "title"} {
		if strings.Contains(query, unwanted) {
			t.Fatalf("probe query fetches %q, want only board key fields:\n%s", unwanted, query)
		}
	}
	if !strings.Contains(query, "number state repository{nameWithOwner}") {
		t.Fatalf("probe query missing board key fields:\n%s", query)
	}
}

func TestProjectStateRejectsRepositoryTarget(t *testing.T) {
	gh := GHCLI{runCommand: func(_ context.Context, _ ...string) ([]byte, error) {
		t.Fatal("repository target should not reach gh")
		return nil, nil
	}}
	if _, err := gh.ProjectState(context.Background(), "lsegal/aviary"); err == nil {
		t.Fatal("repository target did not produce an error")
	}
}

// The stub agent that writeFakeAgent installs is the test binary itself, so
// these variables carry its behaviour into the child process.
const (
	fakeAgentLogEnv          = "AVIARY_FAKE_AGENT_LOG"
	fakeAgentResumeOutputEnv = "AVIARY_FAKE_AGENT_RESUME_OUTPUT"
	fakeAgentResumeCodeEnv   = "AVIARY_FAKE_AGENT_RESUME_CODE"
)

func TestMain(m *testing.M) {
	if log := os.Getenv(fakeAgentLogEnv); log != "" {
		os.Exit(runFakeAgent(log, os.Args[1:]))
	}
	code := m.Run()
	// The contract harness builds testdata/fakeagent once for the whole suite.
	removeFakeAgentCLI()
	os.Exit(code)
}

// runFakeAgent records the invocation and answers a resume the way a dead
// agent session would, so RunSession's restart handling can be exercised on
// every platform.
func runFakeAgent(log string, args []string) int {
	file, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		_ = file.Close()
		return 1
	}
	ceiling, ceilingSet := os.LookupEnv("CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS")
	if _, err := fmt.Fprintf(file, "cwd=%s\nbg_wait_ceiling_set=%t bg_wait_ceiling=%s\n%s\n<<<END>>>\n", cwd, ceilingSet, ceiling, strings.Join(args, " ")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		_ = file.Close()
		return 1
	}
	if err := file.Close(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for _, arg := range args {
		if arg == "--resume" || arg == "resume" {
			fmt.Println(os.Getenv(fakeAgentResumeOutputEnv))
			code, err := strconv.Atoi(os.Getenv(fakeAgentResumeCodeEnv))
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			return code
		}
	}
	fmt.Println("started")
	return 0
}

func TestCommandArgsPromptResumedSessionWithIssueUpdate(t *testing.T) {
	runner := CommandRunner{Agent: "claude", Repo: "o/r", Identity: "SELF"}
	session := AgentSession{ID: "sess-1", Agent: "claude", Resume: true, Update: "The description of issue #7 changed while you were working on it."}
	args := commandArgsForSession(runner, Issue{Number: 7, Target: "o/r"}, session)
	prompt := args[len(args)-1]
	if !strings.HasPrefix(prompt, session.Update) {
		t.Fatalf("resumed prompt did not lead with the update: %q", prompt)
	}
	if strings.Contains(prompt, "Recover the existing work") {
		t.Fatalf("resumed prompt used the generic recovery text: %q", prompt)
	}
	if !slices.Contains(args, "--resume") || !slices.Contains(args, "sess-1") {
		t.Fatalf("update was not delivered to the existing session: %v", args)
	}
	// A resume with no update keeps the generic continuation prompt.
	plain := commandArgsForSession(runner, Issue{Number: 7, Target: "o/r"}, AgentSession{ID: "sess-1", Agent: "claude", Resume: true})
	if !strings.Contains(plain[len(plain)-1], "Recover the existing work") {
		t.Fatalf("plain resume lost its recovery prompt: %q", plain[len(plain)-1])
	}
}

// TestGHCLIBoundsEveryCommand checks each `gh` call carries a deadline of its
// own. Every read the poll loop makes against GitHub is one of these calls, so
// an invocation that hangs used to park the goroutine that dispatches work for
// the rest of the run.
func TestGHCLIBoundsEveryCommand(t *testing.T) {
	var deadline time.Time
	var bounded bool
	gh := GHCLI{runCommand: func(ctx context.Context, _ ...string) ([]byte, error) {
		deadline, bounded = ctx.Deadline()
		return nil, nil
	}}
	if _, err := gh.run(context.Background(), "api", "user"); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !bounded {
		t.Fatal("the gh call ran with no deadline")
	}
	if left := time.Until(deadline); left <= 0 || left > ghCommandTimeout {
		t.Fatalf("deadline in %s, want one within the %s command timeout", left, ghCommandTimeout)
	}
}

// TestGHCLIReportsATimedOutCommand checks a `gh` call that runs out of time
// fails with a description of what happened rather than a bare context error,
// and that a run being shut down is not reported as one.
func TestGHCLIReportsATimedOutCommand(t *testing.T) {
	previous := ghCommandTimeout
	ghCommandTimeout = 20 * time.Millisecond
	defer func() { ghCommandTimeout = previous }()
	gh := GHCLI{runCommand: func(ctx context.Context, _ ...string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	_, err := gh.run(context.Background(), "issue", "list")
	if err == nil || !strings.Contains(err.Error(), "did not finish within") {
		t.Fatalf("error %v, want one naming the command that ran out of time", err)
	}
	if !strings.Contains(err.Error(), "gh issue list") {
		t.Fatalf("error %v, want it to name the gh call", err)
	}
	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := gh.run(stopped, "issue", "list"); err == nil || strings.Contains(err.Error(), "did not finish within") {
		t.Fatalf("error %v, want the shutdown reported as a cancellation", err)
	}
}

// TestGHCLIRunUsesItsOwnTimeoutAndFoldsOutputIntoErrors checks a GHCLI with
// a Timeout is bounded by it, and that Run's error carries what gh printed.
func TestGHCLIRunUsesItsOwnTimeoutAndFoldsOutputIntoErrors(t *testing.T) {
	gh := GHCLI{Timeout: 20 * time.Millisecond, runCommand: func(ctx context.Context, _ ...string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	_, err := gh.Run(context.Background(), "auth", "status")
	if err == nil || !strings.Contains(err.Error(), "gh auth status did not finish within") {
		t.Fatalf("error %v, want the call reported as timed out at its own deadline", err)
	}

	gh = GHCLI{runCommand: func(context.Context, ...string) ([]byte, error) {
		return []byte("HTTP 404: Not Found\n"), errors.New("exit status 1")
	}}
	out, err := gh.Run(context.Background(), "api", "repos/a/b")
	if out != nil || err == nil || err.Error() != "exit status 1: HTTP 404: Not Found" {
		t.Fatalf("Run = %q, %v; want gh's output folded into the error", out, err)
	}

	gh = GHCLI{runCommand: func(context.Context, ...string) ([]byte, error) { return []byte("ok"), nil }}
	if out, err := gh.Run(context.Background(), "api", "user"); err != nil || string(out) != "ok" {
		t.Fatalf("Run = %q, %v; want stdout", out, err)
	}
}

func TestRemoteControlCodexNoticeWarnsWhenCodexIsConfigured(t *testing.T) {
	notice := remoteControlCodexNotice(true, []string{"codex"})
	if notice != codexRemoteControlNotice {
		t.Fatalf("notice = %q, want the codex remote control notice", notice)
	}
	if !strings.Contains(notice, "codex remote-control") {
		t.Errorf("notice = %q, want it to name the codex remote-control daemon", notice)
	}
}

func TestRemoteControlCodexNoticeWarnsWhenCodexIsOneOfSeveralAgents(t *testing.T) {
	if got := remoteControlCodexNotice(true, []string{"claude", "codex"}); got != codexRemoteControlNotice {
		t.Fatalf("notice = %q, want the codex remote control notice", got)
	}
}

func TestRemoteControlCodexNoticeSilentWithoutCodex(t *testing.T) {
	if got := remoteControlCodexNotice(true, []string{"claude"}); got != "" {
		t.Fatalf("notice = %q, want no notice for a claude-only watch", got)
	}
}

func TestRemoteControlCodexNoticeSilentWhenRemoteControlIsOff(t *testing.T) {
	if got := remoteControlCodexNotice(false, []string{"codex"}); got != "" {
		t.Fatalf("notice = %q, want no notice when remote control is off", got)
	}
}

// TestRemoteControlNoLeverFindingNamesEveryCandidate keeps the recorded finding
// from #506 complete: the point of the constant is that the next reader does not
// have to re-measure the three levers, so it has to name all of them and where
// the remaining answer is tracked.
func TestRemoteControlNoLeverFindingNamesEveryCandidate(t *testing.T) {
	for _, required := range []string{"remoteControlAtStartup", "autoUploadSessions", "claude remote-control", remoteControlUpstreamIssue} {
		if !strings.Contains(remoteControlNoLeverFinding, required) {
			t.Errorf("remoteControlNoLeverFinding does not mention %q", required)
		}
	}
}

// TestRemoteControlUpstreamIssueIsAClaudeCodeIssue guards the one pointer that
// makes the finding actionable: a aviary issue or a bare repository URL here
// would send the next reader somewhere that cannot resolve it.
func TestRemoteControlUpstreamIssueIsAClaudeCodeIssue(t *testing.T) {
	if !strings.HasPrefix(remoteControlUpstreamIssue, "https://github.com/anthropics/claude-code/issues/") {
		t.Fatalf("remoteControlUpstreamIssue = %q, want an anthropics/claude-code issue URL", remoteControlUpstreamIssue)
	}
}

// TestRemoteControlInertNoticeWarnsWhenOptedIn covers the one thing turning the
// flag on still does: say that it reaches nobody. A watch left at the default
// has nothing to explain and must stay silent.
func TestRemoteControlInertNoticeWarnsWhenOptedIn(t *testing.T) {
	if got := remoteControlInertNotice(true); got != remoteControlNoLeverFinding {
		t.Errorf("notice = %q, want the recorded finding", got)
	}
	if got := remoteControlInertNotice(false); got != "" {
		t.Errorf("notice = %q, want no notice when remote control is off", got)
	}
}
