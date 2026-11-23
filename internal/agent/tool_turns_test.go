package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

// writeSession writes records as agentID's session file name.
func writeSession(t *testing.T, agentID, name string, records ...any) string {
	t.Helper()
	path := filepath.Join(store.AgentDir(agentID), "sessions", name+".jsonl")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	var data []byte
	for _, rec := range records {
		line, err := json.Marshal(rec)
		require.NoError(t, err)
		data = append(append(data, line...), '\n')
	}
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

// toolMessage is a tool record as the runner saves it.
func toolMessage(t *testing.T, rec toolEventRecord, saved time.Time) domain.Message {
	t.Helper()
	content, err := json.Marshal(rec)
	require.NoError(t, err)
	return domain.Message{ID: "msg_tool", Role: domain.MessageRoleTool, Content: string(content), Timestamp: saved}
}

// A call belongs to the user message sent before it started, even when its
// record was saved after a later message, and calls of other tools or without
// a result are left out.
func TestFindToolCallTurns(t *testing.T) {
	setTestDataDir(t)
	base := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	at := func(seconds int) time.Time { return base.Add(time.Duration(seconds) * time.Second) }
	shot := domain.Message{ID: "msg_shot", Role: domain.MessageRoleUser, Content: "bug: see this", MediaURLs: []string{"data:image/png;base64,eA=="}, Timestamp: at(0)}
	followUp := domain.Message{ID: "msg_follow", Role: domain.MessageRoleUser, Content: "also this", Timestamp: at(20)}
	path := writeSession(t, "Aviary", "signal_1",
		map[string]any{"id": "signal_1", "created_at": at(-10)},
		shot,
		followUp,
		toolMessage(t, toolEventRecord{ID: "call_" + strconv.FormatInt(at(10).UnixNano(), 10) + "_3", Name: "project_issue_create", Result: `{"url":"https://github.com/o/r/issues/7"}`}, at(30)),
		toolMessage(t, toolEventRecord{ID: "toolu_01", Name: "project_issue_create", Result: `{"url":"https://github.com/o/r/issues/8"}`}, at(40)),
		toolMessage(t, toolEventRecord{ID: "call_9_1", Name: "project_issue_create", Error: "boom"}, at(41)),
		toolMessage(t, toolEventRecord{Name: "project_status", Result: "{}"}, at(42)),
	)
	writeSession(t, "projects", "projects-chat",
		domain.Message{ID: "msg_early", Role: domain.MessageRoleUser, Content: "file it", Timestamp: at(-5)},
		toolMessage(t, toolEventRecord{Name: "project_issue_create", Result: `{"url":"https://github.com/o/r/issues/6"}`}, at(-4)),
	)

	turns, err := FindToolCallTurns("project_issue_create")
	require.NoError(t, err)
	require.Len(t, turns, 3)

	assert.Equal(t, "projects", turns[0].AgentID)
	assert.Equal(t, "msg_early", turns[0].Prompt.ID)

	assert.Equal(t, "Aviary", turns[1].AgentID)
	assert.Equal(t, path, turns[1].Session)
	assert.True(t, turns[1].CalledAt.Equal(at(10)), "called at %s", turns[1].CalledAt)
	assert.Equal(t, `{"url":"https://github.com/o/r/issues/7"}`, turns[1].Result)
	require.NotNil(t, turns[1].Prompt)
	assert.Equal(t, "msg_shot", turns[1].Prompt.ID)
	assert.Equal(t, shot.MediaURLs, turns[1].Prompt.MediaURLs)

	// A call the backend named carries no start time, so it counts from when
	// its record was saved.
	assert.True(t, turns[2].CalledAt.Equal(at(40)))
	assert.Equal(t, "msg_follow", turns[2].Prompt.ID)
}

func TestFindToolCallTurnsWithoutHistory(t *testing.T) {
	setTestDataDir(t)
	turns, err := FindToolCallTurns("project_issue_create")
	require.NoError(t, err)
	assert.Empty(t, turns)

	// A session that cannot be read is skipped, not fatal.
	require.NoError(t, os.MkdirAll(filepath.Join(store.AgentDir("Aviary"), "sessions", "unreadable.jsonl"), 0o700))
	writeSession(t, "Aviary", "tools_only", toolMessage(t, toolEventRecord{Name: "project_issue_create", Result: "{}"}, time.Now()))
	turns, err = FindToolCallTurns("project_issue_create")
	require.NoError(t, err)
	require.Len(t, turns, 1)
	assert.Nil(t, turns[0].Prompt)
}
