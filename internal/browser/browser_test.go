package browser

import (
	"context"
	"testing"
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
	if got == "" {
		t.Fatal("expected non-empty default user data dir")
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
