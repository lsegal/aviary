package llm

import (
	"regexp"
	"strings"
)

var imageDataURLPattern = regexp.MustCompile(`(?is)data:image/[a-z0-9.+-]+;base64,[a-z0-9+/=\r\n]+`)

// ParseImageDataURL extracts mime type and base64 payload from a data URL.
func ParseImageDataURL(mediaURL string) (mimeType string, data string, ok bool) {
	s := strings.TrimSpace(mediaURL)
	if s == "" || !strings.HasPrefix(strings.ToLower(s), "data:image/") {
		return "", "", false
	}
	comma := strings.IndexByte(s, ',')
	if comma <= 5 || comma >= len(s)-1 {
		return "", "", false
	}
	head := strings.TrimSpace(s[:comma])
	payload := strings.Map(func(r rune) rune {
		switch r {
		case '\r', '\n', '\t', ' ':
			return -1
		default:
			return r
		}
	}, s[comma+1:])
	if payload == "" {
		return "", "", false
	}
	semi := strings.IndexByte(head, ';')
	if semi <= 5 {
		return "", "", false
	}
	mime := strings.ToLower(head[5:semi])
	if !strings.Contains(head[semi+1:], "base64") {
		return "", "", false
	}
	return mime, payload, true
}

// ExtractFirstImageDataURL removes the first image data URL from text and returns
// the cleaned text and extracted media URL.
func ExtractFirstImageDataURL(text string) (cleaned string, mediaURL string) {
	loc := imageDataURLPattern.FindStringIndex(text)
	if loc == nil {
		return strings.TrimSpace(text), ""
	}
	url := imageDataURLPattern.FindString(text)
	if _, _, ok := ParseImageDataURL(url); !ok {
		return strings.TrimSpace(text), ""
	}
	without := text[:loc[0]] + text[loc[1]:]
	return strings.TrimSpace(without), strings.TrimSpace(url)
}
