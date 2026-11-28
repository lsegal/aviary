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

// boardTarget is the project target every test in this file polls. A board is
// the interesting case for the fallback precisely because its items come from
// more than one repository.
const boardTarget = "https://github.com/users/lsegal/projects/3"

// visionBoardPage is a board page that can also be screenshotted, so the
// fallback has something to photograph. It counts captures so a test can prove
// none were taken.
type visionBoardPage struct {
	fakeBoardPage
	screenshots int
	shotErr     error
}

func (p *visionBoardPage) Screenshot() ([]byte, error) {
	p.screenshots++
	if p.shotErr != nil {
		return nil, p.shotErr
	}
	return []byte("png"), nil
}

// newVisionBoard wires a board to a screenshot-capable page and a fallback,
// with the settle wait made free so a poll loop costs no real time.
func newVisionBoard(page *visionBoardPage, vision *Vision) *Board {
	return &Board{
		Page:           func(string) (Page, error) { return page, nil },
		sleep:          func(context.Context, time.Duration) bool { return true },
		settleAttempts: 2,
		Vision:         vision,
	}
}

// pollBoardTicks runs the board the way the watch loop does: one ListIssues per
// tick, with the fallback's clock advancing by the browser-mode poll interval.
func pollBoardTicks(board *Board, ticks int) {
	for i := 0; i < ticks; i++ {
		_, _ = board.ListIssues(context.Background(), boardTarget)
	}
}

// A board that renders normally must never reach for the camera, no matter how
// long it is polled: zero AI calls in the steady state is the design target for
// project targets exactly as it is for issue pages.
func TestBoardVisionNeverCalledWhileTheBoardRenders(t *testing.T) {
	clock := &visionClock{step: DefaultWatchInterval}
	vision, asks, _ := testVisionRefs(t, visionRunLimit, visionCooldown, clock, nil, true)
	page := &visionBoardPage{fakeBoardPage: fakeBoardPage{documents: []string{readBoardFixture(t, "project-board-table.html")}}}
	board := newVisionBoard(page, vision)

	// Twenty minutes of polling, which is twice the cooldown.
	pollBoardTicks(board, int(20*time.Minute/DefaultWatchInterval))
	if issues := boardIssues(t, board, boardTarget); len(issues) != 4 {
		t.Fatalf("extracted %d issues from a healthy board, want 4", len(issues))
	}
	if page.screenshots != 0 || *asks != 0 {
		t.Fatalf("a healthy board used the vision fallback: %d screenshot(s), %d agent call(s)", page.screenshots, *asks)
	}
}

