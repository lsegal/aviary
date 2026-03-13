package memory

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/store"

	"github.com/stretchr/testify/assert"
)

type mockProvider struct {
	events []llm.Event
	err    error
}

func (m *mockProvider) Stream(_ context.Context, _ llm.Request) (<-chan llm.Event, error) {
	if m.err != nil {
		return nil, m.err
	}
	ch := make(chan llm.Event, len(m.events)+1)
	for _, e := range m.events {
		ch <- e
	}
	if len(m.events) == 0 || m.events[len(m.events)-1].Type != llm.EventTypeDone {
		ch <- llm.Event{Type: llm.EventTypeDone}
	}
	close(ch)
	return ch, nil
}

func TestEstimateTokensAndNewID(t *testing.T) {
	assert.Equal(t, 0, estimateTokens(""))
	assert.GreaterOrEqual(t, estimateTokens("hello"), 1)

	a := newID("mem")
	assert.True(t, strings.HasPrefix(a, "mem_"))

}

func setupDataDir(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	err := store.EnsureDirs()
	assert.NoError(t, err)

}

func TestManager_AppendAllLoadClearPool(t *testing.T) {
	setupDataDir(t)
	m := New()
	err := m.Append("pool1", "s1", "user", "hello world")
	assert.NoError(t, err)

	err = m.Append("pool1", "s1", "assistant", "hello there")
	assert.NoError(t, err)

	all, err := m.All("pool1")
	assert.NoError(t, err)
	assert.Len(t, all, 2)

	ctxAll, err := m.LoadContext("pool1", 0)
	assert.NoError(t, err)
	assert.Len(t, ctxAll, 2)

	limited, err := m.LoadContext("pool1", 1)
	assert.NoError(t, err)
	assert.LessOrEqual(t, len(limited), 1)

	pool := m.GetPool("pool1")
	assert.Equal(t, "pool1", pool.ID)
	assert.Equal(t, "pool1", pool.Name)
	err = m.Clear("pool1")
	assert.NoError(t, err)

	all, err = m.All("pool1")
	assert.NoError(t, err)
	assert.Len(t, all, 0)

}

func TestManager_Search(t *testing.T) {
	setupDataDir(t)
	m := New()
	_ = m.Append("pool", "s", "user", "The quick brown fox")
	_ = m.Append("pool", "s", "assistant", "Jumps over lazy dog")
	_ = m.Append("pool", "s", "user", "another message")

	matches, err := m.Search("pool", "quick fox")
	assert.NoError(t, err)
	assert.Len(t, matches, 1)

	matches, err = m.Search("pool", "dog")
	assert.NoError(t, err)
	assert.Len(t, matches, 1)

	matches, err = m.Search("pool", "")
	assert.NoError(t, err)
	assert.Len(t, matches, 3)

}

func TestCompact_NilProviderAndNoop(t *testing.T) {
	setupDataDir(t)
	m := New()

	for i := 0; i < 25; i++ {
		_ = m.Append("cp", "s", "user", "some content")
	}
	err := m.Compact(context.Background(), "cp", nil, 20)
	assert.NoError(t, err)

	all, _ := m.All("cp")
	assert.Equal(t, 1, len(all))
	assert.Equal(t, "summary", all[0].Role)
	assert.Contains(t, all[0].Content, "Compacted 25 messages.")
	assert.Contains(t, all[0].Content, "some content")
	err = m.Compact(context.Background(), "cp", nil, 50)
	assert.NoError(t, err)

}

