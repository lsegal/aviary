package projects

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/projects/browser"
)

// The ways Projects reads GitHub. Browser is the default: it reads the
// rendered pages in Aviary's own browser, signed in as the user, which costs
// no API rate limit. Webhook and poll are the API-backed modes.
const (
	DeliveryBrowser = "browser"
	DeliveryWebhook = "webhook"
	DeliveryPoll    = "poll"
)

// BrowserInterval is how often browser delivery reloads its pages by default.
const BrowserInterval = browser.DefaultWatchInterval

const (
	// githubHomeURL is loaded to read a profile's sign-in state. Every page
	// GitHub renders for a signed-in session carries the account's login.
	githubHomeURL = "https://github.com/"
	// githubLoginURL is where the sign-in window starts, so the user lands on
	// the sign-in form instead of having to find it.
	githubLoginURL = "https://github.com/login"
	// signInTimeout bounds the wait for a sign-in to finish. Past it the
	// window is closed rather than held open forever.
	signInTimeout = 5 * time.Minute
	// browserTabIdleFloor is the shortest a tab is left open unread, so a
	// short interval does not close and reopen tabs between reads.
	browserTabIdleFloor = time.Minute
)

// signInPollInterval is how often the open sign-in window is re-read while
// waiting for the sign-in. It is a variable so tests need not spend it.
var signInPollInterval = 2 * time.Second

// githubLoginJS reads the signed-in user's login out of the current page, or
// an empty string when the page was served to a signed-out session.
const githubLoginJS = `(() => {
  const meta = document.querySelector('meta[name="user-login"]');
  return meta ? (meta.getAttribute('content') || '') : '';
})()`

// BrowserOptions decide which browser browser delivery drives and how.
type BrowserOptions struct {
	// Binary and Profile come from Aviary's browser settings, so Projects
	// reads GitHub through the same browser and profile as Aviary itself.
	Binary  string
	Profile string
	// Vision enables the opt-in, capped screenshot fallback for pages the
	// DOM extractor stops recognising.
	Vision bool
	// Headed drives a visible browser instead of a headless one, so the pages
	// being read can be watched. It is a debugging switch.
	Headed bool
}

// BrowserOptionsFrom builds the options from Aviary's browser settings.
func BrowserOptionsFrom(settings config.BrowserConfig) BrowserOptions {
	return BrowserOptions{Binary: settings.Binary, Profile: settings.ProfileDir}
}

// Config converts the options into the launcher's configuration.
func (o BrowserOptions) Config() browser.Config {
	return browser.Config{Binary: o.Binary, Profile: o.Profile, Headed: o.Headed}
}

// ResolveDelivery validates a delivery mode and settles the poll interval it
// runs at. An empty mode is browser delivery, and browser delivery reloads
// every BrowserInterval unless an interval was set explicitly.
func ResolveDelivery(mode string, interval time.Duration, intervalSet bool, options BrowserOptions) (string, time.Duration, error) {
	if mode == "" {
		mode = DeliveryBrowser
	}
	switch mode {
	case DeliveryBrowser:
	case DeliveryWebhook, DeliveryPoll:
		if options.Vision {
			return mode, interval, fmt.Errorf("the screenshot fallback only applies to browser delivery, not %s", mode)
		}
		if options.Headed {
			return mode, interval, fmt.Errorf("a visible browser only applies to browser delivery, not %s", mode)
		}
		return mode, interval, nil
	default:
		return mode, interval, fmt.Errorf("delivery must be browser, webhook, or poll, got %q", mode)
	}
	if options.Headed {
		if err := headedEnvironmentCheck(); err != nil {
			return mode, interval, fmt.Errorf("a visible browser needs a window: %w", err)
		}
	}
	if !intervalSet {
		interval = BrowserInterval
	}
	return mode, interval, nil
}

// browserTabIdleTimeout is how long a tab may go unread before it is closed.
// Every watched page is re-read once per tick, so missing several ticks in a
// row is what tells a page apart from one nothing reads any more.
func browserTabIdleTimeout(interval time.Duration) time.Duration {
	return max(3*interval, browserTabIdleFloor)
}

