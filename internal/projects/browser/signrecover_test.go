package browser

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

// recoveryHarness is a recovery with every side effect replaced: no browser is
// launched, no window is opened, and time is whatever the test says it is.
type recoveryHarness struct {
	recovery  *SignInRecovery
	logs      []string
	suspended int
	resumed   int
	logins    int
	now       time.Time
	mu        sync.Mutex
}

func newRecoveryHarness(t *testing.T, headed error, login func() (string, error)) *recoveryHarness {
	t.Helper()
	h := &recoveryHarness{now: time.Unix(1700000000, 0)}
	h.recovery = &SignInRecovery{
		config:  Config{Profile: "/tmp/profile"},
		logf:    func(format string, args ...interface{}) { h.logs = append(h.logs, fmt.Sprintf(format, args...)) },
		timeout: time.Second,
		suspend: func() error { h.suspended++; return nil },
		resume:  func() error { h.resumed++; return nil },
		login: func(_ context.Context, config Config, _ io.Writer, _ time.Duration) (string, error) {
			h.logins++
			// The window has to open on the same profile the watch loop reads
			// GitHub with; signing a different one in would fix nothing.
			if config.Profile != "/tmp/profile" {
				h.logs = append(h.logs, "login received the wrong profile: "+config.Profile)
			}
			return login()
		},
		headed: func() error { return headed },
		now:    func() time.Time { return h.now },
	}
	return h
}

func (h *recoveryHarness) logged(substr string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, line := range h.logs {
		if strings.Contains(line, substr) {
			return true
		}
	}
	return false
}

func TestSignedOutStatusClassification(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		signIn signInState
		want   bool
	}{
		{name: "404 on a signed-out page is a sign-in signal", status: 404, signIn: signInState{SignedOut: true}, want: true},
		{name: "403 on a signed-out page is a sign-in signal", status: 403, signIn: signInState{SignedOut: true}, want: true},
		{name: "404 on a signed-in page is a real 404", status: 404, signIn: signInState{SignedIn: true, Login: "lsegal"}, want: false},
		{name: "404 with no evidence either way blames nothing", status: 404, want: false},
		{name: "500 is never a sign-in signal", status: 500, signIn: signInState{SignedOut: true}, want: false},
		{name: "200 is never a sign-in signal", status: 200, signIn: signInState{SignedOut: true}, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			page := &fakePage{status: test.status, signIn: test.signIn}
			if got := signedOutStatus(page, test.status); got != test.want {
				t.Fatalf("signedOutStatus(%d) = %v, want %v", test.status, got, test.want)
			}
			if test.status < 400 && page.signInEvals != 0 {
				t.Fatalf("a status that cannot mean signed out still cost %d sign-in probe(s)", page.signInEvals)
			}
		})
	}
}

func TestIssueSourceReportsSignedOutOn404(t *testing.T) {
	page := &fakePage{status: 404, signIn: signInState{SignedOut: true}}
	source := newTestIssueSource(page, core.DefaultIssueFilter, false, nil)
	source.profile = "/tmp/profile"
	_, err := source.ListIssues(context.Background(), "lsegal/aviary")
	if !errors.Is(err, ErrSignedOut) {
		t.Fatalf("error %v, want a signed-out diagnosis", err)
	}
	var signedOut *SignedOutError
	if !errors.As(err, &signedOut) || signedOut.Status != 404 {
		t.Fatalf("error %v does not carry the 404 that produced it", err)
	}
	if !strings.Contains(err.Error(), "Sign in to GitHub in Aviary's browser") || !strings.Contains(err.Error(), "/tmp/profile") {
		t.Fatalf("error %q does not name the profile and the fix", err)
	}
}

func TestBoardReportsSignedOutOn404(t *testing.T) {
	page := &fakePage{status: 404, signIn: signInState{SignedOut: true}}
	board := &Board{Page: func(string) (Page, error) { return page, nil }, Profile: "/tmp/profile"}
	_, err := board.ListIssues(context.Background(), "https://github.com/users/lsegal/projects/1")
	if !errors.Is(err, ErrSignedOut) {
		t.Fatalf("error %v, want a signed-out diagnosis", err)
	}
}

