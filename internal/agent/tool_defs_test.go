package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildLLMToolDefinitions_ExamplesIncludeAllRequiredFields(t *testing.T) {
	tools := []ToolInfo{{
		Name:        "session_set_target",
		Description: "Set a session target",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agent":      map[string]any{"type": "string"},
				"session_id": map[string]any{"type": "string"},
				"channel_type": map[string]any{
					"type": "string",
					"enum": []any{"slack", "discord"},
				},
				"channel_id": map[string]any{"type": "string"},
				"target":     map[string]any{"type": "string"},
			},
			"required": []any{"agent", "session_id", "channel_type", "channel_id", "target"},
		},
	}}

	defs := BuildLLMToolDefinitions(tools)
	require.Len(t, defs, 1)
	require.Len(t, defs[0].Examples, 1)

	example := defs[0].Examples[0]
	assert.Equal(t, "assistant", example["agent"])
	assert.Equal(t, "current", example["session_id"])
	assert.Equal(t, "slack", example["channel_type"])
	assert.Equal(t, "alerts", example["channel_id"])
	assert.Equal(t, "C12345678", example["target"])
	assert.Len(t, example, 5)
}
