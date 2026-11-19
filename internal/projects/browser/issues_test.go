package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

// fakePage stands in for a tab: it records what it was asked to load and
// answers each evaluation with the next canned extraction result, so the issue
// source can be exercised without a browser and without a network.
type fakePage struct {
	results   []issueList
	evalErr   error
	navigated []string
	reloads   int
	status    int
	evals     int
	// signIn is the answer the sign-in probe gets. The zero value reports a
	// page that is neither signed in nor signed out, which is what every test
	// that predates the probe wants: the diagnosis stays out of the way.
	signIn      signInState
	signInEvals int
}

func (p *fakePage) Navigate(url string) error {
	p.navigated = append(p.navigated, url)
	return nil
}

func (p *fakePage) Reload() error {
	p.reloads++
	return nil
}

func (p *fakePage) HTTPStatus() int { return p.status }

func (p *fakePage) Eval(_ string, out any) error {
	if p.evalErr != nil {
		return p.evalErr
	}
	// The sign-in probe is a separate evaluation with its own result type, and
	// it is deliberately not counted as a page read: the page counters below
	// measure how many pages of the list were walked.
	if state, ok := out.(*signInState); ok {
		p.signInEvals++
		*state = p.signIn
		return nil
	}
	if p.evals >= len(p.results) {
		return fmt.Errorf("unexpected evaluation %d", p.evals+1)
	}
	result := p.results[p.evals]
	p.evals++
	// Round-tripping through JSON mirrors how chromedp decodes the script's
	// value, so a field the script names differently than Go does would fail
	// here rather than silently arriving zeroed.
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, out.(*issueList))
}

// newTestIssueSource builds a source reading every target through one fake page.
func newTestIssueSource(page *fakePage, filter string, allIssues bool, logf func(string, ...interface{})) *IssueSource {
	if page.status == 0 {
		page.status = 200
	}
	return &IssueSource{
		pageFor: func(string) (Page, error) { return page, nil },
		// One attempt per page load keeps the canned results below matching
		// evaluations one for one; the settle wait has its own tests.
		settleAttempts: 1,
		filter:         filter,
		allIssues:      allIssues,
		logf:           logf,
		Hydration:      NewHydration(nil, nil),
		reported:       map[string]bool{},
		lastURL:        map[string]string{},
		lastRows:       map[string]string{},
	}
}

func TestIssuesURL(t *testing.T) {
	for _, test := range []struct {
		name      string
		filter    string
		allIssues bool
		want      string
	}{
		{
			name:   "default filter",
			filter: core.DefaultIssueFilter,
			want:   "https://github.com/lsegal/aviary/issues?q=is%3Aissue+state%3Aopen+assignee%3A%40me+author%3A%40me",
		},
		{
			name:   "custom filter",
			filter: "label:ready",
			want:   "https://github.com/lsegal/aviary/issues?q=is%3Aissue+state%3Aopen+label%3Aready",
		},
		{
			name:      "all issues drops the filter",
			filter:    core.DefaultIssueFilter,
			allIssues: true,
			want:      "https://github.com/lsegal/aviary/issues?q=is%3Aissue+state%3Aopen",
		},
		{
			name:   "empty filter",
			filter: "",
			want:   "https://github.com/lsegal/aviary/issues?q=is%3Aissue+state%3Aopen",
		},
		{
			name:   "filter naming its own kind cannot ask for pull requests",
			filter: "is:pr assignee:@me",
			want:   "https://github.com/lsegal/aviary/issues?q=is%3Aissue+state%3Aopen+assignee%3A%40me",
		},
		{
			name:   "filter naming its own state cannot ask for closed issues",
			filter: "state:closed label:ready",
			want:   "https://github.com/lsegal/aviary/issues?q=is%3Aissue+state%3Aopen+label%3Aready",
		},
		{
			name:   "filter repeating the qualifiers does not double them",
			filter: "is:issue state:open label:ready",
			want:   "https://github.com/lsegal/aviary/issues?q=is%3Aissue+state%3Aopen+label%3Aready",
		},
		{
			name:      "all issues drops a filter that named its own kind",
			filter:    "is:pr label:ready",
			allIssues: true,
			want:      "https://github.com/lsegal/aviary/issues?q=is%3Aissue+state%3Aopen",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := issuesURL("lsegal/aviary", test.filter, test.allIssues); got != test.want {
				t.Fatalf("issuesURL = %s, want %s", got, test.want)
			}
		})
	}
}

