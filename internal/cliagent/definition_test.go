package cliagent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/llm"
)

// fakeGeneric stands in for a CLI driven only through its definition. It
// answers in gemini's stream-json with the session it was started under, how,
// and the prompt it received. Started without a session it prints a captured
// one on stderr.
func fakeGeneric() {
	args := os.Args[1:]
	value := func(flag string) string {
		if i := slices.Index(args, flag); i >= 0 && i+1 < len(args) {
			return args[i+1]
		}
		return ""
	}
	how, session := "new", value("--session-id")
	if id := value("--resume"); id != "" {
		how, session = "resume", id
	}
	if session == "gone" {
		fmt.Fprintln(os.Stderr, "Error: no previous sessions found")
		os.Exit(1)
	}
	if session == "" {
		session = "captured-1"
		fmt.Fprintln(os.Stderr, "session id: "+session)
	}
	out := json.NewEncoder(os.Stdout)
	reply := fmt.Sprintf("%s session=%s model=%s mode=%s prompt=%s", how, session, value("--model"), value("--mode"), value("-p"))
	_ = out.Encode(map[string]any{"type": "message", "role": "assistant", "content": reply[:4], "delta": true})
	_ = out.Encode(map[string]any{"type": "message", "role": "assistant", "content": reply[4:], "delta": true})
	_ = out.Encode(map[string]any{"type": "result", "stats": map[string]any{"input_tokens": 7, "output_tokens": 2}})
}

// useGenericDefinition installs a registry holding a definition for the fake
// CLI, assigning or capturing its session as assign says.
func useGenericDefinition(t *testing.T, assign string) Options {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	session := clidefs.Session{Assign: assign}
	if assign == clidefs.AssignCapture {
		session.Capture = `session id: (\S+)`
	}
	registry, err := clidefs.NewRegistry(clidefs.Definition{
		Name:     "fakecli",
		Binary:   exe,
		Session:  session,
		Output:   clidefs.Output{Format: clidefs.FormatGeminiJSON},
		Approval: map[clidefs.ApprovalMode][]string{clidefs.ApprovalReadOnly: {"--mode", "ro"}, clidefs.ApprovalAuto: {"--mode", "auto"}},
		Args: clidefs.Args{
			Run: []clidefs.Fragment{
				{When: "session", Args: []string{"--session-id", "{session}"}},
				{Args: []string{"{approval}"}},
				{When: "model", Args: []string{"--model", "{model}"}},
				{Args: []string{"-p", "{prompt}"}},
			},
			Resume: []clidefs.Fragment{
				{Args: []string{"--resume", "{session}"}},
				{Args: []string{"{approval}"}},
				{Args: []string{"-p", "{prompt}"}},
			},
		},
	})
	require.NoError(t, err)
	llm.SetCLIProviderRegistry(registry)
	t.Cleanup(func() { llm.SetCLIProviderRegistry(nil) })
	return Options{Backend: "fakecli", Model: "m1", WorkDir: t.TempDir(), Env: []string{"CLIAGENT_FAKE=generic"}}
}

