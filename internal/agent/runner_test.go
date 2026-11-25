package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/store"
)

// providerFunc adapts a function to llm.Provider.
type providerFunc func(ctx context.Context, req llm.Request) (<-chan llm.Event, error)

func (f providerFunc) Stream(ctx context.Context, req llm.Request) (<-chan llm.Event, error) {
	return f(ctx, req)
}

// stubToolClient lists tools and answers each call through call.
type stubToolClient struct {
	tools []ToolInfo
	call  func(ctx context.Context, name string, args map[string]any) (string, error)
}

func (s *stubToolClient) ListTools(context.Context) ([]ToolInfo, error) { return s.tools, nil }
func (s *stubToolClient) CallToolText(ctx context.Context, name string, args map[string]any) (string, error) {
	return s.call(ctx, name, args)
}
func (s *stubToolClient) Close() error { return nil }

func useToolClient(t *testing.T, client ToolClient, err error) {
	t.Helper()
	SetToolClientFactory(func(context.Context) (ToolClient, error) { return client, err })
	t.Cleanup(func() { SetToolClientFactory(nil) })
}

// turnEvents is what one prompt streamed to its consumer.
type turnEvents struct {
	text     string
	tools    []ToolEvent
	statuses []string
	media    []string
	err      error
	terminal StreamEventType
}

// runTurn runs one prompt through PromptMediaWithOverrides and collects its
// events until the turn ends. onEvent, when set, sees each event first.
func runTurn(ctx context.Context, t *testing.T, runner *AgentRunner, message string, media []string, overrides RunOverrides, onEvent func(StreamEvent)) turnEvents {
	t.Helper()
	var (
		mu   sync.Mutex
		out  turnEvents
		once sync.Once
	)
	done := make(chan struct{})
	runner.PromptMediaWithOverrides(ctx, message, media, overrides, func(e StreamEvent) {
		if onEvent != nil {
			onEvent(e)
		}
		mu.Lock()
		defer mu.Unlock()
		switch e.Type {
		case StreamEventText:
			out.text += e.Text
		case StreamEventTool:
			out.tools = append(out.tools, *e.Tool)
		case StreamEventStatus:
			out.statuses = append(out.statuses, e.Text)
		case StreamEventMedia:
			out.media = append(out.media, e.MediaURL)
		case StreamEventError, StreamEventDone, StreamEventStop:
			out.err = e.Err
			out.terminal = e.Type
			once.Do(func() { close(done) })
		}
	})
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("timeout waiting for the turn to end")
	}
	runner.Wait()
	mu.Lock()
	defer mu.Unlock()
	return out
}

func noHistory() RunOverrides {
	history := false
	return RunOverrides{History: &history}
}

