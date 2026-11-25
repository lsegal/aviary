package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/browser"
	internalmcp "github.com/lsegal/aviary/internal/mcp"
)

// browserCall is one browser tool call received by the fake server.
type browserCall struct {
	name string
	args map[string]any
}

// startBrowserToolServer serves every browser tool the CLI calls, recording
// each call and answering "<tool> ok", or failing with "<tool> failed" when
// fail is set, and points the CLI's dispatcher at it.
func startBrowserToolServer(t *testing.T, fail bool) *[]browserCall {
	t.Helper()
	calls := &[]browserCall{}
	s := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fake-aviary"}, nil)
	for _, name := range []string{
		"browser_open", "browser_tabs", "browser_click", "browser_navigate", "browser_keystroke",
		"browser_fill", "browser_screenshot", "browser_wait", "browser_text", "browser_query", "browser_eval",
	} {
		s.AddTool(&sdkmcp.Tool{Name: name, InputSchema: map[string]any{"type": "object"}}, func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			var args map[string]any
			if len(req.Params.Arguments) > 0 {
				if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
					return nil, err
				}
			}
			*calls = append(*calls, browserCall{name: name, args: args})
			if fail {
				return nil, errors.New(name + " failed")
			}
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: name + " ok"}}}, nil
		})
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return s }, &sdkmcp.StreamableHTTPOptions{JSONResponse: true}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	useRemoteDispatcher(t, srv.URL)
	return calls
}

// browserFlags holds the values of the browser commands' flag variables.
type browserFlags struct {
	tab, selector, text             string
	count, maxLen, maxText, timeout int
	html                            bool
}

// setBrowserFlags sets the browser commands' flag variables, restoring them
// in cleanup.
func setBrowserFlags(t *testing.T, f browserFlags) {
	t.Helper()
	prev := browserFlags{browserTabID, browserSelector, browserText, browserCount, browserMaxLen, browserMaxText, browserTimeout, browserHTML}
	apply := func(f browserFlags) {
		browserTabID, browserSelector, browserText = f.tab, f.selector, f.text
		browserCount, browserMaxLen, browserMaxText, browserTimeout = f.count, f.maxLen, f.maxText, f.timeout
		browserHTML = f.html
	}
	apply(f)
	t.Cleanup(func() { apply(prev) })
}

// runBrowserCmd runs c's RunE with args and returns what it printed.
func runBrowserCmd(t *testing.T, c *cobra.Command, args ...string) (string, error) {
	t.Helper()
	c.SetContext(context.Background())
	return captureStdout(t, func() error { return c.RunE(c, args) })
}

