package clidefs

import (
	"encoding/json"
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

// trustHome is a ConfigHome shaped like Claude's: the settings file lives
// beside the directory until X_HOME names one.
func trustHome(t *testing.T) (ConfigHome, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	t.Setenv("X_HOME", "")
	return ConfigHome{
		Env:        "X_HOME",
		Default:    "~/.x",
		Private:    []string{".x.json"},
		Outside:    map[string]string{".x.json": "~/.x.json"},
		StorageEnv: "X_STORAGE",
		Trust:      &WorkspaceTrust{File: ".x.json", Path: []string{"projects", "{dir}", "trusted"}},
	}, root
}

func trustedProjects(t *testing.T, overlay string) map[string]map[string]any {
	t.Helper()
	var settings struct {
		Projects map[string]map[string]any `json:"projects"`
	}
	data, err := os.ReadFile(filepath.Join(overlay, ".x.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &settings))
	return settings.Projects
}

// TestConfigHomeTrustsLaunchWorkspaces shows the overlay copy records each
// launch's workspace as trusted, keeps the user's own entries, keeps the
// workspace of an earlier launch trusted across a later refresh while it
// still exists, and never writes to the user's file (#565).
func TestConfigHomeTrustsLaunchWorkspaces(t *testing.T) {
	home, root := trustHome(t)
	user := []byte(`{"big": 12345678901234567890, "projects": {"/mine": {"trusted": false, "note": "x"}}}`)
	require.NoError(t, os.WriteFile(filepath.Join(root, ".x.json"), user, 0o600))
	base, first, second := t.TempDir(), t.TempDir(), t.TempDir()

	overlay, err := home.Overlay(base, first)
	require.NoError(t, err)
	projects := trustedProjects(t, overlay)
	assert.Equal(t, true, projects[filepath.ToSlash(first)]["trusted"])
	assert.Equal(t, map[string]any{"trusted": false, "note": "x"}, projects["/mine"])
	data, err := os.ReadFile(filepath.Join(overlay, ".x.json"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "12345678901234567890", "numbers are copied exactly")

	_, err = home.Overlay(base, second)
	require.NoError(t, err)
	projects = trustedProjects(t, overlay)
	assert.Equal(t, true, projects[filepath.ToSlash(first)]["trusted"], "a run still going stays trusted")
	assert.Equal(t, true, projects[filepath.ToSlash(second)]["trusted"])

	require.NoError(t, os.Remove(first))
	_, err = home.Overlay(base)
	require.NoError(t, err)
	projects = trustedProjects(t, overlay)
	assert.NotContains(t, projects, filepath.ToSlash(first), "a removed workspace is forgotten")
	assert.Equal(t, true, projects[filepath.ToSlash(second)]["trusted"])

	got, err := os.ReadFile(filepath.Join(root, ".x.json"))
	require.NoError(t, err)
	assert.Equal(t, user, got, "the user's file changed")
}

// TestConfigHomeTrustsRepositoryRoot shows a workspace inside a git
// repository has the repository root trusted too, which is what Claude keys
// trust by.
func TestConfigHomeTrustsRepositoryRoot(t *testing.T) {
	home, _ := trustHome(t)
	repo := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(repo, ".git"), 0o700))
	sub := filepath.Join(repo, "sub")
	require.NoError(t, os.Mkdir(sub, 0o700))

	overlay, err := home.Overlay(t.TempDir(), sub)
	require.NoError(t, err)
	projects := trustedProjects(t, overlay)
	assert.Equal(t, true, projects[filepath.ToSlash(sub)]["trusted"])
	assert.Equal(t, true, projects[filepath.ToSlash(repo)]["trusted"])
}

// TestConfigHomeOutsideFollowsEnv shows a Private file is copied from its
// outside location only while Env is unset, and from inside the directory Env
// names otherwise; StorageEnv carries the user's own Env value.
func TestConfigHomeOutsideFollowsEnv(t *testing.T) {
	home, root := trustHome(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, ".x.json"), []byte(`{"from":"outside"}`), 0o600))
	definition := Definition{ConfigHome: &home}

	env, err := definition.HomeEnv(t.TempDir())
	require.NoError(t, err)
	require.Len(t, env, 2)
	assert.Equal(t, "X_STORAGE=", env[1], "an unset Env leaves stored sign-in at the CLI's default")
	overlay := strings.TrimPrefix(env[0], "X_HOME=")
	data, err := os.ReadFile(filepath.Join(overlay, ".x.json"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "outside")

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".x.json"), []byte(`{"from":"inside"}`), 0o600))
	t.Setenv("X_HOME", dir)
	env, err = definition.HomeEnv(t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, "X_STORAGE="+dir, env[1])
	data, err = os.ReadFile(filepath.Join(strings.TrimPrefix(env[0], "X_HOME="), ".x.json"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "inside")
}

