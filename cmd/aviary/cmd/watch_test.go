package cmd

import (
	"bytes"
	"io"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/pflag"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/store"
)

// watchAgentFlags parses args with the flags watchAgent reads.
func watchAgentFlags(t *testing.T, args ...string) *pflag.FlagSet {
	t.Helper()
	f := pflag.NewFlagSet("watch", pflag.ContinueOnError)
	f.SetOutput(io.Discard)
	f.String("agent", "", "")
	f.String("model", "", "")
	f.StringArray("fallback", nil, "")
	f.String("provider", "", "")
	if err := f.Parse(args); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestWatchAgentModelFlags(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		want      watchAgentChoice
		warnings  int
		wantError string
	}{
		{name: "no flags", want: watchAgentChoice{}},
		{
			name: "model and fallbacks",
			args: []string{"--agent", "coder", "--model", "claude/opus", "--fallback", "codex/gpt-6", "--fallback", "gemini/pro"},
			want: watchAgentChoice{Name: "coder", Model: "claude/opus", Fallbacks: []string{"codex/gpt-6", "gemini/pro"}},
		},
		{
			name:     "deprecated provider",
			args:     []string{"--provider", "claude/opus"},
			want:     watchAgentChoice{Model: "claude/opus"},
			warnings: 1,
		},
		{
			name:      "model and provider",
			args:      []string{"--model", "claude/opus", "--provider", "codex/gpt-6"},
			wantError: "--provider is the deprecated name of --model; pass only --model",
		},
		{name: "empty model", args: []string{"--model", ""}, wantError: `--model "" is not a model`},
		{name: "colon shorthand", args: []string{"--model", "claude:opus"}, want: watchAgentChoice{Model: "claude:opus"}},
		{name: "bare provider", args: []string{"--provider", "claude"}, wantError: `--provider "claude" is not a model`, warnings: 1},
		{name: "bad fallback", args: []string{"--fallback", "codex/"}, wantError: `--fallback "codex/" is not a model`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var warn bytes.Buffer
			got, err := watchAgent(watchAgentFlags(t, tt.args...), &warn)
			checkWatchAgentChoice(t, got, err, tt.want, tt.wantError)
			if n := strings.Count(warn.String(), "--provider is deprecated; use --model\n"); n != tt.warnings || n != strings.Count(warn.String(), "\n") {
				t.Errorf("warnings = %q, want %d deprecation line(s)", warn.String(), tt.warnings)
			}
		})
	}
}

