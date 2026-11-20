package projects

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// agentShimMaxSize bounds how much of a batch file is read when looking for
// the program it launches. npm, pnpm and Yarn shims are a few hundred bytes.
const agentShimMaxSize = 64 << 10

// agentShimArgv returns the program and arguments to start an agent with.
//
// On Windows a .cmd or .bat file can only run through cmd.exe, and cmd.exe
// ends each argument at its first newline, so a multi-line prompt handed to
// an npm shim such as claude.cmd reached the agent as its first line only
// . The shim is therefore replaced by the program it launches,
// which receives the prompt intact. A batch file aviary cannot read through is
// still run as is, unless the prompt has a line break it would lose: that is
// refused with an error naming models.cli_definitions.<agent>.binary as the way to point aviary at
// the real executable.
func agentShimArgv(binary string, args []string) (string, []string, error) {
	if runtime.GOOS != "windows" {
		return binary, args, nil
	}
	path, err := exec.LookPath(binary)
	if err != nil || !isBatchFile(path) {
		return binary, args, nil
	}
	if argv := resolveBatchShim(path); len(argv) > 0 {
		return argv[0], append(argv[1:], args...), nil
	}
	for _, arg := range args {
		if strings.ContainsAny(arg, "\r\n") {
			return binary, args, fmt.Errorf("agent binary %s is a batch file, and cmd.exe would cut its multi-line prompt at the first line break; pass the executable it launches as models.cli_definitions.<agent>.binary", path)
		}
	}
	return binary, args, nil
}

func isBatchFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cmd", ".bat":
		return true
	}
	return false
}

// resolveBatchShim reads a package-manager shim and returns the command line
// it runs ahead of the arguments it forwards with %*, or nil when the file is
// not a shim it understands or what the shim launches does not exist. npm's
// cmd-shim runs either the package's native executable or node with the
// package's script; pnpm's does the same with %~dp0 in place of %dp0%. Each
// line that forwards %* is tried in turn, so a shim choosing between a
// bundled node.exe and node on PATH resolves to whichever it would run.
func resolveBatchShim(path string) []string {
	info, err := os.Stat(path)
	if err != nil || info.Size() > agentShimMaxSize {
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	dir := filepath.Dir(path)
	for _, line := range strings.Split(string(content), "\n") {
		if argv := shimLineArgv(line, dir); len(argv) > 0 {
			return argv
		}
	}
	return nil
}

// shimLineArgv resolves one shim line of the form `... & "PROGRAM" ARGS %*`.
func shimLineArgv(line, dir string) []string {
	before, _, found := strings.Cut(line, "%*")
	if !found {
		return nil
	}
	// npm's shim runs the program after `endLocal & goto ... || title ... &`,
	// so only what follows the last unquoted & is the command.
	command := before
	quoted := false
	for i, r := range before {
		switch {
		case r == '"':
			quoted = !quoted
		case r == '&' && !quoted:
			command = before[i+1:]
		}
	}
	tokens, ok := splitShimCommand(command)
	if !ok || len(tokens) == 0 {
		return nil
	}
	nodeExe := filepath.Join(dir, "node.exe")
	program := "node"
	if fileExists(nodeExe) {
		program = nodeExe
	}
	argv := make([]string, 0, len(tokens))
	for _, token := range tokens {
		token = strings.ReplaceAll(token, "%_prog%", program)
		token = strings.ReplaceAll(token, "%dp0%", dir+`\`)
		token = strings.ReplaceAll(token, "%~dp0", dir+`\`)
		if strings.Contains(token, "%") {
			return nil
		}
		if strings.ContainsAny(token, `\/`) {
			token = filepath.Clean(filepath.FromSlash(strings.ReplaceAll(token, `\`, "/")))
			if !fileExists(token) {
				return nil
			}
		}
		argv = append(argv, token)
	}
	if !strings.ContainsAny(argv[0], `\/`) {
		resolved, err := exec.LookPath(argv[0])
		if err != nil {
			return nil
		}
		argv[0] = resolved
	}
	// A shim that only hands off to another batch file is no better than the
	// one it replaced.
	if isBatchFile(argv[0]) {
		return nil
	}
	return argv
}

// splitShimCommand splits a batch command into its words, keeping quoted
// words whole. A leading @ (echo off) or ( of an IF block is dropped. It
// reports false for a command using batch syntax it does not model.
func splitShimCommand(command string) ([]string, bool) {
	command = strings.TrimLeft(strings.TrimSpace(command), "@(")
	var tokens []string
	var word strings.Builder
	inWord, quoted := false, false
	for _, r := range command {
		switch {
		case r == '"':
			quoted = !quoted
			inWord = true
		case !quoted && (r == ' ' || r == '\t' || r == '\r'):
			if inWord {
				tokens = append(tokens, word.String())
				word.Reset()
				inWord = false
			}
		case !quoted && strings.ContainsRune("|<>^()", r):
			return nil, false
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	if quoted {
		return nil, false
	}
	if inWord {
		tokens = append(tokens, word.String())
	}
	return tokens, true
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
