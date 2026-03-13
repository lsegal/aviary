// Package server implements the Aviary HTTPS server.
package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	"github.com/lsegal/aviary/internal/update"
)

// ErrRestartRequired is returned by ListenAndServe when an explicit process
// restart was requested (for example via the daemons API).
var ErrRestartRequired = errors.New("server restart required")

// Server wraps an HTTPS server with token auth, MCP routing, and agent management.
type Server struct {
	cfg               *config.Config
	token             string
	mux               *http.ServeMux
	httpSrv           *http.Server
	runCtx            context.Context
	agents            *agent.Manager
	sched             *scheduler.Scheduler
	mem               *memory.Manager
	channels          *channels.Manager
	brw               *browser.Manager
	sampler           *ProcSampler
	watcher           *config.Watcher
	listenerRestartCh chan struct{}
	hardRestartCh     chan struct{}
	upgradeCh         chan struct{}
	msgFn             func(agentName, channelType, configuredID string, ch channels.Channel, msg channels.IncomingMessage)
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
		cfg:               cfg,
		token:             token,
		mux:               http.NewServeMux(),
		agents:            agent.NewManager(factory),
		listenerRestartCh: make(chan struct{}, 1),
		hardRestartCh:     make(chan struct{}, 1),
		upgradeCh:         make(chan struct{}, 1),
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
	if s.sched != nil {
		s.sched.SetTaskOutputDelivery(s.deliverTaskOutput)
	}
	s.sampler = NewProcSampler()
	cdpPort := cfg.Browser.CDPPort
	if cdpPort == 0 {
		cdpPort = config.DefaultCDPPort
	}
	s.brw = browser.NewManager(cfg.Browser.Binary, cdpPort, cfg.Browser.ProfileDir, cfg.Browser.Headless)

	// Inject deps into MCP tool handlers.
	mcp.SetDeps(&mcp.Deps{
		Agents:    s.agents,
		Scheduler: s.sched,
		Memory:    s.mem,
		Browser:   s.brw,
		Auth:      authStore,
		Upgrade:   s.triggerUpgrade,
	})
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
		s.applyConfigReload(newCfg)
	})

	s.registerRoutes()
	return s
}

func (s *Server) applyConfigReload(newCfg *config.Config) {
	oldCfg := s.cfg
	if err := store.UpdateChannelMetadataState(oldCfg, newCfg, time.Now().UTC()); err != nil {
		slog.Warn("server: failed to update channel metadata state", "err", err)
	}
	mcp.SyncLiveServer(newCfg)
	s.agents.Reconcile(newCfg)
	if s.sched != nil {
		s.sched.Reconcile(newCfg)
	}
	if s.runCtx != nil && s.msgFn != nil && s.channels != nil {
		s.channels.Reconcile(s.runCtx, newCfg, s.msgFn)
	}
	s.cfg = newCfg
	if serverSettingsChanged(oldCfg, newCfg) {
		slog.Info("server: settings changed, rotating listener")
		select {
		case s.listenerRestartCh <- struct{}{}:
		default:
		}
	}
}

func (s *Server) registerRoutes() {
	mcpSrv := mcp.NewServer()
	mcp.SetLiveServer(mcpSrv)
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
	s.mux.Handle("/api/version", BearerMiddleware(s.token, http.HandlerFunc(s.versionHandler)))
	s.mux.Handle("/api/version/upgrade", BearerMiddleware(s.token, http.HandlerFunc(s.versionUpgradeHandler)))

	// Daemons status + log-stream endpoints.
	s.mux.Handle("/api/daemons", BearerMiddleware(s.token, http.HandlerFunc(s.daemonsHandler)))
	s.mux.Handle("/api/daemons/logs", BearerMiddleware(s.token, http.HandlerFunc(s.daemonLogsHandler)))
	s.mux.Handle("/api/daemons/restart", BearerMiddleware(s.token, http.HandlerFunc(s.daemonRestartHandler)))

	// Web UI: SPA served from embedded web/dist.
	s.mux.Handle("/", webFileServer())
}

