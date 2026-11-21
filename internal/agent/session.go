package agent

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

var idCounter atomic.Uint64

// sessionRecord is the JSON shape written as the first line of a session JSONL
// file. It intentionally omits AgentID because the agent is already encoded in
// the file path (agents/<agentID>/sessions/<sessionID>.jsonl).
type sessionRecord struct {
	ID        string             `json:"id,omitempty"`
	Name      string             `json:"name,omitempty"`
	TaskID    string             `json:"task_id,omitempty"`
	Type      domain.SessionType `json:"type,omitempty"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
}

func toSessionRecord(s *domain.Session) sessionRecord {
	return sessionRecord{
		ID:        s.ID,
		Name:      s.Name,
		TaskID:    s.TaskID,
		Type:      s.Type,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
	}
}

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
		Name:      id,
		Type:      domain.SessionTypeUser,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	path := store.SessionPath(agentID, id)
	if err := store.AppendJSONL(path, toSessionRecord(sess)); err != nil {
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
	if err := store.AppendJSONL(store.SessionPath(agentID, id), toSessionRecord(sess)); err != nil {
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
	return m.GetOrCreateNamedTyped(agentID, name, sessionTypeFor(agentID, name))
}

// GetOrCreateNamedTyped is like GetOrCreateNamed but uses the provided session
// type when creating a new session instead of inferring it from the name.
func (m *SessionManager) GetOrCreateNamedTyped(agentID, name string, typ domain.SessionType) (*domain.Session, error) {
	if name == "" {
		name = "main"
	}
	id := name

	// Try finding existing session first.
	if p := store.FindSessionPath(agentID, id); p != "" {
		// Read as generic maps and reconstruct session metadata from the
		// stored session metadata.
		lines, err := store.ReadJSONL[map[string]any](p)
		var created, updated time.Time
		var sessType domain.SessionType
		storedID := ""
		storedName := ""
		if err == nil {
			for _, m := range lines {
				if v, ok := m["id"]; ok {
					if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
						storedID = strings.TrimSpace(s)
					}
				}
				if v, ok := m["name"]; ok {
					if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
						storedName = strings.TrimSpace(s)
					}
				}
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
			sessType = sessionTypeFor(agentID, name)
		}
		if storedName == "" {
			storedName = name
		}
		if storedID == "" {
			storedID = id
		}
		sess := &domain.Session{
			ID:        storedID,
			AgentID:   agentID,
			Name:      storedName,
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
	if err := store.AppendJSONL(path, toSessionRecord(sess)); err != nil {
		return nil, fmt.Errorf("creating session %q: %w", name, err)
	}
	return sess, nil
}

// sessionTypeFor is the type of agentID's session named name: the Projects
// prompt chat is hidden, and every other name is inferred.
func sessionTypeFor(agentID, name string) domain.SessionType {
	if agentID == config.ProjectsAgentName && name == config.ProjectsChatSession {
		return domain.SessionTypeHidden
	}
	return inferSessionType(name)
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
		if err != nil || len(lines) == 0 {
			continue
		}
		// A file whose first record is not a header (no created_at) still
		// holds messages, for example when a message was appended before the
		// header was written. List it with its ID from the file name and its
		// first message time as created_at.
		header, records := lines[0], lines[1:]
		if _, ok := header["created_at"]; !ok {
			header, records = map[string]any{}, lines
		}
		var created, updated time.Time
		var sessType domain.SessionType
		if v, ok := header["created_at"]; ok {
			if s, ok := v.(string); ok {
				if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
					created = t
				}
			}
		}
		if v, ok := header["updated_at"]; ok {
			if s, ok := v.(string); ok {
				if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
					updated = t
				}
			}
		}
		if v, ok := header["type"]; ok {
			if s, ok := v.(string); ok {
				sessType = domain.SessionType(s)
			}
		}
		if created.IsZero() {
			created = firstRecordAt(records)
		}
		if created.IsZero() {
			continue
		}
		fname := strings.TrimSuffix(e.Name(), ".jsonl")
		name := fname
		id := fname
		if v, ok := header["name"]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				name = strings.TrimSpace(s)
			}
		}
		if v, ok := header["id"]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				id = strings.TrimSpace(s)
			}
		}
		if sessType == "" {
			sessType = sessionTypeFor(agentID, name)
		}
		meta, _ := store.ReadSessionMeta(store.SessionMetaPath(agentID, id))
		s := &domain.Session{
			ID:            id,
			AgentID:       agentID,
			Name:          name,
			Type:          sessType,
			Title:         meta.Title,
			LastMessageAt: latest(meta.LastMessageAt, lastMessageAt(records)),
			CreatedAt:     created,
			UpdatedAt:     updated,
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

// sessionHeader returns the session's header record, or nil when its file
// starts with a message instead.
func sessionHeader(lines []map[string]any) map[string]any {
	if len(lines) == 0 {
		return nil
	}
	if _, ok := lines[0]["created_at"]; !ok {
		return nil
	}
	return lines[0]
}

// sessionAt reads the identity of the session stored at path.
func sessionAt(agentID, sessionID, path string) (*domain.Session, []map[string]any, error) {
	lines, err := store.ReadJSONL[map[string]any](path)
	if err != nil {
		return nil, nil, fmt.Errorf("reading session %q: %w", sessionID, err)
	}
	sess := &domain.Session{ID: sessionID, AgentID: agentID, Name: strings.TrimSuffix(filepath.Base(path), ".jsonl")}
	if header := sessionHeader(lines); header != nil {
		if s, _ := header["name"].(string); strings.TrimSpace(s) != "" {
			sess.Name = strings.TrimSpace(s)
		}
		if s, _ := header["type"].(string); s != "" {
			sess.Type = domain.SessionType(s)
		}
	}
	if sess.Type == "" {
		sess.Type = sessionTypeFor(agentID, sess.Name)
	}
	return sess, lines, nil
}

// Delete removes the session file for the given session ID.
// It returns an error if the session cannot be found or deleted, or is a
// permanent session such as Main Chat, which can only be cleared.
func (m *SessionManager) Delete(agentID, sessionID string) error {
	p := store.FindSessionPath(agentID, sessionID)
	if p == "" {
		return fmt.Errorf("session %q not found", sessionID)
	}
	sess, _, err := sessionAt(agentID, sessionID, p)
	if err != nil {
		return err
	}
	if !sess.Deletable() {
		return fmt.Errorf("session %q cannot be deleted; clear it instead", sessionID)
	}
	if err := os.Remove(p); err != nil {
		return fmt.Errorf("deleting session %q: %w", sessionID, err)
	}
	for _, sidecar := range []string{store.SessionMetaPath(agentID, sessionID), store.SessionChannelsPath(agentID, sessionID)} {
		if err := os.Remove(sidecar); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("deleting session %q: %w", sessionID, err)
		}
	}
	return nil
}

// Clear removes every message from the session and keeps the session itself:
// its ID, name, type, display title, and channel bindings stay, so anything
// that points at it keeps working. The model's stored conversation and CLI
// session are dropped too, so the next prompt starts fresh.
func (m *SessionManager) Clear(agentID, sessionID string) error {
	p := store.FindSessionPath(agentID, sessionID)
	if p == "" {
		return fmt.Errorf("session %q not found", sessionID)
	}
	sess, lines, err := sessionAt(agentID, sessionID, p)
	if err != nil {
		return err
	}
	header := sessionHeader(lines)
	if header == nil {
		created := firstRecordAt(lines)
		if created.IsZero() {
			created = time.Now()
		}
		header = map[string]any{"id": sessionID, "name": sess.Name, "type": sess.Type, "created_at": created}
	}
	header["updated_at"] = time.Now()
	if err := store.RewriteJSONL(p, []map[string]any{header}); err != nil {
		return fmt.Errorf("clearing session %q: %w", sessionID, err)
	}
	err = store.UpdateSessionMeta(store.SessionMetaPath(agentID, sessionID), func(meta *store.SessionMeta) {
		meta.Conversation = nil
		meta.CLI = nil
		meta.LastMessageAt = nil
	})
	if err != nil {
		return fmt.Errorf("clearing session %q: %w", sessionID, err)
	}
	notifySessionMessage(agentID, sessionID, "")
	return nil
}

// Rename sets the session's display title. The session keeps its name, which
// channels and the default "main" session look it up by.
func (m *SessionManager) Rename(agentID, sessionID, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("session name is required")
	}
	if store.FindSessionPath(agentID, sessionID) == "" {
		return fmt.Errorf("session %q not found", sessionID)
	}
	err := store.UpdateSessionMeta(store.SessionMetaPath(agentID, sessionID), func(meta *store.SessionMeta) {
		meta.Title = title
	})
	if err != nil {
		return fmt.Errorf("renaming session %q: %w", sessionID, err)
	}
	notifySessionMessage(agentID, sessionID, "")
	return nil
}

// SessionTitle returns the session's display title, or "" when it has none.
func SessionTitle(agentID, sessionID string) string {
	meta, err := store.ReadSessionMeta(store.SessionMetaPath(agentID, sessionID))
	if err != nil {
		return ""
	}
	return meta.Title
}

// lastMessageAt returns the newest user or assistant message time among a
// session's records, or nil when it has no such message.
func lastMessageAt(records []map[string]any) *time.Time {
	var last time.Time
	for _, rec := range records {
		role, _ := rec["role"].(string)
		if role != string(domain.MessageRoleUser) && role != string(domain.MessageRoleAssistant) {
			continue
		}
		s, _ := rec["timestamp"].(string)
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil && t.After(last) {
			last = t
		}
	}
	if last.IsZero() {
		return nil
	}
	return &last
}

// firstRecordAt returns the earliest non-zero timestamp among records, or the
// zero time when none carries one. Response markers carry a zero timestamp.
func firstRecordAt(records []map[string]any) time.Time {
	var first time.Time
	for _, rec := range records {
		s, _ := rec["timestamp"].(string)
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil && !t.IsZero() && (first.IsZero() || t.Before(first)) {
			first = t
		}
	}
	return first
}

// latest returns the later of two optional times.
func latest(a, b *time.Time) *time.Time {
	if a == nil || (b != nil && b.After(*a)) {
		return b
	}
	return a
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
	return appendSessionMessage(agentID, sessionID, domain.Message{
		ID:        newID("msg"),
		Role:      role,
		Sender:    sender,
		Content:   content,
		Timestamp: time.Now(),
	})
}

// appendSessionMessage persists msg to the session, records it as the
// session's last message when it is a user or assistant message, and fires the
// session-message observer.
func appendSessionMessage(agentID, sessionID string, msg domain.Message) error {
	if err := store.AppendJSONL(store.SessionPath(agentID, sessionID), msg); err != nil {
		return err
	}
	if msg.Role == domain.MessageRoleUser || msg.Role == domain.MessageRoleAssistant {
		ts := msg.Timestamp
		err := store.UpdateSessionMeta(store.SessionMetaPath(agentID, sessionID), func(meta *store.SessionMeta) {
			if meta.LastMessageAt == nil || ts.After(*meta.LastMessageAt) {
				meta.LastMessageAt = &ts
			}
		})
		if err != nil {
			slog.Warn("agent: failed to record last message time", "agent", agentID, "session", sessionID, "err", err)
		}
	}
	notifySessionMessage(agentID, sessionID, string(msg.Role))
	return nil
}

// AppendReplyToSession appends an assistant reply to a session and forwards it
// to any registered delivery targets for that session.
func AppendReplyToSession(agentID, sessionID, content string) error {
	if err := AppendMessageToSessionWithSender(agentID, sessionID, domain.MessageRoleAssistant, content, nil); err != nil {
		return err
	}
	deliverToSession(agentID, sessionID, content)
	return nil
}

// AppendMediaMessageToSession appends a message with optional text and media to
// an existing session and fires the session-message observer.
func AppendMediaMessageToSession(agentID, sessionID string, role domain.MessageRole, content string, mediaURLs []string) error {
	return AppendMediaMessageToSessionWithSender(agentID, sessionID, role, content, mediaURLs, nil)
}

// AppendMediaMessageToSessionWithSender appends a message with optional text,
// media, and structured sender metadata to an existing session.
func AppendMediaMessageToSessionWithSender(agentID, sessionID string, role domain.MessageRole, content string, mediaURLs []string, sender *domain.MessageSender) error {
	return appendSessionMessage(agentID, sessionID, domain.Message{
		ID:        newID("msg"),
		Role:      role,
		Sender:    sender,
		Content:   content,
		MediaURLs: mediaURLs,
		Timestamp: time.Now(),
	})
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
	p := store.FindSessionPath(agentID, sessionID)
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
