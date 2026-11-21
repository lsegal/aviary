package codingskills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallGlobalFromReleaseTag(t *testing.T) {
	m, npx, home := newTestManager(t, "2.0.0", "claude", "codex")
	// Already-current copies are refreshed too, so a re-run is the same install.
	writeSkill(t, filepath.Join(home, ".agents", "skills"), "gh-fix", "fix v2\n")

	result, err := m.InstallGlobal(context.Background())
	require.NoError(t, err)

	tree := "https://github.com/lsegal/aviary/tree/v2.0.0/.agents/skills/"
	agents := []string{"--global", "--agent", "universal", "--agent", "claude-code", "--agent", "codex", "-y"}
	var want [][]string
	for _, skill := range []string{"gh-fix", "gh-discuss", "gh-create", "gh-safe-merge"} {
		want = append(want, append([]string{"--yes", "skills", "add", tree + skill}, agents...))
	}
	assert.Equal(t, want, npx.calls)
	assert.Equal(t, []string{"gh-fix", "gh-discuss", "gh-create", "gh-safe-merge"}, result.Installed)
}

func TestInstallGlobalWithoutCLIsUsesUniversalTarget(t *testing.T) {
	m, npx, _ := newTestManager(t, "dev")
	var sources []string
	npx.onRun = func(args []string) {
		sources = append(sources, args[3])
		_, err := os.Stat(filepath.Join(args[3], "SKILL.md"))
		require.NoError(t, err, "a dev build installs the bundled copy")
		assert.Equal(t, []string{"--global", "--agent", "universal", "-y"}, args[4:])
	}

	_, err := m.InstallGlobal(context.Background())
	require.NoError(t, err)
	require.Len(t, sources, 4)
	_, err = os.Stat(sources[0])
	assert.True(t, os.IsNotExist(err), "the extracted copy is removed afterwards")
}

func TestInstallOnFirstLaunchRunsOnce(t *testing.T) {
	m, npx, _ := newTestManager(t, "2.0.0")
	dir := filepath.Join(t.TempDir(), "aviary")

	npx.err = errors.New("offline")
	m.InstallOnFirstLaunch(context.Background(), dir)
	require.Len(t, npx.calls, 1)
	_, err := os.Stat(filepath.Join(dir, firstLaunchMarker))
	assert.True(t, os.IsNotExist(err), "a failed install is retried on the next start")

	npx.err = nil
	m.InstallOnFirstLaunch(context.Background(), dir)
	require.Len(t, npx.calls, 5)
	_, err = os.Stat(filepath.Join(dir, firstLaunchMarker))
	require.NoError(t, err)

	m.InstallOnFirstLaunch(context.Background(), dir)
	assert.Len(t, npx.calls, 5, "later starts skip the install")
}
