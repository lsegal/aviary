package filesystem

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPolicyOrderedRulesAndNegation(t *testing.T) {
	workspace := t.TempDir()

	policy, err := NewPolicy([]string{
		"./**",
		"!./secret/**",
		"./secret/allowed.txt",
	}, workspace)
	require.NoError(t, err)

	allowed, err := ResolvePath("./README.md", workspace)
	require.NoError(t, err)
	assert.True(t, policy.Allows(allowed))

	denied, err := ResolvePath("./secret/plan.md", workspace)
	require.NoError(t, err)
	assert.False(t, policy.Allows(denied))

	restored, err := ResolvePath("./secret/allowed.txt", workspace)
	require.NoError(t, err)
	assert.True(t, policy.Allows(restored))
}

func TestPolicySpecialPrefixes(t *testing.T) {
	dataDir := t.TempDir()
	workspace := t.TempDir()
	store.SetDataDir(dataDir)
	t.Cleanup(func() { store.SetDataDir("") })
	t.Setenv("AVIARY_CONFIG_BASE_DIR", dataDir)

	policy, err := NewPolicy([]string{"~/**", "@/**", "!@/token"}, workspace)
	require.NoError(t, err)

	cfgFile, err := ResolvePath("@/notes/test.md", workspace)
	require.NoError(t, err)
	assert.True(t, policy.Allows(cfgFile))

	tokenFile, err := ResolvePath("@/token", workspace)
	require.NoError(t, err)
	assert.False(t, policy.Allows(tokenFile))

	home, err := os.UserHomeDir()
	require.NoError(t, err)
	homeFile, err := ResolvePath(filepath.Join(home, "documents", "x.txt"), workspace)
	require.NoError(t, err)
	assert.True(t, policy.Allows(homeFile))
}

func TestResolvePathBlocksTraversalOutsideResolvedBase(t *testing.T) {
	workspace := t.TempDir()

	targetDir := t.TempDir()
	linkPath := filepath.Join(workspace, "linked")
	require.NoError(t, os.Symlink(targetDir, linkPath))

	resolved, err := ResolvePath("./linked/../linked/secret.txt", workspace)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(normalizePath(resolved), normalizePath(filepath.Join(filepath.Base(targetDir), "secret.txt"))))
	assert.NotContains(t, resolved, workspace)
}

func TestResolvePathRejectsDriveRelativePaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-specific path semantics")
	}
	_, err := ResolvePath(`C:temp\file.txt`, "")
	require.Error(t, err)
}

func TestPolicyCanonicalizesWorkspacePrefixBeforeMatching(t *testing.T) {
	realWorkspace := t.TempDir()
	linkRoot := t.TempDir()
	linkWorkspace := filepath.Join(linkRoot, "workspace-link")
	if err := os.Symlink(realWorkspace, linkWorkspace); err != nil {
		t.Skipf("symlink setup unavailable: %v", err)
	}

	policy, err := NewPolicy([]string{"./sandbox/**"}, linkWorkspace)
	require.NoError(t, err)

	resolved, err := ResolvePath("./sandbox/demo.txt", linkWorkspace)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(normalizePath(resolved), normalizePath(filepath.Join(filepath.Base(realWorkspace), "sandbox", "demo.txt"))))
	assert.True(t, policy.Allows(resolved))
}

func TestPolicyFromAgentNilSafe(t *testing.T) {
	policy, err := PolicyFromAgent(&config.AgentConfig{Name: "bot"}, "")
	require.NoError(t, err)
	allowed, err := ResolvePath(filepath.Join(t.TempDir(), "x.txt"), "")
	require.NoError(t, err)
	assert.False(t, policy.Allows(allowed))
}
