package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/projects"
	"github.com/lsegal/aviary/internal/scheduler"
)

// setupProjectTasks gives a test a scheduler and a projects manager watching
// acme/app, owned by agent bot, and other/elsewhere's task outside them.
func setupProjectTasks(t *testing.T) (*Dispatcher, *scheduler.Scheduler) {
	t.Helper()
	d, s := setupDispatcherWithScheduler(t)
	cfg, err := config.Load("")
	require.NoError(t, err)
	cfg.Agents[0].Projects = []config.ProjectConfig{{Target: "acme/app"}}
	cfg.Agents = append(cfg.Agents, config.AgentConfig{Name: "other", Model: "test/x"})
	require.NoError(t, config.Save("", cfg))
	cfg, err = config.Load("")
	require.NoError(t, err)

	m, err := projects.NewManager(filepath.Join(t.TempDir(), "projects"))
	require.NoError(t, err)
	m.SetEngineEnabled(false)
	m.Reconcile(cfg)
	deps := GetDeps()
	deps.Agents.Reconcile(cfg)
	s.Reconcile(cfg)
	SetDeps(&Deps{Agents: deps.Agents, Scheduler: s, Projects: m})

	prev := taskAnalyzerFunc
	taskAnalyzerFunc = func(context.Context, string, string, string) (*taskAnalyzerResult, error) {
		return &taskAnalyzerResult{Analysis: &compileTaskAnalysis{NeedsDiscovery: true, Reason: "needs judgement"}, Model: "test/x"}, nil
	}
	t.Cleanup(func() { taskAnalyzerFunc = prev })
	return d, s
}

func TestProjectTaskCreatePreviewsAndSavesLikeTaskSchedule(t *testing.T) {
	d, s := setupProjectTasks(t)

	dry := callJSON(t, d, "project_task_create", map[string]any{
		"project": "acme/app", "name": "weekly-seo", "content": "Review the docs site's SEO and file issues",
		"trigger": "weekly", "days": "mon", "at": "09:00", "timezone": "UTC", "precompile": false, "dry_run": true,
	})
	assert.Equal(t, false, dry["saved"])
	preview := dry["preview"].(map[string]any)
	assert.Equal(t, "weekly on Mon at 09:00 (UTC)", preview["schedule"])
	assert.Len(t, preview["next_runs"], 3)
	assert.InDelta(t, 4, preview["runs_per_30_days"], 1)
	estimate := dry["estimate"].(map[string]any)
	assert.Equal(t, taskVerdictNonDeterministic, estimate["verdict"])
	assert.Contains(t, estimate, "per_run")
	cfg, err := config.Load("")
	require.NoError(t, err)
	assert.Empty(t, cfg.Agents[0].Tasks)

	created := callJSON(t, d, "project_task_create", map[string]any{
		"project": "acme/app", "name": "weekly-seo", "content": "Review the docs site's SEO and file issues",
		"trigger": "weekly", "days": "mon", "at": "09:00", "timezone": "UTC", "precompile": false,
	})
	assert.Equal(t, true, created["saved"])
	task := created["task"].(map[string]any)
	assert.Equal(t, "bot/weekly-seo", task["id"])
	assert.Equal(t, "weekly on Mon at 09:00 (UTC)", task["trigger"])
	assert.NotEmpty(t, task["next_run"])
	assert.Equal(t, "file", task["source"].(map[string]any)["kind"])

	// Saved as task_schedule saves it: a project task file of the owning
	// agent, which the scheduler and the board read like a UI-made task.
	cfg, err = config.Load("")
	require.NoError(t, err)
	require.Len(t, cfg.Agents[0].Tasks, 1)
	saved := cfg.Agents[0].Tasks[0]
	assert.Equal(t, "project", saved.Type)
	assert.Equal(t, "acme/app", saved.Project)
	assert.Equal(t, "weekly", saved.Trigger)
	assert.True(t, saved.FromFile)
	var listed bool
	for _, st := range s.ListTasks() {
		listed = listed || st.ID == "bot/weekly-seo"
	}
	assert.True(t, listed)

	assert.Contains(t, toolErrorText(d.CallTool(context.Background(), "project_task_create", map[string]any{
		"project": "acme/app", "name": "bad", "content": "x", "trigger": "weekly", "at": "09:00",
	})), "weekly trigger requires days")
	assert.Contains(t, toolErrorText(d.CallTool(context.Background(), "project_task_create", map[string]any{
		"project": "acme/app", "name": "weekly-seo", "content": "x", "every": "1h", "trigger": "interval",
	})), "already has a task named")
	assert.Contains(t, toolErrorText(d.CallTool(context.Background(), "project_task_create", map[string]any{
		"project": "not/watched", "content": "x", "trigger": "interval", "every": "1h",
	})), "not a project this instance watches")
}

