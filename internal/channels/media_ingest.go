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

	"github.com/lsegal/aviary/internal/store"
)

const maxInlineIncomingMediaBytes = 8 << 20

func looksLikeImage(contentType, name string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if strings.HasPrefix(contentType, "image/") {
		return true
	}
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(name))) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return true
	default:
		return false
	}
}

func ingestRemoteMedia(ctx context.Context, channelType, sourceURL, fileName string, headers map[string]string) (string, error) {
	if strings.TrimSpace(sourceURL) == "" {
		return "", fmt.Errorf("media URL is required")
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Aviary/1.0")
	for key, value := range headers {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			req.Header.Set(key, value)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("download failed: %s", resp.Status)
	}
	contentType := resp.Header.Get("Content-Type")
	if !looksLikeImage(contentType, fileName) {
		return "", fmt.Errorf("not an image: %s", contentType)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxInlineIncomingMediaBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", fmt.Errorf("downloaded media is empty")
	}
	if len(data) > maxInlineIncomingMediaBytes {
		return "", fmt.Errorf("downloaded media exceeds %d bytes", maxInlineIncomingMediaBytes)
	}
	return persistIncomingMedia(channelType, fileName, contentType, data)
}

func ingestLocalMedia(channelType, sourcePath, fileName, contentType string) (string, error) {
	if strings.TrimSpace(sourcePath) == "" {
		return "", fmt.Errorf("source path is required")
	}
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", fmt.Errorf("media file is empty")
	}
	if len(data) > maxInlineIncomingMediaBytes {
		return "", fmt.Errorf("media file exceeds %d bytes", maxInlineIncomingMediaBytes)
	}
	if strings.TrimSpace(contentType) == "" {
		contentType = http.DetectContentType(data)
	}
	if !looksLikeImage(contentType, firstNonEmpty(fileName, filepath.Base(sourcePath))) {
		return "", fmt.Errorf("not an image: %s", contentType)
	}
	return persistIncomingMedia(channelType, firstNonEmpty(fileName, filepath.Base(sourcePath)), contentType, data)
}

func persistIncomingMedia(channelType, fileName, contentType string, data []byte) (string, error) {
	dir := store.IncomingMediaDir(channelType)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating incoming media directory: %w", err)
	}
	name := sanitizeMediaName(fileName, contentType)
	path := filepath.Join(dir, fmt.Sprintf("%d_%s", time.Now().UnixMilli(), name))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("writing incoming media: %w", err)
	}
	if strings.TrimSpace(contentType) == "" {
		contentType = http.DetectContentType(data)
	}
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "image"
}

func sanitizeMediaName(fileName, contentType string) string {
	name := strings.TrimSpace(fileName)
	if name == "" {
		name = "image"
		switch {
		case strings.Contains(contentType, "png"):
			name += ".png"
		case strings.Contains(contentType, "jpeg"):
			name += ".jpg"
		case strings.Contains(contentType, "gif"):
			name += ".gif"
		case strings.Contains(contentType, "webp"):
			name += ".webp"
		}
	}
	name = filepath.Base(name)
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	name = replacer.Replace(name)
	if strings.TrimSpace(name) == "" {
		return "image"
	}
	return name
}
