package mcp

import (
	"context"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Client is the interface for calling Aviary MCP tools.
type Client interface {
	// CallTool invokes an MCP tool by name with the given arguments.
	CallTool(ctx context.Context, name string, args any) (*sdkmcp.CallToolResult, error)
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
		Version: "0.1.0",
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
	return c.session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
}

// Close shuts down the in-process connection.
func (c *InProcessClient) Close() error {
	c.cancel()
	return c.session.Close()
}
