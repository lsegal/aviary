package browser

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/config"
)

// Manager manages browser sessions via Chrome DevTools Protocol.
// Each operation connects to Chrome on demand — no persistent in-process session
// is held between calls, which allows CLI invocations to share a single Chrome
// process via tab IDs.
type Manager struct {
	mu         sync.Mutex // protects Chrome launch
	binary     string
	cdpPort    int
	profileDir string
	headless   bool
	reuseTabs  bool
	sessions   map[string]*Session
}

const defaultOperationTimeout = 15 * time.Second

// NewManager creates a browser Manager.
// binary is the path to the Chromium/Chrome executable (empty = auto-detect).
// cdpPort is the remote debugging port (0 = default 9222).
// profileDir is the Chrome profile-directory name (empty = "Default").
// headless controls whether the browser window is shown (false = visible).
// reuseTabs controls whether Open reuses an existing tab when the URL matches exactly.
// When reuseTabs is omitted it defaults to true.
func NewManager(binary string, cdpPort int, profileDir string, headless bool, reuseTabs ...bool) *Manager {
	if cdpPort == 0 {
		cdpPort = 9222
	}
	shouldReuseTabs := true
	if len(reuseTabs) > 0 {
		shouldReuseTabs = reuseTabs[0]
	}
	return &Manager{binary: binary, cdpPort: cdpPort, profileDir: profileDir, headless: headless, reuseTabs: shouldReuseTabs, sessions: make(map[string]*Session)}
}

// Open navigates to url in a new Chrome tab, launching Chrome if necessary.
// It returns the CDP target ID of the new tab, which callers must pass to
// subsequent operations (Click, Type, Screenshot, EvalJS) to target that tab.
// Chrome and the tab persist after this call returns.
func (m *Manager) Open(ctx context.Context, url string) (string, error) {
	opCtx, cancel := withDefaultTimeout(ctx, defaultOperationTimeout)
	defer cancel()

	if _, err := m.ensureChrome(opCtx); err != nil {
		return "", err
	}

	if m.reuseTabs {
		if tabID, ok := m.findReusableTab(url); ok {
			slog.Info("browser: reused tab", "url", url, "tab", tabID)
			return tabID, nil
		}
	}

	cdpBaseURL := fmt.Sprintf("http://localhost:%d", m.cdpPort)
	tab, err := createTab(opCtx, cdpBaseURL, url)
	if err != nil {
		return "", err
	}

	if m.reuseTabs {
		if tabID, ok := m.findReusableTabAfterOpen(tab.ID, url); ok {
			if err := m.CloseTab(tab.ID); err != nil {
				slog.Warn("browser: failed to close duplicate tab", "tab", tab.ID, "err", err)
			}
			slog.Info("browser: reused redirected tab", "requested_url", url, "resolved_tab", tab.ID, "tab", tabID)
			return tabID, nil
		}
	}

	slog.Info("browser: tab opened", "url", url, "tab", tab.ID)
	return tab.ID, nil
}

func (m *Manager) findReusableTab(requestedURL string) (string, bool) {
	tabs, err := m.Tabs()
	if err != nil {
		return "", false
	}
	for _, tab := range tabs {
		if urlsEquivalent(tab.URL, requestedURL) {
			return tab.ID, true
		}
	}
	return "", false
}

func (m *Manager) findReusableTabAfterOpen(newTabID, requestedURL string) (string, bool) {
	tabs, err := m.Tabs()
	if err != nil {
		return "", false
	}

	resolvedURL := requestedURL
	for _, tab := range tabs {
		if tab.ID == newTabID && strings.TrimSpace(tab.URL) != "" {
			resolvedURL = tab.URL
			break
		}
	}

	for _, tab := range tabs {
		if tab.ID == newTabID {
			continue
		}
		if urlsEquivalent(tab.URL, requestedURL) || urlsEquivalent(tab.URL, resolvedURL) {
			return tab.ID, true
		}
	}
	return "", false
}

func urlsEquivalent(a, b string) bool {
	return normalizeBrowserURL(a) == normalizeBrowserURL(b)
}

func normalizeBrowserURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return trimmed
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	if (parsed.Scheme == "http" && parsed.Port() == "80") || (parsed.Scheme == "https" && parsed.Port() == "443") {
		parsed.Host = parsed.Hostname()
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	return parsed.String()
}

// Tabs returns all currently open page tabs in the browser.
// Returns an error if Chrome is not running.
func (m *Manager) Tabs() ([]ChromeTab, error) {
	cdpBaseURL := fmt.Sprintf("http://localhost:%d", m.cdpPort)
	tabs, err := fetchTabs(cdpBaseURL)
	if err != nil {
		return nil, fmt.Errorf("chrome not running on port %d", m.cdpPort)
	}
	var pages []ChromeTab
	for _, t := range tabs {
		if t.Type == "page" {
			pages = append(pages, t)
		}
	}
	return pages, nil
}

// Click clicks the element matching selector in the given tab.
func (m *Manager) Click(ctx context.Context, tabID, selector string) error {
	opCtx, cancel := withDefaultTimeout(ctx, defaultOperationTimeout)
	defer cancel()

	slog.Info("browser: click", "tab", tabID, "selector", selector)
	return m.withTab(opCtx, tabID, func(s *Session) error { return s.Click(selector) })
}

// Type types text into the element matching selector in the given tab.
func (m *Manager) Type(ctx context.Context, tabID, selector, text string) error {
	opCtx, cancel := withDefaultTimeout(ctx, defaultOperationTimeout)
	defer cancel()

	slog.Info("browser: type", "tab", tabID, "selector", selector)
	return m.withTab(opCtx, tabID, func(s *Session) error { return s.Type(selector, text) })
}