func TestConfigHomeValidateTrust(t *testing.T) {
	valid := func() *ConfigHome {
		return &ConfigHome{Env: "X", Default: "~/.x", Private: []string{"c.json"}}
	}
	for _, tc := range []struct {
		edit func(*ConfigHome)
		want string
	}{
		{func(c *ConfigHome) { c.Outside = map[string]string{"d.json": "~/d.json"} }, `"configHome.outside"`},
		{func(c *ConfigHome) { c.Outside = map[string]string{"c.json": " "} }, `"configHome.outside.c.json"`},
		{func(c *ConfigHome) { c.StorageEnv = "A=B" }, `"configHome.storageEnv"`},
		{func(c *ConfigHome) { c.Trust = &WorkspaceTrust{File: "d.json", Path: []string{"{dir}"}} }, `"configHome.trust.file"`},
		{func(c *ConfigHome) { c.Trust = &WorkspaceTrust{File: "c.json", Path: []string{"projects"}} }, `"configHome.trust.path"`},
	} {
		home := valid()
		tc.edit(home)
		err := home.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), tc.want)
	}
}

// TestHomeEnvWithoutConfigHome shows a definition that declares no ConfigHome
// launches against the user's own directory: no overlay variable is added and
// the launch env is just the definition's Env.
func TestHomeEnvWithoutConfigHome(t *testing.T) {
	definition := Definition{Name: "plain", Env: map[string]string{"PLAIN_MODE": "1"}}
	base := t.TempDir()
	home, err := definition.HomeEnv(base)
	require.NoError(t, err)
	assert.Empty(t, home)
	own, err := definition.OwnHomeEnv(base)
	require.NoError(t, err)
	assert.Empty(t, own)
	env, err := definition.LaunchEnv(base)
	require.NoError(t, err)
	assert.Equal(t, []string{"PLAIN_MODE=1"}, env)
	entries, err := os.ReadDir(base)
	require.NoError(t, err)
	assert.Empty(t, entries, "no overlay is prepared without a ConfigHome")
}

// TestConfigHomeDefaultWithoutTilde shows a Default that is not under ~/ is
// used as given, made absolute.
func TestConfigHomeDefaultWithoutTilde(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cli-home")
	t.Setenv("X_HOME", "")
	home, err := ConfigHome{Env: "X_HOME", Default: dir}.Home()
	require.NoError(t, err)
	assert.Equal(t, dir, home)
}

