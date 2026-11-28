package skills

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWatcherTriggersOnSkillFileChanges(t *testing.T) {
	root := filepath.Join(t.TempDir(), "skills")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "demo"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "demo", "SKILL.md"), []byte("first"), 0o600))

	w := NewWatcher(root)
	defer w.Stop()

	changed := make(chan struct{}, 8)
	w.OnChange(func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})

	go func() {
		_ = w.Start()
	}()
	time.Sleep(50 * time.Millisecond)

	require.Eventually(t, func() bool {
		return os.WriteFile(filepath.Join(root, "demo", "SKILL.md"), []byte("second"), 0o600) == nil
	}, 2*time.Second, 50*time.Millisecond)

	assert.Eventually(t, func() bool {
		return len(changed) > 0
	}, 3*time.Second, 50*time.Millisecond)
}

func TestWatcherTriggersWhenNewSkillDirectoryIsCreated(t *testing.T) {
	root := filepath.Join(t.TempDir(), "skills")
	require.NoError(t, os.MkdirAll(root, 0o700))

	w := NewWatcher(root)
	defer w.Stop()

	changed := make(chan struct{}, 8)
	w.OnChange(func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})

	go func() {
		_ = w.Start()
	}()
	time.Sleep(50 * time.Millisecond)

	require.NoError(t, os.MkdirAll(filepath.Join(root, "new-skill"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "new-skill", "SKILL.md"), []byte("content"), 0o600))

	assert.Eventually(t, func() bool {
		return len(changed) > 0
	}, 3*time.Second, 50*time.Millisecond)
}

func newTestFSWatcher(t *testing.T) *fsnotify.Watcher {
	t.Helper()
	fw, err := fsnotify.NewWatcher()
	require.NoError(t, err)
	t.Cleanup(func() { _ = fw.Close() })
	return fw
}

func startWatcher(t *testing.T, w *Watcher) <-chan error {
	t.Helper()
	errc := make(chan error, 1)
	go func() { errc <- w.Start() }()
	return errc
}

func TestNewWatcherDefaultsToInstalledDirs(t *testing.T) {
	base := filepath.Join(t.TempDir(), "aviary-config")
	home := t.TempDir()
	t.Setenv("AVIARY_CONFIG_BASE_DIR", base)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	w := NewWatcher()

	assert.Equal(t, []string{
		filepath.Join(base, "skills"),
		filepath.Join(home, ".agents", "skills"),
	}, w.dirs)
}

func TestNewWatcherNormalizesAndDedupesDirs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "skills")
	other := filepath.Join(t.TempDir(), "other")

	w := NewWatcher(root, "  "+root+"  ", root+string(os.PathSeparator), other, filepath.Join(other, "x", ".."))

	assert.Equal(t, []string{root, other}, w.dirs)
}

func TestWatcherMatches(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "skills")
	w := NewWatcher(root)

	assert.True(t, w.matches(root), "watched root itself")
	assert.True(t, w.matches(root+string(os.PathSeparator)), "root with trailing separator")
	assert.True(t, w.matches(filepath.Join(root, "demo", "SKILL.md")), "file inside root")
	assert.True(t, w.matches(parent), "parent of root, for root create/remove")
	assert.False(t, w.matches(filepath.Join(parent, "skills-other")), "sibling sharing a prefix")
	assert.False(t, w.matches(filepath.Join(parent, "notes.txt")), "unrelated file in parent")
	assert.False(t, w.matches(filepath.Join(t.TempDir(), "SKILL.md")), "unrelated directory")
}

func TestWatcherStartFailsWhenRootParentIsMissing(t *testing.T) {
	w := NewWatcher(filepath.Join(t.TempDir(), "missing", "skills"))

	select {
	case err := <-startWatcher(t, w):
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		w.Stop()
		t.Fatal("Start did not return an error for a missing root parent")
	}
}

func TestWatcherStartReturnsAfterStop(t *testing.T) {
	root := filepath.Join(t.TempDir(), "skills")
	w := NewWatcher(root)
	errc := startWatcher(t, w)

	w.Stop()

	select {
	case err := <-errc:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after Stop")
	}
}

func TestWatchCreatedDir(t *testing.T) {
	fw := newTestFSWatcher(t)
	watched := map[string]struct{}{}
	dir := t.TempDir()

	watchCreatedDir(fw, watched, filepath.Join(dir, "missing"))
	assert.Empty(t, watched, "missing paths are ignored")

	file := filepath.Join(dir, "SKILL.md")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))
	watchCreatedDir(fw, watched, file)
	assert.Empty(t, watched, "regular files are ignored")

	nested := filepath.Join(dir, "new", "nested")
	require.NoError(t, os.MkdirAll(nested, 0o700))
	watchCreatedDir(fw, watched, filepath.Join(dir, "new"))
	assert.Contains(t, watched, filepath.Join(dir, "new"))
	assert.Contains(t, watched, nested)
	assert.ElementsMatch(t, []string{filepath.Join(dir, "new"), nested}, fw.WatchList())
}

func TestAddWatchTreeSkipsMissingAndFiles(t *testing.T) {
	fw := newTestFSWatcher(t)
	watched := map[string]struct{}{}
	dir := t.TempDir()

	require.NoError(t, addWatchTree(fw, watched, filepath.Join(dir, "missing")))
	file := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))
	require.NoError(t, addWatchTree(fw, watched, file))

	assert.Empty(t, watched)
	assert.Empty(t, fw.WatchList())
}

func TestAddWatchTreeReportsStatErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows reports a path below a file as not existing")
	}
	fw := newTestFSWatcher(t)
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))

	err := addWatchTree(fw, map[string]struct{}{}, filepath.Join(file, "child"))

	require.Error(t, err)
}

