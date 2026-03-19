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

const defaultCompactKeep = 200 // pool entries allowed before compaction runs

// Compact summarizes an entire pool via LLM and replaces it with a single
// summary entry once the pool exceeds the configured threshold. If provider is
// nil, the full pool is replaced with a compact local digest.
func (m *Manager) Compact(ctx context.Context, poolID string, provider llm.Provider, threshold int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if threshold <= 0 {
		threshold = defaultCompactKeep
	}

	all, err := store.ReadJSONL[domain.MemoryEntry](store.MemoryPath(poolID))
	if err != nil {
		return fmt.Errorf("reading pool %s: %w", poolID, err)
	}
	if len(all) <= threshold {
		return nil // nothing to compact
	}

	summaryText, err := summarize(ctx, provider, all)
	if err != nil {
		summaryText = fallbackSummary(all)
	}

	summary := domain.MemoryEntry{
		ID:        newID("summary"),
		Role:      "summary",
		Content:   summaryText,
		Tokens:    estimateTokens(summaryText),
		Timestamp: time.Now(),
	}

	return store.RewriteJSONL(store.MemoryPath(poolID), []domain.MemoryEntry{summary})
}

func summarize(ctx context.Context, provider llm.Provider, entries []domain.MemoryEntry) (string, error) {
	if provider == nil {
		return fallbackSummary(entries), nil
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

func fallbackSummary(entries []domain.MemoryEntry) string {
	if len(entries) == 0 {
		return "[0 messages compacted]"
	}

	lines := []string{fmt.Sprintf("Compacted %d messages.", len(entries))}
	for _, e := range selectDigestEntries(entries, 6) {
		content := strings.Join(strings.Fields(strings.TrimSpace(e.Content)), " ")
		if content == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", e.Role, truncateRunes(content, 140)))
	}
	if len(lines) == 1 {
		return fmt.Sprintf("[%d messages compacted]", len(entries))
	}
	return strings.Join(lines, "\n")
}

func selectDigestEntries(entries []domain.MemoryEntry, limit int) []domain.MemoryEntry {
	if limit <= 0 || len(entries) <= limit {
		return entries
	}

	headCount := limit / 2
	tailCount := limit - headCount
	selected := append([]domain.MemoryEntry{}, entries[:headCount]...)
	selected = append(selected, entries[len(entries)-tailCount:]...)
	return selected
}

func truncateRunes(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}
