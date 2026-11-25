package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	internalmcp "github.com/lsegal/aviary/internal/mcp"
	"github.com/lsegal/aviary/internal/server"
	"github.com/lsegal/aviary/internal/store"
)

func TestParseToolArgs(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    map[string]any
		wantErr string
	}{
		{name: "object", raw: `{"message":"hi","count":2}`, want: map[string]any{"message": "hi", "count": float64(2)}},
		{name: "blank is empty", raw: "  ", want: map[string]any{}},
		{name: "null is empty", raw: "null", want: map[string]any{}},
		{name: "non-object", raw: `["x"]`, wantErr: "--args must be a JSON object"},
		{name: "invalid JSON", raw: `{"x"`, wantErr: "parsing --args as JSON"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := parseToolArgs(tt.raw)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, args)
		})
	}
}

func TestParseToolInvocationArgs_FlatFlags(t *testing.T) {
	tool := internalmcp.ToolInfo{
		Name: "browser_wait",
		InputSchema: map[string]any{
			"required": []any{"tab_id", "selector"},
			"properties": map[string]any{
				"tab_id":       map[string]any{"type": "string"},
				"selector":     map[string]any{"type": "string"},
				"timeout_ms":   map[string]any{"type": "integer"},
				"include_html": map[string]any{"type": "boolean"},
				"tags": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "string"},
				},
			},
		},
	}

	args, err := parseToolInvocationArgs(tool, []string{
		"--tab_id", "tab-1",
		"--selector=#ready",
		"--timeout_ms", "2500",
		"--include_html",
		"--tags", "a,b,c",
	})
	require.NoError(t, err)
	assert.Equal(t, "tab-1", args["tab_id"])
	assert.Equal(t, "#ready", args["selector"])
	assert.Equal(t, 2500, args["timeout_ms"])
	assert.Equal(t, true, args["include_html"])
	assert.Equal(t, []any{"a", "b", "c"}, args["tags"])
}

func TestParseToolInvocationArgs_MergesArgsAndFlags(t *testing.T) {
	tool := internalmcp.ToolInfo{
		Name: "browser_open",
		InputSchema: map[string]any{
			"required": []any{"url"},
			"properties": map[string]any{
				"url": map[string]any{"type": "string"},
			},
		},
	}

	args, err := parseToolInvocationArgs(tool, []string{
		"--args", `{"url":"https://old.example","extra":"x"}`,
		"--url", "https://new.example",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://new.example", args["url"])
	assert.Equal(t, "x", args["extra"])
}

func TestParseToolInvocationArgs_Errors(t *testing.T) {
	tool := internalmcp.ToolInfo{
		Name: "complex",
		InputSchema: map[string]any{
			"required": []string{"name"},
			"properties": map[string]any{
				"name":   map[string]any{"type": "string"},
				"config": map[string]any{"type": "object"},
				"limit":  map[string]any{"type": "integer"},
			},
		},
	}

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "object flag", args: []string{"--name", "n", "--config", "{}"}, wantErr: "--config is not supported as a flat flag; use --args"},
		{name: "positional argument", args: []string{"stray"}, wantErr: `unexpected argument "stray"; use --<field> <value>`},
		{name: "args without value", args: []string{"--args"}, wantErr: "--args requires a JSON object value"},
		{name: "invalid args JSON", args: []string{"--args={"}, wantErr: "parsing --args as JSON"},
		{name: "unknown flag", args: []string{"--nope", "x"}, wantErr: `unknown flag --nope for tool "complex"`},
		{name: "flag without value", args: []string{"--name"}, wantErr: "--name requires a value"},
		{name: "invalid value", args: []string{"--name", "n", "--limit", "many"}, wantErr: "--limit must be an integer"},
		{name: "required flag missing", args: []string{"--limit", "3"}, wantErr: "--name is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseToolInvocationArgs(tool, tt.args)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestParseToolInvocationArgs_InlineArgsAndHelpTokens(t *testing.T) {
	tool := internalmcp.ToolInfo{Name: "plain"}

	args, err := parseToolInvocationArgs(tool, []string{"help", `--args={"k":"v"}`, "-h"})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"k": "v"}, args, "help tokens are skipped and --args=JSON is merged")
}

