package domain

// ChannelType identifies the kind of communication channel.
type ChannelType string

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
	Token     string      `json:"token"`      // auth reference
	ChannelID string      `json:"channel_id"` // Slack channel name, Discord channel ID, etc.
	Phone     string      `json:"phone"`      // Signal phone number
	AllowFrom []string    `json:"allow_from"`
}
