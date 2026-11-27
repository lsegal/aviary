package projects

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/projects/core"
)

func TestCheckIssueFilterCountsMatches(t *testing.T) {
	m := newTestManager(t)
	var query string
	m.ghRunCommand = func(_ context.Context, args ...string) ([]byte, error) {
		for _, arg := range args {
			if q, ok := strings.CutPrefix(arg, "q="); ok {
				query = q
			}
		}
		return []byte(`{"total_count": 12, "items": []}`), nil
	}
	matches, err := m.CheckIssueFilter(context.Background(), "o/r", "  label:bug   -label:wip ")
	if err != nil {
		t.Fatal(err)
	}
	if matches != 12 {
		t.Fatalf("matches = %d, want 12", matches)
	}
	if want := "repo:o/r is:issue state:open label:bug -label:wip"; query != want {
		t.Fatalf("query = %q, want %q", query, want)
	}
}

func TestCheckIssueFilterRejectsInvalidSyntax(t *testing.T) {
	m := newTestManager(t)
	m.ghRunCommand = func(context.Context, ...string) ([]byte, error) {
		t.Error("an invalid filter reached GitHub")
		return nil, errors.New("unexpected")
	}
	for _, filter := range []string{"", "label:", `label:"good first`, "(label:bug", "-label:"} {
		if _, err := m.CheckIssueFilter(context.Background(), "o/r", filter); err == nil {
			t.Errorf("CheckIssueFilter(%q) succeeded, want an error", filter)
		}
	}
}

func TestCheckIssueFilterReportsGitHubsReason(t *testing.T) {
	m := newTestManager(t)
	m.ghRunCommand = func(context.Context, ...string) ([]byte, error) {
		return []byte(`{"message":"Validation Failed","errors":[{"message":"The listed users cannot be searched"}]}gh: Validation Failed (HTTP 422)`), errors.New("exit status 1")
	}
	_, err := m.CheckIssueFilter(context.Background(), "o/r", "assignee:nobody")
	if err == nil || err.Error() != "The listed users cannot be searched" {
		t.Fatalf("err = %v, want GitHub's reason", err)
	}
}

func TestValidateIssueFilterAcceptsSharedSyntax(t *testing.T) {
	for _, filter := range []string{"assignee:@me", `label:bug -label:"won't fix" assignee:@me`, "(label:a OR label:b) is:open", "no:assignee"} {
		if err := core.ValidateIssueFilter(filter); err != nil {
			t.Errorf("ValidateIssueFilter(%q) = %v", filter, err)
		}
	}
}

func TestTargetFiltersOverrideOnlyTheirProject(t *testing.T) {
	m := newTestManager(t)
	cfg := ownedBy(config.ProjectsConfig{Filter: []string{"label:shared"}}, "o/a", "o/b")
	cfg.Agents[0].Projects[0].Filter = []string{"label:mine"}
	m.Reconcile(cfg)
	status := m.Status(context.Background(), 0, nil)
	filters := status["issue_filters"].(map[string]string)
	if filters["o/a"] != "label:mine" || filters["o/b"] != "label:shared" {
		t.Fatalf("issue_filters = %v", filters)
	}
	if custom := status["custom_filters"].([]string); !slices.Equal(custom, []string{"o/a"}) {
		t.Fatalf("custom_filters = %v", custom)
	}
	if status["issue_filter"] != "label:shared" {
		t.Fatalf("shared issue_filter = %v", status["issue_filter"])
	}
}

func TestSetWatchFilterOverridesAndResets(t *testing.T) {
	m := newTestManager(t)
	if err := m.AddWatch(Watch{Targets: []string{"o/a", "o/b"}}); err != nil {
		t.Fatal(err)
	}
	if ok, err := m.SetWatchFilter("o/a", []string{"label:bug"}); !ok || err != nil {
		t.Fatalf("SetWatchFilter = %v, %v", ok, err)
	}
	if got := m.IssueFilter("o/a"); got != "label:bug" {
		t.Fatalf("o/a filter = %q", got)
	}
	if got := m.IssueFilter("o/b"); got != config.DefaultProjectsFilter {
		t.Fatalf("o/b filter = %q, want the default", got)
	}
	if ok, err := m.SetWatchFilter("o/a", nil); !ok || err != nil {
		t.Fatalf("reset = %v, %v", ok, err)
	}
	if got := m.IssueFilter("o/a"); got != config.DefaultProjectsFilter {
		t.Fatalf("o/a filter after reset = %q", got)
	}
	if ok, _ := m.SetWatchFilter("o/none", []string{"label:x"}); ok {
		t.Fatal("SetWatchFilter found a watch for an unwatched target")
	}
}

func TestGHCLIListIssuesUsesTargetFilter(t *testing.T) {
	var queries []string
	g := GHCLI{
		Filter:          "label:shared",
		FilterOverrides: core.NewFilterOverrides(map[string]string{"o/a": "label:mine"}),
		runCommand: func(_ context.Context, args ...string) ([]byte, error) {
			for _, arg := range args {
				if q, ok := strings.CutPrefix(arg, "q="); ok {
					queries = append(queries, q)
				}
			}
			return []byte(`{"items": []}`), nil
		},
	}
	for _, target := range []string{"o/a", "o/b"} {
		if _, err := g.ListIssues(context.Background(), target); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"repo:o/a is:issue state:open label:mine", "repo:o/b is:issue state:open label:shared"}
	if !slices.Equal(queries, want) {
		t.Fatalf("queries = %q, want %q", queries, want)
	}
	g.FilterOverrides.Set(nil)
	if filter, _ := g.filterFor("o/a"); filter != "label:shared" {
		t.Fatalf("filter after clearing overrides = %q", filter)
	}
}

