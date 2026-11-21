package cliagent

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/lsegal/aviary/internal/commandpolicy"
)

// CommandHook has Claude Code check every call to its shell tools against
// an agent's Allowed Exec Commands before running it, through a PreToolUse
// hook that runs Executable's "cli-hook" command. Claude's dontAsk mode still
// runs commands it classifies as read-only, such as "echo hi", with no allow
// rule covering them, so its own allow rules alone cannot narrow the shell to
// the agent's rules (#595).
type CommandHook struct {
	// Executable is the aviary binary that answers the hook.
	Executable string
	// Agent names the agent whose rules these are, for the refusal.
	Agent string
	// Rules are the agent's ordered Allowed Exec Commands, as written.
	Rules []string
}

// commandHookEnv carries the hook's agent and rules to the hook command
// through the CLI's environment, which the hook command inherits.
const commandHookEnv = "AVIARY_COMMAND_HOOK"

// commandHookTools are the Claude Code tools whose command the hook checks.
const commandHookTools = "Bash|PowerShell"

type commandHookConfig struct {
	Agent string   `json:"agent"`
	Rules []string `json:"rules"`
}

// claudeHookSettings returns the --settings JSON that installs the hook.
func claudeHookSettings(hook *CommandHook) string {
	command := `"` + filepath.ToSlash(hook.Executable) + `" cli-hook`
	data, _ := json.Marshal(map[string]any{"hooks": map[string]any{
		"PreToolUse": []any{map[string]any{
			"matcher": commandHookTools,
			"hooks":   []any{map[string]any{"type": "command", "command": command}},
		}},
	}})
	return string(data)
}

// commandHookEnvValue returns the commandHookEnv entry for hook.
func commandHookEnvValue(hook *CommandHook) string {
	data, _ := json.Marshal(commandHookConfig{Agent: hook.Agent, Rules: hook.Rules})
	return commandHookEnv + "=" + string(data)
}

// RunCommandHook answers one Claude Code PreToolUse hook call read from in,
// with getenv supplying the hook's configuration. It writes a deny decision
// naming the responsible rule when the agent's Allowed Exec Commands refuse
// the command, as Aviary's own exec tool would, and nothing otherwise, which
// leaves the call to Claude's own permission check. A call it cannot check
// is refused rather than left to run.
func RunCommandHook(in io.Reader, out io.Writer, getenv func(string) string) error {
	var call struct {
		ToolInput struct {
			Command string `json:"command"`
		} `json:"tool_input"`
	}
	var reason string
	err := json.NewDecoder(in).Decode(&call)
	if err != nil {
		err = fmt.Errorf("the hook input is unreadable: %w", err)
	} else if command := strings.TrimSpace(call.ToolInput.Command); command != "" {
		reason, err = commandHookRefusal(getenv(commandHookEnv), command)
	}
	if err != nil {
		reason = "Refused by Aviary: " + err.Error()
	}
	if reason == "" {
		return nil
	}
	return json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName":            "PreToolUse",
		"permissionDecision":       "deny",
		"permissionDecisionReason": reason,
	}})
}

// commandHookRefusal returns why the configured rules refuse command, or ""
// when they allow it.
func commandHookRefusal(raw, command string) (string, error) {
	var cfg commandHookConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return "", fmt.Errorf("the %s hook configuration is unreadable: %w", commandHookEnv, err)
	}
	policy, err := commandpolicy.New(cfg.Rules)
	if err != nil {
		return "", err
	}
	allowed, rule := policy.Decide(command)
	if allowed {
		return "", nil
	}
	if rule == "" {
		return fmt.Sprintf("Refused by Aviary: no rule in agent %q's Allowed Exec Commands allows this command: %s", cfg.Agent, command), nil
	}
	return fmt.Sprintf("Refused by Aviary: rule %q in agent %q's Allowed Exec Commands denies this command: %s", rule, cfg.Agent, command), nil
}
