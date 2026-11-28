package server

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lsegal/aviary/internal/logging"
)

// A process keeping its logs in memory, as `aviary watch` does, serves its
// log history and live appends from there.
func TestLogsFromMemory(t *testing.T) {
	logging.Shutdown()
	t.Cleanup(logging.Shutdown)
	assert.NoError(t, logging.InitMemory())
	slog.Info("memory-line-1")
	slog.Info("memory-line-2")

	entries, hasMore, err := readHistoricalLogEntries(-1, 1)
	assert.NoError(t, err)
	assert.True(t, hasMore)
	if assert.Len(t, entries, 1) && assert.NotNil(t, entries[0].Offset) {
		assert.Equal(t, "memory-line-2", entries[0].Message)
		// Paging from the entry's offset reaches the line before it.
		slog.Info("memory-line-2b")
		older, _, err := readHistoricalLogEntries(*entries[0].Offset, 1)
		assert.NoError(t, err)
		if assert.Len(t, older, 1) {
			assert.Equal(t, "memory-line-1", older[0].Message)
		}
	}

	_, offset, _ := readLogAppends(-1)
	slog.Info("memory-line-3")
	chunk, next, reset := readLogAppends(offset)
	assert.False(t, reset)
	assert.Equal(t, 1, strings.Count(string(chunk), "\n"))
	assert.Contains(t, string(chunk), "memory-line-3")

	chunk, _, _ = readLogAppends(next)
	assert.Empty(t, chunk)

	// An offset past the end means the log started over.
	_, _, reset = readLogAppends(next + 1000)
	assert.True(t, reset)
}
