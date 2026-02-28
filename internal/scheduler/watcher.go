package scheduler

import (
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const watchDebounce = 500 * time.Millisecond

// FileWatcher watches directories for file changes matching glob patterns.
type FileWatcher struct {
	watcher  *fsnotify.Watcher
	handlers map[string]watchEntry // task name → entry
	mu       sync.Mutex
	stop     chan struct{}
	once     sync.Once
}

type watchEntry struct {
	glob string
	fn   func(path string)
}

// NewFileWatcher creates a FileWatcher.
func NewFileWatcher() (*FileWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &FileWatcher{
		watcher:  w,
		handlers: make(map[string]watchEntry),
		stop:     make(chan struct{}),
	}, nil
}

// Add registers a named file-watch trigger.
// glob is a filepath.Match pattern. fn is called with the matched path.
// The directory portion of glob is watched.
func (fw *FileWatcher) Add(name, glob string, fn func(path string)) error {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	fw.handlers[name] = watchEntry{glob: glob, fn: fn}

	dir := filepath.Dir(glob)
	if err := fw.watcher.Add(dir); err != nil {
		slog.Warn("file watcher: could not watch dir", "dir", dir, "err", err)
	}
	slog.Info("file watch added", "name", name, "glob", glob)
	return nil
}

// Remove unregisters a named file-watch trigger.
func (fw *FileWatcher) Remove(name string) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	delete(fw.handlers, name)
}

// Start begins dispatching file events. Blocks until Stop is called.
func (fw *FileWatcher) Start() {
	pending := make(map[string]*time.Timer) // path → debounce timer

	for {
		select {
		case <-fw.stop:
			fw.watcher.Close() //nolint:errcheck
			return

		case event, ok := <-fw.watcher.Events:
			if !ok {
				return
			}
			path := event.Name
			// Debounce: reset timer on each event for the same path.
			if t, ok := pending[path]; ok {
				t.Reset(watchDebounce)
			} else {
				pending[path] = time.AfterFunc(watchDebounce, func() {
					fw.mu.Lock()
					handlers := make([]watchEntry, 0, len(fw.handlers))
					for _, h := range fw.handlers {
						handlers = append(handlers, h)
					}
					fw.mu.Unlock()

					for _, h := range handlers {
						if matched, _ := filepath.Match(h.glob, path); matched {
							slog.Info("file watch triggered", "path", path)
							h.fn(path)
						}
					}
					delete(pending, path)
				})
			}

		case err, ok := <-fw.watcher.Errors:
			if !ok {
				return
			}
			slog.Warn("file watcher error", "err", err)
		}
	}
}

// Stop halts the file watcher.
func (fw *FileWatcher) Stop() {
	fw.once.Do(func() { close(fw.stop) })
}
