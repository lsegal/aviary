package channels

import (
	"testing"

	"github.com/lsegal/aviary/internal/config"
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
		if len(got) != len(tc.want) {
			t.Errorf("splitFrom(%q) = %v; want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("splitFrom(%q)[%d] = %q; want %q", tc.in, i, got[i], tc.want[i])
			}
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
		if got != tc.want {
			t.Errorf("matchesMentionPrefixes(%q, %v) = %v; want %v", tc.text, tc.prefixes, got, tc.want)
		}
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
		if got != tc.want {
			t.Errorf("isDirectMention(%q, %q) = %v; want %v", tc.text, tc.botUserID, got, tc.want)
		}
	}
}

// TestCheckAllowed_NoEntries verifies that an empty allow-list returns not allowed.
func TestCheckAllowed_NoEntries(t *testing.T) {
	result := checkAllowed(nil, "+1", "", "hello", false, "", false)
	if result.allowed {
		t.Error("expected not allowed with no entries")
	}
}

// TestCheckAllowed_DirectMessage tests DM allow logic.
func TestCheckAllowed_DirectMessage(t *testing.T) {
	entries := []config.AllowFromEntry{
		{From: "+15551234567"},
	}

	// Matching sender.
	result := checkAllowed(entries, "+15551234567", "", "hello", false, "", false)
	if !result.allowed {
		t.Error("expected allowed for matching DM sender")
	}

	// Non-matching sender.
	result = checkAllowed(entries, "+19990000000", "", "hello", false, "", false)
	if result.allowed {
		t.Error("expected not allowed for non-matching DM sender")
	}
}

// TestCheckAllowed_Wildcard tests wildcard sender matching.
func TestCheckAllowed_Wildcard(t *testing.T) {
	entries := []config.AllowFromEntry{{From: "*"}}
	result := checkAllowed(entries, "anyone", "", "hello", false, "", false)
	if !result.allowed {
		t.Error("expected allowed for wildcard DM")
	}
}

// TestCheckAllowed_GroupMessage tests group-chat allow logic.
func TestCheckAllowed_GroupMessage(t *testing.T) {
	entries := []config.AllowFromEntry{
		{From: "*", AllowedGroups: "group123"},
	}

	// Correct group, no mention filter.
	result := checkAllowed(entries, "user1", "group123", "hello", true, "", false)
	if !result.allowed {
		t.Error("expected allowed for matching group")
	}

	// Wrong group.
	result = checkAllowed(entries, "user1", "wronggroup", "hello", true, "", false)
	if result.allowed {
		t.Error("expected not allowed for wrong group")
	}
}

// TestCheckAllowed_MentionPrefixes tests mention filtering in groups.
func TestCheckAllowed_MentionPrefixes(t *testing.T) {
	entries := []config.AllowFromEntry{
		{From: "*", AllowedGroups: "*", MentionPrefixes: []string{"aviary"}},
	}

	// Message starts with prefix.
	result := checkAllowed(entries, "user1", "groupX", "aviary help me", true, "", false)
	if !result.allowed {
		t.Error("expected allowed for matching prefix")
	}

	// Message doesn't match prefix.
	result = checkAllowed(entries, "user1", "groupX", "random text", true, "", false)
	if result.allowed {
		t.Error("expected not allowed for non-matching prefix")
	}
}

// TestCheckAllowed_RespondToMentions tests @mention handling.
func TestCheckAllowed_RespondToMentions(t *testing.T) {
	entries := []config.AllowFromEntry{
		{From: "*", AllowedGroups: "*", RespondToMentions: true},
	}

	// Platform mention (wasMentioned).
	result := checkAllowed(entries, "user1", "groupX", "hey there", true, "", true)
	if !result.allowed {
		t.Error("expected allowed when wasMentioned=true")
	}

	// Bot @mention syntax.
	result = checkAllowed(entries, "user1", "groupX", "<@BOTID> help", true, "BOTID", false)
	if !result.allowed {
		t.Error("expected allowed when message contains @mention")
	}

	// Not mentioned.
	result = checkAllowed(entries, "user1", "groupX", "no mention", true, "BOTID", false)
	if result.allowed {
		t.Error("expected not allowed when not mentioned and no prefix match")
	}
}

// TestCheckAllowed_RestrictTools verifies tool restriction is passed through.
func TestCheckAllowed_RestrictTools(t *testing.T) {
	entries := []config.AllowFromEntry{
		{From: "+1", RestrictTools: []string{"tool_a", "tool_b"}},
	}
	result := checkAllowed(entries, "+1", "", "hello", false, "", false)
	if !result.allowed {
		t.Fatal("expected allowed")
	}
	if len(result.restrictTools) != 2 {
		t.Errorf("expected 2 restrict tools, got %d", len(result.restrictTools))
	}
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
		if got != tc.want {
			t.Errorf("matchesAllowedGroup(%q, %q) = %v; want %v", tc.allowedGroups, tc.channelID, got, tc.want)
		}
	}
}
