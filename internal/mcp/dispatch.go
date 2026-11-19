package mcp

import (
	"context"
	"fmt"
	"path/filepath"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/auth"
	"github.com/lsegal/aviary/internal/browser"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/store"
)

// Dispatcher selects the right MCP client — in-process or remote —
// based on whether the Aviary server is currently running.
type Dispatcher struct {
	serverURL string
	token     string
}

// NewDispatcher creates a Dispatcher targeting the given server URL.
func NewDispatcher(serverURL, token string) *Dispatcher {
	return &Dispatcher{
		serverURL: serverURL,
		token:     token,
	}
}

// Resolve returns a connected Client.
// If the server is running (PID file present and process alive), it returns a RemoteClient.
// Otherwise it returns an InProcessClient connected to a fresh local server.
func (d *Dispatcher) Resolve(ctx context.Context) (Client, error) {
	return d.resolve(ctx, nil)
}

func (d *Dispatcher) resolve(ctx context.Context, opts *sdkmcp.ClientOptions) (Client, error) {
	if d.isServerRunning() {
		tok := d.token
		if tok == "" {
			// Try to load token from disk.
			var err error
			tok, err = loadStoredToken()
			if err != nil {
				return nil, fmt.Errorf("loading token: %w", err)
			}
		}
		return newRemoteClient(ctx, d.serverURL, tok, opts)
	}

	// Server not running — create a local in-process client.
	if err := ensureInProcessDeps(); err != nil {
		return nil, err
	}
	srv := NewServer()
	return newInProcessClient(ctx, srv, opts)
}

func ensureInProcessDeps() error {
	// If deps were explicitly set (by the server or tests), use them as-is.
	if depsSet {
		return nil
	}

	if err := store.EnsureDirs(); err != nil {
		return fmt.Errorf("ensuring data directories: %w", err)
	}

	cfg, err := config.Load("")
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	authPath := filepath.Join(store.SubDir(store.DirAuth), "credentials.json")
	authStore, _ := auth.NewFileStore(authPath)
	authResolver := func(ref string) (string, error) {
		if authStore == nil {
			return "", fmt.Errorf("auth store unavailable")
		}
		return auth.Resolve(authStore, ref)
	}

	factory := llm.NewFactory(authResolver)
	if authStore != nil {
		factory.WithTokenSetter(authStore.Set)
	}
	agents := agent.NewManager(factory)
	agents.Reconcile(cfg)

	SetDeps(&Deps{
		Agents: agents,
		Browser: browser.NewManager(
			cfg.Browser.Binary,
			cdpPortOrDefault(cfg.Browser.CDPPort),
			cfg.Browser.ProfileDir,
			cfg.Browser.Headless,
			config.EffectiveBrowserReuseTabs(cfg.Browser),
		),
		Auth: authStore,
	})

	agent.SetToolClientFactory(NewAgentToolClient)
	return nil
}

// CallTool is a convenience wrapper: resolves client, calls tool, closes client.
func (d *Dispatcher) CallTool(ctx context.Context, name string, args any) (string, error) {
	c, err := d.Resolve(ctx)
	if err != nil {
		return "", err
	}
	defer c.Close() //nolint:errcheck

	result, err := c.CallTool(ctx, name, args)
	if err != nil {
		return "", fmt.Errorf("calling tool %q: %w", name, err)
	}

	return extractText(result), nil
}

// sessionClient is a Client backed by an MCP client session.
type sessionClient interface {
	clientSession() *sdkmcp.ClientSession
}

// CallToolProgress is like CallTool, but asks the tool for progress
// notifications and passes each one's message to onProgress while it runs.
func (d *Dispatcher) CallToolProgress(ctx context.Context, name string, args any, onProgress func(string)) (string, error) {
	c, err := d.resolve(ctx, &sdkmcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, req *sdkmcp.ProgressNotificationClientRequest) {
			onProgress(req.Params.Message)
		},
	})
	if err != nil {
		return "", err
	}
	defer c.Close() //nolint:errcheck

	sc, ok := c.(sessionClient)
	if !ok {
		return "", fmt.Errorf("calling tool %q: client does not support progress", name)
	}
	logToolCall("progress", name, args)
	params := &sdkmcp.CallToolParams{Name: name, Arguments: args}
	params.SetProgressToken(name)
	result, err := sc.clientSession().CallTool(ctx, params)
	if err != nil {
		return "", fmt.Errorf("calling tool %q: %w", name, err)
	}
	if result.IsError {
		return "", fmt.Errorf("%s", extractText(result))
	}
	return extractText(result), nil
}

// ListTools is a convenience wrapper: resolves client, lists tools, closes client.
func (d *Dispatcher) ListTools(ctx context.Context) ([]ToolInfo, error) {
	c, err := d.Resolve(ctx)
	if err != nil {
		return nil, err
	}
	defer c.Close() //nolint:errcheck

	tools, err := c.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing tools: %w", err)
	}

	return tools, nil
}

// isServerRunning returns true if the Aviary server PID file exists and the process is alive.
// It imports the check from the server package without creating a circular dependency via interface.
func (d *Dispatcher) isServerRunning() bool {
	// Delegated to package-level function to allow easy testing.
	return checkServerRunning()
}

// checkServerRunning checks the PID file.
// Populated in dispatch_unix.go / dispatch_windows.go or via the server package.
var checkServerRunning = func() bool { return false }

// SetServerChecker allows the server package to inject its IsRunning check.
func SetServerChecker(fn func() bool) {
	checkServerRunning = fn
}

// loadStoredToken is satisfied by the server package — set at startup.
var loadStoredToken = func() (string, error) {
	return "", fmt.Errorf("no token configured; use --token or run 'aviary serve' first")
}

// SetTokenLoader allows the server package to inject its LoadToken function.
func SetTokenLoader(fn func() (string, error)) {
	loadStoredToken = fn
}

func cdpPortOrDefault(port int) int {
	if port == 0 {
		return config.DefaultCDPPort
	}
	return port
}
