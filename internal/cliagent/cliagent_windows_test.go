//go:build windows

package cliagent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// npmNativeShim is the shim npm writes for a package whose bin is a native
// executable, as @anthropic-ai/claude-code installs claude.cmd. PACKAGE is
// replaced with the package path under node_modules.
const npmNativeShim = `@ECHO off
GOTO start
:find_dp0
SET dp0=%~dp0
EXIT /b
:start
SETLOCAL
CALL :find_dp0
"%dp0%\node_modules\PACKAGE"   %*
`

// installFakeShim installs the test binary behind an npm shim named name.cmd,
// as npm installs claude and codex on Windows, and returns the shim's path.
func installFakeShim(t *testing.T, name string) string {
	t.Helper()
	test, err := os.ReadFile(os.Args[0])
	require.NoError(t, err)
	dir := t.TempDir()
	pkg := filepath.Join(name, "bin", name+".exe")
	exe := filepath.Join(dir, "node_modules", pkg)
	require.NoError(t, os.MkdirAll(filepath.Dir(exe), 0o755))
	require.NoError(t, os.WriteFile(exe, test, 0o755))
	shim := filepath.Join(dir, name+".cmd")
	content := []byte(strings.ReplaceAll(npmNativeShim, "PACKAGE", pkg))
	require.NoError(t, os.WriteFile(shim, content, 0o644))
	return shim
}

// A CLI installed by npm is a .cmd shim, and cmd.exe cut its command line at
// the first newline: the arguments after the chat's multi-line system prompt
// were lost, the approval flags among them, so a session in the full mode
// still hit permission checks (#553). The shim is started as the executable
// it launches, so every argument arrives intact.
func TestClaudeSession_NpmShimKeepsApprovalArguments(t *testing.T) {
	opts := fakeOptions(t, Claude)
	opts.Binary = installFakeShim(t, "claude")
	opts.SystemPrompt = "# AGENTS.md\n\nSay \"hi\" & use %PATH% ^ carefully."
	opts.Approval = []string{"--dangerously-skip-permissions"}
	s, err := NewSession(opts)
	require.NoError(t, err)
	defer s.Close() //nolint:errcheck

	r, err := s.Send(context.Background(), Turn{Text: "system"}, nil)
	require.NoError(t, err)
	assert.Contains(t, r.Text, "skip=true")
	assert.Contains(t, r.Text, "text="+opts.SystemPrompt)
}

// codex takes the system prompt on its command line as developer
// instructions, which cmd.exe would expand (%PATH%) and split (&) when run
// through codex.cmd.
func TestCodexSession_NpmShimKeepsDeveloperInstructions(t *testing.T) {
	opts := fakeOptions(t, Codex)
	opts.Binary = installFakeShim(t, "codex")
	opts.SystemPrompt = "Say \"hi\" & use %PATH% ^ carefully.\nThen stop."
	s, err := NewSession(opts)
	require.NoError(t, err)
	defer s.Close() //nolint:errcheck

	r, err := s.Send(context.Background(), Turn{Text: "hello"}, nil)
	require.NoError(t, err)
	assert.Contains(t, r.Text, "developer="+tomlString(opts.SystemPrompt)+" prompt=hello")
}
