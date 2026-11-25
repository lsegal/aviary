package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/projects"
	"github.com/lsegal/aviary/internal/store"
)

var projectToolNames = []string{
	"project_status", "project_job_list", "project_job_get", "project_job_explain",
	"project_job_retry", "project_job_pause", "project_job_resume", "project_job_stop", "project_job_dismiss",
	"project_job_queue", "project_queue_candidates",
	"project_refresh", "project_add", "project_update", "project_filter_check", "project_remove",
	"project_clone_list", "project_clone_clear", "project_unblock_run",
	"project_issue_comment", "project_issue_create", "project_issue_backfill_media",
	"project_github_signin", "project_github_use_polling", "project_github_import",
	"project_github_read", "project_site_fetch",
}

// setupProjects gives each test its own config and a projects manager that
// tracks projects without starting an engine against GitHub.
func setupProjects(t *testing.T) (*Dispatcher, *projects.Manager) {
	t.Helper()
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	store.SetDataDir(base + "/aviary")
	t.Cleanup(func() { store.SetDataDir("") })
	t.Setenv("AVIARY_CONFIG_BASE_DIR", base+"/aviary")
	require.NoError(t, store.EnsureDirs())
	require.NoError(t, config.Save("", &config.Config{}))

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	m, err := projects.NewManager(filepath.Join(base, "projects"))
	require.NoError(t, err)
	m.SetEngineEnabled(false)
	SetDeps(&Deps{Projects: m})
	return NewDispatcher("https://localhost:16677", ""), m
}

func callJSON(t *testing.T, d *Dispatcher, tool string, args map[string]any) map[string]any {
	t.Helper()
	out, err := d.CallTool(context.Background(), tool, args)
	require.NoError(t, err)
	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &result), out)
	return result
}

func TestProjectTools_NilDeps(t *testing.T) {
	old := GetDeps()
	SetDeps(&Deps{})
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })
	d := NewDispatcher("https://localhost:16677", "")
	job := map[string]any{"number": 1}
	args := map[string]map[string]any{
		"project_job_get": job, "project_job_explain": job, "project_job_retry": job,
		"project_job_pause": job, "project_job_resume": job, "project_job_stop": job, "project_job_dismiss": job,
		"project_job_queue":        {"skill": "gh-safe-merge", "numbers": []any{1}},
		"project_queue_candidates": {"skill": "gh-safe-merge"},
		"project_add":              {"target": "o/r"}, "project_remove": {"target": "o/r"},
		"project_update":        {"target": "o/r", "filter": "label:bug"},
		"project_filter_check":  {"target": "o/r", "filter": "label:bug"},
		"project_issue_comment": {"number": 1, "text": "x"}, "project_issue_create": {"text": "x"},
		"project_issue_backfill_media": {"numbers": []any{1}},
		"project_github_import":        {"profile": "/p"},
		"project_site_fetch":           {"url": "https://aviary.bot"},
	}
	for _, tool := range projectToolNames {
		toolArgs := args[tool]
		if toolArgs == nil {
			toolArgs = map[string]any{}
		}
		toolCallContains(t, d, tool, toolArgs, "projects are not available")
	}
}

