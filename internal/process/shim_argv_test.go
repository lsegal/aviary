package process

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fakeWindowsLookPath makes ShimArgv behave as on Windows, resolving every
// binary to path (or failing with err), and restores the real seams after t.
func fakeWindowsLookPath(t *testing.T, path string, err error) {
	t.Helper()
	oldGOOS, oldLookPath := goos, lookPath
	t.Cleanup(func() { goos, lookPath = oldGOOS, oldLookPath })
	goos = "windows"
	lookPath = func(string) (string, error) { return path, err }
}

func TestShimArgvLeavesArgvUnchangedOffWindows(t *testing.T) {
	oldGOOS, oldLookPath := goos, lookPath
	t.Cleanup(func() { goos, lookPath = oldGOOS, oldLookPath })
	goos = "linux"
	lookPath = func(string) (string, error) {
		t.Fatal("lookPath called off Windows")
		return "", nil
	}
	args := []string{"-p", "line one\nline two"}
	binary, got, err := ShimArgv("claude", args)
	if err != nil || binary != "claude" || !reflect.DeepEqual(got, args) {
		t.Fatalf("ShimArgv = %q, %q, %v; want claude, %q, nil", binary, got, err, args)
	}
}

func TestShimArgvLeavesNonBatchBinariesAlone(t *testing.T) {
	args := []string{"line one\nline two"}
	for name, tc := range map[string]struct {
		path string
		err  error
	}{
		"executable":  {path: `C:\tools\claude.exe`},
		"not on PATH": {err: errors.New("executable file not found")},
	} {
		t.Run(name, func(t *testing.T) {
			fakeWindowsLookPath(t, tc.path, tc.err)
			binary, got, err := ShimArgv("claude", args)
			if err != nil || binary != "claude" || !reflect.DeepEqual(got, args) {
				t.Fatalf("ShimArgv = %q, %q, %v; want claude, %q, nil", binary, got, err, args)
			}
		})
	}
}

func TestShimArgvReplacesResolvableNpmShim(t *testing.T) {
	dir := t.TempDir()
	shim := filepath.Join(dir, "claude.cmd")
	exe := filepath.Join(dir, "node_modules", "@anthropic-ai", "claude-code", "bin", "claude.exe")
	writeShimFile(t, shim, npmNativeShim)
	writeShimFile(t, exe, "")
	fakeWindowsLookPath(t, shim, nil)

	binary, args, err := ShimArgv("claude", []string{"-p", "line one\nline two", "--yes"})
	if err != nil {
		t.Fatalf("ShimArgv error = %v", err)
	}
	if binary != exe {
		t.Fatalf("ShimArgv binary = %q, want %q", binary, exe)
	}
	if want := []string{"-p", "line one\nline two", "--yes"}; !reflect.DeepEqual(args, want) {
		t.Fatalf("ShimArgv args = %q, want %q", args, want)
	}
}

func TestShimArgvUnreadableBatchFile(t *testing.T) {
	shim := filepath.Join(t.TempDir(), "agent.BAT")
	fakeWindowsLookPath(t, shim, nil)

	for _, arg := range []string{"line one\nline two", "line one\rline two"} {
		binary, args, err := ShimArgv("agent", []string{arg, "--yes"})
		if err == nil || !strings.Contains(err.Error(), "models.cli_definitions") || !strings.Contains(err.Error(), shim) {
			t.Fatalf("ShimArgv(%q) error = %v, want one naming %s and models.cli_definitions", arg, err, shim)
		}
		if binary != "agent" || !reflect.DeepEqual(args, []string{arg, "--yes"}) {
			t.Fatalf("ShimArgv(%q) = %q, %q; want argv unchanged", arg, binary, args)
		}
	}

	args := []string{"-p", "single line", "--yes"}
	binary, got, err := ShimArgv("agent", args)
	if err != nil || binary != "agent" || !reflect.DeepEqual(got, args) {
		t.Fatalf("ShimArgv = %q, %q, %v; want agent, %q, nil", binary, got, err, args)
	}
}

func TestCommandReportsShimArgvError(t *testing.T) {
	fakeWindowsLookPath(t, filepath.Join(t.TempDir(), "agent.cmd"), nil)

	cmd := Command(context.Background(), "agent", "line one\nline two")
	if cmd.Err == nil || !strings.Contains(cmd.Err.Error(), "models.cli_definitions") {
		t.Fatalf("Command Err = %v, want the ShimArgv error", cmd.Err)
	}
	if err := cmd.Start(); err == nil || !strings.Contains(err.Error(), "models.cli_definitions") {
		t.Fatalf("Start error = %v, want the ShimArgv error", err)
	}
}

func TestCommandWithoutShimError(t *testing.T) {
	oldGOOS := goos
	t.Cleanup(func() { goos = oldGOOS })
	goos = "linux"

	// The test binary itself stands in for an executable exec can find.
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := Command(context.Background(), binary, "-p", "line one\nline two")
	if cmd.Err != nil {
		t.Fatalf("Command Err = %v, want nil", cmd.Err)
	}
	if want := []string{binary, "-p", "line one\nline two"}; !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("Command Args = %q, want %q", cmd.Args, want)
	}
}

func TestIsBatchFile(t *testing.T) {
	for path, want := range map[string]bool{
		`C:\npm\claude.cmd`: true,
		`C:\npm\claude.BAT`: true,
		`C:\npm\claude.exe`: false,
		"claude":            false,
	} {
		if got := isBatchFile(path); got != want {
			t.Errorf("isBatchFile(%q) = %v, want %v", path, got, want)
		}
	}
}