// ApplyBrowserSources swaps the engine's GitHub reads for their browser-backed
// equivalents: issue lists from the rendered issues page, boards from the
// rendered Projects page with the board's own Status column, and the comments
// the handoff reads from the rendered conversation. Writes (comments, status
// moves) and Discussions stay on gh.
//
// Newly seen candidates are hydrated with their body and dependency state
// through gh, once per issue for the run. A signed-out read is recorded for
// the board's banner instead of opening a window, and the returned recovery
// is what the banner's "Sign in to GitHub" calls.
func ApplyBrowserSources(w *Engine, driver *browser.Browser, options BrowserOptions, gh GHCLI) *browser.SignInRecovery {
	if w == nil || driver == nil {
		return nil
	}
	// One queue paces every page load, so a tick that reloads many targets
	// trickles its requests out instead of firing them all at once.
	driver.SetLoadQueue(browser.NewLoadQueue(w.Interval, w.logf))
	driver.SetTabIdleTimeout(browserTabIdleTimeout(w.Interval), w.logf)
	var vision *browser.Vision
	if options.Vision {
		runner, _ := w.Runner.(CommandRunner)
		vision = browser.NewVision(visionAgentRunner{runner: runner}, w.logf)
	}
	repos := browser.NewIssueSource(driver, gh, w.issueHandled, gh.Filter, gh.AllIssues, vision, w.logf)
	board := browser.NewBoard(driver, gh.Filter, gh.AllIssues, vision, repos.Hydration)
	recovery := browser.NewSignInRecovery(driver, options.Config(), SignInFlow(), w.logf)
	w.Issues = browser.NewSignInGuard(browser.WatchIssues{Repos: repos, Board: board, Work: gh}, recovery)
	w.Projects = board
	// After the machine sleeps, dead tabs are replaced and an unresponsive
	// browser is restarted when the run wakes, not on the read that finds it.
	w.Recover = func(context.Context) { driver.Recover() }
	w.GitHubSession = recovery.State
	if w.Comments != nil {
		w.Comments = browser.NewCommentSource(driver, w.Comments, w.logf)
	}
	return recovery
}

// SignInFlow is the visible sign-in window a signed-out run opens when the
// user asks.
func SignInFlow() browser.SignIn {
	return browser.SignIn{Login: GitHubSignIn, Headed: headedEnvironmentCheck, Timeout: signInTimeout}
}

// signInSession is a browser opened on the profile and the tab to drive it
// through.
type signInSession struct {
	page  browser.Page
	close func()
}

// openSignInSession launches a browser on the configured profile. It is a
// variable so tests can exercise the flow without a browser.
var openSignInSession = func(ctx context.Context, config browser.Config) (*signInSession, error) {
	driver, err := browser.Start(ctx, config)
	if err != nil {
		return nil, err
	}
	page, err := driver.Tab("signin")
	if err != nil {
		_ = driver.Close()
		return nil, err
	}
	return &signInSession{page: page, close: func() { _ = driver.Close() }}, nil
}

// readGitHubLogin reports the login the current page is rendered for, or an
// empty string when the profile is signed out.
func readGitHubLogin(page browser.Page) (string, error) {
	var login string
	if err := page.Eval(githubLoginJS, &login); err != nil {
		return "", fmt.Errorf("read GitHub sign-in state: %w", err)
	}
	return strings.TrimSpace(login), nil
}

// visitGitHubLogin navigates to a GitHub URL and reads the sign-in state off
// the page it lands on.
func visitGitHubLogin(page browser.Page, url string) (string, error) {
	if err := page.Navigate(url); err != nil {
		return "", fmt.Errorf("open %s: %w", url, err)
	}
	return readGitHubLogin(page)
}

// describeProfile names the profile directory being signed in.
func describeProfile(override string) string {
	dir, err := browser.ProfileDir(override)
	if err != nil {
		return "Aviary's browser profile"
	}
	return dir
}

// GitHubSignedIn reports the GitHub login Aviary's browser profile is signed
// in as, or an empty string when it is signed out. It reads headlessly, so
// asking never puts a window on screen.
func GitHubSignedIn(ctx context.Context, config browser.Config) (string, error) {
	config.Headed = false
	session, err := openSignInSession(ctx, config)
	if err != nil {
		return "", err
	}
	defer session.close()
	return visitGitHubLogin(session.page, githubHomeURL)
}

