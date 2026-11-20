package cliagent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain lets the test binary stand in for the claude and codex CLIs.
func TestMain(m *testing.M) {
	switch os.Getenv("CLIAGENT_FAKE") {
	case "claude":
		fakeClaude()
		os.Exit(0)
	case "codex":
		fakeCodex()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakeClaude answers each stream-json user message with the turn number, its
// PID, and the text it received.
func fakeClaude() {
	args := os.Args[1:]
	session := "new-session"
	if i := slices.Index(args, "--resume"); i >= 0 {
		session = args[i+1]
	} else if i := slices.Index(args, "--session-id"); i >= 0 {
		session = args[i+1]
	}
	out := json.NewEncoder(os.Stdout)
	_ = out.Encode(map[string]any{"type": "system", "subtype": "init", "session_id": session})
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	turn := 0
	for sc.Scan() {
		turn++
		var msg struct {
			Message struct {
				Content []map[string]any `json:"content"`
			} `json:"message"`
		}
		_ = json.Unmarshal(sc.Bytes(), &msg)
		var text string
		images := 0
		for _, b := range msg.Message.Content {
			switch b["type"] {
			case "text":
				text, _ = b["text"].(string)
			case "image":
				images++
			}
		}
		if text == "sleep" {
			time.Sleep(time.Minute)
		}
		if text == "exit" {
			fmt.Fprintln(os.Stderr, "boom")
			os.Exit(3)
		}
		reply := fmt.Sprintf("turn=%d pid=%d images=%d token=%s text=%s", turn, os.Getpid(), images, os.Getenv(mcpTokenEnv), text)
		_ = out.Encode(map[string]any{"type": "stream_event", "event": map[string]any{"type": "message_start"}})
		_ = out.Encode(map[string]any{"type": "stream_event", "event": map[string]any{
			"type": "content_block_delta", "delta": map[string]any{"type": "text_delta", "text": reply},
		}})
		_ = out.Encode(map[string]any{"type": "assistant", "message": map[string]any{
			"content": []any{map[string]any{"type": "text", "text": reply}},
		}})
		_ = out.Encode(map[string]any{
			"type": "result", "subtype": "success", "result": reply, "session_id": session,
			"usage": map[string]any{"input_tokens": 3, "output_tokens": 5}, "total_cost_usd": 0.25,
		})
	}
}

// fakeCodex reports the thread it resumed and echoes the prompt.
func fakeCodex() {
	args := os.Args[1:]
	thread := "thread-new"
	if len(args) > 1 && args[1] == "resume" {
		thread = args[len(args)-2]
	}
	prompt, _ := bufio.NewReader(os.Stdin).ReadString(0)
	images := 0
	for _, a := range args {
		if a == "--image" {
			images++
		}
	}
	out := json.NewEncoder(os.Stdout)
	_ = out.Encode(map[string]any{"type": "thread.started", "thread_id": thread})
	if prompt == "fail" {
		_ = out.Encode(map[string]any{"type": "turn.failed", "error": map[string]any{"message": "nope"}})
		return
	}
	_ = out.Encode(map[string]any{"type": "item.completed", "item": map[string]any{
		"id": "i0", "type": "agent_message", "text": fmt.Sprintf("thread=%s images=%d prompt=%s", thread, images, prompt),
	}})
	_ = out.Encode(map[string]any{"type": "turn.completed", "usage": map[string]any{
		"input_tokens": 10, "cached_input_tokens": 4, "output_tokens": 2,
	}})
}

func fakeOptions(t *testing.T, backend Backend) Options {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	return Options{
		Backend: backend, Model: "m", Binary: exe, WorkDir: t.TempDir(),
		Env: []string{"CLIAGENT_FAKE=" + string(backend)},
	}
}

func collect(events *[]Event) func(Event) {
	return func(e Event) { *events = append(*events, e) }
}

const redPixel = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGP4z8AAAAMBAQDJ/pLvAAAAAElFTkSuQmCC"

func TestParseModel(t *testing.T) {
	b, m, err := ParseModel("cli/claude/opus")
	require.NoError(t, err)
	assert.Equal(t, Claude, b)
	assert.Equal(t, "opus", m)

	b, m, err = ParseModel("cli/codex/gpt-6-sol")
	require.NoError(t, err)
	assert.Equal(t, Codex, b)
	assert.Equal(t, "gpt-6-sol", m)

	_, _, err = ParseModel("cli/gemini/pro")
	assert.ErrorContains(t, err, `unknown CLI backend "gemini"`)
	_, _, err = ParseModel("cli/claude")
	assert.ErrorContains(t, err, "expected <claude|codex>/<model> or cli/<claude|codex>/<model>")
	_, _, err = ParseModel("anthropic/claude-opus")
	assert.ErrorContains(t, err, "is not a CLI model")
	_, _, err = ParseModel("gemini/pro")
	assert.ErrorContains(t, err, "is not a CLI model")

	assert.True(t, IsCLIModel("cli/claude/opus"))
	assert.False(t, IsCLIModel("anthropic/claude-opus"))
}

// The backend's own name is a provider since the providers unification, so
// "claude/opus" selects the claude CLI exactly as "cli/claude/opus" does.
func TestParseModel_BackendAsProvider(t *testing.T) {
	b, m, err := ParseModel("claude/opus")
	require.NoError(t, err)
	assert.Equal(t, Claude, b)
	assert.Equal(t, "opus", m)

	b, m, err = ParseModel("codex/openai/gpt-6-sol")
	require.NoError(t, err)
	assert.Equal(t, Codex, b)
	assert.Equal(t, "openai/gpt-6-sol", m)

	_, _, err = ParseModel("claude/")
	assert.ErrorContains(t, err, "invalid CLI model")

	assert.True(t, IsCLIModel("claude/opus"))
	assert.True(t, IsCLIModel(" codex/gpt-6-sol"))
	assert.False(t, IsCLIModel("gemini/pro"))
	assert.False(t, IsCLIModel("claude"))
}

func TestClaudeArgs_BuiltinToolsOff(t *testing.T) {
	args := claudeArgs(Options{Model: "opus", SystemPrompt: "rules", MCPURL: "http://127.0.0.1:1/mcp", MCPToken: "secret", NewSessionID: "abc", Approval: []string{"--permission-mode", "dontAsk"}}, "")
	assert.Equal(t, "", args[slices.Index(args, "--tools")+1])
	assert.Equal(t, "mcp__aviary", args[slices.Index(args, "--allowedTools")+1])
	// The approval flags are always passed, so a defaultMode in the user's
	// own Claude settings cannot block Aviary's MCP tools (issue #513).
	assert.Equal(t, "dontAsk", args[slices.Index(args, "--permission-mode")+1])
	assert.Contains(t, args, "--strict-mcp-config")
	assert.Equal(t, "abc", args[slices.Index(args, "--session-id")+1])
	assert.Equal(t, "rules", args[slices.Index(args, "--system-prompt")+1])

	cfg := args[slices.Index(args, "--mcp-config")+1]
	assert.Contains(t, cfg, `"url":"http://127.0.0.1:1/mcp"`)
	assert.Contains(t, cfg, "${AVIARY_MCP_TOKEN}")
	assert.NotContains(t, strings.Join(args, " "), "secret", "the token must not appear on the command line")
}

func TestClaudeArgs_BuiltinToolsOnAndResume(t *testing.T) {
	args := claudeArgs(Options{Model: "opus", BuiltinTools: true, Approval: []string{"--permission-mode", "auto"}}, "sess-1")
	assert.NotContains(t, args, "--tools")
	assert.NotContains(t, args, "--allowedTools")
	assert.Equal(t, "auto", args[slices.Index(args, "--permission-mode")+1])
	assert.Equal(t, "sess-1", args[slices.Index(args, "--resume")+1])
	assert.Equal(t, `{"mcpServers":{}}`, args[slices.Index(args, "--mcp-config")+1])

	args = claudeArgs(Options{Model: "opus", Ephemeral: true, NewSessionID: "x"}, "")
	assert.Contains(t, args, "--no-session-persistence")
	assert.NotContains(t, args, "--session-id")
}

func TestCodexArgs(t *testing.T) {
	args := codexArgs(Options{Model: "gpt", SystemPrompt: "say \"hi\"\nnow", MCPURL: "http://127.0.0.1:1/mcp", MCPToken: "secret"}, "", []string{"/tmp/a.png"})
	joined := strings.Join(args, " ")
	assert.Equal(t, []string{"exec", "--json"}, args[:2])
	assert.Contains(t, joined, `sandbox_mode="read-only"`)
	assert.Contains(t, joined, `web_search="disabled"`)
	// Without its own tools the agent stays read-only whatever the mode.
	assert.NotContains(t, codexArgs(Options{Model: "gpt", Approval: []string{"--dangerously-bypass-approvals-and-sandbox"}}, "", nil), "--dangerously-bypass-approvals-and-sandbox")
	for _, f := range codexBuiltinFeatures {
		assert.Contains(t, joined, "--disable "+f)
	}
	assert.Contains(t, joined, `mcp_servers.aviary.url="http://127.0.0.1:1/mcp"`)
	assert.Contains(t, joined, `mcp_servers.aviary.bearer_token_env_var="AVIARY_MCP_TOKEN"`)
	assert.Contains(t, args, `developer_instructions="say \"hi\"\nnow"`)
	assert.Contains(t, joined, "--image /tmp/a.png")
	assert.NotContains(t, joined, "secret")
	assert.Equal(t, "-", args[len(args)-1])

	args = codexArgs(Options{Model: "gpt", BuiltinTools: true, Approval: []string{"-c", `sandbox_mode="workspace-write"`}}, "thread-1", nil)
	joined = strings.Join(args, " ")
	assert.Equal(t, []string{"exec", "resume"}, args[:2])
	assert.Equal(t, []string{"thread-1", "-"}, args[len(args)-2:])
	assert.Contains(t, joined, `sandbox_mode="workspace-write"`)
	assert.NotContains(t, joined, "--disable")
	assert.NotContains(t, joined, "mcp_servers")
}

func TestClaudeMapper(t *testing.T) {
	var events []Event
	m := newClaudeMapper(collect(&events))
	lines := []string{
		`{"type":"system","subtype":"init","session_id":"s1"}`,
		`{"type":"stream_event","event":{"type":"message_start"}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hel"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"hmm"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"lo"}}}`,
		`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"Hello"},{"type":"tool_use","id":"t1","name":"mcp__aviary__memory_search","input":{"q":"x"}},{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"ls"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ignored"},{"type":"tool_result","tool_use_id":"t2","content":[{"type":"text","text":"a.txt"}]}]}}`,
		`{"type":"stream_event","event":{"type":"message_start"}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":" Done."}]}}`,
		`not json`,
	}
	for _, l := range lines {
		_, done, err := m.handle([]byte(l))
		require.NoError(t, err)
		require.False(t, done)
	}
	res, done, err := m.handle([]byte(`{"type":"result","subtype":"success","result":"Hello Done.","total_cost_usd":0.5,"usage":{"input_tokens":1,"output_tokens":2,"cache_read_input_tokens":3,"cache_creation_input_tokens":4}}`))
	require.NoError(t, err)
	assert.True(t, done)
	assert.Equal(t, "Hello Done.", res.Text)

	var text strings.Builder
	var tools []ToolEvent
	var usage *Usage
	for _, e := range events {
		switch e.Type {
		case EventText:
			text.WriteString(e.Text)
		case EventToolStart, EventToolResult:
			tools = append(tools, *e.Tool)
		case EventUsage:
			usage = e.Usage
		}
	}
	assert.Equal(t, EventSession, events[0].Type)
	assert.Equal(t, "s1", events[0].SessionID)
	assert.Equal(t, "Hello Done.", text.String(), "streamed text must not repeat in the assistant snapshot")
	require.Len(t, tools, 2, "Aviary MCP tools are reported by the MCP boundary, not the CLI stream")
	assert.Equal(t, ToolEvent{ID: "t2", Name: "Bash", Args: map[string]any{"command": "ls"}}, tools[0])
	assert.Equal(t, "a.txt", tools[1].Result)
	assert.Equal(t, &Usage{InputTokens: 1, OutputTokens: 2, CacheReadTokens: 3, CacheWriteTokens: 4, CostUSD: 0.5}, usage)
}

func TestClaudeMapper_ErrorResult(t *testing.T) {
	m := newClaudeMapper(nil)
	_, done, err := m.handle([]byte(`{"type":"result","subtype":"error_during_execution","is_error":true,"result":"API Error: 401"}`))
	assert.True(t, done)
	assert.EqualError(t, err, "claude: API Error: 401")

	_, _, err = m.handle([]byte(`{"type":"result","subtype":"error_max_turns","errors":["too many turns"]}`))
	assert.EqualError(t, err, "claude: too many turns")
}

func TestCodexMapper(t *testing.T) {
	var events []Event
	m := newCodexMapper(collect(&events))
	lines := []string{
		`{"type":"thread.started","thread_id":"th1"}`,
		`{"type":"turn.started"}`,
		`{"type":"item.completed","item":{"id":"r","type":"reasoning","text":"thinking"}}`,
		`{"type":"item.completed","item":{"id":"a","type":"agent_message","text":"Looking."}}`,
		`{"type":"item.started","item":{"id":"c","type":"command_execution","command":"ls","status":"in_progress"}}`,
		`{"type":"item.completed","item":{"id":"c","type":"command_execution","command":"ls","aggregated_output":"nope","exit_code":2,"status":"failed"}}`,
		`{"type":"item.started","item":{"id":"m","type":"mcp_tool_call","server":"aviary","tool":"memory_search","arguments":{}}}`,
		`{"type":"item.completed","item":{"id":"m","type":"mcp_tool_call","server":"aviary","tool":"memory_search","arguments":{}}}`,
		`{"type":"item.completed","item":{"id":"f","type":"file_change","changes":[{"path":"a.go","kind":"update"}],"status":"completed"}}`,
		`{"type":"item.completed","item":{"id":"b","type":"agent_message","text":"Done."}}`,
	}
	for _, l := range lines {
		done, err := m.handle([]byte(l))
		require.NoError(t, err)
		require.False(t, done)
	}
	done, err := m.handle([]byte(`{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":4,"output_tokens":2}}`))
	require.NoError(t, err)
	assert.True(t, done)
	assert.Equal(t, "th1", m.threadID)
	assert.Equal(t, "Done.", m.lastMessage)

	var text strings.Builder
	var tools []ToolEvent
	var usage *Usage
	for _, e := range events {
		switch e.Type {
		case EventText:
			text.WriteString(e.Text)
		case EventToolStart, EventToolResult:
			tools = append(tools, *e.Tool)
		case EventUsage:
			usage = e.Usage
		}
	}
	assert.Equal(t, "Looking.\n\nDone.", text.String())
	require.Len(t, tools, 4, "aviary MCP calls are reported by the MCP boundary")
	assert.Equal(t, "shell", tools[0].Name)
	assert.Equal(t, "nope", tools[1].Error)
	assert.Empty(t, tools[1].Result)
	assert.Equal(t, "file_change", tools[2].Name)
	assert.Equal(t, "completed", tools[3].Result)
	assert.Equal(t, &Usage{InputTokens: 6, OutputTokens: 2, CacheReadTokens: 4}, usage)
}

func TestCodexMapper_ExternalTools(t *testing.T) {
	var events []Event
	m := newCodexMapper(collect(&events))
	for _, l := range []string{
		`{"type":"item.started","item":{"id":"w","type":"web_search","query":"go"}}`,
		`{"type":"item.completed","item":{"id":"w","type":"web_search","query":"go","status":"completed"}}`,
		`{"type":"item.completed","item":{"id":"g","type":"mcp_tool_call","server":"gh","tool":"view","arguments":{"n":1},"result":{"content":[{"type":"text","text":"body"}]}}}`,
	} {
		_, err := m.handle([]byte(l))
		require.NoError(t, err)
	}
	var tools []ToolEvent
	for _, e := range events {
		tools = append(tools, *e.Tool)
	}
	require.Len(t, tools, 4)
	assert.Equal(t, ToolEvent{ID: "w", Name: "web_search", Args: map[string]any{"query": "go"}}, tools[0])
	assert.Equal(t, "completed", tools[1].Result)
	assert.Equal(t, ToolEvent{ID: "g", Name: "gh.view", Args: map[string]any{"n": float64(1)}}, tools[2], "a tool that never reported item.started still starts")
	assert.Equal(t, "body", tools[3].Result)
}

func TestCodexMapper_Errors(t *testing.T) {
	m := newCodexMapper(nil)
	_, err := m.handle([]byte(`{"type":"error","message":"{\"type\":\"error\",\"status\":400,\"error\":{\"message\":\"model not supported\"}}"}`))
	assert.EqualError(t, err, "codex: model not supported")
	done, err := m.handle([]byte(`{"type":"turn.failed","error":{"message":"quota"}}`))
	assert.True(t, done)
	assert.EqualError(t, err, "codex: quota")
}

func TestClaudeSession_LongLivedProcess(t *testing.T) {
	opts := fakeOptions(t, Claude)
	opts.MCPToken = "tok"
	opts.NewSessionID = "sess-1"
	s, err := NewSession(opts)
	require.NoError(t, err)
	defer s.Close() //nolint:errcheck

	var events []Event
	r1, err := s.Send(context.Background(), Turn{Text: "one", MediaURL: redPixel}, collect(&events))
	require.NoError(t, err)
	assert.Contains(t, r1.Text, "turn=1")
	assert.Contains(t, r1.Text, "images=1")
	assert.Contains(t, r1.Text, "token=tok")
	assert.Equal(t, EventSession, events[0].Type)
	assert.Equal(t, "sess-1", events[0].SessionID)

	r2, err := s.Send(context.Background(), Turn{Text: "two"}, nil)
	require.NoError(t, err)
	assert.Contains(t, r2.Text, "turn=2", "the second turn reuses the running process")
	pid := strings.Fields(r1.Text)[1]
	assert.Contains(t, r2.Text, pid)

	// A canceled turn kills the process; the next turn resumes the session in
	// a new one.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err = s.Send(ctx, Turn{Text: "sleep"}, nil)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	events = nil
	r3, err := s.Send(context.Background(), Turn{Text: "three"}, collect(&events))
	require.NoError(t, err)
	assert.Contains(t, r3.Text, "turn=1")
	assert.NotContains(t, r3.Text, pid)
	assert.Equal(t, "sess-1", events[0].SessionID, "restarted process resumes the same session")
}

func TestClaudeSession_ProcessExit(t *testing.T) {
	s, err := NewSession(fakeOptions(t, Claude))
	require.NoError(t, err)
	defer s.Close() //nolint:errcheck
	_, err = s.Send(context.Background(), Turn{Text: "exit"}, nil)
	assert.ErrorContains(t, err, "boom")

	r, err := s.Send(context.Background(), Turn{Text: "again"}, nil)
	require.NoError(t, err)
	assert.Contains(t, r.Text, "text=again")
}

func TestCodexSession_ResumesThread(t *testing.T) {
	s, err := NewSession(fakeOptions(t, Codex))
	require.NoError(t, err)

	var events []Event
	r1, err := s.Send(context.Background(), Turn{Text: "hello", MediaURL: redPixel}, collect(&events))
	require.NoError(t, err)
	assert.Equal(t, "thread=thread-new images=1 prompt=hello", r1.Text)
	assert.Equal(t, Event{Type: EventSession, SessionID: "thread-new"}, events[0])

	r2, err := s.Send(context.Background(), Turn{Text: "again"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "thread=thread-new images=0 prompt=again", r2.Text)

	_, err = s.Send(context.Background(), Turn{Text: "fail"}, nil)
	assert.EqualError(t, err, "codex: nope")
}

func TestCodexSession_MissingBinary(t *testing.T) {
	s, err := NewSession(Options{Backend: Codex, Model: "m", Binary: "/nonexistent/codex"})
	require.NoError(t, err)
	_, err = s.Send(context.Background(), Turn{Text: "hi"}, nil)
	assert.Error(t, err)
}
