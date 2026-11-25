package mcp

import (
	"context"
	"sort"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
)

// setupLiveServerTest isolates config and the shared skills directory, and
// restores the live server and deps when the test ends.
func setupLiveServerTest(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	oldDeps := GetDeps()
	t.Cleanup(func() { SetDeps(oldDeps) })
	SetDeps(&Deps{Agents: agent.NewManager(nil)})

	t.Cleanup(func() { SetLiveServer(nil) })
}

func liveServerToolNames(t *testing.T, srv *sdkmcp.Server) []string {
	t.Helper()
	client, err := NewInProcessClient(context.Background(), srv)
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	tools, err := client.ListTools(context.Background())
	require.NoError(t, err)
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}

func enabledSkillsConfig() *config.Config {
	return &config.Config{
		Skills: map[string]config.SkillConfig{
			"gogcli":   {Enabled: true},
			"himalaya": {Enabled: true},
		},
	}
}

func TestSyncLiveServerWithoutLiveServerIsNoOp(t *testing.T) {
	setupLiveServerTest(t)
	SetLiveServer(nil)

	srv := NewServer()
	before := liveServerToolNames(t, srv)

	assert.NotPanics(t, func() { SyncLiveServer(enabledSkillsConfig()) })

	after := liveServerToolNames(t, srv)
	assert.Equal(t, before, after)
	assert.NotContains(t, after, gogcliToolName)
	assert.NotContains(t, after, himalayaToolName)
}

func TestRefreshLiveServerWithoutLiveServerIsNoOp(t *testing.T) {
	setupLiveServerTest(t)
	SetLiveServer(nil)

	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test"}, nil)
	assert.NotPanics(t, RefreshLiveServer)

	// A server with no tools never registered any; adding one proves the
	// list is otherwise reachable and that refresh did not touch it.
	sdkmcp.AddTool(srv, &sdkmcp.Tool{Name: "probe"}, func(context.Context, *sdkmcp.CallToolRequest, struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		return &sdkmcp.CallToolResult{}, struct{}{}, nil
	})
	assert.Equal(t, []string{"probe"}, liveServerToolNames(t, srv))
}

func TestSyncLiveServerAddsAndRemovesSkillTools(t *testing.T) {
	setupLiveServerTest(t)

	srv := NewServer()
	SetLiveServer(srv)

	names := liveServerToolNames(t, srv)
	assert.NotContains(t, names, gogcliToolName)
	assert.NotContains(t, names, himalayaToolName)

	SyncLiveServer(enabledSkillsConfig())
	names = liveServerToolNames(t, srv)
	assert.Contains(t, names, gogcliToolName)
	assert.Contains(t, names, himalayaToolName)
	assert.Contains(t, names, "config_save", "syncing skills must keep the other tools")

	SyncLiveServer(&config.Config{
		Skills: map[string]config.SkillConfig{
			"gogcli":   {Enabled: false},
			"himalaya": {Enabled: true},
		},
	})
	names = liveServerToolNames(t, srv)
	assert.NotContains(t, names, gogcliToolName)
	assert.Contains(t, names, himalayaToolName)

	SyncLiveServer(&config.Config{})
	names = liveServerToolNames(t, srv)
	assert.NotContains(t, names, gogcliToolName)
	assert.NotContains(t, names, himalayaToolName)
	assert.Contains(t, names, "config_save")
}

func TestRefreshLiveServerRegistersEveryTool(t *testing.T) {
	setupLiveServerTest(t)

	want := liveServerToolNames(t, NewServer())
	require.NotEmpty(t, want)

	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test"}, nil)
	SetLiveServer(srv)
	RefreshLiveServer()

	assert.Equal(t, want, liveServerToolNames(t, srv))

	// Refreshing again replaces tools by name rather than duplicating them.
	RefreshLiveServer()
	assert.Equal(t, want, liveServerToolNames(t, srv))
}
