package mcp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
)

// useInProcessDispatch makes Dispatcher resolve to an in-process client
// backed by deps, restoring the previous server checker and deps afterwards.
func useInProcessDispatch(t *testing.T, deps *Deps) *Dispatcher {
	t.Helper()
	oldDeps, oldDepsSet := globalDeps, depsSet
	prevChecker := checkServerRunning
	t.Cleanup(func() {
		globalDeps, depsSet = oldDeps, oldDepsSet
		checkServerRunning = prevChecker
	})
	SetServerChecker(func() bool { return false })
	SetDeps(deps)
	return NewDispatcher("https://localhost:16677", "")
}

// useRemoteDispatch makes Dispatcher resolve to a RemoteClient talking to
// handler over httptest, restoring the previous checker and loader afterwards.
func useRemoteDispatch(t *testing.T, handler http.Handler) *Dispatcher {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	prevChecker, prevLoader := checkServerRunning, loadStoredToken
	t.Cleanup(func() {
		checkServerRunning = prevChecker
		loadStoredToken = prevLoader
	})
	SetServerChecker(func() bool { return true })
	return NewDispatcher(ts.URL, "test-token")
}

func sortedToolInfoNames(tools []ToolInfo) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}

func TestDispatcherListTools_ReturnsInProcessServerTools(t *testing.T) {
	d := useInProcessDispatch(t, &Deps{Agents: agent.NewManager(nil)})

	got, err := d.ListTools(context.Background())
	require.NoError(t, err)

	direct, err := NewInProcessClient(context.Background(), NewServer())
	require.NoError(t, err)
	defer direct.Close() //nolint:errcheck
	want, err := direct.ListTools(context.Background())
	require.NoError(t, err)

	require.NotEmpty(t, got)
	assert.Equal(t, sortedToolInfoNames(want), sortedToolInfoNames(got))
	assert.Contains(t, sortedToolInfoNames(got), "agent_run")
}

// With the server running and no token available, every dispatcher entry
// point reports the token-loading failure instead of connecting anywhere.
func TestDispatcher_ResolveErrorPropagates(t *testing.T) {
	prevChecker, prevLoader := checkServerRunning, loadStoredToken
	t.Cleanup(func() {
		checkServerRunning = prevChecker
		loadStoredToken = prevLoader
	})
	SetServerChecker(func() bool { return true })
	SetTokenLoader(prevLoader)
	d := NewDispatcher("https://127.0.0.1:1", "")

	ctx := context.Background()
	_, err := d.Resolve(ctx)
	require.ErrorContains(t, err, "loading token")
	require.ErrorContains(t, err, "no token configured")

	text, err := d.CallTool(ctx, "agent_list", nil)
	assert.Empty(t, text)
	assert.ErrorContains(t, err, "loading token")

	text, isError, err := d.CallToolWithProgress(ctx, "agent_list", nil, func(string) bool { return true })
	assert.Empty(t, text)
	assert.False(t, isError)
	assert.ErrorContains(t, err, "loading token")

	tools, err := d.ListTools(ctx)
	assert.Nil(t, tools)
	assert.ErrorContains(t, err, "loading token")
}

func TestDispatcher_UnknownToolIsAnError(t *testing.T) {
	d := useInProcessDispatch(t, &Deps{Agents: agent.NewManager(nil)})
	ctx := context.Background()

	text, err := d.CallTool(ctx, "no_such_tool", map[string]any{})
	assert.Empty(t, text)
	assert.ErrorContains(t, err, `calling tool "no_such_tool"`)

	text, isError, err := d.CallToolWithProgress(ctx, "no_such_tool", map[string]any{}, func(string) bool { return true })
	assert.Empty(t, text)
	assert.False(t, isError)
	assert.ErrorContains(t, err, `calling tool "no_such_tool"`)
}

// progressTestServer serves a "count" tool that reports progress before
// answering and a "fail" tool whose handler returns an error.
func progressTestServer() *sdkmcp.Server {
	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test"}, nil)
	sdkmcp.AddTool(srv, &sdkmcp.Tool{Name: "count"}, func(ctx context.Context, req *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		for i, message := range []string{"one", "two", "last"} {
			_ = req.Session.NotifyProgress(ctx, &sdkmcp.ProgressNotificationParams{
				ProgressToken: req.Params.GetProgressToken(),
				Progress:      float64(i + 1),
				Message:       message,
			})
		}
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "done"}}}, struct{}{}, nil
	})
	sdkmcp.AddTool(srv, &sdkmcp.Tool{Name: "fail"}, func(context.Context, *sdkmcp.CallToolRequest, struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		return nil, struct{}{}, errors.New("handler exploded")
	})
	return srv
}

