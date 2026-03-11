package mcp

import (
	"context"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
)

type agentToolClient struct {
	client Client
}

// NewAgentToolClient creates a local in-process MCP-backed tool client for the agent loop.
func NewAgentToolClient(ctx context.Context) (agent.ToolClient, error) {
	client, err := NewInProcessClient(ctx, NewServer())
	if err != nil {
		return nil, err
	}
	return &agentToolClient{client: client}, nil
}

func (c *agentToolClient) ListTools(ctx context.Context) ([]agent.ToolInfo, error) {
	tools, err := c.client.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	preset := agentPermissionsPreset(ctx)
	out := make([]agent.ToolInfo, 0, len(tools))
	for _, t := range tools {
		if !config.IsToolAllowedByPreset(preset, t.Name) {
			continue
		}
		if t.Name == "exec" && !agentHasExecConfig(ctx) {
			continue
		}
		out = append(out, agent.ToolInfo{Name: t.Name, Description: t.Description})
	}
	return out, nil
}

func (c *agentToolClient) CallToolText(ctx context.Context, name string, args map[string]any) (string, error) {
	return c.client.CallToolText(ctx, name, args)
}

func (c *agentToolClient) Close() error {
	return c.client.Close()
}

func agentHasExecConfig(ctx context.Context) bool {
	runner := runnerForAgentContext(ctx)
	if runner == nil || runner.Config() == nil || runner.Config().Permissions == nil || runner.Config().Permissions.Exec == nil {
		return false
	}
	return len(runner.Config().Permissions.Exec.AllowedCommands) > 0
}

func agentPermissionsPreset(ctx context.Context) config.PermissionsPreset {
	runner := runnerForAgentContext(ctx)
	if runner == nil {
		return config.PermissionsPresetStandard
	}
	return config.EffectivePermissionsPreset(runner.Config().Permissions)
}

func runnerForAgentContext(ctx context.Context) *agent.AgentRunner {
	deps := GetDeps()
	if deps == nil || deps.Agents == nil {
		return nil
	}
	agentID, ok := agent.SessionAgentIDFromContext(ctx)
	if !ok || agentID == "" {
		return nil
	}
	runner, ok := deps.Agents.GetByID(agentID)
	if !ok || runner == nil {
		return nil
	}
	return runner
}