func TestAgentRunner_NativeToolRoundsRecordErrorsMediaAndResponseID(t *testing.T) {
	setTestDataDir(t)
	verbose := true
	prov := &sequenceProvider{responses: [][]llm.Event{
		{
			{Type: llm.EventTypeText, Text: "Checking."},
			{Type: llm.EventTypeToolCall, ToolCall: &llm.ToolCall{ID: "c1", Name: "search_web", Arguments: map[string]any{"query": "cats"}}},
		},
		{{Type: llm.EventTypeToolCall, ToolCall: &llm.ToolCall{ID: "c2", Name: "ghost", Arguments: map[string]any{}}}},
		{
			{Type: llm.EventTypeMedia, MediaURL: "data:image/png;base64,AA"},
			{Type: llm.EventTypeText, Text: "Here."},
			{Type: llm.EventTypeDone, ResponseID: "resp_1"},
		},
		{{Type: llm.EventTypeText, Text: "again"}},
	}}
	useToolClient(t, &stubToolClient{
		tools: []ToolInfo{{Name: "search_web", Description: "Look things up"}},
		call: func(context.Context, string, map[string]any) (string, error) {
			return "", errors.New("backend down")
		},
	}, nil)
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_rounds", Name: "rounds", Model: "test/model"},
		&config.AgentConfig{Name: "rounds", Verbose: &verbose},
		prov, nil,
	)
	ctx := WithSessionID(context.Background(), "rounds-sess")

	got := runTurn(ctx, t, runner, "find cats", nil, RunOverrides{}, nil)
	require.Equal(t, StreamEventDone, got.terminal, "err: %v", got.err)
	assert.Equal(t, "Checking.Here.", got.text)
	assert.Equal(t, []string{`I am searching for "cats"`}, got.statuses, "verbose mode narrates each tool call")
	require.Len(t, got.tools, 2)
	assert.Equal(t, "search_web", got.tools[1].Name)
	assert.Equal(t, "backend down", got.tools[1].Error)
	assert.True(t, got.tools[1].Done)
	assert.Equal(t, []string{"data:image/png;base64,AA"}, got.media)

	require.Equal(t, 3, prov.callCount())
	second := prov.requests[1].Messages
	require.GreaterOrEqual(t, len(second), 3)
	tail := second[len(second)-3:]
	assert.Equal(t, llm.Message{Role: llm.RoleAssistant, Content: "Checking."}, tail[0], "text before a tool call stays in the conversation")
	require.NotNil(t, tail[1].ToolCall)
	assert.Equal(t, "c1", tail[1].ToolCall.ID)
	require.NotNil(t, tail[2].Result)
	assert.Equal(t, "error: backend down", tail[2].Result.Content, "a failed tool call reaches the model as an error result")
	third := prov.requests[2].Messages
	assert.Equal(t, `Tool "ghost" is not available. Choose one of the available tools.`, third[len(third)-1].Content)

	msgs := sessionMessages(t, "agent_rounds", "rounds-sess")
	var toolErr, mediaMsg, answer bool
	for _, m := range msgs {
		switch {
		case m.Role == domain.MessageRoleTool && strings.Contains(m.Content, "backend down"):
			toolErr = true
		case m.Role == domain.MessageRoleAssistant && len(m.MediaURLs) == 1 && m.Content == "":
			mediaMsg = true
		case m.Role == domain.MessageRoleAssistant && m.Content == "Here.":
			answer = true
		}
	}
	assert.True(t, toolErr, "the failed call is recorded in history")
	assert.True(t, mediaMsg, "a returned image is stored as its own message")
	assert.True(t, answer)

	meta, err := store.ReadSessionMeta(store.SessionMetaPath("agent_rounds", "rounds-sess"))
	require.NoError(t, err)
	require.NotNil(t, meta.Conversation)
	assert.Equal(t, "resp_1", meta.Conversation.ID)

	got = runTurn(ctx, t, runner, "more", nil, RunOverrides{}, nil)
	require.Equal(t, StreamEventDone, got.terminal)
	require.Equal(t, 4, prov.callCount())
	assert.Equal(t, "resp_1", prov.requests[3].PreviousResponseID, "the next turn resumes the provider conversation")
	assert.Len(t, prov.requests[3].Messages, 1, "a resumed conversation sends only the new turn")
}

func TestAgentRunner_CanceledToolCallStopsTheTurn(t *testing.T) {
	setTestDataDir(t)
	prov := &sequenceProvider{responses: [][]llm.Event{
		{{Type: llm.EventTypeToolCall, ToolCall: &llm.ToolCall{ID: "c1", Name: "slow", Arguments: map[string]any{"n": 1.0}}}},
	}}
	useToolClient(t, &stubToolClient{
		tools: []ToolInfo{{Name: "slow"}},
		call: func(ctx context.Context, _ string, _ map[string]any) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		},
	}, nil)
	runner := NewAgentRunner(&domain.Agent{ID: "agent_cancel", Name: "cancel", Model: "test/model"}, &config.AgentConfig{Name: "cancel"}, prov, nil)
	ctx, cancel := context.WithCancel(WithSessionID(context.Background(), "cancel-sess"))
	defer cancel()

	got := runTurn(ctx, t, runner, "go", nil, RunOverrides{}, func(e StreamEvent) {
		if e.Type == StreamEventTool && !e.Tool.Done {
			cancel()
		}
	})
	assert.Equal(t, StreamEventStop, got.terminal)
	assert.Equal(t, 1, prov.callCount(), "a canceled turn asks the model nothing more")
	for _, m := range sessionMessages(t, "agent_cancel", "cancel-sess") {
		assert.NotEqual(t, domain.MessageRoleTool, m.Role, "a canceled call records no result")
	}
}

