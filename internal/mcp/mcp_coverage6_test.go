package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func decodeToolJSON(t *testing.T, out string) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &m), out)
	return m
}

func numberedLines(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return b.String()
}

func TestFileEdit_ChangesOneLineInLargeFile(t *testing.T) {
	d, ctx, workspace := setupMCPWithFilesystemAgent(t, []string{"./sandbox/**"})
	path := filepath.Join(workspace, "sandbox", "big.txt")
	writeTestFile(t, path, numberedLines(3000))

	out, err := d.CallTool(ctx, "file_edit", map[string]any{
		"path":       "./sandbox/big.txt",
		"old_string": "line 1500\n",
		"new_string": "line fifteen hundred\n",
	})
	require.NoError(t, err)
	res := decodeToolJSON(t, out)
	assert.Equal(t, float64(1), res["replacements"])
	diff := res["diff"].(string)
	assert.Contains(t, diff, "-line 1500\n")
	assert.Contains(t, diff, "+line fifteen hundred\n")
	assert.Less(t, len(diff), 500, "diff should only show the changed hunk")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, strings.Replace(numberedLines(3000), "line 1500\n", "line fifteen hundred\n", 1), string(data))
}

func TestFileEdit_RefusesMissingAndAmbiguousStrings(t *testing.T) {
	d, ctx, workspace := setupMCPWithFilesystemAgent(t, []string{"./sandbox/**"})
	path := filepath.Join(workspace, "sandbox", "dup.txt")
	writeTestFile(t, path, "foo\nbar\nfoo\n")

	out, err := d.CallTool(ctx, "file_edit", map[string]any{"path": "./sandbox/dup.txt", "old_string": "baz", "new_string": "qux"})
	require.NoError(t, err)
	assert.Contains(t, out, "old_string not found")

	out, err = d.CallTool(ctx, "file_edit", map[string]any{"path": "./sandbox/dup.txt", "old_string": "foo", "new_string": "qux"})
	require.NoError(t, err)
	assert.Contains(t, out, "old_string appears 2 times")

	out, err = d.CallTool(ctx, "file_edit", map[string]any{"path": "./sandbox/dup.txt", "old_string": "foo", "new_string": "foo"})
	require.NoError(t, err)
	assert.Contains(t, out, "identical")

	out, err = d.CallTool(ctx, "file_edit", map[string]any{"path": "./sandbox/dup.txt", "old_string": "", "new_string": "x"})
	require.NoError(t, err)
	assert.Contains(t, out, "must not be empty")

	data, _ := os.ReadFile(path)
	assert.Equal(t, "foo\nbar\nfoo\n", string(data))

	out, err = d.CallTool(ctx, "file_edit", map[string]any{"path": "./sandbox/dup.txt", "old_string": "foo", "new_string": "qux", "replace_all": true})
	require.NoError(t, err)
	assert.Equal(t, float64(2), decodeToolJSON(t, out)["replacements"])
	data, _ = os.ReadFile(path)
	assert.Equal(t, "qux\nbar\nqux\n", string(data))
}

func TestFileEdit_PreservesCRLFModeAndBOM(t *testing.T) {
	d, ctx, workspace := setupMCPWithFilesystemAgent(t, []string{"./sandbox/**"})
	path := filepath.Join(workspace, "sandbox", "win.txt")
	writeTestFile(t, path, "\ufeffone\r\ntwo\r\nthree\r\n")
	require.NoError(t, os.Chmod(path, 0o600))
	before, err := os.Stat(path)
	require.NoError(t, err)

	out, err := d.CallTool(ctx, "file_edit", map[string]any{"path": "./sandbox/win.txt", "old_string": "one\ntwo", "new_string": "one\n2\nextra"})
	require.NoError(t, err)
	assert.NotContains(t, decodeToolJSON(t, out)["diff"], "\r")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "\ufeffone\r\n2\r\nextra\r\nthree\r\n", string(data))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, before.Mode().Perm(), info.Mode().Perm())

	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "atomic write must not leave temporary files")
}

