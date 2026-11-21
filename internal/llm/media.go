package llm

import (
	"encoding/base64"
	"fmt"
	"net/url"
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

// InlineTextAttachments moves text file data URLs out of mediaURLs and into
// the message as <attachment> blocks, since models only take images as media.
// It returns the message and the remaining media URLs.
func InlineTextAttachments(message string, mediaURLs []string) (string, []string) {
	var media []string
	var b strings.Builder
	b.WriteString(message)
	for _, mediaURL := range mediaURLs {
		name, text, ok := ParseTextDataURL(mediaURL)
		if !ok {
			media = append(media, mediaURL)
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		if name == "" {
			b.WriteString("<attachment>\n")
		} else {
			fmt.Fprintf(&b, "<attachment name=%q>\n", name)
		}
		b.WriteString(text)
		if !strings.HasSuffix(text, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("</attachment>")
	}
	return b.String(), media
}

// ParseTextDataURL decodes a text data URL such as
// "data:text/plain;name=notes.txt;base64,...". The name parameter is
// optional and percent-encoded.
func ParseTextDataURL(mediaURL string) (name string, text string, ok bool) {
	s := strings.TrimSpace(mediaURL)
	if !strings.HasPrefix(strings.ToLower(s), "data:") {
		return "", "", false
	}
	comma := strings.IndexByte(s, ',')
	if comma < 0 {
		return "", "", false
	}
	params := strings.Split(s[len("data:"):comma], ";")
	if !isTextMediaType(strings.ToLower(strings.TrimSpace(params[0]))) {
		return "", "", false
	}
	encoded := false
	for _, p := range params[1:] {
		p = strings.TrimSpace(p)
		switch {
		case strings.EqualFold(p, "base64"):
			encoded = true
		case strings.HasPrefix(strings.ToLower(p), "name="):
			if n, err := url.PathUnescape(p[len("name="):]); err == nil {
				name = n
			}
		}
	}
	payload := s[comma+1:]
	if encoded {
		data, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return "", "", false
		}
		return name, string(data), true
	}
	data, err := url.PathUnescape(payload)
	if err != nil {
		return "", "", false
	}
	return name, data, true
}

func isTextMediaType(mediaType string) bool {
	if strings.HasPrefix(mediaType, "text/") {
		return true
	}
	switch mediaType {
	case "application/json", "application/xml", "application/javascript",
		"application/x-yaml", "application/yaml", "application/toml",
		"application/x-sh", "application/sql":
		return true
	}
	return strings.HasSuffix(mediaType, "+json") || strings.HasSuffix(mediaType, "+xml")
}
