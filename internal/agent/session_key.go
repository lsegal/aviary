package agent

import "strings"

// SessionRuntimeKey namespaces session-local runtime state by agent.
func SessionRuntimeKey(agentID, sessionID string) string {
	agentID = strings.TrimSpace(agentID)
	sessionID = strings.TrimSpace(sessionID)
	if agentID == "" {
		return sessionID
	}
	if sessionID == "" {
		return agentID
	}
	return agentID + ":" + sessionID
}