func TestBrowserCommandsCallTools(t *testing.T) {
	tests := []struct {
		name     string
		cmd      *cobra.Command
		args     []string
		flags    browserFlags
		tool     string
		wantArgs map[string]any
	}{
		{name: "open", cmd: browserOpenCmd, args: []string{"https://example.com"}, tool: "browser_open", wantArgs: map[string]any{"url": "https://example.com"}},
		{name: "tabs", cmd: browserTabsCmd, tool: "browser_tabs", wantArgs: map[string]any{}},
		{name: "click", cmd: browserClickCmd, flags: browserFlags{tab: "T1", selector: "#go"}, tool: "browser_click", wantArgs: map[string]any{"tab_id": "T1", "selector": "#go"}},
		{name: "navigate", cmd: browserNavigateCmd, args: []string{"https://example.org"}, flags: browserFlags{tab: "T1"}, tool: "browser_navigate", wantArgs: map[string]any{"tab_id": "T1", "url": "https://example.org"}},
		{name: "type text flag", cmd: browserTypeCmd, flags: browserFlags{tab: "T1", selector: "input", text: "from flag"}, tool: "browser_keystroke", wantArgs: map[string]any{"tab_id": "T1", "selector": "input", "text": "from flag"}},
		{name: "type argument wins over flag", cmd: browserTypeCmd, args: []string{"from arg"}, flags: browserFlags{tab: "T1", selector: "input", text: "from flag"}, tool: "browser_keystroke", wantArgs: map[string]any{"tab_id": "T1", "selector": "input", "text": "from arg"}},
		{name: "fill text flag", cmd: browserFillCmd, flags: browserFlags{tab: "T2", selector: "textarea", text: "hello"}, tool: "browser_fill", wantArgs: map[string]any{"tab_id": "T2", "selector": "textarea", "text": "hello"}},
		{name: "fill argument wins over flag", cmd: browserFillCmd, args: []string{"world"}, flags: browserFlags{tab: "T2", selector: "textarea", text: "hello"}, tool: "browser_fill", wantArgs: map[string]any{"tab_id": "T2", "selector": "textarea", "text": "world"}},
		{name: "screenshot", cmd: browserScreenshotCmd, flags: browserFlags{tab: "T3"}, tool: "browser_screenshot", wantArgs: map[string]any{"tab_id": "T3"}},
		{name: "wait", cmd: browserWaitCmd, flags: browserFlags{tab: "T1", selector: ".ready", timeout: 2500}, tool: "browser_wait", wantArgs: map[string]any{"tab_id": "T1", "selector": ".ready", "timeout_ms": float64(2500)}},
		{name: "text without selector", cmd: browserTextCmd, flags: browserFlags{tab: "T1", maxLen: 4000}, tool: "browser_text", wantArgs: map[string]any{"tab_id": "T1", "selector": "", "max_length": float64(4000)}},
		{name: "text with selector", cmd: browserTextCmd, flags: browserFlags{tab: "T1", selector: "main", maxLen: 80}, tool: "browser_text", wantArgs: map[string]any{"tab_id": "T1", "selector": "main", "max_length": float64(80)}},
		{name: "query", cmd: browserQueryCmd, flags: browserFlags{tab: "T1", selector: "a", count: 5, maxText: 50, html: true}, tool: "browser_query", wantArgs: map[string]any{"tab_id": "T1", "selector": "a", "count": float64(5), "max_text_length": float64(50), "include_html": true}},
		{name: "eval", cmd: browserEvalCmd, args: []string{"document.title"}, flags: browserFlags{tab: "T4"}, tool: "browser_eval", wantArgs: map[string]any{"tab_id": "T4", "javascript": "document.title"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := startBrowserToolServer(t, false)
			setBrowserFlags(t, tt.flags)

			out, err := runBrowserCmd(t, tt.cmd, tt.args...)
			require.NoError(t, err)
			assert.Equal(t, tt.tool+" ok\n", out, "prints the tool's output")
			require.Len(t, *calls, 1)
			assert.Equal(t, tt.tool, (*calls)[0].name)
			assert.Equal(t, tt.wantArgs, (*calls)[0].args)
		})
	}
}

func TestBrowserCommandsReturnToolErrors(t *testing.T) {
	tests := []struct {
		cmd  *cobra.Command
		args []string
		tool string
	}{
		{cmd: browserOpenCmd, args: []string{"https://example.com"}, tool: "browser_open"},
		{cmd: browserTabsCmd, tool: "browser_tabs"},
		{cmd: browserClickCmd, tool: "browser_click"},
		{cmd: browserNavigateCmd, args: []string{"https://example.com"}, tool: "browser_navigate"},
		{cmd: browserTypeCmd, tool: "browser_keystroke"},
		{cmd: browserFillCmd, tool: "browser_fill"},
		{cmd: browserScreenshotCmd, tool: "browser_screenshot"},
		{cmd: browserWaitCmd, tool: "browser_wait"},
		{cmd: browserTextCmd, tool: "browser_text"},
		{cmd: browserQueryCmd, tool: "browser_query"},
		{cmd: browserEvalCmd, args: []string{"1+1"}, tool: "browser_eval"},
	}
	for _, tt := range tests {
		t.Run(tt.cmd.Name(), func(t *testing.T) {
			calls := startBrowserToolServer(t, true)
			setBrowserFlags(t, browserFlags{tab: "T1", selector: "#x", text: "hi"})

			out, err := runBrowserCmd(t, tt.cmd, tt.args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.tool+" failed")
			assert.Empty(t, out, "prints nothing on failure")
			assert.Len(t, *calls, 1)
		})
	}
}

