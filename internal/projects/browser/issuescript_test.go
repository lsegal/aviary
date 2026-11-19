package browser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fixtureTab serves the captured issues-page fixtures over loopback and
// opens a tab on them, so the extractor script runs against real GitHub markup
// in a real browser without touching the network or the user's own profile. The
// test is skipped when no Chromium-based browser can be launched.
func fixtureTab(t *testing.T) (*Tab, string) {
	t.Helper()
	if _, err := findBinary(""); err != nil {
		t.Skipf("no Chromium-based browser available: %v", err)
	}
	server := httptest.NewServer(http.FileServer(http.Dir("testdata")))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	browser, err := Start(ctx, Config{Profile: t.TempDir()})
	if err != nil {
		t.Skipf("could not launch a browser: %v", err)
	}
	t.Cleanup(func() { _ = browser.Close() })
	tab, err := browser.Tab("fixtures")
	if err != nil {
		t.Fatalf("open tab: %v", err)
	}
	return tab, server.URL
}

// extractFixture loads one fixture and returns what the extractor script made
// of it.
func extractFixture(t *testing.T, tab *Tab, baseURL, fixture string) issueList {
	t.Helper()
	if err := tab.Navigate(baseURL + "/" + fixture); err != nil {
		t.Fatalf("navigate to %s: %v", fixture, err)
	}
	var list issueList
	if err := tab.Eval(issueRowsScript, &list); err != nil {
		t.Fatalf("evaluate extractor on %s: %v", fixture, err)
	}
	return list
}

// TestExtractorScriptAgainstFixtures runs the extractor against captured GitHub
// issues-page HTML: a signed-in list with labels, an empty list, a closed row,
// and a page that is not an issue list at all.
func TestExtractorScriptAgainstFixtures(t *testing.T) {
	tab, baseURL := fixtureTab(t)

	t.Run("signed-in list", func(t *testing.T) {
		list := extractFixture(t, tab, baseURL, "github-issues-list.html")
		if !list.Recognized || list.Empty {
			t.Fatalf("recognized=%v empty=%v, want a recognized non-empty list", list.Recognized, list.Empty)
		}
		wantNumbers := []int{1704, 1701, 1699, 1697, 1683, 1677, 1644}
		if len(list.Rows) != len(wantNumbers) {
			t.Fatalf("got %d rows, want %d: %+v", len(list.Rows), len(wantNumbers), list.Rows)
		}
		for i, row := range list.Rows {
			if row.Number != wantNumbers[i] {
				t.Fatalf("row %d is #%d, want #%d", i, row.Number, wantNumbers[i])
			}
			if row.Repository != "lsegal/yard" {
				t.Fatalf("row #%d repository %q", row.Number, row.Repository)
			}
			if row.State != "open" {
				t.Fatalf("row #%d state %q, want open", row.Number, row.State)
			}
			if row.Title == "" {
				t.Fatalf("row #%d has no title", row.Number)
			}
		}
		if want := "Sidebar navigation hidden on small screens with no way to open it"; list.Rows[0].Title != want {
			t.Fatalf("first title %q, want %q", list.Rows[0].Title, want)
		}
		labels := map[int][]string{}
		for _, row := range list.Rows {
			labels[row.Number] = row.Labels
		}
		for number, want := range map[int]string{1697: "bug", 1683: "enhancement", 1677: "enhancement"} {
			got := labels[number]
			if len(got) != 1 || got[0] != want {
				t.Fatalf("row #%d labels %v, want [%s]", number, got, want)
			}
		}
		if got := labels[1704]; len(got) != 0 {
			t.Fatalf("row #1704 labels %v, want none", got)
		}
		if want := "https://github.com/lsegal/yard/issues?page=2&q=is%3Aissue+state%3Aopen"; list.Next != want {
			t.Fatalf("next %q, want %q", list.Next, want)
		}
		if got := nextIssuesURL(list.Next); got != list.Next {
			t.Fatalf("nextIssuesURL rejected the pager's own target %q", list.Next)
		}
	})

	t.Run("empty list", func(t *testing.T) {
		list := extractFixture(t, tab, baseURL, "github-issues-empty.html")
		if !list.Recognized || !list.Empty {
			t.Fatalf("recognized=%v empty=%v, want a recognized empty list", list.Recognized, list.Empty)
		}
		if len(list.Rows) != 0 {
			t.Fatalf("got %d rows, want none: %+v", len(list.Rows), list.Rows)
		}
		if list.Next != "" {
			t.Fatalf("next %q, want none", list.Next)
		}
	})

	// An issues page whose list drew no rows reports the list it named, so the
	// caller can tell an empty list from markup it could not read once its
	// render wait is over, even when the blankslate beside the list carries
	// none of the markers the script knows (issue #413).
	t.Run("empty list with an unrecognized blankslate", func(t *testing.T) {
		list := extractFixture(t, tab, baseURL, "github-issues-empty-unmarked.html")
		if !list.Container || list.Recognized {
			t.Fatalf("container=%v recognized=%v, want an unrecognized page that named its list", list.Container, list.Recognized)
		}
		if len(list.Rows) != 0 {
			t.Fatalf("got %d rows, want none: %+v", len(list.Rows), list.Rows)
		}
	})

	// GitHub's page shell carries one hidden "Uh oh! There was an error"
	// blankslate per lazily loaded fragment on every repository page, so a
	// document-wide search for an empty-state marker answered yes on a page
	// whose issue list had not been drawn yet: the extractor called an
	// unrendered page a recognised empty list on the first evaluation and the
	// render wait was never spent (issue #427).
	t.Run("shell whose list has not rendered", func(t *testing.T) {
		list := extractFixture(t, tab, baseURL, "github-issues-shell.html")
		if list.Recognized || list.Empty || list.Container {
			t.Fatalf("recognized=%v empty=%v container=%v, want an unrendered page recognised as nothing", list.Recognized, list.Empty, list.Container)
		}
		if len(list.Rows) != 0 {
			t.Fatalf("got %d rows from an unrendered page: %+v", len(list.Rows), list.Rows)
		}
	})

	// The same shell with the list drawn: GitHub renders its empty state
	// inside the list rather than beside it, and that one is on the page
	// rather than hidden, so it is read as the empty list it is (issue #427).
	t.Run("empty list rendered inside the shell", func(t *testing.T) {
		list := extractFixture(t, tab, baseURL, "github-issues-empty-inline.html")
		if !list.Recognized || !list.Empty || !list.Container {
			t.Fatalf("recognized=%v empty=%v container=%v, want a recognised empty list", list.Recognized, list.Empty, list.Container)
		}
		if len(list.Rows) != 0 {
			t.Fatalf("got %d rows, want none: %+v", len(list.Rows), list.Rows)
		}
	})

	t.Run("closed row", func(t *testing.T) {
		list := extractFixture(t, tab, baseURL, "github-issues-closed.html")
		if len(list.Rows) != 1 {
			t.Fatalf("got %d rows, want 1: %+v", len(list.Rows), list.Rows)
		}
		if list.Rows[0].State != "closed" {
			t.Fatalf("state %q, want closed", list.Rows[0].State)
		}
	})

	t.Run("not an issue list", func(t *testing.T) {
		list := extractFixture(t, tab, baseURL, "github-issues-signed-out.html")
		if list.Recognized {
			t.Fatalf("a sign-in page was recognized as an issue list: %+v", list)
		}
		if len(list.Rows) != 0 {
			t.Fatalf("got %d rows from a sign-in page: %+v", len(list.Rows), list.Rows)
		}
		// A page that names no list must not be read as an empty one.
		if list.Container {
			t.Fatalf("a sign-in page reported a list container: %+v", list)
		}
	})
}

