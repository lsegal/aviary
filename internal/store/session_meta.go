package store

import (
	"errors"
	"os"
	"sync"
	"time"
)

// ConversationMeta holds the provider-native conversation ID for a session and
// when it was last used. Providers that support server-side history (e.g. the
// OpenAI Responses API) populate this so subsequent requests can pass only the
// new user turn instead of replaying the full message history.
type ConversationMeta struct {
	ID         string    `json:"id"`
	LastUsedAt time.Time `json:"last_used_at"`
}

// CLISessionMeta records the CLI-native session that a CLI-backed agent
// ("cli/..." model) resumes for an Aviary session.
type CLISessionMeta struct {
	Backend string `json:"backend"`
	ID      string `json:"id"`
}

// SessionMeta is the sidecar metadata stored alongside a session JSONL file.
type SessionMeta struct {
	// Title is the session's display name: generated from its first prompt or
	// set by a rename. The session's own name stays its lookup key.
	Title        string            `json:"title,omitempty"`
	Conversation *ConversationMeta `json:"conversation,omitempty"`
	CLI          *CLISessionMeta   `json:"cli,omitempty"`
}

// sessionMetaMu serializes read-modify-write updates of session sidecars, so
// a title change cannot drop a concurrently saved conversation ID.
var sessionMetaMu sync.Mutex

// ReadSessionMeta loads the sidecar metadata for a session. Returns a zero
// SessionMeta (not an error) when the file does not exist.
func ReadSessionMeta(path string) (SessionMeta, error) {
	meta, err := ReadJSON[SessionMeta](path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SessionMeta{}, nil
		}
		return SessionMeta{}, err
	}
	return meta, nil
}

// UpdateSessionMeta applies update to the sidecar at path and writes it back
// atomically, serialized against every other update.
func UpdateSessionMeta(path string, update func(*SessionMeta)) error {
	sessionMetaMu.Lock()
	defer sessionMetaMu.Unlock()
	meta, _ := ReadSessionMeta(path)
	update(&meta)
	return WriteJSON(path, meta)
}
