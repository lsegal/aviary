package channels

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/lsegal/aviary/internal/config"
)

// Manager manages channel lifecycle across all agents.
type Manager struct {
	mu       sync.Mutex
	channels map[string]Channel       // key: agentName+"/"+channelType+"/"+channelID
	cancels  map[string]context.CancelFunc
}

// NewManager creates a channel Manager.
func NewManager() *Manager {
	return &Manager{
		channels: make(map[string]Channel),
		cancels:  make(map[string]context.CancelFunc),
	}
}

// Reconcile idempotently starts/stops channels from the config.
// msgFn receives messages and should route them to the appropriate agent runner.
func (m *Manager) Reconcile(ctx context.Context, cfg *config.Config, msgFn func(agentName string, msg IncomingMessage)) {
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

			agentName := ac.Name
			ch.OnMessage(func(msg IncomingMessage) {
				msgFn(agentName, msg)
			})

			cctx, cancel := context.WithCancel(ctx)
			m.channels[key] = ch
			m.cancels[key] = cancel

			go func(k string, c Channel) {
				if err := c.Start(cctx); err != nil && cctx.Err() == nil {
					slog.Warn("channel error", "key", k, "err", err)
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
}

func newChannel(cc config.ChannelConfig) Channel {
	switch cc.Type {
	case "slack":
		// For Slack: Token field holds the bot token; a separate App-Level token
		// would be needed for Socket Mode. For now use a placeholder.
		return NewSlackChannel("", cc.Token, cc.AllowFrom)
	case "discord":
		return NewDiscordChannel(cc.Token, cc.AllowFrom)
	case "signal":
		return NewSignalChannel(cc.Phone, cc.AllowFrom)
	default:
		slog.Warn("unknown channel type", "type", cc.Type)
		return nil
	}
}

func channelKey(agentName, channelType string, idx int) string {
	return fmt.Sprintf("%s/%s/%d", agentName, channelType, idx)
}
