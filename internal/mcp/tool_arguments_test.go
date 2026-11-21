package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// toolArgumentsFile is the e2e mock server's copy of every tool's declared
// arguments, so it rejects arguments the real server would reject.
var toolArgumentsFile = filepath.Join("..", "..", "web", "e2e", "helpers", "toolArguments.json")

// TestToolArgumentsFileIsCurrent keeps web/e2e/helpers/toolArguments.json in
// step with the real tool schemas. Regenerate it with
// AVIARY_UPDATE_TOOL_ARGUMENTS=1 go test ./internal/mcp -run TestToolArgumentsFileIsCurrent.
func TestToolArgumentsFileIsCurrent(t *testing.T) {
	c, err := NewInProcessClient(context.Background(), NewServer())
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck

	tools, err := c.ListTools(context.Background())
	require.NoError(t, err)

	args := map[string][]string{}
	for _, tool := range tools {
		data, err := json.Marshal(tool.InputSchema)
		require.NoError(t, err)
		var schema struct {
			Properties           map[string]json.RawMessage `json:"properties"`
			AdditionalProperties any                        `json:"additionalProperties"`
		}
		require.NoError(t, json.Unmarshal(data, &schema))
		// The mock rejects every undeclared argument, as the server does
		// only for a schema that forbids them.
		require.Equal(t, false, schema.AdditionalProperties, "%s accepts undeclared arguments", tool.Name)
		names := make([]string, 0, len(schema.Properties))
		for name := range schema.Properties {
			names = append(names, name)
		}
		sort.Strings(names)
		args[tool.Name] = names
	}
	want, err := json.MarshalIndent(args, "", "\t")
	require.NoError(t, err)
	want = append(want, '\n')

	if os.Getenv("AVIARY_UPDATE_TOOL_ARGUMENTS") != "" {
		require.NoError(t, os.WriteFile(toolArgumentsFile, want, 0o644))
		return
	}
	got, err := os.ReadFile(toolArgumentsFile)
	require.NoError(t, err)
	// A Windows checkout may carry CRLF line endings.
	assert.Equal(t, string(want), strings.ReplaceAll(string(got), "\r\n", "\n"), "tool arguments changed; regenerate %s with AVIARY_UPDATE_TOOL_ARGUMENTS=1", toolArgumentsFile)
}
