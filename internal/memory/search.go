package memory

import (
	"strings"

	"github.com/lsegal/aviary/internal/domain"
)

// Search performs a case-insensitive keyword search over a pool's entries.
// All terms must appear in the content for an entry to match.
func (m *Manager) Search(poolID, query string) ([]domain.MemoryEntry, error) {
	all, err := m.All(poolID)
	if err != nil {
		return nil, err
	}

	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return all, nil
	}

	var matches []domain.MemoryEntry
	for _, e := range all {
		lower := strings.ToLower(e.Content)
		match := true
		for _, t := range terms {
			if !strings.Contains(lower, t) {
				match = false
				break
			}
		}
		if match {
			matches = append(matches, e)
		}
	}
	return matches, nil
}