// TestLaunchEnvReportsAnOverlayItCannotPrepare shows each way preparing the
// overlay can fail is reported with the variable it was for, while LaunchEnv
// still returns the definition's own Env so the caller can launch against the
// user's directory instead.
func TestLaunchEnvReportsAnOverlayItCannotPrepare(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, root string) (ConfigHome, string)
		want  string
	}{
		{
			name: "home under a file",
			setup: func(t *testing.T, root string) (ConfigHome, string) {
				blocker := filepath.Join(root, "file")
				require.NoError(t, os.WriteFile(blocker, nil, 0o600))
				t.Setenv("X_HOME", filepath.Join(blocker, "home"))
				return ConfigHome{Env: "X_HOME", Default: "~/.x", Private: []string{"settings.json"}}, t.TempDir()
			},
		},
		{
			name: "base is a file",
			setup: func(t *testing.T, root string) (ConfigHome, string) {
				t.Setenv("X_HOME", filepath.Join(root, "home"))
				base := filepath.Join(root, "base")
				require.NoError(t, os.WriteFile(base, nil, 0o600))
				return ConfigHome{Env: "X_HOME", Default: "~/.x", Private: []string{"settings.json"}}, base
			},
		},
		{
			name: "private file is a directory",
			setup: func(t *testing.T, root string) (ConfigHome, string) {
				home := filepath.Join(root, "home")
				require.NoError(t, os.MkdirAll(filepath.Join(home, "settings.json"), 0o700))
				t.Setenv("X_HOME", home)
				return ConfigHome{Env: "X_HOME", Default: "~/.x", Private: []string{"settings.json"}}, t.TempDir()
			},
			want: "preparing the X_HOME overlay",
		},
		{
			name: "trusted settings are not JSON",
			setup: func(t *testing.T, root string) (ConfigHome, string) {
				home := filepath.Join(root, "home")
				require.NoError(t, os.MkdirAll(home, 0o700))
				require.NoError(t, os.WriteFile(filepath.Join(home, "settings.json"), []byte("{not json"), 0o600))
				t.Setenv("X_HOME", home)
				return ConfigHome{
					Env: "X_HOME", Default: "~/.x", Private: []string{"settings.json"},
					Trust: &WorkspaceTrust{File: "settings.json", Path: []string{"projects", "{dir}"}},
				}, t.TempDir()
			},
			want: "trusting the workspace in the X_HOME overlay: reading settings.json",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			configHome, base := test.setup(t, t.TempDir())
			definition := Definition{Name: "x", Env: map[string]string{"X_MODE": "1"}, ConfigHome: &configHome}
			env, err := definition.LaunchEnv(base, t.TempDir())
			require.Error(t, err)
			if test.want != "" {
				assert.Contains(t, err.Error(), test.want)
			}
			assert.Equal(t, []string{"X_MODE=1"}, env, "the definition's Env is still returned")
		})
	}
}

// TestOverlayRefreshFailsWhenAStaleCopyCannotBeRemoved shows a Private file
// the user deleted is removed from the overlay on refresh, and that a copy
// which cannot be removed is reported rather than left silently stale.
func TestOverlayRefreshFailsWhenAStaleCopyCannotBeRemoved(t *testing.T) {
	home, base := t.TempDir(), t.TempDir()
	t.Setenv("X_HOME", home)
	configHome := ConfigHome{Env: "X_HOME", Default: "~/.x", Private: []string{"settings.json"}}
	require.NoError(t, os.WriteFile(filepath.Join(home, "settings.json"), []byte("{}"), 0o600))
	overlay, err := configHome.Overlay(base)
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(overlay, "settings.json"))

	require.NoError(t, os.Remove(filepath.Join(home, "settings.json")))
	_, err = configHome.Overlay(base)
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(overlay, "settings.json"), "the copy of a deleted Private file stays")

	// A non-empty directory in the copy's place cannot be removed.
	require.NoError(t, os.MkdirAll(filepath.Join(overlay, "settings.json", "inner"), 0o700))
	_, err = configHome.Overlay(base)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "preparing the X_HOME overlay")
}

// TestWorkspaceKeysOfNoWorkspace shows an empty workspace contributes no
// trusted key, so a launch without a workspace trusts nothing new.
func TestWorkspaceKeysOfNoWorkspace(t *testing.T) {
	assert.Empty(t, workspaceKeys(""))
}

// TestConfigHomeForgetsTrustWhenNoWorkspaceRemains shows the overlay's list
// of trusted workspaces is deleted once none of them exists, and that a list
// which cannot be deleted is reported instead of trusting stale workspaces.
func TestConfigHomeForgetsTrustWhenNoWorkspaceRemains(t *testing.T) {
	home, _ := trustHome(t)
	base, workspace := t.TempDir(), t.TempDir()
	overlay, err := home.Overlay(base, workspace)
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(overlay, trustedFile))

	require.NoError(t, os.Remove(workspace))
	_, err = home.Overlay(base)
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(overlay, trustedFile))

	require.NoError(t, os.MkdirAll(filepath.Join(overlay, trustedFile, "inner"), 0o700))
	_, err = home.Overlay(base)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "trusting the workspace in the X_HOME overlay")
}
