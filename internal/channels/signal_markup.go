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

func formatSignalMarkup(text string) string {
	return formatSignalMessage(text).Text
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
	out := signalFormattedText{
		Text: strings.Trim(parsed.text, "\n"),
	}
	for _, span := range parsed.styles {
		start := span.Start
		end := span.Start + span.Length
		if start < 0 || end > utf16Len(parsed.text) || span.Length <= 0 {
			continue
		}
		out.TextStyles = append(out.TextStyles, fmt.Sprintf("%d:%d:%s", start, span.Length, span.Style))
	}
	if delta := utf16Len(parsed.text) - utf16Len(out.Text); delta > 0 {
		out.TextStyles = trimSignalStyles(out.TextStyles, delta)
	}
	return out
}

type signalParseResult struct {
	text   string
	styles []signalStyleSpan
}

func parseSignalMarkdown(input, stop string) (signalParseResult, int, bool) {
	var out strings.Builder
	var styles []signalStyleSpan
	i := 0

	appendText := func(s string) {
		out.WriteString(s)
	}

	for i < len(input) {
		if stop != "" && strings.HasPrefix(input[i:], stop) {
			return signalParseResult{text: out.String(), styles: styles}, i + len(stop), true
		}

		switch {
		case strings.HasPrefix(input[i:], "```"):
			content, consumed, ok := parseSignalFence(input[i:])
			if !ok {
				appendText("```")
				i += 3
				continue
			}
			start := utf16Len(out.String())
			appendText(content)
			length := utf16Len(content)
			if length > 0 {
				styles = append(styles, signalStyleSpan{Start: start, Length: length, Style: "MONOSPACE"})
			}
			i += consumed
		case strings.HasPrefix(input[i:], "**"):
			if inner, consumed, ok := parseSignalMarkdown(input[i+2:], "**"); ok {
				start := utf16Len(out.String())
				appendText(inner.text)
				styles = append(styles, shiftSignalStyles(inner.styles, start)...)
				if l := utf16Len(inner.text); l > 0 {
					styles = append(styles, signalStyleSpan{Start: start, Length: l, Style: "BOLD"})
				}
				i += consumed + 2
				continue
			}
			appendText("**")
			i += 2
		case strings.HasPrefix(input[i:], "__"):
			if inner, consumed, ok := parseSignalMarkdown(input[i+2:], "__"); ok {
				start := utf16Len(out.String())
				appendText(inner.text)
				styles = append(styles, shiftSignalStyles(inner.styles, start)...)
				if l := utf16Len(inner.text); l > 0 {
					styles = append(styles, signalStyleSpan{Start: start, Length: l, Style: "BOLD"})
				}
				i += consumed + 2
				continue
			}
			appendText("__")
			i += 2
		case strings.HasPrefix(input[i:], "~~"):
			if inner, consumed, ok := parseSignalMarkdown(input[i+2:], "~~"); ok {
				start := utf16Len(out.String())
				appendText(inner.text)
				styles = append(styles, shiftSignalStyles(inner.styles, start)...)
				if l := utf16Len(inner.text); l > 0 {
					styles = append(styles, signalStyleSpan{Start: start, Length: l, Style: "STRIKETHROUGH"})
				}
				i += consumed + 2
				continue
			}
			appendText("~~")
			i += 2
		case strings.HasPrefix(input[i:], "||"):
			if inner, consumed, ok := parseSignalMarkdown(input[i+2:], "||"); ok {
				start := utf16Len(out.String())
				appendText(inner.text)
				styles = append(styles, shiftSignalStyles(inner.styles, start)...)
				if l := utf16Len(inner.text); l > 0 {
					styles = append(styles, signalStyleSpan{Start: start, Length: l, Style: "SPOILER"})
				}
				i += consumed + 2
				continue
			}
			appendText("||")
			i += 2
		case input[i] == '`':
			content, consumed, ok := parseSignalInlineCode(input[i:])
			if !ok {
				appendText("`")
				i++
				continue
			}
			start := utf16Len(out.String())
			appendText(content)
			if l := utf16Len(content); l > 0 {
				styles = append(styles, signalStyleSpan{Start: start, Length: l, Style: "MONOSPACE"})
			}
			i += consumed
		case input[i] == '*':
			if !signalCanOpenEmphasis(input, i, 1) {
				appendText("*")
				i++
				continue
			}
			if inner, consumed, ok := parseSignalMarkdown(input[i+1:], "*"); ok {
				start := utf16Len(out.String())
				appendText(inner.text)
				styles = append(styles, shiftSignalStyles(inner.styles, start)...)
				if l := utf16Len(inner.text); l > 0 {
					styles = append(styles, signalStyleSpan{Start: start, Length: l, Style: "ITALIC"})
				}
				i += consumed + 1
				continue
			}
			appendText("*")
			i++
		case input[i] == '_':
			if !signalCanOpenEmphasis(input, i, 1) {
				appendText("_")
				i++
				continue
			}
			if inner, consumed, ok := parseSignalMarkdown(input[i+1:], "_"); ok {
				start := utf16Len(out.String())
				appendText(inner.text)
				styles = append(styles, shiftSignalStyles(inner.styles, start)...)
				if l := utf16Len(inner.text); l > 0 {
					styles = append(styles, signalStyleSpan{Start: start, Length: l, Style: "ITALIC"})
				}
				i += consumed + 1
				continue
			}
			appendText("_")
			i++
		default:
			_, size := utf8.DecodeRuneInString(input[i:])
			appendText(input[i : i+size])
			i += size
		}
	}

	return signalParseResult{text: out.String(), styles: styles}, i, stop == ""
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

func trimSignalStyles(styles []string, trimEnd int) []string {
	if trimEnd <= 0 {
		return styles
	}
	var out []string
	for _, style := range styles {
		var start, length int
		var kind string
		if _, err := fmt.Sscanf(style, "%d:%d:%s", &start, &length, &kind); err != nil {
			continue
		}
		end := start + length
		trimmedEnd := end - trimEnd
		if trimmedEnd <= start {
			continue
		}
		out = append(out, fmt.Sprintf("%d:%d:%s", start, trimmedEnd-start, kind))
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