// TestSignInScriptAgainstFixtures runs the sign-in probe in a real browser
// against the captured markup, which is the only thing that can show the
// signal is read the way GitHub actually emits it. The signed-out issues page
// is the one that matters: it is a successful, correctly-empty extraction, so
// nothing else on the page distinguishes it from a repository with no ready
// work (issue #402).
func TestSignInScriptAgainstFixtures(t *testing.T) {
	tab, baseURL := fixtureTab(t)

	signIn := func(fixture string) signInState {
		t.Helper()
		if err := tab.Navigate(baseURL + "/" + fixture); err != nil {
			t.Fatalf("navigate to %s: %v", fixture, err)
		}
		var state signInState
		if err := tab.Eval(signInScript, &state); err != nil {
			t.Fatalf("evaluate sign-in probe on %s: %v", fixture, err)
		}
		return state
	}

	t.Run("signed-out empty issues page", func(t *testing.T) {
		// The extractor is right about this page, which is the whole problem.
		list := extractFixture(t, tab, baseURL, "github-issues-signed-out-empty.html")
		if !list.Recognized || !list.Empty || len(list.Rows) != 0 {
			t.Fatalf("recognized=%v empty=%v rows=%d, want a recognised empty list", list.Recognized, list.Empty, len(list.Rows))
		}
		state := signIn("github-issues-signed-out-empty.html")
		if !state.SignedOut || state.SignedIn {
			t.Fatalf("signIn=%+v, want signed out", state)
		}
		// GitHub emits the meta tag with empty content when there is no
		// viewer, so an empty login must not read as a signed-in session.
		if state.Login != "" {
			t.Fatalf("login %q, want empty", state.Login)
		}
	})

	t.Run("issue list is not called signed out", func(t *testing.T) {
		// A page with no evidence either way must not be blamed on the
		// profile, or every markup change would report the wrong cause.
		for _, fixture := range []string{"github-issues-list.html", "github-issues-empty.html", "project-board-table.html"} {
			if state := signIn(fixture); state.SignedOut {
				t.Fatalf("%s reported as signed out: %+v", fixture, state)
			}
		}
	})
}
