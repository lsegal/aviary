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
		Type:      domain.SessionTypeUser,
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
		Type:      domain.SessionTypeUser,
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
// The session type is inferred from the name: "main" → main, names containing
// ":" → channel, anything else → user.
func (m *SessionManager) GetOrCreateNamed(agentID, name string) (*domain.Session, error) {
	return m.GetOrCreateNamedTyped(agentID, name, inferSessionType(name))
}

// GetOrCreateNamedTyped is like GetOrCreateNamed but uses the provided session
// type when creating a new session instead of inferring it from the name.
func (m *SessionManager) GetOrCreateNamedTyped(agentID, name string, typ domain.SessionType) (*domain.Session, error) {
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
		var sessType domain.SessionType
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
				if v, ok := m["type"]; ok {
					if s, ok := v.(string); ok {
						sessType = domain.SessionType(s)
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
		if sessType == "" {
			sessType = inferSessionType(name)
		}
		sess := &domain.Session{
			ID:        id,
			AgentID:   agentID,
			Name:      name,
			Type:      sessType,
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
		Type:      typ,
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

// inferSessionType guesses the session type from the session name.
func inferSessionType(name string) domain.SessionType {
	if name == "main" {
		return domain.SessionTypeMain
	}
	if strings.Contains(name, ":") {
		return domain.SessionTypeChannel
	}
	return domain.SessionTypeUser
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
		var sessType domain.SessionType
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
			if v, ok := m["type"]; ok {
				if s, ok := v.(string); ok {
					sessType = domain.SessionType(s)
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
		if sessType == "" {
			sessType = inferSessionType(name)
		}
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
			Type:      sessType,
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
	return AppendMessageToSessionWithSender(agentID, sessionID, role, content, nil)
}

// AppendMessageToSessionWithSender appends a message with structured sender
// metadata to an existing session and fires the session-message observer so
// WebSocket clients are notified.
func AppendMessageToSessionWithSender(agentID, sessionID string, role domain.MessageRole, content string, sender *domain.MessageSender) error {
	msg := domain.Message{
		ID:        newID("msg"),
		Role:      role,
		Sender:    sender,
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
	if err := AppendMessageToSessionWithSender(agentID, sessionID, domain.MessageRoleAssistant, content, nil); err != nil {
		return err
	}
	deliverToSession(sessionID, content)
	return nil
}

// AppendMediaMessageToSession appends a message with optional text and media to
// an existing session and fires the session-message observer.
func AppendMediaMessageToSession(agentID, sessionID string, role domain.MessageRole, content, mediaURL string) error {
	return AppendMediaMessageToSessionWithSender(agentID, sessionID, role, content, mediaURL, nil)
}

// AppendMediaMessageToSessionWithSender appends a message with optional text,
// media, and structured sender metadata to an existing session.
func AppendMediaMessageToSessionWithSender(agentID, sessionID string, role domain.MessageRole, content, mediaURL string, sender *domain.MessageSender) error {
	msg := domain.Message{
		ID:        newID("msg"),
		Role:      role,
		Sender:    sender,
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

// MarkMessageResponded appends a response marker to the session JSONL indicating
// that promptMsgID was successfully answered by responseMsgID. The marker has an
// empty Role so it is filtered from normal message listings, but the response_id
// is merged into the original message when listing or looking up by ID.
func MarkMessageResponded(agentID, sessionID, promptMsgID, responseMsgID string) error {
	marker := domain.Message{
		ID:         promptMsgID,
		ResponseID: responseMsgID,
	}
	return store.AppendJSONL(store.SessionPath(agentID, sessionID), marker)
}

// HasMessageResponse reports whether promptMsgID already has a response_id
// recorded in the session JSONL (last-write-wins across all records with that ID).
func HasMessageResponse(agentID, sessionID, promptMsgID string) bool {
	p := store.FindSessionPath(sessionID)
	if p == "" {
		p = store.SessionPath(agentID, sessionID)
	}
	lines, err := store.ReadJSONL[domain.Message](p)
	if err != nil {
		return false
	}
	var responseID string
	for _, msg := range lines {
		if msg.ID == promptMsgID && msg.ResponseID != "" {
			responseID = msg.ResponseID
		}
	}
	return responseID != ""
}