func TestProjectTaskListGetUpdatePauseRunDelete(t *testing.T) {
	d, _ := setupProjectTasks(t)
	_, err := d.CallTool(context.Background(), "task_schedule", map[string]any{
		"agent": "bot", "name": "triage", "type": "project", "project": "acme/app",
		"content": "Triage new issues", "every": "2h", "trigger": "interval", "precompile": false,
	})
	require.NoError(t, err)
	// Tasks outside the watched projects are never listed or managed.
	_, err = d.CallTool(context.Background(), "task_schedule", map[string]any{
		"agent": "other", "name": "elsewhere", "type": "project", "project": "other/elsewhere",
		"content": "x", "every": "1h", "trigger": "interval", "precompile": false,
	})
	require.NoError(t, err)
	_, err = d.CallTool(context.Background(), "task_schedule", map[string]any{
		"agent": "bot", "name": "digest", "content": "x", "every": "1h", "trigger": "interval", "precompile": false,
	})
	require.NoError(t, err)

	list := callJSON(t, d, "project_task_list", map[string]any{})
	require.EqualValues(t, 1, list["total"])
	entry := list["tasks"].([]any)[0].(map[string]any)
	assert.Equal(t, "bot/triage", entry["id"])
	assert.Equal(t, "acme/app", entry["project"])
	assert.Equal(t, true, entry["enabled"])
	assert.Equal(t, false, entry["paused"])
	assert.Equal(t, "every 2h0m0s (Local)", entry["trigger"])
	assert.NotEmpty(t, entry["next_run"])
	assert.Contains(t, entry, "precompile")
	assert.Equal(t, "file", entry["source"].(map[string]any)["kind"])
	assert.Contains(t, toolErrorText(d.CallTool(context.Background(), "project_task_list", map[string]any{"project": "other/elsewhere"})), "not a project this instance watches")

	got := callJSON(t, d, "project_task_get", map[string]any{"name": "triage"})
	assert.Equal(t, "Triage new issues", got["prompt"])
	assert.Equal(t, "default routing", got["send_to"])
	assert.Equal(t, "queue", got["trigger_fields"].(map[string]any)["overlap"])
	assert.Len(t, got["next_runs"], 3)
	assert.Equal(t, []any{}, got["recent_runs"])
	for _, name := range []string{"elsewhere", "digest"} {
		assert.Contains(t, toolErrorText(d.CallTool(context.Background(), "project_task_get", map[string]any{"name": name})), "no scheduled task")
	}

	updated := callJSON(t, d, "project_task_update", map[string]any{
		"name": "triage", "trigger": "daily", "every": "", "at": "08:30", "model": "test/y", "send_to": "silent", "new_name": "morning-triage",
	})
	assert.Contains(t, updated["edit"], "edited its task file")
	cfg, err := config.Load("")
	require.NoError(t, err)
	var task config.TaskConfig
	for _, tc := range cfg.Agents[0].Tasks {
		if tc.Name == "morning-triage" {
			task = tc
		}
	}
	assert.Equal(t, "daily", task.Trigger)
	assert.Equal(t, "08:30", task.At)
	assert.Equal(t, "test/y", task.Model)
	assert.Equal(t, "silent", task.Target)
	assert.Contains(t, toolErrorText(d.CallTool(context.Background(), "project_task_update", map[string]any{"name": "morning-triage", "at": "25:99"})), "invalid")
	assert.Contains(t, toolErrorText(d.CallTool(context.Background(), "project_task_update", map[string]any{"name": "morning-triage", "new_project": "other/elsewhere"})), "not a project this instance watches")

	paused := callJSON(t, d, "project_task_pause", map[string]any{"name": "morning-triage"})
	assert.Equal(t, true, paused["task"].(map[string]any)["paused"])
	cfg, err = config.Load("")
	require.NoError(t, err)
	for _, tc := range cfg.Agents[0].Tasks {
		if tc.Name == "morning-triage" {
			assert.False(t, config.BoolOr(tc.Enabled, true))
		}
	}
	resumed := callJSON(t, d, "project_task_resume", map[string]any{"name": "bot/morning-triage"})
	assert.Equal(t, false, resumed["task"].(map[string]any)["paused"])

	run := callJSON(t, d, "project_task_run", map[string]any{"name": "morning-triage"})
	assert.Equal(t, "bot/morning-triage", run["task"])
	assert.NotNil(t, run["task_job"])

	preview := callJSON(t, d, "project_task_delete", map[string]any{"name": "morning-triage"})
	assert.Equal(t, true, preview["confirmation_required"])
	assert.Contains(t, preview["removes"], "run history")
	path, err := config.FindMarkdownTask(config.AgentTasksDir(cfg.Agents[0]), "morning-triage")
	require.NoError(t, err)
	assert.FileExists(t, path)
	deleted := callJSON(t, d, "project_task_delete", map[string]any{"name": "morning-triage", "confirm": true})
	assert.Equal(t, true, deleted["deleted"])
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err))
}

