package cmd

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/clidoctor"
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

func TestDescribeCLIBackend(t *testing.T) {
	ok, line := describeCLIBackend(clidoctor.Report{Name: "codex", Binary: "codex", Auth: clidoctor.Unknown})
	assert.False(t, ok)
	assert.Equal(t, "codex CLI not found in PATH", line)

	ok, line = describeCLIBackend(clidoctor.Report{Binary: "claude", Path: "/opt/claude", Version: "2.1.312 (Claude Code)", Auth: clidoctor.SignedIn, AuthDetail: "claude.ai"})
	assert.True(t, ok)
	assert.Equal(t, "claude 2.1.312, signed in (claude.ai)", line)

	ok, line = describeCLIBackend(clidoctor.Report{Binary: "claude", Path: "/opt/claude", Version: "2.1", Auth: clidoctor.SignedOut})
	assert.False(t, ok)
	assert.Equal(t, "claude 2.1, not signed in", line)

	ok, line = describeCLIBackend(clidoctor.Report{Binary: "codex", Path: "/opt/codex", Version: "codex-cli 0.1.0", Auth: clidoctor.SignedIn, VersionNote: "older than the required 0.2.0"})
	assert.False(t, ok)
	assert.Equal(t, "codex 0.1.0, signed in; version older than the required 0.2.0", line)
}
