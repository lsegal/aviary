package agent

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/store"
)

type turnMediaKey struct{}

// WithTurnMedia stores the media attached to the user message that started
// the current turn, so tools can act on it, such as attaching a screenshot to
// a filed issue.
func WithTurnMedia(ctx context.Context, mediaURLs []string) context.Context {
	if len(mediaURLs) == 0 {
		return ctx
	}
	return context.WithValue(ctx, turnMediaKey{}, append([]string(nil), mediaURLs...))
}

// TurnMediaFromContext returns the current turn's media URLs.
func TurnMediaFromContext(ctx context.Context) []string {
	v, _ := ctx.Value(turnMediaKey{}).([]string)
	return v
}

// TurnMediaDir is where data URLs from a turn are saved as files for tools
// that need a local path.
func TurnMediaDir() string {
	return filepath.Join(store.MediaDir(), "turn")
}

// MediaFile returns a local file holding mediaURL: the path of a file:// URL
// or a plain path, or a file under TurnMediaDir that a data URL is saved to.
// Remote URLs are refused, since nothing local holds them.
func MediaFile(mediaURL string) (string, error) {
	s := strings.TrimSpace(mediaURL)
	if path, ok := llm.ParseFileURL(s); ok {
		return path, nil
	}
	lower := strings.ToLower(s)
	switch {
	case strings.HasPrefix(lower, "data:"):
		return saveDataURL(s)
	case strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "https://"):
		return "", fmt.Errorf("%s is a remote URL, not a local file", truncateMediaURL(s))
	case s == "":
		return "", fmt.Errorf("empty attachment")
	}
	return s, nil
}

// saveDataURL writes a base64 data URL to TurnMediaDir, named by its content
// so saving the same media twice reuses one file.
func saveDataURL(dataURL string) (string, error) {
	head, payload, ok := strings.Cut(dataURL[len("data:"):], ",")
	params := strings.Split(head, ";")
	if !ok || params[len(params)-1] != "base64" {
		return "", fmt.Errorf("%s is not a base64 data URL", truncateMediaURL(dataURL))
	}
	data, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(payload), ""))
	if err != nil {
		return "", fmt.Errorf("%s: %w", truncateMediaURL(dataURL), err)
	}
	contentType := strings.ToLower(strings.TrimSpace(params[0]))
	sum := sha256.Sum256(data)
	name := llm.DataURLName(dataURL)
	if name == "" {
		name = "attachment" + mediaExtension(contentType)
	}
	dir := TurnMediaDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, hex.EncodeToString(sum[:8])+"_"+filepath.Base(name))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// mediaExtension is the usual file extension for contentType.
func mediaExtension(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "video/quicktime":
		return ".mov"
	}
	if exts, _ := mime.ExtensionsByType(contentType); len(exts) > 0 {
		return exts[0]
	}
	return ""
}

func truncateMediaURL(s string) string {
	if len(s) > 40 {
		return s[:40] + "..."
	}
	return s
}
