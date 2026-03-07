package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	path := store.SessionPath(agentID, id)
	if err := store.AppendJSONL(path, sess); err != nil {
		return nil, fmt.Errorf("creating session: %w", err)
	}
	return sess, nil
}

// CreateWithName creates a new unique session with the given display name.
// Unlike GetOrCreateNamed, this always creates a fresh session (no dedup).
func (m *SessionManager) CreateWithName(agentID, name string) (*domain.Session, error) {
	id := newID("sess")
	sess := &domain.Session{
		ID:        id,
		AgentID:   agentID,
		Name:      name,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store.AppendJSONL(store.SessionPath(agentID, id), sess); err != nil {
		return nil, fmt.Errorf("creating session %q: %w", name, err)
	}
	return sess, nil
}

// GetOrCreate returns the agent's main session, creating it if it doesn't exist.
func (m *SessionManager) GetOrCreate(agentID string) (*domain.Session, error) {
	return m.GetOrCreateNamed(agentID, "main")
}

// GetOrCreateNamed returns the named session for an agent, creating it if needed.
// The session is stored with a deterministic ID: "{agentID}-{name}".
func (m *SessionManager) GetOrCreateNamed(agentID, name string) (*domain.Session, error) {
	if name == "" {
		name = "main"
	}
	id := agentID + "-" + name
	path := store.SessionPath(agentID, id)
	lines, err := store.ReadJSONL[domain.Session](path)
	if err == nil && len(lines) > 0 {
		return &lines[0], nil
	}
	sess := &domain.Session{
		ID:        id,
		AgentID:   agentID,
		Name:      name,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store.AppendJSONL(path, sess); err != nil {
		return nil, fmt.Errorf("creating session %q: %w", name, err)
	}
	return sess, nil
}

// List returns all sessions for agentID, sorted by creation time with "main" first.
func (m *SessionManager) List(agentID string) ([]*domain.Session, error) {
	sessDir := filepath.Join(store.AgentDir(agentID), "sessions")
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing sessions: %w", err)
	}

	var sessions []*domain.Session
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(sessDir, e.Name())
		lines, err := store.ReadJSONL[domain.Session](path)
		if err != nil || len(lines) == 0 {
			continue
		}
		s := lines[0]
		if s.AgentID != agentID {
			continue
		}
		sessions = append(sessions, &s)
	}

	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].Name == "main" {
			return true
		}
		if sessions[j].Name == "main" {
			return false
		}
		return sessions[i].CreatedAt.Before(sessions[j].CreatedAt)
	})
	return sessions, nil
}

// newID generates a simple timestamped ID with a prefix.
func newID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}
