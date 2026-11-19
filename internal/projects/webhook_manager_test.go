package projects

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func projectItemsResponse(repos ...string) []byte {
	nodes := make([]string, 0, len(repos))
	for i, repo := range repos {
		nodes = append(nodes, fmt.Sprintf(`{"id":"item%d","content":{"__typename":"Issue","number":%d,"title":"t","state":"OPEN","repository":{"nameWithOwner":%q}}}`, i, i+1, repo))
	}
	return []byte(fmt.Sprintf(`{"data":{"user":{"projectV2":{"items":{"nodes":[%s],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}}`, strings.Join(nodes, ",")))
}

func orgProjectItemsResponse(repos ...string) []byte {
	return []byte(strings.Replace(string(projectItemsResponse(repos...)), `"user"`, `"organization"`, 1))
}

// An organization project keeps its projects_v2_item hook and gains a narrow
// issue hook on each repository backing the board, so a change to an issue an
// agent is working is delivered instead of waiting out the poll (issue #471).
func TestWebhookSpecsSupportOrganizationProjects(t *testing.T) {
	target, err := parseTarget("https://github.com/orgs/example/projects/3")
	if err != nil {
		t.Fatal(err)
	}
	gh := GHCLI{runCommand: func(_ context.Context, args ...string) ([]byte, error) {
		if args[0] != "api" || args[1] != "graphql" {
			return nil, fmt.Errorf("unexpected command %v", args)
		}
		return orgProjectItemsResponse("example/beta", "example/alpha", "example/beta"), nil
	}}
	specs, err := gh.webhookSpecs(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 3 {
		t.Fatalf("webhook specs = %#v", specs)
	}
	if specs[0].apiPath != "orgs/example/hooks" || specs[0].name != "organization project example" || !reflect.DeepEqual(specs[0].events, []string{"projects_v2_item"}) {
		t.Fatalf("webhook spec = %#v", specs[0])
	}
	if specs[1].apiPath != "repos/example/alpha/hooks" || specs[2].apiPath != "repos/example/beta/hooks" {
		t.Fatalf("webhook specs = %#v", specs)
	}
	if !reflect.DeepEqual(specs[1].events, []string{"issues", "issue_comment", "pull_request_review", "pull_request_review_comment", "ping"}) {
		t.Fatalf("webhook spec events = %#v", specs[1].events)
	}
}

// A board whose repositories cannot be listed still keeps the board hook the
// whole target depends on; the repositories are retried on the next cycle.
func TestWebhookSpecsKeepOrganizationBoardHookWhenRepositoriesUnavailable(t *testing.T) {
	target, err := parseTarget("https://github.com/orgs/example/projects/3")
	if err != nil {
		t.Fatal(err)
	}
	gh := GHCLI{runCommand: func(_ context.Context, _ ...string) ([]byte, error) {
		return nil, errors.New("HTTP 502")
	}}
	specs, err := gh.webhookSpecs(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 || specs[0].apiPath != "orgs/example/hooks" {
		t.Fatalf("webhook specs = %#v", specs)
	}
}

// The narrow project issue hook must not shrink a repository target's own
// hook when both watch the same repository.
func TestProjectIssueWebhookEventsAreASubsetOfRepositoryEvents(t *testing.T) {
	if missing := missingWebhookEvents(repositoryWebhookSpec("owner/repo").events, projectIssueWebhookSpec("owner/repo").events); len(missing) > 0 {
		t.Fatalf("project issue events missing from repository events: %v", missing)
	}
}

func TestOrganizationWebhookErrorsReportRequiredAccess(t *testing.T) {
	spec := webhookSpec{apiPath: "orgs/example/hooks", name: "organization project example"}
	err := webhookAccessError("list", spec, errors.New("HTTP 404"))
	if !strings.Contains(err.Error(), "organization-owner") || !strings.Contains(err.Error(), "gh auth refresh -s admin:org_hook") {
		t.Fatalf("organization webhook error = %v", err)
	}
}

// A user-owned project gets no projects_v2 webhook from GitHub, so push mode
// has to watch the repositories backing the board instead of falling back to
// polling for the whole target (issue #234).
func TestWebhookSpecsWatchPersonalProjectRepositories(t *testing.T) {
	target, err := parseTarget("https://github.com/users/octocat/projects/3")
	if err != nil {
		t.Fatal(err)
	}
	gh := GHCLI{runCommand: func(_ context.Context, args ...string) ([]byte, error) {
		if args[0] != "api" || args[1] != "graphql" {
			return nil, fmt.Errorf("unexpected command %v", args)
		}
		return projectItemsResponse("octocat/beta", "octocat/alpha", "octocat/beta"), nil
	}}
	specs, err := gh.webhookSpecs(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 {
		t.Fatalf("webhook specs = %#v", specs)
	}
	if specs[0].apiPath != "repos/octocat/alpha/hooks" || specs[1].apiPath != "repos/octocat/beta/hooks" {
		t.Fatalf("webhook specs = %#v", specs)
	}
	if !reflect.DeepEqual(specs[0].events, []string{"issues", "pull_request", "push", "ping", "issue_comment", "pull_request_review", "pull_request_review_comment"}) {
		t.Fatalf("webhook spec events = %#v", specs[0].events)
	}
}

// The board query ignores the ready filter: a repository has to be watched
// before any of its issues become ready.
func TestPersonalProjectRepositoryLookupIgnoresFilter(t *testing.T) {
	target, err := parseTarget("https://github.com/users/octocat/projects/3")
	if err != nil {
		t.Fatal(err)
	}
	var itemQuery string
	gh := GHCLI{Filter: "assignee:@me", runCommand: func(_ context.Context, args ...string) ([]byte, error) {
		for i, arg := range args {
			if strings.HasPrefix(arg, "itemQuery=") {
				itemQuery = strings.TrimPrefix(args[i], "itemQuery=")
			}
		}
		return projectItemsResponse("octocat/alpha"), nil
	}}
	if _, err := gh.webhookSpecs(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(itemQuery, "assignee") {
		t.Fatalf("project repository lookup applied the ready filter: %q", itemQuery)
	}
}

func TestWebhookSpecsReportEmptyPersonalProject(t *testing.T) {
	target, err := parseTarget("https://github.com/users/octocat/projects/3")
	if err != nil {
		t.Fatal(err)
	}
	gh := GHCLI{runCommand: func(_ context.Context, _ ...string) ([]byte, error) {
		return projectItemsResponse(), nil
	}}
	_, err = gh.webhookSpecs(context.Background(), target)
	if !errors.Is(err, errProjectWebhookUnavailable) || !strings.Contains(err.Error(), "periodic synchronization") {
		t.Fatalf("empty personal project webhook error = %v", err)
	}
}

func TestWebhookSpecsPreserveRepositoryEvents(t *testing.T) {
	target, err := parseTarget("owner/repo")
	if err != nil {
		t.Fatal(err)
	}
	specs, err := (GHCLI{}).webhookSpecs(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 || specs[0].apiPath != "repos/owner/repo/hooks" || !reflect.DeepEqual(specs[0].events, []string{"issues", "pull_request", "push", "ping", "issue_comment", "pull_request_review", "pull_request_review_comment"}) {
		t.Fatalf("webhook specs = %#v", specs)
	}
}

// fakeConfigurer records ConfigureWebhook calls and replays a scripted result
// per call, standing in for the repositories a board is backed by over time.
type fakeConfigurer struct {
	mu      sync.Mutex
	calls   []string
	results []struct {
		created []string
		err     error
	}
}

func (f *fakeConfigurer) ConfigureWebhook(_ context.Context, target, _, _ string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, target)
	if len(f.results) == 0 {
		return nil, nil
	}
	result := f.results[0]
	f.results = f.results[1:]
	return result.created, result.err
}

// A repository added to the board after startup has no webhook until the
// target is configured again, so reconciliation has to keep running and has to
// announce only the webhooks it actually created (issue #238).
func TestWebhookReconcilerConfiguresNewlyDiscoveredRepositories(t *testing.T) {
	gh := &fakeConfigurer{results: []struct {
		created []string
		err     error
	}{
		{created: []string{"octocat/alpha"}},
		{},
		{created: []string{"octocat/beta"}},
	}}
	var logs []string
	r := newWebhookReconciler(gh, []string{"https://github.com/users/octocat/projects/3"}, "https://tunnel/webhook", "", func(format string, args ...interface{}) {
		logs = append(logs, fmt.Sprintf(format, args...))
	})
	for i := 0; i < 3; i++ {
		r.reconcile(context.Background())
	}
	if len(gh.calls) != 3 {
		t.Fatalf("configure calls = %#v", gh.calls)
	}
	want := []string{"configured GitHub webhook for octocat/alpha", "configured GitHub webhook for octocat/beta"}
	if !reflect.DeepEqual(logs, want) {
		t.Fatalf("reconciler logs = %#v, want %#v", logs, want)
	}
}

// Reconciling a steady board must not create or churn webhooks, and must not
// repeat itself in the log.
func TestWebhookReconcilerIsQuietWhenNothingChanges(t *testing.T) {
	gh := &fakeConfigurer{}
	var logs []string
	r := newWebhookReconciler(gh, []string{"o/r"}, "https://tunnel/webhook", "", func(format string, args ...interface{}) {
		logs = append(logs, fmt.Sprintf(format, args...))
	})
	for i := 0; i < 3; i++ {
		r.reconcile(context.Background())
	}
	if len(logs) != 0 {
		t.Fatalf("reconciler logs = %#v, want none", logs)
	}
}

// A repository glorp cannot configure is reported once and never stops the
// daemon; the targets after it still get reconciled.
func TestWebhookReconcilerReportsFailuresWithoutStopping(t *testing.T) {
	failure := fmt.Errorf("%w (1 of 2): create webhooks for octocat/beta: HTTP 403", errWebhookPartiallyConfigured)
	gh := &fakeConfigurer{results: []struct {
		created []string
		err     error
	}{
		{created: []string{"octocat/alpha"}, err: failure},
		{},
		{err: failure},
		{},
	}}
	var logs []string
	r := newWebhookReconciler(gh, []string{"https://github.com/users/octocat/projects/3", "o/r"}, "https://tunnel/webhook", "", func(format string, args ...interface{}) {
		logs = append(logs, fmt.Sprintf(format, args...))
	})
	r.reconcile(context.Background())
	r.reconcile(context.Background())
	if len(gh.calls) != 4 {
		t.Fatalf("configure calls = %#v, want every target reconciled on both cycles", gh.calls)
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "configured GitHub webhook for octocat/alpha") || !strings.Contains(joined, "HTTP 403") {
		t.Fatalf("reconciler logs = %#v", logs)
	}
	if strings.Count(joined, "HTTP 403") != 1 {
		t.Fatalf("repeated failure logged more than once: %#v", logs)
	}
}

// A Discussions-board target needs the `discussion` event; the issue events a
// repository target subscribes to never announce a new thread (issue #226).
func TestWebhookSpecsSubscribeDiscussionEvents(t *testing.T) {
	target, err := parseTarget("https://github.com/octocat/alpha/discussions")
	if err != nil {
		t.Fatal(err)
	}
	specs, err := (GHCLI{}).webhookSpecs(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 {
		t.Fatalf("webhook specs = %#v", specs)
	}
	if specs[0].apiPath != "repos/octocat/alpha/hooks" || specs[0].name != "octocat/alpha discussions" {
		t.Fatalf("webhook spec = %#v", specs[0])
	}
	if !reflect.DeepEqual(specs[0].events, []string{"discussion", "ping"}) {
		t.Fatalf("webhook spec events = %#v", specs[0].events)
	}
}

// Watching both owner/repo and owner/repo/discussions reuses one webhook, so
// the second target has to widen the existing subscription rather than find
// its endpoint already registered and silently receive nothing.
func TestMissingWebhookEvents(t *testing.T) {
	tests := []struct {
		name             string
		existing, wanted []string
		want             []string
	}{
		{name: "adds unsubscribed events", existing: []string{"issues", "ping"}, wanted: []string{"discussion", "ping"}, want: []string{"discussion"}},
		{name: "already subscribed", existing: []string{"discussion", "ping"}, wanted: []string{"discussion", "ping"}},
		{name: "wildcard covers everything", existing: []string{"*"}, wanted: []string{"discussion", "ping"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := missingWebhookEvents(test.existing, test.wanted); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("missing = %#v, want %#v", got, test.want)
			}
		})
	}
}

// TestGHCLIAPITimesOut checks webhook configuration's `gh api` calls are
// bounded like every other `gh` call. Reconciliation runs on the poll loop, so
// one stuck on a connection that died while the machine slept used to stop
// the watch for good (issue #683).
func TestGHCLIAPITimesOut(t *testing.T) {
	binary := fakeQuotaBinary(t, "sleep 30")
	previous := ghCommandTimeout
	ghCommandTimeout = 150 * time.Millisecond
	defer func() { ghCommandTimeout = previous }()
	start := time.Now()
	_, err := GHCLI{Binary: binary}.api(context.Background(), "repos/o/r/hooks", "")
	if err == nil || !strings.Contains(err.Error(), "did not finish within") {
		t.Fatalf("error %v, want the call reported as timed out", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("call took %s, want it abandoned at the timeout", elapsed)
	}
}
