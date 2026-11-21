package projects

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/store"
)

// symlinkedRoot returns a temp directory and a symlink to it, as macOS's
// /var is a symlink to /private/var.
func symlinkedRoot(t *testing.T) (target, link string) {
	t.Helper()
	base := t.TempDir()
	target = filepath.Join(base, "private", "T")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link = filepath.Join(base, "T")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	return resolvePath(target), link
}

func TestCloneUnderSymlinkedPathIsInsideTempDirAndListedOnce(t *testing.T) {
	target, link := symlinkedRoot(t)
	statePath := filepath.Join(t.TempDir(), ".aviary.json")
	clone := makeClone(t, target, "r-gh-fix-430", "o/r")
	viaLink := filepath.Join(link, "r-gh-fix-430")
	if err := saveScopedWorkState(statePath, map[string]workState{"o/r#743": {Status: "active", CheckoutDirectory: viaLink}}, []string{"o/r"}); err != nil {
		t.Fatal(err)
	}
	w := &Engine{Targets: []string{"o/r"}, StatePath: statePath, Out: &bytes.Buffer{}}
	// The temp directory is configured through the link, and the run
	// announces its clone through both paths.
	w.SetTempDirs([]string{link})
	issue := Issue{Number: 743, Target: "o/r"}
	w.recordClone("o/r#743", "o/r", issue, viaLink)
	w.recordClone("o/r#743", "o/r", issue, clone)
	snapshot, err := w.ListClones(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Clones) != 1 {
		t.Fatalf("listed %+v, want the one directory once", snapshot.Clones)
	}
	if entry := snapshot.Clones[0]; entry.Path != clone || entry.Kept || !entry.InUse || strings.Contains(entry.Reason, "outside") {
		t.Fatalf("clone = %+v, want %s in use inside the temp directory", entry, clone)
	}
	if !underRoots(viaLink, []string{target}) || !underRoots(clone, []string{link}) {
		t.Fatal("a clone reached through either path must be inside the temp directory reached through the other")
	}
}

func TestCloneRegistryDeduplicatesSymlinkedRecordsOnLoad(t *testing.T) {
	target, link := symlinkedRoot(t)
	clone := makeClone(t, target, "r-gh-fix-421", "o/r")
	path := filepath.Join(t.TempDir(), ".aviary.clones.json")
	old := time.Now().Add(-48 * time.Hour)
	saved := []cloneRecord{
		{Path: clone, Repository: "o/r", Issue: 684, PullRequest: 688, Job: "o/r#684", Created: old},
		{Path: filepath.Join(link, "r-gh-fix-421"), Repository: "o/r", Issue: 746, Job: "o/r#746", Created: old.Add(time.Hour)},
	}
	if err := store.WriteJSON(path, saved); err != nil {
		t.Fatal(err)
	}
	reg, err := loadCloneRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	list := reg.list()
	if len(list) != 1 || list[0].Path != clone || list[0].Job != "o/r#746" || list[0].PullRequest != 0 {
		t.Fatalf("records = %+v, want only #746's record under %s", list, clone)
	}
	// The cleanup is saved, so the duplicate does not come back.
	onDisk, err := store.ReadJSON[[]cloneRecord](path)
	if err != nil || len(onDisk) != 1 || onDisk[0].Job != "o/r#746" {
		t.Fatalf("saved records = %+v, %v", onDisk, err)
	}
}

func TestReusedCloneDropsTheEarlierJobsRecord(t *testing.T) {
	target, link := symlinkedRoot(t)
	clone := makeClone(t, target, "r-gh-fix-422", "o/r")
	reg := &cloneRegistry{records: map[string]cloneRecord{}}
	_ = reg.record(cloneRecord{Path: clone, Repository: "o/r", Issue: 740, PullRequest: 742, Job: "o/r#740"})
	_ = reg.record(cloneRecord{Path: filepath.Join(link, "r-gh-fix-422"), Repository: "o/r", Issue: 745, Job: "o/r#745"})
	list := reg.list()
	if len(list) != 1 || list[0].Job != "o/r#745" || list[0].Issue != 745 || list[0].PullRequest != 0 {
		t.Fatalf("records = %+v, want only #745's record without #740's pull request", list)
	}
}

