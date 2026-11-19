package browser

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/projects/core"
)

// TestSignedOutErrorIdentity pins the two properties callers depend on:
// a signed-out read is matchable as its own category, and it is not the
// extraction failure the screenshot fallback exists to recover. No screenshot
// of a signed-out page can be read into issues, so routing one there would
// spend the vision budget on a page that can never answer.
func TestSignedOutErrorIdentity(t *testing.T) {
	err := error(&SignedOutError{URL: "https://github.com/lsegal/aviary/issues", Profile: "/tmp/profile"})
	if !errors.Is(err, ErrSignedOut) {
		t.Fatalf("signed-out error does not match ErrSignedOut")
	}
	if errors.Is(err, ErrExtraction) {
		t.Fatalf("signed-out error must not be mistaken for an extraction failure")
	}
	if !errors.Is(&ExtractionError{URL: "u"}, ErrExtraction) {
		t.Fatalf("extraction error stopped matching its own sentinel")
	}
	if errors.Is(&ExtractionError{URL: "u"}, ErrSignedOut) {
		t.Fatalf("extraction error must not be mistaken for a signed-out read")
	}
	for _, want := range []string{"https://github.com/lsegal/aviary/issues", "/tmp/profile", "profile_directory", "Sign in to GitHub in Aviary's browser", "gh polling"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("signed-out message %q does not mention %q", err.Error(), want)
		}
	}
	// Without a known profile the message still has to stand on its own.
	bare := (&SignedOutError{URL: "https://github.com/lsegal/aviary/issues"}).Error()
	if strings.Contains(bare, "the browser profile at ") {
		t.Fatalf("bare message should not name an empty profile: %q", bare)
	}
}

// TestSignedOutProbe covers the probe's answers, including the one that
// matters most: an evaluation that fails must not be read as evidence of
// anything, or a broken probe would blame a perfectly good profile.
func TestSignedOutProbe(t *testing.T) {
	for _, test := range []struct {
		name  string
		state signInState
		fail  bool
		want  bool
	}{
		{name: "signed out", state: signInState{SignedOut: true}, want: true},
		{name: "signed in", state: signInState{SignedIn: true, Login: "lsegal"}},
		{name: "no evidence either way"},
		{name: "probe failed", fail: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			page := &fakePage{signIn: test.state, status: 200}
			if test.fail {
				page.evalErr = errors.New("boom")
			}
			if got := signedOutPage(page); got != test.want {
				t.Fatalf("signedOutPage = %v, want %v", got, test.want)
			}
		})
	}
}

// TestIssueSourceSignedOutEmptyList is the regression for issue #402: a
// page that renders GitHub's own honest empty result for an "@me" filter must
// stop the run with an actionable message rather than being reported as an
// issue list with nothing in it.
func TestIssueSourceSignedOutEmptyList(t *testing.T) {
	var logs []string
	page := &fakePage{
		results: []issueList{{Rows: nil, Recognized: true, Empty: true}},
		signIn:  signInState{SignedOut: true},
	}
	source := newTestIssueSource(page, core.DefaultIssueFilter, false, func(format string, args ...interface{}) {
		logs = append(logs, format)
	})
	source.profile = "/tmp/aviary-profile"
	issues, err := source.ListIssues(context.Background(), "lsegal/aviary")
	if err == nil {
		t.Fatalf("signed-out empty list returned %d issue(s) and no error", len(issues))
	}
	if !errors.Is(err, ErrSignedOut) {
		t.Fatalf("error %v does not match ErrSignedOut", err)
	}
	if !strings.Contains(err.Error(), "/tmp/aviary-profile") {
		t.Fatalf("error %v does not name the profile", err)
	}
	if page.signInEvals != 1 {
		t.Fatalf("sign-in probe ran %d time(s), want exactly 1", page.signInEvals)
	}
	for _, line := range logs {
		if strings.Contains(line, "browser read") {
			t.Fatalf("a signed-out read must not be logged as an issue count: %q", line)
		}
	}
}

// TestIssueSourceSignedOutUnreadablePage prefers the signed-out
// diagnosis over the extraction one when both could be reported: an extractor
// fix cannot help a profile that is logged out.
func TestIssueSourceSignedOutUnreadablePage(t *testing.T) {
	page := &fakePage{
		results: []issueList{{Recognized: false}},
		signIn:  signInState{SignedOut: true},
	}
	source := newTestIssueSource(page, core.DefaultIssueFilter, false, nil)
	_, err := source.ListIssues(context.Background(), "lsegal/aviary")
	if !errors.Is(err, ErrSignedOut) {
		t.Fatalf("error %v, want a signed-out read", err)
	}
	if errors.Is(err, ErrExtraction) {
		t.Fatalf("error %v was reported as an extraction failure", err)
	}
}

