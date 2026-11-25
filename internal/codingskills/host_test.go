package codingskills

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/clidefs"
)

// TestStatusReadsTheLockUnderXDGStateHome shows skills.sh's lock is read from
// XDG_STATE_HOME when set, and that a lock that is not JSON leaves the
// installed copies undated instead of failing the status.
func TestStatusReadsTheLockUnderXDGStateHome(t *testing.T) {
	m, _, home := newTestManager(t, "v2.0.0", "codex")
	state := filepath.Join(home, "state")
	m.Env.Getenv = func(key string) string {
		if key == "XDG_STATE_HOME" {
			return state
		}
		return ""
	}
	writeSkill(t, filepath.Join(home, ".agents", "skills"), "gh-fix", "fix v2\n")
	lock := filepath.Join(state, "skills", ".skill-lock.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(lock), 0o755))
	require.NoError(t, os.WriteFile(lock, []byte(`{"skills":{"gh-fix":{"source":"lsegal/aviary","ref":"v2.0.0"}}}`), 0o644))

	install := m.Status().Skills[0].Installs[0]
	assert.Equal(t, "2.0.0", install.Version)
	assert.Equal(t, Repo, install.Source)

	require.NoError(t, os.WriteFile(lock, []byte("{not json"), 0o644))
	install = m.Status().Skills[0].Installs[0]
	assert.True(t, install.Installed)
	assert.True(t, install.Current, "the copy still matches the bundle")
	assert.Empty(t, install.Version)
	assert.Empty(t, install.Source)
}

// TestReleaseVersionNeedsThreeNumericParts shows only an X.Y.Z version counts
// as a release with a tag to install from; anything else, including a manager
// that reports no version at all, is a development build.
func TestReleaseVersionNeedsThreeNumericParts(t *testing.T) {
	m, _, _ := newTestManager(t, "")
	for version, release := range map[string]bool{
		"v2.0.0":     true,
		"2.10.3":     true,
		"v2.0.0-rc1": false,
		"2..0":       false,
		"2.0":        false,
		"dev":        false,
	} {
		m.Version = func() string { return " " + version + " " }
		_, got := m.releaseVersion()
		assert.Equal(t, release, got, version)
	}
	m.Version = nil
	v, release := m.releaseVersion()
	assert.Equal(t, "dev", v)
	assert.False(t, release)
	assert.Equal(t, "dev", m.Status().Version)
}

// TestZeroEnvReadsTheRealHost shows an Env with no fields set falls back to
// the real home directory, environment, and PATH lookup.
func TestZeroEnvReadsTheRealHost(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "claude-config"))
	var env Env
	assert.Equal(t, filepath.Join(home, ".agents", "skills"), env.SkillsDir("codex"))
	assert.Equal(t, filepath.Join(home, "claude-config", "skills"), env.SkillsDir("claude-code"))

	exe, err := os.Executable()
	require.NoError(t, err)
	assert.True(t, env.detected(exe), "a binary that exists is detected")
	assert.False(t, env.detected(filepath.Join(home, "no-such-cli")), "a missing binary is not")
}

// TestDefaultManagerUsesTheBuiltinCLIs shows the process-wide manager is
// built once, lists the CLIs of the built-in agent definitions, and carries
// the bundled skills.
func TestDefaultManagerUsesTheBuiltinCLIs(t *testing.T) {
	m := Default()
	require.Same(t, m, Default())
	registry, err := clidefs.Builtin()
	require.NoError(t, err)
	assert.Equal(t, CLIsFromRegistry(registry), m.CLIs())
	assert.NotNil(t, m.Run)
	assert.NotNil(t, m.Version)
	data, err := fs.ReadFile(m.Bundle, ".agents/skills/gh-fix/SKILL.md")
	require.NoError(t, err)
	assert.NotEmpty(t, data)
}

// TestRunNpxReportsAMissingOrFailingNpx shows the real npx runner says Node.js
// is needed when npx is not on PATH, and reports the last line npx printed
// when it fails.
func TestRunNpxReportsAMissingOrFailingNpx(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	err := runNpx(context.Background(), "--yes", "skills")
	assert.ErrorContains(t, err, "npx is not installed; install Node.js to install skills")

	if runtime.GOOS == "windows" {
		t.Skip("fake npx needs a POSIX shell")
	}
	bin := t.TempDir()
	script := "#!/bin/sh\necho \"cloning $3\"\necho 'error: repository not found' >&2\nexit 2\n"
	require.NoError(t, os.WriteFile(filepath.Join(bin, "npx"), []byte(script), 0o755))
	t.Setenv("PATH", bin)
	err = runNpx(context.Background(), "--yes", "skills", "add")
	assert.ErrorContains(t, err, "exit status 2: error: repository not found")

	require.NoError(t, os.WriteFile(filepath.Join(bin, "npx"), []byte("#!/bin/sh\nexit 0\n"), 0o755))
	assert.NoError(t, runNpx(context.Background(), "--yes", "skills", "add"))
}

// TestRunSyncSyncsNowAndOnEveryTick shows RunSync installs right away from the
// saved config, again on each tick so a CLI set up later is covered, and
// stops once its context ends.
func TestRunSyncSyncsNowAndOnEveryTick(t *testing.T) {
	t.Setenv("AVIARY_CONFIG_BASE_DIR", t.TempDir())
	m, npx, _ := newTestManager(t, "2.0.0", "claude")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	perSync := len(testBundle) / 2 // one npx call per shipped skill
	npx.onRun = func([]string) {
		if len(npx.calls) == 2*perSync {
			cancel()
		}
	}
	done := make(chan struct{})
	go func() {
		m.RunSync(ctx, 10*time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("RunSync did not stop after its context ended")
	}
	assert.Len(t, npx.calls, 2*perSync, "one sync at start and one on the next tick")
}