// refreshFactory serves provider until a forced refresh, which serves
// refreshed or fails with refreshErr.
type refreshFactory struct {
	provider   llm.Provider
	refreshed  llm.Provider
	refreshErr error
	mu         sync.Mutex
	refreshes  int
}

func (f *refreshFactory) ForModel(string) (llm.Provider, error) { return f.provider, nil }

func (f *refreshFactory) ForModelForceRefresh(string) (llm.Provider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshes++
	if f.refreshErr != nil {
		return nil, f.refreshErr
	}
	return f.refreshed, nil
}

func TestAgentRunner_RefreshesTokenOnceAfter401(t *testing.T) {
	unauthorized := &mockProvider{err: errors.New("401 unauthorized")}
	for _, tc := range []struct {
		name         string
		factory      *refreshFactory
		wantTerminal StreamEventType
		wantText     string
	}{
		{
			name:         "refreshed token answers",
			factory:      &refreshFactory{provider: unauthorized, refreshed: &mockProvider{events: []llm.Event{{Type: llm.EventTypeText, Text: "ok"}}}},
			wantTerminal: StreamEventDone,
			wantText:     "ok",
		},
		{
			name:         "refresh fails",
			factory:      &refreshFactory{provider: unauthorized, refreshErr: errors.New("no refresh token")},
			wantTerminal: StreamEventError,
		},
		{
			name:         "refreshed token still rejected",
			factory:      &refreshFactory{provider: unauthorized, refreshed: unauthorized},
			wantTerminal: StreamEventError,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setTestDataDir(t)
			runner := NewAgentRunner(&domain.Agent{ID: "agent_401", Name: "auth", Model: "test/model"}, &config.AgentConfig{Name: "auth"}, nil, nil)
			runner.factory = tc.factory
			got := runTurn(WithSessionID(context.Background(), "auth-sess"), t, runner, "hi", nil, RunOverrides{}, nil)
			assert.Equal(t, tc.wantTerminal, got.terminal)
			assert.Equal(t, tc.wantText, got.text)
			if tc.wantTerminal == StreamEventError {
				assert.ErrorContains(t, got.err, "401")
			}
			assert.Equal(t, 1, tc.factory.refreshes, "a token is refreshed at most once per prompt")
		})
	}
}

func words(n int, word string) string {
	return strings.TrimSpace(strings.Repeat(word+" ", n))
}

