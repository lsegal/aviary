package projects

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/projects/browser"
)

// fakeHandover stands in for the browser a sign-in window takes the profile
// from, recording what the manager did to it.
type fakeHandover struct {
	mu       sync.Mutex
	calls    []string
	onResume func()
}

func (f *fakeHandover) Suspend() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "suspend")
	return nil
}

func (f *fakeHandover) Resume() error {
	f.mu.Lock()
	f.calls = append(f.calls, "resume")
	onResume := f.onResume
	f.mu.Unlock()
	if onResume != nil {
		onResume()
	}
	return nil
}

func (f *fakeHandover) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// browserManager is a manager with a browser-delivery run whose sign-in
// window and browser are fakes.
func browserManager(t *testing.T, headed error, login func() (string, error)) (*Manager, *fakeHandover) {
	t.Helper()
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	handover := &fakeHandover{}
	signIn := browser.SignIn{
		Login: func(context.Context, browser.Config, io.Writer, time.Duration) (string, error) {
			return login()
		},
		Headed:  func() error { return headed },
		Timeout: time.Second,
	}
	profile := filepath.Join(t.TempDir(), "profile")
	m.running = &engineRun{
		settings: engineSettings{Delivery: DeliveryBrowser, Browser: BrowserOptions{Profile: profile}},
		signIn:   browser.NewSignInRecovery(handover, browser.Config{Profile: profile}, signIn, m.logf),
		driver:   handover,
	}
	return m, handover
}

func TestManagerGitHubSignInOpensTheWindowInTheBackground(t *testing.T) {
	release := make(chan struct{})
	m, handover := browserManager(t, nil, func() (string, error) {
		<-release
		return "octocat", nil
	})
	result, err := m.GitHubSignIn()
	if err != nil || result["opened"] != true {
		t.Fatalf("GitHubSignIn = %v, %v", result, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !m.GitHubSession().SigningIn {
		if time.Now().After(deadline) {
			t.Fatal("the session never reported the open window")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := m.GitHubSignIn(); !errors.Is(err, browser.ErrSigningIn) {
		t.Fatalf("second GitHubSignIn = %v, want ErrSigningIn", err)
	}
	close(release)
	for m.GitHubSession().SigningIn {
		if time.Now().After(deadline) {
			t.Fatal("the window never closed")
		}
		time.Sleep(time.Millisecond)
	}
	if got := strings.Join(handover.recorded(), ","); got != "suspend,resume" {
		t.Fatalf("browser calls %q, want the profile handed over and back", got)
	}
}

func TestManagerGitHubSignInWithoutADisplay(t *testing.T) {
	m, handover := browserManager(t, errors.New("no display server"), func() (string, error) {
		t.Fatal("opened a window with no display")
		return "", nil
	})
	_, err := m.GitHubSignIn()
	if !errors.Is(err, browser.ErrSignInWindowUnavailable) || !strings.Contains(err.Error(), "gh polling") {
		t.Fatalf("GitHubSignIn without a display = %v", err)
	}
	if len(handover.recorded()) != 0 {
		t.Fatal("stopped the browser for a window that can never appear")
	}
	if got := m.GitHubSession().WindowUnavailable; got != "no display server" {
		t.Fatalf("session WindowUnavailable = %q", got)
	}
}

func TestManagerImportRestartsTheBrowserAroundTheImport(t *testing.T) {
	m, handover := browserManager(t, nil, func() (string, error) { return "", nil })
	from := t.TempDir()
	saved := `[{"name":"user_session","value":"abc","domain":"github.com","path":"/"}]`
	if err := os.WriteFile(filepath.Join(from, "aviary-session-cookies.json"), []byte(saved), 0o600); err != nil {
		t.Fatal(err)
	}
	imported := filepath.Join(m.running.settings.Browser.Profile, "aviary-session-cookies.json")
	// The browser saves its own session as it stops, so the import has to be
	// written after Suspend and in place by Resume.
	handover.onResume = func() {
		if _, err := os.Stat(imported); err != nil {
			t.Errorf("browser resumed before the import was written: %v", err)
		}
	}
	result, err := m.ImportBrowserProfile(from)
	if err != nil || result["imported"] != true {
		t.Fatalf("ImportBrowserProfile = %v, %v", result, err)
	}
	if got := strings.Join(handover.recorded(), ","); got != "suspend,resume" {
		t.Fatalf("browser calls %q", got)
	}
	if _, err := m.ImportBrowserProfile(t.TempDir()); !errors.Is(err, browser.ErrNoSavedSession) {
		t.Fatalf("import from a profile without a sign-in = %v", err)
	}
	if got := strings.Join(handover.recorded(), ","); got != "suspend,resume,suspend,resume" {
		t.Fatalf("a failed import left the browser stopped: %q", got)
	}
}

func TestManagerStartBrowserFailureNamesGHPolling(t *testing.T) {
	previous := startBrowserDriver
	t.Cleanup(func() { startBrowserDriver = previous })
	startBrowserDriver = func(context.Context, browser.Config) (*browser.Browser, error) {
		return nil, errors.New("no Chrome, Chromium or Edge installation found")
	}
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	run := &engineRun{settings: engineSettings{Delivery: DeliveryBrowser}, engine: &Engine{}}
	err = m.startBrowser(context.Background(), run, GHCLI{})
	if err == nil || !strings.Contains(err.Error(), "no Chrome") || !strings.Contains(err.Error(), "projects.delivery to poll") {
		t.Fatalf("startBrowser = %v", err)
	}
}

func TestManagerBrowserSettingsFollowAviarysBrowser(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m.SetBrowser(config.BrowserConfig{Binary: "/opt/chrome", ProfileDir: "/data/profile"})
	m.SetAgents(nil, config.ModelsConfig{Defaults: &config.ModelDefaults{Model: "cli/codex"}}, config.ApprovalConfig{})
	m.cfg = config.ProjectsConfig{List: []config.ProjectConfig{{Target: "o/r"}}, BrowserVision: true}
	m.mu.Lock()
	s, err := m.settingsLocked()
	m.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	want := BrowserOptions{Binary: "/opt/chrome", Profile: "/data/profile", Vision: true}
	if s.Delivery != DeliveryBrowser || s.Interval != 20*time.Second || s.Browser != want {
		t.Fatalf("settings delivery %q every %s with %+v, want browser every 20s with %+v", s.Delivery, s.Interval, s.Browser, want)
	}
	// A browser setting change restarts the engine, so a new profile is used.
	before := s.restartKey()
	m.SetBrowser(config.BrowserConfig{ProfileDir: "/data/other"})
	m.mu.Lock()
	s, _ = m.settingsLocked()
	m.mu.Unlock()
	if s.restartKey() == before {
		t.Fatal("a new browser profile does not restart the engine")
	}

	m.cfg.Delivery, m.cfg.BrowserVision = DeliveryPoll, false
	m.mu.Lock()
	s, _ = m.settingsLocked()
	m.mu.Unlock()
	if s.Browser != (BrowserOptions{}) {
		t.Fatalf("gh polling carries browser options %+v", s.Browser)
	}
	if got := m.GitHubSession(); got.SignedOut || got.SigningIn {
		t.Fatalf("no browser run reported a session %+v", got)
	}
	if _, err := m.GitHubSignIn(); !errors.Is(err, errNoBrowserDelivery) {
		t.Fatalf("GitHubSignIn without browser delivery = %v", err)
	}
}
