package agent

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/store"
)

// maxInlineAttachmentTextBytes caps how much of a text attachment the model
// sees inline. The rest stays in the saved file.
const maxInlineAttachmentTextBytes = 32 << 10

// maxDocumentAttachmentBytes caps PDFs sent to the model as documents.
const maxDocumentAttachmentBytes = 32 << 20

// resolveFileAttachments turns file:// attachments into model input. Text
// files are inlined as <attachment> blocks, truncated past a limit. PDFs
// become document data URLs, except for CLI agents, which take only images.
// Other files are dropped: the message already names their saved path, so
// the agent can open them with its file tools. Only files under Aviary's
// media directory are read.
func resolveFileAttachments(message string, mediaURLs []string, isCLI bool) (string, []string) {
	var media []string
	var b strings.Builder
	b.WriteString(message)
	for _, mediaURL := range mediaURLs {
		path, ok := llm.ParseFileURL(mediaURL)
		if !ok {
			media = append(media, mediaURL)
			continue
		}
		if !insideMediaDir(path) {
			continue
		}
		contentType := attachmentContentType(path)
		switch {
		case llm.IsTextMediaType(contentType):
			block, err := inlineTextAttachment(path)
			if err != nil {
				continue
			}
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(block)
		case contentType == "application/pdf" && !isCLI:
			info, err := os.Stat(path)
			if err != nil || info.Size() > maxDocumentAttachmentBytes {
				continue
			}
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			media = append(media, "data:application/pdf;name="+url.PathEscape(attachmentDisplayName(path))+";base64,"+base64.StdEncoding.EncodeToString(data))
		}
	}
	return b.String(), media
}

func insideMediaDir(path string) bool {
	rel, err := filepath.Rel(filepath.Clean(store.MediaDir()), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// attachmentDisplayName strips the timestamp prefix saved attachments carry.
func attachmentDisplayName(path string) string {
	name := filepath.Base(path)
	if prefix, rest, ok := strings.Cut(name, "_"); ok && rest != "" && strings.Trim(prefix, "0123456789") == "" {
		return rest
	}
	return name
}

func attachmentContentType(path string) string {
	if contentType := llm.ContentTypeFromName(path); contentType != "" {
		return contentType
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close() //nolint:errcheck
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	contentType, _, _ := strings.Cut(http.DetectContentType(head[:n]), ";")
	return contentType
}

func inlineTextAttachment(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close() //nolint:errcheck
	data, err := io.ReadAll(io.LimitReader(f, maxInlineAttachmentTextBytes+1))
	if err != nil {
		return "", err
	}
	truncated := len(data) > maxInlineAttachmentTextBytes
	if truncated {
		data = data[:maxInlineAttachmentTextBytes]
		// Don't split a multi-byte character.
		for i := 0; i < utf8.UTFMax-1 && !utf8.Valid(data); i++ {
			data = data[:len(data)-1]
		}
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("not UTF-8 text")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "<attachment name=%q path=\"%s\">\n", attachmentDisplayName(path), path)
	b.Write(data)
	if len(data) > 0 && data[len(data)-1] != '\n' {
		b.WriteString("\n")
	}
	if truncated {
		fmt.Fprintf(&b, "[truncated after %d KB; read the full file at %s]\n", maxInlineAttachmentTextBytes>>10, path)
	}
	b.WriteString("</attachment>")
	return b.String(), nil
}
