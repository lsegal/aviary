package skills

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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

	require.NoError(t, os.MkdirAll(filepath.Join(root, "new-skill"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "new-skill", "SKILL.md"), []byte("content"), 0o600))

	assert.Eventually(t, func() bool {
		return len(changed) > 0
	}, 3*time.Second, 50*time.Millisecond)
}
