package agent

import (
	"context"
	"sync"
	"sync/atomic"
)

type sessionProcessingObserver func(agentID, sessionID string, processing bool)

// sessionProcessingObs is read by every prompt goroutine, so it is swapped
// atomically rather than assigned directly.
var sessionProcessingObs atomic.Pointer[sessionProcessingObserver]

type sessionRunRegistry struct {
	mu        sync.Mutex
	nextID    uint64
	bySession map[string]map[uint64]context.CancelFunc
}

var runs = &sessionRunRegistry{bySession: make(map[string]map[uint64]context.CancelFunc)}

// SetSessionProcessingObserver registers an optional observer for processing
// state changes in a session.
func SetSessionProcessingObserver(obs func(agentID, sessionID string, processing bool)) {
	if obs == nil {
		sessionProcessingObs.Store(nil)
		return
	}
	o := sessionProcessingObserver(obs)
	sessionProcessingObs.Store(&o)
}

// IsSessionProcessing returns true when at least one prompt is currently active
// for the given session.
func IsSessionProcessing(agentID, sessionID string) bool {
	key := SessionRuntimeKey(agentID, sessionID)
	if key == "" {
		return false
	}
	runs.mu.Lock()
	defer runs.mu.Unlock()
	active := runs.bySession[key]
	return len(active) > 0
}

// StopSession cancels all in-flight prompts for the given session and returns
// the number of canceled runs.
func StopSession(agentID, sessionID string) int {
	key := SessionRuntimeKey(agentID, sessionID)
	if key == "" {
		return 0
	}

	runs.mu.Lock()
	active := runs.bySession[key]
	if len(active) == 0 {
		runs.mu.Unlock()
		return 0
	}
	cancels := make([]context.CancelFunc, 0, len(active))
	for _, cancel := range active {
		cancels = append(cancels, cancel)
	}
	delete(runs.bySession, key)
	runs.mu.Unlock()

	notifySessionProcessing(agentID, sessionID, false)
	for _, cancel := range cancels {
		cancel()
	}
	return len(cancels)
}

func trackSessionRun(agentID, sessionID string, cancel context.CancelFunc) func() {
	key := SessionRuntimeKey(agentID, sessionID)
	if key == "" {
		return untracked
	}
	runID, wasProcessing := addSessionRun(key, cancel)
	if !wasProcessing {
		notifySessionProcessing(agentID, sessionID, true)
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			if removeSessionRun(key, runID) {
				notifySessionProcessing(agentID, sessionID, false)
			}
		})
	}
}

// untracked is the release function of a run or call that was not tracked.
func untracked() {
	// Nothing was recorded, so there is nothing to release.
}

// addSessionRun records cancel as a run of the session at key, reporting
// whether the session already had runs.
func addSessionRun(key string, cancel context.CancelFunc) (runID uint64, wasProcessing bool) {
	runs.mu.Lock()
	defer runs.mu.Unlock()
	active := runs.bySession[key]
	wasProcessing = len(active) > 0
	if active == nil {
		active = make(map[uint64]context.CancelFunc)
		runs.bySession[key] = active
	}
	runs.nextID++
	runID = runs.nextID
	active[runID] = cancel
	return runID, wasProcessing
}

// removeSessionRun forgets the session's run, reporting whether the session
// became idle by it.
func removeSessionRun(key string, runID uint64) bool {
	runs.mu.Lock()
	defer runs.mu.Unlock()
	active := runs.bySession[key]
	if _, ok := active[runID]; !ok {
		return false
	}
	delete(active, runID)
	if len(active) > 0 {
		return false
	}
	delete(runs.bySession, key)
	return true
}

func notifySessionProcessing(agentID, sessionID string, processing bool) {
	if obs := sessionProcessingObs.Load(); obs != nil {
		(*obs)(agentID, sessionID, processing)
	}
}
