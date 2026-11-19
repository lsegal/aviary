package browser

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

// fakeIssueHydrator stands in for the REST hydrator and records every fetch,
// so the tests below can assert the request budget browser mode promises:
// O(new candidate issues), never O(list) and never O(ticks).
type fakeIssueHydrator struct {
	calls        []string
	err          error
	body         string
	dependsOn    []core.IssueDependency
	hasSubIssues bool
}

func (h *fakeIssueHydrator) HydrateIssue(_ context.Context, repo string, issue *core.Issue) error {
	h.calls = append(h.calls, fmt.Sprintf("%s#%d", repo, issue.Number))
	if h.err != nil {
		return h.err
	}
	issue.Body = h.body
	issue.DependsOn = h.dependsOn
	issue.HasSubIssues = h.hasSubIssues
	return nil
}

// hydratingTestSource builds an issue source that reads through one fake page
// and hydrates through one fake hydrator.
func hydratingTestSource(page *fakePage, hydrate IssueHydrator, handled func(core.Issue) bool) *IssueSource {
	source := newTestIssueSource(page, "", false, nil)
	source.Hydration = NewHydration(hydrate, handled)
	return source
}

// rowsResult builds a single-page extraction result for the given numbers.
func rowsResult(numbers ...int) issueList {
	list := issueList{Recognized: true}
	for _, number := range numbers {
		list.Rows = append(list.Rows, issueRow{Number: number, Repository: "lsegal/aviary", Title: fmt.Sprintf("issue %d", number), State: "open"})
	}
	return list
}

func repeatResult(list issueList, times int) []issueList {
	results := make([]issueList, 0, times)
	for i := 0; i < times; i++ {
		results = append(results, list)
	}
	return results
}

// TestIssueSourceSteadyStateTickMakesNoRequests is the budget
// assertion: once the candidates on a page have been hydrated, reloading the
// same unchanged list any number of times costs nothing at all.
func TestIssueSourceSteadyStateTickMakesNoRequests(t *testing.T) {
	const ticks = 6
	page := &fakePage{results: repeatResult(rowsResult(1, 2), ticks)}
	hydrate := &fakeIssueHydrator{body: "hydrated"}
	source := hydratingTestSource(page, hydrate, nil)
	for tick := 0; tick < ticks; tick++ {
		if _, err := source.ListIssues(context.Background(), "lsegal/aviary"); err != nil {
			t.Fatalf("tick %d: ListIssues() error = %v", tick+1, err)
		}
		if tick == 0 && len(hydrate.calls) != 2 {
			t.Fatalf("first tick hydrated %v, want one fetch per new issue", hydrate.calls)
		}
	}
	if len(hydrate.calls) != 2 {
		t.Fatalf("hydrations after %d unchanged ticks = %v, want the first tick's two and nothing more", ticks, hydrate.calls)
	}
}

// TestIssueSourceHydratesOnlyNewIssues checks the O(new issues) half of
// the budget: a list that grows by one costs exactly one fetch.
func TestIssueSourceHydratesOnlyNewIssues(t *testing.T) {
	page := &fakePage{results: []issueList{rowsResult(1, 2), rowsResult(1, 2, 3)}}
	hydrate := &fakeIssueHydrator{}
	source := hydratingTestSource(page, hydrate, nil)
	for tick := 0; tick < 2; tick++ {
		if _, err := source.ListIssues(context.Background(), "lsegal/aviary"); err != nil {
			t.Fatalf("tick %d: ListIssues() error = %v", tick+1, err)
		}
	}
	want := []string{"lsegal/aviary#1", "lsegal/aviary#2", "lsegal/aviary#3"}
	if strings.Join(hydrate.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("hydrations = %v, want %v", hydrate.calls, want)
	}
}

