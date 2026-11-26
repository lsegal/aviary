package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
)

// #1307: project_add, project_update, project_status and project_check all
// speak a saved project's mode.
func TestProjectModeAddUpdateAndStatus(t *testing.T) {
	d, m := setupProjects(t)
	cfg := &config.Config{Agents: []config.AgentConfig{{Name: "coder", Model: "cli/codex", Projects: config.AgentProjectsConfig{{Target: "o/a"}}}}}
	require.NoError(t, config.Save("", cfg))
	m.Reconcile(cfg)

	added := callJSON(t, d, "project_add", map[string]any{"target": "o/b", "agent": "coder", "mode": "manual"})
	assert.Equal(t, "manual", added["mode"])
	saved, err := config.Load("")
	require.NoError(t, err)
	assert.Equal(t, "manual", saved.Agents[0].Projects[1].Mode)
	m.Reconcile(saved)
	status := callJSON(t, d, "project_status", map[string]any{})
	assert.Equal(t, map[string]any{"o/a": "watch", "o/b": "manual"}, status["modes"])
	assert.Equal(t, map[string]any{"o/a": "global", "o/b": "project"}, status["mode_sources"])
	assert.Equal(t, "watch", status["default_mode"])
	assert.NotContains(t, status["issue_counts"], "o/b", "a manual project has no issue count")

	updated := callJSON(t, d, "project_update", map[string]any{"target": "o/a", "mode": "manual"})
	assert.Equal(t, "manual", updated["mode"])
	assert.Equal(t, true, updated["saved"])
	saved, err = config.Load("")
	require.NoError(t, err)
	assert.Equal(t, "manual", saved.Agents[0].Projects[0].Mode)
	assert.Empty(t, saved.Agents[0].Projects[0].Filter, "changing the mode leaves the filter alone")

	updated = callJSON(t, d, "project_update", map[string]any{"target": "o/a", "mode": "watch"})
	assert.Equal(t, "watch", updated["mode"])
	assert.Equal(t, "project", updated["mode_source"])
	saved, err = config.Load("")
	require.NoError(t, err)
	assert.Equal(t, "watch", saved.Agents[0].Projects[0].Mode, "watch is saved as an override")
	assert.Equal(t, "manual", saved.Agents[0].Projects[1].Mode, "only the named project changes")

	updated = callJSON(t, d, "project_update", map[string]any{"target": "o/a", "mode": "inherit"})
	assert.Equal(t, "watch", updated["mode"])
	assert.Equal(t, "global", updated["mode_source"])
	saved, err = config.Load("")
	require.NoError(t, err)
	assert.Empty(t, saved.Agents[0].Projects[0].Mode, "inherit removes the override")

	toolCallContains(t, d, "project_update", map[string]any{"target": "o/a", "mode": "sometimes"}, "unknown project mode")
	toolCallContains(t, d, "project_add", map[string]any{"target": "o/c", "agent": "coder", "mode": "sometimes"}, "unknown project mode")
}

func TestProjectModeRuntimeWatch(t *testing.T) {
	d, m := setupProjects(t)
	m.Reconcile(&config.Config{Models: config.ModelsConfig{Defaults: &config.ModelDefaults{Model: "cli/codex/gpt-5.6-terra"}}})
	before := readFile(t, config.DefaultPath())
	added := callJSON(t, d, "project_add", map[string]any{"target": "o/r", "runtime": true, "mode": "manual"})
	assert.Equal(t, "manual", added["mode"])
	status := callJSON(t, d, "project_status", map[string]any{})
	assert.Equal(t, map[string]any{"o/r": "manual"}, status["modes"])

	updated := callJSON(t, d, "project_update", map[string]any{"target": "o/r", "mode": "watch"})
	assert.Equal(t, true, updated["runtime"])
	assert.Equal(t, "watch", updated["mode"])
	assert.Equal(t, before, readFile(t, config.DefaultPath()), "a runtime watch's mode is never saved")
}