func TestFileEdit_RefusesBinaryFiles(t *testing.T) {
	d, ctx, workspace := setupMCPWithFilesystemAgent(t, []string{"./sandbox/**"})
	writeTestFile(t, filepath.Join(workspace, "sandbox", "bin.dat"), "\xff\xfe\x00a")
	out, err := d.CallTool(ctx, "file_edit", map[string]any{"path": "./sandbox/bin.dat", "old_string": "a", "new_string": "b"})
	require.NoError(t, err)
	assert.Contains(t, out, "not utf-8 text")
}

func TestFileMultiEdit_AllOrNothing(t *testing.T) {
	d, ctx, workspace := setupMCPWithFilesystemAgent(t, []string{"./sandbox/**"})
	path := filepath.Join(workspace, "sandbox", "multi.go")
	writeTestFile(t, path, "alpha\nbeta\ngamma\n")

	out, err := d.CallTool(ctx, "file_multi_edit", map[string]any{
		"path": "./sandbox/multi.go",
		"edits": []map[string]any{
			{"old_string": "alpha", "new_string": "ALPHA"},
			{"old_string": "missing", "new_string": "x"},
		},
	})
	require.NoError(t, err)
	assert.Contains(t, out, "edit 2 of 2")
	assert.Contains(t, out, "no edits were applied")
	data, _ := os.ReadFile(path)
	assert.Equal(t, "alpha\nbeta\ngamma\n", string(data))

	out, err = d.CallTool(ctx, "file_multi_edit", map[string]any{
		"path": "./sandbox/multi.go",
		"edits": []map[string]any{
			{"old_string": "alpha", "new_string": "ALPHA"},
			{"old_string": "ALPHA\nbeta", "new_string": "ALPHA\nBETA"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, float64(2), decodeToolJSON(t, out)["replacements"])
	data, _ = os.ReadFile(path)
	assert.Equal(t, "ALPHA\nBETA\ngamma\n", string(data))

	out, err = d.CallTool(ctx, "file_multi_edit", map[string]any{"path": "./sandbox/multi.go", "edits": []map[string]any{}})
	require.NoError(t, err)
	assert.Contains(t, out, "edits is required")
}

func TestFileRead_OffsetLimitAndSizeCap(t *testing.T) {
	d, ctx, workspace := setupMCPWithFilesystemAgent(t, []string{"./sandbox/**"})
	writeTestFile(t, filepath.Join(workspace, "sandbox", "lines.txt"), numberedLines(50))

	out, err := d.CallTool(ctx, "file_read", map[string]any{"path": "./sandbox/lines.txt", "offset": 10, "limit": 3})
	require.NoError(t, err)
	res := decodeToolJSON(t, out)
	assert.Equal(t, "    10\tline 10\n    11\tline 11\n    12\tline 12\n", res["content"])
	assert.Equal(t, float64(50), res["total_lines"])
	assert.Equal(t, float64(12), res["end_line"])
	assert.Contains(t, res["note"], "offset 13")

	out, err = d.CallTool(ctx, "file_read", map[string]any{"path": "./sandbox/lines.txt", "offset": 99})
	require.NoError(t, err)
	assert.Contains(t, decodeToolJSON(t, out)["note"], "past the end")

	out, err = d.CallTool(ctx, "file_read", map[string]any{"path": "./sandbox/lines.txt"})
	require.NoError(t, err)
	assert.Equal(t, numberedLines(50), decodeToolJSON(t, out)["content"], "small files are still returned whole")

	big := strings.Repeat(strings.Repeat("x", 199)+"\n", 3000)
	writeTestFile(t, filepath.Join(workspace, "sandbox", "big.txt"), big)
	out, err = d.CallTool(ctx, "file_read", map[string]any{"path": "./sandbox/big.txt"})
	require.NoError(t, err)
	res = decodeToolJSON(t, out)
	assert.Equal(t, true, res["truncated"])
	assert.Contains(t, res["note"], "call file_read with offset")
	assert.True(t, strings.HasPrefix(res["content"].(string), "     1\t"))

	out, err = d.CallTool(ctx, "file_read", map[string]any{"path": "./sandbox/lines.txt", "offset": -1})
	require.NoError(t, err)
	assert.Contains(t, out, "must be >= 0")

	writeTestFile(t, filepath.Join(workspace, "sandbox", "bin.dat"), "\xff\xfe")
	out, err = d.CallTool(ctx, "file_read", map[string]any{"path": "./sandbox/bin.dat", "limit": 1})
	require.NoError(t, err)
	assert.Contains(t, out, "binary")
}

func TestFileWrite_RefusesBlindOverwriteForAgentRuns(t *testing.T) {
	d, ctx, workspace := setupMCPWithFilesystemAgent(t, []string{"./sandbox/**"})
	ctx = agent.WithWriteGuard(ctx)
	path := filepath.Join(workspace, "sandbox", "existing.txt")
	writeTestFile(t, path, "old\n")

	out, err := d.CallTool(ctx, "file_write", map[string]any{"path": "./sandbox/existing.txt", "content": "new\n"})
	require.NoError(t, err)
	assert.Contains(t, out, "read it with file_read first")

	_, err = d.CallTool(ctx, "file_read", map[string]any{"path": "./sandbox/existing.txt"})
	require.NoError(t, err)
	out, err = d.CallTool(ctx, "file_write", map[string]any{"path": "./sandbox/existing.txt", "content": "new\n"})
	require.NoError(t, err)
	res := decodeToolJSON(t, out)
	assert.Equal(t, false, res["created"])
	assert.Contains(t, res["diff"], "-old\n+new\n")

	future := time.Now().Add(time.Hour)
	require.NoError(t, os.Chtimes(path, future, future))
	out, err = d.CallTool(ctx, "file_write", map[string]any{"path": "./sandbox/existing.txt", "content": "newer\n"})
	require.NoError(t, err)
	assert.Contains(t, out, "changed since it was last read")

	out, err = d.CallTool(ctx, "file_write", map[string]any{"path": "./sandbox/brand-new.txt", "content": "hi\n"})
	require.NoError(t, err)
	assert.Equal(t, true, decodeToolJSON(t, out)["created"])
}

func setupSearchTree(t *testing.T, root string) {
	t.Helper()
	writeTestFile(t, filepath.Join(root, ".gitignore"), "build/\n*.log\n")
	writeTestFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {\n\tTODO()\n}\n")
	writeTestFile(t, filepath.Join(root, "pkg", "util.go"), "package pkg\n// TODO: one\n// todo: two\n")
	writeTestFile(t, filepath.Join(root, "pkg", "util_test.go"), "package pkg\n")
	writeTestFile(t, filepath.Join(root, "pkg", ".gitignore"), "generated.go\n")
	writeTestFile(t, filepath.Join(root, "pkg", "generated.go"), "// TODO generated\n")
	writeTestFile(t, filepath.Join(root, "build", "out.go"), "// TODO build\n")
	writeTestFile(t, filepath.Join(root, "debug.log"), "TODO log\n")
	writeTestFile(t, filepath.Join(root, ".git", "HEAD"), "TODO git\n")
}

func TestFileGlobAndGrep_InRepositoryClone(t *testing.T) {
	d, ctx, workspace := setupMCPWithFilesystemAgent(t, []string{"./repo/**"})
	setupSearchTree(t, filepath.Join(workspace, "repo"))

	out, err := d.CallTool(ctx, "file_glob", map[string]any{"pattern": "**/*.go", "path": "./repo"})
	require.NoError(t, err)
	assert.Equal(t, []any{"main.go", "pkg/util.go", "pkg/util_test.go"}, decodeToolJSON(t, out)["files"])

	out, err = d.CallTool(ctx, "file_glob", map[string]any{"pattern": "*.go", "path": "./repo", "include_ignored": true})
	require.NoError(t, err)
	assert.Equal(t, []any{"build/out.go", "main.go", "pkg/generated.go", "pkg/util.go", "pkg/util_test.go"}, decodeToolJSON(t, out)["files"])

	out, err = d.CallTool(ctx, "file_grep", map[string]any{"pattern": "TODO", "path": "./repo"})
	require.NoError(t, err)
	matches := decodeToolJSON(t, out)["matches"].([]any)
	require.Len(t, matches, 2)
	assert.Equal(t, "main.go", matches[0].(map[string]any)["file"])
	assert.Equal(t, float64(4), matches[0].(map[string]any)["line"])

	out, err = d.CallTool(ctx, "file_grep", map[string]any{"pattern": "todo", "path": "./repo", "case_insensitive": true, "mode": "count"})
	require.NoError(t, err)
	assert.Equal(t, []any{
		map[string]any{"file": "main.go", "count": float64(1)},
		map[string]any{"file": "pkg/util.go", "count": float64(2)},
	}, decodeToolJSON(t, out)["counts"])

	out, err = d.CallTool(ctx, "file_grep", map[string]any{"pattern": "package", "path": "./repo", "glob": "pkg/*.go", "mode": "files"})
	require.NoError(t, err)
	assert.Equal(t, []any{"pkg/util.go", "pkg/util_test.go"}, decodeToolJSON(t, out)["files"])

	out, err = d.CallTool(ctx, "file_grep", map[string]any{"pattern": "TODO", "path": "./repo/main.go", "context": 1})
	require.NoError(t, err)
	match := decodeToolJSON(t, out)["matches"].([]any)[0].(map[string]any)
	assert.Equal(t, []any{"func main() {"}, match["before"])
	assert.Equal(t, []any{"}"}, match["after"])

	out, err = d.CallTool(ctx, "file_grep", map[string]any{"pattern": "TODO", "path": "./repo", "limit": 1})
	require.NoError(t, err)
	assert.Equal(t, true, decodeToolJSON(t, out)["truncated"])

	out, err = d.CallTool(ctx, "file_grep", map[string]any{"pattern": "(", "path": "./repo"})
	require.NoError(t, err)
	assert.Contains(t, out, "invalid pattern")

	out, err = d.CallTool(ctx, "file_grep", map[string]any{"pattern": "x", "path": "./repo", "mode": "bogus"})
	require.NoError(t, err)
	assert.Contains(t, out, "unsupported mode")
}

func TestNewFileTools_RefuseOutsideAllowlistAndConfigDir(t *testing.T) {
	d, ctx, workspace := setupMCPWithFilesystemAgent(t, []string{"./sandbox/**", "!./sandbox/private/**"})
	writeTestFile(t, filepath.Join(workspace, "sandbox", "private", "secret.txt"), "TODO secret\n")
	writeTestFile(t, filepath.Join(workspace, "sandbox", "ok.txt"), "TODO ok\n")
	outside := t.TempDir()
	writeTestFile(t, filepath.Join(outside, "x.txt"), "TODO outside\n")

	calls := []struct {
		tool string
		args map[string]any
	}{
		{"file_edit", map[string]any{"path": filepath.Join(outside, "x.txt"), "old_string": "TODO", "new_string": "x"}},
		{"file_edit", map[string]any{"path": "./sandbox/private/secret.txt", "old_string": "TODO", "new_string": "x"}},
		{"file_multi_edit", map[string]any{"path": filepath.Join(outside, "x.txt"), "edits": []map[string]any{{"old_string": "TODO", "new_string": "x"}}}},
		{"file_read", map[string]any{"path": filepath.Join(outside, "x.txt"), "offset": 1}},
		{"file_glob", map[string]any{"pattern": "*", "path": outside}},
		{"file_grep", map[string]any{"pattern": "TODO", "path": outside}},
	}
	for _, c := range calls {
		out, err := d.CallTool(ctx, c.tool, c.args)
		require.NoError(t, err)
		assert.Contains(t, out, "refused by Aviary: the agent's Filesystem Allowed Paths", c.tool)
	}
	configCalls := map[string]map[string]any{
		"file_edit":       {"path": "@/aviary.yaml", "old_string": "a", "new_string": "b"},
		"file_multi_edit": {"path": "@/aviary.yaml", "edits": []map[string]any{{"old_string": "a", "new_string": "b"}}},
		"file_read":       {"path": "@/aviary.yaml", "offset": 1},
		"file_glob":       {"path": "@", "pattern": "*"},
		"file_grep":       {"path": "@", "pattern": "x"},
	}
	for tool, args := range configCalls {
		out, err := d.CallTool(ctx, tool, args)
		require.NoError(t, err)
		assert.Contains(t, out, "Aviary config directory", tool)
	}

	out, err := d.CallTool(ctx, "file_grep", map[string]any{"pattern": "TODO", "path": "./sandbox", "mode": "files"})
	require.NoError(t, err)
	assert.Equal(t, []any{"ok.txt"}, decodeToolJSON(t, out)["files"], "denied files are skipped")
}

func TestFilesystemGrant_GivesJobAccessToItsCloneOnly(t *testing.T) {
	d, baseCtx, _ := setupMCPWithFilesystemAgent(t, nil)
	clone := t.TempDir()
	setupSearchTree(t, clone)
	jobCtx := agent.WithFilesystemGrant(baseCtx, clone)

	out, err := d.CallTool(jobCtx, "file_read", map[string]any{"path": "main.go", "offset": 1, "limit": 1})
	require.NoError(t, err)
	assert.Equal(t, "     1\tpackage main\n", decodeToolJSON(t, out)["content"])

	out, err = d.CallTool(jobCtx, "file_edit", map[string]any{"path": "pkg/util.go", "old_string": "one", "new_string": "uno"})
	require.NoError(t, err)
	assert.Contains(t, decodeToolJSON(t, out)["diff"], "+// TODO: uno")

	out, err = d.CallTool(jobCtx, "file_grep", map[string]any{"pattern": "uno", "mode": "files"})
	require.NoError(t, err)
	assert.Equal(t, []any{"pkg/util.go"}, decodeToolJSON(t, out)["files"])

	out, err = d.CallTool(jobCtx, "file_glob", map[string]any{"pattern": "main.go"})
	require.NoError(t, err)
	assert.Equal(t, []any{"main.go"}, decodeToolJSON(t, out)["files"])

	out, err = d.CallTool(jobCtx, "file_read", map[string]any{"path": "../escape.txt"})
	require.NoError(t, err)
	assert.Contains(t, out, "outside the job's granted directory")

	// Once the job's context is gone, the agent's own context has no access.
	out, err = d.CallTool(baseCtx, "file_read", map[string]any{"path": filepath.Join(clone, "main.go")})
	require.NoError(t, err)
	assert.Contains(t, out, "no filesystem allowedPaths configured")
}

func TestFilesystemGrant_DefaultsExecCwd(t *testing.T) {
	d, ctx := setupMCPWithExecAgent(t, &config.ExecPermissionsConfig{AllowedCommands: []string{"pwd"}})
	clone := t.TempDir()
	out, err := d.CallTool(agent.WithFilesystemGrant(ctx, clone), "exec", map[string]any{"command": "pwd"})
	require.NoError(t, err)
	res := decodeToolJSON(t, out)
	assert.Equal(t, clone, res["cwd"])
}

func TestFileEditTools_ResultsFeedFileChangeEvents(t *testing.T) {
	d, ctx, workspace := setupMCPWithFilesystemAgent(t, []string{"./sandbox/**"})
	writeTestFile(t, filepath.Join(workspace, "sandbox", "a.txt"), "a\n")

	out, err := d.CallTool(ctx, "file_edit", map[string]any{"path": "./sandbox/a.txt", "old_string": "a", "new_string": "b"})
	require.NoError(t, err)
	change, ok := agent.FileChangeFromToolResult("file_edit", out)
	require.True(t, ok)
	want, err := filepath.EvalSymlinks(filepath.Join(workspace, "sandbox", "a.txt"))
	require.NoError(t, err)
	assert.Equal(t, want, change.Path)
	assert.Contains(t, change.Diff, "+b")

	out, err = d.CallTool(ctx, "file_write", map[string]any{"path": "./sandbox/new.txt", "content": "n\n"})
	require.NoError(t, err)
	change, ok = agent.FileChangeFromToolResult("file_write", out)
	require.True(t, ok)
	assert.Contains(t, change.Diff, "+n")

	_, ok = agent.FileChangeFromToolResult("file_read", out)
	assert.False(t, ok)
	_, ok = agent.FileChangeFromToolResult("file_edit", "error: old_string not found")
	assert.False(t, ok)
}
