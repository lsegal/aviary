package mcp

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/projects"
)

// ── Channel tools ─────────────────────────────────────────────────────────────

func cfpLoadConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load("")
	require.NoError(t, err)
	return cfg
}

func cfpAgent(t *testing.T, cfg *config.Config, name string) config.AgentConfig {
	t.Helper()
	ai := agentIndex(cfg, name)
	require.GreaterOrEqual(t, ai, 0, "agent %q", name)
	return cfg.Agents[ai]
}

func cfpTask(a config.AgentConfig, name string) (config.TaskConfig, bool) {
	for _, tc := range a.Tasks {
		if tc.Name == name {
			return tc, true
		}
	}
	return config.TaskConfig{}, false
}

func cfpProjectReports(a config.AgentConfig, target string) *config.ProjectReportsConfig {
	for _, p := range a.Projects {
		if p.Target == target {
			return p.Reports
		}
	}
	return nil
}

func TestCFPCovChannelCreateValidatesAndDerivesUniqueIDs(t *testing.T) {
	d, _ := setupDispatcherWithScheduler(t)

	toolCallContains(t, d, "channel_create", map[string]any{"agent": "bot", "name": "  ", "type": "slack"}, "name is required")
	toolCallContains(t, d, "channel_create", map[string]any{"agent": " ", "name": "X", "type": "slack"}, "agent is required")
	toolCallContains(t, d, "channel_create", map[string]any{"agent": "ghost", "name": "X", "type": "slack"}, `agent "ghost" is not configured`)

	// A signal channel starts with its conversational behaviors on.
	callJSON(t, d, "channel_create", map[string]any{"agent": "BOT", "name": "Family", "type": "signal"})
	// Names that slug to an ID already taken get a numeric suffix, and a name
	// with no letters or digits falls back to the channel type.
	callJSON(t, d, "channel_create", map[string]any{"agent": "bot", "name": "Team Slack", "type": "slack"})
	callJSON(t, d, "channel_create", map[string]any{"agent": "bot", "name": "team-slack!", "type": "slack"})
	callJSON(t, d, "channel_create", map[string]any{"agent": "bot", "name": "!!!", "type": "discord"})

	a := cfpAgent(t, cfpLoadConfig(t), "bot")
	require.Len(t, a.Channels, 4)
	signal := a.Channels[a.ChannelByName("Family")]
	assert.Equal(t, "signal", signal.Type)
	assert.Equal(t, "family", signal.ID)
	for _, on := range []*bool{signal.ShowTyping, signal.ReplyToReplies, signal.ReactToEmoji, signal.SendReadReceipts} {
		assert.True(t, config.BoolOr(on, false))
	}
	assert.Equal(t, "team-slack", a.Channels[a.ChannelByName("Team Slack")].ID)
	assert.Equal(t, "team-slack-2", a.Channels[a.ChannelByName("team-slack!")].ID)
	assert.Equal(t, "discord", a.Channels[a.ChannelByName("!!!")].ID)

	// Rename and remove refuse missing names and collisions.
	toolCallContains(t, d, "channel_rename", map[string]any{"agent": "bot", "channel": "Family", "new_name": " "}, "new_name is required")
	toolCallContains(t, d, "channel_rename", map[string]any{"agent": "bot", "channel": "Nope", "new_name": "X"}, `agent "bot" has no channel named "Nope"`)
	toolCallContains(t, d, "channel_rename", map[string]any{"agent": "bot", "channel": "Family", "new_name": "team slack"}, `already has a channel named "team slack"`)
	toolCallContains(t, d, "channel_remove", map[string]any{"agent": "bot", "channel": "Nope", "delete_data": true}, "no channel named")
	toolCallContains(t, d, "channel_sessions", map[string]any{"agent": "ghost", "channel": "Family"}, "is not configured")

	// Renaming a channel to a different casing of its own name is allowed.
	out := callText(t, d, "channel_rename", map[string]any{"agent": "bot", "channel": "family", "new_name": "FAMILY"})
	assert.Contains(t, out, `channel "Family" renamed to "FAMILY"`)

	// delete_data removes the channel's own session.
	own := callList(t, d, "channel_sessions", map[string]any{"agent": "bot", "channel": "!!!"})
	require.Len(t, own, 1)
	out = callText(t, d, "channel_remove", map[string]any{"agent": "bot", "channel": "!!!", "delete_data": true})
	assert.Contains(t, out, `channel "!!!" removed; its sessions were deleted`)
	a = cfpAgent(t, cfpLoadConfig(t), "bot")
	assert.Equal(t, -1, a.ChannelByName("!!!"))
	for _, s := range callList(t, d, "session_list", map[string]any{"agent": "bot"}) {
		assert.NotEqual(t, own[0]["id"], s["id"])
	}
}

