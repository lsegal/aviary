package mcp

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/buildinfo"
)

// RemoteClient calls an MCP server over HTTPS using the Streamable HTTP transport.
type RemoteClient struct {
	session *sdkmcp.ClientSession
	cancel  context.CancelFunc
}

// NewRemoteClient connects to the Aviary server at serverURL using the given token.
// The URL should be the base server URL (e.g. "https://localhost:16677"); /mcp is appended.
func NewRemoteClient(ctx context.Context, serverURL, token string) (*RemoteClient, error) {
	// Use an HTTP client that skips TLS verification for self-signed certs.
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}

	// Attach bearer token to all requests.
	transport := &bearerTransport{
		base:  httpClient.Transport,
		token: token,
	}
	httpClient.Transport = transport

	endpoint := serverURL + "/mcp"
	t := &sdkmcp.StreamableClientTransport{
		Endpoint:             endpoint,
		HTTPClient:           httpClient,
		DisableStandaloneSSE: true, // avoids extra GET connection for simple CLI calls
	}

	connCtx, cancel := context.WithCancel(ctx)

	c := sdkmcp.NewClient(&sdkmcp.Implementation{
		Name:    "aviary-cli",
		Version: buildinfo.Version,
	}, nil)

	session, err := c.Connect(connCtx, t, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("connecting to %s: %w", endpoint, err)
	}

	return &RemoteClient{
		session: session,
		cancel:  cancel,
	}, nil
}

// CallTool invokes the named tool on the remote server.
func (c *RemoteClient) CallTool(ctx context.Context, name string, args any) (*sdkmcp.CallToolResult, error) {
	logToolCall("remote", name, args)
	return c.session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
}

// ListTools returns discoverable tools from the remote server.
func (c *RemoteClient) ListTools(ctx context.Context) ([]ToolInfo, error) {
	res, err := c.session.ListTools(ctx, &sdkmcp.ListToolsParams{})
	if err != nil {
		return nil, err
	}
	out := make([]ToolInfo, 0, len(res.Tools))
	for _, t := range res.Tools {
		if t == nil {
			continue
		}
		out = append(out, ToolInfo{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
	}
	return out, nil
}

// CallToolText invokes a tool and returns concatenated text content.
func (c *RemoteClient) CallToolText(ctx context.Context, name string, args any) (string, error) {
	result, err := c.CallTool(ctx, name, args)
	if err != nil {
		return "", err
	}
	return extractText(result), nil
}

// Close shuts down the remote connection.
func (c *RemoteClient) Close() error {
	c.cancel()
	return c.session.Close()
}

// bearerTransport injects an Authorization header into every request.
type bearerTransport struct {
	base  http.RoundTripper
	token string
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(r)
}
