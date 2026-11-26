package channels

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatSignalMessage_Table(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		text   string
		styles []string
	}{
		{"whitespace only is returned unchanged", "  \n\t ", "  \n\t ", nil},
		{"inline code", "run `go test` now", "run go test now", []string{"4:7:MONOSPACE"}},
		{"empty inline code adds no style", "a``b", "ab", nil},
		{"unterminated backtick is literal", "a `b c", "a `b c", nil},
		{"inline code cannot span lines", "a `b\nc` d", "a `b\nc` d", nil},
		{"fence without language line", "```code```", "code", []string{"0:4:MONOSPACE"}},
		{"fence with language line", "x\n```go\nfmt.Println()\n```\ny", "x\nfmt.Println()\ny", []string{"2:13:MONOSPACE"}},
		{"empty fence adds no style", "a``````b", "ab", nil},
		{"unterminated fence is literal", "```abc", "```abc", nil},
		{"italic underscore", "an _it_ word", "an it word", []string{"3:2:ITALIC"}},
		{"bold underscore", "__b__", "b", []string{"0:1:BOLD"}},
		{"nested italic in bold", "**bold *it* x**", "bold it x", []string{"5:2:ITALIC", "0:9:BOLD"}},
		{"nested bold in strikethrough", "~~a **b**~~", "a b", []string{"2:1:BOLD", "0:3:STRIKETHROUGH"}},
		{"nested italic in spoiler", "||_s_ t||", "s t", []string{"0:1:ITALIC", "0:3:SPOILER"}},
		{"nested strike in double underscore", "__~~x~~__", "x", []string{"0:1:STRIKETHROUGH", "0:1:BOLD"}},
		{"nested code in italic", "*see `x`*", "see x", []string{"4:1:MONOSPACE", "0:5:ITALIC"}},
		{"nested bold in underscore italic", "_a **b**_", "a b", []string{"2:1:BOLD", "0:3:ITALIC"}},
		{"unmatched bold is literal", "**open", "**open", nil},
		{"unmatched double underscore is literal", "__open", "__open", nil},
		{"unmatched strikethrough is literal", "~~open", "~~open", nil},
		{"unmatched spoiler is literal", "||open", "||open", nil},
		{"unmatched italic is literal", "*open", "*open", nil},
		{"unmatched underscore italic is literal", "_open", "_open", nil},
		{"empty bold adds no style", "a ****b", "a b", nil},
		{"empty strikethrough adds no style", "~~~~", "", nil},
		{"empty spoiler adds no style", "||||", "", nil},
		{"empty double underscore adds no style", "____", "", nil},
		{"underscores inside a word are literal", "snake_case_name", "snake_case_name", nil},
		{"asterisks inside a word are literal", "a*b*c", "a*b*c", nil},
		{"asterisk followed by space is literal", "2 * 3", "2 * 3", nil},
		{"trailing asterisk is literal", "end*", "end*", nil},
		{"underscore followed by space is literal", "a _ b", "a _ b", nil},
		{"emphasis after punctuation opens", "(*x*)", "(x)", []string{"1:1:ITALIC"}},
		{"emoji shifts UTF-16 offsets", "😀 **hi**", "😀 hi", []string{"3:2:BOLD"}},
		{"emoji inside style counts two units", "**😀a**", "😀a", []string{"0:3:BOLD"}},
		{"emoji inside inline code", "x `🎉` y", "x 🎉 y", []string{"2:2:MONOSPACE"}},
		{"style reaching trimmed newlines is clipped", "**hi\n\n**", "hi", []string{"0:2:BOLD"}},
		{"leading newlines are trimmed", "\n\nplain", "plain", nil},
		{"trailing newline keeps earlier style length", "**hi** there\n", "hi there", []string{"0:2:BOLD"}},
		{"leading newline shifts style start", "\n**hi**", "hi", []string{"0:2:BOLD"}},
		{"leading and trailing newlines around styles", "\n\n`a` **b**\n\n", "a b", []string{"0:1:MONOSPACE", "2:1:BOLD"}},
		{"style starting in leading newlines is clipped", "**\n\nhi** x", "hi x", []string{"0:2:BOLD"}},
		{"fence followed by trailing newline", "see\n```\ncode\n```\n", "see\ncode", []string{"4:4:MONOSPACE"}},
		{"emoji with leading and trailing newlines", "\n😀 **hi**\n", "😀 hi", []string{"3:2:BOLD"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatSignalMessage(tt.in)
			assert.Equal(t, tt.text, got.Text)
			assert.Equal(t, tt.styles, got.TextStyles)
		})
	}
}

