package channels

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	signalFencedCodeRE = regexp.MustCompile("(?s)```[A-Za-z0-9_+-]*\\n?(.*?)```")
	signalLinkRE       = regexp.MustCompile(`\[(.+?)\]\((https?://[^\s)]+)\)`)
	signalBoldStarRE   = regexp.MustCompile(`(?s)\*\*(.+?)\*\*`)
	signalBoldUndRE    = regexp.MustCompile(`(?s)__(.+?)__`)
	signalStrikeRE     = regexp.MustCompile(`(?s)~~(.+?)~~`)
)

// formatSignalMarkup converts common Markdown patterns into the lightweight
// formatting syntax Signal recognizes.
func formatSignalMarkup(text string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}

	text, restore := stashSignalInlineCode(text)
	text = signalFencedCodeRE.ReplaceAllStringFunc(text, func(block string) string {
		m := signalFencedCodeRE.FindStringSubmatch(block)
		if len(m) < 2 {
			return block
		}
		return stashSignalToken("code", strings.Trim(m[1], "\n"))
	})
	text = signalLinkRE.ReplaceAllString(text, "$1 ($2)")
	text = signalBoldStarRE.ReplaceAllStringFunc(text, func(match string) string {
		m := signalBoldStarRE.FindStringSubmatch(match)
		if len(m) < 2 {
			return match
		}
		return stashSignalToken("bold", m[1])
	})
	text = signalBoldUndRE.ReplaceAllStringFunc(text, func(match string) string {
		m := signalBoldUndRE.FindStringSubmatch(match)
		if len(m) < 2 {
			return match
		}
		return stashSignalToken("bold", m[1])
	})
	text = signalStrikeRE.ReplaceAllString(text, "~$1~")
	text = convertSignalStarItalics(text)

	lines := strings.Split(text, "\n")
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "#") {
			trimmed = strings.TrimLeft(trimmed, "#")
			lines[i] = strings.TrimSpace(trimmed)
		}
	}
	return restoreSignalTokens(restore(strings.Join(lines, "\n")))
}

func convertSignalStarItalics(text string) string {
	var out strings.Builder
	for i := 0; i < len(text); i++ {
		if text[i] != '*' {
			out.WriteByte(text[i])
			continue
		}
		if i > 0 && !isSignalMarkupBoundary(text[i-1]) {
			out.WriteByte(text[i])
			continue
		}

		end := strings.IndexByte(text[i+1:], '*')
		if end < 0 {
			out.WriteByte(text[i])
			continue
		}
		end += i + 1
		content := text[i+1 : end]
		if strings.TrimSpace(content) == "" || strings.HasPrefix(content, " ") || strings.HasSuffix(content, " ") {
			out.WriteByte(text[i])
			continue
		}
		if end+1 < len(text) && !isSignalMarkupBoundary(text[end+1]) {
			out.WriteByte(text[i])
			continue
		}

		out.WriteByte('_')
		out.WriteString(content)
		out.WriteByte('_')
		i = end
	}
	return out.String()
}

func isSignalMarkupBoundary(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '(', '[', '{', '"', '\'', '.', ',', ';', ':', '!', '?', '-':
		return true
	default:
		return false
	}
}

func stashSignalInlineCode(text string) (string, func(string) string) {
	parts := map[string]string{}
	var idx int
	out := regexp.MustCompile("`[^`\n]+`").ReplaceAllStringFunc(text, func(match string) string {
		key := "\x00CODE" + strconv.Itoa(idx) + "\x00"
		parts[key] = match
		idx++
		return key
	})
	return out, func(s string) string {
		for key, value := range parts {
			s = strings.ReplaceAll(s, key, value)
		}
		return s
	}
}

func stashSignalToken(kind, content string) string {
	return "\x00" + strings.ToUpper(kind) + ":" + content + "\x00"
}

func restoreSignalTokens(text string) string {
	for {
		start := strings.IndexByte(text, '\x00')
		if start < 0 {
			return text
		}
		end := strings.IndexByte(text[start+1:], '\x00')
		if end < 0 {
			return text
		}
		end += start + 1
		token := text[start+1 : end]
		replacement := token
		if strings.HasPrefix(token, "BOLD:") {
			replacement = "*" + strings.TrimPrefix(token, "BOLD:") + "*"
		} else if strings.HasPrefix(token, "CODE:") {
			replacement = "`" + strings.TrimPrefix(token, "CODE:") + "`"
		}
		text = text[:start] + replacement + text[end+1:]
	}
}
