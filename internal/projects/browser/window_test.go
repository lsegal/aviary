package browser

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Files the stand-in browser and the window tests talk through, in its
// --user-data-dir: it writes fakeWindowListed once it has answered a page list,
// and it reads fakeWindowClosed (report no pages, as macOS does with the
// application still running) and fakeBrowserQuit (exit, as closing the last
// window does elsewhere).
const (
	fakeWindowListed = "FakeWindowListed"
	fakeWindowClosed = "FakeWindowClosed"
	fakeBrowserQuit  = "FakeBrowserQuit"
)

// serveFakeWindow gives the stand-in browser an open window for the tests here
// to close.
func serveFakeWindow(dataDir string) {
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(dataDir, name))
		return err == nil
	}
	http.HandleFunc("/json/list", func(w http.ResponseWriter, _ *http.Request) {
		targets := []targetInfo{}
		if !exists(fakeWindowClosed) {
			targets = append(targets, targetInfo{ID: "window", Type: "page"})
		}
		_ = json.NewEncoder(w).Encode(targets)
		_ = os.WriteFile(filepath.Join(dataDir, fakeWindowListed), nil, 0o600)
	})
	go func() {
		for !exists(fakeBrowserQuit) {
			time.Sleep(5 * time.Millisecond)
		}
		os.Exit(0)
	}()
}

// fakeWindowConfig points OpenWindow at the stand-in browser with its waits
// shortened, and answers the shutdown request by telling the stand-in to exit,
// since it has no DevTools WebSocket to send one over.
func fakeWindowConfig(t *testing.T) Config {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	t.Setenv(fakeBrowserEnv, "1")
	interval, stopTimeout, closer := windowPollInterval, windowStopTimeout, closeBrowser
	windowPollInterval, windowStopTimeout = 5*time.Millisecond, 10*time.Second
	t.Cleanup(func() { windowPollInterval, windowStopTimeout, closeBrowser = interval, stopTimeout, closer })
	profile := t.TempDir()
	closeBrowser = func(context.Context, string) error {
		return os.WriteFile(filepath.Join(profile, fakeBrowserQuit), nil, 0o600)
	}
	return Config{Binary: executable, Profile: profile}
}

// openFakeWindow runs OpenWindow in the background, waits until the browser has
// reported its window, and returns the channel OpenWindow's result arrives on.
func openFakeWindow(ctx context.Context, t *testing.T, config Config) <-chan error {
	t.Helper()
	result := make(chan error, 1)
	go func() { result <- OpenWindow(ctx, config) }()
	listed := filepath.Join(config.Profile, fakeWindowListed)
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if _, err := os.Stat(listed); err == nil {
			return result
		}
		if time.Now().After(deadline) {
			t.Fatal("the browser never reported its window")
		}
		select {
		case err := <-result:
			t.Fatalf("OpenWindow() returned before the window opened: %v", err)
		default:
		}
	}
}

// awaitWindowResult fails the test if OpenWindow is still blocked after the
// browser should have gone away.
func awaitWindowResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(20 * time.Second):
		t.Fatal("OpenWindow() did not return")
		return nil
	}
}

