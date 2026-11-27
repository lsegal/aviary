package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
)

func TestNextTaskName(t *testing.T) {
	assert.Equal(t, "release", nextTaskName("release", nil))
	assert.Equal(t, "release", nextTaskName("release", []string{"other"}))
	assert.Equal(t, "release-2", nextTaskName("release", []string{"release"}))
	assert.Equal(t, "release-3", nextTaskName("release", []string{"release", "release-2"}))
	assert.Equal(t, "x-3", nextTaskName("x-2", []string{"x", "x-2"}))
	assert.Equal(t, "x-6", nextTaskName("x", []string{"x", "x-2", "x-5", "x-05x"}))
	assert.Equal(t, "x-8", nextTaskName("x-7", []string{"x-7"}))
}

func TestRepoReferences(t *testing.T) {
	prompt := "Release lsegal/aviary: see https://github.com/lsegal/aviary/releases and ~/src/aviary/CHANGELOG.md."
	assert.Equal(t, []string{"lsegal/aviary", "lsegal/aviary", "~/src/aviary/CHANGELOG.md"}, repoReferences(prompt, "lsegal/aviary"))
	assert.Empty(t, repoReferences("Check lsegal/aviary-docs and aviary", "lsegal/aviary"))
	assert.Empty(t, repoReferences("lsegal/aviary", ""))

	assert.Equal(t,
		"Release lsegal/yard from https://github.com/lsegal/yard.git in /home/me/yard; leave lsegal/aviary-docs.",
		replaceRepoReferences("Release LSegal/Aviary from https://github.com/lsegal/aviary.git in /home/me/aviary; leave lsegal/aviary-docs.", "lsegal/aviary", "lsegal/yard"))
	assert.Equal(t, "unchanged", replaceRepoReferences("unchanged", "lsegal/aviary", ""))
}

// setupDuplicateProjects watches acme/app and acme/web, owned by agent bot,
// and acme/lib, owned by agent other, with a paused, precompiled task
// release on acme/app.
func setupDuplicateProjects(t *testing.T) *Dispatcher {
	t.Helper()
	d, s := setupProjectTasks(t)
	cfg, err := config.Load("")
	require.NoError(t, err)
	cfg.Agents[0].Projects = []config.ProjectConfig{{Target: "acme/app"}, {Target: "acme/web"}}
	cfg.Agents[1].Projects = []config.ProjectConfig{{Target: "acme/lib"}}
	require.NoError(t, config.Save("", cfg))
	cfg, err = config.Load("")
	require.NoError(t, err)
	deps := GetDeps()
	deps.Projects.Reconcile(cfg)
	deps.Agents.Reconcile(cfg)
	s.Reconcile(cfg)

	callJSON(t, d, "project_task_create", map[string]any{
		"project": "acme/app", "name": "release", "content": "Cut a release of acme/app from https://github.com/acme/app",
		"trigger": "weekly", "days": "fri", "at": "16:00", "timezone": "UTC", "overlap": "skip", "missed": "run_once",
		"model": "test/x", "precompile": false,
	})
	callJSON(t, d, "project_task_pause", map[string]any{"name": "bot/release"})
	cfg, err = config.Load("")
	require.NoError(t, err)
	for i := range cfg.Agents[0].Tasks {
		cfg.Agents[0].Tasks[i].Compiled = "return 'stale'"
	}
	require.NoError(t, config.Save("", cfg))
	return d
}

func savedTask(t *testing.T, agent, name string) config.TaskConfig {
	t.Helper()
	cfg, err := config.Load("")
	require.NoError(t, err)
	for _, a := range cfg.Agents {
		if a.Name != agent {
			continue
		}
		for _, tc := range a.Tasks {
			if tc.Name == name {
				return tc
			}
		}
	}
	t.Fatalf("no task %s/%s", agent, name)
	return config.TaskConfig{}
}

func TestProjectTaskDuplicateSameProjectSuffixesName(t *testing.T) {
	d := setupDuplicateProjects(t)

	copied := callJSON(t, d, "project_task_duplicate", map[string]any{"name": "release"})
	assert.Equal(t, true, copied["saved"])
	assert.Equal(t, "release-2", copied["name"])
	assert.Equal(t, "bot/release", copied["duplicated_from"])

	tc := savedTask(t, "bot", "release-2")
	assert.Equal(t, "acme/app", tc.Project)
	assert.Equal(t, "Cut a release of acme/app from https://github.com/acme/app", tc.Prompt)
	assert.Equal(t, "weekly", tc.Trigger)
	assert.Equal(t, "fri", tc.Days)
	assert.Equal(t, "16:00", tc.At)
	assert.Equal(t, "UTC", tc.Timezone)
	assert.Equal(t, "skip", tc.Overlap)
	assert.Equal(t, "run_once", tc.Missed)
	assert.Equal(t, "test/x", tc.Model)
	assert.False(t, config.BoolOr(tc.Enabled, true), "the copy is paused like its source")
	assert.Empty(t, tc.Compiled, "the compiled script is not copied")

	again := callJSON(t, d, "project_task_duplicate", map[string]any{"name": "release-2"})
	assert.Equal(t, "release-3", again["name"])
}

func TestProjectTaskDuplicateAcrossProjects(t *testing.T) {
	d := setupDuplicateProjects(t)

	// Another agent's project has no release, so the name is kept, and
	// the prompt keeps its references unless asked to replace them.
	dry := callJSON(t, d, "project_task_duplicate", map[string]any{"name": "release", "new_project": "acme/lib", "dry_run": true})
	assert.Equal(t, false, dry["saved"])
	assert.Equal(t, "other", dry["agent"])
	assert.Equal(t, "weekly on Fri at 16:00 (UTC)", dry["preview"].(map[string]any)["schedule"])
	refs := dry["repo_references"].(map[string]any)
	assert.Equal(t, []any{"acme/app", "acme/app"}, refs["found"])
	assert.Equal(t, false, refs["replaced"])

	kept := callJSON(t, d, "project_task_duplicate", map[string]any{"name": "release", "new_project": "acme/lib"})
	assert.Equal(t, "release", kept["name"])
	assert.Equal(t, "Cut a release of acme/app from https://github.com/acme/app", savedTask(t, "other", "release").Prompt)

	replaced := callJSON(t, d, "project_task_duplicate", map[string]any{
		"name": "bot/release", "new_project": "acme/web", "new_name": "web-release", "replace_repo_references": true,
	})
	assert.Equal(t, "web-release", replaced["name"])
	assert.Equal(t, true, replaced["repo_references"].(map[string]any)["replaced"])
	tc := savedTask(t, "bot", "web-release")
	assert.Equal(t, "acme/web", tc.Project)
	assert.Equal(t, "Cut a release of acme/web from https://github.com/acme/web", tc.Prompt)

	assert.Contains(t, toolErrorText(d.CallTool(context.Background(), "project_task_duplicate", map[string]any{
		"name": "release", "project": "acme/app", "new_project": "not/watched",
	})), "not a project this instance watches")
	assert.Contains(t, toolErrorText(d.CallTool(context.Background(), "project_task_duplicate", map[string]any{
		"name": "release", "project": "acme/app", "new_name": "web-release",
	})), "already has a task named")
}
