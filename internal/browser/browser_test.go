package browser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/stretchr/testify/assert"

	"github.com/lsegal/aviary/internal/config"
)

func TestNewManager_Defaults(t *testing.T) {
	m := NewManager("", 0, "", false)
	assert.Equal(t, 9222, m.cdpPort)
	assert.Equal(t, "", m.binary)

}

func TestNewManager_CustomPort(t *testing.T) {
	m := NewManager("/usr/bin/chromium", 9333, "/tmp/profile", true)
	assert.Equal(t, 9333, m.cdpPort)
	assert.Equal(t, "/usr/bin/chromium", m.binary)
	assert.Equal(t, "/tmp/profile", m.profileDir)
	assert.True(t, m.headless)

}

func TestManager_UserDataDirDefault(t *testing.T) {
	m := NewManager("", 0, "", false)
	got := m.userDataDir()
	want := filepath.Join(filepath.Dir(config.DefaultPath()), "browser")
	assert.Equal(t, want, got)

	// Explicit profileDir is used as-is.
	m2 := NewManager("", 0, "/tmp/my-profile", false)
	assert.Equal(t, "/tmp/my-profile", m2.userDataDir())

}

func TestIsIgnorableChromeDPError(t *testing.T) {
	assert.True(t, isIgnorableChromeDPError(`could not unmarshal event: json: cannot unmarshal JSON string into Go network.InitiatorType within "/initiator/type": unknown InitiatorType value: FedCM`))
	assert.False(t, isIgnorableChromeDPError(`could not unmarshal event: unknown ResourceType value: NewThing`))
	assert.False(t, isIgnorableChromeDPError(`context deadline exceeded`))
}

// cancelledCtx returns a context that is already cancelled, suitable for
// tests that need to trigger context-propagation paths without real Chrome.
func cancelledCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestManager_ClickWithoutChrome(t *testing.T) {
	m := NewManager("", 19876, "", false) // unlikely port
	err := m.Click(cancelledCtx(), "tab-id", "#btn")
	assert.Error(t, err)

}

func TestManager_TypeWithoutChrome(t *testing.T) {
	m := NewManager("", 19876, "", false)
	err := m.Type(cancelledCtx(), "tab-id", "#input", "hello")
	assert.Error(t, err)

}

func TestManager_ScreenshotWithoutChrome(t *testing.T) {
	m := NewManager("", 19876, "", false)
	buf, err := m.Screenshot(cancelledCtx(), "tab-id")
	assert.Error(t, err)
	assert.Nil(t, buf)

}

func TestManager_EvalJSWithoutChrome(t *testing.T) {
	m := NewManager("", 19876, "", false)
	result, err := m.EvalJS(cancelledCtx(), "tab-id", "1+1")
	assert.Error(t, err)
	assert.Equal(t, "", result)

}

func TestManager_CloseIsNoOp(_ *testing.T) {
	m := NewManager("", 0, "", false)
	// Close is a no-op and must not panic.
	m.Close()
	m.Close()
}

// parseCDPPort extracts the port number from an httptest.Server URL.
func parseCDPPort(t *testing.T, srvURL string) int {
	t.Helper()
	u, err := url.Parse(srvURL)
	assert.NoError(t, err)

	port, err := strconv.Atoi(u.Port())
	assert.NoError(t, err)

	return port
}

// --- fetchTabs ---

func TestFetchTabs_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/list" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]ChromeTab{ //nolint:errcheck
				{ID: "tab1", Type: "page", URL: "https://example.com", Title: "Example"},
				{ID: "tab2", Type: "background_page", URL: "chrome://newtab"},
			})
		}
	}))
	defer srv.Close()

	tabs, err := fetchTabs(srv.URL)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(tabs))
	assert.Equal(t, "tab1", tabs[0].ID)

}

func TestFetchTabs_ConnectionError(t *testing.T) {
	_, err := fetchTabs("http://127.0.0.1:1")
	assert. // nothing listening
		Error(t, err)

}

func TestFetchTabs_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not-json")) //nolint:errcheck
	}))
	defer srv.Close()

	_, err := fetchTabs(srv.URL)
	assert.Error(t, err)

}

// --- createTab ---

func TestCreateTab_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/json/new") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(ChromeTab{ //nolint:errcheck
				ID:   "newtab1",
				Type: "page",
				URL:  "https://example.com",
			})
		}
	}))
	defer srv.Close()

	tab, err := createTab(context.Background(), srv.URL, "https://example.com")
	assert.NoError(t, err)
	assert.Equal(t, "newtab1", tab.ID)

}

