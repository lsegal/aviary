package memory

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/store"
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
	if estimateTokens("") != 0 {
		t.Fatal("empty text should estimate 0")
	}
	if estimateTokens("hello") < 1 {
		t.Fatal("non-empty text should estimate at least 1")
	}
	a := newID("mem")
	if !strings.HasPrefix(a, "mem_") {
		t.Fatalf("newID should include prefix, got %s", a)
	}
}

func setupDataDir(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := store.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}
}

func TestManager_AppendAllLoadClearPool(t *testing.T) {
	setupDataDir(t)
	m := New()

	if err := m.Append("pool1", "s1", "user", "hello world"); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	if err := m.Append("pool1", "s1", "assistant", "hello there"); err != nil {
		t.Fatalf("append 2: %v", err)
	}

	all, err := m.All("pool1")
	if err != nil || len(all) != 2 {
		t.Fatalf("all got len=%d err=%v", len(all), err)
	}

	ctxAll, err := m.LoadContext("pool1", 0)
	if err != nil || len(ctxAll) != 2 {
		t.Fatalf("loadcontext all len=%d err=%v", len(ctxAll), err)
	}

	limited, err := m.LoadContext("pool1", 1)
	if err != nil {
		t.Fatalf("loadcontext limited err=%v", err)
	}
	if len(limited) > 1 {
		t.Fatalf("expected limited context <=1 entry, got %d", len(limited))
	}

	pool := m.GetPool("pool1")
	if pool.ID != "pool1" || pool.Name != "pool1" {
		t.Fatalf("unexpected pool: %+v", pool)
	}

	if err := m.Clear("pool1"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	all, err = m.All("pool1")
	if err != nil || len(all) != 0 {
		t.Fatalf("expected empty after clear, len=%d err=%v", len(all), err)
	}
}

func TestManager_Search(t *testing.T) {
	setupDataDir(t)
	m := New()
	_ = m.Append("pool", "s", "user", "The quick brown fox")
	_ = m.Append("pool", "s", "assistant", "Jumps over lazy dog")
	_ = m.Append("pool", "s", "user", "another message")

	matches, err := m.Search("pool", "quick fox")
	if err != nil || len(matches) != 1 {
		t.Fatalf("search quick fox len=%d err=%v", len(matches), err)
	}

	matches, err = m.Search("pool", "dog")
	if err != nil || len(matches) != 1 {
		t.Fatalf("search dog len=%d err=%v", len(matches), err)
	}

	matches, err = m.Search("pool", "")
	if err != nil || len(matches) != 3 {
		t.Fatalf("empty query should return all, len=%d err=%v", len(matches), err)
	}
}

func TestCompact_NilProviderAndNoop(t *testing.T) {
	setupDataDir(t)
	m := New()

	for i := 0; i < 25; i++ {
		_ = m.Append("cp", "s", "user", "some content")
	}

	if err := m.Compact(context.Background(), "cp", nil, 20); err != nil {
		t.Fatalf("compact nil provider: %v", err)
	}
	all, _ := m.All("cp")
	if len(all) != 21 {
		t.Fatalf("expected 21 entries after compaction, got %d", len(all))
	}
	if all[0].Role != "summary" {
		t.Fatalf("first entry should be summary, got %s", all[0].Role)
	}

	if err := m.Compact(context.Background(), "cp", nil, 50); err != nil {
		t.Fatalf("compact noop path: %v", err)
	}
}

func TestCompact_WithProviderAndFallback(t *testing.T) {
	setupDataDir(t)
	m := New()
	for i := 0; i < 25; i++ {
		_ = m.Append("cp2", "s", "user", "line")
	}

	provider := &mockProvider{events: []llm.Event{{Type: llm.EventTypeText, Text: "summary text"}, {Type: llm.EventTypeDone}}}
	if err := m.Compact(context.Background(), "cp2", provider, 20); err != nil {
		t.Fatalf("compact with provider: %v", err)
	}
	all, _ := m.All("cp2")
	if all[0].Content != "summary text" {
		t.Fatalf("unexpected summary content: %s", all[0].Content)
	}

	for i := 0; i < 25; i++ {
		_ = m.Append("cp3", "s", "user", "line")
	}
	errProvider := &mockProvider{events: []llm.Event{{Type: llm.EventTypeError, Error: errors.New("stream error")}}}
	if err := m.Compact(context.Background(), "cp3", errProvider, 20); err != nil {
		t.Fatalf("compact should fallback on provider error: %v", err)
	}
	all, _ = m.All("cp3")
	if all[0].Role != "summary" {
		t.Fatalf("fallback summary missing")
	}
}

func TestSummarize(t *testing.T) {
	entries := []domain.MemoryEntry{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}

	t.Run("nil provider", func(t *testing.T) {
		text, err := summarize(context.Background(), nil, entries)
		if err != nil {
			t.Fatalf("summarize nil provider: %v", err)
		}
		if text == "" {
			t.Fatal("summary text should not be empty")
		}
	})

	t.Run("provider stream error", func(t *testing.T) {
		provider := &mockProvider{err: errors.New("stream setup failed")}
		if _, err := summarize(context.Background(), provider, entries); err == nil {
			t.Fatal("expected stream error")
		}
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
	if err != nil {
		t.Fatalf("GetNotes missing: %v", err)
	}
	if notes != "" {
		t.Errorf("expected empty string for missing notes, got %q", notes)
	}
}

func TestSetNotesAndGetNotes(t *testing.T) {
	setupMemoryDir(t)
	m := New()
	if err := m.SetNotes("pool1", "hello notes"); err != nil {
		t.Fatalf("SetNotes: %v", err)
	}
	got, err := m.GetNotes("pool1")
	if err != nil {
		t.Fatalf("GetNotes: %v", err)
	}
	if got != "hello notes" {
		t.Errorf("GetNotes = %q; want %q", got, "hello notes")
	}
}

func TestAppendNote(t *testing.T) {
	setupMemoryDir(t)
	m := New()

	// Append to empty
	if err := m.AppendNote("pool1", "first note"); err != nil {
		t.Fatalf("AppendNote 1: %v", err)
	}
	// Append second
	if err := m.AppendNote("pool1", "second note"); err != nil {
		t.Fatalf("AppendNote 2: %v", err)
	}

	got, err := m.GetNotes("pool1")
	if err != nil {
		t.Fatalf("GetNotes: %v", err)
	}
	if !strings.Contains(got, "- first note") {
		t.Errorf("expected first note in output, got %q", got)
	}
	if !strings.Contains(got, "- second note") {
		t.Errorf("expected second note in output, got %q", got)
	}
}

func TestCompact_NilProvider(t *testing.T) {
	setupMemoryDir(t)
	m := New()
	// Add enough entries to exceed keepRecent.
	for i := 0; i < 5; i++ {
		_ = m.Append("pool_cp", "sess1", "user", "some content")
	}
	// Compact with nil provider falls back to dropping old entries without LLM; should not error.
	err := m.Compact(context.Background(), "pool_cp", nil, 2)
	if err != nil {
		t.Errorf("Compact with nil provider should succeed, got: %v", err)
	}
	// After compaction, pool should have <= keepRecent+1 entries (summary + recent).
	entries, err2 := m.All("pool_cp")
	if err2 != nil {
		t.Fatalf("All after compact: %v", err2)
	}
	if len(entries) > 3 {
		t.Errorf("expected compacted pool size <= 3, got %d", len(entries))
	}
}

func TestSearch_MultiplePools(t *testing.T) {
	setupMemoryDir(t)
	m := New()

	_ = m.Append("poolA", "sess1", "user", "apple fruit")
	_ = m.Append("poolB", "sess1", "user", "banana fruit")

	resultsA, err := m.Search("poolA", "apple")
	if err != nil {
		t.Fatalf("Search poolA: %v", err)
	}
	if len(resultsA) == 0 {
		t.Error("expected search results for poolA")
	}

	resultsB, err := m.Search("poolB", "banana")
	if err != nil {
		t.Fatalf("Search poolB: %v", err)
	}
	if len(resultsB) == 0 {
		t.Error("expected search results for poolB")
	}
}