// A board that never renders spends at most one screenshot per cooldown window,
// and the per-run cap that stops it is the same budget the issues page spends
// from: three calls for the run, not three per page kind.
func TestBoardVisionSpendsOneScreenshotPerCooldownAndStopsAtTheSharedCap(t *testing.T) {
	clock := &visionClock{step: DefaultWatchInterval}
	// The cap is raised out of the way first, so this half measures the
	// cooldown alone: 20 minutes of polling spans two cooldown windows.
	vision, asks, _ := testVisionRefs(t, 1000, visionCooldown, clock, nil, true)
	page := &visionBoardPage{fakeBoardPage: fakeBoardPage{documents: []string{readBoardFixture(t, "project-board-loading.html")}}}
	board := newVisionBoard(page, vision)

	pollBoardTicks(board, int(20*time.Minute/DefaultWatchInterval))
	if *asks != 2 || page.screenshots != 2 {
		t.Fatalf("expected 2 calls across 20 minutes of a broken board, got %d screenshot(s) and %d agent call(s)", page.screenshots, *asks)
	}

	// Now the cap, with one budget shared by a broken board and a broken
	// issues page: between them they may spend three calls for the whole run.
	clock = &visionClock{step: time.Hour}
	shared, sharedAsks, logs := testVisionRefs(t, visionRunLimit, visionCooldown, clock, nil, false)
	boardPage := &visionBoardPage{fakeBoardPage: fakeBoardPage{documents: []string{readBoardFixture(t, "project-board-loading.html")}}}
	// The board asks for the qualified shape and the issues page for the bare
	// one, so the shared asker checks the shape per call rather than up front.
	inner := shared.ask
	shared.ask = func(ctx context.Context, image []byte, url string, qualified bool) ([]VisionRef, error) {
		if wantQualified := strings.Contains(url, "/projects/"); qualified != wantQualified {
			t.Errorf("%s asked with qualified=%v, want %v", url, qualified, wantQualified)
		}
		return inner(ctx, image, url, false)
	}
	sharedBoard := newVisionBoard(boardPage, shared)
	issuePage := &visionPage{}
	issues := newTestIssueSource(&fakePage{}, "", false, nil)
	issues.pageFor = func(string) (Page, error) { return issuePage, nil }
	issues.vision = shared

	for i := 0; i < 50; i++ {
		_, _ = sharedBoard.ListIssues(context.Background(), boardTarget)
		_, _ = issues.ListIssues(context.Background(), "owner/repo")
	}
	if *sharedAsks != visionRunLimit {
		t.Fatalf("the run cap is not shared: %d call(s), want %d", *sharedAsks, visionRunLimit)
	}
	if total := boardPage.screenshots + issuePage.screenshots; total != visionRunLimit {
		t.Fatalf("board and issues page took %d screenshot(s) between them, want %d", total, visionRunLimit)
	}
	if boardPage.screenshots == 0 {
		t.Fatal("the board never got to spend from the shared budget")
	}
	if joined := strings.Join(*logs, "\n"); !strings.Contains(joined, "off for the rest of this run") {
		t.Fatalf("the shared run cap was not reported to the user:\n%s", joined)
	}
}

// The point of asking a board for OWNER/REPO#NUMBER: the answer comes back as
// issues the dispatch path can address, each carrying the repository it lives
// in rather than a number with no home.
func TestBoardVisionRecoveredReferencesBecomeAddressableIssues(t *testing.T) {
	clock := &visionClock{step: DefaultWatchInterval}
	answer := []VisionRef{
		{Repository: "lsegal/aviary", Number: 412, Status: "Todo"},
		{Repository: "lsegal/yard", Number: 398, Status: "In Progress"},
		// The same item twice is what a scrolled screenshot can look like.
		{Repository: "lsegal/aviary", Number: 412, Status: "Todo"},
		// References that name no repository or no number cannot be
		// addressed, so they are dropped.
		{Number: 7, Status: "Todo"},
		{Repository: "lsegal/aviary", Status: "Todo"},
	}
	vision, asks, _ := testVisionRefs(t, visionRunLimit, visionCooldown, clock, answer, true)
	page := &visionBoardPage{fakeBoardPage: fakeBoardPage{documents: []string{readBoardFixture(t, "project-board-loading.html")}}}
	board := newVisionBoard(page, vision)

	issues, err := board.ListIssues(context.Background(), boardTarget)
	if err != nil {
		t.Fatalf("expected the fallback to recover the board: %v", err)
	}
	if *asks != 1 {
		t.Fatalf("expected exactly one vision call, got %d", *asks)
	}
	if len(issues) != 2 {
		t.Fatalf("recovered %d issues, want 2 de-duplicated: %+v", len(issues), issues)
	}
	want := []string{"lsegal/aviary#412", "lsegal/yard#398"}
	wantStatus := []string{"Todo", "In Progress"}
	for i, issue := range issues {
		if got := fmt.Sprintf("%s#%d", issue.Repository, issue.Number); got != want[i] {
			t.Fatalf("issue %d is %q, want %q", i, got, want[i])
		}
		if issue.ProjectStatus != wantStatus[i] {
			t.Fatalf("issue %d has status %q, want %q", i, issue.ProjectStatus, wantStatus[i])
		}
	}
	// The recovery is not free for the rest of the window: the push-mode probe
	// that follows on the same tick is inside the cooldown, so it reports the
	// extraction failure rather than spending a second screenshot.
	if _, err := board.ProjectState(context.Background(), boardTarget); !errors.Is(err, ErrExtraction) {
		t.Fatalf("ProjectState inside the cooldown: %v, want the extraction error", err)
	}
	if *asks != 1 || page.screenshots != 1 {
		t.Fatalf("the cooldown did not hold: %d screenshot(s), %d agent call(s)", page.screenshots, *asks)
	}
}

