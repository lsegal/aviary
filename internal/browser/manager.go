package browser

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/chromedp/chromedp"
	"github.com/lsegal/aviary/internal/store"
)

// Manager manages a single browser session lifecycle.
type Manager struct {
	mu      sync.Mutex
	session *Session
	binary  string
	cdpPort int
}

// NewManager creates a browser Manager.
// binary is the path to the Chromium/Chrome executable (empty = auto-detect).
// cdpPort is the remote debugging port (0 = default 9222).
func NewManager(binary string, cdpPort int) *Manager {
	if cdpPort == 0 {
		cdpPort = 9222
	}
	return &Manager{binary: binary, cdpPort: cdpPort}
}

// Open starts a browser session navigated to url.
// If a session is already open it is reused.
func (m *Manager) Open(ctx context.Context, url string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.session == nil {
		s, err := m.newSession(ctx)
		if err != nil {
			return err
		}
		m.session = s
	}

	return m.session.Navigate(url)
}

// Click forwards to the active session.
func (m *Manager) Click(selector string) error {
	return m.withSession(func(s *Session) error { return s.Click(selector) })
}

// Type forwards to the active session.
func (m *Manager) Type(selector, text string) error {
	return m.withSession(func(s *Session) error { return s.Type(selector, text) })
}

// Screenshot captures the current page.
func (m *Manager) Screenshot() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.session == nil {
		return nil, fmt.Errorf("no browser session open")
	}
	return m.session.Screenshot()
}

// EvalJS evaluates JavaScript in the active session.
func (m *Manager) EvalJS(expr string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.session == nil {
		return "", fmt.Errorf("no browser session open")
	}
	return m.session.EvalJS(expr)
}

// Close terminates the active browser session.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.session != nil {
		m.session.Close()
		m.session = nil
	}
}

func (m *Manager) withSession(fn func(*Session) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.session == nil {
		return fmt.Errorf("no browser session open; call Open first")
	}
	return fn(m.session)
}

func (m *Manager) newSession(ctx context.Context) (*Session, error) {
	profileDir := filepath.Join(store.DataDir(), "browser-profile")

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserDataDir(profileDir),
		chromedp.Flag("remote-debugging-port", fmt.Sprintf("%d", m.cdpPort)),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
	)
	if m.binary != "" {
		opts = append(opts, chromedp.ExecPath(m.binary))
	}

	return newSession(ctx, opts)
}
