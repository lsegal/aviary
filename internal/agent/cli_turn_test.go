package agent

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/cliagent"
	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

type bearerRoundTripper struct{ token string }

func (b bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(req)
}

// connectCLIMCP connects to the scoped MCP endpoint the way a CLI would.
func connectCLIMCP(ctx context.Context, url, token string) (*sdkmcp.ClientSession, error) {
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "fake-cli", Version: "1"}, nil)
	return client.Connect(ctx, &sdkmcp.StreamableClientTransport{
		Endpoint:   url,
		HTTPClient: &http.Client{Transport: bearerRoundTripper{token: token}},
	}, nil)
}

// fakeCLISession stands in for a CLI: on each turn it lists the MCP tools it
// was given, calls the requested ones, and replies with what happened.
type fakeCLISession struct {
	opts   cliagent.Options
	calls  []string // tools to call each turn
	turns  *[]fakeCLITurn
	mu     *sync.Mutex
	closed bool
}

type fakeCLITurn struct {
	opts    cliagent.Options
	text    string
	tools   []string
	results []string
}

func (f *fakeCLISession) Send(ctx context.Context, turn cliagent.Turn, onEvent func(cliagent.Event)) (cliagent.Result, error) {
	rec := fakeCLITurn{opts: f.opts, text: turn.Text}
	if f.opts.MCPURL != "" {
		sess, err := connectCLIMCP(ctx, f.opts.MCPURL, f.opts.MCPToken)
		if err != nil {
			return cliagent.Result{}, err
		}
		defer sess.Close() //nolint:errcheck
		list, err := sess.ListTools(ctx, &sdkmcp.ListToolsParams{})
		if err != nil {
			return cliagent.Result{}, err
		}
		for _, t := range list.Tools {
			rec.tools = append(rec.tools, t.Name)
		}
		for _, name := range f.calls {
			res, err := sess.CallTool(ctx, &sdkmcp.CallToolParams{Name: name, Arguments: map[string]any{"q": "x"}})
			switch {
			case err != nil:
				rec.results = append(rec.results, "refused: "+err.Error())
			case res.IsError:
				rec.results = append(rec.results, "error: "+res.Content[0].(*sdkmcp.TextContent).Text)
			default:
				rec.results = append(rec.results, res.Content[0].(*sdkmcp.TextContent).Text)
			}
		}
	}
	id := f.opts.ResumeID
	if id == "" {
		id = f.opts.NewSessionID
	}
	if id == "" {
		id = "codex-thread"
	}
	onEvent(cliagent.Event{Type: cliagent.EventSession, SessionID: id})
	onEvent(cliagent.Event{Type: cliagent.EventToolStart, Tool: &cliagent.ToolEvent{ID: "b1", Name: "Bash", Args: map[string]any{"command": "ls"}}})
	onEvent(cliagent.Event{Type: cliagent.EventToolResult, Tool: &cliagent.ToolEvent{ID: "b1", Name: "Bash", Args: map[string]any{"command": "ls"}, Result: "a.txt"}})
	onEvent(cliagent.Event{Type: cliagent.EventText, Text: "hello "})
	onEvent(cliagent.Event{Type: cliagent.EventText, Text: "there"})
	onEvent(cliagent.Event{Type: cliagent.EventUsage, Usage: &cliagent.Usage{InputTokens: 7, OutputTokens: 3, CacheReadTokens: 2, CostUSD: 0.125}})
	f.mu.Lock()
	*f.turns = append(*f.turns, rec)
	f.mu.Unlock()
	return cliagent.Result{Text: "hello there"}, nil
}

func (f *fakeCLISession) Close() error { f.closed = true; return nil }

type fakeCLIBackend struct {
	mu       sync.Mutex
	calls    []string
	turns    []fakeCLITurn
	sessions []*fakeCLISession
}

func useFakeCLI(t *testing.T, calls ...string) *fakeCLIBackend {
	t.Helper()
	fb := &fakeCLIBackend{calls: calls}
	prev := newCLISession
	newCLISession = func(opts cliagent.Options) (cliagent.Session, error) {
		fb.mu.Lock()
		defer fb.mu.Unlock()
		s := &fakeCLISession{opts: opts, calls: fb.calls, turns: &fb.turns, mu: &fb.mu}
		fb.sessions = append(fb.sessions, s)
		return s, nil
	}
	t.Cleanup(func() { newCLISession = prev })
	return fb
}

func runCLIPrompt(t *testing.T, runner *AgentRunner, sessionID, message string) (string, []ToolEvent) {
	t.Helper()
	var text strings.Builder
	var tools []ToolEvent
	done := make(chan error, 1)
	runner.Prompt(WithSessionID(context.Background(), sessionID), message, func(e StreamEvent) {
		switch e.Type {
		case StreamEventText:
			text.WriteString(e.Text)
		case StreamEventTool:
			tools = append(tools, *e.Tool)
		case StreamEventError:
			done <- e.Err
		case StreamEventDone, StreamEventStop:
			done <- nil
		}
	})
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("timeout")
	}
	runner.Wait()
	return text.String(), tools
}

func newCLITestRunner(t *testing.T, id string, perms *config.PermissionsConfig) *AgentRunner {
	t.Helper()
	runner := NewAgentRunner(
		&domain.Agent{ID: id, Name: id, Model: "cli/claude/opus"},
		&config.AgentConfig{Name: id, Model: "cli/claude/opus", Permissions: perms},
		nil, nil,
	)
	t.Cleanup(runner.Stop)
	return runner
}