func TestCFPCovChannelSessionsReportLastMessageAndBadChannels(t *testing.T) {
	d, _ := setupDispatcherWithScheduler(t)
	created := callJSON(t, d, "channel_create", map[string]any{"agent": "bot", "name": "Ops", "type": "slack"})
	sessionID := created["session_id"].(string)

	before := callList(t, d, "channel_sessions", map[string]any{"agent": "bot", "channel": "Ops"})
	require.Len(t, before, 1)
	assert.NotContains(t, before[0], "last_message_at")

	require.NoError(t, agent.AppendMessageToSession("bot", sessionID, domain.MessageRoleUser, "hello"))
	after := callList(t, d, "channel_sessions", map[string]any{"agent": "bot", "channel": "Ops"})
	require.Len(t, after, 1)
	assert.NotEmpty(t, after[0]["last_message_at"])
	assert.Equal(t, false, after[0]["is_processing"])

	// A channel saved without an ID has no session to list.
	cfg := cfpLoadConfig(t)
	cfg.Agents[0].Channels = append(cfg.Agents[0].Channels, config.ChannelConfig{Type: "slack", Name: "Raw"})
	require.NoError(t, config.Save("", cfg))
	toolCallContains(t, d, "channel_sessions", map[string]any{"agent": "bot", "channel": "Raw"}, "channel needs an agent, a type and an id")
}

func TestCFPCovChannelCreateWithoutChannelManager(t *testing.T) {
	d, _ := setupDispatcherWithScheduler(t)
	SetDeps(nil)
	assert.Nil(t, channelManager())

	created := callJSON(t, d, "channel_create", map[string]any{"agent": "bot", "name": "Ops", "type": "discord"})
	assert.NotEmpty(t, created["session_id"])
	a := cfpAgent(t, cfpLoadConfig(t), "bot")
	require.Len(t, a.Channels, 1)
	assert.Equal(t, "discord", a.Channels[0].Type)
}

