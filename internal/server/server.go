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
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/mcp"
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
	skillsWatcher     *skills.Watcher
	listenerRestartCh chan struct{}
	hardRestartCh     chan struct{}
	upgradeCh         chan struct{}
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
		upgradeCh:         make(chan struct{}, 1),
	}
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

	// Inject deps into MCP tool handlers.
	mcp.SetDeps(&mcp.Deps{
		Agents:    s.agents,
		Scheduler: s.sched,
		Channels:  s.channels,
		Browser:   s.brw,
		Auth:      authStore,
		Upgrade:   s.triggerUpgrade,
	})
	agent.SetToolClientFactory(mcp.NewAgentToolClient)
	agent.SetSessionMessageObserver(func(agentID, sessionID, role string) {
		wsBroadcast(wsEvent{Type: "session_message", AgentID: agentID, SessionID: sessionID, Role: role})
	})
	agent.SetSessionProcessingObserver(func(agentID, sessionID string, processing bool) {
		v := processing
		wsBroadcast(wsEvent{Type: "session_processing", AgentID: agentID, SessionID: sessionID, IsProcessing: &v})
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
	go func() {
		if err := s.skillsWatcher.Start(); err != nil {
			_ = err // Non-fatal; skill hot-reload just won't work.
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
		s.skillsWatcher.Stop()
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

	var clearAssistantStatus func()
	if as, ok := ch.(channels.AssistantStatusSender); ok && as.ShowAssistantStatus() && strings.TrimSpace(msg.ThreadTS) != "" {
		sendAssistantStatus := func(status string) {
			if err := as.SendAssistantStatus(msg.Channel, msg.ThreadTS, slackAssistantStatusText(status)); err != nil {
				slog.Debug("server: failed to update assistant status", "type", channelType, "channel", msg.Channel, "err", err)
			}
		}
		sendAssistantStatus("thinking")
		clearAssistantStatus = func() {
			sendAssistantStatus("")
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

	runner.PromptMediaWithOverrides(msgCtx, msg.Text, msg.MediaURL, rOpts, func(e agent.StreamEvent) {
		switch e.Type {
		case agent.StreamEventText:
			if slackStreamer != nil {
				slackStreamer.Append(e.Text)
			}
		case agent.StreamEventTool:
			if slackStreamer != nil && e.Tool != nil {
				if status := slackToolStatusText(e.Tool); status != "" {
					if as, ok := ch.(channels.AssistantStatusSender); ok && as.ShowAssistantStatus() && strings.TrimSpace(msg.ThreadTS) != "" {
						if err := as.SendAssistantStatus(msg.Channel, msg.ThreadTS, status); err != nil {
							slog.Debug("server: failed to update assistant status", "type", channelType, "channel", msg.Channel, "err", err)
						}
					}
				}
				slackStreamer.UpsertToolOutput(e.Tool)
			}
		case agent.StreamEventStatus:
			if slackStreamer == nil {
				sendOrEditStatus(e.Text)
			}
			if as, ok := ch.(channels.AssistantStatusSender); ok && as.ShowAssistantStatus() && strings.TrimSpace(msg.ThreadTS) != "" {
				if err := as.SendAssistantStatus(msg.Channel, msg.ThreadTS, slackAssistantStatusText(e.Text)); err != nil {
					slog.Debug("server: failed to update assistant status", "type", channelType, "channel", msg.Channel, "err", err)
				}
			}
		case agent.StreamEventError:
			if slackStreamer != nil && e.Err != nil {
				slackStreamer.Append("\nError: " + e.Err.Error())
				slackStreamer.Flush()
			}
			if stopTyping != nil {
				stopTyping()
			}
			if clearAssistantStatus != nil {
				clearAssistantStatus()
				clearAssistantStatus = nil
			}
		case agent.StreamEventStop:
			if slackStreamer != nil {
				slackStreamer.Append("\nStopped.")
				slackStreamer.Flush()
			}
			if stopTyping != nil {
				stopTyping()
			}
			if clearAssistantStatus != nil {
				clearAssistantStatus()
				clearAssistantStatus = nil
			}
		case agent.StreamEventDone:
			if slackStreamer != nil {
				slackStreamer.Flush()
			}
			if stopTyping != nil {
				stopTyping()
			}
			if clearAssistantStatus != nil {
				clearAssistantStatus()
				clearAssistantStatus = nil
			}
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
	s.FlushLines(false)
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
	s.FlushLines(true)
	s.FlushTools()
}

func (s *slackThreadStreamer) FlushLines(final bool) {
	if s == nil {
		return
	}
	for {
		text := s.pending.String()
		idx := strings.IndexByte(text, '\n')
		if idx < 0 {
			if final {
				s.sendLine(strings.TrimRight(text, "\r"))
				s.pending.Reset()
			}
			return
		}
		line := strings.TrimRight(text[:idx], "\r")
		s.pending.Reset()
		s.pending.WriteString(text[idx+1:])
		s.sendLine(line)
	}
}

func (s *slackThreadStreamer) sendLine(line string) {
	if s == nil || line == "" {
		return
	}
	_, err := s.thread.SendThreadMessageAndGetID(s.channel, s.threadTS, line+"\n")
	if err != nil {
		slog.Debug("server: failed to send Slack line", "channel", s.channel, "thread", s.threadTS, "err", err)
	}
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

func slackToolStatusText(tool *agent.ToolEvent) string {
	if tool == nil || tool.Result != "" || tool.Error != "" {
		return ""
	}
	switch tool.Name {
	case "agent_file_read":
		file := toolArgString(tool.Args, "file", "path")
		if file == "" {
			return "is reading a file"
		}
		return "is reading " + file
	default:
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			return ""
		}
		return "is using " + name
	}
}

func toolArgString(args map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := args[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case string:
			if text := strings.TrimSpace(v); text != "" {
				return text
			}
		case fmt.Stringer:
			if text := strings.TrimSpace(v.String()); text != "" {
				return text
			}
		}
	}
	return ""
}

func slackAssistantStatusText(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return ""
	}
	status = strings.TrimPrefix(status, "I am ")
	status = strings.TrimPrefix(status, "I'm ")
	if status == "thinking" {
		return "is thinking"
	}
	if strings.HasPrefix(status, "is ") {
		return status
	}
	return "is " + status
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
	parts := strings.SplitN(route, ":", 3)
	if len(parts) != 3 {
		return fmt.Errorf("invalid task target %q", route)
	}
	channelType := strings.TrimSpace(parts[0])
	configuredID := strings.TrimSpace(parts[1])
	targetID := strings.TrimSpace(parts[2])
	if channelType == "" {
		return fmt.Errorf("task target channel type is required")
	}
	if configuredID == "" {
		return fmt.Errorf("task target configured channel id is required")
	}
	if targetID == "" {
		return fmt.Errorf("task target delivery id is required")
	}
	return s.channels.SendOnConfiguredChannel(agentName, channelType, configuredID, targetID, text)
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
