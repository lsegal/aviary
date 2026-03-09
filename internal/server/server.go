// Package server implements the Aviary HTTPS server.
package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

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

// ErrRestartRequired is returned by ListenAndServe when a config change requires a server restart.
var ErrRestartRequired = errors.New("server restart required")

// Server wraps an HTTPS server with token auth, MCP routing, and agent management.
type Server struct {
	cfg       *config.Config
	token     string
	mux       *http.ServeMux
	httpSrv   *http.Server
	agents    *agent.Manager
	sched     *scheduler.Scheduler
	mem       *memory.Manager
	channels  *channels.Manager
	brw       *browser.Manager
	sampler   *ProcSampler
	watcher   *config.Watcher
	restartCh chan struct{}
}

// New creates a new Server with the given config and auth token.
func New(cfg *config.Config, token string) *Server {
	// Create auth store first — needed for both MCP deps and LLM token refresh.
	authPath := filepath.Join(store.SubDir(store.DirAuth), "credentials.json")
	authStore, _ := auth.NewFileStore(authPath)

	authResolver := makeAuthResolver()
	factory := llm.NewFactory(authResolver)
	if authStore != nil {
		factory.WithTokenSetter(authStore.Set)
	}
	s := &Server{
		cfg:       cfg,
		token:     token,
		mux:       http.NewServeMux(),
		agents:    agent.NewManager(factory),
		restartCh: make(chan struct{}, 1),
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
	s.sampler = NewProcSampler()
	cdpPort := cfg.Browser.CDPPort
	if cdpPort == 0 {
		cdpPort = config.DefaultCDPPort
	}
	s.brw = browser.NewManager(cfg.Browser.Binary, cdpPort, cfg.Browser.ProfileDir, cfg.Browser.Headless)

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
	agent.SetMemoryCompactionObserver(func(agentID, poolID string, started bool) {
		v := started
		wsBroadcast(wsEvent{Type: "memory_compaction", AgentID: agentID, PoolID: poolID, IsProcessing: &v})
	})

	// Install the log hub as the global slog handler, delegating to the
	// preconfigured default handler (stderr + file, when logging.Init() ran).
	// Only do this once — on restart slog.Default() is already globalHub,
	// so setting it as its own delegate would cause infinite recursion.
	if slog.Default().Handler() != globalHub {
		globalHub.setDelegate(slog.Default().Handler())
		slog.SetDefault(slog.New(globalHub))
		slog.Info("server: logger initialized", "component", "server")
	}

	// Set up config watcher.
	s.watcher = config.NewWatcher("")
	s.watcher.OnChange(func(newCfg *config.Config) {
		s.agents.Reconcile(newCfg)
		if s.sched != nil {
			s.sched.Reconcile(newCfg)
		}
		// If server-level settings changed, signal ListenAndServe to restart.
		if serverSettingsChanged(s.cfg, newCfg) {
			slog.Info("server: settings changed, restarting")
			s.cfg = newCfg
			select {
			case s.restartCh <- struct{}{}:
			default:
			}
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

	// Log stream SSE endpoint + history REST endpoint.
	s.mux.Handle("/api/logs", BearerMiddleware(s.token, http.HandlerFunc(logsHandler)))
	s.mux.Handle("/api/logs/history", BearerMiddleware(s.token, http.HandlerFunc(logsHistoryHandler)))

	// Daemons status + log-stream endpoints.
	s.mux.Handle("/api/daemons", BearerMiddleware(s.token, http.HandlerFunc(s.daemonsHandler)))
	s.mux.Handle("/api/daemons/logs", BearerMiddleware(s.token, http.HandlerFunc(s.daemonLogsHandler)))

	// Web UI: SPA served from embedded web/dist.
	s.mux.Handle("/", webFileServer())
}

// ListenAndServe starts the server on the configured port.
// It returns only when the context is cancelled or an error occurs.
// Returns ErrRestartRequired if a config change requires a restart.
func (s *Server) ListenAndServe(ctx context.Context) error {
	port := s.cfg.Server.Port
	if port == 0 {
		port = 16677
	}

	host := "127.0.0.1"
	if s.cfg.Server.ExternalAccess {
		host = "0.0.0.0"
	}
	addr := fmt.Sprintf("%s:%d", host, port)

	var ln net.Listener
	if s.cfg.Server.NoTLS {
		var err error
		ln, err = net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("listening on %s: %w", addr, err)
		}
	} else {
		var tlsCert, tlsKey string
		if s.cfg.Server.TLS != nil {
			tlsCert = s.cfg.Server.TLS.Cert
			tlsKey = s.cfg.Server.TLS.Key
		}
		cert, err := LoadOrGenerateTLS(tlsCert, tlsKey)
		if err != nil {
			return fmt.Errorf("loading TLS: %w", err)
		}
		tlsCfg := &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
		ln, err = tls.Listen("tcp", addr, tlsCfg)
		if err != nil {
			return fmt.Errorf("listening on %s: %w", addr, err)
		}
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

	// Start process sampler — periodically collects CPU/RSS for all daemon PIDs.
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pids := []int{os.Getpid()}
				for _, cs := range s.channels.List() {
					if cs.Daemon != nil && cs.Daemon.PID > 0 {
						pids = append(pids, cs.Daemon.PID)
					}
				}
				s.sampler.Sample(pids)
			}
		}
	}()

	// Start channel integrations and route messages to agents.
	s.channels.Reconcile(ctx, s.cfg, func(agentName string, msg channels.IncomingMessage) {
		runner, ok := s.agents.Get(agentName)
		if !ok {
			return
		}
		// Channel reply is fire-and-forget; errors logged by runner.
		runner.Prompt(ctx, msg.Text)
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.httpSrv.Serve(ln)
	}()

	restart := false
	select {
	case <-ctx.Done():
	case <-s.restartCh:
		restart = true
	case err := <-errCh:
		return err
	}

	// Graceful shutdown (covers both normal stop and restart).
	s.watcher.Stop()
	s.channels.Stop()
	if s.sched != nil {
		s.sched.Stop()
	}
	s.agents.Stop()
	_ = s.httpSrv.Shutdown(context.Background())

	if restart {
		return ErrRestartRequired
	}
	return nil
}

func tlsConfigChanged(a, b *config.TLSConfig) bool {
	if a == nil && b == nil {
		return false
	}
	if a == nil || b == nil {
		return true
	}
	return *a != *b
}

// serverSettingsChanged reports whether a config change affects server-level
// settings that require a restart (port, TLS mode, bind address).
func serverSettingsChanged(oldCfg, newCfg *config.Config) bool {
	return oldCfg.Server.Port != newCfg.Server.Port ||
		oldCfg.Server.ExternalAccess != newCfg.Server.ExternalAccess ||
		oldCfg.Server.NoTLS != newCfg.Server.NoTLS ||
		tlsConfigChanged(oldCfg.Server.TLS, newCfg.Server.TLS)
}

// Addr returns the server address string.
func (s *Server) Addr() string {
	port := s.cfg.Server.Port
	if port == 0 {
		port = 16677
	}
	scheme := "https"
	if s.cfg.Server.NoTLS {
		scheme = "http"
	}
	return fmt.Sprintf("%s://localhost:%d", scheme, port)
}

// Agents returns the agent manager.
func (s *Server) Agents() *agent.Manager { return s.agents }
