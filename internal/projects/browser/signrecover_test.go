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