func TestSignInRecoveryRecordsSignedOutWithoutOpeningAWindow(t *testing.T) {
	h := newRecoveryHarness(t, nil, func() (string, error) {
		t.Fatal("a signed-out read opened a sign-in window on its own")
		return "", nil
	})
	h.recovery.attempt(context.Background(), &SignedOutError{URL: "https://github.com/lsegal/private/issues", Profile: "/tmp/profile", Status: 404})
	if h.suspended != 0 || h.logins != 0 {
		t.Fatalf("suspended %d, logins %d; a signed-out read only records the session", h.suspended, h.logins)
	}
	state := h.recovery.State()
	if !state.SignedOut || state.URL != "https://github.com/lsegal/private/issues" || state.Status != 404 || !state.Since.Equal(h.now) {
		t.Fatalf("state %+v, want the signed-out 404 for the banner", state)
	}
	if state.WindowUnavailable != "" {
		t.Fatalf("a machine with a display reported %q", state.WindowUnavailable)
	}
	if !h.logged("GitHub returned 404 for https://github.com/lsegal/private/issues") || !h.logged("gh polling") {
		t.Fatalf("recovery did not announce the 404 and the ways forward: %v", h.logs)
	}
}

func TestSignInRecoveryBacksOffItsReminder(t *testing.T) {
	h := newRecoveryHarness(t, nil, func() (string, error) { return "", nil })
	signedOut := &SignedOutError{URL: "https://github.com/lsegal/private/issues", Profile: "/tmp/profile"}
	h.recovery.attempt(context.Background(), signedOut)
	since := h.now
	for tick := 0; tick < 5; tick++ {
		h.now = h.now.Add(time.Minute)
		h.recovery.attempt(context.Background(), signedOut)
	}
	if got := len(h.logs); got != 1 {
		t.Fatalf("logged %d reminders during the back-off, want 1: %v", got, h.logs)
	}
	if state := h.recovery.State(); !state.Since.Equal(since) {
		t.Fatalf("Since moved to %v; it marks when the spell began (%v)", state.Since, since)
	}
	h.now = since.Add(signInBackoff)
	h.recovery.attempt(context.Background(), signedOut)
	if got := len(h.logs); got != 2 {
		t.Fatalf("did not remind again after the back-off: %v", h.logs)
	}
	if want := h.now.Add(2 * signInBackoff); !h.recovery.remind.Equal(want) {
		t.Fatalf("next reminder at %v, want the doubled back-off at %v", h.recovery.remind, want)
	}
}

func TestSignInRecoverySignInOpensWindowAndResumes(t *testing.T) {
	h := newRecoveryHarness(t, nil, func() (string, error) { return "lsegal", nil })
	h.recovery.attempt(context.Background(), &SignedOutError{URL: "https://github.com/lsegal/private/issues"})
	login, err := h.recovery.SignIn(context.Background())
	if err != nil || login != "lsegal" {
		t.Fatalf("SignIn() = %q, %v", login, err)
	}
	if h.suspended != 1 || h.resumed != 1 || h.logins != 1 {
		t.Fatalf("suspended %d, resumed %d, logins %d; want one of each", h.suspended, h.resumed, h.logins)
	}
	if h.logged("wrong profile") {
		t.Fatalf("signed in on the wrong profile: %v", h.logs)
	}
	if state := h.recovery.State(); state.SignedOut || state.SigningIn {
		t.Fatalf("state %+v after a sign-in that worked", state)
	}
	if !h.logged("signed in to GitHub as lsegal") {
		t.Fatalf("did not report the account it signed in as: %v", h.logs)
	}
}

func TestSignInRecoverySignInFailureKeepsPolling(t *testing.T) {
	h := newRecoveryHarness(t, nil, func() (string, error) { return "", errors.New("timed out") })
	h.recovery.attempt(context.Background(), &SignedOutError{URL: "https://github.com/lsegal/private/issues"})
	if _, err := h.recovery.SignIn(context.Background()); err == nil {
		t.Fatal("SignIn() succeeded after the login timed out")
	}
	if h.resumed != 1 {
		t.Fatalf("a failed sign-in left the headless browser stopped (%d resume(s))", h.resumed)
	}
	state := h.recovery.State()
	if !state.SignedOut || !strings.Contains(state.LastError, "timed out") {
		t.Fatalf("state %+v, want still signed out with the failure", state)
	}
}