// checkWatchAgentChoice checks watchAgent's result: an error containing
// wantError when set, otherwise want.
func checkWatchAgentChoice(t *testing.T, got watchAgentChoice, err error, want watchAgentChoice, wantError string) {
	t.Helper()
	if wantError != "" {
		if err == nil || !strings.Contains(err.Error(), wantError) {
			t.Fatalf("error = %v, want %q", err, wantError)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != want.Name || got.Model != want.Model || !slices.Equal(got.Fallbacks, want.Fallbacks) {
		t.Errorf("choice = %+v, want %+v", got, want)
	}
}

// modelFlagWatchSessions sets up a global Aviary whose coder agent falls
// back to gemini/flash, with fake claude and codex CLIs on PATH, and returns
// a func that starts a session watching someone/existing from it, ended when
// the test ends.
func modelFlagWatchSessions(t *testing.T) func(choice watchAgentChoice, mode string) (*watchSession, error) {
	t.Helper()
	bin := t.TempDir()
	for _, name := range []string{"claude", "codex"} {
		if err := os.WriteFile(filepath.Join(bin, exeName(name)), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	configHome := t.TempDir()
	globalDir := writeGlobalAviary(t, configHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	store.SetDataDir(globalDir)
	t.Cleanup(func() { store.SetDataDir(dataDir) })
	sources, global, err := resolveGlobalWatchSources(filepath.Join(globalDir, "aviary.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	global.Agents[0].Fallbacks = []string{"gemini/flash"}
	return func(choice watchAgentChoice, mode string) (*watchSession, error) {
		session, err := startWatchSession(t.TempDir(), watchSessionParams{
			global:  global,
			sources: sources,
			choice:  choice,
			targets: []string{"someone/existing"},
			mode:    mode,
		})
		t.Cleanup(func() { endWatchSession(session, io.Discard) })
		return session, err
	}
}

// loadSingleAgentConfig loads the session config at path, failing unless it
// has exactly one agent.
func loadSingleAgentConfig(t *testing.T, path string) *config.Config {
	t.Helper()
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Agents) != 1 {
		t.Fatalf("session agents = %+v", cfg.Agents)
	}
	return cfg
}

func TestStartWatchSessionAppliesModelFlags(t *testing.T) {
	start := modelFlagWatchSessions(t)
	sessionOf := func(choice watchAgentChoice) (*config.Config, error) {
		t.Helper()
		session, err := start(choice, "")
		if err != nil {
			return nil, err
		}
		return loadSingleAgentConfig(t, session.ConfigPath), nil
	}

	checkModelFlagsOverrideJobs(t, sessionOf)
	checkNoModelFlagsKeepAgent(t, sessionOf)

	session, err := start(watchAgentChoice{}, config.ProjectModeManual)
	if err != nil {
		t.Fatal(err)
	}
	manual, err := config.Load(session.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := manual.Agents[0].Projects.Modes(); !maps.Equal(got, map[string]string{"someone/existing": config.ProjectModeManual}) {
		t.Errorf("--manual watched manual targets %v", got)
	}
	if _, err := sessionOf(watchAgentChoice{Model: "nosuch/m"}); err == nil || !strings.Contains(err.Error(), `unknown provider "nosuch"`) {
		t.Errorf("an unknown provider was accepted: %v", err)
	}
	if _, err := sessionOf(watchAgentChoice{Model: "claude/opus", Fallbacks: []string{"gemini/pro"}}); err == nil || !strings.Contains(err.Error(), "the gemini CLI is not installed") {
		t.Errorf("an uninstalled CLI was accepted: %v", err)
	}
}

// checkModelFlagsOverrideJobs checks that --model and --fallback run jobs on
// their models without changing the session's agent.
func checkModelFlagsOverrideJobs(t *testing.T, sessionOf func(watchAgentChoice) (*config.Config, error)) {
	t.Helper()
	cfg, err := sessionOf(watchAgentChoice{Model: "codex:gpt-6", Fallbacks: []string{"claude/sonnet", "claude/haiku"}})
	if err != nil {
		t.Fatal(err)
	}
	agent := cfg.Agents[0]
	if agent.Name != "coder" || agent.Model != "claude/opus" || !slices.Equal(agent.Fallbacks, []string{"gemini/flash"}) {
		t.Errorf("--model and --fallback changed the agent: %+v", agent)
	}
	if got := config.ProjectJobSpecs(cfg, "coder"); !slices.Equal(got, []string{"codex/gpt-6", "claude/sonnet", "claude/haiku"}) {
		t.Errorf("--model and --fallback ran jobs on %v", got)
	}
	if got := config.ProjectModelSource(cfg, "coder"); got != config.ProjectModelSourceOverride {
		t.Errorf("model source = %q", got)
	}
}

// checkNoModelFlagsKeepAgent checks that a session without model flags or
// --manual runs jobs on the agent's own models and saves no mode.
func checkNoModelFlagsKeepAgent(t *testing.T, sessionOf func(watchAgentChoice) (*config.Config, error)) {
	t.Helper()
	cfg, err := sessionOf(watchAgentChoice{})
	if err != nil {
		t.Fatal(err)
	}
	if got := config.ProjectJobSpecs(cfg, "coder"); !slices.Equal(got, []string{"claude/opus", "gemini/flash"}) {
		t.Errorf("no model flags ran jobs on %v, want the agent's own", got)
	}
	if got := cfg.Agents[0].Projects[0].Mode; got != "" {
		t.Errorf("a watch without --manual saved mode %q", got)
	}
}

func TestWatchModeFlag(t *testing.T) {
	t.Cleanup(func() { watchOpts.manual = false })
	if got := watchMode(); got != "" {
		t.Errorf("watchMode without --manual = %q", got)
	}
	watchOpts.manual = true
	if got := watchMode(); got != config.ProjectModeManual {
		t.Errorf("watchMode with --manual = %q", got)
	}
}

func TestWatchHelpAndCompletionsHideProvider(t *testing.T) {
	t.Cleanup(func() {
		_ = watchCmd.Flags().Set("help", "false")
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})
	// Completions first: a --help run leaves the help flag set, and cobra
	// completes nothing after it.
	for _, args := range [][]string{{"__complete", "watch", "--"}, {"watch", "--help"}} {
		var out bytes.Buffer
		rootCmd.SetOut(&out)
		rootCmd.SetErr(io.Discard)
		rootCmd.SetArgs(args)
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		for _, flag := range []string{"--model", "--fallback"} {
			if !strings.Contains(out.String(), flag) {
				t.Errorf("%v lists no %s:\n%s", args, flag, out.String())
			}
		}
		if !strings.Contains(out.String(), "--manual") {
			t.Errorf("%v lists no --manual:\n%s", args, out.String())
		}
		if strings.Contains(out.String(), "--provider") {
			t.Errorf("%v lists the deprecated --provider:\n%s", args, out.String())
		}
	}
}

// exeName is name as an executable on PATH: Windows needs the .exe extension.
func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}
