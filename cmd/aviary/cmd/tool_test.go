package cmd

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	internalmcp "github.com/lsegal/aviary/internal/mcp"
	"github.com/lsegal/aviary/internal/store"
)

func TestParseToolArgs(t *testing.T) {
	args, err := parseToolArgs(`{"message":"hi","count":2}`)
	require.NoError(t, err)
	assert.Equal(t, "hi", args["message"])
	assert.Equal(t, float64(2), args["count"])
}

func TestParseToolArgs_RejectsNonObject(t *testing.T) {
	_, err := parseToolArgs(`["x"]`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JSON object")
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

func TestParseToolInvocationArgs_RejectsObjectFlags(t *testing.T) {
	tool := internalmcp.ToolInfo{
		Name: "complex",
		InputSchema: map[string]any{
			"properties": map[string]any{
				"config": map[string]any{"type": "object"},
			},
		},
	}

	_, err := parseToolInvocationArgs(tool, []string{"--config", "{}"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "use --args")
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
