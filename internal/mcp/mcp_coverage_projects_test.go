package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
	"project_job_retry", "project_job_pause", "project_job_resume", "project_job_stop",
	"project_refresh", "project_add", "project_remove", "project_cli_list",
	"project_clone_list", "project_clone_clear", "project_unblock_run",
	"project_issue_comment", "project_issue_create",
	"project_github_signin", "project_github_use_polling", "project_github_import",
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
		"project_job_pause": job, "project_job_resume": job, "project_job_stop": job,
		"project_add": {"target": "o/r"}, "project_remove": {"target": "o/r"},
		"project_issue_comment": {"number": 1, "text": "x"}, "project_issue_create": {"text": "x"},
		"project_github_import": {"profile": "/p"},
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
	for _, name := range []string{"project_status", "project_job_list", "project_job_get", "project_job_explain", "project_cli_list", "project_clone_list"} {
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
	assert.Equal(t, "ready", status["ready_label"])
	assert.EqualValues(t, 3, status["concurrency"])

	jobs := callJSON(t, d, "project_job_list", map[string]any{"status": "active"})
	assert.Equal(t, []any{}, jobs["jobs"])
	assert.EqualValues(t, 0, jobs["total"])

	toolCallContains(t, d, "project_job_get", map[string]any{"number": 7}, "no job #7")
	toolCallContains(t, d, "project_job_explain", map[string]any{"number": 7, "target": "o/r"}, "no job o/r#7")
	toolCallContains(t, d, "project_job_get", map[string]any{"number": 0}, "job number is required")
	for _, tool := range []string{"project_job_retry", "project_job_pause", "project_job_resume", "project_job_stop", "project_unblock_run"} {
		toolCallContains(t, d, tool, map[string]any{"number": 7}, "no projects are being watched")
	}
	for _, tool := range []string{"project_refresh", "project_clone_list", "project_clone_clear"} {
		toolCallContains(t, d, tool, map[string]any{}, "no projects are being watched")
	}
	toolCallContains(t, d, "project_issue_comment", map[string]any{"number": 7, "text": "hi"}, "no projects are being watched")
	toolCallContains(t, d, "project_issue_create", map[string]any{"text": "hi"}, "no projects are being watched")
}

func TestProjectAddAndRemoveSavedProject(t *testing.T) {
	d, _ := setupProjects(t)
	added := callJSON(t, d, "project_add", map[string]any{"target": "o/r", "agents": []any{"claude/opus", "codex"}})
	assert.Equal(t, true, added["saved"])
	cfg, err := config.Load("")
	require.NoError(t, err)
	require.Len(t, cfg.Projects.List, 1)
	assert.Equal(t, config.ProjectConfig{Target: "o/r", Agents: []string{"claude/opus", "codex"}}, cfg.Projects.List[0])
	// Only the project was written: every setting is still its default.
	cfg.Projects.List = nil
	assert.True(t, cfg.Projects.IsZero())

	status := callJSON(t, d, "project_status", map[string]any{})
	assert.Equal(t, []any{"o/r"}, status["targets"])
	toolCallContains(t, d, "project_add", map[string]any{"target": "o/r"}, "already watched")
	toolCallContains(t, d, "project_add", map[string]any{"target": " "}, "target is required")
	toolCallContains(t, d, "project_add", map[string]any{"target": "o/s", "settings": map[string]any{"no_merge": true}}, "only to a runtime watch")

	removed := callJSON(t, d, "project_remove", map[string]any{"target": "o/r"})
	assert.Equal(t, true, removed["removed"])
	cfg, err = config.Load("")
	require.NoError(t, err)
	assert.Empty(t, cfg.Projects.List)
	toolCallContains(t, d, "project_remove", map[string]any{"target": "o/r"}, "not watched")
}

// TestProjectAddRuntimeWritesNoConfig checks the zero-config path aviary watch
// takes: a runtime watch is watched without touching aviary.yaml.
func TestProjectAddRuntimeWritesNoConfig(t *testing.T) {
	d, _ := setupProjects(t)
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

func TestProjectCLIListReportsEveryDefinition(t *testing.T) {
	d, _ := setupProjects(t)
	t.Setenv("PATH", t.TempDir())
	result := callJSON(t, d, "project_cli_list", map[string]any{})
	clis, ok := result["clis"].([]any)
	require.True(t, ok, result)
	var names []string
	for _, raw := range clis {
		cli := raw.(map[string]any)
		names = append(names, cli["name"].(string))
		assert.Equal(t, false, cli["installed"], cli["name"])
	}
	for _, want := range []string{"claude", "codex", "gemini"} {
		assert.Contains(t, names, want)
	}
}

func TestProjectStatusLogOffset(t *testing.T) {
	d, m := setupProjects(t)
	m.Log("first")
	m.Log("second")
	status := callJSON(t, d, "project_status", map[string]any{})
	offset := status["log_offset"].(float64)
	assert.Equal(t, []any{"first", "second"}, status["recent_logs"])
	m.Log("third")
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
	require.NoError(t, config.Save("", &config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "m"}}}))
	reports := map[string]any{"agent": "bot", "targets": []any{"slack:main:C1"}, "events": []any{"merged", "failed"}}

	toolCallContains(t, d, "project_add", map[string]any{"target": "o/r", "reports": map[string]any{"agent": "ghost", "targets": []any{"slack:main:C1"}}}, `reports agent "ghost" is not configured`)
	toolCallContains(t, d, "project_add", map[string]any{"target": "o/r", "runtime": true, "reports": reports}, "reports apply only to a saved project")

	added := callJSON(t, d, "project_add", map[string]any{"target": "o/r", "reports": reports})
	assert.Equal(t, true, added["saved"])
	cfg, err := config.Load("")
	require.NoError(t, err)
	require.Len(t, cfg.Projects.List, 1)
	assert.Equal(t, &config.ProjectReportsConfig{Agent: "bot", Targets: []string{"slack:main:C1"}, Events: []string{"merged", "failed"}}, cfg.Projects.List[0].Reports)
}