func TestAgentRunner_FitsOverBudgetRequests(t *testing.T) {
	// test/model has the default 16000-token input budget.
	const budget = 16000
	toolRound := []llm.Event{
		{Type: llm.EventTypeText, Text: words(5000, "reply")},
		{Type: llm.EventTypeToolCall, ToolCall: &llm.ToolCall{ID: "c1", Name: "lookup", Arguments: map[string]any{}}},
	}
	final := []llm.Event{{Type: llm.EventTypeText, Text: "final"}}
	for _, tc := range []struct {
		name      string
		message   string
		responses [][]llm.Event
		check     func(t *testing.T, reqs []llm.Request)
	}{
		{
			name:      "oldest messages are summarized",
			message:   words(9000, "ask"),
			responses: [][]llm.Event{toolRound, {{Type: llm.EventTypeText, Text: "key facts"}}, final},
			check: func(t *testing.T, reqs []llm.Request) {
				require.Len(t, reqs, 3)
				assert.True(t, strings.HasPrefix(reqs[1].Messages[0].Content, "Summarize the following conversation"))
				msgs := reqs[2].Messages
				require.Len(t, msgs, 3)
				assert.Equal(t, "[Summarized earlier conversation]:\nkey facts", msgs[0].Content)
				assert.NotNil(t, msgs[1].ToolCall, "the newest messages are kept as they were")
				assert.NotNil(t, msgs[2].Result)
			},
		},
		{
			name:      "a failed summary drops the oldest messages",
			message:   words(9000, "ask"),
			responses: [][]llm.Event{toolRound, {{Type: llm.EventTypeText, Text: "  "}}, final},
			check: func(t *testing.T, reqs []llm.Request) {
				require.Len(t, reqs, 3)
				msgs := reqs[2].Messages
				assert.LessOrEqual(t, llm.EstimateRequestTokens(reqs[2]), budget)
				require.NotEmpty(t, msgs)
				assert.NotNil(t, msgs[len(msgs)-1].Result, "the newest message is kept")
				for _, m := range msgs {
					assert.NotContains(t, m.Content, "[Summarized earlier conversation]")
				}
			},
		},
		{
			name:      "a single oversized message is truncated",
			message:   "start " + words(20000, "ask") + " end",
			responses: [][]llm.Event{final},
			check: func(t *testing.T, reqs []llm.Request) {
				require.Len(t, reqs, 1)
				require.Len(t, reqs[0].Messages, 1)
				content := reqs[0].Messages[0].Content
				assert.Less(t, len(strings.Fields(content)), 13000, "the message is cut to about the budget")
				assert.True(t, strings.HasSuffix(content, " end"), "the newest text is kept")
				assert.NotContains(t, content, "start")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setTestDataDir(t)
			prov := &sequenceProvider{responses: tc.responses}
			useToolClient(t, &stubToolClient{
				tools: []ToolInfo{{Name: "lookup"}},
				call:  func(context.Context, string, map[string]any) (string, error) { return "found", nil },
			}, nil)
			runner := NewAgentRunner(&domain.Agent{ID: "agent_budget", Name: "budget", Model: "test/model"}, &config.AgentConfig{Name: "budget"}, prov, nil)
			got := runTurn(WithSessionID(context.Background(), "budget-sess"), t, runner, tc.message, nil, noHistory(), nil)
			require.Equal(t, StreamEventDone, got.terminal, "err: %v", got.err)
			assert.True(t, strings.HasSuffix(got.text, "final"))
			tc.check(t, prov.requests)
		})
	}
}

func TestAgentRunner_ToolLoopStopsAfterMaxRounds(t *testing.T) {
	setTestDataDir(t)
	var mu sync.Mutex
	calls := 0
	prov := providerFunc(func(context.Context, llm.Request) (<-chan llm.Event, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		ch := make(chan llm.Event, 2)
		ch <- llm.Event{Type: llm.EventTypeToolCall, ToolCall: &llm.ToolCall{ID: "c", Name: "lookup", Arguments: map[string]any{}}}
		ch <- llm.Event{Type: llm.EventTypeDone}
		close(ch)
		return ch, nil
	})
	useToolClient(t, &stubToolClient{
		tools: []ToolInfo{{Name: "lookup"}},
		call:  func(context.Context, string, map[string]any) (string, error) { return "again", nil },
	}, nil)
	runner := NewAgentRunner(&domain.Agent{ID: "agent_loop", Name: "loop", Model: "test/model"}, &config.AgentConfig{Name: "loop"}, prov, nil)

	got := runTurn(WithSessionID(context.Background(), "loop-sess"), t, runner, "go", nil, noHistory(), nil)
	require.Equal(t, StreamEventError, got.terminal)
	assert.EqualError(t, got.err, "tool loop exceeded 200 rounds")
	mu.Lock()
	assert.Equal(t, 200, calls)
	mu.Unlock()
	msgs := sessionMessages(t, "agent_loop", "loop-sess")
	last := msgs[len(msgs)-1]
	assert.Equal(t, domain.MessageRoleAssistant, last.Role)
	assert.Equal(t, "Error: tool loop exceeded 200 rounds", last.Content)
}