func TestProjectTaskDefinedInConfig(t *testing.T) {
	d, _ := setupProjectTasks(t)
	cfg, err := config.Load("")
	require.NoError(t, err)
	enabled := true
	cfg.Agents[0].Tasks = []config.TaskConfig{{
		Name: "nightly", Type: "project", Project: "acme/app", Prompt: "Clean up", Every: "24h", Trigger: "interval", Enabled: &enabled,
	}}
	require.NoError(t, config.Save("", cfg))
	require.NoError(t, reloadTaskConfig())

	got := callJSON(t, d, "project_task_get", map[string]any{"name": "nightly"})
	assert.Equal(t, "config", got["source"].(map[string]any)["kind"])

	// Deleting a task aviary.yaml defines is refused with the reason, as
	// task_delete refuses it.
	assert.Contains(t, toolErrorText(d.CallTool(context.Background(), "project_task_delete", map[string]any{"name": "nightly", "confirm": true})), "defined in aviary.yaml")

	// Editing one saves it to a task file, and the result says so.
	updated := callJSON(t, d, "project_task_update", map[string]any{"name": "nightly", "content": "Clean up stale branches"})
	assert.Contains(t, updated["edit"], "replaces its definition in")
	assert.Equal(t, "file", updated["task"].(map[string]any)["source"].(map[string]any)["kind"])
}

func TestProjectTaskToolsAreTheCodeLoopAgentsOnly(t *testing.T) {
	for _, name := range []string{
		"project_task_list", "project_task_get", "project_task_create", "project_task_update", "project_task_duplicate",
		"project_task_pause", "project_task_resume", "project_task_run", "project_task_delete",
	} {
		assert.Contains(t, config.ProjectsAgentTools, name)
	}
	assert.Contains(t, config.ProjectsAgentRules, "project_task_create")
}
