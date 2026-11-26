package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// agentToolCall is one call the fake agent server received.
type agentToolCall struct {
	Name string
	Args map[string]any
}

// agentToolServer is a fake Aviary server exposing the agent tools. Each tool
// records its call and replies with the text in replies, or fails with
// IsError when the reply is in errors.
type agentToolServer struct {
	mu      sync.Mutex
	calls   []agentToolCall
	replies map[string]string
	errors  map[string]bool
	events  []string
}

func (s *agentToolServer) recorded() []agentToolCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]agentToolCall(nil), s.calls...)
}

// startAgentToolServer serves the agent tools over MCP and points the CLI's
// dispatcher at it.
func startAgentToolServer(t *testing.T) *agentToolServer {
	t.Helper()
	fake := &agentToolServer{replies: map[string]string{}, errors: map[string]bool{}}
	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fake-aviary"}, nil)
	for _, name := range []string{"agent_list", "agent_run", "agent_stop", "agent_template_sync"} {
		srv.AddTool(&sdkmcp.Tool{Name: name, InputSchema: map[string]any{"type": "object"}}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			args := map[string]any{}
			if len(req.Params.Arguments) > 0 {
				if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
					return nil, err
				}
			}
			fake.mu.Lock()
			fake.calls = append(fake.calls, agentToolCall{Name: name, Args: args})
			reply, isError, events := fake.replies[name], fake.errors[name], fake.events
			fake.mu.Unlock()
			if args["stream_events"] == true {
				for i, event := range events {
					_ = req.Session.NotifyProgress(ctx, &sdkmcp.ProgressNotificationParams{ProgressToken: req.Params.GetProgressToken(), Progress: float64(i + 1), Message: event})
				}
			}
			return &sdkmcp.CallToolResult{IsError: isError, Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: reply}}}, nil
		})
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return srv }, nil))
	httpSrv := httptest.NewServer(mux)
	t.Cleanup(httpSrv.Close)
	useRemoteDispatcher(t, httpSrv.URL)
	return fake
}

// runAgentCmd runs c with args, returning what it printed to stdout and to
// the command's own output.
func runAgentCmd(t *testing.T, c *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var cmdOut string
	stdout, err := captureStdout(t, func() error {
		out, err := runTaskCmd(t, c, args...)
		cmdOut = out
		return err
	})
	return stdout + cmdOut, err
}

func TestAgentListPrintsTheServerReply(t *testing.T) {
	fake := startAgentToolServer(t)
	fake.replies["agent_list"] = `[{"name":"bot"}]`

	out, err := runAgentCmd(t, agentListCmd)
	require.NoError(t, err)
	assert.Equal(t, "[{\"name\":\"bot\"}]\n", out)
	require.Len(t, fake.recorded(), 1)
	assert.Equal(t, "agent_list", fake.recorded()[0].Name)
}

func TestAgentStopAndTemplateSyncSendTheAgentName(t *testing.T) {
	fake := startAgentToolServer(t)
	fake.replies["agent_stop"] = "stopped bot"
	fake.replies["agent_template_sync"] = "synced 3 files"

	out, err := runAgentCmd(t, agentStopCmd, "bot")
	require.NoError(t, err)
	assert.Equal(t, "stopped bot\n", out)

	out, err = runAgentCmd(t, agentTemplateSyncCmd, "bot")
	require.NoError(t, err)
	assert.Equal(t, "synced 3 files\n", out)

	assert.Equal(t, []agentToolCall{
		{Name: "agent_stop", Args: map[string]any{"name": "bot"}},
		{Name: "agent_template_sync", Args: map[string]any{"agent": "bot"}},
	}, fake.recorded())
}

// With no name, the sole configured agent is resolved through agent_list
// before the command's own tool is called.
func TestAgentStopResolvesTheSoleAgent(t *testing.T) {
	fake := startAgentToolServer(t)
	fake.replies["agent_list"] = `[{"name":"solo"}]`

	_, err := runAgentCmd(t, agentStopCmd)
	require.NoError(t, err)
	calls := fake.recorded()
	require.Len(t, calls, 2)
	assert.Equal(t, "agent_list", calls[0].Name)
	assert.Equal(t, agentToolCall{Name: "agent_stop", Args: map[string]any{"name": "solo"}}, calls[1])
}

func TestAgentCommandsReportDispatchErrors(t *testing.T) {
	useRemoteDispatcher(t, "http://127.0.0.1:1")
	for _, tt := range []struct {
		cmd  *cobra.Command
		args []string
	}{
		{agentListCmd, nil},
		{agentStopCmd, []string{"bot"}},
		{agentTemplateSyncCmd, []string{"bot"}},
		{agentRunCmd, []string{"bot", "hi"}},
		{agentRunCmd, []string{"bot", "hi", "--output-format", "json"}},
	} {
		t.Run(tt.cmd.Name()+" "+strings.Join(tt.args, " "), func(t *testing.T) {
			out, err := runAgentCmd(t, tt.cmd, tt.args...)
			require.Error(t, err)
			assert.Empty(t, out)
		})
	}
}

