package mcp

import (
	"context"

	"github.com/lsegal/aviary/internal/agent"
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
	out := make([]agent.ToolInfo, 0, len(tools))
	for _, t := range tools {
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