func TestParseToolFieldValue(t *testing.T) {
	tests := []struct {
		name    string
		field   toolField
		value   string
		want    any
		wantErr string
	}{
		{name: "untyped is string", field: toolField{Name: "f"}, value: "x", want: "x"},
		{name: "number", field: toolField{Name: "f", Type: "number"}, value: "1.5", want: 1.5},
		{name: "bad number", field: toolField{Name: "f", Type: "number"}, value: "x", wantErr: "--f must be a number"},
		{name: "boolean", field: toolField{Name: "f", Type: "boolean"}, value: "false", want: false},
		{name: "bad boolean", field: toolField{Name: "f", Type: "boolean"}, value: "maybe", wantErr: "--f must be true or false"},
		{name: "integer array", field: toolField{Name: "f", Type: "array", ItemType: "integer"}, value: " 1, ,2 ", want: []any{1, 2}},
		{name: "blank array", field: toolField{Name: "f", Type: "array", ItemType: "string"}, value: " ", want: []any{}},
		{name: "bad array item", field: toolField{Name: "f", Type: "array", ItemType: "integer"}, value: "1,x", wantErr: "--f must be an integer"},
		{name: "unsupported type", field: toolField{Name: "f", Type: "binary"}, value: "x", wantErr: `--f uses unsupported type "binary"; use --args`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseToolFieldValue(tt.field, tt.value)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestToolInputFields(t *testing.T) {
	t.Run("schema that is not an object has no fields", func(t *testing.T) {
		assert.Empty(t, toolInputFields(internalmcp.ToolInfo{InputSchema: "nope"}))
	})

	fields := toolInputFields(internalmcp.ToolInfo{InputSchema: map[string]any{
		"required": []string{"id"},
		"properties": map[string]any{
			"id":       map[string]any{"type": "string", "description": "  The ID.  "},
			"bogus":    "not a schema",
			"optional": map[string]any{"type": []any{"null", "integer"}},
			"ids":      map[string]any{"items": map[string]any{"description": "untyped"}},
		},
	}})
	assert.Equal(t, map[string]toolField{
		"id": {
			Name: "id", Required: true, Type: "string", ItemType: "string", Description: "The ID.",
			Schema: map[string]any{"type": "string", "description": "  The ID.  "},
		},
		"optional": {
			Name: "optional", Type: "integer", ItemType: "string",
			Schema: map[string]any{"type": []any{"null", "integer"}},
		},
		"ids": {
			Name: "ids", Type: "array", ItemType: "string",
			Schema: map[string]any{"items": map[string]any{"description": "untyped"}},
		},
	}, fields, "required may be a []string, non-object properties are skipped, and untyped items default to string")
}

func TestSchemaType(t *testing.T) {
	tests := []struct {
		name   string
		schema map[string]any
		want   string
	}{
		{name: "string", schema: map[string]any{"type": "number"}, want: "number"},
		{name: "null only", schema: map[string]any{"type": "null"}, want: ""},
		{name: "nullable any list", schema: map[string]any{"type": []any{"null", 3, "boolean"}}, want: "boolean"},
		{name: "nullable string list", schema: map[string]any{"type": []string{"null", "integer"}}, want: "integer"},
		{name: "all-null string list", schema: map[string]any{"type": []string{"null"}}, want: ""},
		{name: "items imply array", schema: map[string]any{"type": "null", "items": map[string]any{}}, want: "array"},
		{name: "no type", schema: map[string]any{}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, schemaType(tt.schema))
		})
	}
}

func TestWriteSingleToolHelp_IncludesFieldFlags(t *testing.T) {
	tool := internalmcp.ToolInfo{
		Name:        "skill_gogcli",
		Description: "Run gog.",
		InputSchema: map[string]any{
			"required": []any{"command"},
			"properties": map[string]any{
				"command": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "string"},
				},
				"account": map[string]any{"type": "string"},
			},
		},
	}

	var out bytes.Buffer
	require.NoError(t, writeSingleToolHelp(&out, tool))

	text := out.String()
	assert.Contains(t, text, "aviary tool skill_gogcli")
	assert.Contains(t, text, "--command <a,b,c>")
	assert.Contains(t, text, "--command")
	assert.Contains(t, text, "string list required")
}

