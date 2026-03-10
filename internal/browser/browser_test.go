package browser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/config"
)

func TestNewManager_Defaults(t *testing.T) {
	m := NewManager("", 0, "", false)
	if m.cdpPort != 9222 {
		t.Fatalf("expected default cdpPort 9222, got %d", m.cdpPort)
	}
	if m.binary != "" {
		t.Fatalf("expected empty binary, got %q", m.binary)
	}
}

func TestNewManager_CustomPort(t *testing.T) {
	m := NewManager("/usr/bin/chromium", 9333, "/tmp/profile", true)
	if m.cdpPort != 9333 {
		t.Fatalf("expected cdpPort 9333, got %d", m.cdpPort)
	}
	if m.binary != "/usr/bin/chromium" {
		t.Fatalf("expected binary /usr/bin/chromium, got %q", m.binary)
	}
	if m.profileDir != "/tmp/profile" {
		t.Fatalf("expected profileDir /tmp/profile, got %q", m.profileDir)
	}
	if !m.headless {
		t.Fatal("expected headless=true")
	}
}

func TestManager_UserDataDirDefault(t *testing.T) {
	m := NewManager("", 0, "", false)
	got := m.userDataDir()
	want := filepath.Join(filepath.Dir(config.DefaultPath()), "browser")
	if got != want {
		t.Fatalf("expected default user data dir %q, got %q", want, got)
	}
	// Explicit profileDir is used as-is.
	m2 := NewManager("", 0, "/tmp/my-profile", false)
	if m2.userDataDir() != "/tmp/my-profile" {
		t.Fatalf("expected '/tmp/my-profile', got %q", m2.userDataDir())
	}
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
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestManager_TypeWithoutChrome(t *testing.T) {
	m := NewManager("", 19876, "", false)
	err := m.Type(cancelledCtx(), "tab-id", "#input", "hello")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestManager_ScreenshotWithoutChrome(t *testing.T) {
	m := NewManager("", 19876, "", false)
	buf, err := m.Screenshot(cancelledCtx(), "tab-id")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if buf != nil {
		t.Fatal("expected nil bytes on error")
	}
}

func TestManager_EvalJSWithoutChrome(t *testing.T) {
	m := NewManager("", 19876, "", false)
	result, err := m.EvalJS(cancelledCtx(), "tab-id", "1+1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if result != "" {
		t.Fatal("expected empty result on error")
	}
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
	if err != nil {
		t.Fatalf("parse URL %q: %v", srvURL, err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("port from %q: %v", srvURL, err)
	}
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
	if err != nil {
		t.Fatalf("fetchTabs: %v", err)
	}
	if len(tabs) != 2 {
		t.Fatalf("expected 2 tabs, got %d", len(tabs))
	}
	if tabs[0].ID != "tab1" {
		t.Errorf("expected tab1, got %q", tabs[0].ID)
	}
}

func TestFetchTabs_ConnectionError(t *testing.T) {
	_, err := fetchTabs("http://127.0.0.1:1") // nothing listening
	if err == nil {
		t.Fatal("expected error for connection refused")
	}
}

func TestFetchTabs_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not-json")) //nolint:errcheck
	}))
	defer srv.Close()

	_, err := fetchTabs(srv.URL)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
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
	if err != nil {
		t.Fatalf("createTab: %v", err)
	}
	if tab.ID != "newtab1" {
		t.Errorf("expected newtab1, got %q", tab.ID)
	}
}

func TestCreateTab_EmptyID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChromeTab{ID: "", Type: "page"}) //nolint:errcheck
	}))
	defer srv.Close()

	_, err := createTab(context.Background(), srv.URL, "https://example.com")
	if err == nil {
		t.Fatal("expected error for empty tab ID")
	}
}

func TestCreateTab_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := createTab(context.Background(), srv.URL, "https://example.com")
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
}

func TestCreateTab_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not-json")) //nolint:errcheck
	}))
	defer srv.Close()

	_, err := createTab(context.Background(), srv.URL, "https://example.com")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
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
	if err != nil {
		t.Fatalf("fetchWebSocketURL: %v", err)
	}
	if !strings.HasPrefix(wsURL, "ws://") {
		t.Errorf("expected ws:// URL, got %q", wsURL)
	}
}