// TestIssueSourceListIssues covers the rows-to-issues mapping the
// dispatch path downstream consumes.
func TestIssueSourceListIssues(t *testing.T) {
	page := &fakePage{results: []issueList{{
		Recognized: true,
		Rows: []issueRow{
			{Number: 12, Repository: "lsegal/aviary", Title: " Fix the parser ", State: "open", Labels: []string{"bug", " ", "ready"}},
			{Number: 13, Title: "No repository on the row", State: ""},
			{Number: 0, Title: "Not an issue row"},
		},
	}}}
	source := newTestIssueSource(page, core.DefaultIssueFilter, false, nil)
	issues, err := source.ListIssues(context.Background(), "lsegal/aviary")
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("got %d issues, want 2: %+v", len(issues), issues)
	}
	if issues[0].Number != 12 || issues[0].Title != "Fix the parser" || issues[0].Repository != "lsegal/aviary" || issues[0].State != "open" {
		t.Fatalf("first issue %+v", issues[0])
	}
	if len(issues[0].Labels) != 2 || issues[0].Labels[0].Name != "bug" || issues[0].Labels[1].Name != "ready" {
		t.Fatalf("labels %+v, want bug and ready with the blank dropped", issues[0].Labels)
	}
	if issues[1].Repository != "lsegal/aviary" {
		t.Fatalf("repository %q, want the watched repository", issues[1].Repository)
	}
	if issues[1].State != "open" {
		t.Fatalf("state %q, want open by default", issues[1].State)
	}
}

// TestIssueSourceEmptyList checks a page that genuinely has no results
// is not mistaken for a page that could not be read.
func TestIssueSourceEmptyList(t *testing.T) {
	page := &fakePage{results: []issueList{{Recognized: true, Empty: true}}}
	source := newTestIssueSource(page, core.DefaultIssueFilter, false, nil)
	issues, err := source.ListIssues(context.Background(), "lsegal/aviary")
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("got %d issues, want none", len(issues))
	}
}

// TestIssueSourceEmptyListWithoutAMarker checks a list container that
// drew no rows is read as an empty list once the render wait is over, rather
// than as an extraction failure reported on every poll (issue #413).
func TestIssueSourceEmptyListWithoutAMarker(t *testing.T) {
	page := &fakePage{results: []issueList{{Container: true}, {Container: true}, {Container: true}}}
	var logged []string
	source := newTestIssueSource(page, core.DefaultIssueFilter, false, func(format string, args ...interface{}) {
		logged = append(logged, fmt.Sprintf(format, args...))
	})
	issues, err := source.ListIssues(context.Background(), "lsegal/aviary")
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("got %d issues, want none", len(issues))
	}
	for _, line := range logged {
		if strings.Contains(line, "could not read") {
			t.Fatalf("an empty list was reported as a failure: %v", logged)
		}
	}
}

// TestIssueSourceExtractionFailure checks an unreadable page produces the
// distinguishable error, and that it is logged once rather than on every tick.
func TestIssueSourceExtractionFailure(t *testing.T) {
	page := &fakePage{results: []issueList{{}, {}, {}}}
	var logged []string
	source := newTestIssueSource(page, core.DefaultIssueFilter, false, func(format string, args ...interface{}) {
		logged = append(logged, fmt.Sprintf(format, args...))
	})
	for tick := 0; tick < 3; tick++ {
		_, err := source.ListIssues(context.Background(), "lsegal/aviary")
		if !errors.Is(err, ErrExtraction) {
			t.Fatalf("tick %d error %v, want ErrExtraction", tick, err)
		}
		var extraction *ExtractionError
		if !errors.As(err, &extraction) {
			t.Fatalf("tick %d error %v does not carry the failing URL", tick, err)
		}
		if !strings.HasPrefix(extraction.URL, "https://github.com/lsegal/aviary/issues?") {
			t.Fatalf("error URL %q", extraction.URL)
		}
	}
	if len(logged) != 1 {
		t.Fatalf("logged %d line(s) over three ticks, want 1: %v", len(logged), logged)
	}
	if !strings.Contains(logged[0], "https://github.com/lsegal/aviary/issues?") {
		t.Fatalf("log line %q does not name the URL", logged[0])
	}
}