func TestCLITurn_StreamsToolsUsageAndResumes(t *testing.T) {
	setTestDataDir(t)
	toolClient := &recordingToolClient{
		tools:   []ToolInfo{{Name: "memory_search", Description: "Search memory"}},
		results: map[string]string{"memory_search": "remembered"},
	}
	SetToolClientFactory(func(context.Context) (ToolClient, error) { return toolClient, nil })
	t.Cleanup(func() { SetToolClientFactory(nil) })
	fb := useFakeCLI(t, "memory_search")

	runner := newCLITestRunner(t, "agent_cli", nil)
	sess, err := NewSessionManager().GetOrCreateNamed("agent_cli", "main")
	require.NoError(t, err)

	text, tools := runCLIPrompt(t, runner, sess.ID, "hi")
	assert.Equal(t, "hello there", text)
	require.Len(t, tools, 4)
	assert.Equal(t, "memory_search", tools[0].Name, "MCP tool calls appear inline through Aviary's tool path")
	assert.Equal(t, "remembered", tools[1].Result)
	assert.Equal(t, "Bash", tools[2].Name)
	assert.Equal(t, "a.txt", tools[3].Result)
	assert.Equal(t, []recordedCall{{Tool: "memory_search", Arguments: map[string]any{"q": "x"}}}, toolClient.calls)

	require.Len(t, fb.turns, 1)
	first := fb.turns[0]
	assert.Equal(t, []string{"memory_search"}, first.tools)
	assert.Equal(t, []string{"remembered"}, first.results)
	assert.Equal(t, "opus", first.opts.Model)
	assert.Contains(t, first.opts.SystemPrompt, `Your agent name is "agent_cli"`)
	assert.False(t, first.opts.BuiltinTools)
	assert.NotEmpty(t, first.opts.NewSessionID)
	assert.Empty(t, first.opts.ResumeID)

	meta, err := store.ReadSessionMeta(store.SessionMetaPath("agent_cli", sess.ID))
	require.NoError(t, err)
	require.NotNil(t, meta.CLI)
	assert.Equal(t, store.CLISessionMeta{Backend: "claude", ID: first.opts.NewSessionID}, *meta.CLI)

	// The second turn reuses the live session.
	runCLIPrompt(t, runner, sess.ID, "again")
	require.Len(t, fb.sessions, 1)
	require.Len(t, fb.turns, 2)

	// After the process goes away, the next turn resumes the stored session.
	runner.stopCLIProcs()
	runner2 := newCLITestRunner(t, "agent_cli", nil)
	runCLIPrompt(t, runner2, sess.ID, "third")
	require.Len(t, fb.sessions, 2)
	assert.Equal(t, first.opts.NewSessionID, fb.turns[2].opts.ResumeID)

	msgs, err := store.ReadJSONL[domain.Message](store.SessionPath("agent_cli", sess.ID))
	require.NoError(t, err)
	var assistant, toolMsgs int
	for _, m := range msgs {
		switch m.Role {
		case domain.MessageRoleAssistant:
			assistant++
			assert.Equal(t, "hello there", m.Content)
		case domain.MessageRoleTool:
			toolMsgs++
		}
	}
	assert.Equal(t, 3, assistant)
	assert.Equal(t, 6, toolMsgs, "both the MCP tool and the CLI's own tool are recorded each turn")

	records, err := store.ReadJSONL[domain.UsageRecord](store.UsagePath())
	require.NoError(t, err)
	require.Len(t, records, 3)
	assert.Equal(t, "cli", records[0].Provider)
	assert.Equal(t, "cli/claude/opus", records[0].Model)
	assert.Equal(t, 7, records[0].InputTokens)
	assert.Equal(t, 3, records[0].OutputTokens)
	assert.Equal(t, 2, records[0].CacheReadTokens)
	assert.Equal(t, 2, records[0].ToolCalls)
	assert.InDelta(t, 0.125, records[0].CostUSD, 1e-9)
}

func TestCLITurn_RefusesToolsOutsidePermissions(t *testing.T) {
	setTestDataDir(t)
	toolClient := &recordingToolClient{tools: []ToolInfo{
		{Name: "memory_search", Description: "Search memory"},
		{Name: "agent_update", Description: "Update an agent"},
	}}
	SetToolClientFactory(func(context.Context) (ToolClient, error) { return toolClient, nil })
	t.Cleanup(func() { SetToolClientFactory(nil) })
	fb := useFakeCLI(t, "agent_update", "exec")

	runner := newCLITestRunner(t, "agent_cli_perm", &config.PermissionsConfig{Tools: []string{"memory_search"}})
	runCLIPrompt(t, runner, "main", "hi")

	require.Len(t, fb.turns, 1)
	assert.Equal(t, []string{"memory_search"}, fb.turns[0].tools)
	require.Len(t, fb.turns[0].results, 2)
	for _, r := range fb.turns[0].results {
		assert.Contains(t, r, "refused:")
	}
	assert.Empty(t, toolClient.calls, "refused calls never reach the tool client")
}

