package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
)

// cliNames returns the names provider_cli_list reported, failing the test on
// any CLI the empty PATH should have left uninstalled.
func cliNames(t *testing.T, result map[string]any) []string {
	t.Helper()
	clis, ok := result["clis"].([]any)
	require.True(t, ok, result)
	var names []string
	for _, raw := range clis {
		cli := raw.(map[string]any)
		names = append(names, cli["name"].(string))
		assert.Equal(t, false, cli["installed"], cli["name"])
		assert.Equal(t, "missing", cli["status"], cli["name"])
	}
	return names
}

func TestProviderCLIListReportsEveryDefinition(t *testing.T) {
	d, _ := setupProjects(t)
	t.Setenv("PATH", t.TempDir())
	names := cliNames(t, callJSON(t, d, "provider_cli_list", map[string]any{}))
	for _, want := range []string{"agy", "claude", "cline", "codex", "gemini", "muse", "opencode"} {
		assert.Contains(t, names, want)
	}
	// Aviary runs its own agents in-process; it is not a provider (#494).
	assert.NotContains(t, names, "aviary")
}

// The CLI providers are core providers: they are listed with no Code Loop
// manager at all, and user-defined CLIs come from models.cli_definitions.
func TestProviderCLIListWithoutProjectsReadsModelsDefinitions(t *testing.T) {
	d, _ := setupProjects(t)
	SetDeps(&Deps{})
	t.Setenv("PATH", t.TempDir())
	require.NoError(t, config.Save("", &config.Config{Models: config.ModelsConfig{
		CLIDefinitions: map[string]any{"acme": map[string]any{
			"binary":  "acme-cli",
			"session": map[string]any{"assign": "none"},
			"output":  map[string]any{"format": "text"},
			"args":    map[string]any{"run": []any{map[string]any{"args": []any{"{prompt}"}}}},
		}},
	}}))
	names := cliNames(t, callJSON(t, d, "provider_cli_list", map[string]any{}))
	assert.Contains(t, names, "acme")
	assert.Contains(t, names, "codex")
}

func TestProviderCLIListRejectsInvalidDefinitions(t *testing.T) {
	d, _ := setupProjects(t)
	require.NoError(t, config.Save("", &config.Config{Models: config.ModelsConfig{
		CLIDefinitions: map[string]any{"acme": map[string]any{"name": "other"}},
	}}))
	toolCallContains(t, d, "provider_cli_list", map[string]any{}, "cli_definitions")
}
