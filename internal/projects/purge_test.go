package projects

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/lsegal/aviary/internal/store"
)

// seedTargetData gives m a work state, transcripts and clones for o/r and
// for the projects beside it, returning the clone directories by job key.
func seedTargetData(t *testing.T, m *Manager) map[string]string {
	t.Helper()
	root := t.TempDir()
	clones := map[string]string{
		"o/r#1":   makeClone(t, root, "r-gh-fix-1", "o/r"),
		"o/r#2":   makeClone(t, root, "r-gh-fix-2", "o/r"),
		"o/s#3":   makeClone(t, root, "s-gh-fix-3", "o/s"),
		"o/r_x#4": makeClone(t, root, "r_x-gh-fix-4", "o/r_x"),
	}
	work := map[string]workState{}
	reg, err := loadCloneRegistry(clonesPathFor(m.statePath()))
	if err != nil {
		t.Fatal(err)
	}
	for key, dir := range clones {
		work[key] = workState{Status: "paused", CheckoutDirectory: dir}
		if err := reg.record(cloneRecord{Path: dir, Job: key}); err != nil {
			t.Fatal(err)
		}
		m.transcripts.For(key).System("ran " + key)
	}
	if err := store.WriteJSON(m.statePath(), work); err != nil {
		t.Fatal(err)
	}
	return clones
}

func savedJobs(t *testing.T, m *Manager) map[string]json.RawMessage {
	t.Helper()
	raw, err := store.ReadJSON[map[string]json.RawMessage](m.statePath())
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestTargetDataCountsOnlyTheProjectsData(t *testing.T) {
	m := newTestManager(t)
	seedTargetData(t, m)
	got, err := m.TargetData("o/r")
	if err != nil {
		t.Fatal(err)
	}
	if want := (TargetData{Jobs: 2, Transcripts: 2, Clones: 2}); got != want {
		t.Fatalf("TargetData(o/r) = %+v, want %+v", got, want)
	}
}

func TestPurgeTargetDeletesOnlyTheProjectsData(t *testing.T) {
	m := newTestManager(t)
	clones := seedTargetData(t, m)
	if err := m.purgeTarget("o/r"); err != nil {
		t.Fatal(err)
	}
	jobs := savedJobs(t, m)
	for key, dir := range clones {
		_, kept := jobs[key]
		transcript := filepath.Join(m.transcripts.dir, transcriptDirName(key))
		want := key == "o/s#3" || key == "o/r_x#4"
		if kept != want || isDir(transcript) != want || isDir(dir) != want {
			t.Errorf("%s: record kept %t, transcript kept %t, clone kept %t; want %t", key, kept, isDir(transcript), isDir(dir), want)
		}
	}
	if got, _ := m.TargetData("o/r"); got != (TargetData{}) {
		t.Fatalf("o/r still has %+v after the purge", got)
	}
	// A purged job's transcript is not served from memory either.
	if events := m.transcripts.For("o/r#1").Events(0); len(events) != 0 {
		t.Fatalf("o/r#1 transcript = %+v, want none", events)
	}
}

func TestPurgeTargetRefusesAWatchedProject(t *testing.T) {
	m := newTestManager(t)
	clones := seedTargetData(t, m)
	m.applyConfig(ownedBy(m.cfg, "o/r"))
	if err := m.purgeTarget("o/r"); err == nil {
		t.Fatal("purged a project that is watched again")
	}
	if _, ok := savedJobs(t, m)["o/r#1"]; !ok || !isDir(clones["o/r#1"]) {
		t.Fatal("a refused purge deleted data")
	}
}

func TestPurgeNeverRemovesALeasedOrLiveClone(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(t.TempDir(), "state.json")
	leased := makeClone(t, root, "r-gh-fix-1", "o/r")
	live := makeClone(t, root, "r-gh-fix-2", "o/r")
	unused := makeClone(t, root, "r-gh-fix-3", "o/r")
	w := &Engine{Targets: []string{"o/s"}, StatePath: statePath, Out: &bytes.Buffer{}, Active: &activeAgents{}}
	w.SetTempDirs([]string{root})
	reg, err := w.cloneRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for i, dir := range []string{leased, live, unused} {
		if err := reg.record(cloneRecord{Path: dir, Job: "o/r#" + string(rune('1'+i))}); err != nil {
			t.Fatal(err)
		}
	}
	// The running engine is handing the first clone to one of its jobs and
	// an agent works in the second.
	w.clones.leased = map[string]string{cloneKey(leased): "o/s#9"}
	w.Active.add(Issue{Number: 8, Target: "o/s"}, live)

	w.removeUnusedClones(reg, func(rec cloneRecord) bool { return cloneOfTarget(rec, "o/r") })

	if !isDir(leased) || !isDir(live) {
		t.Fatal("removed a clone a job still uses")
	}
	if isDir(unused) {
		t.Fatal("kept the unused clone")
	}
}

func TestPurgeTargetWorkStateKeepsOtherProjects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"o/r#1":{"status":"paused"},"o/r#2":{"status":"completed"},"o/s#3":{"status":"completed"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if n, err := purgeTargetWorkState(path, "o/r"); err != nil || n != 2 {
		t.Fatalf("purge = %d, %v; want 2", n, err)
	}
	if raw, _ := store.ReadJSON[map[string]json.RawMessage](path); len(raw) != 1 || raw["o/s#3"] == nil {
		t.Fatalf("state = %v, want only o/s#3", raw)
	}
}