func cfpWriteTaskFile(t *testing.T, a config.AgentConfig, file, content string) string {
	t.Helper()
	dir := config.AgentTasksDir(a)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, file)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestCFPCovChannelRenameAndRemoveRetargetWorkflowsAndReports(t *testing.T) {
	bot := config.AgentConfig{
		Name:     "bot",
		Model:    "test/x",
		Channels: []config.ChannelConfig{{Type: "slack", ID: "ops", Name: "Ops"}},
		Projects: []config.ProjectConfig{
			{Target: "acme/app", Reports: &config.ProjectReportsConfig{Agent: "bot", Targets: []string{"channel:Ops", "session:main"}}},
			{Target: "acme/web", Reports: &config.ProjectReportsConfig{Agent: "bot", Targets: []string{"channel:ops"}}},
			{Target: "acme/doc", Reports: &config.ProjectReportsConfig{Agent: "other", Targets: []string{"channel:Ops"}}},
		},
		Tasks: []config.TaskConfig{{Name: "greeter", Trigger: config.TriggerMessage, Channel: "Ops", Prompt: "Say hi"}},
	}
	other := config.AgentConfig{Name: "other", Model: "test/x", Channels: []config.ChannelConfig{{Type: "slack", ID: "ops", Name: "Ops"}}}
	setupSchedulerDeps(t, &config.Config{Agents: []config.AgentConfig{bot, other}})
	d := NewDispatcher("https://localhost:16677", "")

	relay := cfpWriteTaskFile(t, bot, "relay.md", "---\nname: relay\ntrigger: message\nchannel: Ops\n---\nRelay the message\n")
	legacy := cfpWriteTaskFile(t, bot, "legacy.md", "---\nname: Weekly Report\nschedule: \"0 9 * * 1\"\ntarget: channel:Ops\n---\nSummarize the week\n")
	quietContent := "---\nname: quiet\nschedule: \"0 8 * * *\"\ntarget: session:main\n---\nStay quiet\n"
	quiet := cfpWriteTaskFile(t, bot, "quiet.md", quietContent)

	out := callText(t, d, "channel_rename", map[string]any{"agent": "bot", "channel": "Ops", "new_name": "Alerts"})
	assert.Contains(t, out, `channel "Ops" renamed to "Alerts"`)

	cfg := cfpLoadConfig(t)
	a := cfpAgent(t, cfg, "bot")
	greeter, ok := cfpTask(a, "greeter")
	require.True(t, ok)
	assert.Equal(t, "Alerts", greeter.Channel, "an inline workflow follows its channel")
	relayTask, ok := cfpTask(a, "relay")
	require.True(t, ok)
	assert.True(t, relayTask.FromFile)
	assert.Equal(t, "Alerts", relayTask.Channel, "a file workflow is rewritten in its file")
	weekly, ok := cfpTask(a, "Weekly Report")
	require.True(t, ok)
	assert.Equal(t, "channel:Alerts", weekly.Target)
	// The rewritten task file takes its name's file name and the old one goes.
	_, err := os.Stat(legacy)
	assert.True(t, os.IsNotExist(err), "the old task file is removed")
	weeklyPath, err := config.FindMarkdownTask(config.AgentTasksDir(a), "Weekly Report")
	require.NoError(t, err)
	assert.NotEqual(t, legacy, weeklyPath)
	// An unaffected task file is left untouched.
	data, err := os.ReadFile(quiet)
	require.NoError(t, err)
	assert.Equal(t, quietContent, string(data))
	// File-backed tasks stay out of aviary.yaml.
	raw, err := os.ReadFile(config.DefaultPath())
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "Weekly Report")

	assert.Equal(t, []string{"channel:Alerts", "session:main"}, cfpProjectReports(a, "acme/app").Targets)
	assert.Equal(t, []string{"channel:Alerts"}, cfpProjectReports(a, "acme/web").Targets)
	assert.Equal(t, []string{"channel:Ops"}, cfpProjectReports(a, "acme/doc").Targets, "reports through another agent are not touched")
	otherAgent := cfpAgent(t, cfg, "other")
	assert.Equal(t, 0, otherAgent.ChannelByName("Ops"))

	out = callText(t, d, "channel_remove", map[string]any{"agent": "bot", "channel": "Alerts"})
	assert.Contains(t, out, "its sessions were archived")

	cfg = cfpLoadConfig(t)
	a = cfpAgent(t, cfg, "bot")
	assert.Empty(t, a.Channels)
	_, ok = cfpTask(a, "greeter")
	assert.False(t, ok, "an inline workflow goes with its channel")
	_, ok = cfpTask(a, "relay")
	assert.False(t, ok, "a file workflow goes with its channel")
	_, err = os.Stat(relay)
	assert.True(t, os.IsNotExist(err), "its task file is deleted")
	weekly, ok = cfpTask(a, "Weekly Report")
	require.True(t, ok)
	assert.Empty(t, weekly.Target, "a delivery target naming it is cleared")
	_, ok = cfpTask(a, "quiet")
	assert.True(t, ok)
	assert.Equal(t, []string{"session:main"}, cfpProjectReports(a, "acme/app").Targets)
	assert.Nil(t, cfpProjectReports(a, "acme/web"), "reports with no target left are dropped")
	assert.Equal(t, []string{"channel:Ops"}, cfpProjectReports(a, "acme/doc").Targets)
}

func TestCFPCovRemoveWorkflowFileOnlyDeletesFileTasks(t *testing.T) {
	setupDispatcherWithScheduler(t)
	a := cfpAgent(t, cfpLoadConfig(t), "bot")
	path := cfpWriteTaskFile(t, a, "flow.md", "---\nname: flow\ntrigger: message\nchannel: Ops\n---\nDo it\n")

	// An inline task has no file, so a same-named file is left alone.
	removeWorkflowFile(a, config.TaskConfig{Name: "flow"})
	_, err := os.Stat(path)
	require.NoError(t, err)

	// A file task whose file is already gone is a no-op.
	removeWorkflowFile(a, config.TaskConfig{Name: "missing", FromFile: true})
	_, err = os.Stat(path)
	require.NoError(t, err)

	removeWorkflowFile(a, config.TaskConfig{Name: "flow", FromFile: true})
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err))
}

