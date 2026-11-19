package browser

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

// signInBackoff is how long a run waits before offering to sign in again
// after an attempt that did not end signed in. A watch loop polls every few
// seconds, and a login window that was declined, timed out, or could never
// appear must not be re-opened on the next tick.
const signInBackoff = 10 * time.Minute

// signInBackoffMax caps the doubling. Past it the run keeps offering
// occasionally rather than giving up for good, because the thing that would fix
// it -- the user coming back to the machine -- can happen at any time.
const signInBackoffMax = time.Hour

// SignInRecovery turns a signed-out read into a login window. The poll
// that read the signed-out page has already failed by the time this runs; the
// run stays alive either way, and the next tick reads the same page with a
// profile that is signed in.
//
// Chrome allows one process per --user-data-dir, so the flow has to hand the
// profile over rather than share it: stop the headless browser, open a headed
// one on the same profile at GitHub's login page, wait for the sign-in, close
// it, and start the headless browser again.
type SignInRecovery struct {
	config  Config
	logf    func(string, ...interface{})
	timeout time.Duration

	// suspend and resume hand the profile to the login window and take it back.
	suspend func() error
	resume  func() error
	// login runs the headed flow. It is a field so tests can exercise the
	// recovery without a browser and without opening a window.
	login LoginFunc
	// headed reports whether a window could appear on this machine at all.
	headed func() error
	now    func() time.Time

	mu sync.Mutex
	// next is the earliest time another attempt may be made, and backoff is
	// how long the wait after the next failure will be.
	next    time.Time
	backoff time.Duration
}

// LoginFunc opens a headed sign-in window on config's profile and returns the
// GitHub login it ended up signed in as. It reports the flow's one progress
// line to out.
type LoginFunc func(ctx context.Context, config Config, out io.Writer, timeout time.Duration) (string, error)

// SignIn is the headed login flow a recovery hands the profile to. The root
// package supplies `Aviary auth`'s own, so the window a signed-out poll opens
// is the one the user would have opened themselves.
type SignIn struct {
	// Login opens the window and waits for the sign-in.
	Login LoginFunc
	// Headed reports whether a window could appear on this machine at all,
	// naming what is missing when one could not.
	Headed func() error
	// Timeout bounds the wait for a sign-in to finish.
	Timeout time.Duration
}

// NewSignInRecovery builds the recovery for a run's browser.
func NewSignInRecovery(browser *Browser, config Config, signIn SignIn, logf func(string, ...interface{})) *SignInRecovery {
	return &SignInRecovery{
		config:  config,
		logf:    logf,
		timeout: signIn.Timeout,
		suspend: browser.Suspend,
		resume:  browser.Resume,
		login:   signIn.Login,
		headed:  signIn.Headed,
		now:     time.Now,
	}
}

// writerFunc adapts a log function to the io.Writer the auth flow writes its
// one progress line to, so that line lands in the run's log with everything
// else instead of on a stdout the terminal UI has taken over.
type writerFunc func(p []byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// logWriter returns a writer that forwards each written chunk to logf.
func (r *SignInRecovery) logWriter() writerFunc {
	return func(p []byte) (int, error) {
		if line := strings.TrimSpace(string(p)); line != "" {
			r.logf("browser: %s", line)
		}
		return len(p), nil
	}
}

// attempt runs the recovery for an error a read just failed with. Errors that
// are not a signed-out diagnosis are ignored, so callers can hand it every
// failure without classifying first.
func (r *SignInRecovery) attempt(ctx context.Context, err error) {
	if r == nil || err == nil || !errors.Is(err, ErrSignedOut) {
		return
	}
	if !r.claim() {
		// Already offered recently. The poll's own failure is logged by the
		// run loop, so staying silent here is what keeps a signed-out profile
		// from filling the log with the same offer every few seconds.
		return
	}
	r.logf("%s", signInAnnouncement(err))
	if reason := r.headed(); reason != nil {
		r.logf("browser: %v", reason)
		r.penalize()
		return
	}
	if err := r.suspend(); err != nil {
		r.logf("browser: could not stop the headless browser to open a sign-in window: %v", err)
		r.penalize()
		return
	}
	login, loginErr := r.login(ctx, r.config, r.logWriter(), r.timeout)
	// The headless browser is started again whichever way the sign-in went: a
	// run that gave up on the login still has to keep polling.
	if err := r.resume(); err != nil {
		r.logf("browser: could not restart the headless browser after the sign-in window closed: %v", err)
	}
	if loginErr != nil {
		r.logf("browser: sign-in did not complete: %v", loginErr)
		r.penalize()
		return
	}
	r.succeed()
	r.logf("browser: signed in to GitHub as %s; the profile is reused by later polls and later runs", login)
}

// signInAnnouncement is the line logged before a window is opened. It names the
// status when GitHub's own status is what gave the session away, so the log
// says what Aviary saw rather than only what it decided.
func signInAnnouncement(err error) string {
	var signedOut *SignedOutError
	if errors.As(err, &signedOut) {
		if signedOut.Status != 0 {
			return fmt.Sprintf("browser: GitHub returned %d for %s — opening a browser window to sign in", signedOut.Status, signedOut.URL)
		}
		return fmt.Sprintf("browser: GitHub served a signed-out page for %s — opening a browser window to sign in", signedOut.URL)
	}
	return "browser: the browser profile is signed out of GitHub — opening a browser window to sign in"
}

// claim reports whether an attempt may run now, and records that one is being
// made. It is the whole of the "do not re-prompt every tick" rule.
func (r *SignInRecovery) claim() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.next.IsZero() && r.now().Before(r.next) {
		return false
	}
	// Held until the attempt finishes, so a second target failing the same
	// tick does not open a second window; a successful sign-in clears it.
	r.next = r.now().Add(signInBackoffMax)
	return true
}

