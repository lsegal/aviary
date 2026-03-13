package domain

// ChannelType identifies the kind of communication channel.
type ChannelType string

// ChannelType values.
const (
	ChannelTypeSlack   ChannelType = "slack"
	ChannelTypeDiscord ChannelType = "discord"
	ChannelTypeSignal  ChannelType = "signal"
)

// Channel represents a communication channel attached to an agent.
type Channel struct {
	ID        string      `json:"id"`
	AgentID   string      `json:"agent_id"`
	Type      ChannelType `json:"type"`
	Token     string      `json:"token"`     // auth reference
	TargetID  string      `json:"target_id"` // Stable per-channel identifier, e.g. Slack bot id or Signal phone number
	AllowFrom []string    `json:"allow_from"`
}