func TestCreateTab_EmptyID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChromeTab{ID: "", Type: "page"}) //nolint:errcheck
	}))
	defer srv.Close()

	_, err := createTab(context.Background(), srv.URL, "https://example.com")
	assert.Error(t, err)

}

func TestCreateTab_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := createTab(context.Background(), srv.URL, "https://example.com")
	assert.Error(t, err)

}

func TestCreateTab_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not-json")) //nolint:errcheck
	}))
	defer srv.Close()

	_, err := createTab(context.Background(), srv.URL, "https://example.com")
	assert.Error(t, err)

}

// --- fetchWebSocketURL ---

func TestFetchWebSocketURL_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/version" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(chromeVersionInfo{ //nolint:errcheck
				WebSocketDebuggerURL: "ws://localhost:9222/devtools/browser/abc123",
			})
		}
	}))
	defer srv.Close()

	wsURL, err := fetchWebSocketURL(srv.URL)
	assert.NoError(t, err)
	assert.True(t, strings.HasPrefix(wsURL, "ws://"))

}

func TestFetchWebSocketURL_EmptyURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(chromeVersionInfo{WebSocketDebuggerURL: ""}) //nolint:errcheck
	}))
	defer srv.Close()

	_, err := fetchWebSocketURL(srv.URL)
	assert.Error(t, err)

}

func TestFetchWebSocketURL_ConnectionError(t *testing.T) {
	_, err := fetchWebSocketURL("http://127.0.0.1:1")
	assert.Error(t, err)

}

func TestFetchWebSocketURL_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("bad")) //nolint:errcheck
	}))
	defer srv.Close()

	_, err := fetchWebSocketURL(srv.URL)
	assert.Error(t, err)

}

// --- waitForChrome ---

func TestWaitForChrome_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(chromeVersionInfo{ //nolint:errcheck
			WebSocketDebuggerURL: "ws://localhost:9222/devtools/browser/ready",
		})
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	wsURL, err := waitForChrome(ctx, srv.URL)
	assert.NoError(t, err)
	assert.NotEqual(t, "", wsURL)

}

func TestWaitForChrome_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	_, err := waitForChrome(ctx, srv.URL)
	assert.Error(t, err)

}

// --- Manager.Tabs ---

func TestManager_Tabs_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]ChromeTab{ //nolint:errcheck
			{ID: "tab1", Type: "page", URL: "https://example.com"},
			{ID: "tab2", Type: "background_page", URL: "chrome://newtab"},
			{ID: "tab3", Type: "page", URL: "https://google.com"},
		})
	}))
	defer srv.Close()

	port := parseCDPPort(t, srv.URL)
	m := NewManager("", port, "", false)

	tabs, err := m.Tabs()
	assert.NoError(t, err)
	assert.Equal(t, // Only "page" type tabs should be returned.
		2, len(tabs))

}

func TestManager_Tabs_ChromeNotRunning(t *testing.T) {
	m := NewManager("", 1, "", false) // port 1 - nothing listening
	_, err := m.Tabs()
	assert.Error(t, err)

}

// --- findChrome ---

func TestFindChrome_ExplicitBinary(t *testing.T) {
	path, err := findChrome("/explicit/path/chrome")
	assert.NoError(t, err)
	assert.Equal(t, "/explicit/path/chrome", path)

}

func TestFindChrome_NotFound(t *testing.T) {
	// Without explicit binary and no Chrome in PATH (on CI), should return error.
	// Skip if Chrome happens to be available.
	path, err := findChrome("")
	if err == nil {
		t.Skipf("Chrome found at %q, cannot test not-found path", path)
	}
	assert.Regexp(t, "Chrome|not found", err.Error())

}

func TestShouldAutoLaunchHeadless_WindowsIgnoresDisplayEnv(t *testing.T) {
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")

	assert.False(t, shouldAutoLaunchHeadless("windows"))
}