func TestCLITurn_BuiltinToolsNeedFullPresetAndOptIn(t *testing.T) {
	setTestDataDir(t)
	fb := useFakeCLI(t)
	for _, tc := range []struct {
		perms *config.PermissionsConfig
		want  bool
	}{
		{nil, false},
		{&config.PermissionsConfig{Preset: config.PermissionsPresetFull}, false},
		{&config.PermissionsConfig{Preset: config.PermissionsPresetStandard, CLIBuiltinTools: true}, false},
		{&config.PermissionsConfig{Preset: config.PermissionsPresetFull, CLIBuiltinTools: true}, true},
	} {
		fb.turns = nil
		runner := newCLITestRunner(t, "agent_cli_builtin", tc.perms)
		runCLIPrompt(t, runner, "main", "hi")
		runner.Stop()
		require.Len(t, fb.turns, 1)
		assert.Equal(t, tc.want, fb.turns[0].opts.BuiltinTools, "%+v", tc.perms)
		assert.Empty(t, fb.turns[0].opts.MCPURL, "no tools means no MCP server")
	}
}

func TestCLIToolScope_RejectsUnknownToken(t *testing.T) {
	scope, err := newCLIToolScope([]ToolInfo{{Name: "memory_search"}})
	require.NoError(t, err)
	url, err := cliMCPURL()
	require.NoError(t, err)

	_, err = connectCLIMCP(context.Background(), url, "wrong")
	assert.Error(t, err)

	sess, err := connectCLIMCP(context.Background(), url, scope.token)
	require.NoError(t, err)
	res, err := sess.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: "memory_search"})
	if err == nil {
		require.True(t, res.IsError)
		assert.Contains(t, res.Content[0].(*sdkmcp.TextContent).Text, "no active Aviary turn")
	} else {
		assert.ErrorContains(t, err, "no active Aviary turn")
	}
	_ = sess.Close()

	scope.release()
	_, err = connectCLIMCP(context.Background(), url, scope.token)
	assert.Error(t, err, "a released token no longer works")
}

// TestCLITurn_ChatApprovalModeReachesTheCLI checks the chat surface's approval
// mode is translated through the CLI provider's definition into the flags the
// CLI starts with, and that changing it restarts the CLI on the next turn
// (issue #513).
func TestCLITurn_ChatApprovalModeReachesTheCLI(t *testing.T) {
	setTestDataDir(t)
	SetToolClientFactory(func(context.Context) (ToolClient, error) { return &recordingToolClient{}, nil })
	t.Cleanup(func() { SetToolClientFactory(nil) })
	fb := useFakeCLI(t)

	m := newTestManager(t)
	m.Reconcile(&config.Config{
		Agents:   []config.AgentConfig{{Name: "agent_cli_approval", Model: "cli/claude/opus"}},
		Approval: config.ApprovalConfig{Mode: clidefs.ApprovalFull, Chat: clidefs.ApprovalReadOnly},
	})
	runner, ok := m.Get("agent_cli_approval")
	require.True(t, ok)
	t.Cleanup(runner.Stop)

	runCLIPrompt(t, runner, "main", "hi")
	require.Len(t, fb.turns, 1)
	assert.Equal(t, []string{"--permission-mode", "dontAsk"}, fb.turns[0].opts.Approval, "the chat override wins over the general mode")

	// With no chat override the general mode applies, on a new CLI process.
	m.Reconcile(&config.Config{
		Agents:   []config.AgentConfig{{Name: "agent_cli_approval", Model: "cli/claude/opus"}},
		Approval: config.ApprovalConfig{Mode: clidefs.ApprovalFull},
	})
	runCLIPrompt(t, runner, "main", "again")
	require.Len(t, fb.turns, 2)
	require.Len(t, fb.sessions, 2, "a new approval mode starts a new CLI process")
	assert.Equal(t, []string{"--dangerously-skip-permissions"}, fb.turns[1].opts.Approval)

	assert.Equal(t, []string{"-c", "sandbox_mode=read-only"}, cliApprovalArgs(cliagent.Codex, clidefs.ApprovalReadOnly))
	assert.Equal(t, []string{"--permission-mode", "auto"}, cliApprovalArgs(cliagent.Claude, ""), "no setting means auto")
}

// TestCLITurn_GrantsTheAgentDirectory checks a CLI session can reach the
// agent's own directory, where its MEMORY.md and memory/ live, whatever its
// working_dir, without widening the approval mode (#534).
func TestCLITurn_GrantsTheAgentDirectory(t *testing.T) {
	setTestDataDir(t)
	SetToolClientFactory(func(context.Context) (ToolClient, error) { return &recordingToolClient{}, nil })
	t.Cleanup(func() { SetToolClientFactory(nil) })
	fb := useFakeCLI(t)
	workDir := t.TempDir()

	m := newTestManager(t)
	m.Reconcile(&config.Config{
		Agents:   []config.AgentConfig{{Name: "agent_cli_home", Model: "cli/claude/opus", WorkingDir: workDir}},
		Approval: config.ApprovalConfig{Chat: clidefs.ApprovalReadOnly},
	})
	runner, ok := m.Get("agent_cli_home")
	require.True(t, ok)
	t.Cleanup(runner.Stop)
	agentDir := store.AgentDir(runner.Agent().ID)

	runCLIPrompt(t, runner, "main", "hi")
	require.Len(t, fb.turns, 1)
	opts := fb.turns[0].opts
	assert.Equal(t, workDir, opts.WorkDir)
	assert.Equal(t, []string{"--add-dir", agentDir}, opts.ExtraDir, "read-only adds no edit rule")
	assert.DirExists(t, agentDir)
	assert.Equal(t, []string{"--permission-mode", "dontAsk"}, opts.Approval, "the grant does not widen read-only")

	// In auto the agent may also edit its own directory, on a new process.
	m.Reconcile(&config.Config{Agents: []config.AgentConfig{{Name: "agent_cli_home", Model: "cli/claude/opus", WorkingDir: workDir}}})
	runner, ok = m.Get("agent_cli_home")
	require.True(t, ok)
	runCLIPrompt(t, runner, "main", "auto")
	require.Len(t, fb.turns, 2)
	extra := fb.turns[1].opts.ExtraDir
	require.Len(t, extra, 4)
	assert.Equal(t, []string{"--add-dir", agentDir, "--allowedTools"}, extra[:3])
	assert.True(t, strings.HasPrefix(extra[3], "Edit(//") && strings.HasSuffix(extra[3], "/agents/agent_cli_home/**)"), extra[3])
	assert.Equal(t, []string{"--permission-mode", "auto"}, fb.turns[1].opts.Approval)

	// Without a working_dir the CLI already runs in the agent's directory.
	m.Reconcile(&config.Config{Agents: []config.AgentConfig{{Name: "agent_cli_home", Model: "cli/claude/opus"}}})
	runner, ok = m.Get("agent_cli_home")
	require.True(t, ok)
	runCLIPrompt(t, runner, "main", "again")
	require.Len(t, fb.turns, 3)
	assert.Equal(t, agentDir, fb.turns[2].opts.WorkDir)
	assert.Empty(t, fb.turns[2].opts.ExtraDir)

	assert.Equal(t, []string{"-c", `sandbox_workspace_write.writable_roots=["/a"]`}, cliExtraDirArgs(cliagent.Codex, clidefs.ApprovalAuto, "/w", "/a"))
}

