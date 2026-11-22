package llm

import (
	"encoding/base64"
	"fmt"
	"mime"
	"net/url"
	"path/filepath"
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
	if !IsTextMediaType(strings.ToLower(strings.TrimSpace(params[0]))) {
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

// IsTextMediaType reports whether a MIME type holds readable text.
func IsTextMediaType(mediaType string) bool {
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

// FileURL returns a file:// URL for a local path. Channel attachments that
// are not images are passed to the agent runner this way.
func FileURL(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

// ParseFileURL returns the local path of a file:// URL.
func ParseFileURL(mediaURL string) (string, bool) {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(mediaURL)), "file://") {
		return "", false
	}
	u, err := url.Parse(strings.TrimSpace(mediaURL))
	if err != nil || u.Path == "" {
		return "", false
	}
	p := u.Path
	// Windows paths look like /C:/dir/file.
	if len(p) > 2 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return filepath.FromSlash(p), true
}

// ParseDocumentDataURL extracts the payload of a PDF data URL, which
// providers that read documents natively accept as a document.
func ParseDocumentDataURL(mediaURL string) (mimeType string, data string, ok bool) {
	s := strings.TrimSpace(mediaURL)
	if !strings.HasPrefix(strings.ToLower(s), "data:application/pdf") {
		return "", "", false
	}
	head, payload, found := strings.Cut(s, ",")
	if !found || payload == "" || !strings.Contains(strings.ToLower(head), ";base64") {
		return "", "", false
	}
	return "application/pdf", payload, true
}

// DataURLName returns the percent-decoded name parameter of a data URL such
// as "data:application/pdf;name=report.pdf;base64,...", or "" when it has none.
func DataURLName(mediaURL string) string {
	s := strings.TrimSpace(mediaURL)
	if !strings.HasPrefix(strings.ToLower(s), "data:") {
		return ""
	}
	head, _, _ := strings.Cut(s[len("data:"):], ",")
	for _, p := range strings.Split(head, ";")[1:] {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(strings.ToLower(p), "name=") {
			if n, err := url.PathUnescape(p[len("name="):]); err == nil {
				return n
			}
		}
	}
	return ""
}

// ContentTypeFromName guesses a MIME type from a file name's extension. It
// returns "" when the extension is unknown.
func ContentTypeFromName(name string) string {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(name)))
	switch ext {
	case "":
		return ""
	case ".log", ".txt":
		return "text/plain"
	case ".yaml", ".yml":
		return "application/yaml"
	case ".md":
		return "text/markdown"
	case ".csv":
		return "text/csv"
	case ".json":
		return "application/json"
	case ".pdf":
		return "application/pdf"
	}
	mediaType, _, _ := strings.Cut(mime.TypeByExtension(ext), ";")
	return strings.ToLower(strings.TrimSpace(mediaType))
}

// isImageMediaURL reports whether mediaURL is an image data URL or a remote
// URL, which providers that take only images as media accept.
func isImageMediaURL(mediaURL string) bool {
	if _, _, ok := ParseImageDataURL(mediaURL); ok {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(mediaURL))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}