func TestProjectCheckReportsMode(t *testing.T) {
	stubProjectCheck(t, []string{"gh"}, map[string]string{"auth status": ""})
	projectConfig = func() (*config.Config, error) {
		return &config.Config{Agents: []config.AgentConfig{{Name: "coder", Projects: config.AgentProjectsConfig{{Target: "users/lsegal/projects/4", Mode: "manual"}}}}}, nil
	}
	res, err := runProjectCheck(context.Background(), projectCheckArgs{Target: "users/lsegal/projects/4"})
	require.NoError(t, err)
	assert.Equal(t, "manual", res.Mode, "the saved project's mode")
	assert.Equal(t, projectCheckInfo, checkByName(res)["mode"].Status)

	res, err = runProjectCheck(context.Background(), projectCheckArgs{Target: "users/lsegal/projects/4", Mode: "watch"})
	require.NoError(t, err)
	assert.Equal(t, "watch", res.Mode, "the form's mode wins")

	res, err = runProjectCheck(context.Background(), projectCheckArgs{Target: "users/lsegal/projects/5"})
	require.NoError(t, err)
	assert.Equal(t, "watch", res.Mode, "watch is the default")

	_, err = runProjectCheck(context.Background(), projectCheckArgs{Target: "users/lsegal/projects/4", Mode: "never"})
	assert.ErrorContains(t, err, "unknown project mode")
}

// #1371: a project without its own mode follows the shared projects mode,
// and project_check and project_status say where the mode comes from.
func TestProjectModeFollowsGlobalDefault(t *testing.T) {
	d, m := setupProjects(t)
	cfg := &config.Config{
		Agents:   []config.AgentConfig{{Name: "coder", Model: "cli/codex", Projects: config.AgentProjectsConfig{{Target: "o/a"}, {Target: "o/b", Mode: "watch"}}}},
		Projects: config.ProjectsConfig{Mode: "manual"},
	}
	require.NoError(t, config.Save("", cfg))
	m.Reconcile(cfg)
	status := callJSON(t, d, "project_status", map[string]any{})
	assert.Equal(t, map[string]any{"o/a": "manual", "o/b": "watch"}, status["modes"])
	assert.Equal(t, map[string]any{"o/a": "global", "o/b": "project"}, status["mode_sources"])
	assert.Equal(t, "manual", status["default_mode"])
	assert.NotContains(t, status["issue_counts"], "o/a")

	added := callJSON(t, d, "project_add", map[string]any{"target": "o/c", "agent": "coder"})
	assert.Equal(t, "manual", added["mode"])
	assert.Equal(t, "global", added["mode_source"])
	saved, err := config.Load("")
	require.NoError(t, err)
	assert.Empty(t, saved.Agents[0].Projects[2].Mode, "a new project inherits by default")

	updated := callJSON(t, d, "project_update", map[string]any{"target": "o/b", "mode": "inherit"})
	assert.Equal(t, "manual", updated["mode"])
	assert.Equal(t, "global", updated["mode_source"])
}

func TestProjectCheckReportsModeSource(t *testing.T) {
	stubProjectCheck(t, []string{"gh"}, map[string]string{"auth status": ""})
	projectConfig = func() (*config.Config, error) {
		return &config.Config{
			Agents:   []config.AgentConfig{{Name: "coder", Projects: config.AgentProjectsConfig{{Target: "o/a"}, {Target: "o/b", Mode: "watch"}}}},
			Projects: config.ProjectsConfig{Mode: "manual"},
		}, nil
	}
	for _, tc := range []struct{ target, mode, want, source string }{
		{"o/a", "", "manual", "global"},
		{"o/b", "", "watch", "project"},
		{"o/b", "inherit", "manual", "global"},
		{"o/a", "watch", "watch", "project"},
	} {
		res, err := runProjectCheck(context.Background(), projectCheckArgs{Target: tc.target, Mode: tc.mode})
		require.NoError(t, err)
		assert.Equal(t, tc.want, res.Mode, "%s %q", tc.target, tc.mode)
		assert.Equal(t, tc.source, res.ModeSource, "%s %q", tc.target, tc.mode)
	}
}