func TestAgentRunner_SystemPromptCarriesTurnContext(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, runner *AgentRunner) context.Context
		want  []string
	}{
		{
			name: "durable memory without a keyword match",
			setup: func(t *testing.T, runner *AgentRunner) context.Context {
				notesPath := store.NotesPath(runner.memoryPoolID())
				require.NoError(t, os.MkdirAll(filepath.Dir(notesPath), 0o700))
				require.NoError(t, os.WriteFile(notesPath, []byte("- Prefers tea\n- Lives in Paris"), 0o600))
				return WithSessionID(context.Background(), "ctx-sess")
			},
			want: []string{"<memory_context>", "Durable memory:\n- Prefers tea\n- Lives in Paris"},
		},
		{
			name: "slack formatting for a slack channel",
			setup: func(*testing.T, *AgentRunner) context.Context {
				return WithChannelSession(context.Background(), "slack", "work", "C1")
			},
			want: []string{"<channel_formatting>", "Slack mrkdwn"},
		},
		{
			name: "tool calls other turns are running",
			setup: func(t *testing.T, runner *AgentRunner) context.Context {
				untrack := trackSessionToolCall(runner.agent.ID, "ctx-sess", sessionToolCall{id: "c9", name: "web_fetch", args: map[string]any{"url": "https://example.com"}, started: time.Now()})
				t.Cleanup(untrack)
				return WithSessionID(context.Background(), "ctx-sess")
			},
			want: []string{"<in_flight_tool_calls>", `web_fetch {"url":"https://example.com"}`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setTestDataDir(t)
			prov := &captureProvider{}
			runner := NewAgentRunner(&domain.Agent{ID: "agent_ctx", Name: "ctx", Model: "test/model"}, &config.AgentConfig{Name: "ctx"}, prov, nil)
			ctx := tc.setup(t, runner)
			got := runTurn(ctx, t, runner, "hello", nil, RunOverrides{}, nil)
			require.Equal(t, StreamEventDone, got.terminal)
			require.Len(t, prov.requests, 1)
			for _, want := range tc.want {
				assert.Contains(t, prov.requests[0].System, want)
			}
		})
	}
}

func TestAgentRunner_ToolClientFactoryErrorFailsTheTurn(t *testing.T) {
	setTestDataDir(t)
	useToolClient(t, nil, errors.New("mcp down"))
	var delivered string
	RegisterSessionDelivery("agent_mcp_err", "mcp-sess", "signal", "+1", func(text string) { delivered = text })
	t.Cleanup(func() { ClearSessionDelivery("agent_mcp_err", "mcp-sess") })
	prov := &captureProvider{}
	runner := NewAgentRunner(&domain.Agent{ID: "agent_mcp_err", Name: "mcp", Model: "test/model"}, &config.AgentConfig{Name: "mcp"}, prov, nil)

	got := runTurn(WithSessionID(context.Background(), "mcp-sess"), t, runner, "hi", nil, RunOverrides{}, nil)
	assert.Equal(t, StreamEventError, got.terminal)
	assert.EqualError(t, got.err, "mcp down")
	assert.Equal(t, "Error: mcp down", delivered)
	assert.Empty(t, prov.requests, "the model is not asked without its tools")
}

func TestAgentRunner_NoProviderAndCanceledStops(t *testing.T) {
	setTestDataDir(t)
	runner := NewAgentRunner(&domain.Agent{ID: "agent_noprov", Name: "noprov", Model: "test/model"}, &config.AgentConfig{Name: "noprov"}, nil, nil)
	ctx, cancel := context.WithCancel(WithSessionID(context.Background(), "noprov-sess"))
	cancel()

	got := runTurn(ctx, t, runner, "hi", nil, RunOverrides{}, nil)
	assert.Equal(t, StreamEventStop, got.terminal)
	for _, m := range sessionMessages(t, "agent_noprov", "noprov-sess") {
		assert.NotEqual(t, domain.MessageRoleAssistant, m.Role, "a canceled turn stores no provider notice")
	}
}

