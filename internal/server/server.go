// Package server implements the Aviary HTTPS server.
package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/browser"
	"github.com/lsegal/aviary/internal/channels"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/mcp"
	"github.com/lsegal/aviary/internal/memory"
	"github.com/lsegal/aviary/internal/scheduler"
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
	s := &Server{
		cfg:    cfg,
		token:  token,
		mux:    http.NewServeMux(),
		agents: agent.NewManager(llm.NewFactory(nil)),
	}

	// Initial reconcile from loaded config.
	s.agents.Reconcile(cfg)

	// Create scheduler (non-fatal if it fails).
	if sched, err := scheduler.New(s.agents, 0); err == nil {
		s.sched = sched
		s.sched.Reconcile(cfg)
	}

	s.mem = memory.New()
	s.channels = channels.NewManager()
	s.brw = browser.NewManager(cfg.Browser.Binary, cfg.Browser.CDPPort)

	// Inject deps into MCP tool handlers.
	mcp.SetDeps(&mcp.Deps{Agents: s.agents, Scheduler: s.sched, Memory: s.mem, Browser: s.brw})

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

	// MCP endpoint: wrapped in bearer middleware.
	s.mux.Handle("/mcp", BearerMiddleware(s.token, mcpHandler))
	s.mux.Handle("/mcp/", BearerMiddleware(s.token, mcpHandler))

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