func TestWriteToolHelp_DynamicCatalog(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	require.NoError(t, store.EnsureDirs())
	require.NoError(t, config.Save("", &config.Config{}))

	oldDispatcher := dispatcher
	oldDeps := internalmcp.GetDeps()
	t.Cleanup(func() {
		dispatcher = oldDispatcher
		internalmcp.SetDeps(oldDeps)
	})

	internalmcp.SetDeps(&internalmcp.Deps{Agents: agent.NewManager(nil)})
	dispatcher = internalmcp.NewDispatcher("https://localhost:16677", "")

	var out bytes.Buffer
	require.NoError(t, writeToolHelp(context.Background(), &out))

	text := out.String()
	assert.Contains(t, text, "aviary tool <name> --field value")
	assert.Contains(t, text, "Arrays use comma-separated values")
	assert.Contains(t, text, "ping")
	assert.Contains(t, text, "skills_list")
}

// startToolServer serves a running Aviary server's MCP endpoint with an echo
// tool, which returns its arguments as JSON, a tool with no description, and a
// tool that fails, and points the CLI's dispatcher at it.
func startToolServer(t *testing.T) string {
	t.Helper()
	s := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fake-aviary"}, nil)
	s.AddTool(&sdkmcp.Tool{
		Name:        "echo",
		Description: "Echo the arguments.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []any{"message"},
			"properties": map[string]any{
				"message": map[string]any{"type": "string", "description": "What to echo."},
				"count":   map[string]any{"type": "integer"},
			},
		},
	}, func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(req.Params.Arguments)}}}, nil
	})
	s.AddTool(&sdkmcp.Tool{Name: "blank", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return &sdkmcp.CallToolResult{}, nil
	})
	s.AddTool(&sdkmcp.Tool{Name: "fail", Description: "Always fails.", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return nil, errors.New("tool exploded")
	})
	mux := http.NewServeMux()
	mux.Handle("/mcp", sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return s }, &sdkmcp.StreamableHTTPOptions{JSONResponse: true}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	useRemoteDispatcher(t, srv.URL)
	return srv.URL
}

// useRemoteDispatcher points the CLI's dispatcher at a running server at url.
func useRemoteDispatcher(t *testing.T, url string) {
	t.Helper()
	prev := dispatcher
	dispatcher = internalmcp.NewDispatcher(url, "test-token")
	internalmcp.SetServerChecker(func() bool { return true })
	t.Cleanup(func() {
		dispatcher = prev
		internalmcp.SetServerChecker(func() bool { running, _, _ := server.IsRunning(); return running })
	})
}

// runToolCmd runs aviary tool with args and returns its output.
func runToolCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	toolCmd.SetOut(&out)
	toolCmd.SetContext(context.Background())
	t.Cleanup(func() { toolCmd.SetOut(nil) })
	err := toolCmd.RunE(toolCmd, args)
	return out.String(), err
}

func TestToolCmd(t *testing.T) {
	startToolServer(t)

	tests := []struct {
		name     string
		args     []string
		want     []string
		wantNot  []string
		wantErr  string
		wantJSON map[string]any
	}{
		{name: "no args shows help", want: []string{"Usage:", "Available Tools:", "echo", "Echo the arguments."}},
		{name: "help shows help", args: []string{"--help"}, want: []string{"aviary tool list", "Available Tools:"}},
		{name: "list", args: []string{"list"}, want: []string{"blank", "No description available.", "echo", "Echo the arguments."}, wantNot: []string{"Usage:"}},
		{name: "unknown tool", args: []string{"missing"}, wantErr: `tool "missing" is not available`},
		{name: "tool help", args: []string{"echo", "--count", "2", "--help"}, want: []string{"aviary tool echo [--count <value>] --message <value>", "What to echo."}},
		{name: "invalid flags", args: []string{"echo", "stray"}, wantErr: `unexpected argument "stray"`},
		{name: "call", args: []string{"echo", "--message", "hi", "--count=2"}, wantJSON: map[string]any{"message": "hi", "count": float64(2)}},
		{name: "failed call", args: []string{"fail"}, wantErr: "tool exploded"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := runToolCmd(t, tt.args...)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			for _, want := range tt.want {
				assert.Contains(t, out, want)
			}
			for _, notWant := range tt.wantNot {
				assert.NotContains(t, out, notWant)
			}
			if tt.wantJSON != nil {
				var got map[string]any
				require.NoError(t, json.Unmarshal([]byte(out), &got), "output: %q", out)
				assert.Equal(t, tt.wantJSON, got, "flags reach the tool as typed arguments")
			}
		})
	}
}