// penalize schedules the next offer after an attempt that did not sign in,
// doubling the wait each time up to the cap.
func (r *SignInRecovery) penalize() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.backoff == 0 {
		r.backoff = signInBackoff
	} else if r.backoff < signInBackoffMax {
		r.backoff *= 2
		if r.backoff > signInBackoffMax {
			r.backoff = signInBackoffMax
		}
	}
	r.next = r.now().Add(r.backoff)
	r.logf("browser: not offering to sign in again for %s; run `aviary auth` to sign the profile in at any time", r.backoff)
}

// succeed clears the back-off after a sign-in that worked, so a session that
// expires later is recovered immediately rather than after the old wait.
func (r *SignInRecovery) succeed() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next, r.backoff = time.Time{}, 0
}

// SignInGuard wraps browser mode's issue source so a read that came back
// signed out opens a login window before the next tick asks again. The error is
// still returned: the poll that hit a signed-out page genuinely failed, and the
// run loop's own retry is what makes the recovered profile take effect.
type SignInGuard struct {
	source   core.IssueSource
	recovery *SignInRecovery
}

// NewSignInGuard wraps source so a read that came back signed out triggers
// recovery before the next tick asks again.
func NewSignInGuard(source core.IssueSource, recovery *SignInRecovery) SignInGuard {
	return SignInGuard{source: source, recovery: recovery}
}

// OriginatingWorkState passes the active-work check through to the wrapped
// source. The guard exists to recover a signed-out *page* read; this check is
// an API read that cannot come back signed out, so it is forwarded untouched
// rather than being lost because the guard does not implement it (issue #469).
func (g SignInGuard) OriginatingWorkState(ctx context.Context, repo string, number int) (core.OriginatingWorkState, error) {
	checker, ok := g.source.(core.WorkClosureChecker)
	if !ok {
		return core.OriginatingWorkState{}, fmt.Errorf("read issue #%d state: %T cannot report work state", number, g.source)
	}
	return checker.OriginatingWorkState(ctx, repo, number)
}

// StacksEnabled passes the stacked pull request check through to the wrapped
// source for the same reason OriginatingWorkState does (issue #657).
func (g SignInGuard) StacksEnabled(ctx context.Context, repo string) (bool, error) {
	stacks, ok := g.source.(core.StackSource)
	if !ok {
		return false, nil
	}
	return stacks.StacksEnabled(ctx, repo)
}

// HydrateIssue passes the paused watch's issue read through to the wrapped
// source for the same reason OriginatingWorkState does (issue #769).
func (g SignInGuard) HydrateIssue(ctx context.Context, repo string, issue *core.Issue) error {
	hydrator, ok := g.source.(IssueHydrator)
	if !ok {
		return fmt.Errorf("read issue #%d: %T cannot read issues", issue.Number, g.source)
	}
	return hydrator.HydrateIssue(ctx, repo, issue)
}

func (g SignInGuard) ListIssues(ctx context.Context, target string) ([]core.Issue, error) {
	issues, err := g.source.ListIssues(ctx, target)
	if err != nil {
		g.recovery.attempt(ctx, err)
	}
	return issues, err
}
