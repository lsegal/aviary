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

// MemoryCompactionObserver is invoked when memory compaction starts or finishes
// for an agent pool. started=true when compaction begins, false when it ends.
type MemoryCompactionObserver func(agentID, poolID string, started bool)

var memoryCompactionObserver MemoryCompactionObserver

// SetMemoryCompactionObserver registers an optional observer for memory
// compaction lifecycle events.
func SetMemoryCompactionObserver(obs MemoryCompactionObserver) {
	memoryCompactionObserver = obs
}

func notifyMemoryCompaction(agentID, poolID string, started bool) {
	if memoryCompactionObserver != nil {
		memoryCompactionObserver(agentID, poolID, started)
	}
}
