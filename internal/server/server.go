// Package server implements the Aviary HTTPS server.
package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/slack-go/slack"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/auth"
	"github.com/lsegal/aviary/internal/browser"
	"github.com/lsegal/aviary/internal/channels"
	"github.com/lsegal/aviary/internal/codingskills"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/mcp"
	"github.com/lsegal/aviary/internal/projects"
	"github.com/lsegal/aviary/internal/saferestart"
	"github.com/lsegal/aviary/internal/scheduler"
	"github.com/lsegal/aviary/internal/sessiontarget"
	"github.com/lsegal/aviary/internal/store"
	"github.com/lsegal/aviary/internal/update"
	"github.com/lsegal/aviary/skills"
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
	channels          *channels.Manager
	brw               *browser.Manager
	sampler           *ProcSampler
	watcher           *config.Watcher
	projects          *projects.Manager
	skillsWatcher     *skills.Watcher
	listenerRestartCh chan struct{}
	hardRestartCh     chan struct{}
	reexecCh          chan struct{}
	restart           restartState
	msgFn             func(agentName, channelType, configuredID string, ch channels.Channel, msg channels.IncomingMessage)
}

// New creates a new Server with the given config and auth token.
func New(cfg *config.Config, token string) *Server {
	s := &Server{
		cfg:               cfg,
		token:             token,
		mux:               http.NewServeMux(),
		listenerRestartCh: make(chan struct{}, 1),
		hardRestartCh:     make(chan struct{}, 1),
		reexecCh:          make(chan struct{}, 1),
	}
	configureCLIProviders(cfg)
	configureRestartGate(cfg)
	// Create auth store first — needed for both MCP deps and LLM token refresh.
	authPath := filepath.Join(store.SubDir(store.DirAuth), "credentials.json")
	authStore, _ := auth.NewFileStore(authPath)

	authResolver := makeAuthResolver()
	factory := llm.NewFactory(authResolver).WithProviderOptionsResolver(func(provider string) (llm.ProviderOptions, bool) {
		if s.cfg == nil || s.cfg.Models.Providers == nil {
			return llm.ProviderOptions{}, false
		}
		pc, ok := s.cfg.Models.Providers[strings.TrimSpace(provider)]
		if !ok {
			return llm.ProviderOptions{}, false
		}
		return llm.ProviderOptions{
			Auth:    pc.Auth,
			BaseURI: pc.BaseURI,
		}, true
	})
	if authStore != nil {
		factory.WithTokenSetter(authStore.Set)
	}
	s.agents = agent.NewManager(factory)

	// Initial reconcile from loaded config.
	s.agents.Reconcile(cfg)

	// Create scheduler (non-fatal if it fails).
	if sched, err := scheduler.New(s.agents, 0); err == nil {
		s.sched = sched
		s.sched.Reconcile(cfg)
	} else {
		slog.Warn("server: scheduler initialization failed; scheduled tasks disabled", "err", err)
	}

	s.channels = channels.NewManager()
	if s.sched != nil {
		s.sched.SetTaskOutputDelivery(s.deliverTaskOutput)
	}
	s.sampler = NewProcSampler()
	cdpPort := cfg.Browser.CDPPort
	if cdpPort == 0 {
		cdpPort = config.DefaultCDPPort
	}
	s.brw = browser.NewManager(
		cfg.Browser.Binary,
		cdpPort,
		cfg.Browser.ProfileDir,
		cfg.Browser.Headless,
		config.EffectiveBrowserReuseTabs(cfg.Browser),
	)

	if manager, err := projects.NewManager(store.SubDir(store.DirProjects)); err == nil {
		s.projects = manager
		manager.SetLogSource(globalHub.since)
		manager.SetReportDelivery(s.deliverTaskOutput)
		manager.SetMentionRouter(s.promptProjectsAgent)
		manager.SetAviaryAgentRunner(s.runProjectsJobTurn)
		manager.SetUsageRecorder(func(rec domain.UsageRecord) {
			if err := store.AppendJSONL(store.UsagePath(), rec); err != nil {
				slog.Warn("server: failed to record projects usage", "err", err)
			}
		})
	} else {
		slog.Warn("server: projects initialization failed; projects disabled", "err", err)
	}
	if s.sched != nil && s.projects != nil {
		s.sched.SetProjectRunner(s.projects.RunProjectTask)
	}

	// Inject deps into MCP tool handlers.
	mcp.SetDeps(&mcp.Deps{
		Projects:      s.projects,
		Agents:        s.agents,
		Scheduler:     s.sched,
		Channels:      s.channels,
		Browser:       s.brw,
		Auth:          authStore,
		Restart:       s.requestRestart,
		RestartStatus: s.RestartStatus,
	})
	agent.SetToolClientFactory(mcp.NewAgentToolClient)
	agent.SetSessionMessageObserver(func(agentID, sessionID, role string) {
		wsBroadcast(wsEvent{Type: "session_message", AgentID: agentID, SessionID: sessionID, Role: role})
	})
	agent.SetSessionProcessingObserver(func(agentID, sessionID string, processing bool) {
		v := processing
		wsBroadcast(wsEvent{Type: "session_processing", AgentID: agentID, SessionID: sessionID, IsProcessing: &v})
	})
	agent.SetActiveTurnObserver(func(turn agent.ActiveTurn) {
		wsBroadcast(sessionTurnEvent(turn))
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
	s.skillsWatcher = skills.NewWatcher()
	s.skillsWatcher.OnChange(func() {
		mcp.SyncLiveServer(s.cfg)
	})

	s.registerRoutes()
	return s
}

// startupWatches are the runtime watches the next server start begins with,
// as `aviary watch` hands them over when it starts the server itself.
var startupWatches []projects.Watch

// SetStartupWatches makes the next server start watch these targets without
// saving them to aviary.yaml.
func SetStartupWatches(watches []projects.Watch) { startupWatches = watches }

// configureCLIProviders installs the models: section's CLI providers in
// core's provider layer, which Code Loop and every other CLI caller resolve
// through.
func configureCLIProviders(cfg *config.Config) {
	if cfg == nil {
		return
	}
	if err := llm.ConfigureCLIProviders(cfg.Models.CLIDefinitions); err != nil {
		slog.Error("server: providers reached through a CLI not applied", "err", err)
	}
}

// startProjects applies the agents' projects and the startup watches.
func (s *Server) startProjects() {
	s.projects.SetBrowser(s.cfg.Browser)
	s.projects.Reconcile(s.cfg)
	for _, watch := range startupWatches {
		if err := s.projects.AddWatch(watch); err != nil {
			slog.Error("server: watch failed to start", "targets", watch.Targets, "err", err)
		}
	}
}

// Projects returns the server's Code Loop manager.
func (s *Server) Projects() *projects.Manager { return s.projects }

func (s *Server) applyConfigReload(newCfg *config.Config) {
	oldCfg := s.cfg
	if err := store.UpdateChannelMetadataState(oldCfg, newCfg, time.Now().UTC()); err != nil {
		slog.Warn("server: failed to update channel metadata state", "err", err)
	}
	configureCLIProviders(newCfg)
	configureRestartGate(newCfg)
	mcp.SyncLiveServer(newCfg)
	s.agents.Reconcile(newCfg)
	if s.sched != nil {
		s.sched.Reconcile(newCfg)
	}
	if s.projects != nil {
		s.projects.SetBrowser(newCfg.Browser)
		go s.projects.Reconcile(newCfg)
	}
	if s.runCtx != nil && s.msgFn != nil && s.channels != nil {
		s.channels.Reconcile(s.runCtx, newCfg, s.msgFn)
	}
	if err := sessiontarget.EnsureChannels(newCfg, s.channels); err != nil {
		slog.Warn("server: failed to set up channel sessions", "err", err)
	}
	cdpPort := newCfg.Browser.CDPPort
	if cdpPort == 0 {
		cdpPort = config.DefaultCDPPort
	}
	s.brw = browser.NewManager(
		newCfg.Browser.Binary,
		cdpPort,
		newCfg.Browser.ProfileDir,
		newCfg.Browser.Headless,
		config.EffectiveBrowserReuseTabs(newCfg.Browser),
	)
	deps := mcp.GetDeps()
	deps.Browser = s.brw
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

	// Daemons status + log-stream endpoints.
	s.mux.Handle("/api/daemons", BearerMiddleware(s.token, http.HandlerFunc(s.daemonsHandler)))
	s.mux.Handle("/api/daemons/logs", BearerMiddleware(s.token, http.HandlerFunc(s.daemonLogsHandler)))
	s.mux.Handle("/api/daemons/restart", BearerMiddleware(s.token, http.HandlerFunc(s.daemonRestartHandler)))

	// Web UI: SPA served from embedded web/dist.
	s.mux.Handle("/", webFileServer())
}

// codingSkillsSyncInterval is how often the server looks for providers set
// up since the last check, so they get the skills Aviary ships.
const codingSkillsSyncInterval = 15 * time.Minute

// updateCodingSkills moves every signed-in CLI's skills to version, the
// release an upgrade installs.
func updateCodingSkills(ctx context.Context, version string) error {
	result, err := codingskills.Default().UpdateTo(ctx, version)
	if len(result.Installed) > 0 {
		slog.Info("coding skills: updated for the upgrade", "version", version, "skills", strings.Join(result.Installed, ", "))
	}
	return err
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
	go func() {
		if err := s.skillsWatcher.Start(); err != nil {
			_ = err // Non-fatal; skill hot-reload just won't work.
		}
	}()

	exe, _ := executablePath()
	update.CleanupStaging(exe)
	go s.runSelfUpdater(ctx)

	// Install the skills Aviary ships globally on first launch, keep them
	// installed and matching this release, and move them to the new release
	// when Aviary upgrades.
	go func() {
		codingskills.Default().InstallOnFirstLaunch(ctx, config.BaseDir())
		codingskills.Default().RunSync(ctx, codingSkillsSyncInterval)
	}()
	saferestart.RegisterSkillsUpdater("shipped", updateCodingSkills)
	defer saferestart.UnregisterSkillsUpdater("shipped")

	// The marker is read once, so a later start never mistakes this one for
	// its own safe restart.
	marker, restarted := saferestart.TakeMarker()

	// Start scheduler.
	if s.sched != nil {
		s.sched.Start(ctx)
		saferestart.Register("tasks", s.sched)
		defer saferestart.Unregister("tasks")
	}

	// Start the Code Loop engine, with any watches `aviary watch` handed over.
	if s.projects != nil {
		// Jobs a safe restart stopped resume in their own sessions and are
		// told that Aviary restarted.
		if restarted {
			s.projects.ResumeAfterRestart(marker)
		}
		saferestart.Register("projects", s.projects)
		defer saferestart.Unregister("projects")
		go s.startProjects()
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
	if err := sessiontarget.EnsureChannels(s.cfg, s.channels); err != nil {
		slog.Warn("server: failed to set up channel sessions", "err", err)
	}

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
			reexec          bool
		)
		select {
		case <-ctx.Done():
		case <-s.listenerRestartCh:
			listenerRestart = true
		case <-s.hardRestartCh:
			hardRestart = true
		case <-s.reexecCh:
			reexec = true
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
		s.skillsWatcher.Stop()
		if s.projects != nil {
			// Checkpoint running jobs while their agents are still alive.
			s.projects.Shutdown()
		}
		s.channels.Stop()
		if s.sched != nil {
			s.sched.Stop()
		}
		s.agents.Stop()

		if reexec {
			return ErrReexecRequired
		}
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
	msgCtx = agent.WithSessionSender(msgCtx, domain.NewMessageSender(msg.From, msg.SenderName, true))

	agentID := agentName
	channelCfg, _ := s.findChannelConfig(agentName, channelType, configuredID)
	sessionName := channelSessionNameForIncoming(agentID, channelCfg, msg)
	if sess, err := agent.NewSessionManager().GetOrCreateNamed(agentID, sessionName); err == nil && sess != nil {
		msgCtx = agent.WithSessionID(msgCtx, sess.ID)
		target := store.SessionChannel{
			Type:         msg.Type,
			ConfiguredID: configuredID,
			ID:           msg.Channel,
			ThreadTS:     strings.TrimSpace(msg.ThreadTS),
		}
		sessiontarget.Register(agentID, agentName, sess.ID, target, s.channels)
		if err := store.EnsureSessionChannelTarget(agentID, sess.ID, target); err != nil {
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

	// Let the platform stop this run (e.g. Slack's native Stop control).
	// Cancelling runCtx cancels the run the same way agent.StopSession does,
	// but only for this channel thread, not other threads sharing the session.
	runCtx, cancelRun := context.WithCancel(msgCtx)
	endRun := cancelRun
	if rs, ok := ch.(channels.RunStopRegistrar); ok && strings.TrimSpace(msg.ThreadTS) != "" {
		unregister := rs.RegisterRunStop(msg.Channel, msg.ThreadTS, cancelRun)
		endRun = func() {
			unregister()
			cancelRun()
		}
	}

	var clearAssistantStatus func()
	if as, ok := ch.(channels.AssistantStatusSender); ok && as.ShowAssistantStatus() && strings.TrimSpace(msg.ThreadTS) != "" {
		setProcessing := func(processing bool) {
			if err := as.SetAssistantProcessing(msg.Channel, msg.ThreadTS, msg.From, processing); err != nil {
				slog.Debug("server: failed to update assistant status", "type", channelType, "channel", msg.Channel, "err", err)
			}
		}
		setProcessing(true)
		clearAssistantStatus = func() {
			setProcessing(false)
		}
	}
	finish := func() {
		endRun()
		if stopTyping != nil {
			stopTyping()
		}
		if clearAssistantStatus != nil {
			clearAssistantStatus()
			clearAssistantStatus = nil
		}
	}

	rOpts := agent.RunOverrides{
		Model:         msg.Model,
		Fallbacks:     msg.Fallbacks,
		RestrictTools: msg.RestrictTools,
		DisabledTools: msg.DisabledTools,
	}

	// If this message quotes another message, include the quoted author/text
	// in the message text so it becomes part of the prompt/session history.
	if strings.TrimSpace(msg.QuoteAuthor) != "" && strings.TrimSpace(msg.QuoteText) != "" {
		// Prefix the incoming user text with the quoted line and author.
		msg.Text = fmt.Sprintf("%s: %s\n\n%s", msg.QuoteAuthor, msg.QuoteText, msg.Text)
	}

	// Verbose mode: send/edit a live status message for each tool call.
	var (
		statusMsgID string
		statusLines []string
	)
	sendOrEditStatus := func(newLine string) {
		statusLines = append(statusLines, newLine)
		text := strings.Join(statusLines, "\n")
		if statusMsgID == "" {
			if sender, ok := ch.(channels.MessageSenderWithID); ok {
				id, err := sender.SendAndGetID(msg.Channel, text)
				if err == nil {
					statusMsgID = id
				}
			} else {
				_ = ch.Send(msg.Channel, newLine)
			}
		} else if editor, ok := ch.(channels.MessageEditor); ok {
			_ = editor.EditMessage(msg.Channel, statusMsgID, text)
		} else {
			_ = ch.Send(msg.Channel, newLine)
		}
	}
	slackStreamer := newSlackThreadStreamer(channelType, ch, msg)
	if slackStreamer != nil {
		rOpts.SuppressDelivery = true
	}

	runner.PromptMediaWithOverrides(runCtx, msg.Text, mediaList(msg.MediaURL), rOpts, func(e agent.StreamEvent) {
		switch e.Type {
		case agent.StreamEventText:
			if slackStreamer != nil {
				slackStreamer.Append(e.Text)
			}
		case agent.StreamEventTool:
			if slackStreamer != nil && e.Tool != nil && runner.IsVerbose() {
				slackStreamer.UpsertToolOutput(e.Tool)
			}
		case agent.StreamEventStatus:
			if slackStreamer == nil {
				sendOrEditStatus(e.Text)
			}
		case agent.StreamEventError:
			if slackStreamer != nil && e.Err != nil {
				slackStreamer.Append("\nError: " + e.Err.Error())
				slackStreamer.Flush()
			}
			finish()
		case agent.StreamEventStop:
			if slackStreamer != nil {
				slackStreamer.Append("\nStopped.")
				slackStreamer.Flush()
			}
			finish()
		case agent.StreamEventDone:
			if slackStreamer != nil {
				slackStreamer.Flush()
			}
			finish()
		}
	})
}

func (s *Server) findChannelConfig(agentName, channelType, configuredID string) (config.ChannelConfig, bool) {
	if s.cfg == nil {
		return config.ChannelConfig{}, false
	}
	for _, ac := range s.cfg.Agents {
		if ac.Name != agentName {
			continue
		}
		for _, cc := range ac.Channels {
			if cc.Type == channelType && cc.ID == configuredID {
				return cc, true
			}
		}
	}
	return config.ChannelConfig{}, false
}

func channelSessionName(cc config.ChannelConfig, msg channels.IncomingMessage) string {
	base := msg.Type + ":" + msg.Channel
	if msg.Type != "slack" {
		return base
	}
	threadTS := strings.TrimSpace(msg.ThreadTS)
	if threadTS == "" {
		return base
	}
	if config.BoolOr(cc.SeparateTopLevelSessions, false) {
		return base + ":" + threadTS
	}
	return base
}

func channelSessionNameForIncoming(agentID string, cc config.ChannelConfig, msg channels.IncomingMessage) string {
	sessionName := channelSessionName(cc, msg)
	if msg.Type != "slack" || !msg.IsThreadReply || !config.BoolOr(cc.SeparateTopLevelSessions, false) {
		return sessionName
	}
	baseName := msg.Type + ":" + msg.Channel
	if sessionName == baseName {
		return sessionName
	}
	if store.FindSessionPath(agentID, sessionName) == "" && store.FindSessionPath(agentID, baseName) != "" {
		return baseName
	}
	return sessionName
}

type slackThreadStreamer struct {
	thread    channels.ThreadMessageSender
	editor    channels.MessageEditor
	blocks    slackBlockMessageSender
	channel   string
	threadTS  string
	pending   strings.Builder
	toolMsgID string
	tools     []slackToolDisclosure
}

type slackBlockMessageSender interface {
	SendThreadBlocksAndGetID(channel, threadTS, fallbackText string, blocks ...slack.Block) (msgID string, err error)
	EditMessageBlocks(channel, msgID, fallbackText string, blocks ...slack.Block) error
}

type slackToolDisclosure struct {
	Name   string
	Args   map[string]any
	Result string
	Error  string
}

func newSlackThreadStreamer(channelType string, ch channels.Channel, msg channels.IncomingMessage) *slackThreadStreamer {
	if channelType != "slack" || strings.TrimSpace(msg.ThreadTS) == "" {
		return nil
	}
	thread, ok := ch.(channels.ThreadMessageSender)
	if !ok {
		return nil
	}
	streamer := &slackThreadStreamer{
		thread:   thread,
		channel:  msg.Channel,
		threadTS: strings.TrimSpace(msg.ThreadTS),
	}
	if blocks, ok := ch.(slackBlockMessageSender); ok {
		streamer.blocks = blocks
	}
	if editor, ok := ch.(channels.MessageEditor); ok {
		streamer.editor = editor
	}
	return streamer
}

func (s *slackThreadStreamer) Append(text string) {
	if s == nil || text == "" {
		return
	}
	s.pending.WriteString(text)
}

func (s *slackThreadStreamer) UpsertToolOutput(tool *agent.ToolEvent) {
	if s == nil || tool == nil || strings.TrimSpace(tool.Name) == "" {
		return
	}
	if tool.Name == "agent_file_read" {
		return
	}
	disclosure := slackToolDisclosure{
		Name:   tool.Name,
		Args:   tool.Args,
		Result: tool.Result,
		Error:  tool.Error,
	}
	for i := len(s.tools) - 1; i >= 0; i-- {
		if s.tools[i].Name == tool.Name && sameToolArgs(s.tools[i].Args, tool.Args) {
			s.tools[i] = disclosure
			s.FlushTools()
			return
		}
	}
	s.tools = append(s.tools, disclosure)
	s.FlushTools()
}

func (s *slackThreadStreamer) Flush() {
	if s == nil {
		return
	}
	if answer := strings.TrimSpace(s.pending.String()); answer != "" {
		_, err := s.thread.SendThreadMessageAndGetID(s.channel, s.threadTS, answer)
		if err != nil {
			slog.Debug("server: failed to send Slack answer", "channel", s.channel, "thread", s.threadTS, "err", err)
		}
	}
	s.pending.Reset()
	s.FlushTools()
}

func (s *slackThreadStreamer) FlushTools() {
	if s == nil || len(s.tools) == 0 {
		return
	}
	blocks := s.toolBlocks()
	if len(blocks) == 0 {
		return
	}
	fallback := "Tool calls"
	if s.toolMsgID == "" {
		var (
			id  string
			err error
		)
		if s.blocks != nil {
			id, err = s.blocks.SendThreadBlocksAndGetID(s.channel, s.threadTS, fallback, blocks...)
		} else {
			id, err = s.thread.SendThreadMessageAndGetID(s.channel, s.threadTS, formatToolDisclosuresBlock(s.tools, 2900))
		}
		if err == nil {
			s.toolMsgID = id
		} else {
			slog.Debug("server: failed to send Slack tool calls", "channel", s.channel, "thread", s.threadTS, "err", err)
		}
		return
	}
	if s.blocks != nil {
		if err := s.blocks.EditMessageBlocks(s.channel, s.toolMsgID, fallback, blocks...); err != nil {
			slog.Debug("server: failed to edit Slack tool calls", "channel", s.channel, "thread", s.threadTS, "err", err)
		}
	}
}

func (s *slackThreadStreamer) toolBlocks() []slack.Block {
	if s == nil || len(s.tools) == 0 {
		return nil
	}
	toolText := formatToolDisclosuresBlock(s.tools, 2900)
	return []slack.Block{slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, toolText, false, false), nil, nil, slack.SectionBlockOptionExpand(false))}
}

func formatToolDisclosuresBlock(tools []slackToolDisclosure, limit int) string {
	var b strings.Builder
	b.WriteString(":hammer_and_wrench: *Tool Calls*")
	for i, tool := range tools {
		part := formatToolDisclosureEntry(tool)
		if b.Len()+len(part) > limit {
			b.WriteString("\n... truncated")
			return b.String()
		}
		b.WriteString(part)
		if i == 7 && len(tools) > 8 {
			fmt.Fprintf(&b, "\n... %d more", len(tools)-8)
			break
		}
	}
	return b.String()
}

func formatToolSummaryLine(tool slackToolDisclosure) string {
	status := slackToolDisclosureStatus(tool)
	input := compactToolInput(tool.Args, 180)
	if input == "" {
		return fmt.Sprintf("%s `%s`", status, tool.Name)
	}
	return fmt.Sprintf("%s `%s` :arrow_right: `%s`", status, tool.Name, escapeSlackInlineCode(input))
}

func formatToolDisclosureEntry(tool slackToolDisclosure) string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(formatToolSummaryLine(tool))
	output := toolOutputText(tool)
	if output == "" {
		return b.String()
	}
	b.WriteString("\n\n")
	if strings.TrimSpace(tool.Error) != "" {
		b.WriteString("Error:")
	} else {
		b.WriteString("Output:")
	}
	b.WriteString("\n```")
	b.WriteString(truncateSlackCodeBlock(output, 700))
	b.WriteString("```")
	return b.String()
}

func slackToolDisclosureStatus(tool slackToolDisclosure) string {
	if strings.TrimSpace(tool.Error) != "" {
		return ":warning:"
	}
	if strings.TrimSpace(tool.Result) == "" {
		return ":thinking_face:"
	}
	return ":white_check_mark:"
}

func toolOutputText(tool slackToolDisclosure) string {
	if strings.TrimSpace(tool.Error) != "" {
		return strings.TrimSpace(tool.Error)
	}
	if strings.TrimSpace(tool.Result) != "" && tool.Name != "agent_file_read" {
		return strings.TrimSpace(tool.Result)
	}
	return ""
}

func compactToolInput(args map[string]any, limit int) string {
	if len(args) == 0 {
		return ""
	}
	data, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	return singleLineToolText(string(data), limit)
}

func singleLineToolText(text string, limit int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if limit > 0 && len(text) > limit {
		if limit <= 3 {
			return strings.Repeat(".", limit)
		}
		return text[:limit-3] + "..."
	}
	return text
}

func truncateSlackCodeBlock(text string, limit int) string {
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "```", "`\u200b``")
	if limit > 0 && len(text) > limit {
		if limit <= 3 {
			return strings.Repeat(".", limit)
		}
		return text[:limit-3] + "..."
	}
	return text
}

func escapeSlackInlineCode(text string) string {
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "`", "'")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	return text
}

