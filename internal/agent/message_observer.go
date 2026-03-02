package agent

// SessionMessageObserver is invoked whenever a session message is persisted.
type SessionMessageObserver func(sessionID, role string)

var sessionMessageObserver SessionMessageObserver

// SetSessionMessageObserver registers an optional observer for persisted
// session messages.
func SetSessionMessageObserver(obs SessionMessageObserver) {
	sessionMessageObserver = obs
}

func notifySessionMessage(sessionID, role string) {
	if sessionMessageObserver != nil {
		sessionMessageObserver(sessionID, role)
	}
}
