package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	authpkg "github.com/lsegal/aviary/internal/auth"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/store"
)

// ── parseInt ─────────────────────────────────────────────────────────────────

func TestParseInt(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"42", 42}, {"0", 0}, {"", 0}, {"  7  ", 7}, {"abc", 0},
	}
	for _, c := range cases {
		if got := parseInt(c.in); got != c.want {
			t.Errorf("parseInt(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

// ── toggleBoolPtr ─────────────────────────────────────────────────────────────

func TestToggleBoolPtr(t *testing.T) {
	// nil with default=true → toggles to false
	got := toggleBoolPtr(nil, true)
	if got == nil || *got != false {
		t.Errorf("toggleBoolPtr(nil, true) = %v, want false", got)
	}
	// nil with default=false → toggles to true
	got2 := toggleBoolPtr(nil, false)
	if got2 == nil || *got2 != true {
		t.Errorf("toggleBoolPtr(nil, false) = %v, want true", got2)
	}
	// explicit true → toggles to false
	b := true
	got3 := toggleBoolPtr(&b, false)
	if got3 == nil || *got3 != false {
		t.Errorf("toggleBoolPtr(&true, false) = %v, want false", got3)
	}
}

// ── joinAllowFrom / parseAllowFrom ────────────────────────────────────────────

func TestJoinAllowFrom(t *testing.T) {
	cases := []struct {
		in   []config.AllowFromEntry
		want string
	}{
		{nil, ""},
		{[]config.AllowFromEntry{{From: "+1"}}, "+1"},
		{[]config.AllowFromEntry{{From: "+1"}, {From: "+2"}}, "+1, +2"},
		{[]config.AllowFromEntry{{From: "  "}, {From: "+3"}}, "+3"}, // blank entries skipped
	}
	for _, c := range cases {
		if got := joinAllowFrom(c.in); got != c.want {
			t.Errorf("joinAllowFrom(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseAllowFrom(t *testing.T) {
	got := parseAllowFrom("+1, +2, +3")
	if len(got) != 3 {
		t.Fatalf("parseAllowFrom: len=%d, want 3", len(got))
	}
	if got[1].From != "+2" {
		t.Errorf("parseAllowFrom[1].From = %q, want +2", got[1].From)
	}
	// empty string → nil
	if parseAllowFrom("") != nil {
		t.Error("parseAllowFrom(\"\") should return nil")
	}
}

// ── intString ─────────────────────────────────────────────────────────────────

func TestIntString(t *testing.T) {
	if intString(0) != "" {
		t.Error("intString(0) should return empty")
	}
	if intString(42) != "42" {
		t.Errorf("intString(42) = %q, want 42", intString(42))
	}
}

// ── boolLabel ─────────────────────────────────────────────────────────────────

func TestBoolLabel(t *testing.T) {
	if boolLabel(true) != "on" {
		t.Error("boolLabel(true) should be 'on'")
	}
	if boolLabel(false) != "off" {
		t.Error("boolLabel(false) should be 'off'")
	}
}

// ── fallback / firstNonEmpty ──────────────────────────────────────────────────

func TestFallback(t *testing.T) {
	if fallback("val", "default") != "val" {
		t.Error("fallback with non-empty should return value")
	}
	if fallback("", "default") != "default" {
		t.Error("fallback with empty should return default")
	}
	if fallback("  ", "default") != "default" {
		t.Error("fallback with whitespace should return default")
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if firstNonEmpty("", "  ", "x", "y") != "x" {
		t.Error("firstNonEmpty should return first non-blank")
	}
	if firstNonEmpty("", "") != "" {
		t.Error("firstNonEmpty with all empty should return empty")
	}
	if firstNonEmpty("a") != "a" {
		t.Error("firstNonEmpty single value")
	}
}

// ── marshalOAuthToken ─────────────────────────────────────────────────────────

func TestMarshalOAuthToken(t *testing.T) {
	tok := &authpkg.OAuthToken{
		AccessToken:  "acc",
		RefreshToken: "ref",
		ExpiresAt:    time.Now().Add(time.Hour).UnixMilli(),
	}
	data, err := marshalOAuthToken(tok)
	if err != nil {
		t.Fatalf("marshalOAuthToken: %v", err)
	}
	if data == "" {
		t.Error("expected non-empty JSON")
	}
}

// ── authStore ─────────────────────────────────────────────────────────────────

func TestAuthStore_TempDir(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	store.SetDataDir(base + "/aviary")
	t.Cleanup(func() { store.SetDataDir("") })
	if err := store.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}

	st := authStore()
	if st == nil {
		t.Fatal("authStore returned nil")
	}
	// Should be able to set and get a value.
	if err := st.Set("test:key", "secret"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	val, err := st.Get("test:key")
	if err != nil || val != "secret" {
		t.Errorf("Get: val=%q err=%v", val, err)
	}
}

// ── runDoctor ─────────────────────────────────────────────────────────────────

func TestRunDoctor_NoConfig(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	store.SetDataDir(base + "/aviary")
	t.Cleanup(func() { store.SetDataDir("") })
	if err := store.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}

	// runDoctor calls os.Exit(1) when there are errors.
	// We can only safely test the no-exit path (no models configured).
	cfgPath := filepath.Join(base, "aviary", "aviary.yaml")
	cfg := config.Default()
	if err := config.Save(cfgPath, &cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cfgFile = cfgPath
	t.Cleanup(func() { cfgFile = "" })

	// No agents/models → no errors → exits cleanly.
	err := runDoctor(nil, nil)
	if err != nil {
		t.Fatalf("runDoctor: %v", err)
	}
}

func TestRunDoctor_MissingConfig(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	store.SetDataDir(base + "/aviary")
	t.Cleanup(func() { store.SetDataDir("") })
	if err := store.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}

	// Point to a config file that doesn't exist.
	cfgFile = filepath.Join(base, "nonexistent.yaml")
	t.Cleanup(func() { cfgFile = "" })

	// Missing config file — should print a warning but not error.
	err := runDoctor(nil, nil)
	if err != nil {
		t.Fatalf("runDoctor with missing config: %v", err)
	}
}

func TestRunDoctor_InvalidYAML(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	store.SetDataDir(base + "/aviary")
	t.Cleanup(func() { store.SetDataDir("") })
	if err := store.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}

	cfgPath := filepath.Join(base, "bad.yaml")
	_ = os.WriteFile(cfgPath, []byte(": invalid: yaml: ["), 0o600)
	cfgFile = cfgPath
	t.Cleanup(func() { cfgFile = "" })

	err := runDoctor(nil, nil)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}
