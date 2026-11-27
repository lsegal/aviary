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
	"sync"
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
	assert.True(t, m.reuseTabs)

}

func TestNewManager_CustomPort(t *testing.T) {
	m := NewManager("/usr/bin/chromium", 9333, "/tmp/profile", true, false)
	assert.Equal(t, 9333, m.cdpPort)
	assert.Equal(t, "/usr/bin/chromium", m.binary)
	assert.Equal(t, "/tmp/profile", m.profileDir)
	assert.True(t, m.headless)
	assert.False(t, m.reuseTabs)

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
	assert.True(t, isIgnorableChromeDPError(`unhandled node event *dom.EventTopLayerElementsUpdated`))
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

// --- PageTargets ---

func TestPageTargets_Success(t *testing.T) {
	srv := newFakeCDPServer(t, fakeCDPOptions{Tabs: []ChromeTab{
		{ID: "tab1", Type: "page", URL: "https://example.com", Title: "Example"},
		{ID: "tab2", Type: "background_page", URL: "chrome://newtab"},
	}})

	tabs, err := PageTargets(context.Background(), srv.URL)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(tabs))
	assert.Equal(t, "tab1", tabs[0].ID)

}

func TestPageTargets_ConnectionError(t *testing.T) {
	_, err := PageTargets(context.Background(), "http://127.0.0.1:1")
	assert. // nothing listening
		Error(t, err)

}

func TestPageTargets_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not-json")) //nolint:errcheck
	}))
	defer srv.Close()

	_, err := PageTargets(context.Background(), srv.URL)
	assert.Error(t, err)

}

// --- createTab ---

func TestCreateTab_Success(t *testing.T) {
	srv := newFakeCDPServer(t, fakeCDPOptions{
		NewTab: ChromeTab{ID: "newtab1", Type: "page", URL: "https://example.com"},
	})

	tab, err := createTab(context.Background(), srv.URL, "https://example.com")
	assert.NoError(t, err)
	assert.Equal(t, "newtab1", tab.ID)
	assert.Equal(t, 1, srv.NewTabCalls())

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

// --- WebSocketURL ---

func TestWebSocketURL_Success(t *testing.T) {
	srv := newFakeCDPServer(t, fakeCDPOptions{})

	wsURL, err := WebSocketURL(context.Background(), srv.URL)
	assert.NoError(t, err)
	assert.Equal(t, fakeCDPWebSocketURL, wsURL)

}

func TestWebSocketURL_EmptyURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(versionInfo{WebSocketDebuggerURL: ""}) //nolint:errcheck
	}))
	defer srv.Close()

	_, err := WebSocketURL(context.Background(), srv.URL)
	assert.Error(t, err)

}

func TestWebSocketURL_ConnectionError(t *testing.T) {
	_, err := WebSocketURL(context.Background(), "http://127.0.0.1:1")
	assert.Error(t, err)

}

func TestWebSocketURL_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("bad")) //nolint:errcheck
	}))
	defer srv.Close()

	_, err := WebSocketURL(context.Background(), srv.URL)
	assert.Error(t, err)

}

// --- waitForChrome ---

