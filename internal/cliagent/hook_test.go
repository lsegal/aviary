package cliagent

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeArgs_CommandHook(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "aviary")
	hook := &CommandHook{Executable: exe, Agent: "a", Rules: []string{"gh *"}}
	args := claudeArgs(Options{Model: "opus", BuiltinTools: true, CommandHook: hook}, "", "")
	i := slices.Index(args, "--settings")
	require.GreaterOrEqual(t, i, 0)
	var settings struct {
		Hooks struct {
			PreToolUse []struct {
				Matcher string `json:"matcher"`
				Hooks   []struct {
					Type    string `json:"type"`
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	require.NoError(t, json.Unmarshal([]byte(args[i+1]), &settings))
	require.Len(t, settings.Hooks.PreToolUse, 1)
	entry := settings.Hooks.PreToolUse[0]
	assert.Equal(t, "Bash|PowerShell", entry.Matcher)
	require.Len(t, entry.Hooks, 1)
	assert.Equal(t, "command", entry.Hooks[0].Type)
	assert.Equal(t, `"`+filepath.ToSlash(exe)+`" cli-hook`, entry.Hooks[0].Command)

	env := cliEnv(Options{CommandHook: hook})
	assert.Contains(t, env, `AVIARY_COMMAND_HOOK={"agent":"a","rules":["gh *"]}`)

	args = claudeArgs(Options{Model: "opus", CommandHook: hook}, "", "")
	assert.NotContains(t, args, "--settings", "no built-in tools, nothing to check")
	args = claudeArgs(Options{Model: "opus", BuiltinTools: true}, "", "")
	assert.NotContains(t, args, "--settings")
}

func runHook(t *testing.T, config, input string) string {
	t.Helper()
	var out strings.Builder
	getenv := func(name string) string {
		if name == commandHookEnv {
			return config
		}
		return ""
	}
	require.NoError(t, RunCommandHook(strings.NewReader(input), &out, getenv))
	if out.Len() == 0 {
		return ""
	}
	var decision struct {
		Output struct {
			Event    string `json:"hookEventName"`
			Decision string `json:"permissionDecision"`
			Reason   string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	require.NoError(t, json.Unmarshal([]byte(out.String()), &decision))
	assert.Equal(t, "PreToolUse", decision.Output.Event)
	assert.Equal(t, "deny", decision.Output.Decision)
	return decision.Output.Reason
}

func TestRunCommandHook(t *testing.T) {
	config := `{"agent":"helper","rules":["gh *","!gh auth *"]}`
	call := func(command string) string {
		data, _ := json.Marshal(map[string]any{"tool_name": "Bash", "tool_input": map[string]string{"command": command}})
		return string(data)
	}

	// A read-only command outside the rules is refused, naming them (#595).
	assert.Equal(t, `Refused by Aviary: no rule in agent "helper"'s Allowed Exec Commands allows this command: echo hi`, runHook(t, config, call("echo hi")))
	assert.Equal(t, `Refused by Aviary: rule "!gh auth *" in agent "helper"'s Allowed Exec Commands denies this command: gh auth token`, runHook(t, config, call("gh auth token")))
	// An allowed command is left to run without asking.
	assert.Empty(t, runHook(t, config, call("gh pr list")))
	assert.Empty(t, runHook(t, config, `{"tool_name":"Bash","tool_input":{}}`), "no command, nothing to check")

	// A call the hook cannot check is refused rather than left to run.
	assert.Contains(t, runHook(t, config, "not json"), "Refused by Aviary: the hook input is unreadable")
	assert.Contains(t, runHook(t, "", call("echo hi")), "Refused by Aviary: the AVIARY_COMMAND_HOOK hook configuration is unreadable")
	assert.Contains(t, runHook(t, `{"agent":"helper","rules":["!"]}`, call("echo hi")), "Refused by Aviary: invalid command allowlist rule")
	assert.Contains(t, runHook(t, `{"agent":"helper","rules":[]}`, call("gh pr list")), "no rule in agent")
}