func TestProjectTools_SchemasAndHints(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test"}, nil)
	registerProjectTools(server)
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "client"}, nil)
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	_, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	session, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	listed, err := session.ListTools(ctx, nil)
	require.NoError(t, err)
	tools := map[string]*sdkmcp.Tool{}
	for _, tool := range listed.Tools {
		tools[tool.Name] = tool
	}
	for _, name := range projectToolNames {
		tool := tools[name]
		require.NotNil(t, tool, name)
		require.NotNil(t, tool.InputSchema, name)
		require.NotNil(t, tool.Annotations, name)
		readOnly := tool.Annotations.ReadOnlyHint
		destructive := tool.Annotations.DestructiveHint != nil && *tool.Annotations.DestructiveHint
		assert.False(t, readOnly && destructive, name)
		if !readOnly {
			assert.NotNil(t, tool.Annotations.DestructiveHint, "%s needs an explicit destructive hint", name)
		}
	}
	for _, name := range []string{"project_status", "project_job_list", "project_job_get", "project_job_explain", "project_clone_list", "project_github_read", "project_site_fetch", "project_filter_check"} {
		assert.True(t, tools[name].Annotations.ReadOnlyHint, name)
	}
	for _, name := range []string{"project_job_stop", "project_remove", "project_clone_clear"} {
		assert.True(t, *tools[name].Annotations.DestructiveHint, name)
	}
	schema, err := json.Marshal(tools["project_job_get"].InputSchema)
	require.NoError(t, err)
	for _, field := range []string{`"number"`, `"target"`, `"lines"`, `"events"`} {
		assert.Contains(t, string(schema), field)
	}
}

func TestProjectStatusAndJobsWithNothingWatched(t *testing.T) {
	d, m := setupProjects(t)
	status := callJSON(t, d, "project_status", map[string]any{})
	assert.Equal(t, m.Identity(), status["identity"])
	assert.Equal(t, false, status["running"])
	assert.Equal(t, []any{}, status["targets"])
	assert.Equal(t, "browser", status["delivery"])
	assert.Equal(t, "20s", status["poll_interval"])
	assert.Equal(t, map[string]any{"signed_out": false, "signing_in": false}, status["github"])
	assert.Equal(t, "assignee:@me", status["issue_filter"])
	assert.NotContains(t, status, "ready_label", "pickup needs no label, so status reports none")
	assert.EqualValues(t, 3, status["concurrency"])

	jobs := callJSON(t, d, "project_job_list", map[string]any{"status": "active"})
	assert.Equal(t, []any{}, jobs["jobs"])
	assert.EqualValues(t, 0, jobs["total"])

	toolCallContains(t, d, "project_job_get", map[string]any{"number": 7}, "no job #7")
	toolCallContains(t, d, "project_job_explain", map[string]any{"number": 7, "target": "o/r"}, "no job o/r#7")
	toolCallContains(t, d, "project_job_get", map[string]any{"number": 0}, "job number is required")
	for _, tool := range []string{"project_job_retry", "project_job_pause", "project_job_resume", "project_job_stop", "project_job_dismiss", "project_unblock_run"} {
		toolCallContains(t, d, tool, map[string]any{"number": 7}, "no projects are being watched")
	}
	for _, tool := range []string{"project_refresh", "project_clone_list", "project_clone_clear"} {
		toolCallContains(t, d, tool, map[string]any{}, "no projects are being watched")
	}
	toolCallContains(t, d, "project_issue_comment", map[string]any{"number": 7, "text": "hi"}, "no projects are being watched")
	toolCallContains(t, d, "project_issue_create", map[string]any{"text": "hi"}, "no projects are being watched")
}