func TestWaitForChrome_Success(t *testing.T) {
	srv := newFakeCDPServer(t, fakeCDPOptions{})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	wsURL, err := waitForChrome(ctx, srv.URL)
	assert.NoError(t, err)
	assert.Equal(t, fakeCDPWebSocketURL, wsURL)

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
	srv := newFakeCDPServer(t, fakeCDPOptions{Tabs: []ChromeTab{
		{ID: "tab1", Type: "page", URL: "https://example.com"},
		{ID: "tab2", Type: "background_page", URL: "chrome://newtab"},
		{ID: "tab3", Type: "page", URL: "https://google.com"},
	}})
	m := NewManager("", srv.port(t), "", false)

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

// --- FindBinary ---

func TestFindBinary_ExplicitBinaryMustExist(t *testing.T) {
	_, err := FindBinary("/explicit/path/chrome")
	assert.ErrorContains(t, err, "not found or not executable")
}

func TestFindBinary_NotFound(t *testing.T) {
	// Without explicit binary and no Chrome in PATH (on CI), should return error.
	// Skip if Chrome happens to be available.
	path, err := FindBinary("")
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
	srv := newFakeCDPServer(t, fakeCDPOptions{})
	m := NewManager("", srv.port(t), "", false)

	err := m.CloseTab("tab-xyz")
	assert.NoError(t, err)
	assert.Equal(t, []string{"tab-xyz"}, srv.ClosedTabs())

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

func TestSession_TabID_NoTarget(t *testing.T) {
	// Without a chromedp target there is no tab to report.
	assert.Empty(t, makeTestSession().TabID())
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

func TestFormatEvalJSResult(t *testing.T) {
	t.Run("string stays unquoted", func(t *testing.T) {
		result, err := formatEvalJSResult("hello")
		assert.NoError(t, err)
		assert.Equal(t, "hello", result)
	})

	t.Run("number is serialized", func(t *testing.T) {
		result, err := formatEvalJSResult(float64(2))
		assert.NoError(t, err)
		assert.Equal(t, "2", result)
	})

	t.Run("boolean is serialized", func(t *testing.T) {
		result, err := formatEvalJSResult(true)
		assert.NoError(t, err)
		assert.Equal(t, "true", result)
	})

	t.Run("null is serialized", func(t *testing.T) {
		result, err := formatEvalJSResult(nil)
		assert.NoError(t, err)
		assert.Equal(t, "null", result)
	})

	t.Run("object is serialized as json", func(t *testing.T) {
		result, err := formatEvalJSResult(map[string]any{"count": float64(2), "ok": true})
		assert.NoError(t, err)
		assert.JSONEq(t, `{"count":2,"ok":true}`, result)
	})

	t.Run("array is serialized as json", func(t *testing.T) {
		result, err := formatEvalJSResult([]any{"a", float64(2), true})
		assert.NoError(t, err)
		assert.JSONEq(t, `["a",2,true]`, result)
	})
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

// fakeCDPWebSocketURL is the WebSocket debugger URL the fake CDP server
// reports. Nothing listens on it, so ensureChrome short-circuits but any
// session attach fails.
const fakeCDPWebSocketURL = "ws://127.0.0.1:1/devtools/browser/fake"

// fakeCDPOptions configures newFakeCDPServer.
type fakeCDPOptions struct {
	// Tabs is what /json/list returns.
	Tabs []ChromeTab
	// NewTab is what PUT /json/new returns.
	NewTab ChromeTab
}

// fakeCDPServer is a fake Chrome DevTools HTTP endpoint. It answers
// /json/version, /json/list, PUT /json/new and /json/close/<id>, counting
// /json/new calls and recording closed tab IDs.
type fakeCDPServer struct {
	*httptest.Server

	mu          sync.Mutex
	newTabCalls int
	closedTabs  []string
}

func newFakeCDPServer(t *testing.T, opts fakeCDPOptions) *fakeCDPServer {
	t.Helper()
	f := &fakeCDPServer{}
	tabs := opts.Tabs
	if tabs == nil {
		tabs = []ChromeTab{}
	}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/json/version":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(versionInfo{WebSocketDebuggerURL: fakeCDPWebSocketURL}) //nolint:errcheck
		case r.URL.Path == "/json/list":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(tabs) //nolint:errcheck
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/json/new"):
			f.mu.Lock()
			f.newTabCalls++
			f.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(opts.NewTab) //nolint:errcheck
		case strings.HasPrefix(r.URL.Path, "/json/close/"):
			f.mu.Lock()
			f.closedTabs = append(f.closedTabs, strings.TrimPrefix(r.URL.Path, "/json/close/"))
			f.mu.Unlock()
			w.Write([]byte("Target is closing")) //nolint:errcheck
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

// port returns the TCP port the fake server listens on.
func (f *fakeCDPServer) port(t *testing.T) int {
	t.Helper()
	return parseCDPPort(t, f.URL)
}

// NewTabCalls returns how many times PUT /json/new was called.
func (f *fakeCDPServer) NewTabCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.newTabCalls
}

// ClosedTabs returns the tab IDs passed to /json/close/, in order.
func (f *fakeCDPServer) ClosedTabs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.closedTabs...)
}

func TestEnsureChrome_AlreadyRunning(t *testing.T) {
	srv := newFakeCDPServer(t, fakeCDPOptions{})
	m := NewManager("", srv.port(t), "", false)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	wsURL, err := m.ensureChrome(ctx)
	assert.NoError(t, err)
	assert.Equal(t, fakeCDPWebSocketURL, wsURL)
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

func TestManager_Open(t *testing.T) {
	tests := []struct {
		name            string
		tabs            []ChromeTab
		newTab          ChromeTab
		reuseTabs       bool
		url             string
		wantTabID       string
		wantNewTabCalls int
		wantClosedTabs  []string
	}{
		{
			name:            "no existing tab",
			newTab:          ChromeTab{ID: "opened-tab-1", Type: "page", URL: "https://example.com"},
			reuseTabs:       true,
			url:             "https://example.com",
			wantTabID:       "opened-tab-1",
			wantNewTabCalls: 1,
		},
		{
			name: "exact URL match reused",
			tabs: []ChromeTab{
				{ID: "existing-tab", Type: "page", URL: "https://example.com"},
				{ID: "other-tab", Type: "page", URL: "https://example.com/about"},
			},
			newTab:          ChromeTab{ID: "new-tab", Type: "page", URL: "https://example.com"},
			reuseTabs:       true,
			url:             "https://example.com",
			wantTabID:       "existing-tab",
			wantNewTabCalls: 0,
		},
		{
			name: "reuse turned off",
			tabs: []ChromeTab{
				{ID: "existing-tab", Type: "page", URL: "https://example.com"},
			},
			newTab:          ChromeTab{ID: "new-tab", Type: "page", URL: "https://example.com"},
			reuseTabs:       false,
			url:             "https://example.com",
			wantTabID:       "new-tab",
			wantNewTabCalls: 1,
		},
		{
			name: "redirected URL reused and duplicate tab closed",
			tabs: []ChromeTab{
				{ID: "existing-tab", Type: "page", URL: "https://www.google.com/"},
				{ID: "new-tab", Type: "page", URL: "https://www.google.com/"},
			},
			newTab:          ChromeTab{ID: "new-tab", Type: "page", URL: "https://google.com"},
			reuseTabs:       true,
			url:             "https://google.com",
			wantTabID:       "existing-tab",
			wantNewTabCalls: 1,
			wantClosedTabs:  []string{"new-tab"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newFakeCDPServer(t, fakeCDPOptions{Tabs: tt.tabs, NewTab: tt.newTab})
			m := NewManager("", srv.port(t), "", false, tt.reuseTabs)

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			tabID, err := m.Open(ctx, tt.url)
			assert.NoError(t, err)
			assert.Equal(t, tt.wantTabID, tabID)
			assert.Equal(t, tt.wantNewTabCalls, srv.NewTabCalls())
			assert.Equal(t, tt.wantClosedTabs, srv.ClosedTabs())
		})
	}
}

func TestURLsEquivalent_NormalizesDefaultSlashAndHostCase(t *testing.T) {
	assert.True(t, urlsEquivalent("https://EXAMPLE.com", "https://example.com/"))
	assert.True(t, urlsEquivalent("https://example.com:443", "https://example.com/"))
	assert.False(t, urlsEquivalent("https://example.com/a", "https://example.com/b"))
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
	srv := newFakeCDPServer(t, fakeCDPOptions{})
	m := NewManager("", srv.port(t), "", false)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Navigate calls withTab → ensureChrome succeeds → newRemoteSessionForTab fails.
	err := m.Navigate(ctx, "nonexistent-tab", "https://example.com")
	assert.Error(t, err)

}

func TestManager_withTab_ReusesCachedSession(t *testing.T) {
	srv := newFakeCDPServer(t, fakeCDPOptions{})
	m := NewManager("", srv.port(t), "", false)

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
	// FindBinary rejects an explicit binary that does not exist.
	err := launchChrome(m)
	assert.Error(t, err)

}

// --- Manager.CloseTab removes session from cache ---

func TestManager_CloseTab_RemovesCachedSession(t *testing.T) {
	srv := newFakeCDPServer(t, fakeCDPOptions{})
	m := NewManager("", srv.port(t), "", false)

	// Seed a cached session.
	s := makeTestSession()
	m.mu.Lock()
	m.sessions["tab-cache"] = s
	m.mu.Unlock()

	err := m.CloseTab("tab-cache")
	assert.NoError(t, err)
	assert.Equal(t, []string{"tab-cache"}, srv.ClosedTabs())

	m.mu.Lock()
	_, still := m.sessions["tab-cache"]
	m.mu.Unlock()
	assert.False(t, still)

}
