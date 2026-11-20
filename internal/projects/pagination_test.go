package projects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// nupp24Timeline builds the lsegal/nupp#24 shape from issue #759: 43 timeline
// events, with the current pull request (#190, a 3-item plan checklist)
// cross-referenced at index 39, on the second page of 30.
func nupp24Timeline(t *testing.T) (first, second []byte) {
	t.Helper()
	events := make([]map[string]any, 0, 43)
	for i := 0; i < 43; i++ {
		events = append(events, map[string]any{"event": "commented"})
	}
	// Older pull requests that merely mention the issue.
	for i, number := range []int{25, 11, 26, 27, 29} {
		events[i*5] = map[string]any{"event": "cross-referenced", "source": map[string]any{"issue": map[string]any{"number": number, "body": "Part of #24", "pull_request": map[string]any{}}}}
	}
	events[39] = map[string]any{"event": "cross-referenced", "source": map[string]any{"issue": map[string]any{"number": 190, "body": "Closes #24", "pull_request": map[string]any{"merged_at": nil}}}}
	first, err := json.Marshal(events[:30])
	if err != nil {
		t.Fatal(err)
	}
	second, err = json.Marshal(events[30:])
	if err != nil {
		t.Fatal(err)
	}
	return first, second
}

const nupp24PullRequest = `{"state":"open","draft":true,"body":"Closes #24\n\n## Plan\n\n- [x] Delete the crate\n- [ ] Repin\n- [ ] Verify"}`

// A closing pull request cross-referenced past the first timeline page is
// found on the public API, and the job's dashboard card shows its number and
// checklist progress.
func TestOriginatingWorkStateReadsTimelinePastFirstPagePublic(t *testing.T) {
	first, second := nupp24Timeline(t)
	var urls []string
	gh := GHCLI{
		publicRepoCache: &sync.Map{},
		runCommand: func(context.Context, ...string) ([]byte, error) {
			return nil, errors.New("should not run gh")
		},
		publicAPI: func(_ context.Context, requestURL string) ([]byte, http.Header, int, error) {
			urls = append(urls, requestURL)
			switch requestURL {
			case "https://api.github.com/repos/lsegal/nupp":
				return []byte(`{"private":false}`), nil, http.StatusOK, nil
			case "https://api.github.com/repos/lsegal/nupp/issues/24":
				return []byte(`{"state":"open"}`), nil, http.StatusOK, nil
			case "https://api.github.com/repos/lsegal/nupp/issues/24/timeline?per_page=100":
				return first, newPublicHeader(`<https://api.github.com/repositories/1/issues/24/timeline?per_page=100&page=2>; rel="next"`), http.StatusOK, nil
			case "https://api.github.com/repositories/1/issues/24/timeline?per_page=100&page=2":
				return second, newPublicHeader(""), http.StatusOK, nil
			case "https://api.github.com/repos/lsegal/nupp/pulls/190":
				return []byte(nupp24PullRequest), nil, http.StatusOK, nil
			}
			t.Fatalf("unexpected request %s", requestURL)
			return nil, nil, 0, nil
		},
	}
	state, err := gh.OriginatingWorkState(context.Background(), "lsegal/nupp", 24)
	want := PullRequestWorkState{Repository: "lsegal/nupp", Number: 190, State: "open", IsDraft: true, TasksDone: 1, TasksTotal: 3}
	if err != nil || !reflect.DeepEqual(state.PullRequests, []PullRequestWorkState{want}) {
		t.Fatalf("OriginatingWorkState() = (%#v, %v), want pull request %#v; requests %v", state, err, want, urls)
	}
	var job JobSnapshot
	if !job.SetPullRequest(state, "lsegal/nupp") {
		t.Fatal("SetPullRequest() reported no change for a job gaining its pull request")
	}
	if job.PullRequestLabel() != "PR #190" || job.TasksDone != 1 || job.TasksTotal != 3 {
		t.Fatalf("job = label %q, tasks %d/%d; want PR #190 with 1/3 tasks", job.PullRequestLabel(), job.TasksDone, job.TasksTotal)
	}
}

