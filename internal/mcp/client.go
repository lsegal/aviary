package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/buildinfo"
)

// ToolInfo is a minimal MCP tool descriptor used for dynamic discovery.
type ToolInfo struct {
	Name        string
	Description string
}

// Client is the interface for calling Aviary MCP tools.
type Client interface {
	// ListTools returns all discoverable MCP tools from the connected server.
	ListTools(ctx context.Context) ([]ToolInfo, error)
	// CallTool invokes an MCP tool by name with the given arguments.
	CallTool(ctx context.Context, name string, args any) (*sdkmcp.CallToolResult, error)
	// CallToolText invokes an MCP tool and concatenates text content in the result.
	CallToolText(ctx context.Context, name string, args any) (string, error)
	// Close releases any resources held by the client.
	Close() error
}

// InProcessClient calls an MCP server directly via an in-memory transport,
// bypassing the network entirely.
type InProcessClient struct {
	server  *sdkmcp.Server
	session *sdkmcp.ClientSession
	cancel  context.CancelFunc
}

// NewInProcessClient creates a client connected directly to srv.
// The caller must call Close() when done.
func NewInProcessClient(ctx context.Context, srv *sdkmcp.Server) (*InProcessClient, error) {
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()

	connCtx, cancel := context.WithCancel(ctx)

	// Start the server side of the in-memory connection.
	go func() {
		if _, err := srv.Connect(connCtx, serverTransport, nil); err != nil {
			// Connection closed; normal on shutdown.
			_ = err
		}
	}()

	// Connect the client side.
	c := sdkmcp.NewClient(&sdkmcp.Implementation{
		Name:    "aviary-cli",
		Version: buildinfo.Version,
	}, nil)

	session, err := c.Connect(connCtx, clientTransport, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("connecting in-process client: %w", err)
	}

	return &InProcessClient{
		server:  srv,
		session: session,
		cancel:  cancel,
	}, nil
}

// CallTool invokes the named tool on the in-process server.
func (c *InProcessClient) CallTool(ctx context.Context, name string, args any) (*sdkmcp.CallToolResult, error) {
	logToolCall("inprocess", name, args)
	return c.session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
}

// ListTools returns discoverable tools from the in-process server.
func (c *InProcessClient) ListTools(ctx context.Context) ([]ToolInfo, error) {
	res, err := c.session.ListTools(ctx, &sdkmcp.ListToolsParams{})
	if err != nil {
		return nil, err
	}
	out := make([]ToolInfo, 0, len(res.Tools))
	for _, t := range res.Tools {
		if t == nil {
			continue
		}
		out = append(out, ToolInfo{Name: t.Name, Description: t.Description})
	}
	return out, nil
}

// CallToolText invokes a tool and returns concatenated text content.
func (c *InProcessClient) CallToolText(ctx context.Context, name string, args any) (string, error) {
	result, err := c.CallTool(ctx, name, args)
	if err != nil {
		return "", err
	}
	return extractText(result), nil
}

// Close shuts down the in-process connection.
func (c *InProcessClient) Close() error {
	c.cancel()
	return c.session.Close()
}

// extractText returns concatenated text content from a CallToolResult.
func extractText(result *sdkmcp.CallToolResult) string {
	if result == nil {
		return ""
	}
	var out []byte
	for _, c := range result.Content {
		data, _ := json.Marshal(c)
		var m map[string]any
		if err := json.Unmarshal(data, &m); err == nil {
			if t, ok := m["text"].(string); ok {
				out = append(out, []byte(t)...)
			}
		}
	}
	return string(out)
}