// ListenAndServe starts the server on the configured port.
// It returns only when the context is cancelled, an error occurs, or an
// explicit process restart is requested.
func (s *Server) ListenAndServe(ctx context.Context) error {
	s.runCtx = ctx

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
	s.msgFn = func(agentName, channelType, configuredID string, ch channels.Channel, msg channels.IncomingMessage) {
		s.handleIncomingChannelMessage(ctx, agentName, channelType, configuredID, ch, msg)
	}
	s.channels.Reconcile(ctx, s.cfg, s.msgFn)
	s.loadSessionDeliveries()

	for {
		ln, err := s.listen()
		if err != nil {
			return err
		}
		s.httpSrv = &http.Server{Handler: s.mux}

		errCh := make(chan error, 1)
		go func(httpSrv *http.Server, ln net.Listener) {
			errCh <- httpSrv.Serve(ln)
		}(s.httpSrv, ln)

		var (
			listenerRestart bool
			hardRestart     bool
		)
		select {
		case <-ctx.Done():
		case <-s.listenerRestartCh:
			listenerRestart = true
		case <-s.hardRestartCh:
			hardRestart = true
		case <-s.upgradeCh:
		case err := <-errCh:
			if errors.Is(err, http.ErrServerClosed) {
				continue
			}
			return err
		}

		_ = s.httpSrv.Shutdown(context.Background())
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		if listenerRestart {
			continue
		}

		s.watcher.Stop()
		s.channels.Stop()
		if s.sched != nil {
			s.sched.Stop()
		}
		s.agents.Stop()

		if hardRestart {
			return ErrRestartRequired
		}
		return nil
	}
}

func (s *Server) handleIncomingChannelMessage(ctx context.Context, agentName, channelType, configuredID string, ch channels.Channel, msg channels.IncomingMessage) {
	runner, ok := s.agents.Get(agentName)
	if !ok {
		return
	}
	msgCtx := agent.WithChannelSession(ctx, channelType, configuredID, msg.Channel)

	agentID := "agent_" + agentName
	if sess, err := agent.NewSessionManager().GetOrCreateNamed(agentID, msg.Type+":"+msg.Channel); err == nil && sess != nil {
		chDest, chRef := msg.Channel, ch
		agent.RegisterSessionDelivery(sess.ID, msg.Type, msg.Channel, func(text string) {
			if err := chRef.Send(chDest, text); err != nil {
				slog.Warn("server: failed to send response to channel", "type", msg.Type, "channel", chDest, "err", err)
			}
		})
		if ms, ok := ch.(channels.MediaSender); ok {
			agent.RegisterSessionMediaDelivery(sess.ID, msg.Type, msg.Channel, func(caption, path string) {
				deliverPath := path
				if staged, err := stageOutgoingMedia(msg.Type, path); err == nil {
					deliverPath = staged
				} else {
					slog.Warn("server: failed to stage outgoing media", "type", msg.Type, "path", path, "err", err)
				}
				if err := ms.SendMedia(chDest, caption, deliverPath); err != nil {
					slog.Warn("server: failed to send media to channel", "type", msg.Type, "channel", chDest, "err", err)
				}
			})
		}
		if err := store.EnsureSessionChannel(agentID, sess.ID, msg.Type, msg.Channel); err != nil {
			slog.Warn("server: failed to update session channels config", "session", sess.ID, "err", err)
		}
	}

	var stopTyping context.CancelFunc
	if ts, ok := ch.(channels.TypingSender); ok && ts.ShowTyping() {
		_ = ts.SendTyping(msg.Channel, false)
		typingCtx, cancel := context.WithCancel(ctx)
		stopTyping = cancel
		go func() {
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()
			defer ts.SendTyping(msg.Channel, true) //nolint:errcheck
			for {
				select {
				case <-typingCtx.Done():
					return
				case <-ticker.C:
					_ = ts.SendTyping(msg.Channel, false)
				}
			}
		}()
	}

	rOpts := agent.RunOverrides{
		Model:         msg.Model,
		Fallbacks:     msg.Fallbacks,
		RestrictTools: msg.RestrictTools,
		DisabledTools: msg.DisabledTools,
	}
	runner.PromptMediaWithOverrides(msgCtx, msg.Text, msg.MediaURL, rOpts, func(e agent.StreamEvent) {
		switch e.Type {
		case agent.StreamEventDone, agent.StreamEventError, agent.StreamEventStop:
			if stopTyping != nil {
				stopTyping()
			}
		}
	})
}