func TestDefinitionSession_AssignedSessionResumes(t *testing.T) {
	opts := useGenericDefinition(t, clidefs.AssignEngine)
	b, model, err := ParseModel("fakecli/m1")
	require.NoError(t, err)
	assert.Equal(t, opts.Backend, b)
	assert.Equal(t, "m1", model)

	opts.NewSessionID = "sess-1"
	opts.SystemPrompt = "be brief"
	opts.BuiltinTools = true
	opts.Approval = []string{"--mode", "auto"}
	s, err := NewSession(opts)
	require.NoError(t, err)

	var events []Event
	r1, err := s.Send(context.Background(), Turn{Text: "hello"}, collect(&events))
	require.NoError(t, err)
	assert.Equal(t, "new session=sess-1 model=m1 mode=auto prompt=<system_instructions>\nbe brief\n</system_instructions>\n\nhello", r1.Text,
		"the first turn runs the run template under the assigned ID, opening with the system prompt")
	assert.Contains(t, events, Event{Type: EventSession, SessionID: "sess-1"})
	assert.Contains(t, events, Event{Type: EventUsage, Usage: &Usage{InputTokens: 7, OutputTokens: 2}})

	r2, err := s.Send(context.Background(), Turn{Text: "again"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "resume session=sess-1 model= mode=auto prompt=again", r2.Text, "a follow-up resumes the session with only the new message")

	// A new session for a stored ID resumes it from the first turn, as a chat
	// does after its process was dropped.
	opts.ResumeID = "sess-1"
	s, err = NewSession(opts)
	require.NoError(t, err)
	r3, err := s.Send(context.Background(), Turn{Text: "later"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "resume session=sess-1 model= mode=auto prompt=later", r3.Text)
}

func TestDefinitionSession_CapturedSessionResumes(t *testing.T) {
	opts := useGenericDefinition(t, clidefs.AssignCapture)
	s, err := NewSession(opts)
	require.NoError(t, err)

	var events []Event
	r1, err := s.Send(context.Background(), Turn{Text: "hello"}, collect(&events))
	require.NoError(t, err)
	assert.Equal(t, "new session=captured-1 model=m1 mode=ro prompt=hello", r1.Text, "without its own tools the CLI runs read-only")
	assert.Contains(t, events, Event{Type: EventSession, SessionID: "captured-1"})

	r2, err := s.Send(context.Background(), Turn{Text: "again"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "resume session=captured-1 model= mode=ro prompt=again", r2.Text)
}

func TestDefinitionSession_MissingSession(t *testing.T) {
	opts := useGenericDefinition(t, clidefs.AssignEngine)
	opts.ResumeID = "gone"
	s, err := NewSession(opts)
	require.NoError(t, err)
	_, err = s.Send(context.Background(), Turn{Text: "hi"}, nil)
	require.Error(t, err, "the runner starts a fresh session on a failed resume")
	assert.True(t, strings.Contains(err.Error(), "no previous sessions found"), err.Error())
}

func TestNewSession_RejectsDefinitionWithoutResume(t *testing.T) {
	_, err := NewSession(Options{Backend: "cline", Model: "m"})
	assert.ErrorContains(t, err, `unknown CLI backend "cline"`)
}

// fakeWired stands in for a definition-driven CLI whose definition declares
// how to take Aviary's MCP server and a system prompt. It calls one tool from
// the registered server and one of its own, then answers with what it was
// handed: its argv, the settings file FAKE_SETTINGS names and the allow file
// its policyPaths names, the system prompt file --sys names, and the token
// from the variable --token-env names.
func fakeWired() {
	args := os.Args[1:]
	value := func(flag string) string {
		if i := slices.Index(args, flag); i >= 0 && i+1 < len(args) {
			return args[i+1]
		}
		return ""
	}
	read := func(path string) string {
		if path == "" {
			return ""
		}
		data, _ := os.ReadFile(path)
		return string(data)
	}
	var settings map[string]any
	_ = json.Unmarshal([]byte(read(os.Getenv("FAKE_SETTINGS"))), &settings)
	allowPath := ""
	if paths, ok := settings["policyPaths"].([]any); ok && len(paths) == 1 {
		allowPath, _ = paths[0].(string)
	}
	reply, _ := json.Marshal(map[string]any{
		"argv":      args,
		"settings":  settings,
		"allowPath": allowPath,
		"allow":     read(allowPath),
		"system":    read(value("--sys")),
		"url":       value("--mcp-url"),
		"token":     os.Getenv(value("--token-env")),
		"prompt":    value("-p"),
	})
	out := json.NewEncoder(os.Stdout)
	_ = out.Encode(map[string]any{"type": "tool_use", "tool_name": "mcp_aviary_echo", "tool_id": "t1", "parameters": map[string]any{"text": "hi"}})
	_ = out.Encode(map[string]any{"type": "tool_result", "tool_id": "t1", "status": "success", "output": "hi"})
	_ = out.Encode(map[string]any{"type": "tool_use", "tool_name": "read_file", "tool_id": "t2", "parameters": map[string]any{"file_path": "a.txt"}})
	_ = out.Encode(map[string]any{"type": "tool_result", "tool_id": "t2", "status": "success", "output": "a"})
	_ = out.Encode(map[string]any{"type": "message", "role": "assistant", "content": string(reply)})
}

// useWiredDefinition installs a registry holding a definition for fakeWired
// that declares an mcp block, with a settings file and arguments, and a
// systemPrompt block.
func useWiredDefinition(t *testing.T) Options {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	registry, err := clidefs.NewRegistry(clidefs.Definition{
		Name:    "wiredcli",
		Binary:  exe,
		Session: clidefs.Session{Assign: clidefs.AssignEngine},
		Output:  clidefs.Output{Format: clidefs.FormatGeminiJSON},
		MCP: &clidefs.MCP{
			Args: []string{"--mcp-url", "{mcpUrl}", "--token-env", "{mcpTokenEnv}"},
			Env:  map[string]string{"FAKE_SETTINGS": "{mcpFile}"},
			File: map[string]any{"mcpServers": map[string]any{"{mcpName}": map[string]any{
				"httpUrl": "{mcpUrl}",
				"headers": map[string]any{"Authorization": "Bearer ${{mcpTokenEnv}}"},
				"timeout": "{mcpTimeoutMs}",
			}}, "policyPaths": []any{"{mcpAllowFile}"}},
			AllowFile:  &clidefs.MCPAllowFile{Extension: ".toml", Lines: []string{"[[rule]]", `mcpName = "{mcpName}"`}},
			ToolPrefix: "mcp_{mcpName}_",
		},
		SystemPrompt: &clidefs.SystemPrompt{Args: []string{"--sys", "{systemPromptFile}"}},
		Args: clidefs.Args{
			Run: []clidefs.Fragment{
				{Args: []string{"--session-id", "{session}"}},
				{Args: []string{"{approval}"}},
				{Args: []string{"-p", "{prompt}"}},
			},
			Resume: []clidefs.Fragment{
				{Args: []string{"--resume", "{session}"}},
				{Args: []string{"{approval}"}},
				{Args: []string{"-p", "{prompt}"}},
			},
		},
	})
	require.NoError(t, err)
	llm.SetCLIProviderRegistry(registry)
	t.Cleanup(func() { llm.SetCLIProviderRegistry(nil) })
	return Options{Backend: "wiredcli", WorkDir: t.TempDir(), NewSessionID: "s1", Env: []string{"CLIAGENT_FAKE=wired"}}
}

type wiredReply struct {
	Argv      []string       `json:"argv"`
	Settings  map[string]any `json:"settings"`
	AllowPath string         `json:"allowPath"`
	Allow     string         `json:"allow"`
	System    string         `json:"system"`
	URL       string         `json:"url"`
	Token     string         `json:"token"`
	Prompt    string         `json:"prompt"`
}

func TestDefinitionSession_PassesMCPAndSystemPrompt(t *testing.T) {
	opts := useWiredDefinition(t)
	assert.True(t, opts.Backend.ServesAviaryTools(), "a definition with an mcp block is offered Aviary's tools")
	opts.SystemPrompt = "be brief"
	opts.MCPURL = "http://127.0.0.1:1/mcp/scoped"
	opts.MCPToken = "secret-token"
	s, err := NewSession(opts)
	require.NoError(t, err)

	for _, text := range []string{"hello", "again"} {
		var events []Event
		r, err := s.Send(context.Background(), Turn{Text: text}, collect(&events))
		require.NoError(t, err)
		var reply wiredReply
		require.NoError(t, json.Unmarshal([]byte(r.Text), &reply), r.Text)

		assert.Equal(t, text, reply.Prompt, "the system prompt is not prepended to the turn")
		assert.Equal(t, "be brief", reply.System, "the system prompt arrives through the declared file")
		assert.Equal(t, opts.MCPURL, reply.URL)
		assert.Equal(t, "secret-token", reply.Token, "the token reaches the CLI through the variable the definition names")
		assert.NotContains(t, strings.Join(reply.Argv, " "), "secret-token", "the token stays off the command line")
		assert.Equal(t, map[string]any{"mcpServers": map[string]any{"aviary": map[string]any{
			"httpUrl": opts.MCPURL,
			"headers": map[string]any{"Authorization": "Bearer ${AVIARY_MCP_TOKEN}"},
			"timeout": float64(MCPToolTimeout.Milliseconds()),
		}}, "policyPaths": []any{reply.AllowPath}}, reply.Settings)
		assert.True(t, strings.HasSuffix(reply.AllowPath, ".toml"), "the allow file's path ends in its extension: %q", reply.AllowPath)
		assert.Equal(t, "[[rule]]\nmcpName = \"aviary\"\n", reply.Allow, "the allow file is written from its lines")
		assert.NoFileExists(t, reply.AllowPath, "the allow file is removed after the turn")

		var tools []string
		for _, event := range events {
			if event.Type == EventToolStart {
				tools = append(tools, event.Tool.Name)
			}
		}
		assert.Equal(t, []string{"read_file"}, tools, "Aviary's own tools are reported by its MCP boundary, not again from the CLI's output")
	}
}

func TestDefinitionSession_WithoutMCPURLPassesNoServer(t *testing.T) {
	opts := useWiredDefinition(t)
	s, err := NewSession(opts)
	require.NoError(t, err)
	var events []Event
	r, err := s.Send(context.Background(), Turn{Text: "hello"}, collect(&events))
	require.NoError(t, err)
	var reply wiredReply
	require.NoError(t, json.Unmarshal([]byte(r.Text), &reply), r.Text)
	assert.Empty(t, reply.URL, "a chat without tools registers no server")
	assert.Nil(t, reply.Settings)
	assert.Empty(t, reply.System, "an empty system prompt writes no file")
	var tools []string
	for _, event := range events {
		if event.Type == EventToolStart {
			tools = append(tools, event.Tool.Name)
		}
	}
	assert.Equal(t, []string{"mcp_aviary_echo", "read_file"}, tools, "without Aviary's server every tool is the CLI's own")
}
