package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

var idCounter atomic.Uint64

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
	// Avoid storing agent/session identifiers inside the JSONL file; filename
	// already encodes the session name/ID. Write only the metadata fields.
	toWrite := *sess
	toWrite.ID = ""
	toWrite.AgentID = ""
	toWrite.Name = ""
	if err := store.AppendJSONL(path, toWrite); err != nil {
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
	toWrite := *sess
	toWrite.ID = ""
	toWrite.AgentID = ""
	toWrite.Name = ""
	if err := store.AppendJSONL(store.SessionPath(agentID, id), toWrite); err != nil {
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

	// Try finding existing session first.
	if p := store.FindSessionPath(id); p != "" {
		// Read as generic maps and reconstruct session metadata from the
		// filename and any stored timestamps. Stored JSONL entries intentionally
		// omit agent/session identifiers.
		lines, err := store.ReadJSONL[map[string]any](p)
		var created, updated time.Time
		if err == nil {
			for _, m := range lines {
				if v, ok := m["created_at"]; ok {
					if s, ok := v.(string); ok {
						if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
							created = t
						}
					}
				}
				if v, ok := m["updated_at"]; ok {
					if s, ok := v.(string); ok {
						if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
							updated = t
						}
					}
				}
				if !created.IsZero() {
					break
				}
			}
		}
		if created.IsZero() {
			created = time.Now()
		}
		if updated.IsZero() {
			updated = created
		}
		sess := &domain.Session{
			ID:        id,
			AgentID:   agentID,
			Name:      name,
			CreatedAt: created,
			UpdatedAt: updated,
		}
		return sess, nil
	}

	path := store.SessionPath(agentID, id)
	sess := &domain.Session{
		ID:        id,
		AgentID:   agentID,
		Name:      name,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	toWrite := *sess
	toWrite.ID = ""
	toWrite.AgentID = ""
	toWrite.Name = ""
	if err := store.AppendJSONL(path, toWrite); err != nil {
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
		lines, err := store.ReadJSONL[map[string]any](path)
		if err != nil {
			continue
		}
		var created, updated time.Time
		for _, m := range lines {
			if v, ok := m["created_at"]; ok {
				if s, ok := v.(string); ok {
					if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
						created = t
					}
				}
			}
			if v, ok := m["updated_at"]; ok {
				if s, ok := v.(string); ok {
					if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
						updated = t
					}
				}
			}
			if !created.IsZero() {
				break
			}
		}
		if created.IsZero() {
			continue
		}
		fname := strings.TrimSuffix(e.Name(), ".jsonl")
		// Decode the filename to recover the original session name (reversing the
		// percent-encoding applied by encodeSessionName in store.SessionPath).
		name := store.DecodeSessionName(fname)
		// Reconstruct the full deterministic ID for named sessions (e.g. "main"
		// → "agent_alpha-main"). Random IDs generated by newID already start with
		// "sess_" and are globally unique, so they are used as-is.
		id := name
		if !strings.HasPrefix(fname, "sess_") {
			id = agentID + "-" + name
		}
		s := &domain.Session{
			ID:        id,
			AgentID:   agentID,
			Name:      name,
			CreatedAt: created,
			UpdatedAt: updated,
		}
		sessions = append(sessions, s)
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

// Delete removes the session file for the given session ID.
// It returns an error if the session cannot be found or deleted.
func (m *SessionManager) Delete(sessionID string) error {
	p := store.FindSessionPath(sessionID)
	if p == "" {
		return fmt.Errorf("session %q not found", sessionID)
	}
	if err := os.Remove(p); err != nil {
		return fmt.Errorf("deleting session %q: %w", sessionID, err)
	}
	return nil
}

// AppendMessageToSession appends a message to an existing session and fires
// the session-message observer so WebSocket clients are notified.
func AppendMessageToSession(agentID, sessionID string, role domain.MessageRole, content string) error {
	msg := domain.Message{
		ID:        newID("msg"),
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
	}
	if err := store.AppendJSONL(store.SessionPath(agentID, sessionID), msg); err != nil {
		return err
	}
	notifySessionMessage(sessionID, string(role))
	return nil
}

// AppendReplyToSession appends an assistant reply to a session and forwards it
// to any registered delivery targets for that session.
func AppendReplyToSession(agentID, sessionID, content string) error {
	if err := AppendMessageToSession(agentID, sessionID, domain.MessageRoleAssistant, content); err != nil {
		return err
	}
	deliverToSession(sessionID, content)
	return nil
}

// AppendMediaMessageToSession appends a message with optional text and media to
// an existing session and fires the session-message observer.
func AppendMediaMessageToSession(agentID, sessionID string, role domain.MessageRole, content, mediaURL string) error {
	msg := domain.Message{
		ID:        newID("msg"),
		Role:      role,
		Content:   content,
		MediaURL:  mediaURL,
		Timestamp: time.Now(),
	}
	if err := store.AppendJSONL(store.SessionPath(agentID, sessionID), msg); err != nil {
		return err
	}
	notifySessionMessage(sessionID, string(role))
	return nil
}

// newID generates a simple timestamped ID with a prefix.
func newID(prefix string) string {
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UnixNano(), idCounter.Add(1))
}