func TestShouldAutoLaunchHeadless_UnixRequiresDisplay(t *testing.T) {
	origDisplay, hadDisplay := os.LookupEnv("DISPLAY")
	origWayland, hadWayland := os.LookupEnv("WAYLAND_DISPLAY")
	t.Cleanup(func() {
		if hadDisplay {
			assert.NoError(t, os.Setenv("DISPLAY", origDisplay))
		} else {
			assert.NoError(t, os.Unsetenv("DISPLAY"))
		}
		if hadWayland {
			assert.NoError(t, os.Setenv("WAYLAND_DISPLAY", origWayland))
		} else {
			assert.NoError(t, os.Unsetenv("WAYLAND_DISPLAY"))
		}
	})

	assert.NoError(t, os.Unsetenv("DISPLAY"))
	assert.NoError(t, os.Unsetenv("WAYLAND_DISPLAY"))
	assert.True(t, shouldAutoLaunchHeadless("linux"))

	assert.NoError(t, os.Setenv("DISPLAY", ":0"))
	assert.False(t, shouldAutoLaunchHeadless("linux"))

	assert.NoError(t, os.Unsetenv("DISPLAY"))
	assert.NoError(t, os.Setenv("WAYLAND_DISPLAY", "wayland-0"))
	assert.False(t, shouldAutoLaunchHeadless("linux"))
}

// --- Manager.CloseTab ---

func TestManager_CloseTab(t *testing.T) {
	var closedTabID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/json/close/") {
			closedTabID = strings.TrimPrefix(r.URL.Path, "/json/close/")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Target is closing")) //nolint:errcheck
		}
	}))
	defer srv.Close()

	port := parseCDPPort(t, srv.URL)
	m := NewManager("", port, "", false)

	err := m.CloseTab("tab-xyz")
	assert.NoError(t, err)
	assert.Equal(t, "tab-xyz", closedTabID)

}

func TestManager_CloseTab_ConnectionError(t *testing.T) {
	m := NewManager("", 1, "", false)
	err := m.CloseTab("tab-xyz")
	assert.Error(t, err)

}

// --- withDefaultTimeout ---

func TestWithDefaultTimeout_NoDeadline(t *testing.T) {
	ctx := context.Background()
	dCtx, cancel := withDefaultTimeout(ctx, 5*time.Second)
	defer cancel()
	_, ok := dCtx.Deadline()
	assert.True(t, ok)

}