// The authenticated fallback asks gh for every timeline page too.
func TestOriginatingWorkStateReadsTimelinePastFirstPageWithGH(t *testing.T) {
	first, second := nupp24Timeline(t)
	var calls [][]string
	gh := GHCLI{runCommand: func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, args)
		switch args[1] {
		case "repos/lsegal/nupp/issues/24":
			return []byte(`{"state":"open"}`), nil
		case "repos/lsegal/nupp/issues/24/timeline?per_page=100":
			if !slices.Contains(args, "--paginate") {
				// gh without --paginate answers with the first page only.
				return first, nil
			}
			// gh --paginate merges the pages into one array.
			return append(append(first[:len(first)-1:len(first)-1], ','), second[1:]...), nil
		case "repos/lsegal/nupp/pulls/190":
			return []byte(nupp24PullRequest), nil
		}
		t.Fatalf("unexpected request %v", args)
		return nil, nil
	}}
	state, err := gh.OriginatingWorkState(context.Background(), "lsegal/nupp", 24)
	if err != nil || len(state.PullRequests) != 1 || state.PullRequests[0].Number != 190 || state.PullRequests[0].TasksTotal != 3 {
		t.Fatalf("OriginatingWorkState() = (%#v, %v), want pull request #190 with 3 tasks; calls %v", state, err, calls)
	}
}

// An issue whose timeline fits on one page reads exactly as before: one
// timeline request, no further pages.
func TestOriginatingWorkStateSinglePageTimeline(t *testing.T) {
	timelineRequests := 0
	gh := GHCLI{
		publicRepoCache: &sync.Map{},
		publicAPI: func(_ context.Context, requestURL string) ([]byte, http.Header, int, error) {
			switch {
			case requestURL == "https://api.github.com/repos/owner/repo":
				return []byte(`{"private":false}`), nil, http.StatusOK, nil
			case requestURL == "https://api.github.com/repos/owner/repo/issues/7":
				return []byte(`{"state":"open"}`), nil, http.StatusOK, nil
			case strings.Contains(requestURL, "/timeline"):
				timelineRequests++
				return []byte(`[{"event":"cross-referenced","source":{"issue":{"number":9,"body":"Closes #7","pull_request":{}}}}]`), newPublicHeader(""), http.StatusOK, nil
			case requestURL == "https://api.github.com/repos/owner/repo/pulls/9":
				return []byte(`{"state":"open"}`), nil, http.StatusOK, nil
			}
			t.Fatalf("unexpected request %s", requestURL)
			return nil, nil, 0, nil
		},
	}
	state, err := gh.OriginatingWorkState(context.Background(), "owner/repo", 7)
	if err != nil || len(state.PullRequests) != 1 || state.PullRequests[0].Number != 9 || timelineRequests != 1 {
		t.Fatalf("OriginatingWorkState() = (%#v, %v) after %d timeline requests", state, err, timelineRequests)
	}
}

// A list response read through a single-page helper is an error rather than a
// silently truncated list, on the public API and through gh alike, so a list
// endpoint that slips past review fails its first test.
func TestSinglePageHelpersRejectListResponses(t *testing.T) {
	gh := GHCLI{
		publicAPI: func(context.Context, string) ([]byte, http.Header, int, error) {
			return []byte(` [{"id":1}]`), nil, http.StatusOK, nil
		},
		runCommand: func(context.Context, ...string) ([]byte, error) {
			return []byte(`[{"id":1}]`), nil
		},
	}
	for _, public := range []bool{true, false} {
		if _, err := gh.apiGETObject(context.Background(), public, "repos/o/r/issues/7/timeline"); !errors.Is(err, errListReadOnePage) {
			t.Errorf("apiGETObject(public=%v) error = %v, want errListReadOnePage", public, err)
		}
	}
	binary := fakeQuotaBinary(t, `echo '[{"id":1}]'`)
	if _, err := (GHCLI{Binary: binary}).api(context.Background(), "repos/o/r/hooks", ""); !errors.Is(err, errListReadOnePage) {
		t.Errorf("api() error = %v, want errListReadOnePage", err)
	}
}