// TestIssueSourceSignedInEmptyList keeps the honest empty list honest: a
// signed-in profile watching a repository with nothing ready still reports no
// issues rather than an error.
func TestIssueSourceSignedInEmptyList(t *testing.T) {
	page := &fakePage{
		results: []issueList{{Recognized: true, Empty: true}},
		signIn:  signInState{SignedIn: true, Login: "lsegal"},
	}
	source := newTestIssueSource(page, core.DefaultIssueFilter, false, nil)
	issues, err := source.ListIssues(context.Background(), "lsegal/aviary")
	if err != nil {
		t.Fatalf("signed-in empty list: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("got %d issue(s), want none", len(issues))
	}
}

// TestIssueSourceSkipsProbeWhenRowsWereRead is the cost guarantee: the
// probe is a failure-path diagnosis, so a poll that read issues must not pay
// for it.
func TestIssueSourceSkipsProbeWhenRowsWereRead(t *testing.T) {
	page := &fakePage{results: []issueList{{
		Rows:       []issueRow{{Number: 402, Repository: "lsegal/aviary", Title: "bug", State: "open"}},
		Recognized: true,
	}}}
	source := newTestIssueSource(page, core.DefaultIssueFilter, false, nil)
	issues, err := source.ListIssues(context.Background(), "lsegal/aviary")
	if err != nil {
		t.Fatalf("list issues: %v", err)
	}
	if len(issues) != 1 || issues[0].Number != 402 {
		t.Fatalf("got %+v, want issue #402", issues)
	}
	if page.signInEvals != 0 {
		t.Fatalf("sign-in probe ran %d time(s) on a successful read", page.signInEvals)
	}
}

// TestBoardSignedOutEmptyBoard is issue #402's other face: a board that
// renders an honestly empty result because "@me" matched nobody must be
// reported as a signed-out profile rather than as a board with no ready work.
func TestBoardSignedOutEmptyBoard(t *testing.T) {
	page := &fakeBoardPage{
		documents: []string{readBoardFixture(t, "project-board-empty.html")},
		status:    200,
		signIn:    signInState{SignedOut: true},
	}
	board, _ := newTestBoard(page)
	board.Profile = "/tmp/aviary-profile"
	_, err := board.ListIssues(context.Background(), "https://github.com/users/lsegal/projects/3")
	if !errors.Is(err, ErrSignedOut) {
		t.Fatalf("error %v, want a signed-out read", err)
	}
	if !strings.Contains(err.Error(), "/tmp/aviary-profile") {
		t.Fatalf("error %v does not name the profile", err)
	}
}

// TestBoardSignedOutUnrenderedBoard checks the ordering against the
// vision fallback: a board that never renders because the profile is signed
// out is diagnosed, not screenshotted.
func TestBoardSignedOutUnrenderedBoard(t *testing.T) {
	page := &fakeBoardPage{
		documents: []string{readBoardFixture(t, "project-board-loading.html")},
		status:    200,
		signIn:    signInState{SignedOut: true},
	}
	board, _ := newTestBoard(page)
	board.settleAttempts = 2
	_, err := board.ListIssues(context.Background(), "https://github.com/users/lsegal/projects/3")
	if !errors.Is(err, ErrSignedOut) {
		t.Fatalf("error %v, want a signed-out read", err)
	}
	if errors.Is(err, ErrExtraction) {
		t.Fatalf("error %v was reported as an extraction failure", err)
	}
}

// TestBoardSignedInEmptyBoard keeps an empty board with a signed-in
// profile reporting no items rather than an error.
func TestBoardSignedInEmptyBoard(t *testing.T) {
	page := &fakeBoardPage{
		documents: []string{readBoardFixture(t, "project-board-empty.html")},
		status:    200,
		signIn:    signInState{SignedIn: true, Login: "lsegal"},
	}
	board, _ := newTestBoard(page)
	issues, err := board.ListIssues(context.Background(), "https://github.com/users/lsegal/projects/3")
	if err != nil {
		t.Fatalf("signed-in empty board: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("got %d item(s), want none", len(issues))
	}
}
