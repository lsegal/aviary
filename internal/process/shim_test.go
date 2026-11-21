package process

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func writeShimFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

// The shim npm writes for a package whose bin is a native executable, as
// @anthropic-ai/claude-code installs claude.cmd.
const npmNativeShim = `@ECHO off
GOTO start
:find_dp0
SET dp0=%~dp0
EXIT /b
:start
SETLOCAL
CALL :find_dp0
"%dp0%\node_modules\@anthropic-ai\claude-code\bin\claude.exe"   %*
`

// The shim npm writes for a package whose bin is a node script.
const npmNodeShim = `@ECHO off
GOTO start
:find_dp0
SET dp0=%~dp0
EXIT /b
:start
SETLOCAL
CALL :find_dp0

IF EXIST "%dp0%\node.exe" (
  SET "_prog=%dp0%\node.exe"
) ELSE (
  SET "_prog=node"
  SET PATHEXT=%PATHEXT:;.JS;=;%
)

endLocal & goto #_undefined_# 2>NUL || title %COMSPEC% & "%_prog%"  "%dp0%\node_modules\@openai\codex\bin\codex.js" %*
`

// The shim pnpm writes for a package whose bin is a node script.
const pnpmNodeShim = `@SETLOCAL
@IF EXIST "%~dp0\node.exe" (
  "%~dp0\node.exe"  "%~dp0\global\5\node_modules\@openai\codex\bin\codex.js" %*
) ELSE (
  @SET PATHEXT=%PATHEXT:;.JS;=;%
  node  "%~dp0\global\5\node_modules\@openai\codex\bin\codex.js" %*
)
`

func TestResolveBatchShimNpmNativeExecutable(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "node_modules", "@anthropic-ai", "claude-code", "bin", "claude.exe")
	writeShimFile(t, exe, "")
	shim := filepath.Join(dir, "claude.cmd")
	writeShimFile(t, shim, npmNativeShim)
	if got, want := resolveBatchShim(shim), []string{exe}; !reflect.DeepEqual(got, want) {
		t.Fatalf("resolveBatchShim = %q, want %q", got, want)
	}
}

func TestResolveBatchShimNpmNodeScriptPrefersBundledNode(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "node_modules", "@openai", "codex", "bin", "codex.js")
	writeShimFile(t, script, "")
	node := filepath.Join(dir, "node.exe")
	writeShimFile(t, node, "")
	shim := filepath.Join(dir, "codex.cmd")
	writeShimFile(t, shim, npmNodeShim)
	if got, want := resolveBatchShim(shim), []string{node, script}; !reflect.DeepEqual(got, want) {
		t.Fatalf("resolveBatchShim = %q, want %q", got, want)
	}
}

func TestResolveBatchShimPnpmFallsBackToNodeOnPath(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "global", "5", "node_modules", "@openai", "codex", "bin", "codex.js")
	writeShimFile(t, script, "")
	bin := t.TempDir()
	node := filepath.Join(bin, "node")
	if runtime.GOOS == "windows" {
		node += ".exe"
	}
	writeShimFile(t, node, "")
	t.Setenv("PATH", bin)
	shim := filepath.Join(dir, "codex.cmd")
	writeShimFile(t, shim, pnpmNodeShim)
	if got, want := resolveBatchShim(shim), []string{node, script}; !reflect.DeepEqual(got, want) {
		t.Fatalf("resolveBatchShim = %q, want %q", got, want)
	}
}

func TestResolveBatchShimRejectsMissingTargetsAndUnknownBatch(t *testing.T) {
	dir := t.TempDir()
	shim := filepath.Join(dir, "claude.cmd")
	writeShimFile(t, shim, npmNativeShim)
	if got := resolveBatchShim(shim); got != nil {
		t.Fatalf("resolveBatchShim with a missing target = %q, want nil", got)
	}
	custom := filepath.Join(dir, "custom.cmd")
	writeShimFile(t, custom, `@echo off
set AGENT=%~dp0agent
call "%AGENT%\run.cmd" %*
`)
	if got := resolveBatchShim(custom); got != nil {
		t.Fatalf("resolveBatchShim of a custom batch file = %q, want nil", got)
	}
}
