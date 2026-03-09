package channels

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/config"
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
}

// NewManager creates a channel Manager.
func NewManager() *Manager {
	return &Manager{
		channels:   make(map[string]Channel),
		cancels:    make(map[string]context.CancelFunc),
		startTimes: make(map[string]time.Time),
		errors:     make(map[string]string),
		sinks:      make(map[string]*LogSink),
	}
}

// Reconcile idempotently starts/stops channels from the config.
// msgFn receives messages and should route them to the appropriate agent runner.
// The ch argument passed to msgFn is the channel the message arrived on; it may
// implement optional interfaces such as TypingSender.
func (m *Manager) Reconcile(ctx context.Context, cfg *config.Config, msgFn func(agentName string, ch Channel, msg IncomingMessage)) {
	m.mu.Lock()
	defer m.mu.Unlock()

	desired := make(map[string]struct{})
	for _, ac := range cfg.Agents {
		for i, cc := range ac.Channels {
			key := channelKey(ac.Name, cc.Type, i)
			desired[key] = struct{}{}

			if _, exists := m.channels[key]; exists {
				continue // already running
			}

			ch := newChannel(cc)
			if ch == nil {
				continue
			}

			// Attach a log sink so managed subprocess output is capturable.
			sink := newLogSink()
			m.sinks[key] = sink
			if ss, ok := ch.(LogSinkSetter); ok {
				ss.SetLogSink(sink)
			}

			agentName := ac.Name
			ch.OnMessage(func(msg IncomingMessage) {
				msgFn(agentName, ch, msg)
			})

			cctx, cancel := context.WithCancel(ctx)
			m.channels[key] = ch
			m.cancels[key] = cancel
			m.startTimes[key] = time.Now()

			go func(k string, c Channel) {
				if err := c.Start(cctx); err != nil && cctx.Err() == nil {
					slog.Warn("channel error", "key", k, "err", err)
					m.mu.Lock()
					m.errors[k] = err.Error()
					m.mu.Unlock()
				}
			}(key, ch)

			slog.Info("channel started", "key", key, "type", cc.Type)
		}
	}

	// Stop channels no longer in config.
	for key := range m.channels {
		if _, ok := desired[key]; !ok {
			m.channels[key].Stop()
			m.cancels[key]()
			delete(m.channels, key)
			delete(m.cancels, key)
			delete(m.startTimes, key)
			delete(m.errors, key)
			delete(m.sinks, key)
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

func newChannel(cc config.ChannelConfig) Channel {
	switch cc.Type {
	case "slack":
		// Token = bot token (xoxb-…), URL = app-level token (xapp-…) for Socket Mode.
		return NewSlackChannel(cc.URL, cc.Token, cc.AllowFrom)
	case "discord":
		return NewDiscordChannel(cc.Token, cc.AllowFrom)
	case "signal":
		return NewSignalChannel(cc.Phone, cc.URL, cc.AllowFrom)
	default:
		slog.Warn("unknown channel type", "type", cc.Type)
		return nil
	}
}

func channelKey(agentName, channelType string, idx int) string {
	return fmt.Sprintf("%s/%s/%d", agentName, channelType, idx)
}