func TestProjectAddAndRemoveSavedProject(t *testing.T) {
	d, m := setupProjects(t)
	require.NoError(t, config.Save("", &config.Config{Agents: []config.AgentConfig{{Name: "coder", Model: "cli/claude/opus"}}}))
	toolCallContains(t, d, "project_add", map[string]any{"target": "o/r", "agent": "nobody"}, `agent "nobody" is not configured`)
	added := callJSON(t, d, "project_add", map[string]any{"target": "o/r", "agent": "coder"})
	assert.Equal(t, true, added["saved"])
	cfg, err := config.Load("")
	require.NoError(t, err)
	assert.Equal(t, "coder", added["agent"])
	require.Len(t, cfg.Agents, 1, "no agent was created for the project")
	assert.Equal(t, config.AgentProjectsConfig{{Target: "o/r"}}, cfg.Agents[0].Projects)

	status := callJSON(t, d, "project_status", map[string]any{})
	assert.Equal(t, []any{"o/r"}, status["targets"])
	toolCallContains(t, d, "project_add", map[string]any{"target": "o/r", "agent": "coder"}, `already watched by agent "coder"`)
	toolCallContains(t, d, "project_add", map[string]any{"target": "o/s"}, "agent is required")
	toolCallContains(t, d, "project_add", map[string]any{"target": " "}, "target is required")
	toolCallContains(t, d, "project_add", map[string]any{"target": "o/s", "agent": "coder", "settings": map[string]any{"no_merge": true}}, "only to a runtime watch")

	// Removing a project stops its jobs that still hold work and names them;
	// finished jobs and other projects' jobs are not counted.
	jobs := []projects.JobSnapshot{{Number: 1, Target: "o/r", Status: "active"}, {Number: 2, Target: "o/r", Status: "waiting"},
		{Number: 3, Target: "o/r", Status: "queued"}, {Number: 4, Target: "o/r", Status: "paused"},
		{Number: 5, Target: "o/r", Status: "complete"}, {Number: 6, Target: "o/r", Status: "failed"},
		{Number: 7, Target: "o/other", Status: "active"}}
	m.Snapshot(projects.EngineSnapshot{Jobs: jobs})
	removed := callJSON(t, d, "project_remove", map[string]any{"target": "o/r"})
	assert.Equal(t, true, removed["removed"])
	assert.Equal(t, []any{1.0, 2.0, 3.0, 4.0}, removed["stopped_jobs"])
	cfg, err = config.Load("")
	require.NoError(t, err)
	assert.Empty(t, cfg.Agents[0].Projects)
	toolCallContains(t, d, "project_remove", map[string]any{"target": "o/r"}, "not watched")
}

// TestProjectAddRuntimeWritesNoConfig checks the zero-config path aviary watch
// takes: a runtime watch is watched without touching aviary.yaml.
func TestProjectAddRuntimeWritesNoConfig(t *testing.T) {
	d, m := setupProjects(t)
	m.Reconcile(&config.Config{Models: config.ModelsConfig{Defaults: &config.ModelDefaults{Model: "cli/codex/gpt-5.6-terra"}}})
	path := config.DefaultPath()
	before := readFile(t, path)
	added := callJSON(t, d, "project_add", map[string]any{"target": "o/r", "runtime": true, "settings": map[string]any{"concurrency": 5, "no_merge": true}})
	assert.Equal(t, true, added["runtime"])
	assert.Equal(t, before, readFile(t, path))
	status := callJSON(t, d, "project_status", map[string]any{})
	assert.Equal(t, []any{"o/r"}, status["targets"])
	assert.EqualValues(t, 5, status["concurrency"])
	toolCallContains(t, d, "project_add", map[string]any{"target": "not a target!", "runtime": true}, "target must be OWNER/REPO")
	callJSON(t, d, "project_remove", map[string]any{"target": "o/r", "runtime": true})
	assert.Equal(t, []any{}, callJSON(t, d, "project_status", map[string]any{})["targets"])
	toolCallContains(t, d, "project_remove", map[string]any{"target": "o/r", "runtime": true}, "has no runtime watch")
	assert.Equal(t, before, readFile(t, path))
}