func TestAgentRunner_AlreadyAnsweredMessageIsNotRunAgain(t *testing.T) {
	setTestDataDir(t)
	prov := &captureProvider{}
	runner := NewAgentRunner(&domain.Agent{ID: "agent_answered", Name: "answered", Model: "test/model"}, &config.AgentConfig{Name: "answered"}, prov, nil)
	require.NoError(t, AppendMessageToSession("agent_answered", "ans-sess", domain.MessageRoleUser, "hi"))
	msgs := sessionMessages(t, "agent_answered", "ans-sess")
	require.Len(t, msgs, 1)
	require.NoError(t, MarkMessageResponded("agent_answered", "ans-sess", msgs[0].ID, "msg_reply"))

	done := make(chan StreamEventType, 1)
	runner.promptCore(WithSessionID(context.Background(), "ans-sess"), "hi", nil, RunOverrides{}, msgs[0].ID, "", false, func(e StreamEvent) {
		done <- e.Type
	})
	select {
	case typ := <-done:
		assert.Equal(t, StreamEventDone, typ)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
	runner.Wait()
	assert.Empty(t, prov.requests)
}

func TestLoadMemoryContext_PoolsAndWholeNotesBudget(t *testing.T) {
	setTestDataDir(t)
	for _, tc := range []struct{ memory, pool string }{
		{"", "private:mem"},
		{"private", "private:mem"},
		{" shared ", "shared"},
		{"team", "team"},
	} {
		runner := NewAgentRunner(&domain.Agent{ID: "agent_mem", Name: "mem"}, &config.AgentConfig{Name: "mem", Memory: tc.memory}, nil, nil)
		assert.Equal(t, tc.pool, runner.memoryPoolID(), "memory %q", tc.memory)
	}

	runner := NewAgentRunner(&domain.Agent{ID: "agent_mem", Name: "mem"}, &config.AgentConfig{Name: "mem", Memory: "team"}, nil, nil)
	notesPath := store.NotesPath("team")
	require.NoError(t, os.MkdirAll(filepath.Dir(notesPath), 0o700))
	lines := []string{words(800, "alpha"), words(800, "beta"), words(800, "gamma")}
	require.NoError(t, os.WriteFile(notesPath, []byte(strings.Join(lines, "\n")), 0o600))

	got := runner.loadMemoryContext("hi")
	assert.True(t, strings.HasPrefix(got, "Durable memory:\n"), "a query with no keywords gets the notes as a whole")
	assert.Contains(t, got, "beta")
	assert.NotContains(t, got, "gamma", "notes past the 2000-word budget are left out")
}

func TestVerboseStatusText(t *testing.T) {
	for _, tc := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"memory_search", map[string]any{"query": "cats"}, `I am searching for "cats"`},
		{"find_files", map[string]any{"pattern": ""}, "I am searching"},
		{"file_read", map[string]any{"path": "/a.txt"}, "I am reading `/a.txt`"},
		{"web_fetch", nil, "I am reading"},
		{"file_write", map[string]any{"file": "b.md"}, "I am writing `b.md`"},
		{"note_append", map[string]any{"path": 3}, "I am writing"},
		{"exec", map[string]any{"cmd": "ls"}, "I am running `ls`"},
		{"Bash", nil, "I am running a command"},
		{"browser_navigate", map[string]any{"url": "https://x.dev"}, "I am navigating to `https://x.dev`"},
		{"browser_click", nil, "I am using the browser"},
		{"memory_store", nil, "I am accessing memory"},
		{"dir_list", map[string]any{"directory": "/tmp"}, "I am listing `/tmp`"},
		{"ls", nil, "I am listing files"},
		{"send_email", nil, "I am using send email"},
	} {
		assert.Equal(t, tc.want, verboseStatusText(tc.tool, tc.args), tc.tool)
	}
}