// TestIssueSourceFollowsPagerToTheCap checks pagination stops at the cap
// and does not return the same issue twice.
func TestIssueSourceFollowsPagerToTheCap(t *testing.T) {
	pageResult := func(number int, next string) issueList {
		return issueList{
			Recognized: true,
			Rows:       []issueRow{{Number: number, Repository: "lsegal/aviary", Title: "Issue", State: "open"}},
			Next:       next,
		}
	}
	page := &fakePage{results: []issueList{
		pageResult(1, "https://github.com/lsegal/aviary/issues?page=2"),
		pageResult(2, "https://github.com/lsegal/aviary/issues?page=3"),
		pageResult(1, "https://github.com/lsegal/aviary/issues?page=4"),
	}}
	source := newTestIssueSource(page, core.DefaultIssueFilter, false, nil)
	issues, err := source.ListIssues(context.Background(), "lsegal/aviary")
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if page.evals != issuesPageLimit {
		t.Fatalf("read %d pages, want the cap of %d", page.evals, issuesPageLimit)
	}
	if len(issues) != 2 {
		t.Fatalf("got %d issues, want 2 after deduplication: %+v", len(issues), issues)
	}
}

func TestIssueSourceStopsAtTheLastPage(t *testing.T) {
	page := &fakePage{results: []issueList{{
		Recognized: true,
		Rows:       []issueRow{{Number: 1, Repository: "lsegal/aviary", Title: "Issue"}},
	}}}
	source := newTestIssueSource(page, core.DefaultIssueFilter, false, nil)
	if _, err := source.ListIssues(context.Background(), "lsegal/aviary"); err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if page.evals != 1 {
		t.Fatalf("read %d pages, want 1 when the page has no pager", page.evals)
	}
}

// TestIssueSourceReloadsUnchangedTarget checks a repeat tick reloads the
// tab it already has open rather than navigating it somewhere it already is.
func TestIssueSourceReloadsUnchangedTarget(t *testing.T) {
	page := &fakePage{results: []issueList{
		{Recognized: true, Empty: true},
		{Recognized: true, Empty: true},
	}}
	source := newTestIssueSource(page, core.DefaultIssueFilter, false, nil)
	for tick := 0; tick < 2; tick++ {
		if _, err := source.ListIssues(context.Background(), "lsegal/aviary"); err != nil {
			t.Fatalf("tick %d: %v", tick, err)
		}
	}
	if len(page.navigated) != 1 {
		t.Fatalf("navigated %d time(s), want 1: %v", len(page.navigated), page.navigated)
	}
	if page.reloads != 1 {
		t.Fatalf("reloaded %d time(s), want 1", page.reloads)
	}
}

// TestIssueSourceLogsOnlyOnChange checks the five-second loop stays quiet
// while nothing about the list changes.
func TestIssueSourceLogsOnlyOnChange(t *testing.T) {
	rows := func(titles ...string) issueList {
		list := issueList{Recognized: true}
		for i, title := range titles {
			list.Rows = append(list.Rows, issueRow{Number: i + 1, Repository: "lsegal/aviary", Title: title, State: "open"})
		}
		return list
	}
	page := &fakePage{results: []issueList{
		rows("One"),
		rows("One"),
		rows("One", "Two"),
		rows("One", "Two"),
	}}
	var logged []string
	source := newTestIssueSource(page, core.DefaultIssueFilter, false, func(format string, args ...interface{}) {
		logged = append(logged, fmt.Sprintf(format, args...))
	})
	for tick := 0; tick < 4; tick++ {
		if _, err := source.ListIssues(context.Background(), "lsegal/aviary"); err != nil {
			t.Fatalf("tick %d: %v", tick, err)
		}
	}
	if len(logged) != 2 {
		t.Fatalf("logged %d line(s) over four ticks with one change, want 2: %v", len(logged), logged)
	}
}

