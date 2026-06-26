package channels

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/auth"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

// ChannelStatus describes a running channel and its daemon, if any.
type ChannelStatus struct {
	Key     string      `json:"key"`
	Agent   string      `json:"agent"`
	Type    string      `json:"type"`
	ID      string      `json:"id"`
	Started time.Time   `json:"started"`
	Daemon  *DaemonInfo `json:"daemon,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// Manager manages channel lifecycle across all agents.
type Manager struct {
	mu         sync.Mutex
	channels   map[string]Channel // key: agentName+"/"+channelType+"/"+channelID
	cancels    map[string]context.CancelFunc
	startTimes map[string]time.Time
	errors     map[string]string
	sinks      map[string]*LogSink // per-channel stdout/stderr capture
	specs      map[string]channelSpec
	slack      map[string]*sharedSlackChannel
	slackAlias map[string]string
}

type channelSpec struct {
	agentName      string
	channelConfig  config.ChannelConfig
	metadata       store.ChannelMetadata
	agentModel     string
	agentFallbacks []string
}

type sharedSlackChannel struct {
	connKey string
	keys    []string
	specs   []channelSpec
	ch      *SlackChannel
	cancel  context.CancelFunc
	sink    *LogSink
	started time.Time
	err     string
}

// NewManager creates a channel Manager.
func NewManager() *Manager {
	return &Manager{
		channels:   make(map[string]Channel),
		cancels:    make(map[string]context.CancelFunc),
		startTimes: make(map[string]time.Time),
		errors:     make(map[string]string),
		sinks:      make(map[string]*LogSink),
		specs:      make(map[string]channelSpec),
		slack:      make(map[string]*sharedSlackChannel),
		slackAlias: make(map[string]string),
	}
}

// Reconcile idempotently starts/stops channels from the config.
// msgFn receives messages and should route them to the appropriate agent runner.
// The ch argument passed to msgFn is the channel the message arrived on; it may
// implement optional interfaces such as TypingSender.
func (m *Manager) Reconcile(ctx context.Context, cfg *config.Config, msgFn func(agentName, channelType, configuredID string, ch Channel, msg IncomingMessage)) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, err := store.ReadAppState()
	if err != nil {
		slog.Warn("channel state read failed", "err", err)
		state = &store.AppState{}
	}

	desired := make(map[string]struct{})
	desiredSlack := make(map[string][]channelSpec)
	for _, ac := range cfg.Agents {
		agentModel := config.EffectiveAgentModel(ac, cfg.Models)
		agentFallbacks := config.EffectiveAgentFallbacks(ac, cfg.Models)
		for _, cc := range ac.Channels {
			key := channelKey(ac.Name, cc.Type, cc.ID)
			if config.BoolOr(cc.Enabled, true) {
				desired[key] = struct{}{}
				spec := channelSpec{
					agentName:      ac.Name,
					channelConfig:  cc,
					metadata:       channelMetadata(state, key),
					agentModel:     agentModel,
					agentFallbacks: append([]string{}, agentFallbacks...),
				}
				existingSpec, exists := m.specs[key]
				m.specs[key] = spec
				if cc.Type == "slack" {
					connKey := slackConnectionKey(cc)
					desiredSlack[connKey] = append(desiredSlack[connKey], spec)
					continue
				}
				if exists && reflect.DeepEqual(existingSpec, spec) && m.channels[key] != nil {
					continue // already running with the desired config
				}
			} else {
				delete(m.specs, key)
			}

			if !config.BoolOr(cc.Enabled, true) {
				continue
			}

			if cc.Type == "slack" {
				continue
			}

			if _, exists := m.channels[key]; exists {
				m.stopChannelLocked(key)
			}

			if err := m.startChannelLocked(ctx, key, channelSpec{
				agentName:      ac.Name,
				channelConfig:  cc,
				metadata:       channelMetadata(state, key),
				agentModel:     agentModel,
				agentFallbacks: append([]string{}, agentFallbacks...),
			}, msgFn); err != nil {
				slog.Warn("channel start failed", "key", key, "err", err)
			}
		}
	}

	for connKey, specs := range desiredSlack {
		existing := m.slack[connKey]
		if existing != nil && reflect.DeepEqual(existing.specs, specs) {
			for _, spec := range specs {
				key := channelKey(spec.agentName, spec.channelConfig.Type, spec.channelConfig.ID)
				m.channels[key] = existing.ch
				m.sinks[key] = existing.sink
				m.startTimes[key] = existing.started
				m.slackAlias[key] = connKey
			}
			continue
		}
		if existing != nil {
			m.stopSharedSlackLocked(connKey)
		}
		if err := m.startSharedSlackLocked(ctx, connKey, specs, msgFn); err != nil {
			slog.Warn("channel start failed", "key", connKey, "err", err)
		}
	}

	for connKey := range m.slack {
		if _, ok := desiredSlack[connKey]; !ok {
			m.stopSharedSlackLocked(connKey)
			slog.Info("channel stopped", "key", connKey)
		}
	}

	// Stop channels no longer in config.
	for key := range m.channels {
		if _, ok := desired[key]; !ok {
			if _, isSlackAlias := m.slackAlias[key]; isSlackAlias {
				delete(m.channels, key)
				delete(m.sinks, key)
				delete(m.startTimes, key)
				delete(m.errors, key)
				delete(m.slackAlias, key)
				continue
			}
			m.stopChannelLocked(key)
			delete(m.specs, key)
			slog.Info("channel stopped", "key", key)
		}
	}
}

// Stop halts all channels.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	stopped := map[Channel]struct{}{}
	for key, ch := range m.channels {
		if _, ok := stopped[ch]; ok {
			continue
		}
		stopped[ch] = struct{}{}
		ch.Stop()
		if cancel := m.cancels[key]; cancel != nil {
			cancel()
		}
	}
	m.channels = make(map[string]Channel)
	m.cancels = make(map[string]context.CancelFunc)
	m.startTimes = make(map[string]time.Time)
	m.errors = make(map[string]string)
	m.sinks = make(map[string]*LogSink)
	m.specs = make(map[string]channelSpec)
	m.slack = make(map[string]*sharedSlackChannel)
	m.slackAlias = make(map[string]string)
}

// SubscribeLogs returns a log subscription for the given daemon key.
// history contains buffered lines already captured; live delivers future lines.
// The caller must call unsub when done. Returns ok=false if the key is unknown.
func (m *Manager) SubscribeLogs(key string) (history []string, live <-chan string, unsub func(), ok bool) {
	m.mu.Lock()
	sink := m.sinks[key]
	m.mu.Unlock()
	if sink == nil {
		return nil, nil, nil, false
	}
	h, l, u := sink.Subscribe()
	return h, l, u, true
}

// Restart recreates and restarts a configured channel instance in place.
func (m *Manager) Restart(ctx context.Context, key string, msgFn func(agentName, channelType, configuredID string, ch Channel, msg IncomingMessage)) error {
	m.mu.Lock()
	spec, ok := m.specs[key]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("configured channel %q not found", key)
	}
	if connKey, ok := m.slackAlias[key]; ok {
		shared := m.slack[connKey]
		if shared == nil {
			m.mu.Unlock()
			return fmt.Errorf("configured channel %q not active", key)
		}
		specs := append([]channelSpec{}, shared.specs...)
		m.stopSharedSlackLocked(connKey)
		err := m.startSharedSlackLocked(ctx, connKey, specs, msgFn)
		m.mu.Unlock()
		if err != nil {
			return err
		}
		slog.Info("channel restarted", "key", key, "type", spec.channelConfig.Type)
		return nil
	}
	m.stopChannelLocked(key)
	err := m.startChannelLocked(ctx, key, spec, msgFn)
	m.mu.Unlock()
	if err != nil {
		return err
	}

	slog.Info("channel restarted", "key", key, "type", spec.channelConfig.Type)
	return nil
}

func (m *Manager) startChannelLocked(ctx context.Context, key string, spec channelSpec, msgFn func(agentName, channelType, configuredID string, ch Channel, msg IncomingMessage)) error {
	resolvedConfig, err := resolveChannelAuthRefs(spec.channelConfig)
	if err != nil {
		return fmt.Errorf("resolve channel auth refs: %w", err)
	}
	ch := newChannel(resolvedConfig, spec.agentModel, spec.agentFallbacks)
	if ch == nil {
		return fmt.Errorf("channel %q could not be created", key)
	}

	// If this is a Signal channel, record the owning agent name so the
	// channel implementation can substitute a human-readable agent name into
	// incoming messages where needed.
	if sc, ok := ch.(*SignalChannel); ok {
		sc.AgentName = spec.agentName
	}
	sink := newLogSink()
	m.sinks[key] = sink
	if ss, ok := ch.(LogSinkSetter); ok {
		ss.SetLogSink(sink)
	}

	agentName := spec.agentName
	channelMeta := spec.metadata

	// Write non-triggering group messages directly to the agent session so
	// they appear as conversation context alongside agent-triggered turns.
	if gcl, ok := ch.(GroupChatLogger); ok {
		if spec.channelConfig.EffectiveGroupChatHistory() > 0 {
			agentID := agentName
			gcl.OnGroupChatMessage(func(msg IncomingMessage) {
				sessionName := msg.Type + ":" + msg.Channel
				sess, err := agent.NewSessionManager().GetOrCreateNamed(agentID, sessionName)
				if err != nil || sess == nil {
					slog.Warn("channel: failed to get session for chat history", "err", err)
					return
				}
				sender := domain.NewMessageSender(msg.From, msg.SenderName, false)
				if err := agent.AppendMessageToSessionWithSender(agentID, sess.ID, domain.MessageRoleUser, strings.TrimSpace(msg.Text), sender); err != nil {
					slog.Warn("channel: failed to log group chat message", "err", err)
				}
			})
		}
	}

	ch.OnMessage(func(msg IncomingMessage) {
		if !shouldProcessIncomingMessage(channelMeta, msg) {
			return
		}
		msgFn(agentName, spec.channelConfig.Type, spec.channelConfig.ID, ch, msg)
	})

	cctx, cancel := context.WithCancel(ctx)
	m.channels[key] = ch
	m.cancels[key] = cancel
	m.startTimes[key] = time.Now()
	delete(m.errors, key)

	go func(k string, c Channel) {
		if err := c.Start(cctx); err != nil && cctx.Err() == nil {
			slog.Warn("channel error", "key", k, "err", err)
			m.mu.Lock()
			m.errors[k] = err.Error()
			m.mu.Unlock()
		}
	}(key, ch)

	slog.Info("channel started", "key", key, "type", spec.channelConfig.Type)
	return nil
}

func (m *Manager) startSharedSlackLocked(ctx context.Context, connKey string, specs []channelSpec, msgFn func(agentName, channelType, configuredID string, ch Channel, msg IncomingMessage)) error {
	if len(specs) == 0 {
		return nil
	}
	resolvedSpecs := make([]channelSpec, 0, len(specs))
	for _, spec := range specs {
		resolvedConfig, err := resolveChannelAuthRefs(spec.channelConfig)
		if err != nil {
			return fmt.Errorf("resolve channel auth refs: %w", err)
		}
		spec.channelConfig = resolvedConfig
		resolvedSpecs = append(resolvedSpecs, spec)
	}

	base := resolvedSpecs[0].channelConfig
	base.AllowFrom = mergeAllowFrom(resolvedSpecs)
	ch := NewSlackChannel(base.URL, base.Token, base.AllowFrom, "", nil)
	ch.showStatus = anySlackStatusEnabled(resolvedSpecs)

	sink := newLogSink()
	ch.SetLogSink(sink)

	if len(resolvedSpecs) > 0 {
		ch.OnGroupChatMessage(func(msg IncomingMessage) {
			for _, spec := range resolvedSpecs {
				if spec.channelConfig.EffectiveGroupChatHistory() <= 0 {
					continue
				}
				if !matchesAnyAllowedGroup(spec.channelConfig.AllowFrom, msg.Channel) {
					continue
				}
				if !shouldProcessIncomingMessage(spec.metadata, msg) {
					continue
				}
				sessionName := msg.Type + ":" + msg.Channel
				sess, err := agent.NewSessionManager().GetOrCreateNamed(spec.agentName, sessionName)
				if err != nil || sess == nil {
					slog.Warn("channel: failed to get session for chat history", "err", err)
					continue
				}
				sender := domain.NewMessageSender(msg.From, msg.SenderName, false)
				if err := agent.AppendMessageToSessionWithSender(spec.agentName, sess.ID, domain.MessageRoleUser, strings.TrimSpace(msg.Text), sender); err != nil {
					slog.Warn("channel: failed to log group chat message", "err", err)
				}
			}
		})
	}

	ch.OnMessage(func(msg IncomingMessage) {
		for _, spec := range resolvedSpecs {
			if !shouldProcessIncomingMessage(spec.metadata, msg) {
				continue
			}
			routed, ok := routedSlackMessage(ch, spec, msg)
			if !ok {
				continue
			}
			msgFn(spec.agentName, spec.channelConfig.Type, spec.channelConfig.ID, ch, routed)
		}
	})

	cctx, cancel := context.WithCancel(ctx)
	started := time.Now()
	shared := &sharedSlackChannel{
		connKey: connKey,
		specs:   append([]channelSpec{}, specs...),
		ch:      ch,
		cancel:  cancel,
		sink:    sink,
		started: started,
	}
	for _, spec := range specs {
		key := channelKey(spec.agentName, spec.channelConfig.Type, spec.channelConfig.ID)
		shared.keys = append(shared.keys, key)
		m.channels[key] = ch
		m.sinks[key] = sink
		m.startTimes[key] = started
		m.slackAlias[key] = connKey
		delete(m.errors, key)
	}
	m.slack[connKey] = shared

	go func(c *sharedSlackChannel) {
		if err := c.ch.Start(cctx); err != nil && cctx.Err() == nil {
			slog.Warn("channel error", "key", connKey, "err", err)
			m.mu.Lock()
			c.err = err.Error()
			for _, key := range c.keys {
				m.errors[key] = err.Error()
			}
			m.mu.Unlock()
		}
	}(shared)

	for _, key := range shared.keys {
		slog.Info("channel started", "key", key, "type", "slack")
	}
	return nil
}

func routedSlackMessage(ch *SlackChannel, spec channelSpec, msg IncomingMessage) (IncomingMessage, bool) {
	isGroup := !strings.HasPrefix(msg.Channel, "D")
	botUserID := ch.botUserID
	if botUserID == "" {
		botUserID = spec.channelConfig.ID
	}
	result := checkAllowed(spec.channelConfig.AllowFrom, msg.From, msg.Channel, msg.Text, isGroup, botUserID, false)
	if !result.allowed {
		return IncomingMessage{}, false
	}
	routed := msg
	routed.RestrictTools = result.restrictTools
	routed.DisabledTools = spec.channelConfig.DisabledTools
	routed.Model = result.model
	if routed.Model == "" {
		routed.Model = firstNonEmpty(spec.channelConfig.Model, spec.agentModel)
	}
	routed.Fallbacks = result.fallbacks
	if len(routed.Fallbacks) == 0 {
		routed.Fallbacks = spec.channelConfig.Fallbacks
	}
	if len(routed.Fallbacks) == 0 {
		routed.Fallbacks = spec.agentFallbacks
	}
	return routed, true
}

func mergeAllowFrom(specs []channelSpec) []config.AllowFromEntry {
	var out []config.AllowFromEntry
	for _, spec := range specs {
		out = append(out, spec.channelConfig.AllowFrom...)
	}
	return out
}

func matchesAnyAllowedGroup(entries []config.AllowFromEntry, channelID string) bool {
	for _, entry := range entries {
		if !config.BoolOr(entry.Enabled, true) {
			continue
		}
		if matchesAllowedGroup(entry.AllowedGroups, channelID) {
			return true
		}
	}
	return false
}

func anySlackStatusEnabled(specs []channelSpec) bool {
	for _, spec := range specs {
		if config.BoolOr(spec.channelConfig.ShowTyping, true) {
			return true
		}
	}
	return false
}

func resolveChannelAuthRefs(cc config.ChannelConfig) (config.ChannelConfig, error) {
	if !strings.HasPrefix(cc.Token, "auth:") && !strings.HasPrefix(cc.URL, "auth:") {
		return cc, nil
	}

	authStore, err := auth.NewFileStore(filepath.Join(store.SubDir(store.DirAuth), "credentials.json"))
	if err != nil {
		return cc, err
	}

	if strings.HasPrefix(cc.Token, "auth:") {
		resolvedToken, err := auth.Resolve(authStore, cc.Token)
		if err != nil {
			return cc, fmt.Errorf("token %q: %w", cc.Token, err)
		}
		cc.Token = resolvedToken
	}
	if strings.HasPrefix(cc.URL, "auth:") {
		resolvedURL, err := auth.Resolve(authStore, cc.URL)
		if err != nil {
			return cc, fmt.Errorf("url %q: %w", cc.URL, err)
		}
		cc.URL = resolvedURL
	}
	return cc, nil
}

func (m *Manager) stopChannelLocked(key string) {
	if connKey, ok := m.slackAlias[key]; ok {
		m.stopSharedSlackLocked(connKey)
		return
	}
	if ch, exists := m.channels[key]; exists {
		ch.Stop()
	}
	if cancel, exists := m.cancels[key]; exists {
		cancel()
	}
	delete(m.channels, key)
	delete(m.cancels, key)
	delete(m.startTimes, key)
	delete(m.errors, key)
	delete(m.sinks, key)
}

func (m *Manager) stopSharedSlackLocked(connKey string) {
	shared := m.slack[connKey]
	if shared == nil {
		return
	}
	shared.ch.Stop()
	if shared.cancel != nil {
		shared.cancel()
	}
	for _, key := range shared.keys {
		delete(m.channels, key)
		delete(m.cancels, key)
		delete(m.startTimes, key)
		delete(m.errors, key)
		delete(m.sinks, key)
		delete(m.slackAlias, key)
	}
	delete(m.slack, connKey)
}

// RouteDelivery sends text to channelID via any running channel of channelType.
// It tries all matching channels and returns on the first success.
func (m *Manager) RouteDelivery(channelType, channelID, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var lastErr error
	for key, ch := range m.channels {
		parts := strings.SplitN(key, "/", 3)
		if len(parts) != 3 || parts[1] != channelType {
			continue
		}
		if err := ch.Send(channelID, text); err != nil {
			lastErr = err
		} else {
			return nil
		}
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("no active channel of type %q", channelType)
}

// SendOnConfiguredChannel sends text using a specific configured channel
// instance identified by agentName/channelType/configuredID.
func (m *Manager) SendOnConfiguredChannel(agentName, channelType, configuredID, channelID, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := channelKey(agentName, channelType, configuredID)
	ch, ok := m.channels[key]
	if !ok {
		return fmt.Errorf("configured channel %q not active", key)
	}
	return ch.Send(channelID, text)
}

// SendMediaOnConfiguredChannel sends a media file using a specific configured
// channel instance identified by agentName/channelType/configuredID.
func (m *Manager) SendMediaOnConfiguredChannel(agentName, channelType, configuredID, channelID, caption, filePath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := channelKey(agentName, channelType, configuredID)
	ch, ok := m.channels[key]
	if !ok {
		return fmt.Errorf("configured channel %q not active", key)
	}
	ms, ok := ch.(MediaSender)
	if !ok {
		return fmt.Errorf("configured channel %q does not support media delivery", key)
	}
	return ms.SendMedia(channelID, caption, filePath)
}

// RouteMediaDelivery sends a media file to channelID via any running channel
// of channelType that implements MediaSender. Returns an error if no matching
// channel supports media or all attempts fail.
func (m *Manager) RouteMediaDelivery(channelType, channelID, caption, filePath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var lastErr error
	for key, ch := range m.channels {
		parts := strings.SplitN(key, "/", 3)
		if len(parts) != 3 || parts[1] != channelType {
			continue
		}
		ms, ok := ch.(MediaSender)
		if !ok {
			continue
		}
		if err := ms.SendMedia(channelID, caption, filePath); err != nil {
			lastErr = err
		} else {
			return nil
		}
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("no active channel of type %q supports media", channelType)
}

// List returns a snapshot of all currently running channels and their daemon status.
func (m *Manager) List() []ChannelStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]ChannelStatus, 0, len(m.channels))
	for key, ch := range m.channels {
		parts := strings.SplitN(key, "/", 3)
		status := ChannelStatus{
			Key:     key,
			Started: m.startTimes[key],
			Error:   m.errors[key],
		}
		if len(parts) == 3 {
			status.Agent = parts[0]
			status.Type = parts[1]
			status.ID = parts[2]
		}
		if dp, ok := ch.(DaemonProvider); ok {
			status.Daemon = dp.DaemonInfo()
		}
		result = append(result, status)
	}
	return result
}

func newChannel(cc config.ChannelConfig, agentModel string, agentFallbacks []string) Channel {
	model := cc.Model
	if model == "" {
		model = agentModel
	}
	fallbacks := cc.Fallbacks
	if len(fallbacks) == 0 {
		fallbacks = agentFallbacks
	}

	switch cc.Type {
	case "slack":
		// Token = bot token (xoxb-…), URL = app-level token (xapp-…) for Socket Mode.
		ch := NewSlackChannel(cc.URL, cc.Token, cc.AllowFrom, model, fallbacks)
		ch.disabledTools = cc.DisabledTools
		ch.showStatus = config.BoolOr(cc.ShowTyping, true)
		return ch
	case "discord":
		if cc.ShowTyping != nil {
			slog.Warn("channel config field ignored", "type", cc.Type, "id", cc.ID, "field", "show_typing", "reason", "Discord channel typing indicators are not implemented")
		}
		ch := NewDiscordChannel(cc.Token, cc.AllowFrom, model, fallbacks)
		ch.disabledTools = cc.DisabledTools
		return ch
	case "signal":
		showTyping := config.BoolOr(cc.ShowTyping, true)
		reactToEmoji := config.BoolOr(cc.ReactToEmoji, true)
		replyToReplies := config.BoolOr(cc.ReplyToReplies, true)
		sendReadReceipts := config.BoolOr(cc.SendReadReceipts, true)
		ch := NewSignalChannel(cc.ID, cc.URL, cc.AllowFrom, showTyping, reactToEmoji, replyToReplies, sendReadReceipts, model, fallbacks)
		ch.disabledTools = cc.DisabledTools
		return ch
	default:
		slog.Warn("unknown channel type", "type", cc.Type)
		return nil
	}
}

func shouldProcessIncomingMessage(meta store.ChannelMetadata, msg IncomingMessage) bool {
	if meta.EnabledAt.IsZero() {
		return true
	}
	receivedAt := msg.ReceivedAt
	if receivedAt.IsZero() {
		receivedAt = time.Now().UTC()
	}
	return !receivedAt.Before(meta.EnabledAt)
}

func channelKey(agentName, channelType, configuredID string) string {
	return fmt.Sprintf("%s/%s/%s", agentName, channelType, strings.TrimSpace(configuredID))
}

func slackConnectionKey(cc config.ChannelConfig) string {
	return "slack/" + strings.TrimSpace(cc.URL) + "/" + strings.TrimSpace(cc.Token)
}

func channelMetadata(state *store.AppState, key string) store.ChannelMetadata {
	if state == nil || state.Channels == nil {
		return store.ChannelMetadata{}
	}
	return state.Channels[key]
}