func TestSignInRecoveryRefusesWithoutADisplay(t *testing.T) {
	h := newRecoveryHarness(t, errors.New("no display server (DISPLAY and WAYLAND_DISPLAY are both unset)"), func() (string, error) {
		t.Fatal("opened a login window on a machine with no display server")
		return "", nil
	})
	h.recovery.attempt(context.Background(), &SignedOutError{URL: "https://github.com/lsegal/private/issues"})
	if state := h.recovery.State(); !strings.Contains(state.WindowUnavailable, "no display server") {
		t.Fatalf("state %+v does not say why no window can appear", state)
	}
	_, err := h.recovery.SignIn(context.Background())
	if !errors.Is(err, ErrSignInWindowUnavailable) {
		t.Fatalf("SignIn() error = %v, want ErrSignInWindowUnavailable", err)
	}
	for _, want := range []string{"no display server", "gh polling", "import"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal %q does not mention %q", err, want)
		}
	}
	if h.suspended != 0 {
		t.Fatalf("stopped the headless browser for a window that can never appear (%d suspend(s))", h.suspended)
	}
}

func TestSignInRecoveryRefusesASecondWindow(t *testing.T) {
	release := make(chan struct{})
	opened := make(chan struct{})
	h := newRecoveryHarness(t, nil, func() (string, error) {
		close(opened)
		<-release
		return "lsegal", nil
	})
	done := make(chan error, 1)
	go func() { _, err := h.recovery.SignIn(context.Background()); done <- err }()
	<-opened
	if _, err := h.recovery.SignIn(context.Background()); !errors.Is(err, ErrSigningIn) {
		t.Fatalf("second SignIn() error = %v, want ErrSigningIn", err)
	}
	if !h.recovery.State().SigningIn {
		t.Fatal("state does not report the open sign-in window")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("first SignIn() error = %v", err)
	}
}

func TestSignInRecoveryIgnoresOtherFailures(t *testing.T) {
	h := newRecoveryHarness(t, nil, func() (string, error) { return "lsegal", nil })
	h.recovery.attempt(context.Background(), errors.New("load issue list: GitHub returned HTTP 500"))
	h.recovery.attempt(context.Background(), nil)
	if h.recovery.State().SignedOut {
		t.Fatal("a failure that is not a signed-out page was recorded as signed out")
	}
}

// guardedIssueSource returns a canned answer, standing in for the page readers
// the guard wraps.
type guardedIssueSource struct {
	err   error
	calls int
}

func (s *guardedIssueSource) ListIssues(context.Context, string) ([]core.Issue, error) {
	s.calls++
	return nil, s.err
}

func TestSignInGuardRecordsAndStillFailsThePoll(t *testing.T) {
	h := newRecoveryHarness(t, nil, func() (string, error) { return "lsegal", nil })
	source := &guardedIssueSource{err: &SignedOutError{URL: "https://github.com/lsegal/private/issues"}}
	guard := SignInGuard{source: source, recovery: h.recovery}
	if _, err := guard.ListIssues(context.Background(), "lsegal/private"); !errors.Is(err, ErrSignedOut) {
		t.Fatalf("guard swallowed the failure: %v", err)
	}
	if !h.recovery.State().SignedOut {
		t.Fatal("guard did not record the signed-out read")
	}
	if h.logins != 0 {
		t.Fatalf("guard opened %d sign-in window(s) on its own", h.logins)
	}
	// A read that worked ends the signed-out spell, however the profile was
	// signed in again.
	source.err = nil
	if _, err := guard.ListIssues(context.Background(), "lsegal/private"); err != nil {
		t.Fatalf("successful read reported %v", err)
	}
	if h.recovery.State().SignedOut {
		t.Fatal("a successful read left the banner up")
	}
}

// fakeHandover records the order the recovery hands the profile over in.
type fakeHandover struct {
	events     *[]string
	suspendErr error
	resumeErr  error
}

func (h fakeHandover) Suspend() error { *h.events = append(*h.events, "suspend"); return h.suspendErr }
func (h fakeHandover) Resume() error  { *h.events = append(*h.events, "resume"); return h.resumeErr }