// TestProjectAddRuntimeModelOverride checks that a runtime watch's model
// settings are validated and replace the agent's models for its jobs.
func TestProjectAddRuntimeModelOverride(t *testing.T) {
	d, m := setupProjects(t)
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, exeName("claude")), []byte("#!/bin/sh\n"), 0o755))
	t.Setenv("PATH", bin)
	m.Reconcile(&config.Config{Models: config.ModelsConfig{Defaults: &config.ModelDefaults{Model: "cli/codex/gpt-5.6-terra"}}})
	toolCallContains(t, d, "project_add", map[string]any{"target": "o/r", "runtime": true, "settings": map[string]any{"model": "nosuch/m"}}, `unknown provider "nosuch"`)
	toolCallContains(t, d, "project_add", map[string]any{"target": "o/r", "runtime": true, "settings": map[string]any{"model": "claude/opus", "fallbacks": []string{"codex/gpt-5.6"}}}, "the codex CLI is not installed")
	callJSON(t, d, "project_add", map[string]any{"target": "o/r", "runtime": true, "settings": map[string]any{"model": "claude:opus"}})
	status := callJSON(t, d, "project_status", map[string]any{})
	assert.Equal(t, map[string]any{"o/r": "claude/opus (project override)"}, status["model_labels"])
	assert.Equal(t, map[string]any{"model": "claude/opus", "fallbacks": []any{}, "source": "project override"}, status["models"].(map[string]any)["o/r"])
}

func TestProjectStatusLogOffset(t *testing.T) {
	d, m := setupProjects(t)
	var logged []string
	m.SetLogSource(func(component string, offset, limit int) ([]string, int) {
		assert.Equal(t, "projects", component)
		lines := slices.Clone(logged[min(max(offset, 0), len(logged)):])
		if limit > 0 && len(lines) > limit {
			lines = lines[len(lines)-limit:]
		}
		return lines, len(logged)
	})
	logged = append(logged, "first", "second")
	status := callJSON(t, d, "project_status", map[string]any{})
	offset := status["log_offset"].(float64)
	assert.Equal(t, []any{"first", "second"}, status["recent_logs"])
	logged = append(logged, "third")
	next := callJSON(t, d, "project_status", map[string]any{"log_offset": offset})
	assert.Equal(t, []any{"third"}, next["recent_logs"])
	assert.Equal(t, offset+1, next["log_offset"])
}

func TestProjectGitHubToolsWithoutBrowserDelivery(t *testing.T) {
	d, m := setupProjects(t)
	toolCallContains(t, d, "project_github_signin", map[string]any{}, "browser delivery is not running")
	toolCallContains(t, d, "project_github_import", map[string]any{"profile": " "}, "profile is required")
	toolCallContains(t, d, "project_github_import", map[string]any{"profile": t.TempDir()}, "no saved GitHub sign-in")

	// Without a running engine the sign-in is imported straight into the
	// profile Aviary's browser settings name.
	profile := filepath.Join(t.TempDir(), "profile")
	m.SetBrowser(config.BrowserConfig{ProfileDir: profile})
	from := t.TempDir()
	saved := `[{"name":"user_session","value":"abc","domain":"github.com","path":"/"}]`
	require.NoError(t, os.WriteFile(filepath.Join(from, "aviary-session-cookies.json"), []byte(saved), 0o600))
	imported := callJSON(t, d, "project_github_import", map[string]any{"profile": from})
	assert.Equal(t, true, imported["imported"])
	assert.Equal(t, saved, readFile(t, filepath.Join(profile, "aviary-session-cookies.json")))
}

func TestProjectGitHubUsePollingSavesDelivery(t *testing.T) {
	d, _ := setupProjects(t)
	require.NoError(t, config.Save("", &config.Config{Agents: []config.AgentConfig{{Name: "coder", Model: "cli/claude/opus"}}}))
	callJSON(t, d, "project_add", map[string]any{"target": "o/r", "agent": "coder"})
	result := callJSON(t, d, "project_github_use_polling", map[string]any{})
	assert.Equal(t, "poll", result["delivery"])
	cfg, err := config.Load("")
	require.NoError(t, err)
	assert.Equal(t, "poll", cfg.Projects.Delivery)
	status := callJSON(t, d, "project_status", map[string]any{})
	assert.Equal(t, "poll", status["delivery"])
	assert.Equal(t, "30s", status["poll_interval"])
	assert.NotContains(t, status, "github", "gh polling has no browser session to report")
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return strings.TrimSpace(string(data))
}

