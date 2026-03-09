package cmd

import (
	"testing"

	"github.com/lsegal/aviary/internal/config"
)

func TestConfigGetKey_Simple(t *testing.T) {
	cfg := config.Default()
	cfg.Browser.ProfileDir = "/my/profile"
	cfg.Browser.Binary = "/usr/bin/chromium"
	cfg.Browser.CDPPort = 9222
	cfg.Scheduler.Concurrency = "auto"

	tests := []struct {
		key  string
		want string
	}{
		{"browser.profile_directory", "/my/profile"},
		{"browser.binary", "/usr/bin/chromium"},
		{"browser.cdp_port", "9222"},
		{"server.port", "16677"},
		{"scheduler.concurrency", "auto"},
	}

	for _, tt := range tests {
		got, err := configGetKey(&cfg, tt.key)
		if err != nil {
			t.Errorf("configGetKey(%q): unexpected error: %v", tt.key, err)
			continue
		}
		if got != tt.want {
			t.Errorf("configGetKey(%q) = %q, want %q", tt.key, got, tt.want)
		}
	}
}

func TestConfigGetKey_NotFound(t *testing.T) {
	cfg := config.Default()
	_, err := configGetKey(&cfg, "browser.nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown key")
	}
}

func TestConfigGetKey_TraverseError(t *testing.T) {
	cfg := config.Default()
	// browser.cdp_port is a scalar (int), not a map — can't traverse into it.
	_, err := configGetKey(&cfg, "browser.cdp_port.foo")
	if err == nil {
		t.Fatal("expected error traversing into scalar")
	}
}

func TestConfigSetKey_String(t *testing.T) {
	cfg := config.Default()
	if err := configSetKey(&cfg, "browser.profile_directory", "/my/profile"); err != nil {
		t.Fatalf("configSetKey: %v", err)
	}
	if cfg.Browser.ProfileDir != "/my/profile" {
		t.Fatalf("expected ProfileDir /my/profile, got %q", cfg.Browser.ProfileDir)
	}
}

func TestConfigSetKey_Int(t *testing.T) {
	cfg := config.Default()
	if err := configSetKey(&cfg, "server.port", "9999"); err != nil {
		t.Fatalf("configSetKey: %v", err)
	}
	if cfg.Server.Port != 9999 {
		t.Fatalf("expected port 9999, got %d", cfg.Server.Port)
	}
}

func TestConfigSetKey_BrowserBinary(t *testing.T) {
	cfg := config.Default()
	if err := configSetKey(&cfg, "browser.binary", "/usr/bin/chromium"); err != nil {
		t.Fatalf("configSetKey: %v", err)
	}
	if cfg.Browser.Binary != "/usr/bin/chromium" {
		t.Fatalf("expected binary /usr/bin/chromium, got %q", cfg.Browser.Binary)
	}
}

func TestConfigSetKey_CDPPort(t *testing.T) {
	cfg := config.Default()
	if err := configSetKey(&cfg, "browser.cdp_port", "9333"); err != nil {
		t.Fatalf("configSetKey: %v", err)
	}
	if cfg.Browser.CDPPort != 9333 {
		t.Fatalf("expected CDPPort 9333, got %d", cfg.Browser.CDPPort)
	}
}

func TestConfigSetKey_NestedModel(t *testing.T) {
	cfg := config.Default()
	if err := configSetKey(&cfg, "models.defaults.model", "anthropic/claude-sonnet-4-5"); err != nil {
		t.Fatalf("configSetKey: %v", err)
	}
	if cfg.Models.Defaults.Model != "anthropic/claude-sonnet-4-5" {
		t.Fatalf("expected model anthropic/claude-sonnet-4-5, got %q", cfg.Models.Defaults.Model)
	}
}

func TestConfigRoundtrip(t *testing.T) {
	cfg := config.Default()

	// Set several values, then get them back.
	sets := []struct{ key, val string }{
		{"browser.profile_directory", "/aviary/profile"},
		{"browser.cdp_port", "9444"},
		{"server.port", "8080"},
	}
	for _, s := range sets {
		if err := configSetKey(&cfg, s.key, s.val); err != nil {
			t.Fatalf("set %q: %v", s.key, err)
		}
	}
	for _, s := range sets {
		got, err := configGetKey(&cfg, s.key)
		if err != nil {
			t.Fatalf("get %q: %v", s.key, err)
		}
		if got != s.val {
			t.Fatalf("roundtrip %q: got %q, want %q", s.key, got, s.val)
		}
	}
}