// TestCLILaunchEnvOverlaysCodexHome shows chat turns launch codex with
// CODEX_HOME at aviary's own overlay of the user's directory, which keeps
// codex's trusted-project writes out of the user's config.toml (#528) and
// whose own config.toml is empty, so the user's settings are ignored while
// aviary's per-turn profile is read (#573).
func TestCLILaunchEnvOverlaysCodexHome(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(home, "config.toml"), []byte("model = \"gpt-5\"\n"), 0o600))
	t.Setenv("CODEX_HOME", home)
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir("") })

	env := cliLaunchEnv(cliagent.Codex, t.TempDir())
	require.Len(t, env, 1)
	overlay, ok := strings.CutPrefix(env[0], "CODEX_HOME=")
	require.True(t, ok, env[0])
	assert.Equal(t, filepath.Join(store.CLIHomesDir(), "chat"), filepath.Dir(overlay))
	config, err := os.ReadFile(filepath.Join(overlay, "config.toml"))
	require.NoError(t, err)
	assert.Empty(t, config)

}

// TestCLILaunchEnvTrustsClaudeWorkDir shows chat turns launch claude with
// CLAUDE_CONFIG_DIR at aviary's overlay, whose .claude.json copy trusts the
// turn's working directory, so Claude applies its permission allow entries
// while the user's own .claude.json is left alone (#565).
func TestCLILaunchEnvTrustsClaudeWorkDir(t *testing.T) {
	home, workDir := t.TempDir(), t.TempDir()
	user := []byte(`{"projects":{}}`)
	require.NoError(t, os.WriteFile(filepath.Join(home, ".claude.json"), user, 0o600))
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir("") })

	env := cliLaunchEnv(cliagent.Claude, workDir)
	require.Len(t, env, 2)
	overlay, ok := strings.CutPrefix(env[0], "CLAUDE_CONFIG_DIR=")
	require.True(t, ok, env[0])
	assert.Equal(t, store.CLIHomesDir(), filepath.Dir(overlay))
	assert.Equal(t, "CLAUDE_SECURESTORAGE_CONFIG_DIR="+home, env[1])
	data, err := os.ReadFile(filepath.Join(overlay, ".claude.json"))
	require.NoError(t, err)
	assert.Contains(t, string(data), strconv.Quote(filepath.ToSlash(workDir)))
	got, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	require.NoError(t, err)
	assert.Equal(t, user, got)
}

// TestCLITurn_PassesAgentPermissionsToTheCLI checks the agent's exec and
// filesystem rules reach the CLI as its own allow and deny rules, so what the
// agent allows the CLI does not then refuse, and that a session without the
// CLI's own tools gets none (#560).
func TestCLITurn_PassesAgentPermissionsToTheCLI(t *testing.T) {
	setTestDataDir(t)
	fb := useFakeCLI(t)
	allowAll := &config.PermissionsConfig{
		Preset:          config.PermissionsPresetFull,
		CLIBuiltinTools: true,
		Exec:            &config.ExecPermissionsConfig{AllowedCommands: []string{"*"}},
		Filesystem:      &config.FilesystemPermissionsConfig{AllowedPaths: []string{"@/**", "!@/token"}},
	}
	runner := newCLITestRunner(t, "agent_cli_perms", allowAll)
	runCLIPrompt(t, runner, "main", "hi")
	runner.Stop()
	require.Len(t, fb.turns, 1)
	opts := fb.turns[0].opts
	base := config.BaseDir()
	token := filepath.Join(base, "token")
	assert.Equal(t, []string{
		"--allowedTools", "Bash", "--allowedTools", "PowerShell",
		"--add-dir", base, "--allowedTools", "Edit(/" + filepath.ToSlash(posixDrive(filepath.Join(base, "**"))) + ")",
		"--disallowedTools", "Read(/" + filepath.ToSlash(posixDrive(token)) + ")", "--disallowedTools", "Edit(/" + filepath.ToSlash(posixDrive(token)) + ")",
	}, opts.Permissions)
	assert.Contains(t, opts.RefusalNote, `chat approval mode is "auto"`)
	assert.Contains(t, opts.RefusalNote, `agent "agent_cli_perms"'s Allowed Exec Commands or Filesystem Allowed Paths`)

	fb.turns = nil
	noBuiltins := *allowAll
	noBuiltins.CLIBuiltinTools = false
	runner = newCLITestRunner(t, "agent_cli_perms_mcp", &noBuiltins)
	runCLIPrompt(t, runner, "main", "hi")
	runner.Stop()
	require.Len(t, fb.turns, 1)
	assert.Empty(t, fb.turns[0].opts.Permissions, "without the CLI's own tools Aviary's MCP boundary enforces the rules")
	assert.Empty(t, fb.turns[0].opts.RefusalNote)
}