// A bare-number answer names no repository, so for a project target it is
// discarded and the original extraction failure is what the caller sees. The
// real parser is used here rather than a canned answer, because rejecting that
// shape is the parser's job.
func TestBoardVisionRejectsABareNumberAnswer(t *testing.T) {
	clock := &visionClock{step: DefaultWatchInterval}
	asks := 0
	vision := &Vision{
		cooldown: visionCooldown,
		limit:    visionRunLimit,
		now:      clock.Now,
		lastCall: map[string]time.Time{},
		ask: func(_ context.Context, _ []byte, _ string, qualified bool) ([]VisionRef, error) {
			asks++
			return parseVisionRefs("[412,398]", qualified)
		},
	}
	page := &visionBoardPage{fakeBoardPage: fakeBoardPage{documents: []string{readBoardFixture(t, "project-board-loading.html")}}}
	board := newVisionBoard(page, vision)

	_, err := board.ListIssues(context.Background(), boardTarget)
	if !errors.Is(err, ErrExtraction) {
		t.Fatalf("expected the original extraction error, got %v", err)
	}
	if asks != 1 {
		t.Fatalf("expected the rejected answer to be discarded, not retried; got %d call(s)", asks)
	}
}

// Without -browser-vision the board has no fallback at all, which is the
// default build.
func TestBoardWithoutVisionNeverScreenshots(t *testing.T) {
	page := &visionBoardPage{fakeBoardPage: fakeBoardPage{documents: []string{readBoardFixture(t, "project-board-loading.html")}}}
	board := newVisionBoard(page, nil)

	if _, err := board.ListIssues(context.Background(), boardTarget); !errors.Is(err, ErrExtraction) {
		t.Fatalf("expected the extraction error, got %v", err)
	}
	if page.screenshots != 0 {
		t.Fatalf("the default build took %d screenshot(s)", page.screenshots)
	}
}

// A page that cannot be photographed gives the fallback nothing to read, so
// the agent is never asked and the extraction failure is what the caller sees.
func TestBoardVisionSkipsAPageThatCannotScreenshot(t *testing.T) {
	clock := &visionClock{step: DefaultWatchInterval}
	vision, asks, _ := testVisionRefs(t, visionRunLimit, visionCooldown, clock, nil, true)
	page := &fakeBoardPage{documents: []string{readBoardFixture(t, "project-board-loading.html")}}
	board := &Board{
		Page:           func(string) (Page, error) { return page, nil },
		sleep:          func(context.Context, time.Duration) bool { return true },
		settleAttempts: 2,
		Vision:         vision,
	}

	if _, err := board.ListIssues(context.Background(), boardTarget); !errors.Is(err, ErrExtraction) {
		t.Fatalf("expected the extraction error, got %v", err)
	}
	if *asks != 0 {
		t.Fatalf("the agent was asked %d time(s) without a screenshot", *asks)
	}
}

