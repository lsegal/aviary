package mcp

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/auth"
	"github.com/lsegal/aviary/internal/browser"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/memory"
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
		return NewRemoteClient(ctx, d.serverURL, tok)
	}

	// Server not running — create a local in-process client.
	if err := ensureInProcessDeps(); err != nil {
		return nil, err
	}
	srv := NewServer()
	return NewInProcessClient(ctx, srv)
}

func ensureInProcessDeps() error {
	deps := GetDeps()
	if deps != nil && deps.Agents != nil && deps.Memory != nil && deps.Browser != nil {
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
		Agents:  agents,
		Memory:  memory.New(),
		Browser: browser.NewManager(cfg.Browser.Binary, cfg.Browser.CDPPort, cfg.Browser.ProfileDir, cfg.Browser.Headless),
		Auth:    authStore,
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
	return "", fmt.Errorf("no token configured; use --token or run 'aviary start' first")
}

// SetTokenLoader allows the server package to inject its LoadToken function.
func SetTokenLoader(fn func() (string, error)) {
	loadStoredToken = fn
}
