package mcp

import (
	"context"
	"log/slog"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/projects"
	"github.com/lsegal/aviary/internal/projects/core"
)

// cliStatuses runs the coding-CLI doctor. It is a variable so tests can
// answer for a machine whose CLIs they do not control.
var cliStatuses = projects.CLIStatuses

// registerProviderTools registers the tools that describe how Aviary reaches
// models beyond stored credentials. The coding CLIs are core providers: the
// Providers settings and Projects both read them from here.
func registerProviderTools(s *sdkmcp.Server) {
	addTool(s, &sdkmcp.Tool{
		Name:        "provider_cli_list",
		Description: "The coding-CLI providers (codex, claude, gemini, ... and any in models.cli_definitions): for each, whether it is installed and where, its version, sign-in, quota, and the models it reports",
		Annotations: readOnlyTool,
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "providers", "tool", "provider_cli_list")
		cfg, err := config.Load("")
		if err != nil {
			return nil, struct{}{}, err
		}
		statuses, err := cliStatuses(ctx, cfg.Models.CLIDefinitions)
		if err != nil {
			return nil, struct{}{}, err
		}
		if statuses == nil {
			statuses = []core.AgentStatus{}
		}
		return jsonResult(map[string]any{"clis": statuses})
	})
}
