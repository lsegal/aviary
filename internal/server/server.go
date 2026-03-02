// Package server implements the Aviary HTTPS server.
package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/auth"
	"github.com/lsegal/aviary/internal/browser"
	"github.com/lsegal/aviary/internal/channels"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/mcp"
	"github.com/lsegal/aviary/internal/memory"
	"github.com/lsegal/aviary/internal/scheduler"
	"github.com/lsegal/aviary/internal/store"
)

// Server wraps an HTTPS server with token auth, MCP routing, and agent management.
type Server struct {
	cfg      *config.Config
	token    string
	mux      *http.ServeMux
	httpSrv  *http.Server
	agents   *agent.Manager
	sched    *scheduler.Scheduler
	mem      *memory.Manager
	channels *channels.Manager
	brw      *browser.Manager
	watcher  *config.Watcher
}

// New creates a new Server with the given config and auth token.
func New(cfg *config.Config, token string) *Server {
	authResolver := makeAuthResolver()
	s := &Server{
		cfg:    cfg,
		token:  token,
		mux:    http.NewServeMux(),
		agents: agent.NewManager(llm.NewFactory(authResolver)),
	}

	// Initial reconcile from loaded config.
	s.agents.Reconcile(cfg)

	// Create scheduler (non-fatal if it fails).
	if sched, err := scheduler.New(s.agents, 0); err == nil {
		s.sched = sched
		s.sched.Reconcile(cfg)
	} else {
		slog.Warn("server: scheduler initialization failed; scheduled tasks disabled", "err", err)
	}

	s.mem = memory.New()
	s.channels = channels.NewManager()
	s.brw = browser.NewManager(cfg.Browser.Binary, cfg.Browser.CDPPort, cfg.Browser.ProfileDir, cfg.Browser.Headless)

	// Open the file-backed credential store (non-fatal if dir doesn't exist yet).
	authPath := filepath.Join(store.SubDir(store.DirAuth), "credentials.json")
	authStore, _ := auth.NewFileStore(authPath)

	// Inject deps into MCP tool handlers.
	mcp.SetDeps(&mcp.Deps{Agents: s.agents, Scheduler: s.sched, Memory: s.mem, Browser: s.brw, Auth: authStore})
	agent.SetToolClientFactory(mcp.NewAgentToolClient)
	agent.SetSessionMessageObserver(func(sessionID, role string) {
		wsBroadcast(wsEvent{Type: "session_message", SessionID: sessionID, Role: role})
	})
	agent.SetSessionProcessingObserver(func(sessionID string, processing bool) {
		v := processing
		wsBroadcast(wsEvent{Type: "session_processing", SessionID: sessionID, IsProcessing: &v})
	})

	// Install the log hub as the global slog handler, delegating to the
	// preconfigured default handler (stderr + file, when logging.Init() ran).
	globalHub.setDelegate(slog.Default().Handler())
	slog.SetDefault(slog.New(globalHub))
	slog.Info("server: logger initialized", "component", "server")

	// Set up config watcher.
	s.watcher = config.NewWatcher("")
	s.watcher.OnChange(func(newCfg *config.Config) {
		s.agents.Reconcile(newCfg)
		if s.sched != nil {
			s.sched.Reconcile(newCfg)
		}
	})

	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	mcpSrv := mcp.NewServer()
	mcpHandler := mcp.HTTPHandler(mcpSrv)

	// Login does not require auth.
	s.mux.HandleFunc("/api/login", LoginHandler(s.token))

	// Health check (public) and WebSocket keepalive (auth via session cookie / ?token=).
	s.mux.HandleFunc("/api/health", healthHandler)
	s.mux.HandleFunc("/api/ws", wsHandler(s.token))

	// MCP endpoint: wrapped in bearer middleware.
	s.mux.Handle("/mcp", BearerMiddleware(s.token, mcpHandler))
	s.mux.Handle("/mcp/", BearerMiddleware(s.token, mcpHandler))

	// Log stream SSE endpoint.
	s.mux.Handle("/api/logs", BearerMiddleware(s.token, http.HandlerFunc(logsHandler)))

	// Web UI: SPA served from embedded web/dist.
	s.mux.Handle("/", webFileServer())
}

// ListenAndServe starts the HTTPS server on the configured port.
// It returns only when the context is cancelled or an error occurs.
func (s *Server) ListenAndServe(ctx context.Context) error {
	port := s.cfg.Server.Port
	if port == 0 {
		port = 16677
	}

	cert, err := LoadOrGenerateTLS(s.cfg.Server.TLS.Cert, s.cfg.Server.TLS.Key)
	if err != nil {
		return fmt.Errorf("loading TLS: %w", err)
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	addr := fmt.Sprintf(":%d", port)
	ln, err := tls.Listen("tcp", addr, tlsCfg)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", addr, err)
	}

	s.httpSrv = &http.Server{Handler: s.mux}

	// Start config watcher in background.
	go func() {
		if err := s.watcher.Start(); err != nil {
			_ = err // Non-fatal; hot-reload just won't work.
		}
	}()

	// Start scheduler.
	if s.sched != nil {
		s.sched.Start(ctx)
	}

	// Start channel integrations and route messages to agents.
	s.channels.Reconcile(ctx, s.cfg, func(agentName string, msg channels.IncomingMessage) {
		runner, ok := s.agents.Get(agentName)
		if !ok {
			return
		}
		runner.Prompt(ctx, msg.Text, func(e agent.StreamEvent) {
			if e.Type == agent.StreamEventDone || e.Type == agent.StreamEventText {
				// Channel reply is fire-and-forget; errors logged by runner.
			}
		})
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.httpSrv.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		s.watcher.Stop()
		s.channels.Stop()
		if s.sched != nil {
			s.sched.Stop()
		}
		s.agents.Stop()
		return s.httpSrv.Shutdown(context.Background())
	case err := <-errCh:
		return err
	}
}

// Addr returns the server address string.
func (s *Server) Addr() string {
	port := s.cfg.Server.Port
	if port == 0 {
		port = 16677
	}
	return fmt.Sprintf("https://localhost:%d", port)
}

// Agents returns the agent manager.
func (s *Server) Agents() *agent.Manager { return s.agents }
