package channels

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

var (
	signalHeadingRE = regexp.MustCompile(`(?m)^[ \t]*#{1,6}[ \t]+`)
	signalLinkRE    = regexp.MustCompile(`\[(.+?)\]\((https?://[^\s)]+)\)`)
	signalUTagRE    = regexp.MustCompile(`(?i)</?u>`)
)

type signalFormattedText struct {
	Text       string
	TextStyles []string
}

type signalStyleSpan struct {
	Start  int
	Length int
	Style  string
}

// formatSignalMessage converts common Markdown into plain text plus Signal's
// native textStyle ranges. Signal does not support underline, so underline tags
// are stripped while preserving their text content.
func formatSignalMessage(text string) signalFormattedText {
	if strings.TrimSpace(text) == "" {
		return signalFormattedText{Text: text}
	}

	text = signalHeadingRE.ReplaceAllString(text, "")
	text = signalLinkRE.ReplaceAllString(text, "$1 ($2)")
	text = signalUTagRE.ReplaceAllString(text, "")

	parsed, _, _ := parseSignalMarkdown(text, "")
	trimmed := strings.Trim(parsed.text, "\n")
	lead := utf16Len(parsed.text) - utf16Len(strings.TrimLeft(parsed.text, "\n"))
	out := signalFormattedText{Text: trimmed}
	for _, span := range clipSignalStyles(parsed.styles, lead, utf16Len(trimmed)) {
		out.TextStyles = append(out.TextStyles, fmt.Sprintf("%d:%d:%s", span.Start, span.Length, span.Style))
	}
	return out
}

type signalParseResult struct {
	text   string
	styles []signalStyleSpan
}

// signalPairedMarkers are the symmetric two-character Markdown delimiters and
// the Signal style each one maps to.
var signalPairedMarkers = []struct{ marker, style string }{
	{"**", "BOLD"},
	{"__", "BOLD"},
	{"~~", "STRIKETHROUGH"},
	{"||", "SPOILER"},
}

func parseSignalMarkdown(input, stop string) (signalParseResult, int, bool) {
	var p signalMarkdownParser
	i := 0
	for i < len(input) {
		if stop != "" && strings.HasPrefix(input[i:], stop) {
			return p.result(), i + len(stop), true
		}
		i = p.step(input, i)
	}
	return p.result(), i, stop == ""
}

// signalMarkdownParser accumulates plain text and style spans for one nesting
// level of parseSignalMarkdown.
type signalMarkdownParser struct {
	out    strings.Builder
	styles []signalStyleSpan
}

func (p *signalMarkdownParser) result() signalParseResult {
	return signalParseResult{text: p.out.String(), styles: p.styles}
}

// step consumes the token at input[i] and returns the index after it.
func (p *signalMarkdownParser) step(input string, i int) int {
	rest := input[i:]
	if strings.HasPrefix(rest, "```") {
		return i + p.code(rest, "```", parseSignalFence)
	}
	for _, m := range signalPairedMarkers {
		if strings.HasPrefix(rest, m.marker) {
			return i + p.delimited(input[i+len(m.marker):], m.marker, m.style)
		}
	}
	switch input[i] {
	case '`':
		return i + p.code(rest, "`", parseSignalInlineCode)
	case '*', '_':
		marker := input[i : i+1]
		if !signalCanOpenEmphasis(input, i, 1) {
			p.out.WriteString(marker)
			return i + 1
		}
		return i + p.delimited(input[i+1:], marker, "ITALIC")
	}
	_, size := utf8.DecodeRuneInString(rest)
	p.out.WriteString(rest[:size])
	return i + size
}

// delimited parses nested Markdown after an opening marker up to its matching
// close and emits it with style. Unmatched markers are kept as literal text.
// It returns the bytes consumed, including the opening marker.
func (p *signalMarkdownParser) delimited(after, marker, style string) int {
	inner, consumed, ok := parseSignalMarkdown(after, marker)
	if !ok {
		p.out.WriteString(marker)
		return len(marker)
	}
	p.emit(inner.text, inner.styles, style)
	return len(marker) + consumed
}

// code emits a monospace span parsed by parse, or the literal marker when the
// span is not closed. It returns the bytes consumed.
func (p *signalMarkdownParser) code(rest, marker string, parse func(string) (string, int, bool)) int {
	content, consumed, ok := parse(rest)
	if !ok {
		p.out.WriteString(marker)
		return len(marker)
	}
	p.emit(content, nil, "MONOSPACE")
	return consumed
}

// emit appends text, its inner style spans (shifted to the current offset)
// and a span of style covering all of text when it is non-empty.
func (p *signalMarkdownParser) emit(text string, inner []signalStyleSpan, style string) {
	start := utf16Len(p.out.String())
	p.out.WriteString(text)
	p.styles = append(p.styles, shiftSignalStyles(inner, start)...)
	if l := utf16Len(text); l > 0 {
		p.styles = append(p.styles, signalStyleSpan{Start: start, Length: l, Style: style})
	}
}

func parseSignalInlineCode(input string) (string, int, bool) {
	if !strings.HasPrefix(input, "`") {
		return "", 0, false
	}
	end := strings.IndexByte(input[1:], '`')
	if end < 0 {
		return "", 0, false
	}
	end++
	if strings.Contains(input[1:end], "\n") {
		return "", 0, false
	}
	return input[1:end], end + 1, true
}

func parseSignalFence(input string) (string, int, bool) {
	if !strings.HasPrefix(input, "```") {
		return "", 0, false
	}
	closeIdx := strings.Index(input[3:], "```")
	if closeIdx < 0 {
		return "", 0, false
	}
	closeIdx += 3
	body := input[3:closeIdx]
	if nl := strings.IndexByte(body, '\n'); nl >= 0 {
		body = body[nl+1:]
	}
	return strings.Trim(body, "\n"), closeIdx + 3, true
}

func shiftSignalStyles(styles []signalStyleSpan, offset int) []signalStyleSpan {
	if len(styles) == 0 {
		return nil
	}
	out := make([]signalStyleSpan, 0, len(styles))
	for _, style := range styles {
		style.Start += offset
		out = append(out, style)
	}
	return out
}

// clipSignalStyles maps style spans from untrimmed text onto text that had
// lead UTF-16 units removed from its start and was then cut to length units.
// Spans are shifted left by lead, clipped to [0, length], and dropped when
// nothing remains.
func clipSignalStyles(styles []signalStyleSpan, lead, length int) []signalStyleSpan {
	var out []signalStyleSpan
	for _, span := range styles {
		if span.Length <= 0 {
			continue
		}
		start := max(span.Start-lead, 0)
		end := min(span.Start+span.Length-lead, length)
		if end <= start {
			continue
		}
		out = append(out, signalStyleSpan{Start: start, Length: end - start, Style: span.Style})
	}
	return out
}

func signalCanOpenEmphasis(s string, idx, markerLen int) bool {
	if idx+markerLen >= len(s) {
		return false
	}
	prevBoundary := idx == 0 || isSignalMarkupBoundary(s[idx-1])
	nextBoundary := isSignalMarkupBoundary(s[idx+markerLen])
	return prevBoundary && !nextBoundary
}

func isSignalMarkupBoundary(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '(', '[', '{', '"', '\'', '.', ',', ';', ':', '!', '?', '-', ')', ']', '}':
		return true
	default:
		return false
	}
}

func utf16Len(s string) int {
	return len(utf16.Encode([]rune(s)))
}
