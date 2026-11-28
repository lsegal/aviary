package projects

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/projects/browser"
)

func TestResolveDeliveryDefaultsToBrowser(t *testing.T) {
	mode, interval, err := ResolveDelivery("", 5*time.Minute, false, BrowserOptions{})
	if err != nil || mode != DeliveryBrowser || interval != 20*time.Second {
		t.Fatalf("ResolveDelivery(\"\") = %q, %s, %v; want browser every 20s", mode, interval, err)
	}
	if _, interval, _ := ResolveDelivery(DeliveryBrowser, time.Minute, true, BrowserOptions{}); interval != time.Minute {
		t.Fatalf("an explicit interval became %s", interval)
	}
	for _, mode := range []string{DeliveryWebhook, DeliveryPoll} {
		got, interval, err := ResolveDelivery(mode, time.Minute, false, BrowserOptions{})
		if err != nil || got != mode || interval != time.Minute {
			t.Fatalf("ResolveDelivery(%q) = %q, %s, %v", mode, got, interval, err)
		}
		if _, _, err := ResolveDelivery(mode, time.Minute, false, BrowserOptions{Vision: true}); err == nil {
			t.Fatalf("%s accepted the screenshot fallback", mode)
		}
		if _, _, err := ResolveDelivery(mode, time.Minute, false, BrowserOptions{Headed: true}); err == nil {
			t.Fatalf("%s accepted a visible browser", mode)
		}
	}
	if _, _, err := ResolveDelivery("rss", time.Minute, false, BrowserOptions{}); err == nil {
		t.Fatal("an unknown delivery mode was accepted")
	}
}

func TestResolveDeliveryVisibleBrowserNeedsADisplay(t *testing.T) {
	previous := headedEnvironmentCheck
	t.Cleanup(func() { headedEnvironmentCheck = previous })
	headedEnvironmentCheck = func() error { return errors.New("no display server") }
	if _, _, err := ResolveDelivery(DeliveryBrowser, 0, false, BrowserOptions{Headed: true}); err == nil || !strings.Contains(err.Error(), "no display server") {
		t.Fatalf("visible browser without a display: %v", err)
	}
	if _, _, err := ResolveDelivery(DeliveryBrowser, 0, false, BrowserOptions{}); err != nil {
		t.Fatalf("headless browser delivery refused without a display: %v", err)
	}
}

func TestBrowserOptionsFromAviarySettings(t *testing.T) {
	options := BrowserOptionsFrom(config.BrowserConfig{Binary: "/opt/chrome", ProfileDir: "/data/profile"})
	cfg := options.Config()
	if cfg.Binary != "/opt/chrome" || cfg.Profile != "/data/profile" || cfg.Headed {
		t.Fatalf("Config() = %+v", cfg)
	}
}

func TestBrowserTabIdleTimeout(t *testing.T) {
	if got := browserTabIdleTimeout(5 * time.Second); got != time.Minute {
		t.Fatalf("idle timeout %s, want the one-minute floor", got)
	}
	if got := browserTabIdleTimeout(time.Minute); got != 3*time.Minute {
		t.Fatalf("idle timeout %s, want three intervals", got)
	}
}

func TestCheckHeadedEnvironment(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(k string) string { return vars[k] }
	}
	if err := checkHeadedEnvironment("linux", env(nil)); err == nil {
		t.Fatal("linux without a display server reported a window was possible")
	}
	if err := checkHeadedEnvironment("linux", env(map[string]string{"WAYLAND_DISPLAY": "wayland-0"})); err != nil {
		t.Fatalf("wayland: %v", err)
	}
	for _, goos := range []string{"darwin", "windows"} {
		if err := checkHeadedEnvironment(goos, env(nil)); err != nil {
			t.Fatalf("%s: %v", goos, err)
		}
	}
}

// loginPage is a page whose sign-in state changes after a number of reads,
// standing in for a window the user signs in through.
type loginPage struct {
	login      string
	afterReads int
	reads      int
	navigated  []string
}

func (p *loginPage) Navigate(url string) error { p.navigated = append(p.navigated, url); return nil }
func (p *loginPage) Reload() error             { return nil }
func (p *loginPage) HTTPStatus() int           { return 200 }
func (p *loginPage) Eval(_ string, out any) error {
	p.reads++
	login := ""
	if p.reads > p.afterReads {
		login = p.login
	}
	data, _ := json.Marshal(login)
	return json.Unmarshal(data, out)
}