func TestToolCmdHelpFunc(t *testing.T) {
	startToolServer(t)

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "known tool shows its flags", args: []string{"echo"}, want: "aviary tool echo [--count <value>] --message <value>"},
		{name: "unknown tool shows the catalog", args: []string{"missing"}, want: "Available Tools:"},
		{name: "help arg shows the catalog", args: []string{"help"}, want: "Available Tools:"},
		{name: "no args shows the catalog", want: "Available Tools:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			toolCmd.SetOut(&out)
			toolCmd.SetContext(context.Background())
			t.Cleanup(func() { toolCmd.SetOut(nil) })
			toolCmd.HelpFunc()(toolCmd, tt.args)
			assert.Contains(t, out.String(), tt.want)
		})
	}
}

func TestActiveDispatcherUsesConfiguredServer(t *testing.T) {
	url := startToolServer(t)
	prevURL, prevToken, prevDataDir := serverURL, token, dataDir
	prevStoreDir := store.DataDir()
	t.Cleanup(func() {
		serverURL, token, dataDir = prevURL, prevToken, prevDataDir
		store.SetDataDir(prevStoreDir)
	})
	dispatcher = nil
	serverURL, token, dataDir = url, "test-token", t.TempDir()

	tools, err := activeDispatcher().ListTools(context.Background())
	require.NoError(t, err)
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	assert.ElementsMatch(t, []string{"echo", "blank", "fail"}, names, "the dispatcher targets --server")
	assert.Equal(t, dataDir, store.DataDir(), "--data-dir applies to the store")
}

func TestWriteToolHelp_ReportsAnUnavailableCatalog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	useRemoteDispatcher(t, srv.URL)

	var out bytes.Buffer
	require.NoError(t, writeToolHelp(context.Background(), &out))
	assert.Contains(t, out.String(), "Available Tools:\n  unavailable: ")
}

var errToolWrite = errors.New("write failed")

// failingWriter fails every write after the first ok writes.
type failingWriter struct{ ok, writes int }

func (w *failingWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes > w.ok {
		return 0, errToolWrite
	}
	return len(p), nil
}

// The help writers stop at, and return, the first write that fails, wherever
// it happens.
func TestToolHelpWritersReturnWriteErrors(t *testing.T) {
	startToolServer(t)
	described := internalmcp.ToolInfo{Name: "echo", Description: "Echo.", InputSchema: map[string]any{
		"required": []any{"message"},
		"properties": map[string]any{
			"message": map[string]any{"type": "string", "description": "What to echo."},
			"loud":    map[string]any{"type": "boolean"},
			"tags":    map[string]any{"type": "array"},
		},
	}}
	bare := internalmcp.ToolInfo{Name: "bare"}

	writers := map[string]func(io.Writer) error{
		"tool help":         func(w io.Writer) error { return writeToolHelp(context.Background(), w) },
		"catalog":           func(w io.Writer) error { return writeToolCatalog(context.Background(), w) },
		"single tool help":  func(w io.Writer) error { return writeSingleToolHelp(w, described) },
		"tool without docs": func(w io.Writer) error { return writeSingleToolHelp(w, bare) },
	}
	for name, write := range writers {
		t.Run(name, func(t *testing.T) {
			all := &failingWriter{ok: 1 << 30}
			require.NoError(t, write(all))
			require.Positive(t, all.writes)
			for ok := 0; ok < all.writes; ok++ {
				w := &failingWriter{ok: ok}
				require.ErrorIs(t, write(w), errToolWrite, "failing write %d", ok+1)
				assert.Equal(t, ok+1, w.writes, "nothing is written after write %d fails", ok+1)
			}
		})
	}

	t.Run("unavailable catalog", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		t.Cleanup(srv.Close)
		useRemoteDispatcher(t, srv.URL)
		all := &failingWriter{ok: 1 << 30}
		require.NoError(t, writeToolHelp(context.Background(), all))
		w := &failingWriter{ok: all.writes - 1}
		require.ErrorIs(t, writeToolHelp(context.Background(), w), errToolWrite, "the unavailable note's write error is returned")
	})
}

func TestWriteSingleToolHelp_ToolWithoutFlags(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, writeSingleToolHelp(&out, internalmcp.ToolInfo{Name: "bare"}))
	assert.Equal(t, "bare\n\nUsage:\n  aviary tool bare [--args '{...}']\n\nThis tool does not declare any input flags.\n", out.String())
}
