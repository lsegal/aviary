package projects

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestStateFilesWriteAtomicallyWithPrivateMode checks that every Code Loop
// state file is written through store.WriteJSON: parent directories are
// created, the file is private, and no temp file is left beside it.
func TestStateFilesWriteAtomicallyWithPrivateMode(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "state")
	statePath := filepath.Join(dir, "state.json")
	scopedPath := filepath.Join(dir, "scoped.json")
	workPath := filepath.Join(dir, "work.json")

	if err := saveState(statePath, map[int]bool{7: true}); err != nil {
		t.Fatalf("saveState: %v", err)
	}
	if err := saveWorkState(workPath, map[int]workState{7: {Status: "completed"}}); err != nil {
		t.Fatalf("saveWorkState: %v", err)
	}
	if err := saveScopedWorkState(scopedPath, map[string]workState{"o/r#7": {Status: "completed"}}, []string{"o/r"}); err != nil {
		t.Fatalf("saveScopedWorkState: %v", err)
	}
	reg, err := loadCloneRegistry(clonesPathFor(statePath))
	if err != nil {
		t.Fatalf("loadCloneRegistry: %v", err)
	}
	reg.mu.Lock()
	reg.records[cloneKey("/tmp/clone")] = cloneRecord{Path: "/tmp/clone"}
	if err := reg.saveLocked(); err != nil {
		t.Fatalf("saveLocked: %v", err)
	}
	reg.mu.Unlock()
	merges := newRecentMerges(mergesPathFor(statePath), time.Now())
	merges.mu.Lock()
	merges.at["o/r#8"] = time.Now()
	merges.save(time.Now())
	merges.mu.Unlock()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("state dir has %v, want exactly the five state files", names)
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %v, want 0600", e.Name(), info.Mode().Perm())
		}
	}

	seen, err := loadState(statePath)
	if err != nil || !seen[7] {
		t.Fatalf("loadState = %v, %v; want issue 7 present", seen, err)
	}
	scoped, err := loadScopedWorkState(scopedPath, []string{"o/r"})
	if err != nil || scoped["o/r#7"].Status != "completed" {
		t.Fatalf("loadScopedWorkState = %v, %v; want o/r#7 completed", scoped, err)
	}
	if got := newRecentMerges(mergesPathFor(statePath), time.Now()).count(time.Now()); got != 1 {
		t.Fatalf("restored merges = %d, want 1", got)
	}
}