// Fill sets the value of the element matching selector in the given tab.
func (m *Manager) Fill(ctx context.Context, tabID, selector, text string) error {
	opCtx, cancel := withDefaultTimeout(ctx, defaultOperationTimeout)
	defer cancel()

	slog.Info("browser: fill", "tab", tabID, "selector", selector)
	return m.withTab(opCtx, tabID, func(s *Session) error { return s.Fill(selector, text) })
}

// WaitVisible waits until the element matching selector is visible in the given tab.
func (m *Manager) WaitVisible(ctx context.Context, tabID, selector string, timeout time.Duration) error {
	opCtx, cancel := withDefaultTimeout(ctx, defaultOperationTimeout)
	defer cancel()

	slog.Info("browser: wait_visible", "tab", tabID, "selector", selector, "timeout", timeout)
	return m.withTab(opCtx, tabID, func(s *Session) error { return s.WaitVisible(selector, timeout) })
}

// Screenshot captures the current page in the given tab as PNG bytes.
func (m *Manager) Screenshot(ctx context.Context, tabID string) ([]byte, error) {
	opCtx, cancel := withDefaultTimeout(ctx, defaultOperationTimeout)
	defer cancel()

	var buf []byte
	err := m.withTab(opCtx, tabID, func(s *Session) error {
		var e error
		buf, e = s.Screenshot()
		return e
	})
	return buf, err
}

// EvalJS evaluates JavaScript in the given tab and returns the result as a string.
func (m *Manager) EvalJS(ctx context.Context, tabID, expr string) (string, error) {
	opCtx, cancel := withDefaultTimeout(ctx, defaultOperationTimeout)
	defer cancel()

	var result string
	err := m.withTab(opCtx, tabID, func(s *Session) error {
		var e error
		result, e = s.EvalJS(expr)
		return e
	})
	return result, err
}

// Navigate navigates an existing tab to url, waiting for the page to load.
func (m *Manager) Navigate(ctx context.Context, tabID, url string) error {
	opCtx, cancel := withDefaultTimeout(ctx, defaultOperationTimeout)
	defer cancel()
	return m.withTab(opCtx, tabID, func(s *Session) error { return s.Navigate(url) })
}

// Resize resizes the browser window that contains the given tab, if supported
// by the browser's CDP implementation.
func (m *Manager) Resize(ctx context.Context, tabID string, width, height int) error {
	opCtx, cancel := withDefaultTimeout(ctx, defaultOperationTimeout)
	defer cancel()

	slog.Info("browser: resize", "tab", tabID, "width", width, "height", height)
	return m.withTab(opCtx, tabID, func(s *Session) error { return s.ResizeWindow(width, height) })
}

// CloseTab closes the given tab via the CDP /json/close endpoint and removes
// its cached session.
func (m *Manager) CloseTab(tabID string) error {
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/json/close/%s", m.cdpPort, tabID)) //nolint:noctx
	if err != nil {
		return err
	}
	resp.Body.Close() //nolint:errcheck
	m.mu.Lock()
	delete(m.sessions, tabID)
	m.mu.Unlock()
	return nil
}

// Close is a no-op: Chrome and its tabs run independently of this Manager.
func (m *Manager) Close() {}

// withTab attaches to tabID, runs fn, then disconnects (leaving the tab open).
func (m *Manager) withTab(ctx context.Context, tabID string, fn func(*Session) error) error {
	wsURL, err := m.ensureChrome(ctx)
	if err != nil {
		return fmt.Errorf("chrome not running: %w", err)
	}

	m.mu.Lock()
	s, ok := m.sessions[tabID]
	m.mu.Unlock()

	if !ok {
		attachCtx := context.WithoutCancel(ctx)
		s, err = newRemoteSessionForTab(attachCtx, wsURL, tabID)
		if err != nil {
			return err
		}
		m.mu.Lock()
		m.sessions[tabID] = s
		m.mu.Unlock()
	}

	return fn(s)
}

// ensureChrome returns the browser-level CDP WebSocket URL, launching Chrome if needed.
func (m *Manager) ensureChrome(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	cdpBaseURL := fmt.Sprintf("http://localhost:%d", m.cdpPort)
	if wsURL, err := fetchWebSocketURL(cdpBaseURL); err == nil {
		return wsURL, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	// Double-check after acquiring lock.
	if wsURL, err := fetchWebSocketURL(cdpBaseURL); err == nil {
		return wsURL, nil
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	headless := shouldLaunchHeadlessFn(m)
	wsURL, err := launchChromeAndWait(ctx, m, cdpBaseURL, headless)
	if err == nil || headless {
		return wsURL, err
	}
	if wsURL, retryErr := fetchWebSocketURL(cdpBaseURL); retryErr == nil {
		return wsURL, nil
	}

	slog.Warn("browser: headed launch failed; retrying in headless mode", "port", m.cdpPort, "err", err)
	return launchChromeAndWait(ctx, m, cdpBaseURL, true)
}

func withDefaultTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

// userDataDir returns the Chrome user data directory for Aviary's browser.
// If profileDir is set it is used as-is; otherwise a browser directory
// alongside aviary.yaml is used.
func (m *Manager) userDataDir() string {
	if m.profileDir != "" {
		return m.profileDir
	}
	return filepath.Join(filepath.Dir(config.DefaultPath()), "browser")
}
