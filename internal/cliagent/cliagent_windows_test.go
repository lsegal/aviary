//go:build windows

package cliagent

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// npm's shim for a package whose bin is a native executable, as
// @anthropic-ai/claude-code installs claude.cmd.
const npmClaudeShim = `@ECHO off
GOTO start
:find_dp0
SET dp0=%~dp0
EXIT /b
:start
SETLOCAL
CALL :find_dp0
"%dp0%\node_modules\@anthropic-ai\claude-code\bin\claude.exe"   %*
`

// A claude installed by npm is a claude.cmd shim, and cmd.exe cuts its command
// line at the first newline: the chat's multi-line system prompt arrived as
// its first line, and the approval flags after it were lost, so a session in
// the full mode still hit permission checks (#553).
func TestClaudeSession_NpmShimKeepsArgumentsAfterSystemPrompt(t *testing.T) {
	test, err := os.ReadFile(os.Args[0])
	require.NoError(t, err)
	dir := t.TempDir()
	exe := filepath.Join(dir, "node_modules", "@anthropic-ai", "claude-code", "bin", "claude.exe")
	require.NoError(t, os.MkdirAll(filepath.Dir(exe), 0o755))
	require.NoError(t, os.WriteFile(exe, test, 0o755))
	shim := filepath.Join(dir, "claude.cmd")
	require.NoError(t, os.WriteFile(shim, []byte(npmClaudeShim), 0o644))

	opts := fakeOptions(t, Claude)
	opts.Binary = shim
	opts.SystemPrompt = "# AGENTS.md\n\nSay \"hi\" & use %PATH% ^ carefully."
	opts.Approval = []string{"--dangerously-skip-permissions"}
	s, err := NewSession(opts)
	require.NoError(t, err)
	defer s.Close() //nolint:errcheck

	r, err := s.Send(context.Background(), Turn{Text: "one"}, nil)
	require.NoError(t, err)
	assert.Contains(t, r.Text, "skip=true")
	assert.Contains(t, r.Text, "system="+strconv.Quote(opts.SystemPrompt))
}
