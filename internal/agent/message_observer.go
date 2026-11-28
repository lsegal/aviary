package agent

// SessionMessageObserver is invoked whenever a session message is persisted.
type SessionMessageObserver func(agentID, sessionID, role string)

var sessionMessageObserver SessionMessageObserver

// SetSessionMessageObserver registers an optional observer for persisted
// session messages.
func SetSessionMessageObserver(obs SessionMessageObserver) {
	sessionMessageObserver = obs
}

func notifySessionMessage(agentID, sessionID, role string) {
	if sessionMessageObserver != nil {
		sessionMessageObserver(agentID, sessionID, role)
	}
}

// SessionRemovedObserver is invoked whenever a session's files are removed,
// either by deleting it or by moving it to a new ID.
type SessionRemovedObserver func(agentID, sessionID string)

var sessionRemovedObserver SessionRemovedObserver

// SetSessionRemovedObserver registers an optional observer for removed
// sessions.
func SetSessionRemovedObserver(obs SessionRemovedObserver) {
	sessionRemovedObserver = obs
}

func notifySessionRemoved(agentID, sessionID string) {
	if sessionRemovedObserver != nil {
		sessionRemovedObserver(agentID, sessionID)
	}
}
