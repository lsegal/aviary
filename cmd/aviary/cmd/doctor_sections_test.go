package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/clidoctor"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/store"
)

// doctorTestEnv points the config and data dirs at a fresh temp dir.
func doctorTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AVIARY_CONFIG_BASE_DIR", "")
	prev := store.DataDir()
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir(prev) })
	require.NoError(t, store.EnsureDirs())
}

// doctorOutput runs fn and returns what it printed to stdout.
func doctorOutput(t *testing.T, fn func()) string {
	t.Helper()
	out, err := captureStdout(t, func() error { fn(); return nil })
	require.NoError(t, err)
	return out
}

func TestWriteDoctorAgents(t *testing.T) {
	out := doctorOutput(t, func() { writeDoctorAgents(&config.Config{}, "/work") })
	assert.Equal(t, "Configured agents\n─────────────────\n  (none)\n", out)

	cfg := &config.Config{Agents: []config.AgentConfig{
		{Name: "plain"},
		{Name: "tuned", Model: "anthropic/claude-x", WorkingDir: "/elsewhere", Permissions: &config.PermissionsConfig{Preset: config.PermissionsPresetFull}},
		{Name: "blank-preset", Permissions: &config.PermissionsConfig{}},
	}}
	out = doctorOutput(t, func() { writeDoctorAgents(cfg, "/work") })
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	require.Len(t, lines, 5)
	assert.NotContains(t, out, "(none)")
	// An agent with no model, preset or directory reports the defaults and
	// the doctor's own working directory.
	assert.Regexp(t, `^  plain\s+model=\(default\)\s+permissions=standard  dir=/work$`, lines[2])
	assert.Regexp(t, `^  tuned\s+model=anthropic/claude-x\s+permissions=full  dir=/elsewhere$`, lines[3])
	assert.Regexp(t, `^  blank-preset\s+model=\(default\)\s+permissions=standard  dir=/work$`, lines[4])
}

func TestWriteDoctorValidation(t *testing.T) {
	cfg := config.Default()
	var nerrs, nwarns int
	out := doctorOutput(t, func() { nerrs, nwarns = writeDoctorValidation(&cfg, nil) })
	assert.Zero(t, nerrs)
	assert.Zero(t, nwarns)
	assert.Contains(t, out, "Validation\n──────────\n  (no issues)\n")

	cfg.Server.Port = 70000
	cfg.Agents = []config.AgentConfig{{Name: "idle"}}
	out = doctorOutput(t, func() { nerrs, nwarns = writeDoctorValidation(&cfg, nil) })
	assert.Equal(t, 1, nerrs)
	assert.Equal(t, 1, nwarns)
	assert.Contains(t, out, "  [ERROR] server.port: port 70000 is out of range")
	assert.Contains(t, out, "  [WARN] agents[0].model: no model configured")
	assert.NotContains(t, out, "(no issues)")
}

func TestWriteDoctorOrphans(t *testing.T) {
	doctorTestEnv(t)
	cfg := &config.Config{Agents: []config.AgentConfig{{Name: "bot"}}}

	var nerrs, nwarns int
	out := doctorOutput(t, func() { nerrs, nwarns = writeDoctorOrphans(cfg) })
	assert.Zero(t, nerrs)
	assert.Zero(t, nwarns)
	assert.Equal(t, "\nOrphaned sessions\n─────────────────\n  (none)\n", out)
}

func TestWriteDoctorOrphans_UnlistableSessionsIsAnError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows reports listing a file as a directory as not-exist, which counts as no sessions")
	}
	doctorTestEnv(t)
	cfg := &config.Config{Agents: []config.AgentConfig{{Name: "bot"}}}

	// A sessions path that is not a directory cannot be listed, which the
	// doctor counts as an error rather than as orphans.
	sessions := filepath.Join(store.AgentDir("bot"), "sessions")
	require.NoError(t, os.MkdirAll(filepath.Dir(sessions), 0o700))
	require.NoError(t, os.WriteFile(sessions, []byte("not a dir"), 0o600))
	var nerrs, nwarns int
	out := doctorOutput(t, func() { nerrs, nwarns = writeDoctorOrphans(cfg) })
	assert.Equal(t, 1, nerrs)
	assert.Zero(t, nwarns)
	assert.Contains(t, out, `  [ERROR] agent "bot": listing sessions:`)
}