func sameToolArgs(a, b map[string]any) bool {
	left, leftErr := json.Marshal(a)
	right, rightErr := json.Marshal(b)
	return leftErr == nil && rightErr == nil && string(left) == string(right)
}

func (s *Server) listen() (net.Listener, error) {
	port := s.cfg.Server.Port
	if port == 0 {
		port = 16677
	}

	host := "127.0.0.1"
	if config.EffectiveServerExternalAccess(s.cfg.Server) {
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
		config.EffectiveServerExternalAccess(oldCfg.Server) != config.EffectiveServerExternalAccess(newCfg.Server) ||
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
	if route == "" || strings.EqualFold(route, "silent") {
		return nil
	}
	if strings.HasPrefix(route, "session:") {
		sessionRef := strings.TrimSpace(strings.TrimPrefix(route, "session:"))
		if sessionRef == "" {
			return fmt.Errorf("task target session is required")
		}
		if store.FindSessionPath(agentName, sessionRef) != "" {
			return agent.AppendReplyToSession(agentName, sessionRef, text)
		}
		sess, err := agent.NewSessionManager().GetOrCreateNamed(agentName, sessionRef)
		if err != nil {
			return fmt.Errorf("resolving task target session %q: %w", sessionRef, err)
		}
		return agent.AppendReplyToSession(agentName, sess.ID, text)
	}
	name, ok := config.ChannelTargetName(route)
	if !ok || name == "" {
		return fmt.Errorf("invalid task target %q; use channel:<channel name> or session:<name>", route)
	}
	return s.deliverToChannel(agentName, name, text)
}

// deliverToChannel appends text to the own session of agentName's channel
// named name, which sends it on to the channel's DeliverTo conversation.
func (s *Server) deliverToChannel(agentName, name, text string) error {
	if s.cfg == nil {
		return fmt.Errorf("no configuration loaded")
	}
	for _, a := range s.cfg.Agents {
		if a.Name != agentName {
			continue
		}
		idx := a.ChannelByName(name)
		if idx < 0 {
			return fmt.Errorf("agent %q has no channel named %q", agentName, name)
		}
		sess, err := sessiontarget.EnsureChannel(agentName, a.Channels[idx], s.channels)
		if err != nil {
			return fmt.Errorf("resolving channel %q session: %w", name, err)
		}
		return agent.AppendReplyToSession(agentName, sess.ID, text)
	}
	return fmt.Errorf("agent %q is not configured", agentName)
}

func stageOutgoingMedia(channelType, sourcePath string) (string, error) {
	return channels.StageOutgoingMedia(channelType, sourcePath)
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
			sessiontarget.Register(cfg.AgentID, cfg.AgentID, cfg.SessionID, ch, s.channels)
		}
	}
	if len(cfgs) > 0 {
		slog.Info("server: loaded session channel deliveries", "sessions", len(cfgs))
	}
}
