package cmd

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/projects/agents"
)

func TestWriteProjectsAgentGates_ListsAgy(t *testing.T) {
	registry, err := agents.Load(nil)
	require.NoError(t, err)
	var out strings.Builder
	writeProjectsAgentGates(&out, registry)
	assert.Contains(t, out.String(), "agy")
	assert.Contains(t, out.String(), "unavailable for Projects: agy has no machine-readable output mode")
	assert.NotContains(t, out.String(), "claude")
}

func TestWriteProjectsAgentGates_UngatedByConfig(t *testing.T) {
	registry, err := agents.Load(map[string]any{"agy": map[string]any{"projectsUnavailable": nil}})
	require.NoError(t, err)
	var out strings.Builder
	writeProjectsAgentGates(&out, registry)
	assert.Equal(t, "  (every agent can run Projects jobs)\n", out.String())
}
