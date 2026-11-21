package config

import (
	"log/slog"
	"strings"
)

// ChannelTargetPrefix starts a delivery target that names a channel:
// "channel:<name>". A task, report or notification sent there lands in the
// channel's own session.
const ChannelTargetPrefix = "channel:"

// channelTypes are the channel types a legacy "<type>:<id>:<target>" target or
// a channel session name can start with.
var channelTypes = []string{"slack", "discord", "signal"}

// EffectiveName returns the channel's display name: Name, else its ID, else
// its type.
func (c ChannelConfig) EffectiveName() string {
	if name := strings.TrimSpace(c.Name); name != "" {
		return name
	}
	if id := strings.TrimSpace(c.ID); id != "" {
		return id
	}
	return c.Type
}

// SessionName is the name of the channel's own session. It is keyed by the
// channel's type and ID, not its name, so a rename keeps the session.
func (c ChannelConfig) SessionName() string {
	return "channel:" + strings.TrimSpace(c.Type) + ":" + strings.TrimSpace(c.ID)
}

// ChannelTarget returns the delivery target that picks the channel named name.
func ChannelTarget(name string) string {
	return ChannelTargetPrefix + strings.TrimSpace(name)
}

// ChannelTargetName returns the channel name a "channel:<name>" target picks,
// and whether target is one.
func ChannelTargetName(target string) (string, bool) {
	target = strings.TrimSpace(target)
	if !strings.HasPrefix(target, ChannelTargetPrefix) {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(target, ChannelTargetPrefix)), true
}

// ChannelByName returns the index of the agent's channel named name, matched
// case-insensitively, or -1.
func (a AgentConfig) ChannelByName(name string) int {
	name = strings.TrimSpace(name)
	if name == "" {
		return -1
	}
	for i, ch := range a.Channels {
		if strings.EqualFold(ch.EffectiveName(), name) {
			return i
		}
	}
	return -1
}

// ChannelByKey returns the index of the agent's channel with this type and
// ID, or -1.
func (a AgentConfig) ChannelByKey(channelType, id string) int {
	channelType, id = strings.TrimSpace(channelType), strings.TrimSpace(id)
	for i, ch := range a.Channels {
		if strings.EqualFold(ch.Type, channelType) && strings.TrimSpace(ch.ID) == id {
			return i
		}
	}
	return -1
}

// ChannelForSession returns the index of the channel that owns the session
// named sessionName, given the configured channel it delivers through
// (configuredID, empty when unknown), or -1. A channel owns its own session
// and the conversations it receives ("<type>:<conversation>"). Without a
// configured ID, a conversation belongs to the agent's only channel of its
// type.
func (a AgentConfig) ChannelForSession(sessionName, channelType, configuredID string) int {
	for i, ch := range a.Channels {
		if ch.SessionName() == sessionName {
			return i
		}
	}
	if channelType == "" {
		channelType = sessionChannelType(sessionName)
	}
	if channelType == "" {
		return -1
	}
	if configuredID != "" {
		return a.ChannelByKey(channelType, configuredID)
	}
	found := -1
	for i, ch := range a.Channels {
		if !strings.EqualFold(ch.Type, channelType) {
			continue
		}
		if found >= 0 {
			return -1
		}
		found = i
	}
	return found
}

// sessionChannelType returns the channel type a conversation session name
// ("signal:+1555…", "slack:C123:…") starts with, or "".
func sessionChannelType(name string) string {
	kind, rest, ok := strings.Cut(name, ":")
	if !ok || rest == "" {
		return ""
	}
	for _, t := range channelTypes {
		if kind == t {
			return t
		}
	}
	return ""
}

// migrateChannelTargets moves delivery targets that pick a channel by its
// configured ID, or by one of its conversation sessions, to the channel's
// name: "<type>:<configured-id>:<target>" and "session:<type>:<conversation>"
// become "channel:<name>". A channel with no DeliverTo takes the target's
// conversation as its own.
func migrateChannelTargets(cfg *Config) {
	byName := map[string]int{}
	for i := range cfg.Agents {
		byName[cfg.Agents[i].Name] = i
	}
	for i := range cfg.Agents {
		agent := &cfg.Agents[i]
		for j := range agent.Tasks {
			agent.Tasks[j].Target = migrateChannelTarget(agent, agent.Tasks[j].Target)
		}
		for j := range agent.Projects {
			reports := agent.Projects[j].Reports
			if reports == nil {
				continue
			}
			k, ok := byName[strings.TrimSpace(reports.Agent)]
			if !ok {
				continue
			}
			for t := range reports.Targets {
				reports.Targets[t] = migrateChannelTarget(&cfg.Agents[k], reports.Targets[t])
			}
		}
	}
}

func migrateChannelTarget(agent *AgentConfig, target string) string {
	trimmed := strings.TrimSpace(target)
	if ref, ok := strings.CutPrefix(trimmed, "session:"); ok {
		ref = strings.TrimSpace(ref)
		idx := agent.ChannelForSession(ref, "", "")
		if idx < 0 {
			return target
		}
		ch := &agent.Channels[idx]
		if ch.DeliverTo == "" && ref != ch.SessionName() {
			// "slack:C123:1700.1" keeps only the conversation.
			_, rest, _ := strings.Cut(ref, ":")
			ch.DeliverTo, _, _ = strings.Cut(rest, ":")
		}
		return ChannelTarget(ch.EffectiveName())
	}
	parts := strings.SplitN(trimmed, ":", 3)
	if len(parts) != 3 || sessionChannelType(trimmed) == "" {
		return target
	}
	idx := agent.ChannelByKey(parts[0], parts[1])
	if idx < 0 {
		return target
	}
	ch := &agent.Channels[idx]
	deliverTo := strings.TrimSpace(parts[2])
	if ch.DeliverTo == "" {
		ch.DeliverTo = deliverTo
	} else if ch.DeliverTo != deliverTo {
		slog.Warn("config: channel target moved to the channel's own conversation",
			"agent", agent.Name, "channel", ch.EffectiveName(), "target", trimmed, "deliver_to", ch.DeliverTo)
	}
	return ChannelTarget(ch.EffectiveName())
}