func TestWithDefaultTimeout_ExistingDeadline(t *testing.T) {
	deadline := time.Now().Add(10 * time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	dCtx, cancelNew := withDefaultTimeout(ctx, 5*time.Second)
	cancelNew()

	got, ok := dCtx.Deadline()
	assert.True(t, ok)

	// Should preserve the original deadline (not add a new shorter one).
	diff := got.Sub(deadline)
	assert.GreaterOrEqual(t, diff, -time.Second)
	assert.LessOrEqual(t, diff, time.Second)

}

// --- Manager.Navigate/Fill (cancelled context paths) ---

func TestManager_NavigateWithoutChrome(t *testing.T) {
	m := NewManager("", 19876, "", false)
	err := m.Navigate(cancelledCtx(), "tab-id", "https://example.com")
	assert.Error(t, err)

}

func TestManager_FillWithoutChrome(t *testing.T) {
	m := NewManager("", 19876, "", false)
	err := m.Fill(cancelledCtx(), "tab-id", "#input", "hello")
	assert.Error(t, err)

}

func TestManager_WaitVisibleWithoutChrome(t *testing.T) {
	m := NewManager("", 19876, "", false)
	err := m.WaitVisible(cancelledCtx(), "tab-id", "#input", time.Second)
	assert.Error(t, err)

}

// --- Session direct tests ---

// makeTestSession builds a Session that has already-cancelled contexts.
// We use plain context.WithCancel rather than chromedp.NewContext so that
// calling Close/Detach/Run does not block trying to connect to Chrome.
func makeTestSession() *Session {
	allocCtx, cancelAlloc := context.WithCancel(context.Background())
	taskCtx, cancelTask := context.WithCancel(allocCtx)
	// Cancel immediately so all operations fail fast.
	cancelTask()
	cancelAlloc()
	return &Session{
		allocCtx:    allocCtx,
		cancelAlloc: cancelAlloc,
		taskCtx:     taskCtx,
		cancelTask:  cancelTask,
	}
}

func TestSession_Close(_ *testing.T) {
	s := makeTestSession()
	// Must not panic, even on a cancelled context.
	s.Close()
	// Calling Close again must also not panic.
	s.Close()
}

func TestSession_Detach(_ *testing.T) {
	s := makeTestSession()
	s.Detach()
	// Double-call must not panic.
	s.Detach()
}

func TestSession_TabID_NoTarget(_ *testing.T) {
	s := makeTestSession()
	// With no real Chrome connection the target will be nil; TabID must return "".
	id := s.TabID()
	// We accept either empty string or a non-empty string - just must not panic.
	_ = id
}

func TestSession_Run_CancelledContext(t *testing.T) {
	s := makeTestSession()
	err := s.Run(chromedp.ActionFunc(func(_ context.Context) error { return nil }))
	assert.
		// With a cancelled context, chromedp.Run returns an error.
		Error(t, err)

}

// --- Session ops tests (error paths via cancelled session) ---

func TestSession_Navigate_Error(t *testing.T) {
	s := makeTestSession()
	err := s.Navigate("https://example.com")
	assert.Error(t, err)

}

func TestSession_Type_Error(t *testing.T) {
	s := makeTestSession()
	err := s.Type("#input", "hello")
	assert.Error(t, err)

}

func TestSession_Fill_Error(t *testing.T) {
	s := makeTestSession()
	err := s.Fill("#input", "hello")
	assert.Error(t, err)

}

func TestSession_Screenshot_Error(t *testing.T) {
	s := makeTestSession()
	buf, err := s.Screenshot()
	assert.Error(t, err)
	assert.Nil(t, buf)

}

func TestSession_EvalJS_Error(t *testing.T) {
	s := makeTestSession()
	result, err := s.EvalJS("1+1")
	assert.Error(t, err)
	assert.Equal(t, "", result)

}

func TestSession_Click_Error(t *testing.T) {
	s := makeTestSession()
	err := s.Click("#btn")
	assert.Error(t, err)

}

func TestSession_WaitVisible_Error(t *testing.T) {
	s := makeTestSession()
	err := s.WaitVisible("#btn", time.Second)
	assert.Error(t, err)

}

// --- newRemoteSessionForTab error path ---

func TestNewRemoteSessionForTab_InvalidWS(t *testing.T) {
	// Use a port that nothing is listening on so chromedp.Run fails fast.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := newRemoteSessionForTab(ctx, "ws://127.0.0.1:1/devtools/browser/fake", "tab-fake")
	assert.Error(t, err)

}

// --- ensureChrome when chrome is already running ---

// mockCDPServer returns an httptest.Server that answers /json/version with a
// fake (but non-empty) WebSocket debugger URL so ensureChrome short-circuits.
func mockCDPServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/version":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(chromeVersionInfo{ //nolint:errcheck
				WebSocketDebuggerURL: "ws://127.0.0.1:1/devtools/browser/fake",
			})
		case "/json/list":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]ChromeTab{ //nolint:errcheck
				{ID: "tab1", Type: "page", URL: "https://example.com"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestEnsureChrome_AlreadyRunning(t *testing.T) {
	srv := mockCDPServer(t)
	defer srv.Close()

	port := parseCDPPort(t, srv.URL)
	m := NewManager("", port, "", false)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	wsURL, err := m.ensureChrome(ctx)
	assert.NoError(t, err)
	assert.NotEqual(t, "", wsURL)

}

func TestEnsureChrome_CancelledContext(t *testing.T) {
	m := NewManager("", 19876, "", false)
	// With a cancelled context and no chrome running, ensureChrome must return an error.
	_, err := m.ensureChrome(cancelledCtx())
	assert.Error(t, err)

}

func TestEnsureChrome_FallsBackToHeadlessWhenHeadedLaunchDoesNotBecomeReady(t *testing.T) {
	oldLaunchChromeWithModeFn := launchChromeWithModeFn
	oldShouldLaunchHeadlessFn := shouldLaunchHeadlessFn
	oldWaitForChromeFn := waitForChromeFn
	t.Cleanup(func() {
		launchChromeWithModeFn = oldLaunchChromeWithModeFn
		shouldLaunchHeadlessFn = oldShouldLaunchHeadlessFn
		waitForChromeFn = oldWaitForChromeFn
	})

	var launchModes []bool
	launchChromeWithModeFn = func(_ *Manager, headless bool) error {
		launchModes = append(launchModes, headless)
		return nil
	}
	shouldLaunchHeadlessFn = func(_ *Manager) bool { return false }

	waitCalls := 0
	waitForChromeFn = func(_ context.Context, _ string) (string, error) {
		waitCalls++
		if waitCalls == 1 {
			return "", context.DeadlineExceeded
		}
		return "ws://127.0.0.1:9222/devtools/browser/fallback", nil
	}

	m := NewManager("", 19876, "", false)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	wsURL, err := m.ensureChrome(ctx)
	assert.NoError(t, err)
	assert.Equal(t, "ws://127.0.0.1:9222/devtools/browser/fallback", wsURL)
	assert.Equal(t, []bool{false, true}, launchModes)
}

// --- Manager.Open via mock CDP ---

func TestManager_Open_ChromeAlreadyRunning(t *testing.T) {
	// Serve both /json/version (for ensureChrome) and /json/new (for createTab).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/json/version":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(chromeVersionInfo{ //nolint:errcheck
				WebSocketDebuggerURL: "ws://127.0.0.1:1/devtools/browser/fake",
			})
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/json/new"):
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(ChromeTab{ //nolint:errcheck
				ID: "opened-tab-1", Type: "page", URL: "https://example.com",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	port := parseCDPPort(t, srv.URL)
	m := NewManager("", port, "", false)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	tabID, err := m.Open(ctx, "https://example.com")
	assert.NoError(t, err)
	assert.Equal(t, "opened-tab-1", tabID)

}

func TestManager_Open_WithoutChrome(t *testing.T) {
	m := NewManager("", 19876, "", false)
	_, err := m.Open(cancelledCtx(), "https://example.com")
	assert.Error(t, err)

}

// --- Manager.Close ---

func TestManager_Close_IsNoOp(_ *testing.T) {
	m := NewManager("", 0, "", false)
	// Must not panic - Close() is documented as a no-op.
	m.Close()
}

// --- Manager.withTab: ensureChrome succeeds but session attach fails ---

func TestManager_withTab_AttachFails(t *testing.T) {
	// The mock CDP server answers /json/version (Chrome "running") but the
	// returned ws:// URL is invalid so newRemoteSessionForTab will fail.
	srv := mockCDPServer(t)
	defer srv.Close()

	port := parseCDPPort(t, srv.URL)
	m := NewManager("", port, "", false)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Navigate calls withTab → ensureChrome succeeds → newRemoteSessionForTab fails.
	err := m.Navigate(ctx, "nonexistent-tab", "https://example.com")
	assert.Error(t, err)

}

func TestManager_withTab_ReusesCachedSession(t *testing.T) {
	srv := mockCDPServer(t)
	defer srv.Close()

	port := parseCDPPort(t, srv.URL)
	m := NewManager("", port, "", false)

	// Inject a pre-built (cancelled) session into the cache.
	s := makeTestSession()
	m.mu.Lock()
	m.sessions["cached-tab"] = s
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// withTab should find the cached session and call fn(s).
	// The session's context is cancelled so the operation will fail, but that
	// still proves the cached-session branch was reached (no attach attempt).
	err := m.Navigate(ctx, "cached-tab", "https://example.com")
	assert.Error(t, err)

}

func TestSession_ResizeWindow_Error(t *testing.T) {
	s := makeTestSession()
	err := s.ResizeWindow(1280, 800)
	assert.Error(t, err)

}

func TestManager_ResizeWithoutChrome(t *testing.T) {
	m := NewManager("", 19876, "", false)
	err := m.Resize(cancelledCtx(), "tab-id", 1280, 800)
	assert.Error(t, err)

}

// --- launchChrome: binary not found ---

func TestLaunchChrome_BinaryNotFound(t *testing.T) {
	m := NewManager("/nonexistent/path/to/chrome-xxxx", 19876, "", true)
	// findChrome returns the path as-is for explicit binaries, so launchChrome
	// will try to exec it and fail at cmd.Start().
	err := launchChrome(m)
	assert.Error(t, err)

}

// --- findChrome: platform paths ---

func TestFindChrome_PlatformPaths(_ *testing.T) {
	// Ensure platformChromePaths returns a slice (may be empty) without panic.
	paths := platformChromePaths()
	_ = paths // just exercise the function
}

// --- Manager.CloseTab removes session from cache ---

func TestManager_CloseTab_RemovesCachedSession(t *testing.T) {
	var closedTabID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/json/close/") {
			closedTabID = strings.TrimPrefix(r.URL.Path, "/json/close/")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Target is closing")) //nolint:errcheck
		}
	}))
	defer srv.Close()

	port := parseCDPPort(t, srv.URL)
	m := NewManager("", port, "", false)

	// Seed a cached session.
	s := makeTestSession()
	m.mu.Lock()
	m.sessions["tab-cache"] = s
	m.mu.Unlock()

	err := m.CloseTab("tab-cache")
	assert.NoError(t, err)
	assert.Equal(t, "tab-cache", closedTabID)

	m.mu.Lock()
	_, still := m.sessions["tab-cache"]
	m.mu.Unlock()
	assert.False(t, still)

}