func TestIssueSourceRejectsHTTPErrors(t *testing.T) {
	page := &fakePage{status: 404, results: []issueList{{Recognized: true}}}
	source := newTestIssueSource(page, core.DefaultIssueFilter, false, nil)
	_, err := source.ListIssues(context.Background(), "lsegal/aviary")
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("error %v, want an HTTP 404 failure", err)
	}
	if errors.Is(err, ErrExtraction) {
		t.Fatalf("a 404 must not be reported as an extraction failure")
	}
}

func TestIssueSourceRejectsNonRepositoryTargets(t *testing.T) {
	for _, target := range []string{
		"https://github.com/orgs/lsegal/projects/1",
		"https://github.com/lsegal/aviary/discussions",
	} {
		page := &fakePage{}
		source := newTestIssueSource(page, core.DefaultIssueFilter, false, nil)
		if _, err := source.ListIssues(context.Background(), target); err == nil {
			t.Fatalf("target %s was accepted, want an OWNER/REPO-only error", target)
		}
	}
}

// TestNextIssuesURL checks a mis-read pager cannot send the tab off
// GitHub or off the issues list.
func TestNextIssuesURL(t *testing.T) {
	for _, test := range []struct {
		candidate string
		want      string
	}{
		{candidate: "https://github.com/lsegal/aviary/issues?page=2", want: "https://github.com/lsegal/aviary/issues?page=2"},
		{candidate: "https://github.com/lsegal/aviary/issues", want: "https://github.com/lsegal/aviary/issues"},
		{candidate: "https://example.com/lsegal/aviary/issues", want: ""},
		{candidate: "http://github.com/lsegal/aviary/issues", want: ""},
		{candidate: "https://github.com/lsegal/aviary/pulls", want: ""},
		{candidate: "", want: ""},
	} {
		if got := nextIssuesURL(test.candidate); got != test.want {
			t.Fatalf("nextIssuesURL(%q) = %q, want %q", test.candidate, got, test.want)
		}
	}
}

// TestIssueSourceImplementsIssueSource keeps the source usable as the
// watch loop's issue source without any structural change to Aviary.
func TestIssueSourceImplementsIssueSource(t *testing.T) {
	var _ core.IssueSource = (*IssueSource)(nil)
	var _ Page = (*Tab)(nil)
}

// TestIssueSourceWaitsForClientRender checks the extractor is retried
// while GitHub's React issues page has not drawn yet, so a poll that lands
// before hydration reports the issues the page went on to render instead of an
// extraction failure (issue #415).
func TestIssueSourceWaitsForClientRender(t *testing.T) {
	page := &fakePage{results: []issueList{
		{},
		{},
		{Recognized: true, Rows: []issueRow{{Number: 415, Repository: "lsegal/aviary", Title: "bug", State: "open"}}},
	}}
	source := newTestIssueSource(page, core.DefaultIssueFilter, false, nil)
	source.settleAttempts = 5
	waits := 0
	source.sleep = func(context.Context, time.Duration) bool { waits++; return true }
	issues, err := source.ListIssues(context.Background(), "lsegal/aviary")
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 1 || issues[0].Number != 415 {
		t.Fatalf("got %+v, want the one issue the page rendered", issues)
	}
	if waits != 2 {
		t.Fatalf("waited %d time(s) for the page to render, want 2", waits)
	}
}

