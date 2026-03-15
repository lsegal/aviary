package channels

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/store"
)

// StageOutgoingMedia copies a generated file into the per-channel outgoing
// media staging directory so channel daemons can access a stable path.
func StageOutgoingMedia(channelType, sourcePath string) (string, error) {
	if strings.TrimSpace(sourcePath) == "" {
		return "", fmt.Errorf("source path is required")
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("source path is a directory")
	}
	dir := store.OutgoingMediaDir(channelType)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	target := filepath.Join(dir, fmt.Sprintf("%d_%s", time.Now().UnixMilli(), filepath.Base(sourcePath)))
	src, err := os.Open(sourcePath)
	if err != nil {
		return "", err
	}
	defer src.Close() //nolint:errcheck
	dst, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	defer dst.Close() //nolint:errcheck
	if _, err := io.Copy(dst, src); err != nil {
		return "", err
	}
	return target, nil
}
