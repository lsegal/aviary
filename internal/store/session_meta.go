package store

import (
	"errors"
	"os"
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

// SessionMeta is the sidecar metadata stored alongside a session JSONL file.
type SessionMeta struct {
	Conversation *ConversationMeta `json:"conversation,omitempty"`
}

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

// WriteSessionMeta atomically writes meta to path.
func WriteSessionMeta(path string, meta SessionMeta) error {
	return WriteJSON(path, meta)
}
