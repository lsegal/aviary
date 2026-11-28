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

// upcomingConfig has two prompt tasks on different intervals, a project
// task, and a paused task that task_upcoming must leave out.
func upcomingConfig() *config.Config {
	disabled := false
	return &config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "test/x", Tasks: []config.TaskConfig{
		{Name: "hourly", Every: "1h", Prompt: "x"},
		{Name: "bihourly", Every: "2h", Prompt: "x"},
		{Name: "triage", Type: "project", Project: "o/r", Model: "test/x", Every: "30m", Prompt: "x"},
		{Name: "paused", Every: "10m", Prompt: "x", Enabled: &disabled},
	}}}}
}

func callUpcoming(t *testing.T, d *Dispatcher, args map[string]any) []taskUpcomingRun {
	t.Helper()
	out, err := d.CallTool(context.Background(), "task_upcoming", args)
	require.NoError(t, err)
	var runs []taskUpcomingRun
	require.NoError(t, json.Unmarshal([]byte(out), &runs), out)
	return runs
}

// assertUpcomingOrder checks runs are soonest first, in the future, and at
// most perTask per task.
func assertUpcomingOrder(t *testing.T, runs []taskUpcomingRun, perTask int) {
	t.Helper()
	counts := map[string]int{}
	var prev time.Time
	for _, run := range runs {
		at, err := time.Parse(time.RFC3339, run.At)
		require.NoError(t, err)
		assert.False(t, at.Before(prev), "runs are soonest first: %v", runs)
		assert.True(t, at.After(time.Now().Add(-time.Minute)))
		prev = at
		counts[run.TaskID]++
		assert.NotEqual(t, "bot/paused", run.TaskID)
	}
	for id, n := range counts {
		assert.LessOrEqual(t, n, perTask, "task %s", id)
	}
}

func TestTaskUpcoming_LimitsFiltersAndOrder(t *testing.T) {
	setupSchedulerDeps(t, upcomingConfig())
	d := NewDispatcher("https://localhost:16677", "")

	// Defaults: 5 runs, one per task, so every enabled task appears once at
	// its next fire, soonest first.
	runs := callUpcoming(t, d, map[string]any{})
	require.Len(t, runs, 3)
	assertUpcomingOrder(t, runs, 1)
	ids := []string{runs[0].TaskID, runs[1].TaskID, runs[2].TaskID}
	assert.ElementsMatch(t, []string{"bot/hourly", "bot/bihourly", "bot/triage"}, ids)
	for _, run := range runs {
		switch run.TaskID {
		case "bot/triage":
			assert.Equal(t, "triage", run.Name)
			assert.Equal(t, "o/r", run.Project)
		case "bot/hourly", "bot/bihourly":
			assert.Empty(t, run.Project)
		default:
			t.Errorf("unexpected task %q", run.TaskID)
		}
	}

	// A larger per_task lists more fires of each task, up to limit.
	runs = callUpcoming(t, d, map[string]any{"per_task": 3})
	require.Len(t, runs, 5)
	assertUpcomingOrder(t, runs, 3)

	// An explicit limit truncates.
	runs = callUpcoming(t, d, map[string]any{"limit": 2})
	assert.Len(t, runs, 2)
	runs = callUpcoming(t, d, map[string]any{"limit": 20, "per_task": 4})
	assert.Len(t, runs, 12)
	assertUpcomingOrder(t, runs, 4)

	// project limits it to that project's tasks, capped per task.
	runs = callUpcoming(t, d, map[string]any{"project": " o/r ", "limit": 10})
	require.Len(t, runs, 1)
	assert.Equal(t, "bot/triage", runs[0].TaskID)
	runs = callUpcoming(t, d, map[string]any{"project": "o/r", "limit": 10, "per_task": 3})
	require.Len(t, runs, 3)
	for _, run := range runs {
		assert.Equal(t, "bot/triage", run.TaskID)
	}
	assertUpcomingOrder(t, runs, 3)
	runs = callUpcoming(t, d, map[string]any{"project": "o/none"})
	assert.Empty(t, runs)
}

