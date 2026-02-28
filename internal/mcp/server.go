// Package mcp implements the MCP server and client dispatch for Aviary.
package mcp

import (
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewServer creates and configures an MCP server with all Aviary tools registered.
func NewServer() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "aviary",
		Version: "0.1.0",
	}, nil)

	Register(s)

	return s
}

// HTTPHandler returns an http.Handler for the MCP server using the
// Streamable HTTP transport (MCP spec compliant).
func HTTPHandler(s *mcp.Server) http.Handler {
	return mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return s
	}, nil)
}
