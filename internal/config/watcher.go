package config

import (
	"log/slog"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const debounceDuration = 300 * time.Millisecond

// Watcher watches aviary.yaml for changes and calls registered handlers.
type Watcher struct {
	path     string
	handlers []func(*Config)
	mu       sync.Mutex
	done     chan struct{}
}

// NewWatcher creates a Watcher for path.
// If path is empty, DefaultPath() is used.
func NewWatcher(path string) *Watcher {
	if path == "" {
		path = DefaultPath()
	}
	return &Watcher{
		path: path,
		done: make(chan struct{}),
	}
}

// OnChange registers a handler that is called whenever the config reloads successfully.
func (w *Watcher) OnChange(fn func(*Config)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.handlers = append(w.handlers, fn)
}

// Start begins watching the config file. It blocks until Stop is called.
func (w *Watcher) Start() error {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer fw.Close() //nolint:errcheck

	// Watch the directory so we catch renames/atomic writes.
	dir := w.path[:max(0, lastSep(w.path))]
	if dir == "" {
		dir = "."
	}
	if err := fw.Add(dir); err != nil {
		return err
	}

	var debounceTimer *time.Timer

	for {
		select {
		case <-w.done:
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return nil

		case event, ok := <-fw.Events:
			if !ok {
				return nil
			}
			if event.Name != w.path {
				continue
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) {
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.AfterFunc(debounceDuration, func() {
					w.reload()
				})
			}

		case err, ok := <-fw.Errors:
			if !ok {
				return nil
			}
			slog.Error("config watcher error", "err", err)
		}
	}
}

// Stop signals the watcher to shut down.
func (w *Watcher) Stop() {
	close(w.done)
}

// reload reads the config and notifies all handlers.
func (w *Watcher) reload() {
	cfg, err := Load(w.path)
	if err != nil {
		slog.Error("config reload failed", "path", w.path, "err", err)
		return
	}
	slog.Info("config reloaded", "path", w.path)

	w.mu.Lock()
	handlers := make([]func(*Config), len(w.handlers))
	copy(handlers, w.handlers)
	w.mu.Unlock()

	for _, h := range handlers {
		h(cfg)
	}
}

func lastSep(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' || s[i] == '\\' {
			return i
		}
	}
	return -1
}
