package browser

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestLaunchChromeWithModeReportsAMissingBinary checks a browser binary
// setting that names nothing is reported instead of launching anything.
func TestLaunchChromeWithModeReportsAMissingBinary(t *testing.T) {
	fakeLookup(t, nil, nil)
	missing := filepath.Join(t.TempDir(), "no-such-chrome")
	m := NewManager(missing, 19876, t.TempDir(), true)

	err := launchChromeWithMode(m, true)
	if err == nil || !strings.Contains(err.Error(), "not found or not executable") || !strings.Contains(err.Error(), strconv.Quote(missing)) {
		t.Fatalf("launchChromeWithMode() error = %v, want the missing binary %q reported", err, missing)
	}
}

// TestLaunchChromeWithModeRefusesAHeldProfile checks the detached browser is
// not started on a profile a browser of this process already holds.
func TestLaunchChromeWithModeRefusesAHeldProfile(t *testing.T) {
	argsFile := useFakeBrowser(t, "exit3")
	profile := t.TempDir()
	lock, err := LockProfile(profile)
	if err != nil {
		t.Fatalf("LockProfile() error = %v", err)
	}
	defer lock.Release()
	m := NewManager("", 19876, profile, true)

	err = launchChromeWithMode(m, true)
	if !errors.Is(err, ErrProfileLocked) || !strings.Contains(err.Error(), profile) {
		t.Fatalf("launchChromeWithMode() error = %v, want ErrProfileLocked naming %s", err, profile)
	}
	if _, statErr := os.Stat(argsFile); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("browser was started on a held profile (args file stat: %v)", statErr)
	}
}

// TestLaunchChromeWithModeStartsTheBrowserDetached checks the discovered
// browser is started with the profile, DevTools port and headless flags, and
// the launch returns without waiting for it.
func TestLaunchChromeWithModeStartsTheBrowserDetached(t *testing.T) {
	for _, headless := range []bool{true, false} {
		t.Run("headless="+strconv.FormatBool(headless), func(t *testing.T) {
			argsFile := useFakeBrowser(t, "exit3")
			profile := t.TempDir()
			port, err := FreePort()
			if err != nil {
				t.Fatal(err)
			}
			m := NewManager("", port, profile, headless)

			if err := launchChromeWithMode(m, headless); err != nil {
				t.Fatalf("launchChromeWithMode() error = %v", err)
			}
			args := waitForLaunchArgs(t, argsFile)
			for _, want := range []string{"--user-data-dir=" + profile, "--remote-debugging-port=" + strconv.Itoa(port)} {
				if !slices.Contains(args, want) {
					t.Errorf("launch args %q missing %q", args, want)
				}
			}
			if got := slices.Contains(args, "--headless=new"); got != headless {
				t.Errorf("launch args %q: headless flag present = %v, want %v", args, got, headless)
			}
			// The check only probes the profile; it must not keep it claimed.
			assertProfileFree(t, profile)
		})
	}
}

// TestLaunchChromeWithModeReportsABrowserThatCannotStart checks a binary that
// resolves but cannot be executed is reported with its path.
func TestLaunchChromeWithModeReportsABrowserThatCannotStart(t *testing.T) {
	notExecutable := t.TempDir() // a directory resolves but cannot be run
	fakeLookup(t, map[string]string{"google-chrome": notExecutable}, nil)
	m := NewManager("", 19876, t.TempDir(), true)

	err := launchChromeWithMode(m, true)
	if err == nil || !strings.Contains(err.Error(), "starting Chrome ("+notExecutable+")") {
		t.Fatalf("launchChromeWithMode() error = %v, want a start failure naming %s", err, notExecutable)
	}
}

