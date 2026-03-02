// Package mcp implements the MCP server and client dispatch for Aviary.
package mcp

import (
	"bytes"
	"io"
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
	base := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return s
	}, nil)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.Body != nil {
			body, err := io.ReadAll(r.Body)
			if err == nil {
				r.Body = io.NopCloser(bytes.NewReader(body))
				if name, args, ok := extractToolCallFromPayload(body); ok {
					logToolCall("http", name, args)
				}
			}
		}
		base.ServeHTTP(w, r)
	})
}
