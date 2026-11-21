package cliagent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/llm"
)

// fakeMuse stands in for `muse exec --json` as Muse Code 1.4.25 behaves
// against a stand-in model: it reads its MCP server and system prompt from -c
// settings, expanding a header that is a whole ${VAR}, calls the server's
// echo tool once, runs one tool of its own, and reports both the way muse
// does, as tool.result records named mcp__<server>__<tool> and read_file. It
// answers with what it was handed.
func fakeMuse() {
	args := os.Args[1:]
	settings := map[string]any{}
	for i, arg := range args {
		if arg != "-c" || i+1 >= len(args) {
			continue
		}
		key, raw, _ := strings.Cut(args[i+1], "=")
		var value any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			_, _ = os.Stderr.WriteString("muse: -c " + key + ": value is not JSON\n")
			os.Exit(2)
		}
		settings[key] = value
	}
	str := func(key string) string { s, _ := settings[key].(string); return s }
	auth := str("mcpServers.aviary.headers.Authorization")
	if strings.HasPrefix(auth, "${") && strings.HasSuffix(auth, "}") {
		auth = os.Getenv(strings.TrimSuffix(strings.TrimPrefix(auth, "${"), "}"))
	}
	out := json.NewEncoder(os.Stdout)
	record := func(payloadType string, payload map[string]any) {
		_ = out.Encode(map[string]any{"schema_version": 1, "payload_type": payloadType, "payload": payload})
	}
	tool := func(id, name, text string) {
		record("task.lifecycle.proposed", map[string]any{"event": map[string]any{"kind": "proposed", "task_kind": "tool." + name}})
		record("tool.result", map[string]any{"call_id": id, "text": text, "correlation_facts": map[string]any{"tool_name": name, "outcome": "success"}})
	}
	if url := str("mcpServers.aviary.url"); url != "" && slices.Contains(args, "--trust-workspace") {
		body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"text":"hi"}}}`)
		req, _ := http.NewRequest(http.MethodPost, url, body)
		req.Header.Set("Authorization", auth)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close() //nolint:errcheck
		}
		tool("call_1", "mcp__aviary__echo", "hi")
	}
	tool("call_2", "read_file", "a")
	reply, _ := json.Marshal(map[string]any{"argv": args, "system": str("run.system_prompt_append"), "prompt": args[len(args)-1]})
	record("run.output.delta", map[string]any{"text": string(reply)})
	record("task.lifecycle.completed", map[string]any{"event": map[string]any{"kind": "completed"}})
}

// useFakeMuse installs a registry holding the built-in muse definition with
// the test binary standing in for muse.
func useFakeMuse(t *testing.T) Options {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	muse, ok := clidefs.MustBuiltin().Lookup("muse")
	require.True(t, ok)
	muse.Binary = exe
	registry, err := clidefs.NewRegistry(muse)
	require.NoError(t, err)
	llm.SetCLIProviderRegistry(registry)
	t.Cleanup(func() { llm.SetCLIProviderRegistry(nil) })
	return Options{Backend: "muse", WorkDir: t.TempDir(), NewSessionID: "s1", Env: []string{"CLIAGENT_FAKE=muse"}}
}

// TestMuseSession_CallsAviaryToolOnce checks a muse chat registers Aviary's
// MCP server so muse calls its tool exactly once a turn, leaves that call out
// of what it reads from muse's output so it is not reported twice, and hands
// muse the system prompt as a setting instead of prepending it to the turn.
func TestMuseSession_CallsAviaryToolOnce(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		calls = append(calls, r.Header.Get("Authorization")+" "+string(bytes.TrimSpace(body)))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"hi"}]}}`))
	}))
	defer server.Close()

	opts := useFakeMuse(t)
	assert.True(t, opts.Backend.ServesAviaryTools(), "muse is offered Aviary's tools")
	opts.SystemPrompt = "Be <brief>.\nSay \"hi\"."
	opts.MCPURL = server.URL + "/mcp/scoped"
	opts.MCPToken = "secret-token"
	s, err := NewSession(opts)
	require.NoError(t, err)

	for turn, text := range []string{"hello", "again"} {
		var events []Event
		r, err := s.Send(context.Background(), Turn{Text: text}, collect(&events))
		require.NoError(t, err)
		var reply wiredReply
		require.NoError(t, json.Unmarshal([]byte(r.Text), &reply), r.Text)

		assert.Equal(t, text, reply.Prompt, "the system prompt is not prepended to the turn")
		assert.Equal(t, opts.SystemPrompt, reply.System, "the system prompt arrives through run.system_prompt_append")
		argv := strings.Join(reply.Argv, " ")
		assert.NotContains(t, argv, "secret-token", "the token stays off the command line")
		assert.Contains(t, argv, "--permission-profile :read-only", "without its own tools muse runs read-only")

		mu.Lock()
		assert.Len(t, calls, turn+1, "muse calls Aviary's tool exactly once a turn")
		assert.True(t, strings.HasPrefix(calls[turn], "Bearer secret-token "), "the call carries the token: %q", calls[turn])
		mu.Unlock()

		var tools []string
		for _, event := range events {
			if event.Type == EventToolStart {
				tools = append(tools, event.Tool.Name)
			}
		}
		assert.Equal(t, []string{"read_file"}, tools, "Aviary's own tools are reported by its MCP boundary, not again from muse's output")
	}
}