func TestReaperRemovesOnlyUnusedClones(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(t.TempDir(), ".aviary.json")
	clones := map[string]string{}
	work := map[string]workState{}
	for number, status := range map[string]string{"1": "active", "2": "waiting", "3": "queued", "4": "paused", "5": "stopping", "6": "completed", "7": "failed", "8": "completed"} {
		key := "o/r#" + number
		clones[key] = makeClone(t, root, "r-gh-fix-"+number, "o/r")
		work[key] = workState{Status: status, CheckoutDirectory: clones[key]}
	}
	// #9's job is gone from the work state.
	clones["o/r#9"] = makeClone(t, root, "r-gh-fix-9", "o/r")
	// A clone found on disk for a job this instance never tracked may be
	// another instance's.
	foreign := makeClone(t, root, "s-gh-fix-10", "o/s")
	if err := saveScopedWorkState(statePath, work, []string{"o/r"}); err != nil {
		t.Fatal(err)
	}
	var logs syncBuffer
	w := &Engine{Targets: []string{"o/r"}, StatePath: statePath, Out: &logs, Active: &activeAgents{}}
	w.SetTempDirs([]string{root})
	for key, dir := range clones {
		number := strings.TrimPrefix(key, "o/r#")
		n, _ := strconv.Atoi(number)
		issue := Issue{Number: n, Target: "o/r"}
		w.recordClone(key, "o/r", issue, dir)
	}
	// An agent is still working in #8's clone.
	w.Active.add(Issue{Number: 8, Target: "o/r"}, clones["o/r#8"])

	w.reapClones()

	for key, dir := range clones {
		want := key == "o/r#6" || key == "o/r#7" || key == "o/r#9"
		if removed := !isDir(dir); removed != want {
			t.Errorf("%s clone removed = %t, want %t", key, removed, want)
		}
	}
	if !isDir(foreign) {
		t.Error("the reaper removed a clone no job of this instance records")
	}
	out := logs.String()
	if !strings.Contains(out, "removed unused clone "+clones["o/r#6"]+" (") || !strings.Contains(out, "freed): job is completed") || !strings.Contains(out, "reaper cleared 3 unused clones, reclaimed") {
		t.Fatalf("logs do not report each removal and the space freed:\n%s", out)
	}
}