func TestAddWatchTreeSkipsUnreadableSubtree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions do not block access on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	fw := newTestFSWatcher(t)
	watched := map[string]struct{}{}
	root := t.TempDir()
	readable := filepath.Join(root, "readable")
	locked := filepath.Join(root, "locked")
	require.NoError(t, os.MkdirAll(readable, 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(locked, "hidden"), 0o700))
	require.NoError(t, os.Chmod(locked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	require.NoError(t, addWatchTree(fw, watched, root))

	assert.Contains(t, watched, root)
	assert.Contains(t, watched, readable)
	assert.NotContains(t, watched, filepath.Join(locked, "hidden"))
}

func TestAddWatchRootsWatchesParentAndTree(t *testing.T) {
	fw := newTestFSWatcher(t)
	watched := map[string]struct{}{}
	parent := t.TempDir()
	root := filepath.Join(parent, "skills")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "demo"), 0o700))

	require.NoError(t, addWatchRoots(fw, watched, root))
	// A second pass over the same root is a no-op.
	require.NoError(t, addWatchRoots(fw, watched, root))

	assert.ElementsMatch(t, []string{parent, root, filepath.Join(root, "demo")}, fw.WatchList())
	assert.Len(t, watched, 3)
}

func TestAddWatchRootsMissingRootWatchesParentOnly(t *testing.T) {
	fw := newTestFSWatcher(t)
	watched := map[string]struct{}{}
	parent := t.TempDir()

	require.NoError(t, addWatchRoots(fw, watched, filepath.Join(parent, "skills")))

	assert.Equal(t, []string{parent}, fw.WatchList())
}

func TestAddWatchRootsFailsWhenParentMissing(t *testing.T) {
	fw := newTestFSWatcher(t)
	watched := map[string]struct{}{}

	err := addWatchRoots(fw, watched, filepath.Join(t.TempDir(), "missing", "skills"))

	require.Error(t, err)
	assert.Empty(t, watched)
}

// countingWatcher runs a Watcher over dirs and counts its notifications.
type countingWatcher struct {
	*Watcher
	mu    sync.Mutex
	calls int
}

func startCountingWatcher(t *testing.T, dirs ...string) *countingWatcher {
	t.Helper()
	cw := &countingWatcher{Watcher: NewWatcher(dirs...)}
	cw.OnChange(func() {
		cw.mu.Lock()
		cw.calls++
		cw.mu.Unlock()
	})
	startWatcher(t, cw.Watcher)
	t.Cleanup(cw.Stop)
	return cw
}

func (cw *countingWatcher) count() int {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	return cw.calls
}

// waitForChange writes to file until a notification beyond the current count
// arrives, polling slower than the debounce so each write's notification can
// fire. It returns the settled count afterwards.
func (cw *countingWatcher) waitForChange(t *testing.T, file string) int {
	t.Helper()
	before := cw.count()
	require.Eventually(t, func() bool {
		if cw.count() > before {
			return true
		}
		_ = os.WriteFile(file, []byte(time.Now().String()), 0o600)
		return false
	}, 10*time.Second, watchDebounce*2)
	return cw.count()
}

func newSkillFile(t *testing.T, root string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "demo"), 0o700))
	skill := filepath.Join(root, "demo", "SKILL.md")
	require.NoError(t, os.WriteFile(skill, []byte("initial"), 0o600))
	return skill
}

func TestWatcherWatchesRootCreatedAfterStart(t *testing.T) {
	parent := t.TempDir()
	ready := filepath.Join(parent, "ready")
	probe := newSkillFile(t, ready)
	root := filepath.Join(parent, "skills")
	cw := startCountingWatcher(t, ready, root)
	cw.waitForChange(t, probe)

	// Only root's parent is watched until root appears; creating it must
	// notify and start watching the new tree.
	late := filepath.Join(root, "late")
	require.NoError(t, os.MkdirAll(late, 0o700))
	skill := filepath.Join(late, "SKILL.md")
	require.NoError(t, os.WriteFile(skill, []byte("created"), 0o600))
	cw.waitForChange(t, skill)

	// The tree is now watched, so a later edit inside it notifies too.
	settled := cw.count()
	require.NoError(t, os.WriteFile(skill, []byte("edited"), 0o600))
	assert.Eventually(t, func() bool { return cw.count() > settled }, 5*time.Second, 10*time.Millisecond)
}

func TestWatcherIgnoresUnrelatedFiles(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "skills")
	skill := newSkillFile(t, root)
	cw := startCountingWatcher(t, root)
	settled := cw.waitForChange(t, skill)

	// Changes to unrelated files in the watched parent never notify.
	unrelated := filepath.Join(parent, "notes.txt")
	for range 5 {
		require.NoError(t, os.WriteFile(unrelated, []byte(time.Now().String()), 0o600))
	}
	require.NoError(t, os.Remove(unrelated))
	assert.Never(t, func() bool { return cw.count() > settled }, watchDebounce*3, 20*time.Millisecond)

	// The watcher is still running and reports the next skill edit.
	assert.Equal(t, settled+1, cw.waitForChange(t, skill))
}

func TestWatcherDebouncesBurstOfChanges(t *testing.T) {
	root := filepath.Join(t.TempDir(), "skills")
	skill := newSkillFile(t, root)
	cw := startCountingWatcher(t, root)
	settled := cw.waitForChange(t, skill)

	// A burst of writes well inside one debounce window notifies once.
	for i := range 10 {
		require.NoError(t, os.WriteFile(skill, []byte{byte('a' + i)}, 0o600))
	}
	require.Eventually(t, func() bool { return cw.count() > settled }, 5*time.Second, 10*time.Millisecond)
	assert.Never(t, func() bool { return cw.count() > settled+1 }, watchDebounce*2, 20*time.Millisecond)
}
