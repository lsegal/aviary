package agent

import "context"

// NormalizeScriptToolArguments injects hidden runtime context into tool calls
// made from embedded scripts so scripts operate on the current agent/session.
func NormalizeScriptToolArguments(ctx context.Context, name string, args map[string]any) map[string]any {
	cloned := make(map[string]any, len(args)+4)
	for key, value := range args {
		cloned[key] = value
	}

	agentID, _ := SessionAgentIDFromContext(ctx)
	sessionID, _ := SessionIDFromContext(ctx)

	switch name {
	case "agent_run", "agent_run_script":
		if agentID != "" {
			cloned["name"] = agentID
		}
		if sessionID != "" {
			cloned["session_id"] = sessionID
		}
	case "session_list", "session_create", "memory_search", "memory_show", "memory_store", "memory_notes_set", "memory_clear",
		"agent_file_list", "agent_file_read", "agent_root_file_list", "agent_root_file_read",
		"agent_root_file_write", "agent_root_file_delete", "task_schedule", "agent_rules_set":
		if agentID != "" {
			cloned["agent"] = agentID
		}
	case "agent_rules_get":
		if agentID != "" {
			cloned["name"] = agentID
		}
	case "session_messages", "session_history", "session_stop", "session_remove", "session_set_target", "session_send":
		if agentID != "" {
			cloned["agent"] = agentID
		}
		if sessionID != "" {
			cloned["session_id"] = sessionID
		}
	}
	return cloned
}