func (s *Server) listen() (net.Listener, error) {
	port := s.cfg.Server.Port
	if port == 0 {
		port = 16677
	}

	host := "127.0.0.1"
	if s.cfg.Server.ExternalAccess {
		host = "0.0.0.0"
	}
	addr := fmt.Sprintf("%s:%d", host, port)

	if s.cfg.Server.NoTLS {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("listening on %s: %w", addr, err)
		}
		return ln, nil
	}

	var tlsCert, tlsKey string
	if s.cfg.Server.TLS != nil {
		tlsCert = s.cfg.Server.TLS.Cert
		tlsKey = s.cfg.Server.TLS.Key
	}
	cert, err := LoadOrGenerateTLS(tlsCert, tlsKey)
	if err != nil {
		return nil, fmt.Errorf("loading TLS: %w", err)
	}
	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
	ln, err := tls.Listen("tcp", addr, tlsCfg)
	if err != nil {
		return nil, fmt.Errorf("listening on %s: %w", addr, err)
	}
	return ln, nil
}

func (s *Server) triggerUpgrade(_ context.Context, version string) error {
	if update.EmulationActive() {
		return nil
	}
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating executable: %w", err)
	}
	if err := update.StartHelper(update.HelperRequest{
		TargetPath:  exePath,
		WaitPID:     os.Getpid(),
		Version:     version,
		RestartArgs: append([]string{}, os.Args[1:]...),
		Repo:        update.DefaultRepo,
		APIBase:     update.DefaultAPIBase,
	}); err != nil {
		return err
	}
	select {
	case s.upgradeCh <- struct{}{}:
	default:
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

func (s *Server) deliverTaskOutput(agentName, route, text string) error {
	route = strings.TrimSpace(route)
	if route == "" {
		return nil
	}
	parts := strings.SplitN(route, ":", 4)
	if len(parts) != 4 || parts[0] != "route" {
		return fmt.Errorf("invalid task target route %q", route)
	}
	configuredID := strings.TrimSpace(parts[2])
	targetID := strings.TrimSpace(parts[3])
	if configuredID == "" {
		return fmt.Errorf("task target configured channel id is required")
	}
	if targetID == "" {
		return fmt.Errorf("task target delivery id is required")
	}
	return s.channels.SendOnConfiguredChannel(agentName, parts[1], configuredID, targetID, text)
}

func stageOutgoingMedia(channelType, sourcePath string) (string, error) {
	if strings.TrimSpace(sourcePath) == "" {
		return "", fmt.Errorf("source path is required")
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("source path is a directory")
	}
	dir := store.OutgoingMediaDir(channelType)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	target := filepath.Join(dir, fmt.Sprintf("%d_%s", time.Now().UnixMilli(), filepath.Base(sourcePath)))
	src, err := os.Open(sourcePath)
	if err != nil {
		return "", err
	}
	defer src.Close() //nolint:errcheck
	dst, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	defer dst.Close() //nolint:errcheck
	if _, err := io.Copy(dst, src); err != nil {
		return "", err
	}
	return target, nil
}

// loadSessionDeliveries reads all persisted session channel configs and
// registers delivery functions so that sessions started from channels continue
// to route responses back to those channels after a server restart.
// Per-message registrations (Reconcile closure) will overwrite these with a
// more direct closure on the next inbound message.
func (s *Server) loadSessionDeliveries() {
	cfgs, err := store.FindAllSessionChannelsConfigs()
	if err != nil {
		slog.Warn("server: failed to load session channel configs", "err", err)
		return
	}
	for _, cfg := range cfgs {
		for _, ch := range cfg.Channels {
			chType, chID, sessionID := ch.Type, ch.ID, cfg.SessionID
			agent.RegisterSessionDelivery(sessionID, chType, chID, func(text string) {
				if err := s.channels.RouteDelivery(chType, chID, text); err != nil {
					slog.Warn("server: failed to deliver to channel", "type", chType, "id", chID, "err", err)
				}
			})
			agent.RegisterSessionMediaDelivery(sessionID, chType, chID, func(caption, path string) {
				deliverPath := path
				if staged, err := stageOutgoingMedia(chType, path); err == nil {
					deliverPath = staged
				} else {
					slog.Warn("server: failed to stage outgoing media", "type", chType, "path", path, "err", err)
				}
				if err := s.channels.RouteMediaDelivery(chType, chID, caption, deliverPath); err != nil {
					slog.Warn("server: failed to deliver media to channel", "type", chType, "id", chID, "err", err)
				}
			})
		}
	}
	if len(cfgs) > 0 {
		slog.Info("server: loaded session channel deliveries", "sessions", len(cfgs))
	}
}