func TestBrowserCommandsValidateFlags(t *testing.T) {
	tests := []struct {
		name    string
		cmd     *cobra.Command
		args    []string
		flags   browserFlags
		wantErr string
	}{
		{name: "click needs tab", cmd: browserClickCmd, flags: browserFlags{selector: "#x"}, wantErr: "--tab is required"},
		{name: "click needs selector", cmd: browserClickCmd, flags: browserFlags{tab: "T1"}, wantErr: "--selector is required"},
		{name: "navigate needs tab", cmd: browserNavigateCmd, args: []string{"https://example.com"}, wantErr: "--tab is required"},
		{name: "type needs tab", cmd: browserTypeCmd, flags: browserFlags{selector: "#x", text: "hi"}, wantErr: "--tab is required"},
		{name: "type needs selector", cmd: browserTypeCmd, flags: browserFlags{tab: "T1", text: "hi"}, wantErr: "--selector is required"},
		{name: "type needs text", cmd: browserTypeCmd, flags: browserFlags{tab: "T1", selector: "#x"}, wantErr: "text is required: pass as argument or use --text"},
		{name: "type rejects empty argument", cmd: browserTypeCmd, args: []string{""}, flags: browserFlags{tab: "T1", selector: "#x"}, wantErr: "text is required"},
		{name: "fill needs tab", cmd: browserFillCmd, flags: browserFlags{selector: "#x", text: "hi"}, wantErr: "--tab is required"},
		{name: "fill needs selector", cmd: browserFillCmd, flags: browserFlags{tab: "T1", text: "hi"}, wantErr: "--selector is required"},
		{name: "fill needs text", cmd: browserFillCmd, flags: browserFlags{tab: "T1", selector: "#x"}, wantErr: "text is required: pass as argument or use --text"},
		{name: "screenshot needs tab", cmd: browserScreenshotCmd, wantErr: "--tab is required"},
		{name: "wait needs tab", cmd: browserWaitCmd, flags: browserFlags{selector: "#x"}, wantErr: "--tab is required"},
		{name: "wait needs selector", cmd: browserWaitCmd, flags: browserFlags{tab: "T1"}, wantErr: "--selector is required"},
		{name: "text needs tab", cmd: browserTextCmd, wantErr: "--tab is required"},
		{name: "query needs tab", cmd: browserQueryCmd, flags: browserFlags{selector: "a"}, wantErr: "--tab is required"},
		{name: "query needs selector", cmd: browserQueryCmd, flags: browserFlags{tab: "T1"}, wantErr: "--selector is required"},
		{name: "eval needs tab", cmd: browserEvalCmd, args: []string{"1+1"}, wantErr: "--tab is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := startBrowserToolServer(t, false)
			setBrowserFlags(t, tt.flags)

			out, err := runBrowserCmd(t, tt.cmd, tt.args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.Empty(t, out)
			assert.Empty(t, *calls, "no tool is called")
		})
	}
}

func TestBrowserCommandsRegistered(t *testing.T) {
	got := map[string]bool{}
	for _, c := range browserCmd.Commands() {
		got[c.Name()] = true
	}
	for _, name := range []string{"open", "tabs", "click", "navigate", "type", "fill", "screenshot", "wait", "text", "query", "eval"} {
		assert.True(t, got[name], "browser %s is registered", name)
	}
	assert.Equal(t, "10000", browserWaitCmd.Flags().Lookup("timeout-ms").DefValue)
	assert.Equal(t, "4000", browserTextCmd.Flags().Lookup("max-length").DefValue)
	assert.Equal(t, "20", browserQueryCmd.Flags().Lookup("count").DefValue)
	assert.Equal(t, "500", browserQueryCmd.Flags().Lookup("max-length").DefValue)
}

// useBrowserDeps installs deps with the given browser manager, restoring the
// previous deps in cleanup.
func useBrowserDeps(t *testing.T, m *browser.Manager) *internalmcp.Deps {
	t.Helper()
	prev := internalmcp.GetDeps()
	deps := &internalmcp.Deps{Browser: m}
	internalmcp.SetDeps(deps)
	t.Cleanup(func() { internalmcp.SetDeps(prev) })
	return deps
}

func TestBrowserPersistentPreRunKeepsExistingManager(t *testing.T) {
	existing := browser.NewManager("", 0, "", true)
	deps := useBrowserDeps(t, existing)

	require.NoError(t, browserCmd.PersistentPreRunE(browserCmd, nil))
	assert.Same(t, existing, deps.Browser)
}

func TestBrowserPersistentPreRunAssignsManager(t *testing.T) {
	deps := useBrowserDeps(t, nil)

	// The manager is only constructed; nothing launches Chrome until a tool
	// uses it.
	require.NoError(t, browserCmd.PersistentPreRunE(browserCmd, nil))
	assert.NotNil(t, deps.Browser)
}