func TestFetchWebSocketURL_EmptyURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(chromeVersionInfo{WebSocketDebuggerURL: ""}) //nolint:errcheck
	}))
	defer srv.Close()

	_, err := fetchWebSocketURL(srv.URL)
	if err == nil {
		t.Fatal("expected error for empty WebSocket URL")
	}
}

func TestFetchWebSocketURL_ConnectionError(t *testing.T) {
	_, err := fetchWebSocketURL("http://127.0.0.1:1")
	if err == nil {
		t.Fatal("expected error for connection refused")
	}
}

func TestFetchWebSocketURL_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("bad")) //nolint:errcheck
	}))
	defer srv.Close()

	_, err := fetchWebSocketURL(srv.URL)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// --- waitForChrome ---

func TestWaitForChrome_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(chromeVersionInfo{ //nolint:errcheck
			WebSocketDebuggerURL: "ws://localhost:9222/devtools/browser/ready",
		})
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	wsURL, err := waitForChrome(ctx, srv.URL)
	if err != nil {
		t.Fatalf("waitForChrome: %v", err)
	}
	if wsURL == "" {
		t.Error("expected non-empty wsURL")
	}
}

func TestWaitForChrome_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	_, err := waitForChrome(ctx, srv.URL)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

// --- Manager.Tabs ---

func TestManager_Tabs_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	if err != nil {
		t.Fatalf("Tabs: %v", err)
	}
	// Only "page" type tabs should be returned.
	if len(tabs) != 2 {
		t.Fatalf("expected 2 page tabs, got %d", len(tabs))
	}
}

func TestManager_Tabs_ChromeNotRunning(t *testing.T) {
	m := NewManager("", 1, "", false) // port 1 - nothing listening
	_, err := m.Tabs()
	if err == nil {
		t.Fatal("expected error when Chrome not running")
	}
}

// --- findChrome ---

func TestFindChrome_ExplicitBinary(t *testing.T) {
	path, err := findChrome("/explicit/path/chrome")
	if err != nil {
		t.Fatalf("findChrome explicit: %v", err)
	}
	if path != "/explicit/path/chrome" {
		t.Errorf("expected path as-is, got %q", path)
	}
}

func TestFindChrome_NotFound(t *testing.T) {
	// Without explicit binary and no Chrome in PATH (on CI), should return error.
	// Skip if Chrome happens to be available.
	path, err := findChrome("")
	if err == nil {
		t.Skipf("Chrome found at %q, cannot test not-found path", path)
	}
	if !strings.Contains(err.Error(), "Chrome") && !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected chrome not found error, got: %v", err)
	}
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
	if err != nil {
		t.Fatalf("CloseTab: %v", err)
	}
	if closedTabID != "tab-xyz" {
		t.Errorf("expected tab-xyz, got %q", closedTabID)
	}
}

func TestManager_CloseTab_ConnectionError(t *testing.T) {
	m := NewManager("", 1, "", false)
	err := m.CloseTab("tab-xyz")
	if err == nil {
		t.Fatal("expected error when Chrome not running")
	}
}

// --- withDefaultTimeout ---

func TestWithDefaultTimeout_NoDeadline(t *testing.T) {
	ctx := context.Background()
	dCtx, cancel := withDefaultTimeout(ctx, 5*time.Second)
	defer cancel()
	_, ok := dCtx.Deadline()
	if !ok {
		t.Error("expected deadline to be set when ctx has none")
	}
}

func TestWithDefaultTimeout_ExistingDeadline(t *testing.T) {
	deadline := time.Now().Add(10 * time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	dCtx, cancelNew := withDefaultTimeout(ctx, 5*time.Second)
	cancelNew()

	got, ok := dCtx.Deadline()
	if !ok {
		t.Error("expected deadline")
	}
	// Should preserve the original deadline (not add a new shorter one).
	diff := got.Sub(deadline)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("expected original deadline preserved, got diff=%v", diff)
	}
}

// --- Manager.Navigate/Fill (cancelled context paths) ---

func TestManager_NavigateWithoutChrome(t *testing.T) {
	m := NewManager("", 19876, "", false)
	err := m.Navigate(cancelledCtx(), "tab-id", "https://example.com")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestManager_FillWithoutChrome(t *testing.T) {
	m := NewManager("", 19876, "", false)
	err := m.Fill(cancelledCtx(), "tab-id", "#input", "hello")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
