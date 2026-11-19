//go:build windows

package projects

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/projects/process"
)

const shimHelperEnv = "AVIARY_SHIM_HELPER"

// TestAgentShimHelperProcess is not a real test: the shim tests run the test
// binary behind a .cmd shim as a stand-in agent that prints the arguments it
// received.
func TestAgentShimHelperProcess(t *testing.T) {
	if os.Getenv(shimHelperEnv) != "1" {
		return
	}
	args := os.Args[1:]
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) > 0 {
		args = args[1:]
	}
	_ = json.NewEncoder(os.Stdout).Encode(args)
	os.Exit(0)
}

// runShimAgent runs binary the way aviary runs an agent and returns the
// arguments the stand-in agent received.
func runShimAgent(t *testing.T, binary string, args ...string) ([]string, error) {
	t.Helper()
	cmd := newAgentCommand(context.Background(), binary, args...)
	cmd.Env = append(os.Environ(), shimHelperEnv+"=1")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := process.Run(cmd); err != nil {
		return nil, err
	}
	var got []string
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("stand-in agent output %q: %v", stdout.String(), err)
	}
	return got, nil
}

func TestAgentCommandPassesMultiLinePromptThroughNpmShim(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "node_modules", "agent", "bin", "agent.exe")
	test, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	writeShimFile(t, exe, string(test))
	shim := filepath.Join(dir, "agent.cmd")
	writeShimFile(t, shim, strings.Replace(npmNativeShim, `@anthropic-ai\claude-code\bin\claude.exe"`, `agent\bin\agent.exe" -test.run=TestAgentShimHelperProcess --`, 1))
	prompt := "/gh-discuss 12\nRepository: owner/repo\n\nSecond paragraph & \"quoted\" %PATH%"
	got, err := runShimAgent(t, shim, "-p", prompt)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"-p", prompt}; !reflect.DeepEqual(got, want) {
		t.Fatalf("agent received %q, want %q", got, want)
	}
}

func TestAgentCommandRefusesMultiLinePromptThroughUnknownBatchFile(t *testing.T) {
	dir := t.TempDir()
	shim := filepath.Join(dir, "agent.cmd")
	writeShimFile(t, shim, "@echo off\r\nset AGENT=\"%~1\"\r\n\""+os.Args[0]+"\" -test.run=TestAgentShimHelperProcess -- %AGENT%\r\n")
	_, err := runShimAgent(t, shim, "first line\nsecond line")
	if err == nil || !strings.Contains(err.Error(), "--agent-binary") {
		t.Fatalf("multi-line prompt through an unknown batch file: err = %v, want one naming --agent-binary", err)
	}
	got, err := runShimAgent(t, shim, "one line")
	if err != nil {
		t.Fatalf("single-line prompt through an unknown batch file: %v", err)
	}
	if want := []string{"one line"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("agent received %q, want %q", got, want)
	}
}