func touchProfileFile(t *testing.T, config Config, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(config.Profile, name), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestOpenWindowReturnsWhenTheBrowserExits(t *testing.T) {
	config := fakeWindowConfig(t)
	result := openFakeWindow(context.Background(), t, config)
	touchProfileFile(t, config, fakeBrowserQuit)
	if err := awaitWindowResult(t, result); err != nil {
		t.Fatalf("OpenWindow() error = %v", err)
	}
}

// macOS keeps the application running after its last window closes, so the
// process exiting alone would leave the command open forever.
func TestOpenWindowShutsDownABrowserLeftWithNoWindows(t *testing.T) {
	config := fakeWindowConfig(t)
	requested := make(chan string, 1)
	quit := closeBrowser
	closeBrowser = func(ctx context.Context, wsURL string) error {
		requested <- wsURL
		return quit(ctx, wsURL)
	}
	result := openFakeWindow(context.Background(), t, config)
	touchProfileFile(t, config, fakeWindowClosed)
	if err := awaitWindowResult(t, result); err != nil {
		t.Fatalf("OpenWindow() error = %v", err)
	}
	select {
	case wsURL := <-requested:
		if !strings.Contains(wsURL, "/devtools/browser/") {
			t.Fatalf("asked %q to shut down, want the browser's own WebSocket", wsURL)
		}
	default:
		t.Fatal("the browser was not asked to shut down cleanly")
	}
}

func TestOpenWindowWaitsForAWindowBeforeTreatingNoPagesAsClosed(t *testing.T) {
	config := fakeWindowConfig(t)
	touchProfileFile(t, config, fakeWindowClosed)
	result := openFakeWindow(context.Background(), t, config)
	select {
	case err := <-result:
		t.Fatalf("OpenWindow() returned while the first window was still opening: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	touchProfileFile(t, config, fakeBrowserQuit)
	if err := awaitWindowResult(t, result); err != nil {
		t.Fatalf("OpenWindow() error = %v", err)
	}
}

func TestOpenWindowShutsTheBrowserDownCleanlyOnCancel(t *testing.T) {
	config := fakeWindowConfig(t)
	requested := false
	quit := closeBrowser
	closeBrowser = func(ctx context.Context, wsURL string) error {
		requested = true
		return quit(ctx, wsURL)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := openFakeWindow(ctx, t, config)
	cancel()
	if err := awaitWindowResult(t, result); err != nil {
		t.Fatalf("OpenWindow() error = %v", err)
	}
	if !requested {
		t.Fatal("the browser was not asked to shut down cleanly, which loses unsaved preferences")
	}
}

func TestOpenWindowKillsABrowserThatWillNotShutDown(t *testing.T) {
	config := fakeWindowConfig(t)
	windowStopTimeout = 10 * time.Millisecond
	closeBrowser = func(context.Context, string) error { return errors.New("refused") }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := openFakeWindow(ctx, t, config)
	cancel()
	if err := awaitWindowResult(t, result); err != nil {
		t.Fatalf("OpenWindow() error = %v", err)
	}
}

// Changes made in a private directory would be thrown away on exit, so unlike
// launch this must not fall back to one.
func TestOpenWindowRefusesAProfileAnotherInstanceHolds(t *testing.T) {
	config := fakeWindowConfig(t)
	lock, err := lockProfile(config.Profile)
	if err != nil {
		t.Fatalf("lockProfile() error = %v", err)
	}
	defer lock.release()
	err = OpenWindow(context.Background(), config)
	if !errors.Is(err, errProfileLocked) {
		t.Fatalf("OpenWindow() error = %v, want errProfileLocked", err)
	}
	if !strings.Contains(err.Error(), config.Profile) {
		t.Fatalf("error does not name the profile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(config.Profile, fakeWindowListed)); err == nil {
		t.Fatal("a browser was launched on a profile another instance holds")
	}
}

func TestOpenWindowHoldsTheProfileUntilItCloses(t *testing.T) {
	config := fakeWindowConfig(t)
	result := openFakeWindow(context.Background(), t, config)
	if lock, err := lockProfile(config.Profile); !errors.Is(err, errProfileLocked) {
		lock.release()
		t.Fatalf("lockProfile() with the window open error = %v, want errProfileLocked", err)
	}
	touchProfileFile(t, config, fakeBrowserQuit)
	if err := awaitWindowResult(t, result); err != nil {
		t.Fatalf("OpenWindow() error = %v", err)
	}
	lock, err := lockProfile(config.Profile)
	if err != nil {
		t.Fatalf("lockProfile() after the window closed error = %v", err)
	}
	lock.release()
}

// Chrome hands off to a browser already on the directory and exits, which only
// the profile lock normally rules out; one started outside Aviary does not hold it.
func TestOpenWindowReportsABrowserThatHandsOff(t *testing.T) {
	config := fakeWindowConfig(t)
	touchProfileFile(t, config, "FakeSingletonLock")
	err := OpenWindow(context.Background(), config)
	if err == nil || !strings.Contains(err.Error(), "another browser") || !strings.Contains(err.Error(), config.Profile) {
		t.Fatalf("OpenWindow() error = %v, want the hand-off explained", err)
	}
}

func TestOpenWindowReportsAMissingBinary(t *testing.T) {
	fakeLookup(t, nil, nil)
	if err := OpenWindow(context.Background(), Config{Binary: "not-a-browser", Profile: t.TempDir()}); err == nil {
		t.Fatal("OpenWindow() error = nil, want the missing binary reported")
	}
}

// The shutdown request only means anything to a real browser, and chromedp's own
// Browser refuses to send it, so this is what proves it arrives. It runs headless
// and skips where no browser is installed, as the other live tests do.
func TestCloseOverDevToolsShutsDownARealBrowser(t *testing.T) {
	live := liveProfile(t, t.TempDir())
	defer func() { _ = live.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	base := debugURL(live.cmd.port)
	if err := closeOverDevTools(ctx, live.cmd.wsURL); err != nil {
		t.Fatalf("closeOverDevTools() error = %v", err)
	}
	for {
		if _, err := webSocketURL(ctx, base); err != nil {
			if ctx.Err() != nil {
				t.Fatal("the browser was still answering after being asked to shut down")
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}