// TestIssueSourceSkipsHandledIssues covers the diff against the
// handled-state file and the in-flight set: work this run already owns is not
// a dispatch candidate, so it is never fetched.
func TestIssueSourceSkipsHandledIssues(t *testing.T) {
	page := &fakePage{results: repeatResult(rowsResult(1, 2), 2)}
	hydrate := &fakeIssueHydrator{}
	source := hydratingTestSource(page, hydrate, func(issue core.Issue) bool { return issue.Number == 1 })
	for tick := 0; tick < 2; tick++ {
		if _, err := source.ListIssues(context.Background(), "lsegal/aviary"); err != nil {
			t.Fatalf("tick %d: ListIssues() error = %v", tick+1, err)
		}
	}
	if len(hydrate.calls) != 1 || hydrate.calls[0] != "lsegal/aviary#2" {
		t.Fatalf("hydrations = %v, want only the unhandled issue fetched once", hydrate.calls)
	}
}

// TestIssueSourceRehydratesOnCandidateReentry checks the one case the
// memo is deliberately dropped for: an issue that stops being a candidate and
// comes back must not be dispatched from a stale body.
func TestIssueSourceRehydratesOnCandidateReentry(t *testing.T) {
	page := &fakePage{results: repeatResult(rowsResult(1), 3)}
	hydrate := &fakeIssueHydrator{}
	tick := 0
	source := hydratingTestSource(page, hydrate, func(core.Issue) bool { return tick == 2 })
	for tick = 1; tick <= 3; tick++ {
		if _, err := source.ListIssues(context.Background(), "lsegal/aviary"); err != nil {
			t.Fatalf("tick %d: ListIssues() error = %v", tick, err)
		}
	}
	if len(hydrate.calls) != 2 {
		t.Fatalf("hydrations = %v, want the issue fetched again after it re-entered the candidate set", hydrate.calls)
	}
}

// TestIssueSourceAppliesHydratedFields checks the hydrated metadata
// reaches the dispatch path, both on the fetching tick and on later cached
// ones.
func TestIssueSourceAppliesHydratedFields(t *testing.T) {
	page := &fakePage{results: repeatResult(rowsResult(7), 2)}
	hydrate := &fakeIssueHydrator{body: "Depends on #4", dependsOn: []core.IssueDependency{{Number: 4, State: "open"}}, hasSubIssues: true}
	source := hydratingTestSource(page, hydrate, nil)
	for tick := 1; tick <= 2; tick++ {
		issues, err := source.ListIssues(context.Background(), "lsegal/aviary")
		if err != nil {
			t.Fatalf("tick %d: ListIssues() error = %v", tick, err)
		}
		if len(issues) != 1 {
			t.Fatalf("tick %d: issues = %#v", tick, issues)
		}
		if issues[0].Body != "Depends on #4" || issues[0].HasSubIssues != true || len(issues[0].DependsOn) != 1 {
			t.Fatalf("tick %d: hydrated issue = %#v", tick, issues[0])
		}
		if blocked, _ := core.IssueBlocked(issues[0]); !blocked {
			t.Fatalf("tick %d: hydrated issue reported unblocked", tick)
		}
	}
	if len(hydrate.calls) != 1 {
		t.Fatalf("hydrations = %v, want the cached tick to fetch nothing", hydrate.calls)
	}
}

// TestIssueSourceReportsHydrationFailure keeps a failed fetch visible
// rather than dispatching an issue with no body.
func TestIssueSourceReportsHydrationFailure(t *testing.T) {
	page := &fakePage{results: repeatResult(rowsResult(1), 1)}
	hydrate := &fakeIssueHydrator{err: errors.New("boom")}
	source := hydratingTestSource(page, hydrate, nil)
	if _, err := source.ListIssues(context.Background(), "lsegal/aviary"); err == nil {
		t.Fatal("ListIssues() error = nil, want the hydration failure")
	}
}

// boardTestItem is one row of a generated board document, so a test can model
// a board changing between ticks instead of needing a fixture per state.
type boardTestItem struct {
	Number int
	Repo   string
	Title  string
	Status string
}

