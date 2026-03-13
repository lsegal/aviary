package channels

import (
	"path"
	"strings"

	"github.com/lsegal/aviary/internal/config"
)

// allowResult contains the outcome of the allowFrom check.
type allowResult struct {
	allowed       bool
	restrictTools []string
	model         string
	fallbacks     []string
}

// checkAllowed applies allowFrom rules to an incoming message and returns
// whether the message should be forwarded, along with any per-entry tool
// restrictions.
//
// isGroup must be true when the message arrived in a group/channel context
// (as opposed to a direct/private message).  botUserID is the bot's own
// platform user ID and is used to detect @mention references in group
// messages (e.g. Slack/Discord "<@BOTID>" syntax); an empty string disables
// that check.  wasMentioned may be set instead (or in addition) when the
// platform provides an explicit "mentioned" signal (e.g. Signal's
// was_mentioned envelope field).
func checkAllowed(
	entries []config.AllowFromEntry,
	from, channelID, text string,
	isGroup bool,
	botUserID string,
	wasMentioned bool,
) allowResult {
	return checkAllowedWithOptions(entries, from, channelID, text, isGroup, botUserID, wasMentioned, false)
}

// checkAllowedReplyToSelf applies allowFrom rules for replies to the agent's
// own messages. Sender and group/channel checks remain mandatory, but
// mention-prefix and respondToMentions gates are skipped so a direct reply in
// an already-allowed conversation continues the thread without requiring a
// fresh mention.
func checkAllowedReplyToSelf(
	entries []config.AllowFromEntry,
	from, channelID string,
	isGroup bool,
) allowResult {
	return checkAllowedWithOptions(entries, from, channelID, "", isGroup, "", false, true)
}

func checkAllowedWithOptions(
	entries []config.AllowFromEntry,
	from, channelID, text string,
	isGroup bool,
	botUserID string,
	wasMentioned bool,
	ignoreMentionRules bool,
) allowResult {
	if len(entries) == 0 {
		return allowResult{}
	}
	for _, entry := range entries {
		if !config.BoolOr(entry.Enabled, true) {
			continue
		}
		for _, id := range splitFrom(entry.From) {
			if isGroup {
				// Step 1: the sender must match this entry's From list.
				if id != "*" && id != from {
					continue
				}
				// Step 2: the group/channel must be explicitly allowed.
				if !matchesAllowedGroup(entry.AllowedGroups, channelID) {
					continue
				}
				if ignoreMentionRules {
					return allowResult{
						allowed:       true,
						restrictTools: entry.RestrictTools,
						model:         entry.Model,
						fallbacks:     entry.Fallbacks,
					}
				}
				// Step 3: optional mention filtering.
				// If no mention filter is configured, all messages pass through.
				if len(entry.MentionPrefixes) == 0 && !entry.RespondToMentions {
					return allowResult{
						allowed:       true,
						restrictTools: entry.RestrictTools,
						model:         entry.Model,
						fallbacks:     entry.Fallbacks,
					}
				}
				if matchesMentionPrefixes(text, entry.MentionPrefixes) ||
					(entry.RespondToMentions && (wasMentioned || isDirectMention(text, botUserID))) {
					return allowResult{
						allowed:       true,
						restrictTools: entry.RestrictTools,
						model:         entry.Model,
						fallbacks:     entry.Fallbacks,
					}
				}
			} else {
				// Direct message: match sender ID only.
				if id == "*" || id == from {
					return allowResult{
						allowed:       true,
						restrictTools: entry.RestrictTools,
						model:         entry.Model,
						fallbacks:     entry.Fallbacks,
					}
				}
			}
		}
	}
	return allowResult{}
}

// splitFrom splits a comma-separated IDs string into individual trimmed IDs.
func splitFrom(s string) []string {
	parts := strings.Split(s, ",")
	ids := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			ids = append(ids, t)
		}
	}
	return ids
}

// matchesAllowedGroup reports whether channelID is permitted by the
// allowedGroups spec.  allowedGroups is a comma-separated list where each
// element is either "*" (any group) or an exact group/channel ID.
// An empty allowedGroups string never matches (entry is DM-only).
func matchesAllowedGroup(allowedGroups, channelID string) bool {
	if allowedGroups == "" {
		return false
	}
	for _, pattern := range splitFrom(allowedGroups) {
		if pattern == "*" || pattern == channelID {
			return true
		}
	}
	return false
}

// matchesMentionPrefixes returns true when text matches at least one pattern.
// Patterns without glob metacharacters are treated as plain prefixes (case-
// insensitive).  Patterns containing *, ? or [ are matched as glob patterns
// against the entire (lowercased, trimmed) text via path.Match.
func matchesMentionPrefixes(text string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(text))
	for _, p := range prefixes {
		pl := strings.ToLower(p)
		if strings.ContainsAny(pl, "*?[") {
			matched, _ := path.Match(pl, lower)
			if matched {
				return true
			}
		} else {
			if strings.HasPrefix(lower, pl) {
				return true
			}
		}
	}
	return false
}

// isDirectMention returns true if text contains a platform @mention of the bot.
// botUserID is the platform user ID; an empty string always returns false.
func isDirectMention(text, botUserID string) bool {
	if botUserID == "" {
		return false
	}
	return strings.Contains(text, "<@"+botUserID+">")
}