func fakeSignInSession(t *testing.T, page *loginPage) *[]browser.Config {
	t.Helper()
	var opened []browser.Config
	previousOpen, previousHeaded, previousPoll := openSignInSession, headedEnvironmentCheck, signInPollInterval
	t.Cleanup(func() {
		openSignInSession, headedEnvironmentCheck, signInPollInterval = previousOpen, previousHeaded, previousPoll
	})
	openSignInSession = func(_ context.Context, config browser.Config) (*signInSession, error) {
		opened = append(opened, config)
		return &signInSession{page: page, close: func() {
			// Nothing to release: the fake page owns no browser.
		}}, nil
	}
	headedEnvironmentCheck = func() error { return nil }
	signInPollInterval = time.Millisecond
	return &opened
}

func TestGitHubSignInWaitsForTheSignIn(t *testing.T) {
	page := &loginPage{login: "octocat", afterReads: 3}
	opened := fakeSignInSession(t, page)
	var out bytes.Buffer
	login, err := GitHubSignIn(context.Background(), browser.Config{Profile: "/data/profile"}, &out, time.Second)
	if err != nil || login != "octocat" {
		t.Fatalf("GitHubSignIn = %q, %v", login, err)
	}
	if len(*opened) != 1 || !(*opened)[0].Headed || (*opened)[0].Profile != "/data/profile" {
		t.Fatalf("opened %+v, want one visible window on the profile", *opened)
	}
	if len(page.navigated) != 1 || page.navigated[0] != githubLoginURL {
		t.Fatalf("navigated %v, want GitHub's sign-in page", page.navigated)
	}
	if !strings.Contains(out.String(), "Sign in to GitHub there") {
		t.Fatalf("progress line %q", out.String())
	}
}

func TestGitHubSignInGivesUpAtTheDeadline(t *testing.T) {
	fakeSignInSession(t, &loginPage{})
	_, err := GitHubSignIn(context.Background(), browser.Config{}, &bytes.Buffer{}, 20*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "gave up") {
		t.Fatalf("GitHubSignIn without a sign-in = %v", err)
	}
}

func TestGitHubSignInRefusesWithoutADisplay(t *testing.T) {
	opened := fakeSignInSession(t, &loginPage{})
	headedEnvironmentCheck = func() error { return errors.New("no display server") }
	if _, err := GitHubSignIn(context.Background(), browser.Config{}, &bytes.Buffer{}, time.Second); err == nil {
		t.Fatal("opened a sign-in window with no display")
	}
	if len(*opened) != 0 {
		t.Fatal("launched a browser for a window that can never appear")
	}
}

func TestGitHubSignedInReadsHeadlessly(t *testing.T) {
	opened := fakeSignInSession(t, &loginPage{login: "octocat"})
	login, err := GitHubSignedIn(context.Background(), browser.Config{Headed: true})
	if err != nil || login != "octocat" {
		t.Fatalf("GitHubSignedIn = %q, %v", login, err)
	}
	if (*opened)[0].Headed {
		t.Fatal("checking the session put a window on screen")
	}
}