// A task that fires every 30 seconds or every minute takes one entry by
// default, leaving room for slower tasks, soonest first.
func TestTaskUpcoming_FrequentTasksOncePerTask(t *testing.T) {
	setupSchedulerDeps(t, &config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "test/x", Tasks: []config.TaskConfig{
		{Name: "halfminute", Every: "30s", Prompt: "x"},
		{Name: "minutely", Every: "1m", Prompt: "x"},
		{Name: "hourly", Every: "1h", Prompt: "x"},
	}}}})
	d := NewDispatcher("https://localhost:16677", "")

	runs := callUpcoming(t, d, map[string]any{})
	require.Len(t, runs, 3)
	assertUpcomingOrder(t, runs, 1)
	assert.Equal(t, "bot/hourly", runs[2].TaskID, "the slow task still makes the list")
}

func TestTaskUpcoming_SchedulerNotInitialized(t *testing.T) {
	setupSchedulerDeps(t, botConfig())
	SetDeps(&Deps{})
	d := NewDispatcher("https://localhost:16677", "")
	assert.Contains(t, toolErrorText(d.CallTool(context.Background(), "task_upcoming", map[string]any{})), "scheduler not initialized")
}

func TestPreviewKey(t *testing.T) {
	assert.Equal(t, "bot/nightly", previewKey(" bot ", " nightly "))
	assert.Equal(t, "other/nightly", previewKey("bot", " other/nightly"))
	assert.Equal(t, "nightly", previewKey("", " nightly "))
	assert.Equal(t, "nightly", previewKey("  ", "nightly"))
}

func TestTaskPreview_UnknownAndPausedTasks(t *testing.T) {
	disabled := false
	setupSchedulerDeps(t, &config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "test/x", Tasks: []config.TaskConfig{
		{Name: "paused", Every: "2h", Prompt: "x", Enabled: &disabled},
	}}}})
	d := NewDispatcher("https://localhost:16677", "")

	assert.Contains(t, toolErrorText(d.CallTool(context.Background(), "task_preview", map[string]any{"name": "missing"})), `task "missing" not found`)

	// The scheduler does not arm a paused task, so its preview comes from the
	// task's own schedule.
	out, err := d.CallTool(context.Background(), "task_preview", map[string]any{"name": "paused", "agent": "bot", "count": 2})
	require.NoError(t, err)
	var res taskPreviewResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	require.Len(t, res.Next, 2)
	first, err := time.Parse(time.RFC3339, res.Next[0])
	require.NoError(t, err)
	second, err := time.Parse(time.RFC3339, res.Next[1])
	require.NoError(t, err)
	assert.Equal(t, 2*time.Hour, second.Sub(first))
}

func TestFindConfiguredTask_Errors(t *testing.T) {
	setupSchedulerDeps(t, &config.Config{Agents: []config.AgentConfig{
		{Name: "bot", Model: "test/x", Tasks: []config.TaskConfig{{Name: "dup", Every: "1h", Prompt: "x"}}},
		{Name: "other", Model: "test/x", Tasks: []config.TaskConfig{{Name: "dup", Every: "1h", Prompt: "y"}}},
	}})
	d := NewDispatcher("https://localhost:16677", "")

	assert.Contains(t, toolErrorText(d.CallTool(context.Background(), "task_update", map[string]any{"name": "  ", "every": "2h"})), "name is required")
	msg := toolErrorText(d.CallTool(context.Background(), "task_update", map[string]any{"name": "dup", "every": "2h"}))
	assert.Contains(t, msg, `task name "dup" is ambiguous (bot/dup, other/dup)`)
	msg = toolErrorText(d.CallTool(context.Background(), "task_delete", map[string]any{"name": "dup"}))
	assert.Contains(t, msg, "is ambiguous")
	assert.Contains(t, toolErrorText(d.CallTool(context.Background(), "task_update", map[string]any{"name": "dup", "agent": "nobody", "every": "2h"})), `task "dup" not found`)

	// The agent or an <agent>/<task> name picks one of them.
	_, agentCfg, task, err := findConfiguredTask("other", "dup")
	require.NoError(t, err)
	assert.Equal(t, "other", agentCfg.Name)
	assert.Equal(t, "y", task.Prompt)
	_, agentCfg, _, err = findConfiguredTask("", "bot/dup")
	require.NoError(t, err)
	assert.Equal(t, "bot", agentCfg.Name)

	// A config that does not parse fails every lookup and reload.
	require.NoError(t, os.WriteFile(config.DefaultPath(), []byte("agents: [\n"), 0o600))
	assert.Contains(t, toolErrorText(d.CallTool(context.Background(), "task_delete", map[string]any{"name": "bot/dup"})), "parsing config")
	require.Error(t, reloadTaskConfig())
}

