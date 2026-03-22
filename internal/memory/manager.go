// Package memory manages per-agent memory notes stored as markdown files.
package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/lsegal/aviary/internal/store"
)

// Manager provides read and write access to human-editable memory notes.
type Manager struct {
	mu sync.Mutex
}

// New creates a Manager.
func New() *Manager { return &Manager{} }

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
