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

// signInBackoff is how long a run waits before reminding the log, again, that
// the profile is still signed out. A watch loop polls every few seconds, and
// the same reminder must not fill the log on every tick.
const signInBackoff = 10 * time.Minute

// signInBackoffMax caps the doubling. Past it the run keeps reminding
// occasionally, because the user can come back to the machine at any time.
const signInBackoffMax = time.Hour

// SignInRecovery handles a signed-out read without opening a window on its
// own: it records the signed-out session for the board's banner, and opens
// GitHub's sign-in page in a visible window only when the user asks (the
// banner's "Sign in to GitHub"). Polling carries on throughout, so the first
// read after a sign-in lands picks the work back up in the same run.
//
// Chrome allows one process per --user-data-dir, so signing in has to hand
// the profile over rather than share it: stop the headless browser, open a
// headed one on the same profile at GitHub's login page, wait for the sign-in,
// close it, and start the headless browser again.
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

	mu    sync.Mutex
	state core.GitHubSession
	// remind is the earliest time the signed-out reminder is logged again,
	// and backoff is how long the wait after that reminder will be.
	remind  time.Time
	backoff time.Duration
}

// LoginFunc opens a headed sign-in window on config's profile and returns the
// GitHub login it ended up signed in as. It reports the flow's one progress
// line to out.
type LoginFunc func(ctx context.Context, config Config, out io.Writer, timeout time.Duration) (string, error)

// SignIn is the headed login flow a recovery hands the profile to.
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
// else.
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

// State reports the GitHub session as the board's banner shows it.
func (r *SignInRecovery) State() core.GitHubSession {
	if r == nil {
		return core.GitHubSession{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.state
	if r.headed != nil {
		if reason := r.headed(); reason != nil {
			state.WindowUnavailable = reason.Error()
		}
	}
	return state
}

// attempt records an error a read just failed with. Errors that are not a
// signed-out diagnosis are ignored, so callers can hand it every failure
// without classifying first.
func (r *SignInRecovery) attempt(_ context.Context, err error) {
	if r == nil || err == nil || !errors.Is(err, ErrSignedOut) {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	if !r.state.SignedOut {
		r.state.SignedOut, r.state.Since = true, now
	}
	r.state.URL, r.state.Status = "", 0
	var signedOut *SignedOutError
	if errors.As(err, &signedOut) {
		r.state.URL, r.state.Status = signedOut.URL, signedOut.Status
	}
	if !r.remind.IsZero() && now.Before(r.remind) {
		// Already said recently: a signed-out profile must not fill the log
		// with the same line every few seconds.
		return
	}
	switch {
	case r.backoff == 0:
		r.backoff = signInBackoff
	case r.backoff < signInBackoffMax:
		r.backoff = min(2*r.backoff, signInBackoffMax)
	}
	r.remind = now.Add(r.backoff)
	r.logf("%s", signInAnnouncement(err))
}

// clear records a read that worked, which ends a signed-out spell however the
// profile came to be signed in again.
func (r *SignInRecovery) clear() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state.SignedOut {
		r.logf("browser: GitHub reads are signed in again")
	}
	r.state = core.GitHubSession{SigningIn: r.state.SigningIn}
	r.remind, r.backoff = time.Time{}, 0
}

// ErrSignInWindowUnavailable reports a machine where no sign-in window can
// appear, such as a container or a server with no display.
var ErrSignInWindowUnavailable = errors.New("no window can appear here to sign in to GitHub")

// ErrSigningIn reports a sign-in window that is already open.
var ErrSigningIn = errors.New("a GitHub sign-in window is already open")

// SignIn opens GitHub's sign-in page in a visible window on the run's
// profile, waits for the sign-in, and hands the profile back to the headless
// browser. It is what the banner's "Sign in to GitHub" does. Where no window
// can appear it fails with ErrSignInWindowUnavailable, naming the two ways
// forward: gh polling, or importing a profile signed in elsewhere.
func (r *SignInRecovery) SignIn(ctx context.Context) (string, error) {
	if reason := r.headed(); reason != nil {
		err := fmt.Errorf("%w: %v. Switch to gh polling, or import an Aviary browser profile that is signed in to GitHub on another machine", ErrSignInWindowUnavailable, reason)
		r.fail(err)
		return "", err
	}
	r.mu.Lock()
	if r.state.SigningIn {
		r.mu.Unlock()
		return "", ErrSigningIn
	}
	r.state.SigningIn, r.state.LastError = true, ""
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.state.SigningIn = false
		r.mu.Unlock()
	}()

	if err := r.suspend(); err != nil {
		err = fmt.Errorf("stop the headless browser to open a sign-in window: %w", err)
		r.fail(err)
		return "", err
	}
	login, loginErr := r.login(ctx, r.config, r.logWriter(), r.timeout)
	// The headless browser is started again whichever way the sign-in went:
	// a run whose sign-in was abandoned still has to keep polling.
	if err := r.resume(); err != nil {
		r.logf("browser: could not restart the headless browser after the sign-in window closed: %v", err)
	}
	if loginErr != nil {
		err := fmt.Errorf("sign-in did not complete: %w", loginErr)
		r.fail(err)
		return "", err
	}
	r.clear()
	r.logf("browser: signed in to GitHub as %s; the profile is reused by later polls and later runs", login)
	return login, nil
}

// fail records why the last sign-in did not work, for the banner to show.
func (r *SignInRecovery) fail(err error) {
	r.mu.Lock()
	r.state.LastError = err.Error()
	r.mu.Unlock()
	r.logf("browser: %v", err)
}

// signInAnnouncement is the line logged for a signed-out read. It names the
// status when GitHub's own status is what gave the session away, so the log
// says what Aviary saw rather than only what it decided.
func signInAnnouncement(err error) string {
	const next = "sign in to GitHub in Aviary's browser from the Projects board, or switch to gh polling"
	var signedOut *SignedOutError
	if errors.As(err, &signedOut) {
		if signedOut.Status != 0 {
			return fmt.Sprintf("browser: GitHub returned %d for %s to a signed-out session — %s", signedOut.Status, signedOut.URL, next)
		}
		return fmt.Sprintf("browser: GitHub served a signed-out page for %s — %s", signedOut.URL, next)
	}
	return "browser: Aviary's browser is signed out of GitHub — " + next
}

// SignInGuard wraps browser mode's issue source so a read that came back
// signed out is recorded for the board's banner, and a read that worked clears
// it. The error is still returned: the poll that hit a signed-out page
// genuinely failed, and is never reported as "no issues".
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
	} else {
		g.recovery.clear()
	}
	return issues, err
}