// GitHubSignIn opens a visible window on the profile at GitHub's sign-in page
// and waits until the profile is signed in, returning the login.
func GitHubSignIn(ctx context.Context, config browser.Config, out io.Writer, timeout time.Duration) (string, error) {
	if err := headedEnvironmentCheck(); err != nil {
		return "", err
	}
	config.Headed = true
	session, err := openSignInSession(ctx, config)
	if err != nil {
		return "", err
	}
	defer session.close()
	login, err := visitGitHubLogin(session.page, githubLoginURL)
	if err != nil {
		return "", err
	}
	if login != "" {
		return login, nil
	}
	_, _ = fmt.Fprintf(out, "Opened a browser window on %s. Sign in to GitHub there and Aviary will close it.\n", describeProfile(config.Profile))
	return waitForGitHubLogin(ctx, session.page, timeout)
}

// waitForGitHubLogin re-reads the open window until it reports a signed-in
// session or the deadline passes.
func waitForGitHubLogin(ctx context.Context, page browser.Page, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(signInPollInterval)
	defer ticker.Stop()
	var last error
	for {
		select {
		case <-ctx.Done():
			if last != nil {
				return "", fmt.Errorf("gave up after %s waiting for the GitHub sign-in to finish: %w", timeout, last)
			}
			return "", fmt.Errorf("gave up after %s waiting for the GitHub sign-in to finish", timeout)
		case <-ticker.C:
		}
		login, err := readGitHubLogin(page)
		if err != nil {
			// The window navigates repeatedly during a sign-in, so a read
			// that lands mid-navigation fails harmlessly.
			last = err
			continue
		}
		if login != "" {
			return login, nil
		}
	}
}

// openBrowserWindow opens a visible window and blocks until it closes. It is
// a variable so tests can exercise the command without opening one.
var openBrowserWindow = browser.OpenWindow

// OpenBrowserWindow opens Aviary's browser visibly on its profile, with
// nothing attached, so its settings (and its GitHub sign-in) can be changed.
// It blocks until the window is closed.
func OpenBrowserWindow(ctx context.Context, config browser.Config, out io.Writer) error {
	if err := headedEnvironmentCheck(); err != nil {
		return fmt.Errorf("cannot open a browser window: %w", err)
	}
	config.Headed = true
	_, _ = fmt.Fprintf(out, "Opening a browser window on %s. Close it when you are done; your changes stay on the profile.\n", describeProfile(config.Profile))
	return openBrowserWindow(ctx, config)
}

// ImportBrowserProfile copies the GitHub sign-in saved in a profile signed in
// elsewhere into the configured profile, for machines where no sign-in window
// can appear.
func ImportBrowserProfile(from string, config browser.Config) error {
	profile, err := browser.ProfileDir(config.Profile)
	if err != nil {
		return err
	}
	if err := browser.ImportSession(from, profile); err != nil {
		if errors.Is(err, browser.ErrNoSavedSession) {
			return fmt.Errorf("%w. Sign in to GitHub in Aviary's browser on a machine with a display first, then import that profile", err)
		}
		return err
	}
	return nil
}

// headedEnvironmentCheck reports whether this machine can put a window on
// screen. It is a variable so tests do not depend on the runner's display.
var headedEnvironmentCheck = func() error { return checkHeadedEnvironment(runtime.GOOS, os.Getenv) }

// displayServerAvailable reports whether this machine can put a window on
// screen at all. On Linux that needs a display server; macOS and Windows
// always have one.
func displayServerAvailable(goos string, getenv func(string) string) bool {
	return goos != "linux" || getenv("DISPLAY") != "" || getenv("WAYLAND_DISPLAY") != ""
}

// checkHeadedEnvironment refuses a window where none could appear, such as
// in Docker or on a server with no display.
func checkHeadedEnvironment(goos string, getenv func(string) string) error {
	if displayServerAvailable(goos, getenv) {
		return nil
	}
	return errors.New("no display server (DISPLAY and WAYLAND_DISPLAY are both unset)")
}
