package browser

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
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
	sessions   map[string]*Session
}

const defaultOperationTimeout = 15 * time.Second

// NewManager creates a browser Manager.
// binary is the path to the Chromium/Chrome executable (empty = auto-detect).
// cdpPort is the remote debugging port (0 = default 9222).
// profileDir is the Chrome profile-directory name (empty = "Default").
// headless controls whether the browser window is shown (false = visible).
func NewManager(binary string, cdpPort int, profileDir string, headless bool) *Manager {
	if cdpPort == 0 {
		cdpPort = 9222
	}
	return &Manager{binary: binary, cdpPort: cdpPort, profileDir: profileDir, headless: headless, sessions: make(map[string]*Session)}
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

	cdpBaseURL := fmt.Sprintf("http://localhost:%d", m.cdpPort)
	tab, err := createTab(opCtx, cdpBaseURL, url)
	if err != nil {
		return "", err
	}

	slog.Info("browser: tab opened", "url", url, "tab", tab.ID)
	return tab.ID, nil
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

	if err := launchChrome(m); err != nil {
		return "", err
	}
	slog.Info("browser: chrome launched", "port", m.cdpPort)

	waitCtx, cancel := withDefaultTimeout(ctx, 15*time.Second)
	defer cancel()
	return waitForChrome(waitCtx, cdpBaseURL)
}

func withDefaultTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

// profileName returns the Chrome profile folder name in the default user data dir.
func (m *Manager) profileName() string {
	if m.profileDir != "" {
		return m.profileDir
	}
	return "Aviary"
}
