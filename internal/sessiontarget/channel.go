package sessiontarget

import (
	"fmt"
	"sort"
	"strings"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/channels"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

// Ways to dispose of a removed channel's sessions.
const (
	ReleaseArchive = "archive"
	ReleaseDelete  = "delete"
)

// EnsureChannel creates the channel's own session when it has none yet and
// points the session's delivery at the channel's DeliverTo conversation. It is
// safe to call on every config load.
func EnsureChannel(agentName string, cc config.ChannelConfig, mgr *channels.Manager) (*domain.Session, error) {
	if strings.TrimSpace(agentName) == "" || strings.TrimSpace(cc.Type) == "" || strings.TrimSpace(cc.ID) == "" {
		return nil, fmt.Errorf("channel needs an agent, a type and an id")
	}
	sess, err := agent.NewSessionManager().GetOrCreateNamedTyped(agentName, cc.SessionName(), domain.SessionTypeChannel)
	if err != nil {
		return nil, err
	}
	target := store.SessionChannel{
		Type:         strings.TrimSpace(cc.Type),
		ConfiguredID: strings.TrimSpace(cc.ID),
		ID:           strings.TrimSpace(cc.DeliverTo),
	}
	if err := store.SetSessionChannelTarget(agentName, sess.ID, target); err != nil {
		return nil, err
	}
	// DeliverTo may have changed or been cleared since the last load.
	agent.ClearSessionDelivery(agentName, sess.ID)
	Register(agentName, agentName, sess.ID, target, mgr)
	return sess, nil
}

// EnsureChannels runs EnsureChannel for every channel in cfg, returning the
// first error after trying them all.
func EnsureChannels(cfg *config.Config, mgr *channels.Manager) error {
	if cfg == nil {
		return nil
	}
	var first error
	for _, a := range cfg.Agents {
		for _, cc := range a.Channels {
			if _, err := EnsureChannel(a.Name, cc, mgr); err != nil && first == nil {
				first = fmt.Errorf("agent %q channel %q: %w", a.Name, cc.EffectiveName(), err)
			}
		}
	}
	return first
}

// ChannelOwner returns the index in a.Channels of the channel that owns sess,
// or -1. Only channel sessions are owned: the channel's own session and the
// conversations it received. Archived sessions are regular chats.
func ChannelOwner(a config.AgentConfig, sess *domain.Session) int {
	if sess == nil || sess.Type != domain.SessionTypeChannel || len(a.Channels) == 0 {
		return -1
	}
	channelType, configuredID := "", ""
	if cfg, err := store.ReadSessionChannels(a.Name, sess.ID); err == nil && len(cfg.Channels) > 0 {
		channelType, configuredID = cfg.Channels[0].Type, cfg.Channels[0].ConfiguredID
	}
	return a.ChannelForSession(sess.Name, channelType, configuredID)
}

// ChannelSessions lists the sessions of a.Channels[idx]: its own session
// first, then the conversations it received, most recent first.
func ChannelSessions(a config.AgentConfig, idx int) ([]*domain.Session, error) {
	if idx < 0 || idx >= len(a.Channels) {
		return nil, fmt.Errorf("channel index %d out of range", idx)
	}
	all, err := agent.NewSessionManager().List(a.Name)
	if err != nil {
		return nil, err
	}
	own := a.Channels[idx].SessionName()
	var out []*domain.Session
	for _, sess := range all {
		if ChannelOwner(a, sess) == idx {
			out = append(out, sess)
		}
	}
	active := func(s *domain.Session) int64 {
		if s.LastMessageAt != nil {
			return s.LastMessageAt.UnixNano()
		}
		return s.CreatedAt.UnixNano()
	}
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].Name == own) != (out[j].Name == own) {
			return out[i].Name == own
		}
		return active(out[i]) > active(out[j])
	})
	return out, nil
}

// ReleaseChannel disposes of the sessions of a.Channels[idx], which is being
// removed: archive keeps each one as a regular chat titled after the channel,
// and delete removes it with its messages.
func ReleaseChannel(a config.AgentConfig, idx int, mode string) error {
	if mode != ReleaseArchive && mode != ReleaseDelete {
		return fmt.Errorf("unknown session disposal %q; use %q or %q", mode, ReleaseArchive, ReleaseDelete)
	}
	sessions, err := ChannelSessions(a, idx)
	if err != nil {
		return err
	}
	name := a.Channels[idx].EffectiveName()
	sm := agent.NewSessionManager()
	for _, sess := range sessions {
		if mode == ReleaseDelete {
			if err := sm.Delete(a.Name, sess.ID); err != nil {
				return err
			}
			agent.ClearSessionDelivery(a.Name, sess.ID)
			continue
		}
		title := name
		if sess.Name != a.Channels[idx].SessionName() {
			label := sess.Title
			if label == "" {
				label = sess.Name
			}
			title = name + ": " + label
		}
		if _, err := sm.Archive(a.Name, sess.ID, title+" (archived)"); err != nil {
			return err
		}
	}
	return nil
}
