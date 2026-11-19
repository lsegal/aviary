package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/agents"
)

// visionPage is a tab that always answers with the same extraction result and
// can be screenshotted, so a whole poll loop can be simulated without a
// browser. It counts screenshots so a test can prove none were taken.
type visionPage struct {
	result      issueList
	status      int
	screenshots int
	shotErr     error
}

func (p *visionPage) Navigate(string) error { return nil }
func (p *visionPage) Reload() error         { return nil }
func (p *visionPage) HTTPStatus() int {
	if p.status == 0 {
		return 200
	}
	return p.status
}

func (p *visionPage) Eval(_ string, out any) error {
	// The sign-in probe must answer "no evidence" here, so the vision tests
	// keep exercising the extraction failure they are about.
	if state, ok := out.(*signInState); ok {
		*state = signInState{}
		return nil
	}
	encoded, err := json.Marshal(p.result)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, out.(*issueList))
}

func (p *visionPage) Screenshot() ([]byte, error) {
	p.screenshots++
	if p.shotErr != nil {
		return nil, p.shotErr
	}
	return []byte("png"), nil
}

// visionClock advances by a fixed step every time it is read, standing in for
// the wall clock a poll loop runs against.
type visionClock struct {
	now  time.Time
	step time.Duration
}

func (c *visionClock) Now() time.Time {
	c.now = c.now.Add(c.step)
	return c.now
}

// testVision builds a fallback with a recorded asker and a controllable clock.
// The asker answers with bare numbers and fails the test if it is asked for a
// qualified answer, so the repository path's contract is checked in place.
func testVision(t *testing.T, limit int, cooldown time.Duration, clock *visionClock, answer []int) (*Vision, *int, *[]string) {
	t.Helper()
	refs := make([]VisionRef, 0, len(answer))
	for _, number := range answer {
		refs = append(refs, VisionRef{Number: number})
	}
	return testVisionRefs(t, limit, cooldown, clock, refs, false)
}

// testVisionRefs is the same seam for either answer shape. wantQualified is the
// shape the source under test is expected to ask for.
func testVisionRefs(t *testing.T, limit int, cooldown time.Duration, clock *visionClock, answer []VisionRef, wantQualified bool) (*Vision, *int, *[]string) {
	t.Helper()
	// Recover can run for several targets at once, so the counters this helper
	// hands back are written from more than one goroutine.
	var mu sync.Mutex
	asks := 0
	var logs []string
	vision := &Vision{
		cooldown: cooldown,
		limit:    limit,
		now:      clock.Now,
		lastCall: map[string]time.Time{},
		ask: func(_ context.Context, _ []byte, _ string, qualified bool) ([]VisionRef, error) {
			mu.Lock()
			asks++
			mu.Unlock()
			if qualified != wantQualified {
				t.Errorf("vision asked with qualified=%v, want %v", qualified, wantQualified)
			}
			return answer, nil
		},
		logf: func(format string, args ...interface{}) {
			mu.Lock()
			defer mu.Unlock()
			logs = append(logs, fmt.Sprintf(format, args...))
		},
	}
	return vision, &asks, &logs
}