// ── File tools ────────────────────────────────────────────────────────────────

func TestCFPCovFileToolsReportFilesystemErrors(t *testing.T) {
	d, ctx, workspace := setupMCPWithFilesystemAgent(t, []string{"./sandbox/**"})
	sandbox := filepath.Join(workspace, "sandbox")
	require.NoError(t, os.MkdirAll(filepath.Join(sandbox, "dir"), 0o755))
	writeTestFile(t, filepath.Join(sandbox, "plain.txt"), "plain")

	call := func(tool string, args map[string]any) string {
		t.Helper()
		out, err := d.CallTool(ctx, tool, args)
		require.NoError(t, err)
		return out
	}

	assert.Contains(t, call("file_read", map[string]any{"path": "./sandbox/missing.txt"}), "reading file metadata")
	assert.Contains(t, call("file_read", map[string]any{"path": "./sandbox/dir"}), "path is a directory")
	assert.Contains(t, call("file_delete", map[string]any{"path": "  "}), "path is required")

	assert.Contains(t, call("file_write", map[string]any{"path": "./sandbox/x.bin", "content": "!!", "encoding": "base64"}), "decoding base64 content")
	assert.Contains(t, call("file_write", map[string]any{"path": "./sandbox/dir", "content": "x"}), "path is a directory")

	assert.Contains(t, call("file_append", map[string]any{"path": "../escape.txt", "content": "x"}), "append refused by Aviary")
	assert.Contains(t, call("file_append", map[string]any{"path": "./sandbox/a.txt", "content": "x", "encoding": "latin1"}), `unsupported encoding "latin1"`)
	assert.Contains(t, call("file_append", map[string]any{"path": "./sandbox/plain.txt/child.txt", "content": "x"}), "creating parent directories")
	assert.Contains(t, call("file_append", map[string]any{"path": "./sandbox/dir", "content": "x"}), "opening file for append")

	assert.Contains(t, call("file_truncate", map[string]any{"path": "./sandbox/plain.txt", "size": -1}), "size must be >= 0")
	assert.Contains(t, call("file_truncate", map[string]any{"path": "../escape.txt", "size": 1}), "truncate refused by Aviary")
	assert.Contains(t, call("file_truncate", map[string]any{"path": "./sandbox/plain.txt/child.txt", "size": 1}), "creating parent directories")
	assert.Contains(t, call("file_truncate", map[string]any{"path": "./sandbox/dir", "size": 1}), "opening file for truncate")

	assert.Contains(t, call("file_delete", map[string]any{"path": "../escape.txt"}), "delete refused by Aviary")
	assert.Contains(t, call("file_delete", map[string]any{"path": "./sandbox/missing.txt"}), "deleting file")

	assert.Contains(t, call("file_copy", map[string]any{"source": "../escape.txt", "destination": "./sandbox/c.txt"}), "copy source refused by Aviary")
	assert.Contains(t, call("file_copy", map[string]any{"source": "./sandbox/plain.txt", "destination": "../escape.txt"}), "copy destination refused by Aviary")
	assert.Contains(t, call("file_copy", map[string]any{"source": "./sandbox/missing.txt", "destination": "./sandbox/c.txt"}), "reading source metadata")
	assert.Contains(t, call("file_copy", map[string]any{"source": "./sandbox/dir", "destination": "./sandbox/c.txt"}), "source is a directory")
	assert.Contains(t, call("file_copy", map[string]any{"source": "./sandbox/plain.txt", "destination": "./sandbox/plain.txt/c.txt"}), "creating destination directories")
	assert.Contains(t, call("file_copy", map[string]any{"source": "./sandbox/plain.txt", "destination": "./sandbox/dir"}), "writing destination file")

	assert.Contains(t, call("file_move", map[string]any{"source": "../escape.txt", "destination": "./sandbox/m.txt"}), "move source refused by Aviary")
	assert.Contains(t, call("file_move", map[string]any{"source": "./sandbox/plain.txt", "destination": "../escape.txt"}), "move destination refused by Aviary")
	assert.Contains(t, call("file_move", map[string]any{"source": "./sandbox/plain.txt", "destination": "./sandbox/plain.txt/m.txt"}), "creating destination directories")
	assert.Contains(t, call("file_move", map[string]any{"source": "./sandbox/missing.txt", "destination": "./sandbox/m.txt"}), "moving file")

	// None of the failed calls changed the existing file.
	data, err := os.ReadFile(filepath.Join(sandbox, "plain.txt"))
	require.NoError(t, err)
	assert.Equal(t, "plain", string(data))
}