func TestCompact_WithProviderAndFallback(t *testing.T) {
	setupDataDir(t)
	m := New()
	for i := 0; i < 25; i++ {
		_ = m.Append("cp2", "s", "user", "line")
	}

	provider := &mockProvider{events: []llm.Event{{Type: llm.EventTypeText, Text: "summary text"}, {Type: llm.EventTypeDone}}}
	err := m.Compact(context.Background(), "cp2", provider, 20)
	assert.NoError(t, err)

	all, _ := m.All("cp2")
	assert.Equal(t, 1, len(all))
	assert.Equal(t, "summary text", all[0].Content)

	for i := 0; i < 25; i++ {
		_ = m.Append("cp3", "s", "user", "line")
	}
	errProvider := &mockProvider{events: []llm.Event{{Type: llm.EventTypeError, Error: errors.New("stream error")}}}
	err = m.Compact(context.Background(), "cp3", errProvider, 20)
	assert.NoError(t, err)

	all, _ = m.All("cp3")
	assert.Equal(t, 1, len(all))
	assert.Equal(t, "summary", all[0].Role)
	assert.Contains(t, all[0].Content, "Compacted 25 messages.")
	assert.Contains(t, all[0].Content, "line")

}

func TestSummarize(t *testing.T) {
	entries := []domain.MemoryEntry{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}

	t.Run("nil provider", func(t *testing.T) {
		text, err := summarize(context.Background(), nil, entries)
		assert.NoError(t, err)
		assert.Contains(t, text, "Compacted 2 messages.")
		assert.Contains(t, text, "user: hello")

	})

	t.Run("provider stream error", func(t *testing.T) {
		provider := &mockProvider{err: errors.New("stream setup failed")}
		_, err := summarize(context.Background(), provider, entries)
		assert.Error(t, err)

	})
}

func setupMemoryDir(t *testing.T) {
	t.Helper()
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir("") })
}

func TestGetNotes_Missing(t *testing.T) {
	setupMemoryDir(t)
	m := New()
	notes, err := m.GetNotes("pool1")
	assert.NoError(t, err)
	assert.Equal(t, "", notes)

}

func TestSetNotesAndGetNotes(t *testing.T) {
	setupMemoryDir(t)
	m := New()
	err := m.SetNotes("pool1", "hello notes")
	assert.NoError(t, err)

	got, err := m.GetNotes("pool1")
	assert.NoError(t, err)
	assert.Equal(t, "hello notes", got)

}

func TestAppendNote(t *testing.T) {
	setupMemoryDir(t)
	m := New()

	// Append to empty
	err := m.AppendNote("pool1", "first note")
	assert.NoError(t, err)

	// Append second
	err = m.AppendNote("pool1", "second note")
	assert.NoError(t, err)

	got, err := m.GetNotes("pool1")
	assert.NoError(t, err)
	assert.True(t, strings.Contains(got, "- first note"))
	assert.True(t, strings.Contains(got, "- second note"))

}

func TestCompact_NilProvider(t *testing.T) {
	setupMemoryDir(t)
	m := New()
	// Add enough entries to exceed the compaction threshold.
	for i := 0; i < 5; i++ {
		_ = m.Append("pool_cp", "sess1", "user", "some content")
	}
	// Compact with nil provider should rewrite the full pool to a single summary.
	err := m.Compact(context.Background(), "pool_cp", nil, 2)
	assert.NoError(t, err)

	// After compaction, the pool should contain only the summary entry.
	entries, err2 := m.All("pool_cp")
	assert.Nil(t, err2)
	assert.Len(t, entries, 1)
	assert.Equal(t, "summary", entries[0].Role)
	assert.Contains(t, entries[0].Content, "Compacted 5 messages.")

}

func TestSearch_MultiplePools(t *testing.T) {
	setupMemoryDir(t)
	m := New()

	_ = m.Append("poolA", "sess1", "user", "apple fruit")
	_ = m.Append("poolB", "sess1", "user", "banana fruit")

	resultsA, err := m.Search("poolA", "apple")
	assert.NoError(t, err)
	assert.NotEqual(t, 0, len(resultsA))

	resultsB, err := m.Search("poolB", "banana")
	assert.NoError(t, err)
	assert.NotEqual(t, 0, len(resultsB))

}