func TestAgentRunTextSendsTheMessage(t *testing.T) {
	fake := startAgentToolServer(t)
	fake.replies["agent_run"] = "hello back"

	out, err := runAgentCmd(t, agentRunCmd, "bot", "hello")
	require.NoError(t, err)
	assert.Equal(t, "hello back", out)
	// history is left to the server unless the flag is given.
	assert.Equal(t, []agentToolCall{{Name: "agent_run", Args: map[string]any{"name": "bot", "message": "hello", "bare": false}}}, fake.recorded())
}

func TestAgentRunSendsHistoryOnlyWhenTheFlagIsGiven(t *testing.T) {
	fake := startAgentToolServer(t)

	_, err := runAgentCmd(t, agentRunCmd, "bot", "hello", "--history=false", "--bare")
	require.NoError(t, err)
	_, err = runAgentCmd(t, agentRunCmd, "bot", "hello", "--history")
	require.NoError(t, err)

	calls := fake.recorded()
	require.Len(t, calls, 2)
	assert.Equal(t, false, calls[0].Args["history"])
	assert.Equal(t, true, calls[0].Args["bare"])
	assert.Equal(t, true, calls[1].Args["history"])
	assert.Equal(t, false, calls[1].Args["bare"])
}

func TestAgentRunMakesTheWorkdirAbsolute(t *testing.T) {
	fake := startAgentToolServer(t)
	wd, err := os.Getwd()
	require.NoError(t, err)

	_, err = runAgentCmd(t, agentRunCmd, "bot", "hello", "--workdir", "clone")
	require.NoError(t, err)

	calls := fake.recorded()
	require.Len(t, calls, 1)
	assert.Equal(t, filepath.Join(wd, "clone"), calls[0].Args["workdir"])
}

func TestAgentRunReadsTheMessageFromAFile(t *testing.T) {
	fake := startAgentToolServer(t)
	path := filepath.Join(t.TempDir(), "prompt.md")
	require.NoError(t, os.WriteFile(path, []byte("from the file\n"), 0o600))

	// With --file, the only argument is the agent name.
	_, err := runAgentCmd(t, agentRunCmd, "bot", "--file", path)
	require.NoError(t, err)

	calls := fake.recorded()
	require.Len(t, calls, 1)
	assert.Equal(t, "bot", calls[0].Args["name"])
	assert.Equal(t, "from the file\n", calls[0].Args["message"])
}

func TestAgentRunMessageErrors(t *testing.T) {
	fake := startAgentToolServer(t)
	empty := filepath.Join(t.TempDir(), "empty.md")
	require.NoError(t, os.WriteFile(empty, nil, 0o600))

	_, err := runAgentCmd(t, agentRunCmd, "bot", "--file", filepath.Join(t.TempDir(), "missing.md"))
	assert.ErrorContains(t, err, "reading file:")

	_, err = runAgentCmd(t, agentRunCmd, "bot", "--file", empty)
	assert.EqualError(t, err, "message required: pass as argument or use --file")

	_, err = runAgentCmd(t, agentRunCmd, "bot", "")
	assert.EqualError(t, err, "message required: pass as argument or use --file")

	_, err = runAgentCmd(t, agentRunCmd, "bot", "hello", "--output-format", "yaml")
	assert.EqualError(t, err, `--output-format must be text or json, not "yaml"`)

	assert.Empty(t, fake.recorded())
}

func TestAgentRunJSONPrintsOneEventPerLine(t *testing.T) {
	fake := startAgentToolServer(t)
	fake.replies["agent_run"] = "Done."
	fake.events = []string{
		`{"type":"tool_call","id":"call_1","name":"file_read","input":{"path":"a"}}`,
		`{"type":"text","text":"Done."}`,
		`{"type":"done"}`,
	}

	out, err := runAgentCmd(t, agentRunCmd, "bot", "read a", "--output-format", "json")
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	require.Len(t, lines, 2, out)
	for _, line := range lines {
		assert.True(t, json.Valid([]byte(line)), line)
	}
	assert.JSONEq(t, `{"type":"tool_call","id":"call_1","name":"file_read","input":{"path":"a"}}`, lines[0])
	assert.JSONEq(t, `{"type":"message","text":"Done."}`, lines[1])

	calls := fake.recorded()
	require.Len(t, calls, 1)
	assert.Equal(t, true, calls[0].Args["stream_events"])
}

// A tool result marked as an error is printed as an error event rather than
// returned, so a JSON consumer sees it in the stream.
func TestAgentRunJSONPrintsToolErrorsAsEvents(t *testing.T) {
	fake := startAgentToolServer(t)
	fake.replies["agent_run"] = `agent "bot" not found`
	fake.errors["agent_run"] = true

	out, err := runAgentCmd(t, agentRunCmd, "bot", "hi", "--output-format", "json")
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"error","text":"agent \"bot\" not found"}`, strings.TrimSpace(out))
}