func TestReaperRunsOnItsInterval(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(t.TempDir(), ".aviary.json")
	clone := makeClone(t, root, "r-gh-fix-1", "o/r")
	if err := saveScopedWorkState(statePath, map[string]workState{"o/r#1": {Status: "completed", CheckoutDirectory: clone}}, []string{"o/r"}); err != nil {
		t.Fatal(err)
	}
	w := &Engine{Targets: []string{"o/r"}, StatePath: statePath, Out: &syncBuffer{}}
	w.SetTempDirs([]string{root})
	w.recordClone("o/r#1", "o/r", Issue{Number: 1, Target: "o/r"}, clone)
	w.clones.reapInterval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.reapClonesEvery(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for isDir(clone) {
		if time.Now().After(deadline) {
			t.Fatal("the reaper never removed the unused clone")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestReleaseJobCloneKeepsSharedAndPausedClones(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(t.TempDir(), ".aviary.json")
	finished := makeClone(t, root, "r-gh-fix-1", "o/r")
	shared := makeClone(t, root, "r-gh-fix-2", "o/r")
	paused := makeClone(t, root, "r-gh-fix-3", "o/r")
	work := map[string]workState{
		"o/r#1": {Status: "completed", CheckoutDirectory: finished},
		"o/r#2": {Status: "completed", CheckoutDirectory: shared},
		"o/r#9": {Status: "active", CheckoutDirectory: shared},
		"o/r#3": {Status: "paused", CheckoutDirectory: paused},
	}
	if err := saveScopedWorkState(statePath, work, []string{"o/r"}); err != nil {
		t.Fatal(err)
	}
	w := &Engine{Targets: []string{"o/r"}, StatePath: statePath, Out: &syncBuffer{}}
	w.SetTempDirs([]string{root})
	for number, dir := range map[int]string{1: finished, 2: shared, 3: paused} {
		w.recordClone(issueKey(Issue{Number: number, Target: "o/r"}), "o/r", Issue{Number: number, Target: "o/r"}, dir)
	}
	w.releaseJobClone("o/r#1", finished)
	w.releaseJobClone("o/r#2", shared)
	w.releaseJobClone("o/r#3", paused)
	if isDir(finished) {
		t.Error("a finished job's clone was kept")
	}
	if !isDir(shared) {
		t.Error("a finished job's clone was removed while another job uses it")
	}
	if !isDir(paused) {
		t.Error("a paused job's clone was removed")
	}
}

// finishingRunner announces a checkout and finishes its run at once.
type finishingRunner struct {
	checkout string
	done     chan struct{}
}

func (f *finishingRunner) AgentName() string                { return "codex" }
func (f *finishingRunner) Run(context.Context, Issue) error { return nil }
func (f *finishingRunner) RunSession(_ context.Context, _ Issue, _ AgentSession, update func(AgentSession)) error {
	update(AgentSession{CheckoutDirectory: f.checkout})
	close(f.done)
	return nil
}

func TestFinishedJobRemovesItsClone(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(map[bool]string{false: "own", true: "shared"}[shared], func(t *testing.T) {
			root := t.TempDir()
			statePath := filepath.Join(t.TempDir(), "state.json")
			clone := makeClone(t, root, "r-gh-fix-7", "o/r")
			if shared {
				// Another job is working in the same directory.
				if err := saveWorkState(statePath, map[int]workState{9: {Status: "active", CheckoutDirectory: clone}}); err != nil {
					t.Fatal(err)
				}
			}
			runner := &finishingRunner{checkout: clone, done: make(chan struct{})}
			var logs syncBuffer
			w := &Engine{
				Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: statePath, Out: &logs,
				Issues: &fakeSource{batches: [][]Issue{{{Number: 7, Target: "o/r"}}}}, Runner: runner,
			}
			w.SetTempDirs([]string{root})
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- w.Run(ctx) }()
			defer func() {
				cancel()
				<-done
			}()
			select {
			case <-runner.done:
			case <-time.After(5 * time.Second):
				t.Fatal("the job was not started")
			}
			deadline := time.Now().Add(5 * time.Second)
			for !strings.Contains(logs.String(), "issue #7 completed") {
				if time.Now().After(deadline) {
					t.Fatalf("the job never completed:\n%s", logs.String())
				}
				time.Sleep(10 * time.Millisecond)
			}
			// The release runs right after the completion is logged.
			time.Sleep(50 * time.Millisecond)
			if removed := !isDir(clone); removed == shared {
				t.Fatalf("clone removed = %t with shared = %t:\n%s", removed, shared, logs.String())
			}
		})
	}
}

func TestReleaseJobCloneKeepsDirectoriesThatAreNotClones(t *testing.T) {
	root := t.TempDir()
	// A checkout that is no git clone, and a clone that holds the state
	// file, are never removed without asking.
	plain := filepath.Join(root, "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	holder := makeClone(t, root, "r-gh-fix-2", "o/r")
	statePath := filepath.Join(holder, ".aviary.json")
	work := map[string]workState{
		"o/r#1": {Status: "completed", CheckoutDirectory: plain},
		"o/r#2": {Status: "completed", CheckoutDirectory: holder},
	}
	if err := saveScopedWorkState(statePath, work, []string{"o/r"}); err != nil {
		t.Fatal(err)
	}
	w := &Engine{Targets: []string{"o/r"}, StatePath: statePath, Out: &syncBuffer{}}
	w.SetTempDirs([]string{root})
	w.releaseJobClone("o/r#1", plain)
	w.releaseJobClone("o/r#2", holder)
	if !isDir(plain) || !isDir(holder) {
		t.Fatal("a directory that is not a removable clone was removed")
	}
}
