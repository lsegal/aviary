package filesystem

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPolicyOrderedRulesAndNegation(t *testing.T) {
	workspace := t.TempDir()
	store.SetWorkspaceDir(workspace)
	t.Cleanup(func() { store.SetWorkspaceDir("") })

	policy, err := NewPolicy([]string{
		"./**",
		"!./secret/**",
		"./secret/allowed.txt",
	})
	require.NoError(t, err)

	allowed, err := ResolvePath("./README.md")
	require.NoError(t, err)
	assert.True(t, policy.Allows(allowed))

	denied, err := ResolvePath("./secret/plan.md")
	require.NoError(t, err)
	assert.False(t, policy.Allows(denied))

	restored, err := ResolvePath("./secret/allowed.txt")
	require.NoError(t, err)
	assert.True(t, policy.Allows(restored))
}

func TestPolicySpecialPrefixes(t *testing.T) {
	dataDir := t.TempDir()
	workspace := t.TempDir()
	store.SetDataDir(dataDir)
	store.SetWorkspaceDir(workspace)
	t.Cleanup(func() {
		store.SetDataDir("")
		store.SetWorkspaceDir("")
	})
	t.Setenv("AVIARY_CONFIG_BASE_DIR", dataDir)

	policy, err := NewPolicy([]string{"~/**", "@/**", "!@/token"})
	require.NoError(t, err)

	cfgFile, err := ResolvePath("@/notes/test.md")
	require.NoError(t, err)
	assert.True(t, policy.Allows(cfgFile))

	tokenFile, err := ResolvePath("@/token")
	require.NoError(t, err)
	assert.False(t, policy.Allows(tokenFile))

	home, err := os.UserHomeDir()
	require.NoError(t, err)
	homeFile, err := ResolvePath(filepath.Join(home, "documents", "x.txt"))
	require.NoError(t, err)
	assert.True(t, policy.Allows(homeFile))
}

func TestResolvePathBlocksTraversalOutsideResolvedBase(t *testing.T) {
	workspace := t.TempDir()
	store.SetWorkspaceDir(workspace)
	t.Cleanup(func() { store.SetWorkspaceDir("") })

	targetDir := t.TempDir()
	linkPath := filepath.Join(workspace, "linked")
	if runtime.GOOS == "windows" {
		require.NoError(t, os.Symlink(targetDir, linkPath))
	} else {
		require.NoError(t, os.Symlink(targetDir, linkPath))
	}

	resolved, err := ResolvePath("./linked/../linked/secret.txt")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(targetDir, "secret.txt"), resolved)
	assert.NotContains(t, resolved, workspace)
}

func TestResolvePathRejectsDriveRelativePaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-specific path semantics")
	}
	_, err := ResolvePath(`C:temp\file.txt`)
	require.Error(t, err)
}

func TestPolicyCanonicalizesWorkspacePrefixBeforeMatching(t *testing.T) {
	realWorkspace := t.TempDir()
	linkRoot := t.TempDir()
	linkWorkspace := filepath.Join(linkRoot, "workspace-link")
	if err := os.Symlink(realWorkspace, linkWorkspace); err != nil {
		t.Skipf("symlink setup unavailable: %v", err)
	}

	store.SetWorkspaceDir(linkWorkspace)
	t.Cleanup(func() { store.SetWorkspaceDir("") })

	policy, err := NewPolicy([]string{"./sandbox/**"})
	require.NoError(t, err)

	resolved, err := ResolvePath("./sandbox/demo.txt")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(realWorkspace, "sandbox", "demo.txt"), resolved)
	assert.True(t, policy.Allows(resolved))
}

func TestPolicyFromAgentNilSafe(t *testing.T) {
	policy, err := PolicyFromAgent(&config.AgentConfig{Name: "bot"})
	require.NoError(t, err)
	allowed, err := ResolvePath(filepath.Join(t.TempDir(), "x.txt"))
	require.NoError(t, err)
	assert.False(t, policy.Allows(allowed))
}
