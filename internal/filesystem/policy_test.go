package filesystem

import (
	"os"
	"path/filepath"
	"regexp"
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

func TestPolicyDecideNamesTheRule(t *testing.T) {
	workspace := t.TempDir()
	policy, err := NewPolicy([]string{"./**", "!./secret/**"}, workspace)
	require.NoError(t, err)

	denied, err := ResolvePath("./secret/plan.md", workspace)
	require.NoError(t, err)
	allowed, rule := policy.Decide(denied)
	assert.False(t, allowed)
	assert.Equal(t, "!./secret/**", rule)

	outside, err := ResolvePath(filepath.Join(t.TempDir(), "x"), workspace)
	require.NoError(t, err)
	allowed, rule = policy.Decide(outside)
	assert.False(t, allowed)
	assert.Empty(t, rule)
}

func TestExpandRulesForAnotherEnforcer(t *testing.T) {
	workspace := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workspace, "docs"), 0o700))
	rules, err := ExpandRules([]string{"./docs/**", "!./docs/secret.md", "./docs", "", "@/**"}, workspace)
	require.NoError(t, err)
	docs := filepath.Join(workspace, "docs")
	assert.Equal(t, []ExpandedRule{
		{Pattern: filepath.Join(docs, "**"), Dir: docs},
		{Pattern: filepath.Join(docs, "secret.md"), Dir: docs, Negated: true},
		{Pattern: docs, Dir: docs},
		{Pattern: filepath.Join(config.BaseDir(), "**"), Dir: config.BaseDir()},
	}, rules)
}