func TestReloadTaskConfig_ReconcilesAvailableDeps(t *testing.T) {
	s := setupSchedulerDeps(t, botConfig())
	require.NoError(t, config.Save("", &config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "test/x", Tasks: []config.TaskConfig{
		{Name: "added", Every: "1h", Prompt: "x"},
	}}}}))

	// Without deps there is nothing to reconcile.
	deps := GetDeps()
	SetDeps(nil)
	require.NoError(t, reloadTaskConfig())
	SetDeps(&Deps{Scheduler: s})
	require.NoError(t, reloadTaskConfig())
	SetDeps(deps)

	tasks := s.ListTasks()
	require.Len(t, tasks, 1)
	assert.Equal(t, "bot/added", tasks[0].ID)
}

// writeTaskFile saves a markdown task file for agent bot.
func writeTaskFile(t *testing.T, file, content string) {
	t.Helper()
	dir := config.AgentTasksDir(config.AgentConfig{Name: "bot"})
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, file), []byte(content), 0o640))
}

func loadBotTask(t *testing.T, name string) config.TaskConfig {
	t.Helper()
	cfg, err := config.Load("")
	require.NoError(t, err)
	for _, task := range cfg.Agents[0].Tasks {
		if task.Name == name {
			return task
		}
	}
	t.Fatalf("task %q not found", name)
	return config.TaskConfig{}
}

func TestTaskUpdate_ValidationErrors(t *testing.T) {
	setupSchedulerDeps(t, &config.Config{Agents: []config.AgentConfig{{
		Name: "bot", Model: "test/x",
		Channels: []config.ChannelConfig{{Type: "slack", ID: "ops", Name: "Ops"}},
	}}})
	writeTaskFile(t, "hourly.md", "---\nname: hourly\nevery: 1h\nprecompile: false\n---\nsay hi\n")
	writeTaskFile(t, "wf.md", "---\nname: wf\ntrigger: message\nchannel: Ops\nprecompile: false\n---\nanswer\n")
	writeTaskFile(t, "triage.md", "---\nname: triage\ntype: project\nproject: o/r\nmodel: test/x\nevery: 1h\nprecompile: false\n---\ntriage\n")
	d := NewDispatcher("https://localhost:16677", "")

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"invalid target", map[string]any{"name": "hourly", "target": "bogus"}, `invalid target "bogus"`},
		{"project on prompt task", map[string]any{"name": "hourly", "project": "o/r"}, "only supported for project tasks and workflows"},
		{"model on prompt task", map[string]any{"name": "hourly", "model": "test/y"}, "only supported for project tasks and workflows"},
		{"reply target on scheduled task", map[string]any{"name": "hourly", "target": "reply"}, "only workflows can reply"},
		{"channel on scheduled task", map[string]any{"name": "hourly", "channel": "Ops"}, "only supported for workflows (trigger=message)"},
		{"match on scheduled task", map[string]any{"name": "hourly", "match": "deploy"}, "only supported for workflows (trigger=message)"},
		{"trigger and watch", map[string]any{"name": "hourly", "watch": "notes.txt"}, "both a schedule trigger and watch"},
		{"no trigger or watch", map[string]any{"name": "hourly", "every": ""}, "needs a schedule trigger or watch"},
		{"workflow on unknown channel", map[string]any{"name": "wf", "channel": "nope"}, `workflows need one of agent "bot"'s channels; "nope" is not one`},
		{"workflow with schedule", map[string]any{"name": "wf", "every": "1h"}, "cannot also have a schedule or watch"},
		{"workflow with run_once", map[string]any{"name": "wf", "run_once": true}, "cannot also have a schedule or watch"},
		{"workflow with watch", map[string]any{"name": "wf", "watch": "notes.txt"}, "cannot also have a schedule or watch"},
		{"project workflow", map[string]any{"name": "triage", "trigger": "message", "every": "", "channel": "Ops"}, "workflows run prompt or script content"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Contains(t, toolErrorText(d.CallTool(context.Background(), "task_update", tc.args)), tc.want)
		})
	}

	// Rejected updates leave the saved tasks unchanged.
	hourly := loadBotTask(t, "hourly")
	assert.Equal(t, "1h", hourly.Every)
	assert.Empty(t, hourly.Target)
	assert.Empty(t, hourly.Watch)
	wf := loadBotTask(t, "wf")
	assert.Equal(t, "Ops", wf.Channel)
	assert.Empty(t, wf.Every)
}

