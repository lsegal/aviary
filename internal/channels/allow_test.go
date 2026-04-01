package channels

import (
	"testing"

	"github.com/lsegal/aviary/internal/config"

	"github.com/stretchr/testify/assert"
)

// TestSplitFrom verifies comma-separated ID splitting.
func TestSplitFrom(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"+1,+2,+3", []string{"+1", "+2", "+3"}},
		{" +1 , +2 ", []string{"+1", "+2"}},
		{"+1", []string{"+1"}},
		{"", []string{}},
		{"  ,  ", []string{}},
	}
	for _, tc := range tests {
		got := splitFrom(tc.in)
		assert.Equal(t, len(tc.want), len(got))
		if len(got) != len(tc.want) {
			continue
		}

		for i := range got {
			assert.Equal(t, tc.want[i], got[i])

		}
	}
}

// TestMatchesMentionPrefixes tests glob and prefix matching.
func TestMatchesMentionPrefixes(t *testing.T) {
	tests := []struct {
		text     string
		prefixes []string
		want     bool
	}{
		{"aviary do something", []string{"aviary"}, true},
		{"aviary do something", []string{"aviary*"}, true},
		{"hello world", []string{"aviary*"}, false},
		{"Hello World", []string{"hello"}, true}, // case-insensitive
		{"AVIARY help", []string{"aviary*"}, true},
		{"  aviary help", []string{"aviary"}, true}, // trimmed
		{"", []string{"aviary"}, false},
		{"msg", []string{}, false},
		{"aviary help", []string{"bot*", "aviary*"}, true}, // multiple patterns
		{"bot do it", []string{"bot*", "aviary*"}, true},
	}
	for _, tc := range tests {
		got := matchesMentionPrefixes(tc.text, tc.prefixes)
		assert.Equal(t, tc.want, got)

	}
}

// TestIsDirectMention verifies @mention detection.
func TestIsDirectMention(t *testing.T) {
	tests := []struct {
		text      string
		botUserID string
		want      bool
	}{
		{"<@BOTID> help", "BOTID", true},
		{"<@BOTID>", "BOTID", true},
		{"hello <@BOTID> world", "BOTID", true},
		{"<@OTHERID>", "BOTID", false},
		{"no mention here", "BOTID", false},
		{"<@BOTID>", "", false}, // empty botUserID disables
	}
	for _, tc := range tests {
		got := isDirectMention(tc.text, tc.botUserID)
		assert.Equal(t, tc.want, got)

	}
}

// TestCheckAllowed_NoEntries verifies that an empty allow-list returns not allowed.
func TestCheckAllowed_NoEntries(t *testing.T) {
	result := checkAllowed(nil, "+1", "", "hello", false, "", false)
	assert.False(t, result.allowed)

}

// TestCheckAllowed_DirectMessage tests DM allow logic.
func TestCheckAllowed_DirectMessage(t *testing.T) {
	entries := []config.AllowFromEntry{
		{From: "+15551234567"},
	}

	// Matching sender.
	result := checkAllowed(entries, "+15551234567", "", "hello", false, "", false)
	assert.True(t, result.allowed)

	// Non-matching sender.
	result = checkAllowed(entries, "+19990000000", "", "hello", false, "", false)
	assert.False(t, result.allowed)

}

// TestCheckAllowed_Wildcard tests wildcard sender matching.
func TestCheckAllowed_Wildcard(t *testing.T) {
	entries := []config.AllowFromEntry{{From: "*"}}
	result := checkAllowed(entries, "anyone", "", "hello", false, "", false)
	assert.True(t, result.allowed)

}

func TestCheckAllowed_DisabledEntryIgnored(t *testing.T) {
	disabled := false
	entries := []config.AllowFromEntry{{Enabled: &disabled, From: "*"}}
	result := checkAllowed(entries, "anyone", "", "hello", false, "", false)
	assert.False(t, result.allowed)
}

// TestCheckAllowed_GroupMessage tests group-chat allow logic.
func TestCheckAllowed_GroupMessage(t *testing.T) {
	entries := []config.AllowFromEntry{
		{From: "*", AllowedGroups: "group123"},
	}

	// Correct group, no mention filter.
	result := checkAllowed(entries, "user1", "group123", "hello", true, "", false)
	assert.True(t, result.allowed)

	// Wrong group.
	result = checkAllowed(entries, "user1", "wronggroup", "hello", true, "", false)
	assert.False(t, result.allowed)

}

// TestCheckAllowed_MentionPrefixes tests mention filtering in groups.
func TestCheckAllowed_MentionPrefixes(t *testing.T) {
	entries := []config.AllowFromEntry{
		{From: "*", AllowedGroups: "*", MentionPrefixes: []string{"aviary"}},
	}

	// Message starts with prefix.
	result := checkAllowed(entries, "user1", "groupX", "aviary help me", true, "", false)
	assert.True(t, result.allowed)

	// Message doesn't match prefix.
	result = checkAllowed(entries, "user1", "groupX", "random text", true, "", false)
	assert.False(t, result.allowed)

}

// TestCheckAllowed_RespondToMentions tests @mention handling.
func TestCheckAllowed_RespondToMentions(t *testing.T) {
	entries := []config.AllowFromEntry{
		{From: "*", AllowedGroups: "*", RespondToMentions: true},
	}

	// Platform mention (wasMentioned).
	result := checkAllowed(entries, "user1", "groupX", "hey there", true, "", true)
	assert.True(t, result.allowed)

	// Bot @mention syntax.
	result = checkAllowed(entries, "user1", "groupX", "<@BOTID> help", true, "BOTID", false)
	assert.True(t, result.allowed)

	// Discord nickname @mention syntax.
	result = checkAllowed(entries, "user1", "groupX", "<@!BOTID> help", true, "BOTID", false)
	assert.True(t, result.allowed)

	// Not mentioned.
	result = checkAllowed(entries, "user1", "groupX", "no mention", true, "BOTID", false)
	assert.False(t, result.allowed)

}

