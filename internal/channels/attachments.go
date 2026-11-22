package channels

import (
	"fmt"
	"mime"
	"strings"

	"github.com/lsegal/aviary/internal/llm"
)

// Attachment is a file received with a channel message.
type Attachment struct {
	Name        string // file name as sent, sanitized
	ContentType string // MIME type, from the platform or detected
	Size        int64  // size in bytes
	Path        string // where the file was saved; empty when saving failed
	DataURL     string // inline image data URL for the model; empty for other files
	Err         string // why the file could not be saved, if it could not
}

// Placeholder describes the attachment for the agent, for example
// "[file attached: log.json (application/json, 1.2 MB) saved at /path]".
func (a Attachment) Placeholder() string {
	details := a.ContentType
	if a.Size > 0 {
		details = strings.TrimPrefix(details+", "+formatBytes(a.Size), ", ")
	}
	desc := a.Name
	if details != "" {
		desc += " (" + details + ")"
	}
	if a.Path == "" {
		reason := a.Err
		if reason == "" {
			reason = "unknown error"
		}
		return fmt.Sprintf("[file attached: %s could not be saved: %s]", desc, reason)
	}
	return fmt.Sprintf("[file attached: %s saved at %s]", desc, a.Path)
}

// PromptInput returns the message text and media URLs for an agent prompt.
// Images go to the model as data URLs. Other files are described by a
// placeholder line in the text and passed as file URLs, which the agent
// runner inlines (text files) or sends as documents (PDFs) where it can.
func PromptInput(text string, attachments []Attachment) (string, []string) {
	var media, lines []string
	for _, att := range attachments {
		if att.DataURL != "" {
			media = append(media, att.DataURL)
			continue
		}
		lines = append(lines, att.Placeholder())
		if att.Path != "" {
			media = append(media, llm.FileURL(att.Path))
		}
	}
	if len(lines) == 0 {
		return text, media
	}
	placeholders := strings.Join(lines, "\n")
	if strings.TrimSpace(text) == "" {
		return placeholders, media
	}
	return text + "\n" + placeholders, media
}

// failedAttachment records an attachment that could not be saved, so the
// agent still learns a file was sent.
func failedAttachment(name, contentType string, size int64, err error) Attachment {
	return Attachment{
		Name:        sanitizeMediaName(name, contentType),
		ContentType: normalizeContentType(contentType),
		Size:        size,
		Err:         err.Error(),
	}
}

func extensionForContentType(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "text/plain":
		return ".txt"
	}
	if exts, err := mime.ExtensionsByType(contentType); err == nil && len(exts) > 0 {
		return exts[0]
	}
	return ""
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
