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
	if !strings.Contains(err.Error(), "aviary auth") || !strings.Contains(err.Error(), "/tmp/profile") {
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

func TestSignInRecoveryOpensWindowAndResumes(t *testing.T) {
	h := newRecoveryHarness(t, nil, func() (string, error) { return "lsegal", nil })
	h.recovery.attempt(context.Background(), &SignedOutError{URL: "https://github.com/lsegal/private/issues", Profile: "/tmp/profile", Status: 404})
	if h.suspended != 1 || h.resumed != 1 || h.logins != 1 {
		t.Fatalf("suspended %d, resumed %d, logins %d; want one of each", h.suspended, h.resumed, h.logins)
	}
	if !h.logged("GitHub returned 404 for https://github.com/lsegal/private/issues — opening a browser window to sign in") {
		t.Fatalf("recovery did not announce the 404 it recovered from: %v", h.logs)
	}
	if h.logged("wrong profile") {
		t.Fatalf("recovery signed in on the wrong profile: %v", h.logs)
	}
	if !h.logged("signed in to GitHub as lsegal") {
		t.Fatalf("recovery did not report the account it signed in as: %v", h.logs)
	}
	// A successful sign-in clears the back-off, so a session that expires later
	// is recovered at once rather than after a wait it did not earn.
	if !h.recovery.next.IsZero() {
		t.Fatalf("a successful sign-in left a back-off of until %v", h.recovery.next)
	}
}

func TestSignInRecoveryBacksOffAfterFailedLogin(t *testing.T) {
	h := newRecoveryHarness(t, nil, func() (string, error) { return "", errors.New("timed out") })
	signedOut := &SignedOutError{URL: "https://github.com/lsegal/private/issues", Profile: "/tmp/profile"}
	h.recovery.attempt(context.Background(), signedOut)
	if h.logins != 1 {
		t.Fatalf("first attempt made %d login(s), want 1", h.logins)
	}
	if h.resumed != 1 {
		t.Fatalf("a failed sign-in left the headless browser stopped (%d resume(s))", h.resumed)
	}
	// The next several ticks must not re-open a window.
	for tick := 0; tick < 5; tick++ {
		h.now = h.now.Add(time.Minute)
		h.recovery.attempt(context.Background(), signedOut)
	}
	if h.logins != 1 {
		t.Fatalf("re-prompted during the back-off: %d login(s)", h.logins)
	}
	// Past the back-off it tries again, and the wait doubles after the second
	// failure rather than staying where it was.
	h.now = h.now.Add(signInBackoff)
	h.recovery.attempt(context.Background(), signedOut)
	if h.logins != 2 {
		t.Fatalf("did not retry after the back-off elapsed: %d login(s)", h.logins)
	}
	if want := h.now.Add(2 * signInBackoff); !h.recovery.next.Equal(want) {
		t.Fatalf("next attempt at %v, want the doubled back-off at %v", h.recovery.next, want)
	}
}

func TestSignInRecoveryRefusesWithoutADisplay(t *testing.T) {
	h := newRecoveryHarness(t, errors.New("no display server (DISPLAY and WAYLAND_DISPLAY are both unset)"), func() (string, error) {
		t.Fatal("opened a login window on a machine with no display server")
		return "", nil
	})
	signedOut := &SignedOutError{URL: "https://github.com/lsegal/private/issues", Profile: "/tmp/profile"}
	h.recovery.attempt(context.Background(), signedOut)
	if h.suspended != 0 {
		t.Fatalf("stopped the headless browser for a window that can never appear (%d suspend(s))", h.suspended)
	}
	if !h.logged("no display server") || !h.logged("aviary auth") {
		t.Fatalf("refusal did not explain itself or point at `aviary auth`: %v", h.logs)
	}
	h.now = h.now.Add(time.Minute)
	h.recovery.attempt(context.Background(), signedOut)
	if h.logins != 0 {
		t.Fatalf("attempted a login after refusing: %d login(s)", h.logins)
	}
}

func TestSignInRecoveryIgnoresOtherFailures(t *testing.T) {
	h := newRecoveryHarness(t, nil, func() (string, error) { return "lsegal", nil })
	h.recovery.attempt(context.Background(), errors.New("load issue list: GitHub returned HTTP 500"))
	h.recovery.attempt(context.Background(), nil)
	if h.logins != 0 || h.suspended != 0 {
		t.Fatalf("a failure that is not a signed-out page opened a window: %d login(s), %d suspend(s)", h.logins, h.suspended)
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

func TestSignInGuardRecoversAndStillFailsThePoll(t *testing.T) {
	h := newRecoveryHarness(t, nil, func() (string, error) { return "lsegal", nil })
	source := &guardedIssueSource{err: &SignedOutError{URL: "https://github.com/lsegal/private/issues"}}
	guard := SignInGuard{source: source, recovery: h.recovery}
	if _, err := guard.ListIssues(context.Background(), "lsegal/private"); !errors.Is(err, ErrSignedOut) {
		t.Fatalf("guard swallowed the failure: %v", err)
	}
	if h.logins != 1 {
		t.Fatalf("guard made %d login attempt(s), want 1", h.logins)
	}
	// A read that worked never reaches the recovery at all.
	source.err = nil
	if _, err := guard.ListIssues(context.Background(), "lsegal/private"); err != nil {
		t.Fatalf("successful read reported %v", err)
	}
	if h.logins != 1 {
		t.Fatalf("a successful read triggered a login: %d attempt(s)", h.logins)
	}
}
