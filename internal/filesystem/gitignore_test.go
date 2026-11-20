package filesystem

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIgnoreMatcherRules(t *testing.T) {
	m := &IgnoreMatcher{}
	for _, line := range []string{"# comment", "", "*.log", "!keep.log", "/root-only.txt", "build/", "docs/**/*.tmp", `\#hash`} {
		m.AddPattern("", line)
	}
	m.AddPattern("sub", "local.txt")

	assert.True(t, m.Ignored("a.log", false))
	assert.True(t, m.Ignored("deep/b.log", false))
	assert.False(t, m.Ignored("keep.log", false))
	assert.True(t, m.Ignored("root-only.txt", false))
	assert.False(t, m.Ignored("nested/root-only.txt", false))
	assert.True(t, m.Ignored("build", true))
	assert.False(t, m.Ignored("build", false), "dir-only patterns skip files")
	assert.True(t, m.Ignored("docs/a/b/c.tmp", false))
	assert.True(t, m.Ignored("docs/c.tmp", false))
	assert.True(t, m.Ignored("#hash", false))
	assert.True(t, m.Ignored("sub/local.txt", false))
	assert.True(t, m.Ignored("sub/deeper/local.txt", false))
	assert.False(t, m.Ignored("local.txt", false), "nested rules only apply beneath their directory")
	assert.False(t, (*IgnoreMatcher)(nil).Ignored("x", false))
}

func TestWalkFilesHonorsGitignore(t *testing.T) {
	root := t.TempDir()
	for path, content := range map[string]string{
		".gitignore":       "out/\n*.bak\n",
		"a.go":             "",
		"a.bak":            "",
		"out/gen.go":       "",
		"pkg/.gitignore":   "!keep.bak\n",
		"pkg/keep.bak":     "",
		"pkg/b.go":         "",
		".git/config":      "",
		"pkg/sub/deep.txt": "",
	} {
		full := filepath.Join(root, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}

	collect := func(respect bool) []string {
		var got []string
		require.NoError(t, WalkFiles(root, respect, func(rel string, _ fs.DirEntry) error {
			got = append(got, rel)
			return nil
		}))
		return got
	}
	assert.Equal(t, []string{".gitignore", "a.go", "pkg/.gitignore", "pkg/b.go", "pkg/keep.bak", "pkg/sub/deep.txt"}, collect(true))
	assert.Equal(t, []string{".gitignore", "a.bak", "a.go", "out/gen.go", "pkg/.gitignore", "pkg/b.go", "pkg/keep.bak", "pkg/sub/deep.txt"}, collect(false))
}

func TestMatchGlob(t *testing.T) {
	assert.True(t, MatchGlob("*.go", "a/b/c.go"))
	assert.True(t, MatchGlob("**/*.go", "c.go"))
	assert.True(t, MatchGlob("**/*.go", "a/b/c.go"))
	assert.False(t, MatchGlob("src/*.go", "src/a/b.go"))
	assert.True(t, MatchGlob("src/**", "src/a/b.go"))
	assert.True(t, MatchGlob("*.{ts,vue}", "web/App.vue"))
	assert.True(t, MatchGlob("./a?.txt", "ab.txt"))
	assert.True(t, MatchGlob("", "anything"))
	assert.True(t, MatchGlob("[a-c].txt", "b.txt"))
}

func TestPolicyMayAllowWithin(t *testing.T) {
	base := t.TempDir()
	p, err := NewPolicy([]string{"./sandbox/*.txt"}, base)
	require.NoError(t, err)
	resolvedBase, err := filepath.EvalSymlinks(base)
	require.NoError(t, err)
	assert.True(t, p.MayAllowWithin(filepath.Join(resolvedBase, "sandbox")))
	assert.True(t, p.MayAllowWithin(resolvedBase))
	assert.False(t, p.MayAllowWithin(filepath.Join(resolvedBase, "other")))
	assert.False(t, (*Policy)(nil).MayAllowWithin(base))
}