func TestOpenBrowserWindowIsVisible(t *testing.T) {
	previousOpen, previousHeaded := openBrowserWindow, headedEnvironmentCheck
	t.Cleanup(func() { openBrowserWindow, headedEnvironmentCheck = previousOpen, previousHeaded })
	var got browser.Config
	openBrowserWindow = func(_ context.Context, config browser.Config) error { got = config; return nil }
	headedEnvironmentCheck = func() error { return nil }
	if err := OpenBrowserWindow(context.Background(), browser.Config{Profile: "/data/profile"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !got.Headed || got.Profile != "/data/profile" {
		t.Fatalf("opened %+v, want a visible window on the profile", got)
	}
	headedEnvironmentCheck = func() error { return errors.New("no display server") }
	if err := OpenBrowserWindow(context.Background(), browser.Config{}, &bytes.Buffer{}); err == nil {
		t.Fatal("opened a window with no display")
	}
}

func TestImportBrowserProfile(t *testing.T) {
	from, to := t.TempDir(), filepath.Join(t.TempDir(), "profile")
	saved := `[{"name":"user_session","value":"abc","domain":"github.com","path":"/"}]`
	if err := os.WriteFile(filepath.Join(from, "aviary-session-cookies.json"), []byte(saved), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ImportBrowserProfile(from, browser.Config{Profile: to}); err != nil {
		t.Fatalf("ImportBrowserProfile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(to, "aviary-session-cookies.json")); err != nil {
		t.Fatalf("imported sign-in missing: %v", err)
	}
	err := ImportBrowserProfile(t.TempDir(), browser.Config{Profile: to})
	if !errors.Is(err, browser.ErrNoSavedSession) || !strings.Contains(err.Error(), "machine with a display") {
		t.Fatalf("import from a profile with no sign-in = %v", err)
	}
}

func TestApplyBrowserSourcesWiresTheEngine(t *testing.T) {
	driver := &browser.Browser{}
	w := &Engine{Interval: BrowserInterval, Comments: GHCLI{}}
	recovery := ApplyBrowserSources(w, driver, BrowserOptions{}, GHCLI{})
	if recovery == nil {
		t.Fatal("no sign-in recovery returned for the banner")
	}
	if _, ok := w.Issues.(browser.SignInGuard); !ok {
		t.Fatalf("issues read through %T, want the signed-out guard", w.Issues)
	}
	if _, ok := w.Projects.(*browser.Board); !ok {
		t.Fatalf("boards read through %T, want the rendered board", w.Projects)
	}
	if _, ok := w.Comments.(*browser.CommentSource); !ok {
		t.Fatalf("comments read through %T, want the rendered conversation", w.Comments)
	}
	if w.Recover == nil || w.GitHubSession == nil {
		t.Fatal("sleep recovery or the GitHub session hook is not wired")
	}
	if w.GitHubSession().SignedOut {
		t.Fatal("a fresh run reported a signed-out session")
	}
	if ApplyBrowserSources(nil, driver, BrowserOptions{}, GHCLI{}) != nil {
		t.Fatal("a nil engine was wired")
	}
}

// scriptedPage is a page whose navigation and reads follow a script, so the
// sign-in flow's error paths can be driven without a browser.
type scriptedPage struct {
	navigateErr error
	// evals answers each read in turn; the last answer repeats.
	evals     []scriptedEval
	reads     int
	navigated []string
}

type scriptedEval struct {
	login string
	err   error
}

func (p *scriptedPage) Navigate(url string) error {
	p.navigated = append(p.navigated, url)
	return p.navigateErr
}
func (p *scriptedPage) Reload() error   { return nil }
func (p *scriptedPage) HTTPStatus() int { return 200 }
func (p *scriptedPage) Eval(_ string, out any) error {
	answer := scriptedEval{}
	if len(p.evals) > 0 {
		answer = p.evals[min(p.reads, len(p.evals)-1)]
	}
	p.reads++
	if answer.err != nil {
		return answer.err
	}
	data, _ := json.Marshal(answer.login)
	return json.Unmarshal(data, out)
}

// signInStub records the sessions the sign-in flow opens and closes.
type signInStub struct {
	opened []browser.Config
	closed int
}

func stubSignInSession(t *testing.T, page browser.Page, openErr error) *signInStub {
	t.Helper()
	stub := &signInStub{}
	previousOpen, previousHeaded, previousPoll := openSignInSession, headedEnvironmentCheck, signInPollInterval
	t.Cleanup(func() {
		openSignInSession, headedEnvironmentCheck, signInPollInterval = previousOpen, previousHeaded, previousPoll
	})
	openSignInSession = func(_ context.Context, config browser.Config) (*signInSession, error) {
		stub.opened = append(stub.opened, config)
		if openErr != nil {
			return nil, openErr
		}
		return &signInSession{page: page, close: func() { stub.closed++ }}, nil
	}
	headedEnvironmentCheck = func() error { return nil }
	signInPollInterval = time.Millisecond
	return stub
}

func TestReadGitHubLogin(t *testing.T) {
	login, err := readGitHubLogin(&scriptedPage{evals: []scriptedEval{{login: "  octocat\n"}}})
	if err != nil || login != "octocat" {
		t.Fatalf("readGitHubLogin = %q, %v; want the trimmed login", login, err)
	}
	evalErr := errors.New("execution context was destroyed")
	_, err = readGitHubLogin(&scriptedPage{evals: []scriptedEval{{err: evalErr}}})
	if !errors.Is(err, evalErr) || !strings.Contains(err.Error(), "read GitHub sign-in state") {
		t.Fatalf("readGitHubLogin on a failed read = %v", err)
	}
}

func TestVisitGitHubLoginReportsNavigationFailure(t *testing.T) {
	navErr := errors.New("net::ERR_NAME_NOT_RESOLVED")
	page := &scriptedPage{navigateErr: navErr, evals: []scriptedEval{{login: "octocat"}}}
	login, err := visitGitHubLogin(page, githubHomeURL)
	if !errors.Is(err, navErr) || !strings.Contains(err.Error(), "open "+githubHomeURL) || login != "" {
		t.Fatalf("visitGitHubLogin = %q, %v", login, err)
	}
	if page.reads != 0 {
		t.Fatal("read the sign-in state off a page that never loaded")
	}
}

func TestDescribeProfile(t *testing.T) {
	named := filepath.Join(t.TempDir(), "work-profile")
	if got := describeProfile(named); got != named {
		t.Fatalf("describeProfile(%q) = %q", named, got)
	}
	want, err := browser.ProfileDir("")
	if err != nil {
		t.Fatal(err)
	}
	if got := describeProfile(""); got != want || got == "" {
		t.Fatalf("describeProfile(\"\") = %q, want the default profile %q", got, want)
	}
}

func TestGitHubSignedInErrors(t *testing.T) {
	openErr := errors.New("no browser binary found")
	stub := stubSignInSession(t, &scriptedPage{}, openErr)
	if _, err := GitHubSignedIn(context.Background(), browser.Config{}); !errors.Is(err, openErr) {
		t.Fatalf("GitHubSignedIn with no browser = %v", err)
	}
	if stub.closed != 0 {
		t.Fatal("closed a session that never opened")
	}

	navErr := errors.New("net::ERR_INTERNET_DISCONNECTED")
	stub = stubSignInSession(t, &scriptedPage{navigateErr: navErr}, nil)
	if _, err := GitHubSignedIn(context.Background(), browser.Config{}); !errors.Is(err, navErr) {
		t.Fatalf("GitHubSignedIn offline = %v", err)
	}
	if stub.closed != 1 {
		t.Fatalf("closed %d sessions after a failed read, want 1", stub.closed)
	}

	stub = stubSignInSession(t, &scriptedPage{}, nil)
	login, err := GitHubSignedIn(context.Background(), browser.Config{})
	if err != nil || login != "" {
		t.Fatalf("GitHubSignedIn signed out = %q, %v; want an empty login", login, err)
	}
	if stub.closed != 1 {
		t.Fatalf("closed %d sessions, want 1", stub.closed)
	}
}

func TestGitHubSignInErrors(t *testing.T) {
	openErr := errors.New("profile is locked")
	stubSignInSession(t, &scriptedPage{}, openErr)
	var out bytes.Buffer
	if _, err := GitHubSignIn(context.Background(), browser.Config{}, &out, time.Second); !errors.Is(err, openErr) {
		t.Fatalf("GitHubSignIn with no browser = %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("announced a window that never opened: %q", out.String())
	}

	navErr := errors.New("net::ERR_CONNECTION_RESET")
	page := &scriptedPage{navigateErr: navErr}
	stub := stubSignInSession(t, page, nil)
	if _, err := GitHubSignIn(context.Background(), browser.Config{}, &out, time.Second); !errors.Is(err, navErr) {
		t.Fatalf("GitHubSignIn when the sign-in page fails = %v", err)
	}
	if stub.closed != 1 || out.Len() != 0 {
		t.Fatalf("closed %d sessions and printed %q, want the window closed silently", stub.closed, out.String())
	}
}

func TestGitHubSignInAlreadySignedIn(t *testing.T) {
	page := &scriptedPage{evals: []scriptedEval{{login: "octocat"}}}
	stub := stubSignInSession(t, page, nil)
	var out bytes.Buffer
	login, err := GitHubSignIn(context.Background(), browser.Config{}, &out, time.Second)
	if err != nil || login != "octocat" {
		t.Fatalf("GitHubSignIn on a signed-in profile = %q, %v", login, err)
	}
	if page.reads != 1 || out.Len() != 0 || stub.closed != 1 {
		t.Fatalf("read %d times, printed %q, closed %d; want one read, no prompt, window closed", page.reads, out.String(), stub.closed)
	}
}

func TestWaitForGitHubLoginRidesOutFailedReads(t *testing.T) {
	stubSignInSession(t, nil, nil)
	readErr := errors.New("execution context was destroyed")
	page := &scriptedPage{evals: []scriptedEval{{err: readErr}, {err: readErr}, {}, {login: "octocat"}}}
	login, err := waitForGitHubLogin(context.Background(), page, time.Second)
	if err != nil || login != "octocat" {
		t.Fatalf("waitForGitHubLogin = %q, %v", login, err)
	}
	if page.reads != 4 {
		t.Fatalf("read %d times, want 4", page.reads)
	}
}

func TestWaitForGitHubLoginReportsTheLastReadError(t *testing.T) {
	stubSignInSession(t, nil, nil)
	readErr := errors.New("target closed")
	_, err := waitForGitHubLogin(context.Background(), &scriptedPage{evals: []scriptedEval{{err: readErr}}}, 20*time.Millisecond)
	if !errors.Is(err, readErr) || !strings.Contains(err.Error(), "gave up after 20ms") {
		t.Fatalf("waitForGitHubLogin with failing reads = %v", err)
	}
}

func TestWaitForGitHubLoginStopsWhenCancelled(t *testing.T) {
	stubSignInSession(t, nil, nil)
	signInPollInterval = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	page := &scriptedPage{evals: []scriptedEval{{login: "octocat"}}}
	_, err := waitForGitHubLogin(ctx, page, time.Minute)
	if err == nil || !strings.Contains(err.Error(), "gave up") {
		t.Fatalf("waitForGitHubLogin after cancel = %v", err)
	}
	if page.reads != 0 {
		t.Fatal("kept reading after the context was cancelled")
	}
}

func TestImportBrowserProfileFailures(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	to := filepath.Join(t.TempDir(), "profile")
	if err := ImportBrowserProfile(missing, browser.Config{Profile: to}); !errors.Is(err, browser.ErrNoSavedSession) {
		t.Fatalf("import from a missing profile = %v", err)
	}

	corrupt := t.TempDir()
	if err := os.WriteFile(filepath.Join(corrupt, "aviary-session-cookies.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := ImportBrowserProfile(corrupt, browser.Config{Profile: to})
	if err == nil || errors.Is(err, browser.ErrNoSavedSession) || !strings.Contains(err.Error(), "decode saved sign-in") {
		t.Fatalf("import of an unreadable sign-in = %v", err)
	}
	if _, statErr := os.Stat(to); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("an unreadable sign-in still created the profile: %v", statErr)
	}

	from := t.TempDir()
	saved := `[{"name":"user_session","value":"abc","domain":"github.com","path":"/"}]`
	if err := os.WriteFile(filepath.Join(from, "aviary-session-cookies.json"), []byte(saved), 0o600); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err = ImportBrowserProfile(from, browser.Config{Profile: filepath.Join(blocker, "profile")})
	if err == nil || errors.Is(err, browser.ErrNoSavedSession) || !strings.Contains(err.Error(), "create browser profile directory") {
		t.Fatalf("import into a profile path under a file = %v", err)
	}
}

func TestApplyBrowserSourcesWithVision(t *testing.T) {
	driver := &browser.Browser{}
	w := &Engine{Interval: BrowserInterval}
	if ApplyBrowserSources(w, driver, BrowserOptions{Vision: true}, GHCLI{}) == nil {
		t.Fatal("no sign-in recovery returned for the banner")
	}
	if _, ok := w.Projects.(*browser.Board); !ok {
		t.Fatalf("boards read through %T, want the rendered board", w.Projects)
	}
	if w.Comments != nil {
		t.Fatalf("comments were wired to %T for an engine that reads none", w.Comments)
	}
	// A browser that never launched has nothing to restart on wake.
	w.Recover(context.Background())
	if ApplyBrowserSources(w, nil, BrowserOptions{}, GHCLI{}) != nil {
		t.Fatal("a nil browser was wired")
	}
}
