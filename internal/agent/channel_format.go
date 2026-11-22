package agent

import "strings"

// slackFormattingGuidance tells the model to write Slack mrkdwn when its
// replies are delivered to a Slack channel.
const slackFormattingGuidance = `Replies in this conversation are delivered to Slack. Format them as Slack mrkdwn, not standard Markdown:
- Bold is *bold* (single asterisks), italic is _italic_, strikethrough is ~strike~.
- Links are <https://example.com|link text>.
- Use • for bullets and ◦ for nested bullets.
- Slack has no headings or tables: use a bold line instead of a heading, and plain lines or bullets instead of a table.
- Inline code and fenced code blocks work as usual.`

// channelFormattingGuidance returns system prompt guidance describing the
// message format of the channel a conversation replies through, or "" when the
// channel needs no guidance.
func channelFormattingGuidance(channelType string) string {
	switch strings.ToLower(strings.TrimSpace(channelType)) {
	case "slack":
		return "<channel_formatting>\n" + slackFormattingGuidance + "\n</channel_formatting>"
	default:
		return ""
	}
}