func TestCFPCovFileToolsEncodingsAndLongLines(t *testing.T) {
	d, ctx, workspace := setupMCPWithFilesystemAgent(t, []string{"./sandbox/**"})
	sandbox := filepath.Join(workspace, "sandbox")

	// base64 content is decoded before it is written.
	payload := []byte{0x00, 0xff, 0x10, 'h', 'i'}
	out, err := d.CallTool(ctx, "file_write", map[string]any{
		"path": "./sandbox/blob.bin", "content": base64.StdEncoding.EncodeToString(payload), "encoding": " Base64 ",
	})
	require.NoError(t, err)
	res := decodeToolJSON(t, out)
	assert.Equal(t, true, res["created"])
	assert.Equal(t, float64(len(payload)), res["bytes"])
	assert.NotContains(t, res, "diff", "binary content has no text diff")
	data, err := os.ReadFile(filepath.Join(sandbox, "blob.bin"))
	require.NoError(t, err)
	assert.Equal(t, payload, data)

	// A small binary file reads back whole as base64.
	out, err = d.CallTool(ctx, "file_read", map[string]any{"path": "./sandbox/blob.bin"})
	require.NoError(t, err)
	res = decodeToolJSON(t, out)
	assert.Equal(t, "base64", res["encoding"])
	assert.Equal(t, base64.StdEncoding.EncodeToString(payload), res["content"])

	// "text" is an alias of utf-8.
	_, err = d.CallTool(ctx, "file_append", map[string]any{"path": "./sandbox/notes.txt", "content": "one", "encoding": "text"})
	require.NoError(t, err)
	data, err = os.ReadFile(filepath.Join(sandbox, "notes.txt"))
	require.NoError(t, err)
	assert.Equal(t, "one", string(data))

	// Ranged reads cut overlong lines.
	writeTestFile(t, filepath.Join(sandbox, "wide.txt"), strings.Repeat("é", maxReadLineChars+5)+"\nshort\n")
	out, err = d.CallTool(ctx, "file_read", map[string]any{"path": "./sandbox/wide.txt", "offset": 1})
	require.NoError(t, err)
	res = decodeToolJSON(t, out)
	content := res["content"].(string)
	first, rest, _ := strings.Cut(content, "\n")
	assert.Equal(t, "     1\t"+strings.Repeat("é", maxReadLineChars)+" ... [line truncated]", first)
	assert.Equal(t, "     2\tshort\n", rest)
	assert.NotContains(t, res, "truncated")
}

func TestCFPCovFileToolsResolveAgentAndWorkingDir(t *testing.T) {
	d, ctx, _ := setupMCPWithFilesystemAgent(t, []string{"./sandbox/**"})

	ghost := agent.WithSessionAgentID(context.Background(), "ghost")
	out, err := d.CallTool(ghost, "file_read", map[string]any{"path": "./sandbox/x.txt"})
	require.NoError(t, err)
	assert.Contains(t, out, `agent "ghost" not found`)

	prev := GetDeps()
	SetDeps(&Deps{})
	out, err = d.CallTool(ctx, "file_read", map[string]any{"path": "./sandbox/x.txt"})
	SetDeps(prev)
	require.NoError(t, err)
	assert.Contains(t, out, "agent manager not initialized")

	// An agent with a working directory resolves relative paths against it.
	workDir := t.TempDir()
	mgr := agent.NewManager(nil)
	mgr.Reconcile(&config.Config{Agents: []config.AgentConfig{{
		Name:        "bot",
		Model:       "test/x",
		WorkingDir:  workDir,
		Permissions: &config.PermissionsConfig{Filesystem: &config.FilesystemPermissionsConfig{AllowedPaths: []string{"./**"}}},
	}}})
	SetDeps(&Deps{Agents: mgr})
	out, err = d.CallTool(ctx, "file_write", map[string]any{"path": "notes/today.txt", "content": "done"})
	require.NoError(t, err)
	want := filepath.Join(workDir, "notes", "today.txt")
	data, err := os.ReadFile(want)
	require.NoError(t, err, out)
	assert.Equal(t, "done", string(data))
	resolved, err := filepath.EvalSymlinks(decodeToolJSON(t, out)["path"].(string))
	require.NoError(t, err)
	wantResolved, err := filepath.EvalSymlinks(want)
	require.NoError(t, err)
	assert.Equal(t, wantResolved, resolved)
}

