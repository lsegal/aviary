package channels

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/store"
)

// maxInlineIncomingMediaBytes caps images sent to the model inline. Larger
// images are still saved and given to the agent as files.
const maxInlineIncomingMediaBytes = 8 << 20

// maxSavedIncomingMediaBytes caps any attachment saved to disk.
const maxSavedIncomingMediaBytes = 50 << 20

func looksLikeImage(contentType, name string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if contentType != "" && contentType != "application/octet-stream" {
		return strings.HasPrefix(contentType, "image/")
	}
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(name))) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return true
	default:
		return false
	}
}

// ingestRemoteMedia downloads an attachment and saves it to disk.
func ingestRemoteMedia(ctx context.Context, channelType, sourceURL, fileName, contentType string, headers map[string]string) (Attachment, error) {
	if strings.TrimSpace(sourceURL) == "" {
		return Attachment{}, fmt.Errorf("media URL is required")
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return Attachment{}, err
	}
	req.Header.Set("User-Agent", "Aviary/1.0")
	for key, value := range headers {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			req.Header.Set(key, value)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Attachment{}, err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Attachment{}, fmt.Errorf("download failed: %s", resp.Status)
	}
	if strings.TrimSpace(contentType) == "" {
		contentType = resp.Header.Get("Content-Type")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSavedIncomingMediaBytes+1))
	if err != nil {
		return Attachment{}, err
	}
	return persistIncomingMedia(channelType, fileName, contentType, data)
}

// ingestLocalMedia copies an attachment from a local path, such as one a
// channel daemon already downloaded.
func ingestLocalMedia(channelType, sourcePath, fileName, contentType string) (Attachment, error) {
	if strings.TrimSpace(sourcePath) == "" {
		return Attachment{}, fmt.Errorf("source path is required")
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		return Attachment{}, err
	}
	if info.Size() > maxSavedIncomingMediaBytes {
		return Attachment{}, fmt.Errorf("attachment exceeds %s", formatBytes(maxSavedIncomingMediaBytes))
	}
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return Attachment{}, err
	}
	return persistIncomingMedia(channelType, mediaName(fileName, filepath.Base(sourcePath)), contentType, data)
}

// persistIncomingMedia saves data to the channel's incoming media directory.
// Images small enough to send inline also get a data URL.
func persistIncomingMedia(channelType, fileName, contentType string, data []byte) (Attachment, error) {
	if len(data) == 0 {
		return Attachment{}, fmt.Errorf("attachment is empty")
	}
	if len(data) > maxSavedIncomingMediaBytes {
		return Attachment{}, fmt.Errorf("attachment exceeds %s", formatBytes(maxSavedIncomingMediaBytes))
	}
	contentType = normalizeContentType(contentType)
	if contentType == "" || contentType == "application/octet-stream" {
		if byExt := llm.ContentTypeFromName(fileName); byExt != "" {
			contentType = byExt
		} else {
			contentType = normalizeContentType(http.DetectContentType(data))
		}
	}
	fileName, contentType = detectAudioType(fileName, contentType, data)
	dir := store.IncomingMediaDir(channelType)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Attachment{}, fmt.Errorf("creating incoming media directory: %w", err)
	}
	name := sanitizeMediaName(fileName, contentType)
	path := filepath.Join(dir, fmt.Sprintf("%d_%s", time.Now().UnixNano(), name))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return Attachment{}, fmt.Errorf("writing incoming media: %w", err)
	}
	att := Attachment{Name: name, ContentType: contentType, Size: int64(len(data)), Path: path}
	if looksLikeImage(contentType, name) && len(data) <= maxInlineIncomingMediaBytes {
		att.DataURL = "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data)
	}
	return att, nil
}

func normalizeContentType(contentType string) string {
	contentType, _, _ = strings.Cut(contentType, ";")
	return strings.ToLower(strings.TrimSpace(contentType))
}

// mediaName returns the first non-empty name, or "attachment".
func mediaName(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "attachment"
}

func sanitizeMediaName(fileName, contentType string) string {
	name := strings.TrimSpace(fileName)
	if name == "" {
		name = "attachment"
		if ext := extensionForContentType(contentType); ext != "" {
			name += ext
		}
	}
	name = filepath.Base(name)
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	name = replacer.Replace(name)
	if strings.TrimSpace(name) == "" || name == "." {
		return "attachment"
	}
	return name
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