func TestSetGHRunCommandRoutesFilterChecks(t *testing.T) {
	m := newTestManager(t)
	calls := 0
	m.SetGHRunCommand(func(context.Context, ...string) ([]byte, error) {
		calls++
		return []byte(`{"total_count": 3}`), nil
	})
	matches, err := m.CheckIssueFilter(context.Background(), "o/r", "label:bug")
	if err != nil || matches != 3 || calls != 1 {
		t.Fatalf("CheckIssueFilter = %d, %v after %d gh calls; want 3, nil after 1", matches, err, calls)
	}
}

func TestCheckIssueFilterRejectsBeforeCallingGH(t *testing.T) {
	m := newTestManager(t)
	m.ghRunCommand = func(context.Context, ...string) ([]byte, error) {
		t.Error("a rejected check reached GitHub")
		return nil, errors.New("unexpected")
	}
	for _, tc := range []struct{ name, target, filter, want string }{
		{"whitespace filter", "o/r", " \t\n ", "the filter is empty"},
		{"invalid syntax", "o/r", "(label:bug", ""},
		{"bad target", "not a target", "label:bug", "target must be OWNER/REPO"},
	} {
		matches, err := m.CheckIssueFilter(context.Background(), tc.target, tc.filter)
		if err == nil || matches != 0 {
			t.Errorf("%s: CheckIssueFilter = %d, %v; want 0 and an error", tc.name, matches, err)
			continue
		}
		if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %q, want it to contain %q", tc.name, err, tc.want)
		}
	}
}

func TestCheckIssueFilterCountsProjectIssues(t *testing.T) {
	m := newTestManager(t)
	var args []string
	m.ghRunCommand = func(_ context.Context, a ...string) ([]byte, error) {
		args = a
		return []byte(`{"data":{"organization":{"projectV2":{"items":{"nodes":[` +
			`{"id":"PVTI_1","fieldValueByName":{"name":"Todo"},"content":{"__typename":"Issue","number":1,"state":"OPEN","repository":{"nameWithOwner":"o/r"}}},` +
			`{"id":"PVTI_2","fieldValueByName":{"name":"Todo"},"content":{"__typename":"PullRequest"}},` +
			`{"id":"PVTI_3","fieldValueByName":{"name":"Done"},"content":{"__typename":"Issue","number":2,"state":"OPEN","repository":{"nameWithOwner":"o/r"}}}` +
			`],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}}`), nil
	}
	matches, err := m.CheckIssueFilter(context.Background(), "https://github.com/orgs/o/projects/4", "label:bug")
	if err != nil {
		t.Fatal(err)
	}
	if matches != 2 {
		t.Fatalf("matches = %d, want the 2 issue items", matches)
	}
	if !slices.Contains(args, "graphql") || !slices.Contains(args, "login=o") || !slices.Contains(args, "number=4") {
		t.Fatalf("gh args = %q, want a project items query for o's project 4", args)
	}
	if !slices.ContainsFunc(args, func(a string) bool { return strings.HasPrefix(a, "itemQuery=") && strings.Contains(a, "label:bug") }) {
		t.Fatalf("gh args = %q, want the filter in the item query", args)
	}
}

func TestCheckIssueFilterWrapsProjectErrors(t *testing.T) {
	m := newTestManager(t)
	m.ghRunCommand = func(context.Context, ...string) ([]byte, error) {
		return nil, errors.New("exit status 1")
	}
	_, err := m.CheckIssueFilter(context.Background(), "https://github.com/users/o/projects/3", "label:bug")
	if err == nil || !strings.HasPrefix(err.Error(), "check filter: ") || !strings.Contains(err.Error(), "exit status 1") {
		t.Fatalf("err = %v, want a wrapped project listing error", err)
	}
}

func TestCheckIssueFilterRejectsMalformedSearch(t *testing.T) {
	m := newTestManager(t)
	m.ghRunCommand = func(context.Context, ...string) ([]byte, error) {
		return []byte(`{"total_count": "many"`), nil
	}
	matches, err := m.CheckIssueFilter(context.Background(), "o/r", "label:bug")
	if err == nil || matches != 0 || !strings.Contains(err.Error(), "check filter: decode search") {
		t.Fatalf("CheckIssueFilter = %d, %v; want a decode error", matches, err)
	}
}

func TestCheckIssueFilterSearchErrorFallbacks(t *testing.T) {
	ghErr := errors.New("exit status 1")
	for _, tc := range []struct{ name, output, want string }{
		{"errors message", `{"message":"Validation Failed","errors":[{"message":""},{"message":"bad qualifier"}]}`, "bad qualifier"},
		{"top-level message only", `gh: {"message":"API rate limit exceeded","errors":[]}`, "API rate limit exceeded"},
		{"empty messages", `{"message":"","errors":[{"message":""}]}`, "check filter: exit status 1"},
		{"malformed JSON", `{"message":`, "check filter: exit status 1"},
		{"no JSON", "gh: could not connect", "check filter: exit status 1"},
		{"no output", "", "check filter: exit status 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestManager(t)
			m.ghRunCommand = func(context.Context, ...string) ([]byte, error) {
				return []byte(tc.output), ghErr
			}
			matches, err := m.CheckIssueFilter(context.Background(), "o/r", "label:bug")
			if err == nil || matches != 0 || err.Error() != tc.want {
				t.Fatalf("CheckIssueFilter = %d, %v; want 0, %q", matches, err, tc.want)
			}
			if strings.HasPrefix(tc.want, "check filter: ") && !errors.Is(err, ghErr) {
				t.Fatalf("err = %v, want it to wrap gh's error", err)
			}
		})
	}
}