// ── Project task tools ────────────────────────────────────────────────────────

// cfpSetupWatchedProjects is setupProjectTasks with bot also watching
// acme/web, other watching other/lib, and a runtime watch of loose/repo that
// no agent owns.
func cfpSetupWatchedProjects(t *testing.T) (*Dispatcher, *projects.Manager) {
	t.Helper()
	d, s := setupProjectTasks(t)
	cfg := cfpLoadConfig(t)
	cfg.Agents[0].Projects = append(cfg.Agents[0].Projects, config.ProjectConfig{Target: "acme/web"})
	cfg.Agents[1].Projects = []config.ProjectConfig{{Target: "other/lib"}}
	// A runtime watch no agent owns runs on models.defaults, which needs a CLI model.
	cfg.Models.Defaults = &config.ModelDefaults{Model: "cli/codex"}
	require.NoError(t, config.Save("", cfg))
	cfg = cfpLoadConfig(t)
	deps := GetDeps()
	deps.Projects.Reconcile(cfg)
	deps.Agents.Reconcile(cfg)
	s.Reconcile(cfg)
	require.NoError(t, deps.Projects.AddWatch(projects.Watch{Targets: []string{"loose/repo"}}))
	return d, deps.Projects
}

func cfpSchedule(t *testing.T, d *Dispatcher, agentName, name, project string) {
	t.Helper()
	_, err := d.CallTool(context.Background(), "task_schedule", map[string]any{
		"agent": agentName, "name": name, "type": "project", "project": project,
		"content": "Look after " + project, "every": "3h", "trigger": "interval", "precompile": false,
	})
	require.NoError(t, err)
}

func TestCFPCovProjectTaskLookupAcrossWatchedProjects(t *testing.T) {
	d, m := cfpSetupWatchedProjects(t)
	watched := m.WatchedProjects()
	assert.Equal(t, "bot", watched["acme/web"])
	assert.Equal(t, "other", watched["other/lib"])
	assert.Contains(t, watched, "loose/repo")
	assert.Empty(t, watched["loose/repo"])

	cfpSchedule(t, d, "other", "sweep", "other/lib")
	cfpSchedule(t, d, "bot", "sweep", "acme/app")
	cfpSchedule(t, d, "bot", "audit", "acme/web")
	cfpSchedule(t, d, "bot", "alpha", "acme/web")

	// Listed by project, then by id.
	list := callJSON(t, d, "project_task_list", map[string]any{})
	require.EqualValues(t, 4, list["total"])
	var ids []string
	for _, e := range list["tasks"].([]any) {
		ids = append(ids, e.(map[string]any)["id"].(string))
	}
	assert.Equal(t, []string{"bot/sweep", "bot/alpha", "bot/audit", "other/sweep"}, ids)
	assert.EqualValues(t, 2, callJSON(t, d, "project_task_list", map[string]any{"project": "acme/web"})["total"])

	ctx := context.Background()
	ambiguous := toolErrorText(d.CallTool(ctx, "project_task_get", map[string]any{"name": "sweep"}))
	assert.Contains(t, ambiguous, `several watched projects have a task "sweep"`)
	assert.Contains(t, ambiguous, "bot/sweep (acme/app)")
	assert.Contains(t, ambiguous, "other/sweep (other/lib)")
	assert.Equal(t, "other", callJSON(t, d, "project_task_get", map[string]any{"name": "sweep", "project": "other/lib"})["agent"])
	assert.Equal(t, "acme/app", callJSON(t, d, "project_task_get", map[string]any{"name": "bot/sweep"})["project"])
	assert.Contains(t, toolErrorText(d.CallTool(ctx, "project_task_get", map[string]any{"name": "  "})), "name is required")
	assert.Contains(t, toolErrorText(d.CallTool(ctx, "project_task_get", map[string]any{"name": "sweep", "project": "nope/x"})), "nope/x is not a project this instance watches")

	for _, tool := range []string{"project_task_pause", "project_task_run", "project_task_delete", "project_task_update"} {
		assert.Contains(t, toolErrorText(d.CallTool(ctx, tool, map[string]any{"name": "ghost"})), `no scheduled task "ghost"`, tool)
	}

	// A task moves only between projects of its own agent.
	assert.Contains(t, toolErrorText(d.CallTool(ctx, "project_task_update", map[string]any{"name": "bot/sweep", "new_project": "other/lib"})),
		`other/lib belongs to agent "other", not "bot"`)
	moved := callJSON(t, d, "project_task_update", map[string]any{"name": "bot/sweep", "new_project": "acme/web"})
	assert.Equal(t, "acme/web", moved["task"].(map[string]any)["project"])

	// A rename onto a name the agent already uses fails after the rest saved.
	renamed := toolErrorText(d.CallTool(ctx, "project_task_update", map[string]any{"name": "bot/sweep", "content": "Sweep harder", "new_name": "audit"}))
	assert.Contains(t, renamed, "the other changes were saved, but renaming failed")
	got := callJSON(t, d, "project_task_get", map[string]any{"name": "bot/sweep"})
	assert.Equal(t, "Sweep harder", got["prompt"])
}

