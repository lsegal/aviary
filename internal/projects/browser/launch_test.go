package browser

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	aviarybrowser "github.com/lsegal/aviary/internal/browser"
	"github.com/lsegal/aviary/internal/config"
)

// fakeLookup replaces browser discovery for the duration of a test so no
// test depends on which browsers happen to be installed on the machine running
// it, and none of them can launch one.
func fakeLookup(t *testing.T, onPath map[string]string, installed map[string]bool) {
	t.Helper()
	previousLook, previousStat := lookPath, statBinary
	t.Cleanup(func() { lookPath, statBinary = previousLook, previousStat })
	lookPath = func(name string) (string, error) {
		if path, ok := onPath[name]; ok {
			return path, nil
		}
		return "", exec.ErrNotFound
	}
	statBinary = func(path string) error {
		if installed[path] {
			return nil
		}
		return os.ErrNotExist
	}
}

func TestFindBinaryPrefersPathInOrder(t *testing.T) {
	fakeLookup(t, map[string]string{
		"chromium": "/usr/bin/chromium",
		"msedge":   "/usr/bin/msedge",
	}, nil)
	path, err := findBinary("")
	if err != nil {
		t.Fatalf("findBinary: %v", err)
	}
	if path != "/usr/bin/chromium" {
		t.Fatalf("found %q, want the earlier name in binaryNames", path)
	}
}

func TestFindBinaryFallsBackToWellKnownPaths(t *testing.T) {
	known := binaryPaths()
	var installed string
	for _, path := range known {
		if path != "" {
			installed = path
			break
		}
	}
	if installed == "" {
		t.Fatalf("binaryPaths returned no usable path on %s", runtime.GOOS)
	}
	fakeLookup(t, nil, map[string]bool{installed: true})
	path, err := findBinary("")
	if err != nil {
		t.Fatalf("findBinary: %v", err)
	}
	if path != installed {
		t.Fatalf("found %q, want %q", path, installed)
	}
}

func TestFindBinaryReportsActionableErrorWhenMissing(t *testing.T) {
	fakeLookup(t, nil, nil)
	_, err := findBinary("")
	if err == nil {
		t.Fatal("expected an error when no browser is installed")
	}
	for _, want := range []string{"browser binary setting", "Chrome"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
}

func TestFindBinaryUsesOverride(t *testing.T) {
	fakeLookup(t, map[string]string{
		"google-chrome": "/usr/bin/google-chrome",
		"my-browser":    "/opt/my-browser",
	}, nil)
	path, err := findBinary("my-browser")
	if err != nil {
		t.Fatalf("findBinary: %v", err)
	}
	if path != "/opt/my-browser" {
		t.Fatalf("found %q, want the override", path)
	}
}

func TestFindBinaryRejectsUnusableOverride(t *testing.T) {
	// A discovered browser must not quietly stand in for the one the user named.
	fakeLookup(t, map[string]string{"google-chrome": "/usr/bin/google-chrome"}, nil)
	_, err := findBinary("/nowhere/chrome")
	if err == nil {
		t.Fatal("expected an error for an override that cannot be run")
	}
	if !strings.Contains(err.Error(), "/nowhere/chrome") {
		t.Fatalf("error %q does not name the override", err)
	}
}

func TestProfileDirUsesOverride(t *testing.T) {
	dir, err := ProfileDir("/tmp/custom-profile")
	if err != nil {
		t.Fatalf("ProfileDir: %v", err)
	}
	if dir != "/tmp/custom-profile" {
		t.Fatalf("profile %q, want the override", dir)
	}
}

// TestProfileDirDefaultsToAviarysBrowserProfile checks Projects reads GitHub
// with the same profile Aviary's own browser signs in with.
func TestProfileDirDefaultsToAviarysBrowserProfile(t *testing.T) {
	dir, err := ProfileDir("")
	if err != nil {
		t.Fatalf("ProfileDir: %v", err)
	}
	if want := aviarybrowser.DefaultProfileDir(); dir != want {
		t.Fatalf("profile %q, want Aviary's browser profile %q", dir, want)
	}
}

func TestConfigFromBrowserSettings(t *testing.T) {
	got := ConfigFrom(config.BrowserConfig{Binary: "/opt/chrome", ProfileDir: "/data/profile", Headless: true})
	if got.Binary != "/opt/chrome" || got.Profile != "/data/profile" || got.Headed {
		t.Fatalf("ConfigFrom = %+v", got)
	}
}

// TestFreePortIsUsable checks the reserved port was released again, so
// the browser can bind it, and that repeated reservations do not collide the
// way a fixed port would across concurrent Aviary instances.
func TestFreePortIsUsable(t *testing.T) {
	port, err := freePort()
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	if port <= 0 || port > 65535 {
		t.Fatalf("port %d is not a valid port", port)
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatalf("reserved port %d was not released: %v", port, err)
	}
	defer func() { _ = listener.Close() }()

	other, err := freePort()
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	if other == port {
		t.Fatalf("second reservation returned the port still bound (%d)", port)
	}
}

func TestLaunchArgs(t *testing.T) {
	args := launchArgs("/profiles/aviary", 4321, false)
	want := []string{
		"--headless=new",
		"--disable-gpu",
		"--no-first-run",
		"--no-default-browser-check",
		"--user-data-dir=/profiles/aviary",
		"--remote-debugging-port=4321",
	}
	if len(args) != len(want) {
		t.Fatalf("args %v, want %v", args, want)
	}
	for i, arg := range want {
		if args[i] != arg {
			t.Fatalf("arg %d is %q, want %q", i, args[i], arg)
		}
	}
}

func TestDebugURLIsLoopbackOnly(t *testing.T) {
	// The DevTools endpoint is unauthenticated, so it must never be reachable
	// from outside the machine.
	if got := debugURL(9222); got != "http://127.0.0.1:9222" {
		t.Fatalf("debug URL %q", got)
	}
}

// TestWaitForReadyPollsUntilReady checks the readiness wait keeps polling
// while the browser is still starting, instead of giving up on the first
// refusal.
func TestWaitForReadyPollsUntilReady(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			t.Errorf("unexpected request for %s", r.URL.Path)
		}
		requests++
		if requests < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = fmt.Fprint(w, `{"webSocketDebuggerUrl":"ws://127.0.0.1:9222/devtools/browser/abc"}`)
	}))
	defer server.Close()

	wsURL, err := waitForReady(context.Background(), server.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("waitForReady: %v", err)
	}
	if wsURL != "ws://127.0.0.1:9222/devtools/browser/abc" {
		t.Fatalf("WebSocket URL %q", wsURL)
	}
	if requests < 3 {
		t.Fatalf("endpoint was polled %d times, expected it to keep retrying", requests)
	}
}

func TestWaitForReadyTimesOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	_, err := waitForReady(context.Background(), server.URL, 250*time.Millisecond)
	if err == nil {
		t.Fatal("expected a timeout when the browser never becomes ready")
	}
	if !strings.Contains(err.Error(), server.URL) {
		t.Fatalf("error %q does not name the endpoint", err)
	}
}

func TestWaitForReadyStopsWhenContextIsCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := waitForReady(ctx, server.URL, time.Minute); err == nil {
		t.Fatal("expected the canceled context to end the wait")
	}
}

func TestWebSocketURLRejectsEmptyEndpointReply(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{}`)
	}))
	defer server.Close()

	if _, err := webSocketURL(context.Background(), server.URL); err == nil {
		t.Fatal("expected an error when the endpoint reports no WebSocket URL")
	}
}

// TestOpenPageTargetFindsTheWindowChromeOpened checks the window a
// headed browser puts on screen by itself is discovered, so `Aviary auth` drives
// it instead of opening a second one (issue #412).
func TestOpenPageTargetFindsTheWindowChromeOpened(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/list" {
			t.Errorf("requested %s, want /json/list", r.URL.Path)
		}
		_, _ = fmt.Fprint(w, `[{"id":"devtools-1","type":"other"},{"id":"page-1","type":"page"},{"id":"page-2","type":"page"}]`)
	}))
	defer server.Close()
	id, err := openPageTarget(context.Background(), server.URL, time.Second)
	if err != nil {
		t.Fatalf("openPageTarget: %v", err)
	}
	if id != "page-1" {
		t.Fatalf("target %q, want the first page target", id)
	}
}

// TestOpenPageTargetPollsUntilTheWindowAppears checks the initial window
// is waited for rather than read once: it is created alongside the DevTools
// endpoint, not before it.
func TestOpenPageTargetPollsUntilTheWindowAppears(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls < 3 {
			_, _ = fmt.Fprint(w, `[]`)
			return
		}
		_, _ = fmt.Fprint(w, `[{"id":"page-1","type":"page"}]`)
	}))
	defer server.Close()
	id, err := openPageTarget(context.Background(), server.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("openPageTarget: %v", err)
	}
	if id != "page-1" {
		t.Fatalf("target %q, want page-1", id)
	}
	if calls < 3 {
		t.Fatalf("endpoint polled %d time(s), want it retried until a page appeared", calls)
	}
}

// TestOpenPageTargetFailsWithNoPage checks a browser reporting no window
// is an error, so the caller falls back to opening its own tab instead of
// attaching to a target that does not exist.
func TestOpenPageTargetFailsWithNoPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `[{"id":"worker-1","type":"service_worker"}]`)
	}))
	defer server.Close()
	if _, err := openPageTarget(context.Background(), server.URL, 200*time.Millisecond); err == nil {
		t.Fatal("expected an error when the browser reports no open page")
	}
}

func TestOpenPageTargetRejectsUnreadableReply(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `not json`)
	}))
	defer server.Close()
	if _, err := openPageTarget(context.Background(), server.URL, 200*time.Millisecond); err == nil {
		t.Fatal("expected an error for a reply that is not a target list")
	}
}

func TestLaunchArgsHeaded(t *testing.T) {
	args := launchArgs("/profiles/aviary", 4321, true)
	for _, unwanted := range []string{"--headless=new", "--disable-gpu"} {
		for _, arg := range args {
			if arg == unwanted {
				t.Fatalf("headed launch still passes %s: %v", unwanted, args)
			}
		}
	}
	want := map[string]bool{
		"--no-first-run":                   true,
		"--no-default-browser-check":       true,
		"--user-data-dir=/profiles/aviary": true,
		"--remote-debugging-port=4321":     true,
	}
	for _, arg := range args {
		delete(want, arg)
	}
	if len(want) != 0 {
		t.Fatalf("headed launch is missing %v: %v", want, args)
	}
}