func TestProjectAddWithReports(t *testing.T) {
	d, _ := setupProjects(t)
	require.NoError(t, config.Save("", &config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "m", Channels: []config.ChannelConfig{{Type: "slack", ID: "main", Name: "Main"}}}}}))
	reports := map[string]any{"agent": "bot", "targets": []any{"channel:Main"}, "events": []any{"merged", "failed"}}

	toolCallContains(t, d, "project_add", map[string]any{"target": "o/r", "reports": map[string]any{"agent": "ghost", "targets": []any{"channel:Main"}}}, `reports agent "ghost" is not configured`)
	toolCallContains(t, d, "project_add", map[string]any{"target": "o/r", "agent": "bot", "reports": map[string]any{"agent": "bot", "targets": []any{"channel:Nope"}}}, `agent "bot" has no channel named "Nope"`)
	toolCallContains(t, d, "project_add", map[string]any{"target": "o/r", "runtime": true, "reports": reports}, "reports apply only to a saved project")

	added := callJSON(t, d, "project_add", map[string]any{"target": "o/r", "agent": "bot", "reports": reports})
	assert.Equal(t, true, added["saved"])
	cfg, err := config.Load("")
	require.NoError(t, err)
	require.Len(t, cfg.Agents[0].Projects, 1)
	assert.Equal(t, &config.ProjectReportsConfig{Agent: "bot", Targets: []string{"channel:Main"}, Events: []string{"merged", "failed"}}, cfg.Agents[0].Projects[0].Reports)
}

func TestProjectsAgentContextOnlyForProjectsAgent(t *testing.T) {
	_, m := setupProjects(t)

	got := projectsAgentContext(context.Background(), config.ProjectsAgentName, "o/r")
	assert.Contains(t, got, "/aviary:"+m.Identity())
	assert.Contains(t, got, "opened this chat from the o/r board")
	assert.Empty(t, projectsAgentContext(context.Background(), "other", "o/r"))

	SetDeps(&Deps{})
	assert.Empty(t, projectsAgentContext(context.Background(), config.ProjectsAgentName, "o/r"))
}

// exeName is name as an executable on PATH: Windows needs the .exe extension.
func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// fakeIssueSearch answers the dry-run issue search with count matches and
// records each query.
func fakeIssueSearch(m *projects.Manager, count int) *[]string {
	var queries []string
	m.SetGHRunCommand(func(_ context.Context, args ...string) ([]byte, error) {
		for _, arg := range args {
			if q, ok := strings.CutPrefix(arg, "q="); ok {
				queries = append(queries, q)
			}
		}
		return json.Marshal(map[string]any{"total_count": count, "items": []any{}})
	})
	return &queries
}