func TestTaskUpdate_RunOnceAndWorkflowChannel(t *testing.T) {
	s := setupSchedulerDeps(t, &config.Config{Agents: []config.AgentConfig{{
		Name: "bot", Model: "test/x",
		Channels: []config.ChannelConfig{{Type: "slack", ID: "ops", Name: "Ops"}, {Type: "slack", ID: "dev"}},
	}}})
	writeTaskFile(t, "hourly.md", "---\nname: hourly\nevery: 1h\nprecompile: false\n---\nsay hi\n")
	writeTaskFile(t, "wf.md", "---\nname: wf\ntrigger: message\nchannel: Ops\nprecompile: false\n---\nanswer\n")
	d := NewDispatcher("https://localhost:16677", "")

	out, err := d.CallTool(context.Background(), "task_update", map[string]any{"name": "hourly", "run_once": true, "trigger": "interval"})
	require.NoError(t, err)
	assert.Contains(t, out, `Task "hourly" updated for agent "bot".`)
	hourly := loadBotTask(t, "hourly")
	assert.True(t, hourly.RunOnce)
	assert.Equal(t, "interval", hourly.Trigger)
	for _, task := range s.ListTasks() {
		if task.ID == "bot/hourly" {
			assert.True(t, task.RunOnce)
		}
	}

	// A workflow's channel is matched case-insensitively and saved under the
	// channel's own name.
	_, err = d.CallTool(context.Background(), "task_update", map[string]any{"name": "wf", "channel": "DEV", "match": " deploy ", "target": "reply"})
	require.NoError(t, err)
	wf := loadBotTask(t, "wf")
	assert.Equal(t, "dev", wf.Channel)
	assert.Equal(t, "deploy", wf.Match)
	assert.Equal(t, "reply", wf.Target)
}

func TestTaskUpdate_TaskFileWriteFails(t *testing.T) {
	setupSchedulerDeps(t, &config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "test/x", Tasks: []config.TaskConfig{
		{Name: "inline", Every: "1h", Prompt: "x"},
	}}}})
	// A file where the tasks directory belongs keeps the task from saving.
	dir := config.AgentTasksDir(config.AgentConfig{Name: "bot"})
	require.NoError(t, os.MkdirAll(filepath.Dir(dir), 0o750))
	require.NoError(t, os.WriteFile(dir, []byte("not a directory"), 0o600))
	d := NewDispatcher("https://localhost:16677", "")

	assert.Contains(t, toolErrorText(d.CallTool(context.Background(), "task_update", map[string]any{"name": "inline", "every": "2h"})), "creating tasks dir")
	cfg, err := config.Load("")
	require.NoError(t, err)
	assert.Equal(t, "1h", cfg.Agents[0].Tasks[0].Every)

	// A task in aviary.yaml cannot be deleted through task_delete.
	assert.Contains(t, toolErrorText(d.CallTool(context.Background(), "task_delete", map[string]any{"name": "inline"})), "is defined in aviary.yaml")
}
