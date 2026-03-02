package agent

import (
	"context"
	"sync"
)

type sessionProcessingObserver func(sessionID string, processing bool)

var sessionProcessingObs sessionProcessingObserver

type sessionRunRegistry struct {
	mu        sync.Mutex
	nextID    uint64
	bySession map[string]map[uint64]context.CancelFunc
}

var runs = &sessionRunRegistry{bySession: make(map[string]map[uint64]context.CancelFunc)}

// SetSessionProcessingObserver registers an optional observer for processing
// state changes in a session.
func SetSessionProcessingObserver(obs func(sessionID string, processing bool)) {
	sessionProcessingObs = obs
}

// IsSessionProcessing returns true when at least one prompt is currently active
// for the given session.
func IsSessionProcessing(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	runs.mu.Lock()
	defer runs.mu.Unlock()
	active := runs.bySession[sessionID]
	return len(active) > 0
}

// StopSession cancels all in-flight prompts for the given session and returns
// the number of canceled runs.
func StopSession(sessionID string) int {
	if sessionID == "" {
		return 0
	}

	runs.mu.Lock()
	active := runs.bySession[sessionID]
	if len(active) == 0 {
		runs.mu.Unlock()
		return 0
	}
	cancels := make([]context.CancelFunc, 0, len(active))
	for _, cancel := range active {
		cancels = append(cancels, cancel)
	}
	delete(runs.bySession, sessionID)
	runs.mu.Unlock()

	notifySessionProcessing(sessionID, false)
	for _, cancel := range cancels {
		cancel()
	}
	return len(cancels)
}

func trackSessionRun(sessionID string, cancel context.CancelFunc) func() {
	if sessionID == "" {
		return func() {}
	}

	runs.mu.Lock()
	active := runs.bySession[sessionID]
	wasProcessing := len(active) > 0
	if active == nil {
		active = make(map[uint64]context.CancelFunc)
		runs.bySession[sessionID] = active
	}
	runs.nextID++
	runID := runs.nextID
	active[runID] = cancel
	runs.mu.Unlock()

	if !wasProcessing {
		notifySessionProcessing(sessionID, true)
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			becameIdle := false
			runs.mu.Lock()
			if active := runs.bySession[sessionID]; active != nil {
				if _, ok := active[runID]; ok {
					delete(active, runID)
					if len(active) == 0 {
						delete(runs.bySession, sessionID)
						becameIdle = true
					}
				}
			}
			runs.mu.Unlock()
			if becameIdle {
				notifySessionProcessing(sessionID, false)
			}
		})
	}
}

func notifySessionProcessing(sessionID string, processing bool) {
	if sessionProcessingObs != nil {
		sessionProcessingObs(sessionID, processing)
	}
}
