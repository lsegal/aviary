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
	return allowRequest{
		from:         from,
		channelID:    channelID,
		text:         text,
		botUserID:    botUserID,
		isGroup:      isGroup,
		wasMentioned: wasMentioned,
	}.check(entries)
}

// checkAllowedReplyContinuation applies allowFrom rules for message replies.
// Sender and group/channel checks remain mandatory, but mention-prefix and
// respondToMentions gates are skipped so an already-allowed conversation
// continues without requiring a fresh mention.
func checkAllowedReplyContinuation(
	entries []config.AllowFromEntry,
	from, channelID string,
	isGroup bool,
) allowResult {
	return allowRequest{
		from:               from,
		channelID:          channelID,
		isGroup:            isGroup,
		ignoreMentionRules: true,
	}.check(entries)
}

// check applies entries' allowFrom rules to the message r describes.
func (r allowRequest) check(entries []config.AllowFromEntry) allowResult {
	for _, entry := range entries {
		if !config.BoolOr(entry.Enabled, true) {
			continue
		}
		// ExcludePrefixes is a global denylist: drop the message if any pattern
		// matches, regardless of whether it is a DM or group message.
		if matchesMentionPrefixes(r.text, entry.ExcludePrefixes) {
			return allowResult{}
		}
		for _, id := range splitFrom(entry.From) {
			if r.idAllows(entry, id) {
				return allowResult{
					allowed:       true,
					restrictTools: entry.RestrictTools,
					model:         entry.Model,
					fallbacks:     entry.Fallbacks,
				}
			}
		}
	}
	return allowResult{}
}

// allowRequest is the incoming message context checked against allowFrom entries.
type allowRequest struct {
	from, channelID, text, botUserID string
	isGroup                          bool
	wasMentioned                     bool
	ignoreMentionRules               bool
}

// idAllows reports whether entry's sender id (one element of its From list)
// admits the message.
func (r allowRequest) idAllows(entry config.AllowFromEntry, id string) bool {
	// The sender must match this entry's From list.
	if id != "*" && id != r.from {
		return false
	}
	if r.isGroup {
		// The group/channel must be explicitly allowed. Mention filtering is
		// optional: with no filter configured, all messages pass through.
		if !matchesAllowedGroup(entry.AllowedGroups, r.channelID) {
			return false
		}
		return r.ignoreMentionRules || !hasMentionFilter(entry) || r.mentionMatches(entry)
	}
	// Direct message: when mentionPrefixGroupOnly=false, mention filters also gate DMs.
	gated := !config.BoolOr(entry.MentionPrefixGroupOnly, true) && !r.ignoreMentionRules && hasMentionFilter(entry)
	return !gated || r.mentionMatches(entry)
}

// mentionMatches reports whether the message satisfies entry's mention filter.
func (r allowRequest) mentionMatches(entry config.AllowFromEntry) bool {
	return matchesMentionPrefixes(r.text, entry.MentionPrefixes) ||
		(entry.RespondToMentions && (r.wasMentioned || isDirectMention(r.text, r.botUserID)))
}

// hasMentionFilter reports whether entry configures any mention filtering.
func hasMentionFilter(entry config.AllowFromEntry) bool {
	return len(entry.MentionPrefixes) > 0 || entry.RespondToMentions
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
		if strings.TrimSpace(pl) == "*" {
			return true
		}
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
	return strings.Contains(text, "<@"+botUserID+">") ||
		strings.Contains(text, "<@!"+botUserID+">")
}
