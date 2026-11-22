package channels

import "testing"

func TestSlackMrkdwn(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain", "hello world", "hello world"},
		{"bold stars", "this is **bold** text", "this is *bold* text"},
		{"bold underscores", "this is __bold__ text", "this is *bold* text"},
		{"identifier underscores untouched", "call __init__ or foo__bar__baz", "call *init* or foo__bar__baz"},
		{"bold italic", "***wow***", "*_wow_*"},
		{"italic underscore kept", "_italic_", "_italic_"},
		{"strike", "~~gone~~", "~gone~"},
		{"link", "see [#876](https://github.com/lsegal/aviary/issues/876)", "see <https://github.com/lsegal/aviary/issues/876|#876>"},
		{"link label equals url", "[https://x.io](https://x.io)", "<https://x.io>"},
		{"image", "![logo](https://x.io/a.png)", "<https://x.io/a.png|logo>"},
		{"bold link", "**[docs](https://x.io)**", "*<https://x.io|docs>*"},
		{"heading", "# Summary", "*Summary*"},
		{"heading with bold", "## **Result** ##", "*Result*"},
		{"bullets", "- one\n- two", "• one\n• two"},
		{"nested bullets", "- one\n  - two\n    - three\n* four", "• one\n    ◦ two\n        ▪ three\n• four"},
		{"numbered list kept", "1. first\n2. second", "1. first\n2. second"},
		{"inline code untouched", "run `**x** [a](https://b.c)` now **ok**", "run `**x** [a](https://b.c)` now *ok*"},
		{"double backtick code", "``a ` **b**`` **c**", "``a ` **b**`` *c*"},
		{"fenced code untouched", "**a**\n```go\n# not heading\n- not bullet\nx := **y**\n```\n**b**", "*a*\n```go\n# not heading\n- not bullet\nx := **y**\n```\n*b*"},
		{"tilde fence untouched", "~~~\n**x**\n~~~", "~~~\n**x**\n~~~"},
		{"mrkdwn idempotent", "*bold* _it_ ~st~ <https://x.io|link>\n• item\n    ◦ nested", "*bold* _it_ ~st~ <https://x.io|link>\n• item\n    ◦ nested"},
		{"horizontal rule kept", "---", "---"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slackMrkdwn(tt.in)
			if got != tt.want {
				t.Fatalf("slackMrkdwn(%q)\n got: %q\nwant: %q", tt.in, got, tt.want)
			}
			if again := slackMrkdwn(got); again != got {
				t.Fatalf("slackMrkdwn not idempotent for %q: %q -> %q", tt.in, got, again)
			}
		})
	}
}