func TestNewSignInRecoveryWiresHandoverLoginAndTimeout(t *testing.T) {
	var events, logs []string
	var gotConfig Config
	var gotTimeout time.Duration
	recovery := NewSignInRecovery(fakeHandover{events: &events}, Config{Profile: "/tmp/wired"}, SignIn{
		Login: func(_ context.Context, config Config, out io.Writer, timeout time.Duration) (string, error) {
			events = append(events, "login")
			gotConfig, gotTimeout = config, timeout
			// The flow's progress line lands in the run's log; blank chunks do not.
			_, _ = fmt.Fprint(out, "  \n")
			_, _ = fmt.Fprint(out, "waiting for the sign-in to finish\n")
			return "octocat", nil
		},
		Headed:  func() error { return nil },
		Timeout: 3 * time.Minute,
	}, func(format string, args ...interface{}) { logs = append(logs, fmt.Sprintf(format, args...)) })

	login, err := recovery.SignIn(context.Background())
	if err != nil || login != "octocat" {
		t.Fatalf("SignIn() = %q, %v; want octocat, nil", login, err)
	}
	if got := strings.Join(events, ","); got != "suspend,login,resume" {
		t.Fatalf("handover order = %s, want suspend,login,resume", got)
	}
	if gotConfig.Profile != "/tmp/wired" || gotTimeout != 3*time.Minute {
		t.Fatalf("login got profile %q timeout %v, want /tmp/wired 3m", gotConfig.Profile, gotTimeout)
	}
	progress := 0
	for _, line := range logs {
		if strings.TrimSpace(strings.TrimPrefix(line, "browser:")) == "" {
			t.Fatalf("blank progress chunk reached the log: %q", line)
		}
		if line == "browser: waiting for the sign-in to finish" {
			progress++
		}
	}
	if progress != 1 {
		t.Fatalf("progress line logged %d times, want 1: %q", progress, logs)
	}

	// The clock is the real one: a signed-out read is stamped with now.
	before := time.Now()
	recovery.attempt(context.Background(), ErrSignedOut)
	if since := recovery.State().Since; since.Before(before) || since.After(time.Now()) {
		t.Fatalf("signed-out spell stamped %v, want between %v and now", since, before)
	}
}

func TestNewSignInRecoveryReportsHandoverFailures(t *testing.T) {
	var events, logs []string
	logf := func(format string, args ...interface{}) { logs = append(logs, fmt.Sprintf(format, args...)) }
	signIn := SignIn{
		Login: func(context.Context, Config, io.Writer, time.Duration) (string, error) {
			events = append(events, "login")
			return "octocat", nil
		},
		Headed: func() error { return nil },
	}

	recovery := NewSignInRecovery(fakeHandover{events: &events, suspendErr: errors.New("chrome busy")}, Config{}, signIn, logf)
	if _, err := recovery.SignIn(context.Background()); err == nil || !strings.Contains(err.Error(), "chrome busy") {
		t.Fatalf("SignIn() error = %v, want the suspend failure", err)
	}
	if got := strings.Join(events, ","); got != "suspend" {
		t.Fatalf("a failed suspend still ran %s", got)
	}
	if !strings.Contains(recovery.State().LastError, "chrome busy") {
		t.Fatalf("banner error = %q, want the suspend failure", recovery.State().LastError)
	}

	events, logs = nil, nil
	recovery = NewSignInRecovery(fakeHandover{events: &events, resumeErr: errors.New("relaunch failed")}, Config{}, signIn, logf)
	if login, err := recovery.SignIn(context.Background()); err != nil || login != "octocat" {
		t.Fatalf("SignIn() = %q, %v; a resume failure must not fail the sign-in", login, err)
	}
	found := false
	for _, line := range logs {
		if strings.Contains(line, "could not restart the headless browser") && strings.Contains(line, "relaunch failed") {
			found = true
		}
	}
	if !found {
		t.Fatalf("resume failure not logged: %q", logs)
	}
}

func TestNewSignInRecoveryReportsAMissingWindow(t *testing.T) {
	var events []string
	recovery := NewSignInRecovery(fakeHandover{events: &events}, Config{}, SignIn{
		Headed: func() error { return errors.New("no display") },
	}, func(string, ...interface{}) {})
	if got := recovery.State().WindowUnavailable; got != "no display" {
		t.Fatalf("WindowUnavailable = %q, want no display", got)
	}
	if _, err := recovery.SignIn(context.Background()); !errors.Is(err, ErrSignInWindowUnavailable) {
		t.Fatalf("SignIn() error = %v, want ErrSignInWindowUnavailable", err)
	}
	if len(events) != 0 {
		t.Fatalf("handover ran %v on a machine with no window", events)
	}
}

func TestSignInRecoveryLogWriter(t *testing.T) {
	for _, test := range []struct {
		name  string
		chunk string
		want  []string
	}{
		{name: "empty chunk logs nothing", chunk: "", want: nil},
		{name: "whitespace chunk logs nothing", chunk: " \t\r\n", want: nil},
		{name: "text is trimmed and prefixed", chunk: "  opened the sign-in page\n", want: []string{"browser: opened the sign-in page"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var logs []string
			recovery := &SignInRecovery{logf: func(format string, args ...interface{}) { logs = append(logs, fmt.Sprintf(format, args...)) }}
			var out io.Writer = recovery.logWriter()
			n, err := out.Write([]byte(test.chunk))
			if err != nil || n != len(test.chunk) {
				t.Fatalf("Write() = %d, %v; want %d, nil", n, err, len(test.chunk))
			}
			if strings.Join(logs, "|") != strings.Join(test.want, "|") {
				t.Fatalf("logged %q, want %q", logs, test.want)
			}
		})
	}
}