func TestCFPCovProjectTaskCreateValidation(t *testing.T) {
	d, _ := cfpSetupWatchedProjects(t)
	ctx := context.Background()
	create := func(args map[string]any) string {
		t.Helper()
		return toolErrorText(d.CallTool(ctx, "project_task_create", args))
	}

	assert.Contains(t, create(map[string]any{"project": "loose/repo", "content": "x", "trigger": "interval", "every": "1h"}), "runtime watch no agent owns")
	assert.Contains(t, create(map[string]any{"project": " ", "content": "x", "trigger": "interval", "every": "1h"}), "project is required")
	assert.Contains(t, create(map[string]any{"project": "acme/app", "content": "  ", "trigger": "interval", "every": "1h"}), "content is required")
	assert.Contains(t, create(map[string]any{"project": "acme/app", "content": "x"}), "project tasks need a schedule trigger")
	assert.Contains(t, create(map[string]any{"project": "acme/app", "content": "x", "schedule": "not a cron"}), "invalid schedule")

	// Without a name the task gets a generated one, found as the new task.
	cfpSchedule(t, d, "bot", "existing", "acme/app")
	created := callJSON(t, d, "project_task_create", map[string]any{
		"project": "acme/app", "content": "Close stale pull requests", "schedule": "0 6 * * *", "precompile": false,
	})
	assert.Equal(t, true, created["saved"])
	task := created["task"].(map[string]any)
	assert.Equal(t, "acme/app", task["project"])
	assert.NotEqual(t, "bot/existing", task["id"])
	assert.NotEmpty(t, task["name"])
	list := callJSON(t, d, "project_task_list", map[string]any{"project": "acme/app"})
	assert.EqualValues(t, 2, list["total"])

	// When the analysis fails, the estimate says why instead of failing.
	prev := taskAnalyzerFunc
	taskAnalyzerFunc = func(context.Context, string, string, string) (*taskAnalyzerResult, error) {
		return nil, errors.New("analyzer offline")
	}
	t.Cleanup(func() { taskAnalyzerFunc = prev })
	dry := callJSON(t, d, "project_task_create", map[string]any{
		"project": "acme/web", "name": "dry", "content": "Something new to analyze", "trigger": "interval", "every": "6h", "dry_run": true,
	})
	estimate := dry["estimate"].(map[string]any)
	assert.Equal(t, false, estimate["available"])
	assert.Contains(t, estimate["reason"], "analyzer offline")
}