// Webhooks are listed through every page, so aviary's own webhook past the
// first page is found instead of duplicated.
func TestConfigureWebhookSpecListsEveryPage(t *testing.T) {
	log := filepath.Join(t.TempDir(), "args")
	binary := fakeQuotaBinary(t, fmt.Sprintf(`echo "$@" >> %s
case "$*" in
  *--paginate*) echo '[{"id":1,"config":{"url":"https://other"}},{"id":2,"config":{"url":"https://tunnel/webhook"},"events":["issues"]}]' ;;
  *) echo '[{"id":1,"config":{"url":"https://other"}}]' ;;
esac`, strconv.Quote(log)))
	spec := webhookSpec{apiPath: "repos/o/r/hooks", name: "o/r", events: []string{"issues"}}
	created, err := (GHCLI{Binary: binary}).configureWebhookSpec(context.Background(), spec, "https://tunnel/webhook", "")
	if err != nil || created {
		t.Fatalf("configureWebhookSpec() = %v, %v; want the existing webhook on page 2 found", created, err)
	}
}

// The open pull requests whose reviews are scanned for mentions are listed
// through every page.
func TestRecentReviewCommentsListsEveryOpenPullRequestPage(t *testing.T) {
	gh := GHCLI{runCommand: func(_ context.Context, args ...string) ([]byte, error) {
		switch {
		case strings.Contains(args[1], "pulls/comments?"):
			return []byte(`[]`), nil
		case strings.HasPrefix(args[1], "repos/o/r/pulls?"):
			if !slices.Contains(args, "--paginate") {
				t.Fatalf("open pull requests listed without --paginate: %v", args)
			}
			return []byte(`[]`), nil
		}
		t.Fatalf("unexpected request %v", args)
		return nil, nil
	}}
	if _, err := gh.RecentReviewComments(context.Background(), "o/r", time.Now()); err != nil {
		t.Fatal(err)
	}
}

// listEndpoint matches a path literal that ends in a GitHub REST collection,
// which GitHub pages. Single-object paths end in a number or a name instead.
var listEndpoint = regexp.MustCompile(`/(timeline|comments|events|issues|pulls|hooks|reviews|labels|assignees|sub_issues|blocked_by|blocking|forks|commits|files|releases|runs|jobs|reactions|stacks)(\?.*)?$`)

// TestNoListEndpointThroughSinglePageHelpers scans the source for single-page
// reads (apiGETObject, and api with no method) whose path names a list
// endpoint. The runtime guard catches the ones a test exercises; this catches
// the rest at review time. This is the third endpoint found reading one page
// of a list, so the class is guarded structurally.
func TestNoListEndpointThroughSinglePageHelpers(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			var path ast.Expr
			switch selector.Sel.Name {
			case "apiGETObject":
				if len(call.Args) >= 3 {
					path = call.Args[2]
				}
			case "api":
				if len(call.Args) >= 3 {
					if method, ok := call.Args[2].(*ast.BasicLit); ok && method.Value == `""` {
						path = call.Args[1]
					}
				}
			}
			if path == nil {
				return true
			}
			ast.Inspect(path, func(node ast.Node) bool {
				literal, ok := node.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					return true
				}
				if value, err := strconv.Unquote(literal.Value); err == nil && listEndpoint.MatchString(value) {
					t.Errorf("%s: %s reads list endpoint %q one page at a time; use apiGETPaginated or apiList", fset.Position(literal.Pos()), selector.Sel.Name, value)
				}
				return true
			})
			return true
		})
	}
}

// The source guard flags the call shapes that caused issues #722 and #759.
func TestListEndpointPattern(t *testing.T) {
	for path, want := range map[string]bool{
		"/timeline":                    true,
		"/timeline?per_page=100":       true,
		"/pulls?":                      true,
		"/dependencies/blocked_by":     true,
		"/sub_issues":                  true,
		"/issues/":                     false,
		"/pulls/":                      false,
		"repos/":                       false,
		"users/":                       false,
		"/stacks?per_page=1":           true,
		"repos/owner/repo/issues/7":    false,
		"repos/owner/repo/pulls/9/x/1": false,
	} {
		if got := listEndpoint.MatchString(path); got != want {
			t.Errorf("listEndpoint.MatchString(%q) = %v, want %v", path, got, want)
		}
	}
}

// fakeQuotaBinary writes a shell script that prints body on stdout and returns
// its path, so the generic reader can be exercised against a real process.
func fakeQuotaBinary(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake quota binary needs a POSIX shell")
	}
	path := filepath.Join(t.TempDir(), "fake-quota")
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
