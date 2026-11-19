package projects

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/projects/core"
)

// stubDiskFree reports free bytes per directory for the duration of a test.
func stubDiskFree(t *testing.T, free map[string]uint64) {
	t.Helper()
	previous := diskFree
	diskFree = func(dir string) (uint64, error) { return free[dir], nil }
	t.Cleanup(func() { diskFree = previous })
}

// makeClone creates a directory that looks like a gh-fix clone of repo.
func makeClone(t *testing.T, root, name, repo string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := "[core]\n\tbare = false\n[remote \"origin\"]\n\turl = https://github.com/" + repo + ".git\n"
	if err := os.WriteFile(filepath.Join(dir, ".git", "config"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), bytes.Repeat([]byte("x"), 1000), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCloneRootsDefaultToSystemTemp(t *testing.T) {
	if got := cloneRoots(nil); len(got) != 1 || got[0] != filepath.Clean(os.TempDir()) {
		t.Fatalf("empty setting = %v, want only the system temp directory", got)
	}
	a, b := t.TempDir(), t.TempDir()
	if got := cloneRoots([]string{a, " ", b, a}); len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("configured roots = %v, want [%s %s] in order", got, a, b)
	}
	if _, err := normalizeTempDirs([]string{"relative/dir"}); err == nil {
		t.Fatal("a relative temp directory was accepted")
	}
}

func TestPickCloneRootFallsThroughOnLowSpace(t *testing.T) {
	full, roomy := t.TempDir(), t.TempDir()
	stubDiskFree(t, map[string]uint64{full: cloneMinFreeBytes - 1, roomy: cloneMinFreeBytes})
	root, err := pickCloneRoot([]string{full, roomy})
	if err != nil || root != roomy {
		t.Fatalf("pickCloneRoot = %q, %v; want the second directory %q", root, err, roomy)
	}
	other := t.TempDir()
	stubDiskFree(t, map[string]uint64{full: 1, roomy: 1, other: cloneMinFreeBytes * 10})
	root, err = pickCloneRoot([]string{full, roomy})
	if !errors.Is(err, errNoCloneSpace) || root != "" {
		t.Fatalf("pickCloneRoot with every directory full = %q, %v; want errNoCloneSpace", root, err)
	}
	if !strings.Contains(err.Error(), full) || !strings.Contains(err.Error(), roomy) {
		t.Fatalf("out-of-space error does not name the directories tried: %v", err)
	}
}

func TestRunFailsWhenNoTempDirectoryHasSpace(t *testing.T) {
	runner := CommandRunner{Agent: "claude", Repo: "o/r", CloneRoot: func() (string, error) { return "", errNoCloneSpace }}
	_, err := runner.runOnce(context.Background(), Issue{Number: 7, Target: "o/r"}, AgentSession{Agent: "claude"}, nil, nil)
	if !errors.Is(err, errNoCloneSpace) {
		t.Fatalf("run error = %v, want errNoCloneSpace", err)
	}
}

func TestNewRunNamesItsCloneDirectory(t *testing.T) {
	root := t.TempDir()
	runner := CommandRunner{Agent: "claude", Repo: "o/r", CloneRoot: func() (string, error) { return root, nil }}
	session, err := runner.resolveRunDir(AgentSession{Agent: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if dir, inCheckout := sessionRunDir(session); dir != root || inCheckout {
		t.Fatalf("run dir = %q (checkout %t), want %q", dir, inCheckout, root)
	}
	args := commandArgsForSession(runner, Issue{Number: 7, Target: "o/r"}, session)
	if prompt := args[len(args)-1]; !strings.HasSuffix(prompt, "Clone directory: "+root) {
		t.Fatalf("prompt does not name the clone directory: %q", prompt)
	}
	// A run that already has its checkout starts there and picks no root.
	checkout := t.TempDir()
	session, _ = runner.resolveRunDir(AgentSession{Agent: "claude", CheckoutDirectory: checkout})
	if dir, inCheckout := sessionRunDir(session); dir != checkout || !inCheckout || session.RunDir != "" {
		t.Fatalf("run with a checkout started in %q (checkout %t, run dir %q)", dir, inCheckout, session.RunDir)
	}
}

// TestClearClonesKeepsActiveAndPausedJobs is the acceptance case: with an
// active job, a paused job, and a finished job's leftover all present, clear
// removes only the finished job's clone and reports the space reclaimed.
func TestClearClonesKeepsActiveAndPausedJobs(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(t.TempDir(), ".aviary.json")
	active := makeClone(t, root, "r-gh-fix-1", "o/r")
	paused := makeClone(t, root, "r-gh-fix-2", "o/r")
	finished := makeClone(t, root, "r-gh-fix-3", "o/r")
	work := map[string]workState{
		"o/r#1": {Status: "active", CheckoutDirectory: active},
		"o/r#2": {Status: "paused", CheckoutDirectory: paused},
		"o/r#3": {Status: "completed", CheckoutDirectory: finished},
	}
	if err := saveScopedWorkState(statePath, work, []string{"o/r", "o/s"}); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	w := &Engine{Targets: []string{"o/r", "o/s"}, StatePath: statePath, Out: &logs}
	w.SetTempDirs([]string{root})
	for number, dir := range map[int]string{1: active, 2: paused, 3: finished} {
		w.recordClone(issueKey(Issue{Number: number, Target: "o/r"}), "o/r", Issue{Number: number, Target: "o/r"}, dir)
	}
	snapshot, err := w.ListClones(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Clones) != 3 {
		t.Fatalf("listed %d clones, want 3: %+v", len(snapshot.Clones), snapshot.Clones)
	}
	for _, clone := range snapshot.Clones {
		if clone.Repository != "o/r" || clone.Issue == 0 {
			t.Fatalf("clone %s lost its repository or issue: %+v", clone.Path, clone)
		}
		if clone.InUse != (clone.Path != finished) {
			t.Fatalf("clone %s in use = %t (%s)", clone.Path, clone.InUse, clone.Reason)
		}
	}
	result, err := w.ClearClones(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Removed) != 1 || result.Removed[0].Path != finished || result.Kept != 2 || result.Reclaimed < 1000 {
		t.Fatalf("clear = %+v, want only %s removed with its space reclaimed", result, finished)
	}
	if isDir(finished) || !isDir(active) || !isDir(paused) {
		t.Fatal("clear removed the wrong directories")
	}
	// Each removal logs its size and the round ends with the total
	//.
	if result.Removed[0].Size != result.Reclaimed {
		t.Fatalf("removed size %d, reclaimed %d", result.Removed[0].Size, result.Reclaimed)
	}
	size := formatBytes(result.Reclaimed)
	for _, want := range []string{"removed unused clone " + finished + " (" + size + ")", "cleared 1 unused clone, reclaimed " + size} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("logs missing %q:\n%s", want, logs.String())
		}
	}
}

func TestClearClonesKeepsLiveAgentCheckouts(t *testing.T) {
	root := t.TempDir()
	dir := makeClone(t, root, "r-gh-fix-5", "o/r")
	agents := &activeAgents{}
	agents.add(Issue{Number: 5, Target: "o/r"}, dir)
	reg := &cloneRegistry{records: map[string]cloneRecord{}}
	_ = reg.record(cloneRecord{Path: dir, Repository: "o/r", Issue: 5, Job: "o/r#5"})
	usage := func() cloneUsage {
		return cloneUsage{roots: []string{root}, work: map[string]workState{"o/r#5": {Status: "completed"}}, live: agents.checkouts()}
	}
	if result := clearClones(reg, usage); len(result.Removed) != 0 || !isDir(dir) {
		t.Fatalf("clear removed a clone a live agent works in: %+v", result)
	}
}

func TestClonesSurviveRestartAndAdoptLeftovers(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(t.TempDir(), "state.json")
	recorded := makeClone(t, root, "r-gh-fix-1", "o/r")
	leftover := makeClone(t, root, "r-gh-fix-9", "o/r")
	unknown := makeClone(t, root, "r-gh-fix-10", "o/r")
	work := map[string]workState{"o/r#1": {Status: "completed"}, "o/r#9": {Status: "completed"}}
	if err := saveScopedWorkState(statePath, work, []string{"o/r", "o/s"}); err != nil {
		t.Fatal(err)
	}
	before := &Engine{Targets: []string{"o/r", "o/s"}, StatePath: statePath, Out: os.Stderr}
	before.SetTempDirs([]string{root})
	before.recordClone("o/r#1", "o/r", Issue{Number: 1, Target: "o/r"}, recorded)

	// A new instance on the same state file is a restart.
	after := &Engine{Targets: []string{"o/r", "o/s"}, StatePath: statePath, Out: os.Stderr}
	after.SetTempDirs([]string{root})
	snapshot, err := after.ListClones(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]core.CloneEntry{}
	for _, clone := range snapshot.Clones {
		byPath[clone.Path] = clone
	}
	if len(byPath) != 3 {
		t.Fatalf("after restart listed %v, want all three clones", snapshot.Clones)
	}
	if clone := byPath[leftover]; clone.InUse || clone.Repository != "o/r" || clone.Issue != 9 || clone.Job != "o/r#9" {
		t.Fatalf("leftover clone of a finished job = %+v, want adopted and unused", clone)
	}
	if clone := byPath[unknown]; !clone.InUse || clone.Reason == "" {
		t.Fatalf("clone no job records = %+v, want kept with a reason", clone)
	}
}

func TestClassifyCloneKeepsWhatItCannotVerify(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	rec := cloneRecord{Path: filepath.Join(outside, "r-gh-fix-1"), Job: "o/r#1"}
	work := map[string]workState{"o/r#1": {Status: "completed"}}
	if inUse, reason := classifyClone(rec, cloneUsage{roots: []string{root}, work: work}); !inUse || !strings.Contains(reason, "outside") {
		t.Fatalf("clone outside the temp directories = %t, %q; want kept", inUse, reason)
	}
	rec.Path = filepath.Join(root, "r-gh-fix-1")
	if inUse, reason := classifyClone(rec, cloneUsage{roots: []string{root}, workErr: errors.New("boom")}); !inUse || !strings.Contains(reason, "boom") {
		t.Fatalf("clone with unreadable work state = %t, %q; want kept", inUse, reason)
	}
	for _, status := range []string{"active", "waiting", "queued", "paused"} {
		work := map[string]workState{"o/r#1": {Status: status}}
		if inUse, _ := classifyClone(rec, cloneUsage{roots: []string{root}, work: work}); !inUse {
			t.Fatalf("clone of a %s job was not in use", status)
		}
	}
}

func TestTempDirsSetting(t *testing.T) {
	dir := t.TempDir()
	w := &Engine{Concurrency: 1, Out: os.Stderr}
	update := []string{dir, " "}
	result := w.applySettingsRequest(SettingsUpdate{TempDirs: &update}, newConcurrencySemaphore(1))
	if result.err != nil {
		t.Fatal(result.err)
	}
	if got := result.snapshot.TempDirs; len(got) != 1 || got[0] != dir {
		t.Fatalf("snapshot temp dirs = %v, want [%s]", got, dir)
	}
	if result.snapshot.TempDirDefault != filepath.Clean(os.TempDir()) {
		t.Fatalf("temp dir default = %q", result.snapshot.TempDirDefault)
	}
	relative := []string{"tmp"}
	if result := w.applySettingsRequest(SettingsUpdate{TempDirs: &relative}, newConcurrencySemaphore(1)); result.err == nil {
		t.Fatal("a relative temp directory was accepted")
	}
	empty := []string{}
	result = w.applySettingsRequest(SettingsUpdate{TempDirs: &empty}, newConcurrencySemaphore(1))
	if len(result.snapshot.TempDirs) != 0 || w.CloneRoots()[0] != filepath.Clean(os.TempDir()) {
		t.Fatalf("clearing the setting left %v (roots %v)", result.snapshot.TempDirs, w.CloneRoots())
	}
}