func TestDispatcherCallToolWithProgress_ForwardsRemoteProgress(t *testing.T) {
	d := useRemoteDispatch(t, HTTPHandler(progressTestServer()))

	var got []string
	text, isError, err := d.CallToolWithProgress(context.Background(), "count", map[string]any{}, func(message string) bool {
		got = append(got, message)
		return message == "last"
	})
	require.NoError(t, err)
	assert.Equal(t, "done", text)
	assert.False(t, isError)
	assert.Equal(t, []string{"one", "two", "last"}, got)
}

// A handler error comes back as a result marked as an error, not a Go error.
func TestDispatcher_HandlerErrorIsReportedAsToolError(t *testing.T) {
	d := useRemoteDispatch(t, HTTPHandler(progressTestServer()))
	ctx := context.Background()

	text, isError, err := d.CallToolWithProgress(ctx, "fail", map[string]any{}, func(string) bool { return true })
	require.NoError(t, err)
	assert.True(t, isError)
	assert.Contains(t, text, "handler exploded")

	text, err = d.CallTool(ctx, "fail", map[string]any{})
	assert.Empty(t, text)
	var toolErr *ToolError
	require.ErrorAs(t, err, &toolErr)
	assert.Equal(t, "fail", toolErr.Tool)
	assert.Contains(t, toolErr.Text, "handler exploded")
	assert.Equal(t, toolErr.Text, err.Error())
}

func TestDispatcherListTools_RemoteListErrorIsWrapped(t *testing.T) {
	mcpHandler := HTTPHandler(progressTestServer())
	d := useRemoteDispatch(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if bytes.Contains(body, []byte(`"tools/list"`)) {
			http.Error(w, "listing disabled", http.StatusInternalServerError)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		mcpHandler.ServeHTTP(w, r)
	}))

	tools, err := d.ListTools(context.Background())
	assert.Nil(t, tools)
	assert.ErrorContains(t, err, "listing tools")
}

// When deps were not injected and the config file cannot be parsed, Resolve
// fails with the config error and leaves deps uninitialised.
func TestDispatcherResolve_InProcessConfigLoadError(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("AVIARY_CONFIG_BASE_DIR", filepath.Join(base, "aviary"))
	require.NoError(t, os.MkdirAll(filepath.Join(base, "aviary"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(base, "aviary", "aviary.yaml"), []byte("agents: [unterminated\n"), 0o600))

	d := useInProcessDispatch(t, &Deps{})
	globalDeps, depsSet = &Deps{}, false

	c, err := d.Resolve(context.Background())
	assert.Nil(t, c)
	require.ErrorContains(t, err, "loading config")
	assert.False(t, depsSet)
	assert.Nil(t, globalDeps.Agents)
}

// Deps injected by the server or tests are used as-is.
func TestEnsureInProcessDeps_KeepsInjectedDeps(t *testing.T) {
	injected := &Deps{Agents: agent.NewManager(nil)}
	useInProcessDispatch(t, injected)

	require.NoError(t, ensureInProcessDeps())
	assert.Same(t, injected, GetDeps())
}

// toolText calls a tool and returns its text. A result the tool marked as an
// error is returned as its message; any other error fails the test.
func toolText(ctx context.Context, t *testing.T, d *Dispatcher, tool string, args any) string {
	t.Helper()
	out, err := d.CallTool(ctx, tool, args)
	var toolErr *ToolError
	if errors.As(err, &toolErr) {
		return toolErr.Text
	}
	require.NoError(t, err, tool)
	return out
}

// requireToolErrorText requires err to be a tool's reported error and returns its
// message.
func requireToolErrorText(t *testing.T, err error, msgAndArgs ...any) string {
	t.Helper()
	var toolErr *ToolError
	require.ErrorAs(t, err, &toolErr, msgAndArgs...)
	return toolErr.Text
}