// capableIssueSource implements every optional interface the guard forwards.
type capableIssueSource struct {
	guardedIssueSource
	err error
}

func (s *capableIssueSource) OriginatingWorkState(_ context.Context, repo string, number int) (core.OriginatingWorkState, error) {
	return core.OriginatingWorkState{IssueState: fmt.Sprintf("%s#%d", repo, number)}, s.err
}

func (s *capableIssueSource) StacksEnabled(_ context.Context, repo string) (bool, error) {
	return repo == "lsegal/stacked", s.err
}

func (s *capableIssueSource) HydrateIssue(_ context.Context, repo string, issue *core.Issue) error {
	issue.Title = "hydrated from " + repo
	return s.err
}

func TestSignInGuardForwardsOptionalReads(t *testing.T) {
	ctx := context.Background()
	source := &capableIssueSource{}
	guard := NewSignInGuard(source, nil)

	state, err := guard.OriginatingWorkState(ctx, "lsegal/aviary", 7)
	if err != nil || state.IssueState != "lsegal/aviary#7" {
		t.Fatalf("OriginatingWorkState() = %+v, %v; want the wrapped source's answer", state, err)
	}
	if enabled, err := guard.StacksEnabled(ctx, "lsegal/stacked"); err != nil || !enabled {
		t.Fatalf("StacksEnabled(stacked) = %v, %v; want true, nil", enabled, err)
	}
	if enabled, err := guard.StacksEnabled(ctx, "lsegal/plain"); err != nil || enabled {
		t.Fatalf("StacksEnabled(plain) = %v, %v; want false, nil", enabled, err)
	}
	issue := &core.Issue{Number: 7}
	if err := guard.HydrateIssue(ctx, "lsegal/aviary", issue); err != nil || issue.Title != "hydrated from lsegal/aviary" {
		t.Fatalf("HydrateIssue() = %v, title %q; want the wrapped source's answer", err, issue.Title)
	}

	// Errors from the wrapped source come back unchanged.
	source.err = errors.New("api down")
	if _, err := guard.OriginatingWorkState(ctx, "lsegal/aviary", 7); !errors.Is(err, source.err) {
		t.Fatalf("OriginatingWorkState() error = %v, want api down", err)
	}
	if _, err := guard.StacksEnabled(ctx, "lsegal/aviary"); !errors.Is(err, source.err) {
		t.Fatalf("StacksEnabled() error = %v, want api down", err)
	}
	if err := guard.HydrateIssue(ctx, "lsegal/aviary", issue); !errors.Is(err, source.err) {
		t.Fatalf("HydrateIssue() error = %v, want api down", err)
	}
}

func TestSignInGuardWithoutOptionalReads(t *testing.T) {
	ctx := context.Background()
	guard := NewSignInGuard(&guardedIssueSource{}, nil)

	if _, err := guard.OriginatingWorkState(ctx, "lsegal/aviary", 7); err == nil || !strings.Contains(err.Error(), "#7") || !strings.Contains(err.Error(), "cannot report work state") {
		t.Fatalf("OriginatingWorkState() error = %v, want a cannot-report error naming #7", err)
	}
	// A source with no stacks support reads as "not enabled", not a failure.
	if enabled, err := guard.StacksEnabled(ctx, "lsegal/aviary"); err != nil || enabled {
		t.Fatalf("StacksEnabled() = %v, %v; want false, nil", enabled, err)
	}
	if err := guard.HydrateIssue(ctx, "lsegal/aviary", &core.Issue{Number: 9}); err == nil || !strings.Contains(err.Error(), "#9") || !strings.Contains(err.Error(), "cannot read issues") {
		t.Fatalf("HydrateIssue() error = %v, want a cannot-read error naming #9", err)
	}
}

func TestNilSignInRecoveryIsANoOp(t *testing.T) {
	// Modes without a browser profile have no recovery; the guard and the
	// board still call through it.
	var recovery *SignInRecovery
	recovery.attempt(context.Background(), ErrSignedOut)
	recovery.clear()
	if state := recovery.State(); state != (core.GitHubSession{}) {
		t.Fatalf("nil recovery State() = %+v, want the zero session", state)
	}
}