func TestParseSignalInlineCode(t *testing.T) {
	content, consumed, ok := parseSignalInlineCode("`abc` rest")
	assert.True(t, ok)
	assert.Equal(t, "abc", content)
	assert.Equal(t, 5, consumed)

	_, _, ok = parseSignalInlineCode("abc")
	assert.False(t, ok, "must start with a backtick")
	_, _, ok = parseSignalInlineCode("`abc")
	assert.False(t, ok, "unterminated")
	_, _, ok = parseSignalInlineCode("`a\nb`")
	assert.False(t, ok, "newline inside")
}

func TestParseSignalFence(t *testing.T) {
	content, consumed, ok := parseSignalFence("```sh\n\necho hi\n\n```tail")
	assert.True(t, ok)
	assert.Equal(t, "echo hi", content)
	assert.Equal(t, len("```sh\n\necho hi\n\n```"), consumed)

	_, _, ok = parseSignalFence("``x``")
	assert.False(t, ok, "must start with a fence")
	_, _, ok = parseSignalFence("```never closed")
	assert.False(t, ok, "unterminated")
}

func TestShiftSignalStyles(t *testing.T) {
	assert.Nil(t, shiftSignalStyles(nil, 5))

	in := []signalStyleSpan{{Start: 0, Length: 2, Style: "BOLD"}, {Start: 3, Length: 1, Style: "ITALIC"}}
	got := shiftSignalStyles(in, 4)
	assert.Equal(t, []signalStyleSpan{{Start: 4, Length: 2, Style: "BOLD"}, {Start: 7, Length: 1, Style: "ITALIC"}}, got)
	assert.Equal(t, 0, in[0].Start, "input must not be mutated")
}

func TestClipSignalStyles(t *testing.T) {
	assert.Nil(t, clipSignalStyles(nil, 1, 5))

	in := []signalStyleSpan{
		{Start: 0, Length: 5, Style: "BOLD"},          // overlaps the leading trim
		{Start: 1, Length: 0, Style: "ITALIC"},        // empty
		{Start: 3, Length: 2, Style: "MONOSPACE"},     // interior
		{Start: 5, Length: 4, Style: "SPOILER"},       // overlaps the trailing trim
		{Start: 0, Length: 2, Style: "STRIKETHROUGH"}, // only covers trimmed text
		{Start: 8, Length: 1, Style: "ITALIC"},        // beyond the trimmed text
	}
	got := clipSignalStyles(in, 2, 5)
	assert.Equal(t, []signalStyleSpan{
		{Start: 0, Length: 3, Style: "BOLD"},
		{Start: 1, Length: 2, Style: "MONOSPACE"},
		{Start: 3, Length: 2, Style: "SPOILER"},
	}, got)
}

func TestSignalCanOpenEmphasis(t *testing.T) {
	tests := []struct {
		s    string
		idx  int
		want bool
	}{
		{"*a", 0, true},
		{" *a", 1, true},
		{"x*a", 1, false},
		{"* a", 0, false},
		{"a*", 1, false},
		{"\"*a", 1, true},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, signalCanOpenEmphasis(tt.s, tt.idx, 1), "%q at %d", tt.s, tt.idx)
	}
}