func TestWriteDoctorOrphans_CountsLeftoversAsWarnings(t *testing.T) {
	doctorTestEnv(t)
	cfg := &config.Config{Agents: []config.AgentConfig{{Name: "bot"}}}
	// The gone-task workflow was deleted but its session was left behind.
	_, err := agent.EnsureTaskSession("bot", "gone-task")
	require.NoError(t, err)

	var nerrs, nwarns int
	out := doctorOutput(t, func() { nerrs, nwarns = writeDoctorOrphans(cfg) })
	assert.Zero(t, nerrs)
	assert.Equal(t, 1, nwarns)
	assert.Contains(t, out, "[WARN] bot/gone-task")
}

func TestWriteDoctorModelCredentials(t *testing.T) {
	stubPing := func(t *testing.T, fn func(model string) error) *[]string {
		t.Helper()
		var pinged []string
		orig := doctorPingModel
		doctorPingModel = func(_ context.Context, _ *llm.Factory, model string) error {
			pinged = append(pinged, model)
			return fn(model)
		}
		t.Cleanup(func() { doctorPingModel = orig })
		return &pinged
	}

	t.Run("no models", func(t *testing.T) {
		pinged := stubPing(t, func(string) error { return nil })
		var nerrs int
		out := doctorOutput(t, func() { nerrs = writeDoctorModelCredentials(&config.Config{}, nil) })
		assert.Zero(t, nerrs)
		assert.Contains(t, out, "Model credentials\n─────────────────\n  (no models configured)\n")
		assert.Empty(t, *pinged)
	})

	t.Run("ok and failing providers", func(t *testing.T) {
		pinged := stubPing(t, func(model string) error {
			if strings.HasPrefix(model, "openai/") {
				return errors.New("401 unauthorized")
			}
			return nil
		})
		cfg := &config.Config{Agents: []config.AgentConfig{
			{Name: "a", Model: "anthropic/claude-x"},
			{Name: "b", Model: "openai/gpt-x"},
		}}
		var nerrs int
		out := doctorOutput(t, func() { nerrs = writeDoctorModelCredentials(cfg, nil) })
		assert.Equal(t, 1, nerrs)
		assert.Regexp(t, `(?m)^  anthropic\s+\[OK\]$`, out)
		assert.Regexp(t, `(?m)^  openai\s+\[ERROR\] 401 unauthorized$`, out)
		assert.NotContains(t, out, "(no models configured)")
		assert.ElementsMatch(t, []string{"anthropic/claude-x", "openai/gpt-x"}, *pinged)
	})

	t.Run("credentials come from the auth store without the auth prefix", func(t *testing.T) {
		// The real ping fails before any request when the store has no
		// OAuth token, so this runs offline.
		var asked []string
		getSecret := func(key string) (string, error) {
			asked = append(asked, key)
			return "", errors.New("not found")
		}
		cfg := &config.Config{Agents: []config.AgentConfig{{Name: "c", Model: "openai-codex/gpt-x"}}}
		var nerrs int
		out := doctorOutput(t, func() { nerrs = writeDoctorModelCredentials(cfg, getSecret) })
		assert.Equal(t, 1, nerrs)
		assert.Contains(t, out, "[ERROR] openai-codex auth: missing OAuth token")
		assert.Contains(t, asked, "openai:oauth")
	})
}

// doctorCLIDefinitions defines two CLI agents: found-cli, whose binary the
// fake probe finds and reports signed in, and missing-cli, whose binary it
// does not find.
func doctorCLIDefinitions() map[string]any {
	def := func(binary string) map[string]any {
		return map[string]any{
			"binary":  binary,
			"session": map[string]any{"assign": "aviary"},
			"output":  map[string]any{"format": "gemini-json"},
			"args": map[string]any{
				"run":    []any{map[string]any{"args": []any{"--session-id", "{session}", "-p", "{prompt}"}}},
				"resume": []any{map[string]any{"args": []any{"--resume", "{session}", "-p", "{prompt}"}}},
			},
			"doctor": map[string]any{"auth": []any{"{binary}", "auth"}},
		}
	}
	return map[string]any{"found-cli": def("found-bin"), "missing-cli": def("missing-bin")}
}

