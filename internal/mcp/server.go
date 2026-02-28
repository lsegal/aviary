// Package mcp implements the MCP server and client dispatch for Aviary.
package mcp

import (
	"context"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewServer creates and configures an MCP server with all Aviary tools registered.
func NewServer() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "aviary",
		Version: "0.1.0",
	}, nil)

	// Placeholder tool — replaced in Phase 3 with all domain operations.
	mcp.AddTool(s, &mcp.Tool{
		Name:        "ping",
		Description: "Check server connectivity",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, struct{}, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "pong"}},
		}, struct{}{}, nil
	})

	return s
}

// HTTPHandler returns an http.Handler for the MCP server using the
// Streamable HTTP transport (MCP spec compliant).
func HTTPHandler(s *mcp.Server) http.Handler {
	return mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return s
	}, nil)
}
