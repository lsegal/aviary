package skills

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const watchDebounce = 300 * time.Millisecond

// Watcher watches installed-skill directories and notifies handlers on change.
type Watcher struct {
	dirs     []string
	handlers []func()
	mu       sync.Mutex
	done     chan struct{}
}

// NewWatcher creates a watcher for the provided dirs.
// When no dirs are provided, all installed skill dirs are watched.
func NewWatcher(dirs ...string) *Watcher {
	if len(dirs) == 0 {
		dirs = InstalledDirs()
	}
	return &Watcher{
		dirs: normalizeWatchDirs(dirs),
		done: make(chan struct{}),
	}
}

// OnChange registers a handler invoked after the watched directories change.
func (w *Watcher) OnChange(fn func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.handlers = append(w.handlers, fn)
}

// Start begins watching skill directories until Stop is called.
func (w *Watcher) Start() error {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer fw.Close() //nolint:errcheck

	watched := make(map[string]struct{})
	for _, dir := range w.dirs {
		if err := addWatchRoots(fw, watched, dir); err != nil {
			return err
		}
	}

	var debounceTimer *time.Timer
	trigger := func() {
		if debounceTimer != nil {
			debounceTimer.Stop()
		}
		debounceTimer = time.AfterFunc(watchDebounce, func() {
			for _, dir := range w.dirs {
				if err := addWatchRoots(fw, watched, dir); err != nil {
					slog.Warn("skills watcher: refresh failed", "dir", dir, "err", err)
				}
			}
			w.notify()
		})
	}

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
			if !w.matches(event.Name) {
				continue
			}
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if err := addWatchTree(fw, watched, event.Name); err != nil {
						slog.Warn("skills watcher: could not watch created dir", "dir", event.Name, "err", err)
					}
				}
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				trigger()
			}

		case err, ok := <-fw.Errors:
			if !ok {
				return nil
			}
			slog.Warn("skills watcher error", "err", err)
		}
	}
}

// Stop stops the watcher.
func (w *Watcher) Stop() {
	close(w.done)
}

func (w *Watcher) notify() {
	w.mu.Lock()
	handlers := append([]func(){}, w.handlers...)
	w.mu.Unlock()
	for _, handler := range handlers {
		handler()
	}
}

func (w *Watcher) matches(path string) bool {
	path = filepath.Clean(path)
	for _, dir := range w.dirs {
		if path == dir {
			return true
		}
		if strings.HasPrefix(path, dir+string(os.PathSeparator)) {
			return true
		}
		if filepath.Dir(dir) == path {
			return true
		}
	}
	return false
}

func normalizeWatchDirs(dirs []string) []string {
	out := make([]string, 0, len(dirs))
	seen := make(map[string]struct{}, len(dirs))
	for _, dir := range dirs {
		dir = filepath.Clean(strings.TrimSpace(dir))
		if dir == "" {
			continue
		}
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		out = append(out, dir)
	}
	return out
}

func addWatchRoots(fw *fsnotify.Watcher, watched map[string]struct{}, dir string) error {
	parent := filepath.Dir(dir)
	if parent == "" {
		parent = "."
	}
	if err := addWatchDir(fw, watched, parent); err != nil {
		return err
	}
	return addWatchTree(fw, watched, dir)
}

func addWatchTree(fw *fsnotify.Watcher, watched map[string]struct{}, root string) error {
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			return nil
		}
		if err := addWatchDir(fw, watched, path); err != nil {
			slog.Warn("skills watcher: could not watch dir", "dir", path, "err", err)
		}
		return nil
	})
}

func addWatchDir(fw *fsnotify.Watcher, watched map[string]struct{}, dir string) error {
	dir = filepath.Clean(dir)
	if _, ok := watched[dir]; ok {
		return nil
	}
	if err := fw.Add(dir); err != nil {
		return err
	}
	watched[dir] = struct{}{}
	return nil
}
