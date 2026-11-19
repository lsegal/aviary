package cliagent

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Status is the doctor's view of one backend.
type Status struct {
	Backend   Backend
	Installed bool
	Path      string
	Version   string
	SignedIn  bool
	// Detail describes the sign-in method, or why the check failed.
	Detail string
}

// OK reports whether the backend is installed and signed in.
func (s Status) OK() bool { return s.Installed && s.SignedIn }

// String renders the status for `aviary doctor`.
func (s Status) String() string {
	if !s.Installed {
		return fmt.Sprintf("%s CLI not found in PATH", s.Backend)
	}
	signIn := "signed in"
	if !s.SignedIn {
		signIn = "not signed in"
	}
	out := fmt.Sprintf("%s %s, %s", s.Backend, s.Version, signIn)
	if s.Detail != "" {
		out += " (" + s.Detail + ")"
	}
	return out
}

// Check reports whether the backend's CLI is installed, its version, and
// whether it is signed in.
func Check(ctx context.Context, b Backend) Status {
	st := Status{Backend: b}
	path, err := exec.LookPath(b.Binary())
	if err != nil {
		return st
	}
	st.Installed = true
	st.Path = path

	if out, err := exec.CommandContext(ctx, path, "--version").Output(); err == nil { //nolint:gosec // path comes from LookPath of a fixed name
		st.Version = parseVersion(string(out))
	}

	switch b {
	case Claude:
		out, err := exec.CommandContext(ctx, path, "auth", "status").Output() //nolint:gosec // see above
		st.SignedIn, st.Detail = parseClaudeAuthStatus(out, err)
	case Codex:
		out, err := exec.CommandContext(ctx, path, "login", "status").CombinedOutput() //nolint:gosec // see above
		st.SignedIn, st.Detail = parseCodexLoginStatus(out, err)
	}
	return st
}

// parseVersion extracts the version number from `<cli> --version` output such
// as "2.1.312 (Claude Code)" or "codex-cli 0.153.2".
func parseVersion(out string) string {
	for _, field := range strings.Fields(out) {
		if field != "" && field[0] >= '0' && field[0] <= '9' {
			return field
		}
	}
	return strings.TrimSpace(out)
}

func parseClaudeAuthStatus(out []byte, err error) (bool, string) {
	var st struct {
		LoggedIn   bool   `json:"loggedIn"`
		AuthMethod string `json:"authMethod"`
	}
	if jerr := json.Unmarshal(out, &st); jerr != nil {
		if err != nil {
			return false, err.Error()
		}
		return false, "unreadable auth status"
	}
	if !st.LoggedIn {
		return false, "run 'claude auth login'"
	}
	return true, st.AuthMethod
}

func parseCodexLoginStatus(out []byte, err error) (bool, string) {
	text := strings.TrimSpace(string(out))
	if err != nil || !strings.HasPrefix(strings.ToLower(text), "logged in") {
		return false, "run 'codex login'"
	}
	return true, strings.TrimSpace(strings.TrimPrefix(text, "Logged in using"))
}