// TestCLITurn_FullModeEnforcesNarrowedRules checks the full chat approval
// mode skips the CLI's own permission checks only for an agent whose rules
// allow every command and the whole Aviary directory. Narrower rules launch
// the CLI with its enforce arguments instead, so it refuses what they leave
// out, naming the agent's rules, and still runs what they allow (#577).
func TestCLITurn_FullModeEnforcesNarrowedRules(t *testing.T) {
	setTestDataDir(t)
	SetToolClientFactory(func(context.Context) (ToolClient, error) { return &recordingToolClient{}, nil })
	t.Cleanup(func() { SetToolClientFactory(nil) })
	fb := useFakeCLI(t)
	enforce := []string{"--permission-mode", "dontAsk", "--allowedTools", "WebFetch", "--allowedTools", "WebSearch", "--allowedTools", "Skill"}
	parent := filepath.Join(filepath.Dir(config.BaseDir()), "**")

	for _, tc := range []struct {
		name     string
		commands []string
		paths    []string
		enforced bool
		hooked   bool
	}{
		{"everything allowed", []string{"*", "!rm *"}, []string{"@/**", "!@/token"}, false, false},
		{"a parent of the Aviary directory", []string{"*"}, []string{parent}, false, false},
		{"narrowed commands", []string{"gh *"}, []string{"@/**"}, true, true},
		{"narrowed paths", []string{"*"}, []string{"@/agents/**"}, true, false},
		{"denied Aviary directory", []string{"*"}, []string{"!@/**"}, true, false},
		{"no command rules", nil, []string{"@/**"}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fb.turns = nil
			perms := &config.PermissionsConfig{
				Preset:          config.PermissionsPresetFull,
				CLIBuiltinTools: true,
				Filesystem:      &config.FilesystemPermissionsConfig{AllowedPaths: tc.paths},
			}
			if tc.commands != nil {
				perms.Exec = &config.ExecPermissionsConfig{AllowedCommands: tc.commands}
			}
			m := newTestManager(t)
			m.Reconcile(&config.Config{
				Agents:   []config.AgentConfig{{Name: "agent_cli_full", Model: "cli/claude/opus", Permissions: perms}},
				Approval: config.ApprovalConfig{Chat: clidefs.ApprovalFull},
			})
			runner, ok := m.Get("agent_cli_full")
			require.True(t, ok)
			t.Cleanup(runner.Stop)
			runCLIPrompt(t, runner, "main", "hi")
			require.Len(t, fb.turns, 1)
			opts := fb.turns[0].opts
			if !tc.enforced {
				assert.Equal(t, []string{"--dangerously-skip-permissions"}, opts.Approval)
				assert.Contains(t, opts.RefusalNote, "not by Aviary")
				assert.Nil(t, opts.CommandHook)
				return
			}
			if tc.hooked {
				// claude's dontAsk mode still runs read-only commands no
				// allow rule covers, so a hook checks every shell call (#595).
				require.NotNil(t, opts.CommandHook)
				assert.Equal(t, "agent_cli_full", opts.CommandHook.Agent)
				assert.Equal(t, tc.commands, opts.CommandHook.Rules)
				assert.NotEmpty(t, opts.CommandHook.Executable)
			} else {
				assert.Nil(t, opts.CommandHook, "rules allowing every command need no hook")
			}
			assert.Equal(t, enforce, opts.Approval)
			assert.Contains(t, opts.RefusalNote, "Refused by Aviary")
			assert.Contains(t, opts.RefusalNote, `agent "agent_cli_full"'s Allowed Exec Commands or Filesystem Allowed Paths`)
			for _, command := range tc.commands {
				if command != "*" && !strings.HasPrefix(command, "!") {
					assert.Contains(t, opts.Permissions, "Bash("+command+")", "an allowed command still runs without asking")
				}
			}
		})
	}
}

// posixDrive writes a Windows drive letter as claude's rules do: C:\a is /c/a.
func posixDrive(path string) string {
	if len(path) >= 2 && path[1] == ':' {
		return "/" + strings.ToLower(path[:1]) + path[2:]
	}
	return path
}

func TestCompleteCLIText_AsksTheCLIModelWithoutToolsOrSession(t *testing.T) {
	fb := useFakeCLI(t)

	got, err := CompleteCLIText(context.Background(), "claude/haiku", "Name this chat.", "plan a trip")
	require.NoError(t, err)
	assert.Equal(t, "hello there", got)

	require.Len(t, fb.sessions, 1)
	s := fb.sessions[0]
	assert.True(t, s.closed)
	assert.Equal(t, cliagent.Claude, s.opts.Backend)
	assert.Equal(t, "haiku", s.opts.Model)
	assert.Equal(t, "Name this chat.", s.opts.SystemPrompt)
	assert.True(t, s.opts.Ephemeral)
	assert.False(t, s.opts.BuiltinTools)
	assert.Empty(t, s.opts.MCPURL)
	require.Len(t, fb.turns, 1)
	assert.Equal(t, "plan a trip", fb.turns[0].text)
}