// boardDocument renders the table layout the board extractor reads, with one
// row per item.
func boardDocument(items ...boardTestItem) string {
	var rows strings.Builder
	for i, item := range items {
		fmt.Fprintf(&rows, `
        <div role="row" data-testid="TableRow" data-row-id="PVTI_%d">
          <div role="gridcell" data-testid="TableCell-Title">
            <a href="/%s/issues/%d">%s</a>
          </div>
          <div role="gridcell" data-testid="TableCell-Status">
            <span class="Truncate-text">%s</span>
          </div>
        </div>`, i+1, item.Repo, item.Number, item.Title, item.Status)
	}
	return `<!DOCTYPE html><html lang="en"><body><div data-testid="memex-app">
      <div role="grid" aria-label="Board">
        <div role="row" data-testid="TableHeader">
          <div role="columnheader" data-testid="TableHeader-Title">Title</div>
          <div role="columnheader" data-testid="TableHeader-Status">Status</div>
        </div>` + rows.String() + `
      </div></div></body></html>`
}

// hydratingTestBoard builds a board reader that reads through one fake page and
// hydrates through one fake hydrator.
func hydratingTestBoard(page *fakeBoardPage, hydrate IssueHydrator, handled func(core.Issue) bool) *Board {
	board, _ := newTestBoard(page)
	board.hydration = NewHydration(hydrate, handled)
	return board
}

const boardTestTarget = "https://github.com/users/lsegal/projects/3"

var twoBoardItems = []boardTestItem{
	{Number: 1, Repo: "lsegal/aviary", Title: "first", Status: "Todo"},
	{Number: 2, Repo: "lsegal/zvidlib", Title: "second", Status: "Todo"},
}

// TestBoardSteadyStateTickMakesNoRequests is the budget assertion for
// the board reader, matching the one the issues page keeps: once the items on
// a board have been hydrated, re-reading the same unchanged board any number
// of times costs nothing at all.
func TestBoardSteadyStateTickMakesNoRequests(t *testing.T) {
	const ticks = 6
	page := &fakeBoardPage{documents: []string{boardDocument(twoBoardItems...)}}
	hydrate := &fakeIssueHydrator{body: "hydrated"}
	board := hydratingTestBoard(page, hydrate, nil)
	for tick := 0; tick < ticks; tick++ {
		issues := boardIssues(t, board, boardTestTarget)
		if len(issues) != 2 || issues[0].Body != "hydrated" {
			t.Fatalf("tick %d: issues = %+v, want both items hydrated", tick+1, issues)
		}
		if tick == 0 && len(hydrate.calls) != 2 {
			t.Fatalf("first tick hydrated %v, want one fetch per board item", hydrate.calls)
		}
	}
	if len(hydrate.calls) != 2 {
		t.Fatalf("hydrations after %d unchanged ticks = %v, want the first tick's two and nothing more", ticks, hydrate.calls)
	}
}

// TestBoardHydratesOnlyNewItems covers the O(new items) half of the
// budget and the field the board owns: a board that grows by one costs exactly
// one more fetch, and hydration never overwrites an item's Status column.
func TestBoardHydratesOnlyNewItems(t *testing.T) {
	grown := append(append([]boardTestItem{}, twoBoardItems...), boardTestItem{Number: 3, Repo: "lsegal/aviary", Title: "third", Status: "In Progress"})
	page := &fakeBoardPage{documents: []string{boardDocument(twoBoardItems...), boardDocument(grown...)}}
	hydrate := &fakeIssueHydrator{body: "hydrated"}
	board := hydratingTestBoard(page, hydrate, nil)
	var issues []core.Issue
	for tick := 0; tick < 2; tick++ {
		issues = boardIssues(t, board, boardTestTarget)
	}
	want := []string{"lsegal/aviary#1", "lsegal/zvidlib#2", "lsegal/aviary#3"}
	if strings.Join(hydrate.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("hydrations = %v, want %v", hydrate.calls, want)
	}
	if len(issues) != 3 {
		t.Fatalf("second tick issues = %+v, want three", issues)
	}
	for i, issue := range issues {
		if issue.ProjectStatus != grown[i].Status {
			t.Errorf("issue #%d ProjectStatus = %q, want %q (hydration must not overwrite the board's own column)", issue.Number, issue.ProjectStatus, grown[i].Status)
		}
		if issue.Body != "hydrated" {
			t.Errorf("issue #%d body = %q, want the hydrated body", issue.Number, issue.Body)
		}
	}
}