// Recovering a board is only worth anything if the issues it recovers can
// actually be dispatched. The Status column the agent reads off the screenshot
// is what the ready-state gate matches, so an item the board shows in its
// default ready column passes that gate under the default ready state exactly like an item the DOM
// extractor read (issue #398).
func TestBoardVisionRecoveredItemsDispatchUnderTheDefaultReadyState(t *testing.T) {
	clock := &visionClock{step: DefaultWatchInterval}
	answer := []VisionRef{
		{Repository: "lsegal/aviary", Number: 412, Status: "Todo"},
		{Repository: "lsegal/yard", Number: 398, Status: "Done"},
	}
	vision, _, _ := testVisionRefs(t, visionRunLimit, visionCooldown, clock, answer, true)
	page := &visionBoardPage{fakeBoardPage: fakeBoardPage{documents: []string{readBoardFixture(t, "project-board-loading.html")}}}
	board := newVisionBoard(page, vision)

	issues, err := board.ListIssues(context.Background(), boardTarget)
	if err != nil {
		t.Fatalf("expected the fallback to recover the board: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("recovered %d issues, want 2: %+v", len(issues), issues)
	}
	// The first item sits in a default ready column, so it dispatches and
	// the finished one does not.
	if !core.ShouldDispatchIssue(boardTarget, issues[0], false, false, false, false, "") {
		t.Fatalf("a recovered %q item is not a dispatch candidate", issues[0].ProjectStatus)
	}
	if core.ShouldDispatchIssue(boardTarget, issues[1], false, false, false, false, "") {
		t.Fatalf("a recovered %q item is a dispatch candidate", issues[1].ProjectStatus)
	}
	// A configured --ready-state is matched the same way.
	if !core.ShouldDispatchIssue(boardTarget, issues[1], false, false, false, false, "Done") {
		t.Fatalf("a recovered item does not honour a configured ready state")
	}
}

// A board that genuinely shows an item no status leaves it undispatchable,
// because there is nothing for the ready-state gate to match and guessing one
// is exactly what this fallback must not do. The run has to say so: a quiet
// recovered board must not look like a recovered board with nothing to do.
func TestBoardVisionSaysWhyAStatuslessItemStaysQuiet(t *testing.T) {
	clock := &visionClock{step: DefaultWatchInterval}
	answer := []VisionRef{
		{Repository: "lsegal/aviary", Number: 412, Status: "Todo"},
		{Repository: "lsegal/yard", Number: 398},
	}
	vision, _, logs := testVisionRefs(t, visionRunLimit, visionCooldown, clock, answer, true)
	page := &visionBoardPage{fakeBoardPage: fakeBoardPage{documents: []string{readBoardFixture(t, "project-board-loading.html")}}}
	board := newVisionBoard(page, vision)

	issues, err := board.ListIssues(context.Background(), boardTarget)
	if err != nil {
		t.Fatalf("expected the fallback to recover the board: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("recovered %d issues, want 2: %+v", len(issues), issues)
	}
	if issues[1].ProjectStatus != "" {
		t.Fatalf("a statusless item was given a status: %q", issues[1].ProjectStatus)
	}
	if core.ShouldDispatchIssue(boardTarget, issues[1], false, false, false, false, "") {
		t.Fatal("an item with no status was treated as ready")
	}
	joined := strings.Join(*logs, "\n")
	if !strings.Contains(joined, "1 of 2 recovered item(s)") || !strings.Contains(joined, "no Status column") {
		t.Fatalf("the run did not say why the recovered board is quiet:\n%s", joined)
	}
	if !strings.Contains(joined, "will not dispatch them") {
		t.Fatalf("the log does not name the consequence:\n%s", joined)
	}
}

// The status the agent reads is used verbatim, so an item parked in the column
// a Aviary instance claims work with is recognised as claimed rather than
// dispatched afresh.
func TestBoardVisionRecoveredInProgressItemIsRecognised(t *testing.T) {
	clock := &visionClock{step: DefaultWatchInterval}
	answer := []VisionRef{{Repository: "lsegal/aviary", Number: 412, Status: "In Progress"}}
	vision, _, _ := testVisionRefs(t, visionRunLimit, visionCooldown, clock, answer, true)
	page := &visionBoardPage{fakeBoardPage: fakeBoardPage{documents: []string{readBoardFixture(t, "project-board-loading.html")}}}
	board := newVisionBoard(page, vision)

	issues, err := board.ListIssues(context.Background(), boardTarget)
	if err != nil {
		t.Fatalf("expected the fallback to recover the board: %v", err)
	}
	if !core.ProjectItemInProgress(boardTarget, issues[0]) {
		t.Fatalf("a recovered %q item is not seen as claimed work", issues[0].ProjectStatus)
	}
}
