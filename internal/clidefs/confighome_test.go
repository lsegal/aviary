package clidefs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// codexOverlay prepares the built-in codex definition's launch env under base
// and returns the CODEX_HOME it names.
func codexOverlay(t *testing.T, base string) string {
	t.Helper()
	codex, ok := MustBuiltin().Lookup("codex")
	require.True(t, ok)
	env, err := codex.LaunchEnv(base)
	require.NoError(t, err)
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, "CODEX_HOME="); ok {
			return value
		}
	}
	require.Failf(t, "no CODEX_HOME", "env %v", env)
	return ""
}

// TestCodexLaunchEnvOverlaysConfigHome shows every codex launch runs against
// a CODEX_HOME overlay: config.toml is a private copy, so codex's
// trusted-project bookkeeping never reaches the user's file, while everything
// else, such as the sign-in it refreshes and its sessions, is the user's own
// (issue #528).
func TestCodexLaunchEnvOverlaysConfigHome(t *testing.T) {
	home, base := t.TempDir(), t.TempDir()
	config := "model = \"gpt-5\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(home, "auth.json"), []byte("old"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(home, "sessions"), 0o700))
	t.Setenv("CODEX_HOME", home)

	overlay := codexOverlay(t, base)
	assert.NotEqual(t, home, overlay)
	got, err := os.ReadFile(filepath.Join(overlay, "config.toml"))
	require.NoError(t, err)
	assert.Equal(t, config, string(got))

	// What codex writes during a launch.
	f, err := os.OpenFile(filepath.Join(overlay, "config.toml"), os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = f.WriteString("[projects.\"/work\"]\ntrust_level = \"trusted\"\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, os.WriteFile(filepath.Join(overlay, "auth.json"), []byte("refreshed"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(overlay, "sessions", "s.jsonl"), []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(overlay, "history.jsonl"), []byte("{}"), 0o600))

	got, err = os.ReadFile(filepath.Join(home, "config.toml"))
	require.NoError(t, err)
	assert.Equal(t, config, string(got), "the user's config.toml changed")
	auth, err := os.ReadFile(filepath.Join(home, "auth.json"))
	require.NoError(t, err)
	assert.Equal(t, "refreshed", string(auth))
	assert.FileExists(t, filepath.Join(home, "sessions", "s.jsonl"))

	// The next launch reuses the overlay, so paths codex recorded through it
	// still resolve, and starts again from the user's current settings.
	edited := config + "model_reasoning_effort = \"low\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(home, "config.toml"), []byte(edited), 0o600))
	assert.Equal(t, overlay, codexOverlay(t, base))
	got, err = os.ReadFile(filepath.Join(overlay, "config.toml"))
	require.NoError(t, err)
	assert.Equal(t, edited, string(got))
	assert.FileExists(t, filepath.Join(overlay, "sessions", "s.jsonl"))
	assert.FileExists(t, filepath.Join(home, "history.jsonl"), "a file codex created in the overlay moves to the user's directory")
	assert.FileExists(t, filepath.Join(overlay, "history.jsonl"))
}

func TestConfigHomeOverlayFollowsUserDirectory(t *testing.T) {
	home, base := filepath.Join(t.TempDir(), "codex"), t.TempDir()
	t.Setenv("CODEX_HOME", home)
	overlay := codexOverlay(t, base)
	assert.DirExists(t, home, "a missing directory is created")
	assert.NoFileExists(t, filepath.Join(overlay, "config.toml"))

	require.NoError(t, os.WriteFile(filepath.Join(home, "config.toml"), []byte("a"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(home, "auth.json"), []byte("b"), 0o600))
	codexOverlay(t, base)
	assert.FileExists(t, filepath.Join(overlay, "config.toml"))
	assert.FileExists(t, filepath.Join(overlay, "auth.json"))

	require.NoError(t, os.Remove(filepath.Join(home, "config.toml")))
	require.NoError(t, os.Remove(filepath.Join(home, "auth.json")))
	codexOverlay(t, base)
	assert.NoFileExists(t, filepath.Join(overlay, "config.toml"), "a removed settings file is not kept")
	_, err := os.Lstat(filepath.Join(overlay, "auth.json"))
	assert.ErrorIs(t, err, os.ErrNotExist, "a link to a removed entry is dropped")

	other := t.TempDir()
	t.Setenv("CODEX_HOME", other)
	assert.NotEqual(t, overlay, codexOverlay(t, base), "each user directory gets its own overlay")
}

func TestConfigHomeValidate(t *testing.T) {
	for _, tc := range []struct {
		home *ConfigHome
		want string
	}{
		{&ConfigHome{Default: "~/.x", Private: []string{"c"}}, `"configHome.env"`},
		{&ConfigHome{Env: "X", Private: []string{"c"}}, `"configHome.default"`},
		{&ConfigHome{Env: "X", Default: "~/.x"}, `"configHome.private"`},
		{&ConfigHome{Env: "X", Default: "~/.x", Private: []string{"../c"}}, `"configHome.private"`},
	} {
		err := tc.home.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), tc.want)
	}
	assert.NoError(t, (&ConfigHome{Env: "X", Default: "~/.x", Private: []string{"config.toml"}}).validate())
}

// TestCodexOwnHomeEnvEmptiesConfig shows chat launches run codex against an
// overlay whose config.toml is aviary's own and empty, so the user's settings
// are not read, while sign-in is shared and the per-launch profiles aviary
// writes there are left alone by later refreshes (#573).
func TestCodexOwnHomeEnvEmptiesConfig(t *testing.T) {
	home, base := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(home, "config.toml"), []byte("model = \"gpt-5\"\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(home, "auth.json"), []byte("token"), 0o600))
	t.Setenv("CODEX_HOME", home)
	codex, ok := MustBuiltin().Lookup("codex")
	require.True(t, ok)

	env, err := codex.OwnHomeEnv(base)
	require.NoError(t, err)
	require.Len(t, env, 1)
	overlay, ok := strings.CutPrefix(env[0], "CODEX_HOME=")
	require.True(t, ok, env[0])
	config, err := os.ReadFile(filepath.Join(overlay, "config.toml"))
	require.NoError(t, err)
	assert.Empty(t, config)
	auth, err := os.ReadFile(filepath.Join(overlay, "auth.json"))
	require.NoError(t, err)
	assert.Equal(t, "token", string(auth))

	// What codex and aviary write during a launch.
	require.NoError(t, os.WriteFile(filepath.Join(overlay, "config.toml"), []byte("[projects.\"/work\"]\n"), 0o600))
	profile := filepath.Join(overlay, LaunchFilePrefix+"1.config.toml")
	require.NoError(t, os.WriteFile(profile, []byte("developer_instructions = \"x\"\n"), 0o600))

	_, err = codex.OwnHomeEnv(base)
	require.NoError(t, err)
	config, err = os.ReadFile(filepath.Join(overlay, "config.toml"))
	require.NoError(t, err)
	assert.Empty(t, config, "each launch starts from empty settings")
	assert.FileExists(t, profile, "a running launch keeps its profile")
	assert.NoFileExists(t, filepath.Join(home, LaunchFilePrefix+"1.config.toml"), "profiles never reach the user's directory")
	user, err := os.ReadFile(filepath.Join(home, "config.toml"))
	require.NoError(t, err)
	assert.Equal(t, "model = \"gpt-5\"\n", string(user))
}