func TestWriteDoctorCLIBackends(t *testing.T) {
	cfg := &config.Config{
		Agents: []config.AgentConfig{
			{Name: "found", Model: "cli/found-cli/m1"},
			{Name: "missing", Model: "cli/missing-cli/m1"},
		},
		Models: config.ModelsConfig{CLIDefinitions: doctorCLIDefinitions()},
	}
	registry, err := clidefs.FromConfig(cfg.Models.CLIDefinitions)
	require.NoError(t, err)

	var probedAuth []string
	orig := newDoctorCLIProbe
	newDoctorCLIProbe = func(r *clidefs.Registry) *clidoctor.Doctor {
		assert.Same(t, registry, r)
		return &clidoctor.Doctor{
			Registry: r,
			LookPath: func(binary string) (string, error) {
				if binary == "found-bin" {
					return "/fake/found-bin", nil
				}
				return "", errors.New("not found")
			},
			Version: func(context.Context, string) ([]byte, error) { return []byte("found-bin 1.2.3\n"), nil },
			Run: func(_ context.Context, argv []string, _ clidefs.Doctor, _ []string) ([]byte, bool) {
				probedAuth = append(probedAuth, strings.Join(argv, " "))
				return []byte("ok"), true
			},
		}
	}
	t.Cleanup(func() { newDoctorCLIProbe = orig })

	var nerrs int
	out := doctorOutput(t, func() { nerrs = writeDoctorCLIBackends(cfg, registry) })
	assert.Equal(t, 1, nerrs)
	assert.Contains(t, out, "CLI backends\n────────────\n")
	assert.Regexp(t, `(?m)^  found-cli\s+\[OK\] found-bin 1\.2\.3, signed in$`, out)
	assert.Regexp(t, `(?m)^  missing-cli\s+\[ERROR\] missing-bin CLI not found in PATH$`, out)
	assert.Equal(t, []string{"/fake/found-bin auth"}, probedAuth)
}

func TestWriteDoctorCLIBackends_NoneConfiguredPrintsNothing(t *testing.T) {
	orig := newDoctorCLIProbe
	newDoctorCLIProbe = func(*clidefs.Registry) *clidoctor.Doctor {
		t.Fatal("no backend should be probed")
		return nil
	}
	t.Cleanup(func() { newDoctorCLIProbe = orig })

	registry, err := clidefs.FromConfig(nil)
	require.NoError(t, err)
	cfg := &config.Config{Agents: []config.AgentConfig{{Name: "api", Model: "anthropic/claude-x"}}}
	var nerrs int
	out := doctorOutput(t, func() { nerrs = writeDoctorCLIBackends(cfg, registry) })
	assert.Zero(t, nerrs)
	assert.Empty(t, out)
}

func TestWriteDoctorCodeLoopAgents(t *testing.T) {
	registry, err := clidefs.FromConfig(nil)
	require.NoError(t, err)
	var nerrs int
	out := doctorOutput(t, func() { nerrs = writeDoctorCodeLoopAgents(registry, nil) })
	assert.Zero(t, nerrs)
	assert.Contains(t, out, "Code Loop agents\n───────────────\n")
	assert.Contains(t, out, "unavailable for Code Loop:")

	out = doctorOutput(t, func() { nerrs = writeDoctorCodeLoopAgents(nil, errors.New("bad cli_definitions")) })
	assert.Equal(t, 1, nerrs)
	assert.Equal(t, "\nCode Loop agents\n───────────────\n  [ERROR] bad cli_definitions\n", out)
}

func TestWriteDoctorBrowserProfile(t *testing.T) {
	doctorTestEnv(t)

	t.Run("missing directory is free", func(t *testing.T) {
		profile := filepath.Join(t.TempDir(), "profile")
		cfg := &config.Config{Browser: config.BrowserConfig{ProfileDir: "  " + profile + "  "}}
		var nerrs int
		out := doctorOutput(t, func() { nerrs = writeDoctorBrowserProfile(cfg) })
		assert.Zero(t, nerrs)
		assert.Contains(t, out, "Browser profile\n───────────────\n")
		assert.Contains(t, out, "  Directory : "+profile+"\n")
		assert.Contains(t, out, "  [OK] lock free\n")
	})

	t.Run("default directory when unset", func(t *testing.T) {
		// doctorTestEnv points the default config dir at a temp dir.
		out := doctorOutput(t, func() { writeDoctorBrowserProfile(&config.Config{}) })
		assert.Contains(t, out, "  Directory : "+filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "aviary", "browser")+"\n")
	})

	t.Run("uninspectable directory is an error", func(t *testing.T) {
		// A regular file in place of the profile directory cannot hold the
		// lock file.
		profile := filepath.Join(t.TempDir(), "profile")
		require.NoError(t, os.WriteFile(profile, []byte("x"), 0o600))
		cfg := &config.Config{Browser: config.BrowserConfig{ProfileDir: profile}}
		var nerrs int
		out := doctorOutput(t, func() { nerrs = writeDoctorBrowserProfile(cfg) })
		assert.Equal(t, 1, nerrs)
		assert.Contains(t, out, "  [ERROR] ")
		assert.NotContains(t, out, "lock free")
	})
}
