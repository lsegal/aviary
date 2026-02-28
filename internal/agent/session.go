package agent

import (
	"fmt"
	"time"

	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

// SessionManager creates and persists agent sessions.
type SessionManager struct{}

// NewSessionManager creates a SessionManager.
func NewSessionManager() *SessionManager { return &SessionManager{} }

// Create creates a new session for the given agent and persists it.
func (m *SessionManager) Create(agentID string) (*domain.Session, error) {
	id := newID("sess")
	sess := &domain.Session{
		ID:        id,
		AgentID:   agentID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	path := store.SessionPath(id)
	// Create the JSONL file (empty; messages appended later).
	if err := store.AppendJSONL(path, sess); err != nil {
		return nil, fmt.Errorf("creating session: %w", err)
	}
	return sess, nil
}

// GetOrCreate returns the agent's existing main session, or creates one.
func (m *SessionManager) GetOrCreate(agentID string) (*domain.Session, error) {
	// For now, always create a new session. Phase 7 adds resumption.
	return m.Create(agentID)
}

// newID generates a simple timestamped ID with a prefix.
func newID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}
