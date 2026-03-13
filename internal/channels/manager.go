package channels

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/store"
)

// ChannelStatus describes a running channel and its daemon, if any.
type ChannelStatus struct {
	Key     string      `json:"key"`
	Agent   string      `json:"agent"`
	Type    string      `json:"type"`
	Index   int         `json:"index"`
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
}

type channelSpec struct {
	agentName      string
	channelConfig  config.ChannelConfig
	metadata       store.ChannelMetadata
	agentModel     string
	agentFallbacks []string
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
	}
}

// Reconcile idempotently starts/stops channels from the config.
// msgFn receives messages and should route them to the appropriate agent runner.
// The ch argument passed to msgFn is the channel the message arrived on; it may
// implement optional interfaces such as TypingSender.
func (m *Manager) Reconcile(ctx context.Context, cfg *config.Config, msgFn func(agentName string, channelIndex int, ch Channel, msg IncomingMessage)) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, err := store.ReadAppState()
	if err != nil {
		slog.Warn("channel state read failed", "err", err)
		state = &store.AppState{}
	}

	desired := make(map[string]struct{})
	for _, ac := range cfg.Agents {
		agentModel := config.EffectiveAgentModel(ac, cfg.Models)
		agentFallbacks := config.EffectiveAgentFallbacks(ac, cfg.Models)
		for i, cc := range ac.Channels {
			key := channelKey(ac.Name, cc.Type, i)
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
				if exists && reflect.DeepEqual(existingSpec, spec) && m.channels[key] != nil {
					continue // already running with the desired config
				}
			} else {
				delete(m.specs, key)
			}

			if !config.BoolOr(cc.Enabled, true) {
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

	// Stop channels no longer in config.
	for key := range m.channels {
		if _, ok := desired[key]; !ok {
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
	for key, ch := range m.channels {
		ch.Stop()
		m.cancels[key]()
	}
	m.channels = make(map[string]Channel)
	m.cancels = make(map[string]context.CancelFunc)
	m.startTimes = make(map[string]time.Time)
	m.errors = make(map[string]string)
	m.sinks = make(map[string]*LogSink)
	m.specs = make(map[string]channelSpec)
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
func (m *Manager) Restart(ctx context.Context, key string, msgFn func(agentName string, channelIndex int, ch Channel, msg IncomingMessage)) error {
	m.mu.Lock()
	spec, ok := m.specs[key]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("configured channel %q not found", key)
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

func (m *Manager) startChannelLocked(ctx context.Context, key string, spec channelSpec, msgFn func(agentName string, channelIndex int, ch Channel, msg IncomingMessage)) error {
	ch := newChannel(spec.channelConfig, spec.agentModel, spec.agentFallbacks)
	if ch == nil {
		return fmt.Errorf("channel %q could not be created", key)
	}

	sink := newLogSink()
	m.sinks[key] = sink
	if ss, ok := ch.(LogSinkSetter); ok {
		ss.SetLogSink(sink)
	}

	agentName := spec.agentName
	channelMeta := spec.metadata
	channelIndex := configuredChannelIndex(key)
	ch.OnMessage(func(msg IncomingMessage) {
		if !shouldProcessIncomingMessage(channelMeta, msg) {
			return
		}
		msgFn(agentName, channelIndex, ch, msg)
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

func (m *Manager) stopChannelLocked(key string) {
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
// instance identified by agentName/channelType/index.
func (m *Manager) SendOnConfiguredChannel(agentName, channelType string, index int, channelID, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := channelKey(agentName, channelType, index)
	ch, ok := m.channels[key]
	if !ok {
		return fmt.Errorf("configured channel %q not active", key)
	}
	return ch.Send(channelID, text)
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
			status.Index, _ = strconv.Atoi(parts[2])
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
		return ch
	case "discord":
		ch := NewDiscordChannel(cc.Token, cc.AllowFrom, model, fallbacks)
		ch.disabledTools = cc.DisabledTools
		return ch
	case "signal":
		showTyping := config.BoolOr(cc.ShowTyping, true)
		reactToEmoji := config.BoolOr(cc.ReactToEmoji, true)
		replyToReplies := config.BoolOr(cc.ReplyToReplies, true)
		sendReadReceipts := config.BoolOr(cc.SendReadReceipts, true)
		ch := NewSignalChannel(cc.Phone, cc.URL, cc.AllowFrom, showTyping, reactToEmoji, replyToReplies, sendReadReceipts, model, fallbacks)
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

func channelKey(agentName, channelType string, idx int) string {
	return fmt.Sprintf("%s/%s/%d", agentName, channelType, idx)
}

func configuredChannelIndex(key string) int {
	parts := strings.SplitN(key, "/", 3)
	if len(parts) != 3 {
		return 0
	}
	index, err := strconv.Atoi(parts[2])
	if err != nil {
		return 0
	}
	return index
}

func channelMetadata(state *store.AppState, key string) store.ChannelMetadata {
	if state == nil || state.Channels == nil {
		return store.ChannelMetadata{}
	}
	return state.Channels[key]
}