func TestCompleteCLIText_RejectsAPIModels(t *testing.T) {
	useFakeCLI(t)
	_, err := CompleteCLIText(context.Background(), "anthropic/claude-haiku", "s", "p")
	assert.Error(t, err)
}

// scriptedCLI answers each CLI turn through send and records every session
// started and every turn's text.
type scriptedCLI struct {
	mu       sync.Mutex
	send     func(ctx context.Context, opts cliagent.Options, turn cliagent.Turn, onEvent func(cliagent.Event)) (cliagent.Result, error)
	startErr error
	sessions []*scriptedCLISession
	texts    []string
}

type scriptedCLISession struct {
	cli    *scriptedCLI
	opts   cliagent.Options
	closed bool
}

func (s *scriptedCLISession) Send(ctx context.Context, turn cliagent.Turn, onEvent func(cliagent.Event)) (cliagent.Result, error) {
	s.cli.mu.Lock()
	s.cli.texts = append(s.cli.texts, turn.Text)
	s.cli.mu.Unlock()
	return s.cli.send(ctx, s.opts, turn, onEvent)
}

func (s *scriptedCLISession) Close() error {
	s.cli.mu.Lock()
	defer s.cli.mu.Unlock()
	s.closed = true
	return nil
}

func useScriptedCLI(t *testing.T, cli *scriptedCLI) *scriptedCLI {
	t.Helper()
	prev := newCLISession
	newCLISession = func(opts cliagent.Options) (cliagent.Session, error) {
		cli.mu.Lock()
		defer cli.mu.Unlock()
		if cli.startErr != nil {
			return nil, cli.startErr
		}
		s := &scriptedCLISession{cli: cli, opts: opts}
		cli.sessions = append(cli.sessions, s)
		return s, nil
	}
	t.Cleanup(func() { newCLISession = prev })
	return cli
}

func replyCLI(text string) func(context.Context, cliagent.Options, cliagent.Turn, func(cliagent.Event)) (cliagent.Result, error) {
	return func(_ context.Context, _ cliagent.Options, _ cliagent.Turn, onEvent func(cliagent.Event)) (cliagent.Result, error) {
		onEvent(cliagent.Event{Type: cliagent.EventText, Text: text})
		return cliagent.Result{Text: text}, nil
	}
}

func TestCLITurn_StaleResumeStartsAFreshSessionSeededWithHistory(t *testing.T) {
	setTestDataDir(t)
	toolClient := &recordingToolClient{tools: []ToolInfo{{Name: "memory_search", Description: "Search memory"}}}
	useToolClient(t, toolClient, nil)
	cli := useScriptedCLI(t, &scriptedCLI{
		send: func(_ context.Context, opts cliagent.Options, _ cliagent.Turn, onEvent func(cliagent.Event)) (cliagent.Result, error) {
			if opts.ResumeID != "" {
				return cliagent.Result{}, errors.New("no conversation found with session ID")
			}
			onEvent(cliagent.Event{Type: cliagent.EventToolStart, Tool: &cliagent.ToolEvent{ID: "b1", Name: "Bash", Args: map[string]any{"command": "ls"}}})
			onEvent(cliagent.Event{Type: cliagent.EventText, Text: "fresh answer"})
			return cliagent.Result{Text: "fresh answer"}, nil
		},
	})

	runner := newCLITestRunner(t, "agent_resume", nil)
	verbose := true
	runner.cfg.Verbose = &verbose
	notesPath := store.NotesPath(runner.memoryPoolID())
	require.NoError(t, os.MkdirAll(filepath.Dir(notesPath), 0o700))
	require.NoError(t, os.WriteFile(notesPath, []byte("- Prefers tea"), 0o600))
	_, err := NewSessionManager().GetOrCreateNamed("agent_resume", "main")
	require.NoError(t, err)
	require.NoError(t, AppendMessageToSession("agent_resume", "main", domain.MessageRoleUser, "earlier question"))
	require.NoError(t, AppendMessageToSession("agent_resume", "main", domain.MessageRoleAssistant, "earlier answer"))
	require.NoError(t, store.UpdateSessionMeta(store.SessionMetaPath("agent_resume", "main"), func(meta *store.SessionMeta) {
		meta.CLI = &store.CLISessionMeta{Backend: "claude", ID: "stale-id"}
	}))

	got := runTurn(WithSessionID(context.Background(), "main"), t, runner, "hello", nil, RunOverrides{}, nil)
	require.Equal(t, StreamEventDone, got.terminal, "err: %v", got.err)
	assert.Equal(t, "fresh answer", got.text)
	assert.Equal(t, []string{"I am running `ls`"}, got.statuses, "verbose mode narrates the CLI's own tool calls")

	require.Len(t, cli.sessions, 2)
	stale, fresh := cli.sessions[0], cli.sessions[1]
	assert.Equal(t, "stale-id", stale.opts.ResumeID)
	assert.True(t, stale.closed, "the failed resume's process is closed")
	assert.Empty(t, fresh.opts.ResumeID)
	assert.NotEmpty(t, fresh.opts.NewSessionID)
	assert.NotEmpty(t, fresh.opts.MCPURL)
	assert.Equal(t, stale.opts.MCPURL, fresh.opts.MCPURL, "the fresh session keeps the turn's Aviary tools")
	assert.Equal(t, stale.opts.MCPToken, fresh.opts.MCPToken)

	require.Len(t, cli.texts, 2)
	retry := cli.texts[1]
	assert.True(t, strings.HasPrefix(retry, "<memory_context>"), "recalled memory leads the retried turn")
	assert.Contains(t, retry, "Prefers tea")
	assert.Contains(t, retry, "<prior_conversation>")
	assert.Contains(t, retry, "<user> earlier question")
	assert.Contains(t, retry, "<assistant> earlier answer")
	assert.True(t, strings.HasSuffix(retry, "hello"))

	meta, err := store.ReadSessionMeta(store.SessionMetaPath("agent_resume", "main"))
	require.NoError(t, err)
	assert.Nil(t, meta.CLI, "the stale CLI session is forgotten")
}

