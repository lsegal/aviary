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
// removed: deleteData removes each one with its messages and media, and
// otherwise each is kept as a regular chat titled after the channel.
func ReleaseChannel(a config.AgentConfig, idx int, deleteData bool) error {
	sessions, err := ChannelSessions(a, idx)
	if err != nil {
		return err
	}
	name := a.Channels[idx].EffectiveName()
	for _, sess := range sessions {
		if deleteData {
			if err := deleteSession(a.Name, sess); err != nil {
				return err
			}
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
		if err := archiveSession(a.Name, sess, title); err != nil {
			return err
		}
	}
	return nil
}

// RekeyChannels moves the sessions of channels whose type stayed but whose ID
// changed between prev and next, matched by their position in each agent's
// channel list: the channel's own session moves to its new name, and its
// conversations record the new configured ID. Without this, editing a
// channel's ID (a Signal channel's phone number) would orphan its session.
func RekeyChannels(prev, next *config.Config) error {
	if prev == nil || next == nil {
		return nil
	}
	sm := agent.NewSessionManager()
	for _, na := range next.Agents {
		pa := findAgentConfig(prev, na.Name)
		if pa == nil {
			continue
		}
		if err := rekeyAgentChannels(sm, pa, &na); err != nil {
			return err
		}
	}
	return nil
}

// findAgentConfig returns the last agent in cfg named name, or nil.
func findAgentConfig(cfg *config.Config, name string) *config.AgentConfig {
	var found *config.AgentConfig
	for i := range cfg.Agents {
		if cfg.Agents[i].Name == name {
			found = &cfg.Agents[i]
		}
	}
	return found
}

// rekeyAgentChannels rekeys each of na's channels whose counterpart at the
// same position in pa kept its type but changed its ID.
func rekeyAgentChannels(sm *agent.SessionManager, pa, na *config.AgentConfig) error {
	for j, nc := range na.Channels {
		if j >= len(pa.Channels) {
			break
		}
		if err := rekeyChannel(sm, pa, na, pa.Channels[j], nc); err != nil {
			return err
		}
	}
	return nil
}

// rekeyChannel moves the sessions owned by pa's channel pc over to na's
// channel nc, when nc is pc with a new ID that no other channel still uses.
func rekeyChannel(sm *agent.SessionManager, pa, na *config.AgentConfig, pc, nc config.ChannelConfig) error {
	oldID, newID := strings.TrimSpace(pc.ID), strings.TrimSpace(nc.ID)
	idChanged := pc.Type == nc.Type && oldID != newID && newID != ""
	if !idChanged {
		return nil
	}
	// Another channel still uses the old ID.
	if na.ChannelByKey(pc.Type, pc.ID) >= 0 {
		return nil
	}
	owned, err := ChannelSessions(*pa, pa.ChannelByKey(pc.Type, pc.ID))
	if err != nil {
		return err
	}
	for _, sess := range owned {
		if err := rekeyChannelSession(sm, na.Name, sess, pc, nc); err != nil {
			return err
		}
	}
	return nil
}

// rekeyChannelSession renames pc's own session to nc's session name (unless
// that name is taken), or rewrites the configured ID recorded by one of its
// conversations.
func rekeyChannelSession(sm *agent.SessionManager, agentName string, sess *domain.Session, pc, nc config.ChannelConfig) error {
	if sess.Name == pc.SessionName() {
		if store.FindSessionPath(agentName, nc.SessionName()) != "" {
			return nil
		}
		_, err := sm.Rekey(agentName, sess.ID, nc.SessionName())
		return err
	}
	cfg, err := store.ReadSessionChannels(agentName, sess.ID)
	if err != nil || len(cfg.Channels) == 0 {
		return nil
	}
	oldID, newID := strings.TrimSpace(pc.ID), strings.TrimSpace(nc.ID)
	for k := range cfg.Channels {
		if cfg.Channels[k].Type == pc.Type && cfg.Channels[k].ConfiguredID == oldID {
			cfg.Channels[k].ConfiguredID = newID
		}
	}
	return store.WriteSessionChannels(cfg)
}
