package memory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/store"
)

const defaultCompactKeep = 200 // entries to keep after compaction

// Compact summarizes the oldest entries in a pool via LLM and replaces them
// with a single summary entry. keepRecent entries at the end are preserved.
// If provider is nil, the oldest entries are simply discarded.
func (m *Manager) Compact(ctx context.Context, poolID string, provider llm.Provider, keepRecent int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if keepRecent <= 0 {
		keepRecent = defaultCompactKeep
	}

	all, err := store.ReadJSONL[domain.MemoryEntry](store.MemoryPath(poolID))
	if err != nil {
		return fmt.Errorf("reading pool %s: %w", poolID, err)
	}
	if len(all) <= keepRecent {
		return nil // nothing to compact
	}

	toCompact := all[:len(all)-keepRecent]
	recent := all[len(all)-keepRecent:]

	summaryText, err := summarize(ctx, provider, toCompact)
	if err != nil {
		// Fallback: drop old entries without summarising.
		summaryText = fmt.Sprintf("[%d messages compacted]", len(toCompact))
	}

	summary := domain.MemoryEntry{
		ID:        newID("summary"),
		PoolID:    poolID,
		Role:      "summary",
		Content:   summaryText,
		Tokens:    estimateTokens(summaryText),
		Timestamp: time.Now(),
	}

	newEntries := append([]domain.MemoryEntry{summary}, recent...)
	return store.RewriteJSONL(store.MemoryPath(poolID), newEntries)
}

func summarize(ctx context.Context, provider llm.Provider, entries []domain.MemoryEntry) (string, error) {
	if provider == nil {
		return fmt.Sprintf("[%d messages compacted]", len(entries)), nil
	}

	var sb strings.Builder
	for _, e := range entries {
		sb.WriteString(e.Role)
		sb.WriteString(": ")
		sb.WriteString(e.Content)
		sb.WriteString("\n")
	}

	req := llm.Request{
		Messages: []llm.Message{
			{
				Role: llm.RoleUser,
				Content: "Summarize the following conversation concisely, preserving key facts:\n\n" +
					sb.String(),
			},
		},
		Stream: true,
	}

	ch, err := provider.Stream(ctx, req)
	if err != nil {
		return "", fmt.Errorf("streaming summary: %w", err)
	}

	var result strings.Builder
	for event := range ch {
		switch event.Type {
		case llm.EventTypeText:
			result.WriteString(event.Text)
		case llm.EventTypeError:
			return "", event.Error
		case llm.EventTypeDone:
			return result.String(), nil
		}
	}
	return result.String(), nil
}
