package agent

import (
	"strings"
	"testing"
)

func TestChannelFormattingGuidance(t *testing.T) {
	got := channelFormattingGuidance("slack")
	for _, want := range []string{"<channel_formatting>", "Slack mrkdwn", "*bold*", "<https://example.com|link text>", "•", "◦"} {
		if !strings.Contains(got, want) {
			t.Fatalf("slack guidance missing %q:\n%s", want, got)
		}
	}
	if channelFormattingGuidance(" Slack ") != got {
		t.Fatal("slack guidance should ignore case and surrounding space")
	}
	for _, ch := range []string{"", "discord", "signal", "web"} {
		if g := channelFormattingGuidance(ch); g != "" {
			t.Fatalf("channel %q should get no guidance, got %q", ch, g)
		}
	}
}
