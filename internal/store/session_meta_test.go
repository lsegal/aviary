package store

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadSessionMeta covers the missing, malformed, and valid sidecar paths.
func TestReadSessionMeta(t *testing.T) {
	tmp := t.TempDir()

	t.Run("missing_file_is_zero_meta", func(t *testing.T) {
		meta, err := ReadSessionMeta(filepath.Join(tmp, "missing.meta.json"))
		assert.NoError(t, err)
		assert.Equal(t, SessionMeta{}, meta)
	})

	t.Run("malformed_json_errors", func(t *testing.T) {
		p := filepath.Join(tmp, "bad.meta.json")
		require.NoError(t, os.WriteFile(p, []byte(`{"title":`), 0o600))

		meta, err := ReadSessionMeta(p)
		assert.ErrorContains(t, err, "parsing")
		assert.Equal(t, SessionMeta{}, meta)
	})

	t.Run("valid_file", func(t *testing.T) {
		p := filepath.Join(tmp, "ok.meta.json")
		body := `{"title":"Plan trip","conversation":{"id":"resp_1","last_used_at":"2026-01-02T03:04:05Z"},"cli":{"backend":"claude","id":"sess-9"},"last_message_at":"2026-01-02T03:05:00Z"}`
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))

		meta, err := ReadSessionMeta(p)
		require.NoError(t, err)
		assert.Equal(t, "Plan trip", meta.Title)
		require.NotNil(t, meta.Conversation)
		assert.Equal(t, "resp_1", meta.Conversation.ID)
		assert.True(t, meta.Conversation.LastUsedAt.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)))
		assert.Equal(t, &CLISessionMeta{Backend: "claude", ID: "sess-9"}, meta.CLI)
		require.NotNil(t, meta.LastMessageAt)
		assert.True(t, meta.LastMessageAt.Equal(time.Date(2026, 1, 2, 3, 5, 0, 0, time.UTC)))
	})
}

// TestUpdateSessionMeta covers creating, round-tripping, and preserving
// fields across updates, plus the write error path.
func TestUpdateSessionMeta(t *testing.T) {
	t.Run("round_trips_all_fields", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "sessions", "main.meta.json")
		usedAt := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
		lastAt := usedAt.Add(time.Minute)

		require.NoError(t, UpdateSessionMeta(p, func(m *SessionMeta) {
			assert.Equal(t, SessionMeta{}, *m, "missing sidecar starts empty")
			m.Title = "Renamed"
			m.Conversation = &ConversationMeta{ID: "resp_42", LastUsedAt: usedAt}
			m.CLI = &CLISessionMeta{Backend: "codex", ID: "thread-7"}
			m.LastMessageAt = &lastAt
		}))

		meta, err := ReadSessionMeta(p)
		require.NoError(t, err)
		assert.Equal(t, "Renamed", meta.Title)
		require.NotNil(t, meta.Conversation)
		assert.Equal(t, "resp_42", meta.Conversation.ID)
		assert.True(t, meta.Conversation.LastUsedAt.Equal(usedAt))
		assert.Equal(t, &CLISessionMeta{Backend: "codex", ID: "thread-7"}, meta.CLI)
		require.NotNil(t, meta.LastMessageAt)
		assert.True(t, meta.LastMessageAt.Equal(lastAt))
	})

	t.Run("preserves_untouched_fields", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "s.meta.json")
		require.NoError(t, UpdateSessionMeta(p, func(m *SessionMeta) {
			m.Conversation = &ConversationMeta{ID: "resp_1"}
		}))
		require.NoError(t, UpdateSessionMeta(p, func(m *SessionMeta) {
			require.NotNil(t, m.Conversation, "update sees the stored sidecar")
			m.Title = "Second"
		}))

		meta, err := ReadSessionMeta(p)
		require.NoError(t, err)
		assert.Equal(t, "Second", meta.Title)
		require.NotNil(t, meta.Conversation)
		assert.Equal(t, "resp_1", meta.Conversation.ID)
	})

	t.Run("malformed_sidecar_is_replaced", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "s.meta.json")
		require.NoError(t, os.WriteFile(p, []byte(`not json`), 0o600))

		require.NoError(t, UpdateSessionMeta(p, func(m *SessionMeta) {
			assert.Equal(t, SessionMeta{}, *m)
			m.Title = "Fresh"
		}))

		meta, err := ReadSessionMeta(p)
		require.NoError(t, err)
		assert.Equal(t, SessionMeta{Title: "Fresh"}, meta)
	})

	t.Run("write_error", func(t *testing.T) {
		dir := t.TempDir()
		blocker := filepath.Join(dir, "file")
		require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))

		err := UpdateSessionMeta(filepath.Join(blocker, "s.meta.json"), func(m *SessionMeta) {
			m.Title = "unreachable"
		})
		assert.Error(t, err)
	})
}

// TestUpdateSessionMetaConcurrent checks that concurrent updates of different
// fields are serialized, so neither write is lost.
func TestUpdateSessionMetaConcurrent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.meta.json")
	const rounds = 50

	for i := 0; i < rounds; i++ {
		require.NoError(t, os.RemoveAll(p))
		start := make(chan struct{})
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			errs <- UpdateSessionMeta(p, func(m *SessionMeta) { m.Title = "title" })
		}()
		go func() {
			defer wg.Done()
			<-start
			errs <- UpdateSessionMeta(p, func(m *SessionMeta) {
				m.Conversation = &ConversationMeta{ID: "resp"}
			})
		}()
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}

		meta, err := ReadSessionMeta(p)
		require.NoError(t, err)
		require.Equal(t, "title", meta.Title, "round %d lost the title", i)
		require.NotNil(t, meta.Conversation, "round %d lost the conversation", i)
		require.Equal(t, "resp", meta.Conversation.ID)
	}
}