// TestCheckAllowed_RestrictTools verifies tool restriction is passed through.
func TestCheckAllowed_RestrictTools(t *testing.T) {
	entries := []config.AllowFromEntry{
		{From: "+1", RestrictTools: []string{"tool_a", "tool_b"}},
	}
	result := checkAllowed(entries, "+1", "", "hello", false, "", false)
	assert.True(t, result.allowed)
	assert.Equal(t, 2, len(result.restrictTools))

}

// TestMatchesAllowedGroup verifies group matching logic.
func TestMatchesAllowedGroup(t *testing.T) {
	tests := []struct {
		allowedGroups string
		channelID     string
		want          bool
	}{
		{"*", "anything", true},
		{"group1", "group1", true},
		{"group1,group2", "group2", true},
		{"group1", "group2", false},
		{"", "group1", false}, // empty = DM-only
	}
	for _, tc := range tests {
		got := matchesAllowedGroup(tc.allowedGroups, tc.channelID)
		assert.Equal(t, tc.want, got)

	}
}

// TestCheckAllowed_MentionPrefixGroupOnly tests that when mentionPrefixGroupOnly=false,
// mention filters also gate direct messages.
func TestCheckAllowed_MentionPrefixGroupOnly_DefaultTrueAllowsDMs(t *testing.T) {
	// Default (nil / true): DMs pass without a prefix.
	entries := []config.AllowFromEntry{
		{From: "*", MentionPrefixes: []string{"aviary"}},
	}
	result := checkAllowed(entries, "+1", "+1", "hello", false, "", false)
	assert.True(t, result.allowed)
}

func TestCheckAllowed_MentionPrefixGroupOnly_FalseRequiresPrefixInDMs(t *testing.T) {
	f := false
	entries := []config.AllowFromEntry{
		{From: "*", MentionPrefixes: []string{"aviary"}, MentionPrefixGroupOnly: &f},
	}

	// No prefix → blocked.
	result := checkAllowed(entries, "+1", "+1", "hello", false, "", false)
	assert.False(t, result.allowed)

	// Matching prefix → allowed.
	result = checkAllowed(entries, "+1", "+1", "aviary do this", false, "", false)
	assert.True(t, result.allowed)
}

func TestCheckAllowed_MentionPrefixGroupOnly_FalseRespondToMentionsInDMs(t *testing.T) {
	f := false
	entries := []config.AllowFromEntry{
		{From: "*", RespondToMentions: true, MentionPrefixGroupOnly: &f},
	}

	// Not mentioned → blocked.
	result := checkAllowed(entries, "+1", "+1", "hello", false, "BOTID", false)
	assert.False(t, result.allowed)

	// wasMentioned=true → allowed.
	result = checkAllowed(entries, "+1", "+1", "hello", false, "BOTID", true)
	assert.True(t, result.allowed)

	// Platform @mention syntax → allowed.
	result = checkAllowed(entries, "+1", "+1", "<@BOTID> help", false, "BOTID", false)
	assert.True(t, result.allowed)
}

func TestCheckAllowed_MentionPrefixGroupOnly_FalseReplyToSelfBypasses(t *testing.T) {
	// ignoreMentionRules=true (reply-to-self path) must bypass the DM filter.
	f := false
	entries := []config.AllowFromEntry{
		{From: "*", MentionPrefixes: []string{"aviary"}, MentionPrefixGroupOnly: &f},
	}
	result := checkAllowedReplyToSelf(entries, "+1", "+1", false)
	assert.True(t, result.allowed)
}

func TestCheckAllowed_MentionPrefixGroupOnly_FalseNoFiltersStillAllowsDMs(t *testing.T) {
	// mentionPrefixGroupOnly=false but no filters configured → still allow.
	f := false
	entries := []config.AllowFromEntry{
		{From: "*", MentionPrefixGroupOnly: &f},
	}
	result := checkAllowed(entries, "+1", "+1", "hello", false, "", false)
	assert.True(t, result.allowed)
}

func TestCheckAllowed_ExcludePrefixes_BlocksDM(t *testing.T) {
	entries := []config.AllowFromEntry{
		{From: "*", ExcludePrefixes: []string{"!", "/"}},
	}
	assert.False(t, checkAllowed(entries, "+1", "+1", "!ignore this", false, "", false).allowed)
	assert.False(t, checkAllowed(entries, "+1", "+1", "/command", false, "", false).allowed)
	assert.True(t, checkAllowed(entries, "+1", "+1", "hello", false, "", false).allowed)
}

func TestCheckAllowed_ExcludePrefixes_BlocksGroup(t *testing.T) {
	entries := []config.AllowFromEntry{
		{From: "*", AllowedGroups: "*", ExcludePrefixes: []string{"!"}},
	}
	assert.False(t, checkAllowed(entries, "+1", "grp1", "!ignore", true, "", false).allowed)
	assert.True(t, checkAllowed(entries, "+1", "grp1", "aviary help", true, "", false).allowed)
}

func TestCheckAllowed_ExcludePrefixes_GlobPattern(t *testing.T) {
	entries := []config.AllowFromEntry{
		{From: "*", ExcludePrefixes: []string{"bot:*"}},
	}
	assert.False(t, checkAllowed(entries, "+1", "+1", "bot: do something", false, "", false).allowed)
	assert.True(t, checkAllowed(entries, "+1", "+1", "human: do something", false, "", false).allowed)
}
