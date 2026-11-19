package agent

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/cliagent"
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
