package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	authpkg "github.com/lsegal/aviary/internal/auth"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/store"

	"github.com/stretchr/testify/assert"
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
		got := parseInt(c.in)
		assert.Equal(t, c.want, got)

	}
}

// ── toggleBoolPtr ─────────────────────────────────────────────────────────────

func TestToggleBoolPtr(t *testing.T) {
	// nil with default=true → toggles to false
	got := toggleBoolPtr(nil, true)
	assert.NotNil(t, got)
	assert.Equal(t, false, *got)

	// nil with default=false → toggles to true
	got2 := toggleBoolPtr(nil, false)
	assert.NotNil(t, got2)
	assert.Equal(t, true, *got2)

	// explicit true → toggles to false
	b := true
	got3 := toggleBoolPtr(&b, false)
	assert.NotNil(t, got3)
	assert.Equal(t, false, *got3)

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
		got := joinAllowFrom(c.in)
		assert.Equal(t, c.want, got)

	}
}

func TestParseAllowFrom(t *testing.T) {
	got := parseAllowFrom("+1, +2, +3")
	assert.Equal(t, 3, len(got))
	assert.Equal(t, "+2", got[1].From)
	assert.Nil(t, // empty string → nil
		parseAllowFrom(""))

}

// ── intString ─────────────────────────────────────────────────────────────────

func TestIntString(t *testing.T) {
	assert.Equal(t, "", intString(0))
	assert.Equal(t, "42", intString(42))

}

// ── boolLabel ─────────────────────────────────────────────────────────────────

func TestBoolLabel(t *testing.T) {
	assert.Equal(t, "on", boolLabel(true))
	assert.Equal(t, "off", boolLabel(false))

}

// ── fallback / firstNonEmpty ──────────────────────────────────────────────────

func TestFallback(t *testing.T) {
	assert.Equal(t, "val", fallback("val", "default"))
	assert.Equal(t, "default", fallback("", "default"))
	assert.Equal(t, "default", fallback("  ", "default"))

}

func TestFirstNonEmpty(t *testing.T) {
	assert.Equal(t, "x", firstNonEmpty("", "  ", "x", "y"))
	assert.Equal(t, "", firstNonEmpty("", ""))
	assert.Equal(t, "a", firstNonEmpty("a"))

}

// ── marshalOAuthToken ─────────────────────────────────────────────────────────

func TestMarshalOAuthToken(t *testing.T) {
	tok := &authpkg.OAuthToken{
		AccessToken:  "acc",
		RefreshToken: "ref",
		ExpiresAt:    time.Now().Add(time.Hour).UnixMilli(),
	}
	data, err := marshalOAuthToken(tok)
	assert.NoError(t, err)
	assert.NotEqual(t, "", data)

}

// ── authStore ─────────────────────────────────────────────────────────────────

func TestAuthStore_TempDir(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	store.SetDataDir(base + "/aviary")
	t.Cleanup(func() { store.SetDataDir("") })
	err := store.EnsureDirs()
	assert.NoError(t, err)

	st := authStore()
	assert.NotNil(t, st)

	// Should be able to set and get a value.
	err = st.Set("test:key", "secret")
	assert.NoError(t, err)

	val, err := st.Get("test:key")
	assert.NoError(t, err)
	assert.Equal(t, "secret", val)

}

// ── runDoctor ─────────────────────────────────────────────────────────────────

func TestRunDoctor_NoConfig(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	store.SetDataDir(base + "/aviary")
	t.Cleanup(func() { store.SetDataDir("") })
	err := store.EnsureDirs()
	assert.NoError(t, err)

	// runDoctor calls os.Exit(1) when there are errors.
	// We can only safely test the no-exit path (no models configured).
	cfgPath := filepath.Join(base, "aviary", "aviary.yaml")
	cfg := config.Default()
	err = config.Save(cfgPath, &cfg)
	assert.NoError(t, err)

	cfgFile = cfgPath
	t.Cleanup(func() { cfgFile = "" })

	// No agents/models → no errors → exits cleanly.
	err = runDoctor(nil, nil)
	assert.NoError(t, err)

}

func TestRunDoctor_MissingConfig(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	store.SetDataDir(base + "/aviary")
	t.Cleanup(func() { store.SetDataDir("") })
	err := store.EnsureDirs()
	assert.NoError(t, err)

	// Point to a config file that doesn't exist.
	cfgFile = filepath.Join(base, "nonexistent.yaml")
	t.Cleanup(func() { cfgFile = "" })

	// Missing config file — should print a warning but not error.
	err = runDoctor(nil, nil)
	assert.NoError(t, err)

}

func TestRunDoctor_InvalidYAML(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	store.SetDataDir(base + "/aviary")
	t.Cleanup(func() { store.SetDataDir("") })
	err := store.EnsureDirs()
	assert.NoError(t, err)

	cfgPath := filepath.Join(base, "bad.yaml")
	_ = os.WriteFile(cfgPath, []byte(": invalid: yaml: ["), 0o600)
	cfgFile = cfgPath
	t.Cleanup(func() { cfgFile = "" })

	err = runDoctor(nil, nil)
	assert.Error(t, err)

}
