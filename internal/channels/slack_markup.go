package channels

import (
	"regexp"
	"strings"
)

var (
	slackHeadingRE    = regexp.MustCompile(`^[ \t]*#{1,6}[ \t]+(.*?)(?:[ \t]+#+)?[ \t]*$`)
	slackBulletRE     = regexp.MustCompile(`^([ \t]*)[-*+][ \t]+(.*)$`)
	slackImageRE      = regexp.MustCompile(`!\[([^\]\n]*)\]\(((?:https?|mailto):[^\s)]+)\)`)
	slackLinkRE       = regexp.MustCompile(`\[([^\]\n]+)\]\(((?:https?|mailto):[^\s)]+)\)`)
	slackBoldItalicRE = regexp.MustCompile(`\*\*\*([^*\n]+?)\*\*\*`)
	slackBoldStarRE   = regexp.MustCompile(`\*\*([^*\n]+?)\*\*`)
	slackBoldUnderRE  = regexp.MustCompile(`(^|[^\w])__([^_\s](?:[^_\n]*?[^_\s])?)__($|[^\w])`)
	slackStrikeRE     = regexp.MustCompile(`~~([^~\n]+?)~~`)
)

// slackBullets are the bullet glyphs used for each nesting level of a list.
var slackBullets = []string{"•", "◦", "▪"}

// slackMrkdwn converts common Markdown into Slack mrkdwn. Fenced code
// blocks and inline code spans pass through unchanged. Text the model already
// wrote in mrkdwn (*bold*, _italic_, ~strike~, <url|text>) is left alone, so the
// conversion is safe to apply to every outgoing Slack message.
func slackMrkdwn(text string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	lines := strings.Split(text, "\n")
	fence := ""
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fence = trimmed[:3]
			continue
		}
		lines[i] = formatSlackLine(line)
	}
	return strings.Join(lines, "\n")
}

func formatSlackLine(line string) string {
	if m := slackHeadingRE.FindStringSubmatch(line); m != nil {
		content := formatSlackInline(m[1])
		if content == "" {
			return ""
		}
		if len(content) > 2 && strings.HasPrefix(content, "*") && strings.HasSuffix(content, "*") {
			content = content[1 : len(content)-1]
		}
		return "*" + content + "*"
	}
	if m := slackBulletRE.FindStringSubmatch(line); m != nil {
		width := 0
		for _, r := range m[1] {
			if r == '\t' {
				width += 4
			} else {
				width++
			}
		}
		level := width / 2
		bullet := slackBullets[level%len(slackBullets)]
		return strings.Repeat("    ", level) + bullet + " " + formatSlackInline(m[2])
	}
	return formatSlackInline(line)
}

// formatSlackInline converts inline Markdown outside of backtick code spans.
func formatSlackInline(s string) string {
	var out strings.Builder
	for s != "" {
		start := strings.IndexByte(s, '`')
		if start < 0 {
			out.WriteString(convertSlackInline(s))
			break
		}
		ticks := 1
		for start+ticks < len(s) && s[start+ticks] == '`' {
			ticks++
		}
		delim := s[start : start+ticks]
		end := strings.Index(s[start+ticks:], delim)
		if end < 0 {
			out.WriteString(convertSlackInline(s))
			break
		}
		end += start + ticks + ticks
		out.WriteString(convertSlackInline(s[:start]))
		out.WriteString(s[start:end])
		s = s[end:]
	}
	return out.String()
}

func convertSlackInline(s string) string {
	if s == "" {
		return s
	}
	s = slackImageRE.ReplaceAllString(s, "<$2|$1>")
	s = slackLinkRE.ReplaceAllStringFunc(s, func(m string) string {
		sub := slackLinkRE.FindStringSubmatch(m)
		label := strings.ReplaceAll(sub[1], "|", "¦")
		if label == sub[2] {
			return "<" + sub[2] + ">"
		}
		return "<" + sub[2] + "|" + label + ">"
	})
	s = slackBoldItalicRE.ReplaceAllString(s, "*_${1}_*")
	s = slackBoldStarRE.ReplaceAllString(s, "*$1*")
	s = slackBoldUnderRE.ReplaceAllString(s, "$1*$2*$3")
	s = slackStrikeRE.ReplaceAllString(s, "~$1~")
	return s
}