func TestGlobToRegex(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    string
		match   []string
		noMatch []string
	}{
		{"single star stays in segment", "/a/*.go", `^/a/[^/]*\.go$`, []string{"/a/x.go", "/a/.go"}, []string{"/a/b/x.go", "/a/x.gox"}},
		{"double star crosses segments", "/a/**", `^/a/.*$`, []string{"/a/b", "/a/b/c/d"}, []string{"/b/a"}},
		{"question mark is one char", "/a/?.txt", `^/a/[^/]\.txt$`, []string{"/a/x.txt"}, []string{"/a/xy.txt", "/a//.txt"}},
		{"character class kept", "/a/[bc].md", `^/a/[bc]\.md$`, []string{"/a/b.md", "/a/c.md"}, []string{"/a/d.md"}},
		{"unclosed bracket is literal", "/a/[b", `^/a/\[b$`, []string{"/a/[b"}, []string{"/a/b"}},
		{"regex metacharacters escaped", `/a/x.+()|{}^$\`, `^/a/x\.\+\(\)\|\{\}\^\$\\$`, []string{`/a/x.+()|{}^$\`}, []string{"/a/xx+()|{}^$"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := globToRegex(tt.pattern)
			assert.Equal(t, tt.want, got)
			re := regexp.MustCompile(got)
			for _, s := range tt.match {
				assert.True(t, re.MatchString(s), "expected %q to match %q", got, s)
			}
			for _, s := range tt.noMatch {
				assert.False(t, re.MatchString(s), "expected %q not to match %q", got, s)
			}
		})
	}
}

func TestSplitPathParts(t *testing.T) {
	sep := string(filepath.Separator)
	assert.Nil(t, splitPathParts(""))
	assert.Equal(t, []string{"a", "b"}, splitPathParts("a/b"))
	assert.Equal(t, []string{sep, "a", "b"}, splitPathParts(sep+"a"+sep+sep+"b"+sep))
	assert.Equal(t, []string{"a", "b", "c"}, splitPathParts(`a\b/c`))

	if runtime.GOOS != "windows" {
		return
	}
	assert.Equal(t, []string{"C:"}, splitPathParts("C:"))
	assert.Equal(t, []string{`C:\`, "a", "b"}, splitPathParts(`C:\a\b`))
	assert.Equal(t, []string{"C:", "a"}, splitPathParts(`C:a`))
	assert.Equal(t, []string{`\\host\share\`, "dir"}, splitPathParts(`\\host\share\dir`))
}

func TestJoinPathPartsRoundTrip(t *testing.T) {
	assert.Empty(t, joinPathParts(nil))
	path := filepath.Join(t.TempDir(), "a", "b")
	assert.Equal(t, path, joinPathParts(splitPathParts(path)))
}

func TestJoinBasePreservingGlob(t *testing.T) {
	base := filepath.Join(t.TempDir(), "base")
	assert.Equal(t, base, joinBasePreservingGlob(base, ""))
	assert.Equal(t, base, joinBasePreservingGlob(base, `/\/`))
	assert.Equal(t, filepath.Join(base, "sub", "*.go"), joinBasePreservingGlob(base, `/sub\*.go`))
	assert.Equal(t, filepath.Join(base, "**", "x?"), joinBasePreservingGlob(base, "**/x?"))
}

func TestNormalizePath(t *testing.T) {
	got := normalizePath(filepath.Join("Dir", "Sub", "..", "File.TXT"))
	if runtime.GOOS == "windows" {
		assert.Equal(t, "dir/file.txt", got)
	} else {
		assert.Equal(t, "Dir/File.TXT", got)
	}
}

func TestResolvePathErrors(t *testing.T) {
	workspace := t.TempDir()

	_, err := ResolvePath("   ", workspace)
	require.ErrorContains(t, err, "path is required")

	_, err = ResolvePath("./src/*.go", workspace)
	require.ErrorContains(t, err, "glob characters are not allowed")
}

func TestResolvePathRelativeAndNonexistent(t *testing.T) {
	workspace := t.TempDir()
	realWorkspace, err := filepath.EvalSymlinks(workspace)
	require.NoError(t, err)

	got, err := ResolvePath("missing/dir/../file.txt", workspace)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(realWorkspace, "missing", "file.txt"), got)

	require.NoError(t, os.WriteFile(filepath.Join(workspace, "exists.txt"), nil, 0o600))
	got, err = ResolvePath("exists.txt", workspace)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(realWorkspace, "exists.txt"), got)
}

func TestResolvePathDefaultsToWorkspaceDir(t *testing.T) {
	workspace := t.TempDir()
	realWorkspace, err := filepath.EvalSymlinks(workspace)
	require.NoError(t, err)
	store.SetWorkspaceDir(workspace)
	t.Cleanup(func() { store.SetWorkspaceDir("") })

	got, err := ResolvePath("notes.md", "")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(realWorkspace, "notes.md"), got)

	policy, err := NewPolicy([]string{"./*.md"}, "")
	require.NoError(t, err)
	assert.True(t, policy.Allows(got))
}

func TestResolvePathSymlinkLoopFails(t *testing.T) {
	workspace := t.TempDir()
	loop := filepath.Join(workspace, "loop")
	if err := os.Symlink(loop, loop); err != nil {
		t.Skipf("symlink setup unavailable: %v", err)
	}

	_, err := ResolvePath("./loop/file.txt", workspace)
	require.ErrorContains(t, err, "resolving symlinks")

	_, err = NewPolicy([]string{"./loop/**"}, workspace)
	require.ErrorContains(t, err, `invalid allowlist rule "./loop/**"`)

	_, err = NewPolicy([]string{"./loop"}, workspace)
	require.ErrorContains(t, err, `invalid allowlist rule "./loop"`)
}

func TestHomePrefixWithoutHomeDirFails(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("home", "")
	if _, err := os.UserHomeDir(); err == nil {
		t.Skip("home directory still resolvable on this platform")
	}

	_, err := ResolvePath("~/notes.txt", t.TempDir())
	require.ErrorContains(t, err, "resolving home dir")

	_, err = NewPolicy([]string{"~/**"}, t.TempDir())
	require.ErrorContains(t, err, `invalid allowlist rule "~/**"`)

	_, err = ExpandRules([]string{"~/**"}, t.TempDir())
	require.ErrorContains(t, err, `invalid allowlist rule "~/**"`)
}

func TestNewPolicyRejectsInvalidCharacterClass(t *testing.T) {
	_, err := NewPolicy([]string{"./**", "./[z-a]"}, t.TempDir())
	require.ErrorContains(t, err, `compiling allowlist rule "./[z-a]"`)
}

func TestNewPolicySkipsBlankRulesAndTrimsNegation(t *testing.T) {
	workspace := t.TempDir()
	policy, err := NewPolicy([]string{"", "   ", "./**", "!   ./private/**"}, workspace)
	require.NoError(t, err)
	require.Len(t, policy.rules, 2)
	assert.True(t, policy.rules[1].negated)

	private, err := ResolvePath("./private/key", workspace)
	require.NoError(t, err)
	allowed, rule := policy.Decide(private)
	assert.False(t, allowed)
	assert.Equal(t, "!   ./private/**", rule)

	public, err := ResolvePath("./public/readme", workspace)
	require.NoError(t, err)
	allowed, rule = policy.Decide(public)
	assert.True(t, allowed)
	assert.Equal(t, "./**", rule)
}

func TestNilPolicyDeniesEverything(t *testing.T) {
	var policy *Policy
	allowed, rule := policy.Decide(t.TempDir())
	assert.False(t, allowed)
	assert.Empty(t, rule)
	assert.False(t, policy.Allows(t.TempDir()))
	assert.False(t, policy.MayAllowWithin(t.TempDir()))
}

func TestMayAllowWithin(t *testing.T) {
	workspace := t.TempDir()
	realWorkspace, err := filepath.EvalSymlinks(workspace)
	require.NoError(t, err)
	policy, err := NewPolicy([]string{"./src/deep/*.go", "!./secret/**"}, workspace)
	require.NoError(t, err)

	assert.True(t, policy.MayAllowWithin(realWorkspace), "ancestor of an allowed pattern")
	assert.True(t, policy.MayAllowWithin(filepath.Join(realWorkspace, "src", "deep")), "directory a glob matches beneath")
	assert.False(t, policy.MayAllowWithin(filepath.Join(realWorkspace, "secret")), "negated rules never allow")
	assert.False(t, policy.MayAllowWithin(filepath.Join(realWorkspace, "other")))
}

func TestPolicyFromAgent(t *testing.T) {
	workspace := t.TempDir()
	inside, err := ResolvePath("./data/x.txt", workspace)
	require.NoError(t, err)

	for name, cfg := range map[string]*config.AgentConfig{
		"nil config":         nil,
		"nil filesystem":     {Permissions: &config.PermissionsConfig{}},
		"empty allowed list": {Permissions: &config.PermissionsConfig{Filesystem: &config.FilesystemPermissionsConfig{}}},
	} {
		t.Run(name, func(t *testing.T) {
			policy, err := PolicyFromAgent(cfg, workspace)
			require.NoError(t, err)
			assert.False(t, policy.Allows(inside))
		})
	}

	t.Run("allowed paths relative to agent dir", func(t *testing.T) {
		cfg := &config.AgentConfig{Permissions: &config.PermissionsConfig{
			Filesystem: &config.FilesystemPermissionsConfig{AllowedPaths: []string{"./data/**"}},
		}}
		policy, err := PolicyFromAgent(cfg, workspace)
		require.NoError(t, err)
		assert.True(t, policy.Allows(inside))
		outside, err := ResolvePath("./other/x.txt", workspace)
		require.NoError(t, err)
		assert.False(t, policy.Allows(outside))
	})

	t.Run("invalid rule", func(t *testing.T) {
		cfg := &config.AgentConfig{Permissions: &config.PermissionsConfig{
			Filesystem: &config.FilesystemPermissionsConfig{AllowedPaths: []string{"./[z-a]"}},
		}}
		_, err := PolicyFromAgent(cfg, workspace)
		require.Error(t, err)
	})
}