func TestCFPCovProjectTaskToolsWithoutSchedulerOrProjects(t *testing.T) {
	d, m := cfpSetupWatchedProjects(t)
	ctx := context.Background()
	cfpSchedule(t, d, "bot", "triage", "acme/app")

	// A task in aviary.yaml the scheduler has not loaded cannot be run yet.
	cfg := cfpLoadConfig(t)
	cfg.Agents[0].Tasks = append(cfg.Agents[0].Tasks, config.TaskConfig{
		Name: "fresh", Type: "project", Project: "acme/app", Prompt: "x", Trigger: "interval", Every: "1h",
	})
	require.NoError(t, config.Save("", cfg))
	assert.Contains(t, toolErrorText(d.CallTool(ctx, "project_task_run", map[string]any{"name": "fresh"})), `task "bot/fresh" not found`)

	// Without a scheduler, entries carry no trigger and runs are refused.
	full := GetDeps()
	SetDeps(&Deps{Agents: full.Agents, Projects: m})
	_, ok := scheduledTask("bot/triage")
	assert.False(t, ok)
	got := callJSON(t, d, "project_task_get", map[string]any{"name": "triage"})
	assert.NotContains(t, got, "trigger")
	assert.Equal(t, []any{}, got["recent_runs"])
	assert.Contains(t, toolErrorText(d.CallTool(ctx, "project_task_run", map[string]any{"name": "triage"})), "scheduler not initialized")

	// Without the projects manager, nothing is managed.
	SetDeps(&Deps{Agents: full.Agents, Scheduler: full.Scheduler})
	assert.Contains(t, toolErrorText(d.CallTool(ctx, "project_task_create", map[string]any{"project": "acme/app", "content": "x", "every": "1h", "trigger": "interval"})), "projects are not available")
	assert.Contains(t, toolErrorText(d.CallTool(ctx, "project_task_update", map[string]any{"name": "triage"})), "projects are not available")

	// With the scheduler back, it still knows the task.
	SetDeps(full)
	sched, ok := scheduledTask("bot/triage")
	require.True(t, ok)
	assert.Equal(t, "bot/triage", sched.ID)
	_, ok = scheduledTask("bot/ghost")
	assert.False(t, ok)
	assert.Empty(t, resultText(nil))
}

// ── Project tools ─────────────────────────────────────────────────────────────

func TestCFPCovProjectJobListTargetsAndQueueTitles(t *testing.T) {
	assert.Equal(t, []string{"a/b", "c/d"}, jobListTargets(projectJobListArgs{Target: "a/b", Targets: []string{"c/d"}}))
	assert.Equal(t, []string{"c/d"}, jobListTargets(projectJobListArgs{Targets: []string{"c/d"}}))

	d, _ := setupProjects(t)
	listed := callJSON(t, d, "project_job_list", map[string]any{"target": "a/b", "targets": []any{"c/d"}})
	assert.Equal(t, []any{}, listed["jobs"])
	assert.EqualValues(t, 0, listed["total"])

	ctx := context.Background()
	assert.Contains(t, toolErrorText(d.CallTool(ctx, "project_job_queue", map[string]any{
		"skill": "gh-safe-merge", "numbers": []any{1}, "titles": map[string]any{"one": "First"},
	})), `titles: "one" is not a number`)
	// Valid titles reach the manager, which refuses while nothing is watched.
	assert.Contains(t, toolErrorText(d.CallTool(ctx, "project_job_queue", map[string]any{
		"skill": "gh-safe-merge", "numbers": []any{1}, "titles": map[string]any{"#1": "First"},
	})), projects.ErrNotWatching.Error())
	assert.Contains(t, toolErrorText(d.CallTool(ctx, "project_queue_candidates", map[string]any{"skill": "gh-safe-merge"})), projects.ErrNotWatching.Error())
	assert.Contains(t, toolErrorText(d.CallTool(ctx, "project_update", map[string]any{"target": " ", "filter": "label:bug"})), "target is required")
}

func TestCFPCovRetryScheduledJobFiresTaskThroughScheduler(t *testing.T) {
	d, _ := setupProjectTasks(t)
	cfpSchedule(t, d, "bot", "nightly", "acme/app")

	got, err := retryScheduledJob("bot/nightly")
	require.NoError(t, err)
	assert.Equal(t, true, got["ok"])
	assert.Equal(t, "retry", got["action"])
	assert.Equal(t, "bot/nightly", got["task"])
	job, ok := got["task_job"].(*domain.Job)
	require.True(t, ok)
	assert.Equal(t, "manual", job.Trigger)
	sched, ok := scheduledTask("bot/nightly")
	require.True(t, ok)
	require.NotEmpty(t, sched.History)
	assert.Equal(t, job.ID, sched.History[len(sched.History)-1].JobID)

	_, err = retryScheduledJob("bot/ghost")
	assert.ErrorContains(t, err, `task "bot/ghost" not found`)

	full := GetDeps()
	SetDeps(&Deps{Agents: full.Agents, Projects: full.Projects})
	_, err = retryScheduledJob("bot/nightly")
	assert.EqualError(t, err, "scheduler not initialized")
	SetDeps(full)
}
