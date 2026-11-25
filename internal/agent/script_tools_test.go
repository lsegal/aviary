package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeScriptToolArguments(t *testing.T) {
	both := WithSessionID(WithSessionAgentID(context.Background(), "ctx-agent"), "ctx-session")
	agentOnly := WithSessionAgentID(context.Background(), "ctx-agent")
	sessionOnly := WithSessionID(context.Background(), "ctx-session")

	callerArgs := func() map[string]any {
		return map[string]any{
			"name":       "caller-name",
			"agent":      "caller-agent",
			"session_id": "caller-session",
			"message":    "hello",
		}
	}
	with := func(overrides map[string]any) map[string]any {
		out := callerArgs()
		for key, value := range overrides {
			out[key] = value
		}
		return out
	}

	type tc struct {
		name string
		tool string
		ctx  context.Context
		want map[string]any
	}
	var tests []tc

	for _, tool := range []string{"agent_run", "agent_run_script"} {
		tests = append(tests,
			tc{tool + " with agent and session", tool, both, with(map[string]any{"name": "ctx-agent", "session_id": "ctx-session"})},
			tc{tool + " with agent only", tool, agentOnly, with(map[string]any{"name": "ctx-agent"})},
			tc{tool + " with session only", tool, sessionOnly, with(map[string]any{"session_id": "ctx-session"})},
			tc{tool + " without context", tool, context.Background(), callerArgs()},
		)
	}
	for _, tool := range []string{"session_list", "session_create", "task_schedule", "agent_rules_set"} {
		tests = append(tests,
			tc{tool + " with agent and session", tool, both, with(map[string]any{"agent": "ctx-agent"})},
			tc{tool + " with session only", tool, sessionOnly, callerArgs()},
			tc{tool + " without context", tool, context.Background(), callerArgs()},
		)
	}
	tests = append(tests,
		tc{"agent_rules_get with agent and session", "agent_rules_get", both, with(map[string]any{"name": "ctx-agent"})},
		tc{"agent_rules_get with session only", "agent_rules_get", sessionOnly, callerArgs()},
		tc{"agent_rules_get without context", "agent_rules_get", context.Background(), callerArgs()},
	)
	for _, tool := range []string{"session_messages", "session_history", "session_stop", "session_turn", "session_remove", "session_clear", "session_rename", "session_send"} {
		tests = append(tests,
			tc{tool + " with agent and session", tool, both, with(map[string]any{"agent": "ctx-agent", "session_id": "ctx-session"})},
			tc{tool + " with agent only", tool, agentOnly, with(map[string]any{"agent": "ctx-agent"})},
			tc{tool + " with session only", tool, sessionOnly, with(map[string]any{"session_id": "ctx-session"})},
			tc{tool + " without context", tool, context.Background(), callerArgs()},
		)
	}
	tests = append(tests,
		tc{"unknown tool with agent and session", "file_read", both, callerArgs()},
		tc{"unknown tool without context", "file_read", context.Background(), callerArgs()},
	)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := callerArgs()
			got := NormalizeScriptToolArguments(tt.ctx, tt.tool, args)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, callerArgs(), args, "input map must not be mutated")
		})
	}
}

func TestNormalizeScriptToolArgumentsReturnsCopy(t *testing.T) {
	ctx := WithSessionID(WithSessionAgentID(context.Background(), "ctx-agent"), "ctx-session")

	got := NormalizeScriptToolArguments(ctx, "file_read", map[string]any{"path": "a.txt"})
	got["path"] = "b.txt"
	assert.Equal(t, map[string]any{"path": "b.txt"}, got)

	args := map[string]any{"path": "a.txt"}
	out := NormalizeScriptToolArguments(ctx, "file_read", args)
	out["extra"] = true
	assert.Equal(t, map[string]any{"path": "a.txt"}, args, "returned map must not alias the input")
}

func TestNormalizeScriptToolArgumentsNilArgs(t *testing.T) {
	ctx := WithSessionID(WithSessionAgentID(context.Background(), "ctx-agent"), "ctx-session")

	assert.Equal(t, map[string]any{"name": "ctx-agent", "session_id": "ctx-session"},
		NormalizeScriptToolArguments(ctx, "agent_run", nil))
	assert.Equal(t, map[string]any{}, NormalizeScriptToolArguments(context.Background(), "agent_run", nil))
}