// TestIssueSourceRendersImmediatelyWithoutWaiting checks a page that has
// already drawn costs a single evaluation and no wait at all, so the settle
// budget is only ever spent by a page that has not rendered.
func TestIssueSourceRendersImmediatelyWithoutWaiting(t *testing.T) {
	for _, test := range []struct {
		name string
		list issueList
	}{
		{name: "list with rows", list: issueList{Recognized: true, Rows: []issueRow{{Number: 1, Repository: "lsegal/aviary", Title: "one", State: "open"}}}},
		{name: "honestly empty list", list: issueList{Recognized: true, Empty: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			page := &fakePage{results: []issueList{test.list}}
			source := newTestIssueSource(page, core.DefaultIssueFilter, false, nil)
			source.settleAttempts = 5
			waits := 0
			source.sleep = func(context.Context, time.Duration) bool { waits++; return true }
			if _, err := source.ListIssues(context.Background(), "lsegal/aviary"); err != nil {
				t.Fatalf("ListIssues: %v", err)
			}
			if page.evals != 1 || waits != 0 {
				t.Fatalf("evaluated %d time(s) with %d wait(s), want 1 and 0", page.evals, waits)
			}
		})
	}
}

// TestIssueSourceReportsPageThatNeverRenders checks the settle wait is
// bounded: a page that never draws is still reported once as the
// distinguishable extraction failure the vision fallback keys on.
func TestIssueSourceReportsPageThatNeverRenders(t *testing.T) {
	page := &fakePage{results: []issueList{{}, {}, {}}}
	source := newTestIssueSource(page, core.DefaultIssueFilter, false, nil)
	source.settleAttempts = 3
	source.sleep = func(context.Context, time.Duration) bool { return true }
	_, err := source.ListIssues(context.Background(), "lsegal/aviary")
	if !errors.Is(err, ErrExtraction) {
		t.Fatalf("error %v, want an extraction failure", err)
	}
	if page.evals != 3 {
		t.Fatalf("evaluated %d time(s), want the 3 attempts the budget allows", page.evals)
	}
}

// TestIssueSourceReportsWhatItSaw checks the failure a page that never
// renders produces says the list did not render, rather than blaming markup
// Aviary could not recognise: the page Aviary actually met was a shell GitHub had
// not finished drawing, and reporting it as a markup change sent the user
// looking for a break that was not there (issue #427).
func TestIssueSourceReportsWhatItSaw(t *testing.T) {
	page := &fakePage{results: []issueList{{}, {}, {}}}
	source := newTestIssueSource(page, core.DefaultIssueFilter, false, nil)
	source.settleAttempts = 3
	source.sleep = func(context.Context, time.Duration) bool { return true }
	_, err := source.ListIssues(context.Background(), "lsegal/aviary")
	if !errors.Is(err, ErrExtraction) {
		t.Fatalf("error %v, want an extraction failure", err)
	}
	if got := err.Error(); !strings.Contains(got, "did not render") || strings.Contains(got, "no empty-list marker") {
		t.Fatalf("error %q, want it to report an unrendered list", got)
	}
}

// TestIssuesSettleBudget keeps the render wait long enough for GitHub's
// own client render, which routinely takes longer than the five seconds the
// budget used to allow (issue #427). The budget is only ever spent by a page
// that has not drawn, so a generous one costs a rendered page nothing.
func TestIssuesSettleBudget(t *testing.T) {
	if budget := time.Duration(defaultIssuesSettleAttempts) * defaultIssuesSettleDelay; budget < 15*time.Second {
		t.Fatalf("settle budget %s, want at least 15s", budget)
	}
}

// TestIssueSourceStopsWaitingWhenCancelled checks a run being shut down
// mid-wait stops instead of sitting out the whole settle budget.
func TestIssueSourceStopsWaitingWhenCancelled(t *testing.T) {
	page := &fakePage{results: []issueList{{}, {}, {}}}
	source := newTestIssueSource(page, core.DefaultIssueFilter, false, nil)
	source.settleAttempts = 3
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source.sleep = func(context.Context, time.Duration) bool { cancel(); return false }
	if _, err := source.ListIssues(ctx, "lsegal/aviary"); !errors.Is(err, context.Canceled) {
		t.Fatalf("error %v, want context.Canceled", err)
	}
	if page.evals != 1 {
		t.Fatalf("evaluated %d time(s) after cancellation, want 1", page.evals)
	}
}