func TestCLITurn_FailuresEndTheTurn(t *testing.T) {
	for _, tc := range []struct {
		name      string
		model     string
		cli       *scriptedCLI
		wantErr   string
		wantStop  bool
		wantUsage bool
	}{
		{
			name: "CLI turn fails",
			cli: &scriptedCLI{send: func(context.Context, cliagent.Options, cliagent.Turn, func(cliagent.Event)) (cliagent.Result, error) {
				return cliagent.Result{}, errors.New("cli crashed")
			}},
			wantErr:   "cli crashed",
			wantUsage: true,
		},
		{
			name:      "CLI fails to start",
			cli:       &scriptedCLI{startErr: errors.New("claude not installed")},
			wantErr:   "claude not installed",
			wantUsage: true,
		},
		{
			name:      "unknown CLI backend",
			model:     "cli/bogus/model",
			cli:       &scriptedCLI{send: replyCLI("unused")},
			wantErr:   `unknown CLI backend "bogus"`,
			wantUsage: true,
		},
		{
			name: "turn canceled",
			cli: &scriptedCLI{send: func(ctx context.Context, _ cliagent.Options, _ cliagent.Turn, _ func(cliagent.Event)) (cliagent.Result, error) {
				<-ctx.Done()
				return cliagent.Result{}, ctx.Err()
			}},
			wantStop: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setTestDataDir(t)
			cli := useScriptedCLI(t, tc.cli)
			runner := newCLITestRunner(t, "agent_clifail", nil)
			var delivered string
			RegisterSessionDelivery("agent_clifail", "fail-sess", "signal", "+1", func(text string) { delivered = text })
			t.Cleanup(func() { ClearSessionDelivery("agent_clifail", "fail-sess") })
			ctx, cancel := context.WithCancel(WithSessionID(context.Background(), "fail-sess"))
			defer cancel()
			if tc.wantStop {
				go func() {
					assert.Eventually(t, func() bool {
						cli.mu.Lock()
						defer cli.mu.Unlock()
						return len(cli.texts) > 0
					}, 5*time.Second, 5*time.Millisecond)
					cancel()
				}()
			}

			got := runTurn(ctx, t, runner, "hi", nil, RunOverrides{Model: tc.model}, nil)
			if tc.wantStop {
				assert.Equal(t, StreamEventStop, got.terminal)
				assert.Empty(t, delivered, "a canceled turn delivers nothing")
			} else {
				assert.Equal(t, StreamEventError, got.terminal)
				assert.ErrorContains(t, got.err, tc.wantErr)
				assert.Contains(t, delivered, "Error: "+tc.wantErr)
			}
			records, err := store.ReadJSONL[domain.UsageRecord](store.UsagePath())
			require.NoError(t, err)
			if tc.wantUsage {
				require.Len(t, records, 1)
				assert.True(t, records[0].HasError)
			} else {
				assert.Empty(t, records)
			}

			// A turn that failed to start leaves no conversation behind holding
			// the session: the next turn starts one.
			cli.mu.Lock()
			cli.startErr = nil
			cli.send = replyCLI("recovered")
			cli.mu.Unlock()
			got = runTurn(WithSessionID(context.Background(), "fail-sess"), t, runner, "again", nil, RunOverrides{}, nil)
			assert.Equal(t, StreamEventDone, got.terminal, "err: %v", got.err)
			assert.Equal(t, "recovered", got.text)
		})
	}
}

func TestCLITurn_EphemeralTurnUsesStreamedTextAndRequestContext(t *testing.T) {
	setTestDataDir(t)
	cli := useScriptedCLI(t, &scriptedCLI{
		send: func(_ context.Context, _ cliagent.Options, _ cliagent.Turn, onEvent func(cliagent.Event)) (cliagent.Result, error) {
			onEvent(cliagent.Event{Type: cliagent.EventText, Text: "streamed "})
			onEvent(cliagent.Event{Type: cliagent.EventText, Text: "reply"})
			return cliagent.Result{}, nil
		},
	})
	runner := newCLITestRunner(t, "agent_eph", nil)
	overrides := noHistory()
	overrides.Context = "board: 3 jobs running"

	got := runTurn(WithSessionID(context.Background(), "eph-sess"), t, runner, "status?", nil, overrides, nil)
	require.Equal(t, StreamEventDone, got.terminal, "err: %v", got.err)
	assert.Equal(t, "streamed reply", got.text)

	require.Len(t, cli.sessions, 1)
	assert.True(t, cli.sessions[0].opts.Ephemeral, "a turn without history runs in a throwaway CLI session")
	assert.Empty(t, cli.sessions[0].opts.NewSessionID)
	assert.True(t, cli.sessions[0].closed, "the throwaway session is closed after its turn")
	require.Len(t, cli.texts, 1)
	assert.Contains(t, cli.texts[0], "<request_context>")
	assert.Contains(t, cli.texts[0], "board: 3 jobs running")

	msgs := sessionMessages(t, "agent_eph", "eph-sess")
	require.Len(t, msgs, 2)
	assert.Equal(t, "streamed reply", msgs[1].Content, "the streamed text is the stored answer when the result has none")
}