func TestNormalizeSessionToolArguments(t *testing.T) {
	for _, tc := range []struct {
		name, tool, session string
		args, want          map[string]any
	}{
		{"no session", "session_history", "", map[string]any{"limit": 5}, map[string]any{"limit": 5}},
		{"nil args", "session_history", "s1", nil, nil},
		{"other tool", "file_read", "s1", map[string]any{"session_id": "current"}, map[string]any{"session_id": "current"}},
		{"explicit session kept", "session_messages", "s1", map[string]any{"session_id": "s2"}, map[string]any{"session_id": "s2"}},
		{"non-string session kept", "session_history", "s1", map[string]any{"session_id": 7}, map[string]any{"session_id": 7}},
		{"current placeholder", "session_history", "s1", map[string]any{"session_id": " Current "}, map[string]any{"session_id": "s1"}},
		{"missing session", "session_messages", "s1", map[string]any{"limit": 5}, map[string]any{"limit": 5, "session_id": "s1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, normalizeSessionToolArguments(tc.tool, tc.session, tc.args))
		})
	}
}

func TestSelectRelevantNoteLines(t *testing.T) {
	assert.Nil(t, selectRelevantNoteLines("- tea is good", "what is it?", 100), "a query of stop words has no terms")
	assert.Equal(t, []string{"tea", "paris", "trip"}, keywordTerms("Tea, tea... to Paris; my TRIP!"))

	var notes []string
	for i := 0; i < 10; i++ {
		notes = append(notes, "- tea note")
	}
	notes = append(notes, "- tea and paris together")
	got := selectRelevantNoteLines(strings.Join(notes, "\n"), "tea paris", 0)
	require.Len(t, got, 8, "at most eight lines are kept")
	assert.Equal(t, "tea note", got[0], "kept lines stay in note order")
	assert.Equal(t, "tea and paris together", got[7], "the best match is kept even when it comes last")

	got = selectRelevantNoteLines("- tea one two\n- tea three four\n- tea five six", "tea", 6)
	assert.Equal(t, []string{"tea one two", "tea three four"}, got, "lines past the word budget are dropped")
}

func TestSenderLabelsAndPrivacy(t *testing.T) {
	assert.Equal(t, "system", ircSenderLabel(domain.Message{Role: domain.MessageRoleSystem}))
	assert.Equal(t, "unknown", ircSenderLabel(domain.Message{Role: domain.MessageRoleTool}))
	assert.Equal(t, "U1 (context only)", ircSenderLabel(domain.Message{Role: domain.MessageRoleUser, Sender: &domain.MessageSender{ID: "U1"}}))
	assert.Equal(t, "Ann", senderLabel(&domain.MessageSender{Name: "Ann", ID: "Ann"}))

	for _, tc := range []struct {
		name    string
		channel *store.SessionChannel
		sender  *domain.MessageSender
		want    string
	}{
		{"signal direct", &store.SessionChannel{Type: "signal", ID: "+15550100"}, nil, "private"},
		{"signal group", &store.SessionChannel{Type: "signal", ID: "group.abc"}, nil, "group"},
		{"slack DM", &store.SessionChannel{Type: "slack", ID: "D123"}, nil, "private"},
		{"slack channel", &store.SessionChannel{Type: "slack", ID: "C123"}, nil, "group"},
		{"other channel, onlooker", &store.SessionChannel{Type: "irc", ID: "#x"}, &domain.MessageSender{ID: "u"}, "group"},
		{"web", nil, nil, "private"},
	} {
		assert.Equal(t, tc.want, inferSessionPrivacy(tc.channel, tc.sender), tc.name)
	}
}

func TestMarkUsageFailureAndListToolsErrors(t *testing.T) {
	markUsageFailure(nil, errors.New("boom")) // a turn without a usage record is left alone
	rec := &domain.UsageRecord{}
	markUsageFailure(rec, nil)
	assert.Equal(t, domain.UsageRecord{}, *rec, "no error marks nothing")
	markUsageFailure(rec, errors.New("boom"))
	assert.True(t, rec.HasError)
	assert.False(t, rec.HasThrottle)

	tools, err := listToolsSafe(context.Background(), &failingListClient{})
	assert.Nil(t, tools)
	assert.EqualError(t, err, "list failed")
}

type failingListClient struct{ stubToolClient }

func (*failingListClient) ListTools(context.Context) ([]ToolInfo, error) {
	return nil, errors.New("list failed")
}