func TestProjectUpdateSetsAndResetsAProjectsOwnFilter(t *testing.T) {
	d, m := setupProjects(t)
	cfg := &config.Config{
		Agents:   []config.AgentConfig{{Name: "coder", Model: "cli/codex", Projects: config.AgentProjectsConfig{{Target: "o/a"}, {Target: "o/b"}}}},
		Projects: config.ProjectsConfig{Filter: []string{"label:shared"}},
	}
	require.NoError(t, config.Save("", cfg))
	m.Reconcile(cfg)
	queries := fakeIssueSearch(m, 12)
	var events []projects.ProjectEvent
	m.SetProjectObserver(func(ev projects.ProjectEvent) { events = append(events, ev) })

	checked := callJSON(t, d, "project_filter_check", map[string]any{"target": "o/a", "filter": "label:bug  -label:wip"})
	assert.EqualValues(t, 12, checked["matches"])
	assert.Equal(t, "label:bug -label:wip", checked["filter"])

	updated := callJSON(t, d, "project_update", map[string]any{"target": "o/a", "filter": "label:bug -label:wip"})
	assert.Equal(t, true, updated["saved"])
	assert.Equal(t, true, updated["custom"])
	assert.EqualValues(t, 12, updated["matches"])
	assert.Equal(t, "label:bug -label:wip", updated["issue_filter"])
	assert.Contains(t, *queries, "repo:o/a is:issue state:open label:bug -label:wip")
	assert.Contains(t, events, projects.ProjectEvent{Type: projects.ProjectUpdated, Target: "o/a", Agent: "coder"}, "open clients hear of the change")

	saved, err := config.Load("")
	require.NoError(t, err)
	assert.Equal(t, []string{"label:bug -label:wip"}, saved.Agents[0].Projects[0].Filter)
	assert.Empty(t, saved.Agents[0].Projects[1].Filter, "other projects keep the shared filter")
	assert.Equal(t, []string{"label:shared"}, saved.Projects.Filter, "the shared filter is unchanged")
	status := callJSON(t, d, "project_status", map[string]any{})
	assert.Equal(t, map[string]any{"o/a": "label:bug -label:wip", "o/b": "label:shared"}, status["issue_filters"])
	assert.Equal(t, []any{"o/a"}, status["custom_filters"])

	reset := callJSON(t, d, "project_update", map[string]any{"target": "o/a", "reset_filter": true})
	assert.Equal(t, false, reset["custom"])
	assert.Equal(t, "label:shared", reset["issue_filter"])
	saved, err = config.Load("")
	require.NoError(t, err)
	assert.Empty(t, saved.Agents[0].Projects[0].Filter)
	status = callJSON(t, d, "project_status", map[string]any{})
	assert.Equal(t, []any{}, status["custom_filters"])
}

func TestProjectUpdateRejectsAnInvalidFilter(t *testing.T) {
	d, m := setupProjects(t)
	cfg := &config.Config{Agents: []config.AgentConfig{{Name: "coder", Model: "cli/codex", Projects: config.AgentProjectsConfig{{Target: "o/a"}}}}}
	require.NoError(t, config.Save("", cfg))
	m.Reconcile(cfg)
	before := readFile(t, config.DefaultPath())
	fakeIssueSearch(m, 0)
	toolCallContains(t, d, "project_update", map[string]any{"target": "o/a", "filter": "label:"}, "invalid filter: label: needs a value")
	toolCallContains(t, d, "project_update", map[string]any{"target": "o/a", "filter": `label:"bug`}, "unbalanced quote")
	toolCallContains(t, d, "project_filter_check", map[string]any{"target": "o/a", "filter": "(label:bug"}, "unbalanced parenthesis")
	toolCallContains(t, d, "project_update", map[string]any{"target": "o/a"}, "nothing to change")
	toolCallContains(t, d, "project_update", map[string]any{"target": "o/a", "filter": "label:x", "reset_filter": true}, "not both")
	toolCallContains(t, d, "project_update", map[string]any{"target": "o/none", "filter": "label:x"}, "not watched")
	assert.Equal(t, before, readFile(t, config.DefaultPath()), "an invalid filter is never saved")
}

func TestProjectUpdateRuntimeWatchKeepsConfig(t *testing.T) {
	d, m := setupProjects(t)
	m.Reconcile(&config.Config{Models: config.ModelsConfig{Defaults: &config.ModelDefaults{Model: "cli/codex/gpt-5.6-terra"}}})
	before := readFile(t, config.DefaultPath())
	fakeIssueSearch(m, 3)
	callJSON(t, d, "project_add", map[string]any{"target": "o/r", "runtime": true})
	callJSON(t, d, "project_add", map[string]any{"target": "o/s", "runtime": true})
	updated := callJSON(t, d, "project_update", map[string]any{"target": "o/r", "filter": "label:bug"})
	assert.Equal(t, true, updated["runtime"])
	assert.Equal(t, before, readFile(t, config.DefaultPath()))
	status := callJSON(t, d, "project_status", map[string]any{})
	assert.Equal(t, map[string]any{"o/r": "label:bug", "o/s": "assignee:@me"}, status["issue_filters"])
}