func TestCompleteCLIText_Failures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cli     *scriptedCLI
		want    string
		wantErr string
	}{
		{name: "CLI fails to start", cli: &scriptedCLI{startErr: errors.New("not installed")}, wantErr: "not installed"},
		{
			name: "CLI turn fails",
			cli: &scriptedCLI{send: func(context.Context, cliagent.Options, cliagent.Turn, func(cliagent.Event)) (cliagent.Result, error) {
				return cliagent.Result{}, errors.New("crashed")
			}},
			wantErr: "crashed",
		},
		{
			name: "streamed text when the result is empty",
			cli: &scriptedCLI{send: func(_ context.Context, _ cliagent.Options, _ cliagent.Turn, onEvent func(cliagent.Event)) (cliagent.Result, error) {
				onEvent(cliagent.Event{Type: cliagent.EventUsage, Usage: &cliagent.Usage{InputTokens: 1}})
				onEvent(cliagent.Event{Type: cliagent.EventText, Text: " Trip plan "})
				return cliagent.Result{}, nil
			}},
			want: "Trip plan",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			useScriptedCLI(t, tc.cli)
			got, err := CompleteCLIText(context.Background(), "claude/haiku", "Name this chat.", "plan a trip")
			if tc.wantErr != "" {
				assert.EqualError(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestCLIArgs_UnknownBackendAndNoRules(t *testing.T) {
	bogus := cliagent.Backend("bogus")
	assert.Nil(t, cliApprovalArgs(bogus, clidefs.ApprovalFull))
	assert.Nil(t, cliExtraDirArgs(bogus, clidefs.ApprovalFull, "/work", "/agent"))
	assert.Nil(t, cliLaunchEnv(bogus, "/work"))
	args, enforce := cliPermissionArgs(bogus, clidefs.ApprovalFull, &config.PermissionsConfig{}, "/work")
	assert.Nil(t, args)
	assert.Nil(t, enforce)
	args, enforce = cliPermissionArgs(cliagent.Claude, clidefs.ApprovalFull, nil, "/work")
	assert.Nil(t, args, "an agent without permissions passes no rules")
	assert.Nil(t, enforce)
}

func TestCLIWorkingDir_ExpandsHome(t *testing.T) {
	setTestDataDir(t)
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	t.Setenv("AVIARY_TEST_PROJECT", "proj")
	runner := newCLITestRunner(t, "agent_wd", nil)
	runner.cfg.WorkingDir = " ~/code/$AVIARY_TEST_PROJECT "
	assert.Equal(t, filepath.Join(home, "code", "proj"), runner.workingDir())

	runner.cfg.WorkingDir = ""
	assert.Equal(t, store.AgentDir("agent_wd"), runner.workingDir(), "without a working_dir the CLI runs in the agent's directory")
}

func TestCLIProcLifecycleHelpers(t *testing.T) {
	sess := &scriptedCLISession{cli: &scriptedCLI{}}
	p := &cliProc{sess: sess}
	p.close()
	assert.True(t, p.closed)
	assert.True(t, sess.closed)
	sess.closed = false
	p.close()
	assert.False(t, sess.closed, "closing twice closes the session once")

	r := &AgentRunner{agent: &domain.Agent{ID: "agent_idle"}}
	r.scheduleCLIIdle(p)
	assert.Nil(t, p.idle, "a closed conversation is not kept for idling")
	url, token := p.sessOptsMCP()
	assert.Empty(t, url, "a conversation without tools has no MCP endpoint")
	assert.Empty(t, token)

	turn := &cliTurn{cancel: func() {}}
	unregister := registerCLITurn("agent_idle\x00s", turn)
	defer unregister()
	require.Same(t, turn, foldCLITurn("agent_idle\x00s"))
	assert.False(t, turn.answer(), "a folded turn cannot answer")
	assert.Nil(t, foldCLITurn("agent_idle\x00s"), "a turn is folded once")
}

func TestCLIMissedTurns_KeepsTheNewest(t *testing.T) {
	key := "agent_missed\x00" + t.Name()
	for i := 1; i <= cliMaxMissedTurns+2; i++ {
		noteCLIMissedTurn(key, "q"+strconv.Itoa(i), "a"+strconv.Itoa(i))
	}
	got := takeCLIMissedTurns(key)
	assert.NotContains(t, got, "User: q1\n")
	assert.NotContains(t, got, "User: q2\n")
	assert.Contains(t, got, "User: q3\nAssistant: a3\n")
	assert.Contains(t, got, "User: q12\nAssistant: a12\n")
	assert.Empty(t, takeCLIMissedTurns(key), "missed turns are delivered once")
}

func TestCLIPriorConversation_SkipsToolOnlyHistory(t *testing.T) {
	setTestDataDir(t)
	runner := newCLITestRunner(t, "agent_prior", nil)
	assert.Empty(t, runner.cliPriorConversation("prior-sess", ""), "an empty session has no prior conversation")
	require.NoError(t, AppendMessageToSession("agent_prior", "prior-sess", domain.MessageRoleUser, "only message"))
	assert.Empty(t, runner.cliPriorConversation("prior-sess", ""), "the current message alone is not prior conversation")
}