// pollTicks runs the issue source the way the watch loop does: one ListIssues
// per tick, with the clock advancing by the browser-mode poll interval.
func pollTicks(t *testing.T, source *IssueSource, ticks int) error {
	t.Helper()
	var lastErr error
	for i := 0; i < ticks; i++ {
		if _, err := source.ListIssues(context.Background(), "owner/repo"); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// A run that is working normally must never reach for the camera, no matter how
// long it polls: the design target for the fallback is zero AI calls in the
// steady state.
func TestVisionNeverCalledWhileExtractionWorks(t *testing.T) {
	clock := &visionClock{step: DefaultWatchInterval}
	vision, asks, _ := testVision(t, visionRunLimit, visionCooldown, clock, []int{7})
	page := &visionPage{result: issueList{
		Recognized: true,
		Rows:       []issueRow{{Number: 12, Title: "one"}},
	}}
	source := newTestIssueSource(&fakePage{}, "", false, nil)
	source.pageFor = func(string) (Page, error) { return page, nil }
	source.vision = vision

	// Twenty minutes of polling, which is twice the cooldown: a per-schedule
	// trigger or a leak on the success path would show up here.
	if err := pollTicks(t, source, int(20*time.Minute/DefaultWatchInterval)); err != nil {
		t.Fatalf("polling a healthy page failed: %v", err)
	}
	if page.screenshots != 0 || *asks != 0 {
		t.Fatalf("healthy polling used the vision fallback: %d screenshot(s), %d agent call(s)", page.screenshots, *asks)
	}
}

// An empty list is a real answer, not a failure, so it must not spend budget
// either.
func TestVisionNeverCalledForAnEmptyList(t *testing.T) {
	clock := &visionClock{step: DefaultWatchInterval}
	vision, asks, _ := testVision(t, visionRunLimit, visionCooldown, clock, []int{7})
	page := &visionPage{result: issueList{Recognized: true, Empty: true}}
	source := newTestIssueSource(&fakePage{}, "", false, nil)
	source.pageFor = func(string) (Page, error) { return page, nil }
	source.vision = vision

	issues, err := source.ListIssues(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %d", len(issues))
	}
	if page.screenshots != 0 || *asks != 0 {
		t.Fatalf("an empty list triggered the fallback: %d screenshot(s), %d agent call(s)", page.screenshots, *asks)
	}
}

// A page that fails to load is not an extraction failure: a screenshot of an
// error page tells an agent nothing, so the budget stays untouched.
func TestVisionNeverCalledForANavigationFailure(t *testing.T) {
	clock := &visionClock{step: DefaultWatchInterval}
	vision, asks, _ := testVision(t, visionRunLimit, visionCooldown, clock, []int{7})
	page := &visionPage{status: 404, result: issueList{Recognized: true}}
	source := newTestIssueSource(&fakePage{}, "", false, nil)
	source.pageFor = func(string) (Page, error) { return page, nil }
	source.vision = vision

	if _, err := source.ListIssues(context.Background(), "owner/repo"); err == nil {
		t.Fatal("expected an error for an HTTP 404 page")
	}
	if page.screenshots != 0 || *asks != 0 {
		t.Fatalf("an HTTP failure triggered the fallback: %d screenshot(s), %d agent call(s)", page.screenshots, *asks)
	}
}

// The cooldown is what keeps a permanently broken page from queueing an agent
// call on every tick of the browser-mode poll loop.
func TestVisionCooldownHoldsUnderRepeatedFailures(t *testing.T) {
	clock := &visionClock{step: DefaultWatchInterval}
	// The per-run cap is raised out of the way so this test measures the
	// cooldown alone.
	vision, asks, _ := testVision(t, 1000, visionCooldown, clock, []int{7})
	page := &visionPage{result: issueList{}}
	source := newTestIssueSource(&fakePage{}, "", false, nil)
	source.pageFor = func(string) (Page, error) { return page, nil }
	source.vision = vision

	// 20 minutes of polling spans two 10-minute cooldown windows, whatever
	// the browser-mode interval happens to be.
	pollTicks(t, source, int(20*time.Minute/DefaultWatchInterval))
	if *asks != 2 {
		t.Fatalf("expected 2 vision calls across 20 minutes of failures, got %d", *asks)
	}
	if page.screenshots != 2 {
		t.Fatalf("expected 2 screenshots, got %d", page.screenshots)
	}
}

// The per-run cap is the second half of the budget: once it is reached the
// fallback switches itself off for good and says so.
func TestVisionRunCapHoldsUnderRepeatedFailures(t *testing.T) {
	// A step far longer than the cooldown means only the cap can stop this.
	clock := &visionClock{step: time.Hour}
	vision, asks, logs := testVision(t, visionRunLimit, visionCooldown, clock, []int{7})
	page := &visionPage{result: issueList{}}
	source := newTestIssueSource(&fakePage{}, "", false, nil)
	source.pageFor = func(string) (Page, error) { return page, nil }
	source.vision = vision

	pollTicks(t, source, 50)
	if *asks != visionRunLimit {
		t.Fatalf("expected the run capped at %d vision calls, got %d", visionRunLimit, *asks)
	}
	if page.screenshots != visionRunLimit {
		t.Fatalf("expected %d screenshots, got %d", visionRunLimit, page.screenshots)
	}
	joined := strings.Join(*logs, "\n")
	if !strings.Contains(joined, "off for the rest of this run") {
		t.Fatalf("the run cap was not reported to the user:\n%s", joined)
	}
	// Every call the fallback does make is announced with its reason and its
	// running count, so a runaway is visible rather than silent.
	if !strings.Contains(joined, "call 1 of 3") || !strings.Contains(joined, "call 3 of 3") {
		t.Fatalf("vision calls were not logged with a running count:\n%s", joined)
	}
}

// The cooldown is per target, so one broken repository does not silence the
// fallback for another.
func TestVisionCooldownIsPerTarget(t *testing.T) {
	clock := &visionClock{step: DefaultWatchInterval}
	vision, asks, _ := testVision(t, 1000, visionCooldown, clock, []int{7})
	source := newTestIssueSource(&fakePage{}, "", false, nil)
	source.pageFor = func(string) (Page, error) { return &visionPage{}, nil }
	source.vision = vision

	for _, target := range []string{"owner/one", "owner/two", "owner/one", "owner/two"} {
		source.ListIssues(context.Background(), target)
	}
	if *asks != 2 {
		t.Fatalf("expected one call per target in the first window, got %d", *asks)
	}
}

// Recovered numbers become issues the dispatch path can hydrate, and paging
// stops there: the fallback answers for the whole list, not for one page.
func TestVisionRecoveredNumbersBecomeIssues(t *testing.T) {
	clock := &visionClock{step: DefaultWatchInterval}
	vision, asks, _ := testVision(t, visionRunLimit, visionCooldown, clock, []int{412, 398})
	source := newTestIssueSource(&fakePage{}, "", false, nil)
	source.pageFor = func(string) (Page, error) { return &visionPage{}, nil }
	source.vision = vision

	issues, err := source.ListIssues(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("expected the fallback to recover the list: %v", err)
	}
	if *asks != 1 {
		t.Fatalf("expected exactly one vision call, got %d", *asks)
	}
	if len(issues) != 2 || issues[0].Number != 412 || issues[1].Number != 398 {
		t.Fatalf("unexpected recovered issues: %+v", issues)
	}
	for _, issue := range issues {
		if issue.Repository != "owner/repo" || issue.State != "open" {
			t.Fatalf("recovered issue is not addressable: %+v", issue)
		}
	}
}

// An answer the agent could not give usefully is discarded, and the original
// extraction error is what the caller sees. Nothing is re-prompted.
func TestVisionDiscardsAnUnparseableAnswerWithoutRetrying(t *testing.T) {
	clock := &visionClock{step: DefaultWatchInterval}
	asks := 0
	vision := &Vision{
		cooldown: visionCooldown,
		limit:    visionRunLimit,
		now:      clock.Now,
		lastCall: map[string]time.Time{},
		ask: func(context.Context, []byte, string, bool) ([]VisionRef, error) {
			asks++
			return nil, errors.New("not a JSON array")
		},
	}
	source := newTestIssueSource(&fakePage{}, "", false, nil)
	source.pageFor = func(string) (Page, error) { return &visionPage{}, nil }
	source.vision = vision

	_, err := source.ListIssues(context.Background(), "owner/repo")
	if !errors.Is(err, ErrExtraction) {
		t.Fatalf("expected the original extraction error, got %v", err)
	}
	if asks != 1 {
		t.Fatalf("expected the failed answer to be discarded, not retried; got %d call(s)", asks)
	}
}

// A screenshot that cannot be taken still costs budget: charging only for
// successful captures would let a broken tab retry on every tick.
func TestVisionChargesForAFailedScreenshot(t *testing.T) {
	// A step longer than the cooldown isolates the per-run cap.
	clock := &visionClock{step: time.Hour}
	vision, asks, _ := testVision(t, visionRunLimit, visionCooldown, clock, []int{7})
	page := &visionPage{shotErr: errors.New("tab is gone")}
	source := newTestIssueSource(&fakePage{}, "", false, nil)
	source.pageFor = func(string) (Page, error) { return page, nil }
	source.vision = vision

	pollTicks(t, source, 50)
	if page.screenshots != visionRunLimit {
		t.Fatalf("expected failed captures to count against the cap, got %d attempt(s)", page.screenshots)
	}
	if *asks != 0 {
		t.Fatalf("expected no agent calls without an image, got %d", *asks)
	}
}

// Without -browser-vision the issue source has no fallback at all.
func TestIssueSourceWithoutVisionNeverScreenshots(t *testing.T) {
	page := &visionPage{}
	source := newTestIssueSource(&fakePage{}, "", false, nil)
	source.pageFor = func(string) (Page, error) { return page, nil }

	if _, err := source.ListIssues(context.Background(), "owner/repo"); !errors.Is(err, ErrExtraction) {
		t.Fatalf("expected the extraction error, got %v", err)
	}
	if page.screenshots != 0 {
		t.Fatalf("the default build took %d screenshot(s)", page.screenshots)
	}
}

func TestParseVisionNumbers(t *testing.T) {
	cases := []struct {
		name    string
		output  string
		want    []int
		wantErr bool
	}{
		{name: "bare array", output: "[412,398,377]", want: []int{412, 398, 377}},
		{name: "empty array", output: "[]", want: nil},
		{name: "trailing narration is tolerated", output: "Looking at the page.\n[12, 7]\n", want: []int{12, 7}},
		{name: "prose only", output: "I see issues 12 and 7.", wantErr: true},
		{name: "object", output: `{"issues":[12]}`, wantErr: true},
		{name: "strings", output: `["12"]`, wantErr: true},
		{name: "zero is not an issue", output: "[0]", wantErr: true},
		{name: "negative is not an issue", output: "[-3]", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseVisionRefs(tc.output, false)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			numbers := make([]int, 0, len(got))
			for _, ref := range got {
				if ref.Repository != "" {
					t.Fatalf("a bare-number answer named a repository: %+v", ref)
				}
				numbers = append(numbers, ref.Number)
			}
			if fmt.Sprint(numbers) != fmt.Sprint(tc.want) {
				t.Fatalf("got %v, want %v", numbers, tc.want)
			}
		})
	}
}

// A project board spans repositories, so its answers must name one. The parser
// is exactly as strict here as it is for bare numbers, and the two shapes are
// mutually exclusive: a bare-number answer to a board is discarded, because the
// numbers it names cannot be resolved to addressable issues.
func TestParseVisionRefsQualified(t *testing.T) {
	cases := []struct {
		name    string
		output  string
		want    []VisionRef
		wantErr bool
	}{
		{name: "qualified array", output: `[{"ref":"octocat/hello-world#412","status":"Todo"},{"ref":"octocat/spoon-knife#398","status":"In Progress"}]`, want: []VisionRef{{Repository: "octocat/hello-world", Number: 412, Status: "Todo"}, {Repository: "octocat/spoon-knife", Number: 398, Status: "In Progress"}}},
		{name: "empty array", output: "[]"},
		{name: "trailing narration is tolerated", output: "Reading the board.\n[{\"ref\":\"o/r#7\",\"status\":\"Ready\"}]\n", want: []VisionRef{{Repository: "o/r", Number: 7, Status: "Ready"}}},
		// An item the board shows no status for is a real answer, not a parse
		// failure: the caller keeps it addressable and says loudly that the
		// ready-state gate will not dispatch it.
		{name: "an empty status is kept", output: `[{"ref":"o/r#7","status":""}]`, want: []VisionRef{{Repository: "o/r", Number: 7}}},
		{name: "a missing status field reads as empty", output: `[{"ref":"o/r#7"}]`, want: []VisionRef{{Repository: "o/r", Number: 7}}},
		{name: "a status is trimmed", output: `[{"ref":"o/r#7","status":"  Todo  "}]`, want: []VisionRef{{Repository: "o/r", Number: 7, Status: "Todo"}}},
		{name: "the old bare-string shape is rejected", output: `["octocat/hello-world#412"]`, wantErr: true},
		{name: "bare numbers are rejected", output: "[412,398]", wantErr: true},
		{name: "bare number strings are rejected", output: `["412"]`, wantErr: true},
		{name: "a repository without an owner is rejected", output: `[{"ref":"repo#412","status":"Todo"}]`, wantErr: true},
		{name: "a reference without a number is rejected", output: `[{"ref":"o/r#","status":"Todo"}]`, wantErr: true},
		{name: "zero is not an issue", output: `[{"ref":"o/r#0","status":"Todo"}]`, wantErr: true},
		{name: "a full URL is rejected", output: `[{"ref":"https://github.com/o/r/issues/412","status":"Todo"}]`, wantErr: true},
		{name: "an extra field is a different shape", output: `[{"ref":"o/r#7","status":"Todo","title":"one"}]`, wantErr: true},
		{name: "a non-string status is rejected", output: `[{"ref":"o/r#7","status":3}]`, wantErr: true},
		{name: "prose in the status field is rejected", output: `[{"ref":"o/r#7","status":"The board groups this item under the Todo column, I think"}]`, wantErr: true},
		{name: "a multi-line status is rejected", output: `[{"ref":"o/r#7","status":"Todo\nReady"}]`, wantErr: true},
		{name: "prose only", output: "The board shows o/r#12.", wantErr: true},
		{name: "object", output: `{"issues":["o/r#12"]}`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseVisionRefs(tc.output, true)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestVisionArgs(t *testing.T) {
	codex := visionArgs(AgentSpec{Name: "codex", Model: "gpt-5"}, "/tmp/shot.png", "https://github.com/o/r/issues", false)
	if codex[0] != "exec" || codex[1] != "--skip-git-repo-check" || codex[2] != "--image" || codex[3] != "/tmp/shot.png" {
		t.Fatalf("codex is not given the image outside a git repository: %v", codex)
	}
	if !strings.Contains(codex[len(codex)-1], "strict JSON array") {
		t.Fatalf("codex prompt does not ask for a strict array: %q", codex[len(codex)-1])
	}
	claude := visionArgs(AgentSpec{Name: "claude"}, "/tmp/shot.png", "https://github.com/o/r/issues", false)
	if claude[0] != "-p" {
		t.Fatalf("claude is not run headlessly: %v", claude)
	}
	prompt := claude[len(claude)-1]
	if !strings.Contains(prompt, "/tmp/shot.png") || !strings.Contains(prompt, "nothing else") {
		t.Fatalf("claude prompt does not point at the image and constrain the answer: %q", prompt)
	}
	// A board target is asked for the qualified shape instead, and only that
	// shape: an example of a bare number would invite the answer the parser
	// discards.
	board := visionArgs(AgentSpec{Name: "claude"}, "/tmp/shot.png", "https://github.com/orgs/o/projects/1", true)
	boardPrompt := board[len(board)-1]
	if !strings.Contains(boardPrompt, "OWNER/REPO#NUMBER") || !strings.Contains(boardPrompt, "several repositories") {
		t.Fatalf("the board prompt does not ask for qualified references: %q", boardPrompt)
	}
	if strings.Contains(boardPrompt, "[412,398,377]") {
		t.Fatalf("the board prompt offers a bare-number example: %q", boardPrompt)
	}
	// The Status column is what the ready-state gate reads, so the board
	// prompt has to ask for it, verbatim rather than guessed (issue #398).
	if !strings.Contains(boardPrompt, `"status"`) || !strings.Contains(boardPrompt, "Status column") {
		t.Fatalf("the board prompt does not ask for the Status column: %q", boardPrompt)
	}
	if !strings.Contains(boardPrompt, "do not guess one") {
		t.Fatalf("the board prompt does not forbid guessing a status: %q", boardPrompt)
	}
}

// Concurrent targets must not be able to overspend the cap between them.
func TestVisionCapHoldsAcrossConcurrentTargets(t *testing.T) {
	clock := &visionClock{step: time.Hour}
	vision, asks, _ := testVision(t, visionRunLimit, visionCooldown, clock, []int{7})
	var mu sync.Mutex
	inner := vision.ask
	vision.ask = func(ctx context.Context, image []byte, url string, qualified bool) ([]VisionRef, error) {
		mu.Lock()
		defer mu.Unlock()
		return inner(ctx, image, url, qualified)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			vision.Recover(context.Background(), fmt.Sprintf("owner/repo%d", i), "url", "reason", func() ([]byte, error) { return []byte("png"), nil }, false)
		}(i)
	}
	wg.Wait()
	if *asks > visionRunLimit {
		t.Fatalf("the cap leaked under concurrency: %d call(s)", *asks)
	}
}

// TestVisionArgsComeFromTheAgentDefinition checks a screenshot is handed to an
// agent through that agent's own definition, so browser mode gains nothing
// hardcoded when a new CLI is declared in configuration.
func TestVisionArgsComeFromTheAgentDefinition(t *testing.T) {
	definition := agents.Definition{
		Name: "acme", Binary: "acme",
		Args: agents.Args{
			Run:    []agents.Fragment{{Args: []string{"{prompt}"}}},
			Resume: []agents.Fragment{{Args: []string{"{prompt}"}}},
			Vision: []agents.Fragment{{Args: []string{"look", "--picture", "{image}"}}, {When: "level", Args: []string{"--care", "{level}"}}, {Args: []string{"{prompt}"}}},
		},
		Session: agents.Session{Assign: agents.AssignNone},
		Output:  agents.Output{Format: agents.FormatText},
	}
	args := visionArgs(AgentSpec{Name: "acme", Definition: &definition, Level: "thorough"}, "/tmp/shot.png", "https://github.com/o/r/issues", false)
	if len(args) != 6 || args[0] != "look" || args[1] != "--picture" || args[2] != "/tmp/shot.png" || args[3] != "--care" || args[4] != "thorough" {
		t.Fatalf("vision args = %#v, want the definition's own template", args)
	}
	if !strings.Contains(args[5], "/tmp/shot.png") {
		t.Fatalf("vision prompt = %q, want it to name the screenshot", args[5])
	}
	// An agent with no vision template renders nothing rather than being run
	// with a bare prompt.
	definition.Args.Vision = nil
	if got := visionArgs(AgentSpec{Name: "acme", Definition: &definition}, "/tmp/shot.png", "https://github.com/o/r/issues", false); got != nil {
		t.Fatalf("vision args = %#v, want none", got)
	}
	// A spec that names an unknown agent and carries no definition renders
	// nothing either.
	if got := visionArgs(AgentSpec{Name: "nobody"}, "/tmp/shot.png", "https://github.com/o/r/issues", false); got != nil {
		t.Fatalf("vision args = %#v, want none", got)
	}
}