// TestBoardSkipsHandledItems keeps the board on the same candidate rule
// the issues page uses: work this run already owns is never fetched.
func TestBoardSkipsHandledItems(t *testing.T) {
	page := &fakeBoardPage{documents: []string{boardDocument(twoBoardItems...)}}
	hydrate := &fakeIssueHydrator{}
	board := hydratingTestBoard(page, hydrate, func(issue core.Issue) bool { return issue.Number == 1 })
	for tick := 0; tick < 2; tick++ {
		boardIssues(t, board, boardTestTarget)
	}
	if len(hydrate.calls) != 1 || hydrate.calls[0] != "lsegal/zvidlib#2" {
		t.Fatalf("hydrations = %v, want only the unhandled item fetched once", hydrate.calls)
	}
}

// TestBoardItemsReportBlocked is the behaviour the whole change exists
// for: a board item with an open dependency, or with open sub-issues, must
// reach the dispatch gate as blocked instead of arriving with an empty body.
func TestBoardItemsReportBlocked(t *testing.T) {
	for _, test := range []struct {
		name    string
		hydrate *fakeIssueHydrator
		want    string
	}{
		{
			name:    "open dependency",
			hydrate: &fakeIssueHydrator{body: "Depends on #4", dependsOn: []core.IssueDependency{{Number: 4, State: "open"}}},
			want:    "depends on #4 (open)",
		},
		{
			name:    "open sub-issues",
			hydrate: &fakeIssueHydrator{body: "parent", hasSubIssues: true},
			want:    "has sub-issues",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			page := &fakeBoardPage{documents: []string{boardDocument(twoBoardItems[:1]...)}}
			board := hydratingTestBoard(page, test.hydrate, nil)
			issues := boardIssues(t, board, boardTestTarget)
			if len(issues) != 1 {
				t.Fatalf("issues = %+v, want one", issues)
			}
			blocked, reason := core.IssueBlocked(issues[0])
			if !blocked || reason != test.want {
				t.Fatalf("core.IssueBlocked() = %v, %q, want true, %q", blocked, reason, test.want)
			}
		})
	}
}

// TestBoardHydrationFailureIsReported keeps a failed fetch from
// silently dispatching a board item with no body.
func TestBoardHydrationFailureIsReported(t *testing.T) {
	page := &fakeBoardPage{documents: []string{boardDocument(twoBoardItems...)}}
	board := hydratingTestBoard(page, &fakeIssueHydrator{err: errors.New("boom")}, nil)
	if _, err := board.ListIssues(context.Background(), boardTestTarget); err == nil {
		t.Fatal("ListIssues() error = nil, want the hydration failure")
	}
}

// TestIssueSourceRefreshesBlockedIssues checks that a blocked issue's
// dependency state is read again once it is due, so a paused issue sees its
// last blocker close (issue #762), while an unblocked one stays cached.
func TestIssueSourceRefreshesBlockedIssues(t *testing.T) {
	page := &fakePage{results: repeatResult(rowsResult(5), 3)}
	hydrate := &fakeIssueHydrator{dependsOn: []core.IssueDependency{{Number: 2, State: "open"}}}
	source := hydratingTestSource(page, hydrate, nil)
	now := time.Now()
	source.now = func() time.Time { return now }
	list := func(tick int) core.Issue {
		t.Helper()
		issues, err := source.ListIssues(context.Background(), "lsegal/aviary")
		if err != nil || len(issues) != 1 {
			t.Fatalf("tick %d: ListIssues() = %#v, %v", tick, issues, err)
		}
		return issues[0]
	}
	list(1)
	// The blocker closes on GitHub; nothing on the page changes.
	hydrate.dependsOn = []core.IssueDependency{{Number: 2, State: "closed"}}
	now = now.Add(blockedRefreshInterval)
	if blocked, reason := core.IssueBlocked(list(2)); blocked {
		t.Fatalf("issue still blocked (%s) after its blocker closed, hydrations = %v", reason, hydrate.calls)
	}
	now = now.Add(blockedRefreshInterval)
	list(3)
	if len(hydrate.calls) != 2 {
		t.Fatalf("hydrations = %v, want a refresh while blocked and none once unblocked", hydrate.calls)
	}
}
