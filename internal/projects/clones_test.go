package projects

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

// stubDiskFree reports free bytes per directory for the duration of a test.
func stubDiskFree(t *testing.T, free map[string]uint64) {
	t.Helper()
	previous := diskFree
	diskFree = func(dir string) (uint64, error) { return free[dir], nil }
	t.Cleanup(func() { diskFree = previous })
}

// makeClone creates a directory that looks like a gh-fix clone of repo and
// returns its resolved path, the form clone records keep.
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
	return resolvePath(dir)
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
	var total int64
	for _, clone := range snapshot.Clones {
		if clone.Size == nil || *clone.Size < 1000 {
			t.Fatalf("clone %s size = %v, want its measured size", clone.Path, clone.Size)
		}
		total += *clone.Size
		if clone.Repository != "o/r" || clone.Issue == 0 {
			t.Fatalf("clone %s lost its repository or issue: %+v", clone.Path, clone)
		}
		if clone.InUse != (clone.Path != finished) {
			t.Fatalf("clone %s in use = %t (%s)", clone.Path, clone.InUse, clone.Reason)
		}
	}
	if snapshot.TotalSize != total || snapshot.Unmeasured != 0 {
		t.Fatalf("total %d (%d unmeasured), want %d", snapshot.TotalSize, snapshot.Unmeasured, total)
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
	if result.Removed[0].Size == nil || *result.Removed[0].Size != result.Reclaimed {
		t.Fatalf("removed size %v, reclaimed %d", result.Removed[0].Size, result.Reclaimed)
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
	if result, err := clearClones(reg, usage); err != nil || len(result.Removed) != 0 || !isDir(dir) {
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
	if clone := byPath[unknown]; clone.InUse || clone.Kept || !strings.Contains(clone.Reason, "not owned by this instance") {
		t.Fatalf("clone no job records = %+v, want unused and not owned by this instance", clone)
	}
}

// TestClonesNoJobRecordsAreNotInUse is #518's acceptance case: with no job
// ever run, leftover clones in the temp directory are listed with their true
// state and cleared, while a paused job's clone stays in use.
func TestClonesNoJobRecordsAreNotInUse(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(t.TempDir(), ".aviary.json")
	var leftovers []string
	for _, name := range []string{"glorp-gh-fix-755", "nupp-gh-fix-139", "aviary-gh-fix-12"} {
		leftovers = append(leftovers, makeClone(t, root, name, "o/"+strings.Split(name, "-")[0]))
	}
	w := &Engine{Targets: []string{"o/r"}, StatePath: statePath, Out: os.Stderr}
	w.SetTempDirs([]string{root})
	snapshot, err := w.ListClones(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Clones) != len(leftovers) {
		t.Fatalf("listed %+v, want every leftover", snapshot.Clones)
	}
	for _, clone := range snapshot.Clones {
		if clone.InUse || clone.Kept || clone.Reason == "" {
			t.Fatalf("leftover %+v, want unused with a reason", clone)
		}
	}

	paused := makeClone(t, root, "r-gh-fix-2", "o/r")
	if err := saveScopedWorkState(statePath, map[string]workState{"o/r#2": {Status: "paused", CheckoutDirectory: paused}}, []string{"o/r"}); err != nil {
		t.Fatal(err)
	}
	result, err := w.ClearClones(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Removed) != len(leftovers) || result.Kept != 1 {
		t.Fatalf("clear = %+v, want every leftover removed and the paused clone kept", result)
	}
	for _, dir := range leftovers {
		if isDir(dir) {
			t.Fatalf("clear left %s", dir)
		}
	}
	if !isDir(paused) {
		t.Fatal("clear removed a paused job's clone")
	}
}

func TestClonesUnreadableWorkStateIsAnError(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(t.TempDir(), ".aviary.json")
	dir := makeClone(t, root, "r-gh-fix-1", "o/r")
	if err := os.WriteFile(statePath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := &Engine{Targets: []string{"o/r"}, StatePath: statePath, Out: os.Stderr}
	w.SetTempDirs([]string{root})
	if _, err := w.ListClones(context.Background()); !errors.Is(err, errWorkStateUnreadable) {
		t.Fatalf("list with unreadable work state = %v, want %v", err, errWorkStateUnreadable)
	}
	if _, err := w.ClearClones(context.Background()); !errors.Is(err, errWorkStateUnreadable) {
		t.Fatalf("clear with unreadable work state = %v, want %v", err, errWorkStateUnreadable)
	}
	if !isDir(dir) {
		t.Fatal("clear removed a clone without reading the work state")
	}
}

func TestClassifyClone(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	rec := cloneRecord{Path: filepath.Join(outside, "r-gh-fix-1"), Job: "o/r#1"}
	work := map[string]workState{"o/r#1": {Status: "completed"}}
	if verdict, reason := classifyClone(rec, cloneUsage{roots: []string{root}, work: work}); verdict != cloneKept || !strings.Contains(reason, "outside") {
		t.Fatalf("clone outside the temp directories = %d, %q; want kept", verdict, reason)
	}
	rec.Path = filepath.Join(root, "r-gh-fix-1")
	for _, status := range []string{"active", "waiting", "queued", "paused", "stopping"} {
		work := map[string]workState{"o/r#1": {Status: status}}
		if verdict, reason := classifyClone(rec, cloneUsage{roots: []string{root}, work: work}); verdict != cloneInUse || reason != "job is "+status {
			t.Fatalf("clone of a %s job = %d, %q; want in use", status, verdict, reason)
		}
	}
	rec.Adopted = true
	if verdict, reason := classifyClone(rec, cloneUsage{roots: []string{root}}); verdict != cloneUnused || !strings.Contains(reason, "not owned") {
		t.Fatalf("adopted clone no job records = %d, %q; want unused", verdict, reason)
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

// TestMeasureCloneSizesStaysResponsive lists many clones whose measurement
// never finishes alongside ones that do: the listing returns at the timeout
// with the fast clones' sizes and total, and the slow ones reported unknown.
func TestMeasureCloneSizesStaysResponsive(t *testing.T) {
	snapshot := core.ClonesSnapshot{}
	for i := range 200 {
		path := "fast"
		if i%2 == 1 {
			path = "slow"
		}
		snapshot.Clones = append(snapshot.Clones, core.CloneEntry{Path: path})
	}
	var calls atomic.Int32
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	measure := func(_ context.Context, path string) (int64, error) {
		calls.Add(1)
		if path == "slow" {
			// A hung disk that does not even honour ctx.
			<-release
		}
		return 10, nil
	}
	start := time.Now()
	measureCloneSizes(context.Background(), &snapshot, 200*time.Millisecond, measure)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("listing took %s, want about the 200ms timeout", elapsed)
	}
	if calls.Load() > cloneSizeWorkers*2 {
		t.Fatalf("measured %d clones at once, want at most %d workers busy", calls.Load(), cloneSizeWorkers)
	}
	var measured int
	for _, clone := range snapshot.Clones {
		if clone.Path == "slow" && clone.Size != nil {
			t.Fatalf("slow clone reported size %d, want unknown", *clone.Size)
		}
		if clone.Size != nil {
			measured++
		}
	}
	if snapshot.TotalSize != int64(measured)*10 || snapshot.Unmeasured != len(snapshot.Clones)-measured || snapshot.Unmeasured == 0 {
		t.Fatalf("total %d, %d unmeasured, %d measured", snapshot.TotalSize, snapshot.Unmeasured, measured)
	}
}

// TestMeasureCloneSizesMeasuresManyClones measures every clone when they
// all finish in time.
func TestMeasureCloneSizesMeasuresManyClones(t *testing.T) {
	root := t.TempDir()
	snapshot := core.ClonesSnapshot{}
	for i := range 40 {
		snapshot.Clones = append(snapshot.Clones, core.CloneEntry{Path: makeClone(t, root, "r-"+strconv.Itoa(i), "o/r")})
	}
	measureCloneSizes(context.Background(), &snapshot, time.Minute, dirSize)
	if snapshot.Unmeasured != 0 {
		t.Fatalf("%d clones unmeasured", snapshot.Unmeasured)
	}
	want, _ := dirSize(context.Background(), snapshot.Clones[0].Path)
	if want < 1000 || snapshot.TotalSize != want*40 {
		t.Fatalf("total %d, want 40 x %d", snapshot.TotalSize, want)
	}
}

// TestDirSizeStopsWhenCancelled reports an unknown size rather than a
// partial one when the walk is cut off.
func TestDirSizeStopsWhenCancelled(t *testing.T) {
	dir := makeClone(t, t.TempDir(), "r", "o/r")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := dirSize(ctx, dir); !errors.Is(err, context.Canceled) {
		t.Fatalf("dirSize after cancel err = %v", err)
	}
}

// A clone another job's checkout points at, or that a live agent for its issue
// has not announced yet, is in use even when its own job finished.
func TestClassifyCloneFindsOtherUsers(t *testing.T) {
	root := t.TempDir()
	rec := cloneRecord{Path: filepath.Join(root, "r-gh-fix-1"), Job: "o/r#1", Issue: 1}
	done := map[string]workState{"o/r#1": {Status: "completed"}}
	for _, c := range []struct {
		name  string
		usage cloneUsage
		want  string
	}{
		{"an unannounced agent for its issue", cloneUsage{live: []checkpointRun{{Target: "o/r", Number: 1}}}, "has not announced its checkout"},
		{"a running board job", cloneUsage{jobs: map[string]JobSnapshot{"o/r#1": {Status: "active"}}}, "job is active"},
		{"another job's checkout", cloneUsage{work: map[string]workState{"o/r#1": {Status: "completed"}, "o/r#2": {Status: "paused", CheckoutDirectory: rec.Path}}}, "job o/r#2 is paused"},
	} {
		t.Run(c.name, func(t *testing.T) {
			c.usage.roots = []string{root}
			if c.usage.work == nil {
				c.usage.work = done
			}
			if verdict, reason := classifyClone(rec, c.usage); verdict != cloneInUse || !strings.Contains(reason, c.want) {
				t.Fatalf("clone = %d, %q; want in use with %q", verdict, reason, c.want)
			}
		})
	}
}

// Clear reads the work state again before removing each clone, and keeps a
// clone when that second read fails.
func TestClearClonesKeepsCloneWhenWorkStateTurnsUnreadable(t *testing.T) {
	root := t.TempDir()
	dir := makeClone(t, root, "r-gh-fix-1", "o/r")
	reg, err := loadCloneRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.record(cloneRecord{Path: dir, Job: "o/r#1", Issue: 1}); err != nil {
		t.Fatal(err)
	}
	reads := 0
	result, err := clearClones(reg, func() cloneUsage {
		reads++
		if reads > 1 {
			return cloneUsage{roots: []string{root}, workErr: errors.New("state locked")}
		}
		return cloneUsage{roots: []string{root}, work: map[string]workState{"o/r#1": {Status: "completed"}}}
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kept != 1 || len(result.Removed) != 0 || len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "state locked") {
		t.Fatalf("clear = %+v, want the clone kept with the read error", result)
	}
	if !isDir(dir) {
		t.Fatal("clear removed a clone it could not classify")
	}
}

func TestCloneRegistryLoadAndRootErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clones.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCloneRegistry(path); err == nil {
		t.Fatal("loaded a corrupt clone registry")
	}
	var none *cloneRegistry
	if err := none.record(cloneRecord{Path: "/tmp/x"}); err != nil || none.list() != nil {
		t.Fatalf("nil registry record = %v, list = %v", err, none.list())
	}
	// A root whose parent is a file cannot be created.
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(file, "clones")
	if _, err := pickCloneRoot([]string{blocked}); !errors.Is(err, errNoCloneSpace) || !strings.Contains(err.Error(), blocked) {
		t.Fatalf("pick with an uncreatable root = %v, want errNoCloneSpace naming it", err)
	}
}
