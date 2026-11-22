package transcribe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"github.com/lsegal/aviary/internal/store"
)

// CacheDir is where transcripts are kept, keyed by audio content.
func CacheDir() string {
	return store.SubDir("transcripts")
}

// Cache wraps an Engine and stores each transcript on disk, keyed by the
// audio's content, model, and language. A redelivered or reloaded voice
// message is read back instead of transcribed again.
type Cache struct {
	Engine Engine
	Dir    string
	Model  string
}

// NewCache returns engine with transcripts cached in dir.
func NewCache(engine Engine, dir, model string) *Cache {
	return &Cache{Engine: engine, Dir: dir, Model: model}
}

// Transcribe implements Engine.
func (c *Cache) Transcribe(ctx context.Context, audioPath string, opts Options) (Transcript, error) {
	key, err := c.key(audioPath, opts)
	if err != nil {
		return Transcript{}, err
	}
	path := filepath.Join(c.Dir, key+".json")
	if data, err := os.ReadFile(path); err == nil {
		var t Transcript
		if json.Unmarshal(data, &t) == nil {
			return t, nil
		}
	}
	t, err := c.Engine.Transcribe(ctx, audioPath, opts)
	if err != nil {
		return t, err
	}
	if data, err := json.Marshal(t); err == nil && os.MkdirAll(c.Dir, 0o700) == nil {
		_ = os.WriteFile(path, data, 0o600)
	}
	return t, nil
}

func (c *Cache) key(audioPath string, opts Options) (string, error) {
	f, err := os.Open(audioPath)
	if err != nil {
		return "", err
	}
	defer f.Close() //nolint:errcheck
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	_, _ = io.WriteString(h, "\x00"+c.Model+"\x00"+opts.Language)
	return hex.EncodeToString(h.Sum(nil)), nil
}
