// Package memory manages per-agent conversation memory stored as JSONL files.
package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

// Manager provides append, load-context, and compaction for memory pools.
type Manager struct {
	mu sync.Mutex
}

// New creates a Manager.
func New() *Manager { return &Manager{} }

// Append adds an entry to the pool identified by poolID.
func (m *Manager) Append(poolID, sessionID, role, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry := domain.MemoryEntry{
		ID:        newID("mem"),
		SessionID: sessionID,
		Role:      role,
		Content:   content,
		Tokens:    estimateTokens(content),
		Timestamp: time.Now(),
	}
	return store.AppendJSONL(store.MemoryPath(poolID), entry)
}

// LoadContext reads pool entries newest-first until maxTokens is consumed.
// The returned slice is in chronological order (oldest first).
func (m *Manager) LoadContext(poolID string, maxTokens int) ([]domain.MemoryEntry, error) {
	all, err := store.ReadJSONL[domain.MemoryEntry](store.MemoryPath(poolID))
	if err != nil {
		return nil, fmt.Errorf("loading memory pool %s: %w", poolID, err)
	}
	if len(all) == 0 {
		return nil, nil
	}

	// Walk newest-first, collect until token budget exhausted.
	var window []domain.MemoryEntry
	used := 0
	for i := len(all) - 1; i >= 0; i-- {
		e := all[i]
		if maxTokens > 0 && used+e.Tokens > maxTokens {
			break
		}
		window = append(window, e)
		used += e.Tokens
	}

	// Reverse to chronological order.
	for i, j := 0, len(window)-1; i < j; i, j = i+1, j-1 {
		window[i], window[j] = window[j], window[i]
	}
	return window, nil
}

// GetPool returns metadata for a pool (always succeeds; pool is virtual).
func (m *Manager) GetPool(id string) *domain.MemoryPool {
	return &domain.MemoryPool{ID: id, Name: id}
}

// Clear removes all entries in a pool.
func (m *Manager) Clear(poolID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return store.RewriteJSONL[domain.MemoryEntry](store.MemoryPath(poolID), nil)
}

// GetNotes reads the human-editable markdown notes file for a pool.
// Returns an empty string (no error) when the file does not yet exist.
func (m *Manager) GetNotes(poolID string) (string, error) {
	data, err := os.ReadFile(store.NotesPath(poolID))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading notes for pool %s: %w", poolID, err)
	}
	return store.StripMarkdownCommentLines(string(data)), nil
}

// SetNotes replaces the entire notes file content for a pool.
func (m *Manager) SetNotes(poolID string, content string) error {
	path := store.NotesPath(poolID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating notes dir: %w", err)
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

// AppendNote adds a new bullet line to the notes file for a pool.
func (m *Manager) AppendNote(poolID, note string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing, err := m.GetNotes(poolID)
	if err != nil {
		return err
	}
	path := store.NotesPath(poolID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating notes dir: %w", err)
	}
	var sb strings.Builder
	trimmed := strings.TrimRight(existing, "\n")
	if trimmed != "" {
		sb.WriteString(trimmed)
		sb.WriteString("\n")
	}
	sb.WriteString("- ")
	sb.WriteString(strings.TrimSpace(note))
	sb.WriteString("\n")
	return os.WriteFile(path, []byte(sb.String()), 0o600)
}

// All returns all entries in a pool.
func (m *Manager) All(poolID string) ([]domain.MemoryEntry, error) {
	return store.ReadJSONL[domain.MemoryEntry](store.MemoryPath(poolID))
}

// estimateTokens gives a rough token count (~0.75 tokens per character, or ~1.3 per word).
func estimateTokens(text string) int {
	words := len(strings.Fields(text))
	tokens := int(float64(words) * 1.3)
	if tokens < 1 && len(text) > 0 {
		return 1
	}
	return tokens
}

func newID(prefix string) string {
	ts := time.Now().UTC().Format("20060102_150405.000000000")
	return prefix + "_" + strings.ReplaceAll(ts, ".", "_")
}
