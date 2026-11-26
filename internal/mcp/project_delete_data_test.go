package mcp

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/store"
)

// projectData is the data a Code Loop project leaves on disk: its job
// record in state.json, its job transcript and its clone.
type projectData struct {
	state, transcript, clone string
}

// watchWithData watches o/r at runtime and gives it, and o/s beside it, one
// finished job each, with a transcript and a clone.
func watchWithData(t *testing.T) (*Dispatcher, projectData) {
	t.Helper()
	d, m := setupProjects(t)
	m.Reconcile(&config.Config{Models: config.ModelsConfig{Defaults: &config.ModelDefaults{Model: "cli/codex/gpt-5.6-terra"}}})
	callJSON(t, d, "project_add", map[string]any{"target": "o/r", "runtime": true})
	stateDir := filepath.Join(filepath.Dir(store.DataDir()), "projects")
	cloneRoot := t.TempDir()
	clones := map[string]string{}
	var records []map[string]any
	for _, key := range []string{"o/r#7", "o/s#8"} {
		dir := filepath.Join(cloneRoot, filepath.Base(key[:3])+"-gh-fix-"+key[4:])
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o755))
		clones[key] = dir
		records = append(records, map[string]any{"path": dir, "job": key})
		transcript := filepath.Join(stateDir, "jobs", "o_"+key[2:3]+"_"+key[4:])
		require.NoError(t, os.MkdirAll(transcript, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(transcript, "events.jsonl"), []byte("{}\n"), 0o600))
	}
	require.NoError(t, store.WriteJSON(filepath.Join(stateDir, "state.clones.json"), records))
	require.NoError(t, store.WriteJSON(filepath.Join(stateDir, "state.json"), map[string]any{
		"o/r#7": map[string]any{"status": "completed", "checkoutDirectory": clones["o/r#7"]},
		"o/s#8": map[string]any{"status": "completed", "checkoutDirectory": clones["o/s#8"]},
	}))
	t.Cleanup(func() {
		for _, dir := range clones {
			_ = os.RemoveAll(dir)
		}
	})
	return d, projectData{state: filepath.Join(stateDir, "state.json"), transcript: filepath.Join(stateDir, "jobs"), clone: cloneRoot}
}

func TestDeletePreviewProjectCounts(t *testing.T) {
	d, _ := watchWithData(t)
	counts := callJSON(t, d, "delete_preview", map[string]any{"kind": "project", "name": "o/r"})
	assert.EqualValues(t, 1, counts["runs"])
	assert.EqualValues(t, 2, counts["files"], "one transcript and one clone")
	assert.EqualValues(t, 0, counts["sessions"])
}

func TestProjectRemoveKeepsDataByDefault(t *testing.T) {
	d, data := watchWithData(t)
	removed := callJSON(t, d, "project_remove", map[string]any{"target": "o/r", "runtime": true})
	assert.Equal(t, false, removed["data_deleted"])
	// Nothing deletes it later either.
	time.Sleep(200 * time.Millisecond)
	assert.Contains(t, readFile(t, data.state), "o/r#7")
	assert.DirExists(t, filepath.Join(data.transcript, "o_r_7"))
	assert.DirExists(t, filepath.Join(data.clone, "r-gh-fix-7"))
}

func TestProjectRemoveDeleteDataPurgesOnlyThatProject(t *testing.T) {
	d, data := watchWithData(t)
	removed := callJSON(t, d, "project_remove", map[string]any{"target": "o/r", "runtime": true, "delete_data": true})
	assert.Equal(t, true, removed["data_deleted"])
	require.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(data.clone, "r-gh-fix-7"))
		return os.IsNotExist(err)
	}, 5*time.Second, 20*time.Millisecond, "the clone is removed")
	state := readFile(t, data.state)
	assert.NotContains(t, state, "o/r#7")
	assert.Contains(t, state, "o/s#8")
	assert.NoDirExists(t, filepath.Join(data.transcript, "o_r_7"))
	assert.DirExists(t, filepath.Join(data.transcript, "o_s_8"))
	assert.DirExists(t, filepath.Join(data.clone, "s-gh-fix-8"))
}