// TestLaunchChromeAndWaitReturnsTheLaunchError checks a failed launch is
// returned at once instead of waiting for a browser that was never started.
func TestLaunchChromeAndWaitReturnsTheLaunchError(t *testing.T) {
	oldLaunch, oldWait := launchChromeWithModeFn, waitForChromeFn
	t.Cleanup(func() { launchChromeWithModeFn, waitForChromeFn = oldLaunch, oldWait })

	launchErr := errors.New("launch failed")
	var launchedHeadless []bool
	launchChromeWithModeFn = func(_ *Manager, headless bool) error {
		launchedHeadless = append(launchedHeadless, headless)
		return launchErr
	}
	waitForChromeFn = func(context.Context, string) (string, error) {
		t.Error("waited for a browser whose launch failed")
		return "ws://unexpected", nil
	}

	wsURL, err := launchChromeAndWait(context.Background(), NewManager("", 19876, t.TempDir(), false), "http://127.0.0.1:19876", true)
	if !errors.Is(err, launchErr) || wsURL != "" {
		t.Fatalf("launchChromeAndWait() = (%q, %v), want (\"\", %v)", wsURL, err, launchErr)
	}
	if !slices.Equal(launchedHeadless, []bool{true}) {
		t.Fatalf("launch modes = %v, want one headless launch", launchedHeadless)
	}
}

// TestLaunchChromeAndWaitBoundsTheWait checks the readiness wait gets a
// deadline even when the caller's context has none.
func TestLaunchChromeAndWaitBoundsTheWait(t *testing.T) {
	oldLaunch, oldWait := launchChromeWithModeFn, waitForChromeFn
	t.Cleanup(func() { launchChromeWithModeFn, waitForChromeFn = oldLaunch, oldWait })

	launchChromeWithModeFn = func(*Manager, bool) error { return nil }
	var gotBaseURL string
	waitForChromeFn = func(ctx context.Context, baseURL string) (string, error) {
		gotBaseURL = baseURL
		if _, ok := ctx.Deadline(); !ok {
			t.Error("readiness wait has no deadline")
		}
		return "ws://127.0.0.1:19876/devtools/browser/ready", nil
	}

	wsURL, err := launchChromeAndWait(context.Background(), NewManager("", 19876, t.TempDir(), true), "http://127.0.0.1:19876", true)
	if err != nil || wsURL != "ws://127.0.0.1:19876/devtools/browser/ready" {
		t.Fatalf("launchChromeAndWait() = (%q, %v), want the ready WebSocket URL", wsURL, err)
	}
	if gotBaseURL != "http://127.0.0.1:19876" {
		t.Fatalf("waited on %q, want the DevTools base URL", gotBaseURL)
	}
}

func TestCreateTabRejectsAMalformedBaseURL(t *testing.T) {
	if tab, err := createTab(context.Background(), "http://bad\x7fhost", "https://example.com"); err == nil {
		t.Fatalf("createTab() = %+v, want an error for a malformed DevTools URL", tab)
	}
}

func TestCreateTabReportsAnUnreachableBrowser(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	baseURL := srv.URL
	srv.Close()

	if tab, err := createTab(context.Background(), baseURL, "https://example.com"); err == nil {
		t.Fatalf("createTab() = %+v, want an error from a browser that is not listening", tab)
	}
}

func TestCreateTabReportsTheUnexpectedStatus(t *testing.T) {
	var gotMethod, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotQuery = r.Method, r.URL.RawQuery
		http.Error(w, "nope", http.StatusMethodNotAllowed)
	}))
	defer srv.Close()

	_, err := createTab(context.Background(), srv.URL, "https://example.com/a?b=c")
	if err == nil || !strings.Contains(err.Error(), "unexpected status 405") {
		t.Fatalf("createTab() error = %v, want the 405 status reported", err)
	}
	if gotMethod != http.MethodPut || gotQuery != "https%3A%2F%2Fexample.com%2Fa%3Fb%3Dc" {
		t.Fatalf("request = %s ?%s, want PUT with the escaped page URL", gotMethod, gotQuery)
	}
}

// waitForLaunchArgs waits for the detached stand-in browser to record the
// arguments it was started with.
func waitForLaunchArgs(t *testing.T, argsFile string) []string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if data, err := os.ReadFile(argsFile); err == nil && len(data) > 0 {
			return strings.Split(string(data), "\n")
		}
		if time.Now().After(deadline) {
			t.Fatal("detached browser never recorded its launch arguments")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
