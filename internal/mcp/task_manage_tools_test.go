package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
)

func TestTaskSchedule_DailyTriggerWithTimezoneAndPolicies(t *testing.T) {
	d, _ := setupDispatcherWithScheduler(t)

	out, err := d.CallTool(context.Background(), "task_schedule", map[string]any{
		"agent":      "bot",
		"name":       "standup",
		"content":    "summarize yesterday",
		"trigger":    "weekly",
		"days":       "mon,wed,fri",
		"at":         "09:15",
		"timezone":   "America/Toronto",
		"overlap":    "skip",
		"missed":     "run_once",
		"precompile": false,
	})
	require.NoError(t, err)
	assert.Contains(t, out, "weekly on Mon, Wed, Fri at 09:15 (America/Toronto)")

	cfg, err := config.Load("")
	require.NoError(t, err)
	require.Len(t, cfg.Agents[0].Tasks, 1)
	task := cfg.Agents[0].Tasks[0]
	assert.Equal(t, "weekly", task.Trigger)
	assert.Equal(t, "mon,wed,fri", task.Days)
	assert.Equal(t, "09:15", task.At)
	assert.Equal(t, "America/Toronto", task.Timezone)
	assert.Equal(t, "skip", task.Overlap)
	assert.Equal(t, "run_once", task.Missed)

	assert.Contains(t, toolErrorText(d.CallTool(context.Background(), "task_schedule", map[string]any{
		"agent": "bot", "content": "x", "trigger": "weekly", "at": "09:00",
	})), "weekly trigger requires days")
}

func TestTaskPreview_UnsavedAndSaved(t *testing.T) {
	d, _ := setupDispatcherWithScheduler(t)

	out, err := d.CallTool(context.Background(), "task_preview", map[string]any{
		"every":    "2h",
		"start_at": "2026-01-01T00:00:00Z",
		"timezone": "UTC",
	})
	require.NoError(t, err)
	var res taskPreviewResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	assert.Equal(t, "every 2h0m0s (UTC)", res.Description)
	require.Len(t, res.Next, 3)
	first, err := time.Parse(time.RFC3339, res.Next[0])
	require.NoError(t, err)
	second, err := time.Parse(time.RFC3339, res.Next[1])
	require.NoError(t, err)
	assert.Equal(t, 2*time.Hour, second.Sub(first))
	assert.True(t, first.After(time.Now()))

	_, err = d.CallTool(context.Background(), "task_schedule", map[string]any{
		"agent": "bot", "name": "nightly", "content": "x", "at": "23:00", "precompile": false,
	})
	require.NoError(t, err)
	out, err = d.CallTool(context.Background(), "task_preview", map[string]any{"name": "bot/nightly", "count": 5})
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	assert.Len(t, res.Next, 5)

	assert.Contains(t, toolErrorText(d.CallTool(context.Background(), "task_preview", map[string]any{})), "no schedule trigger")
}

func TestTaskUpdateAndDelete(t *testing.T) {
	d, s := setupDispatcherWithScheduler(t)

	_, err := d.CallTool(context.Background(), "task_schedule", map[string]any{
		"agent": "bot", "name": "digest", "content": "send digest", "schedule": "0 9 * * *", "precompile": false,
	})
	require.NoError(t, err)

	_, err = d.CallTool(context.Background(), "task_update", map[string]any{
		"name":     "digest",
		"schedule": "",
		"every":    "45m",
		"content":  "send a shorter digest",
		"overlap":  "skip",
	})
	require.NoError(t, err)
	cfg, err := config.Load("")
	require.NoError(t, err)
	task := cfg.Agents[0].Tasks[0]
	assert.Empty(t, task.Schedule)
	assert.Equal(t, "45m", task.Every)
	assert.Equal(t, "send a shorter digest", task.Prompt)
	assert.Equal(t, "skip", task.Overlap)
	tasks := s.ListTasks()
	require.Len(t, tasks, 1)
	assert.Equal(t, "every 45m0s (Local)", tasks[0].Description)
	assert.NotNil(t, tasks[0].NextRun)

	assert.Contains(t, toolErrorText(d.CallTool(context.Background(), "task_update", map[string]any{"name": "digest", "every": "soon"})), "invalid every")

	_, err = d.CallTool(context.Background(), "task_update", map[string]any{"name": "digest", "enabled": false})
	require.NoError(t, err)
	assert.Empty(t, s.ListTasks())

	_, err = d.CallTool(context.Background(), "task_delete", map[string]any{"name": "bot/digest"})
	require.NoError(t, err)
	files, _ := filepath.Glob(filepath.Join(config.AgentTasksDir(cfg.Agents[0]), "*.md"))
	assert.Empty(t, files)
	cfg, err = config.Load("")
	require.NoError(t, err)
	assert.Empty(t, cfg.Agents[0].Tasks)

	assert.Contains(t, toolErrorText(d.CallTool(context.Background(), "task_delete", map[string]any{"name": "digest"})), "not found")
}

func TestTaskUpdate_RenamedFileIsReplaced(t *testing.T) {
	d, _ := setupDispatcherWithScheduler(t)
	cfg, err := config.Load("")
	require.NoError(t, err)
	dir := config.AgentTasksDir(cfg.Agents[0])
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "custom-file.md"), []byte("---\nname: My Task\nevery: 1h\n---\nhello\n"), 0o640))

	_, err = d.CallTool(context.Background(), "task_update", map[string]any{"name": "My Task", "every": "2h"})
	require.NoError(t, err)
	files, _ := filepath.Glob(filepath.Join(dir, "*.md"))
	require.Len(t, files, 1)
	cfg, err = config.Load("")
	require.NoError(t, err)
	assert.Equal(t, "2h", cfg.Agents[0].Tasks[0].Every)
}

// toolErrorText returns a failed tool call's message, whether the failure
// came back as a Go error or as the tool's error text.
func toolErrorText(out string, err error) string {
	if err != nil {
		return err.Error()
	}
	return out
}
