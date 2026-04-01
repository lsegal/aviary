package mcp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/buildinfo"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/store"
)

// RemoteClient calls an MCP server over HTTPS using the Streamable HTTP transport.
type RemoteClient struct {
	session *sdkmcp.ClientSession
	cancel  context.CancelFunc
}

// NewRemoteClient connects to the Aviary server at serverURL using the given token.
// The URL should be the base server URL (e.g. "https://localhost:16677"); /mcp is appended.
func NewRemoteClient(ctx context.Context, serverURL, token string) (*RemoteClient, error) {
	httpTransport, err := newRemoteHTTPTransport(serverURL)
	if err != nil {
		return nil, err
	}
	httpClient := &http.Client{Transport: httpTransport}

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

func newRemoteHTTPTransport(serverURL string) (*http.Transport, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(serverURL)), "https://") {
		return transport, nil
	}

	rootCAs, err := loadRemoteServerCAPool()
	if err != nil {
		return nil, fmt.Errorf("loading TLS roots for %s: %w", serverURL, err)
	}
	if transport.TLSClientConfig != nil {
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	} else {
		transport.TLSClientConfig = &tls.Config{}
	}
	transport.TLSClientConfig.RootCAs = rootCAs
	return transport, nil
}

func loadRemoteServerCAPool() (*x509.CertPool, error) {
	rootCAs, err := x509.SystemCertPool()
	if err != nil || rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}

	certPath, err := remoteServerCertPath()
	if err != nil {
		return nil, err
	}
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("reading certificate %q: %w", certPath, err)
	}
	if ok := rootCAs.AppendCertsFromPEM(certPEM); !ok {
		return nil, fmt.Errorf("parsing certificate %q", certPath)
	}
	return rootCAs, nil
}

func remoteServerCertPath() (string, error) {
	cfg, err := config.Load("")
	if err != nil {
		return "", fmt.Errorf("loading config: %w", err)
	}
	if cfg != nil && cfg.Server.TLS != nil && strings.TrimSpace(cfg.Server.TLS.Cert) != "" {
		return resolveConfigPath(cfg.Server.TLS.Cert), nil
	}
	return filepath.Join(store.SubDir(store.DirCerts), "cert.pem"), nil
}

func resolveConfigPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(config.BaseDir(), path)
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
