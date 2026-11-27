package projects

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/projects/core"
)

// polledJobs is a snapshot after a complete poll of o/a and o/b with jobs.
func polledJobs(jobs ...JobSnapshot) core.Snapshot {
	return core.Snapshot{Targets: []string{"o/a", "o/b"}, IssueCounts: map[string]int{"o/a": 1, "o/b": 1}, LastPoll: time.Now(), Jobs: jobs}
}

// restartManager shuts m down, saving its job store, and starts a manager on
// the same state directory watching the same projects.
func restartManager(t *testing.T, m *Manager, cfg *config.Config) *Manager {
	t.Helper()
	m.Shutdown()
	restarted, err := NewManager(m.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	restarted.SetEngineEnabled(false)
	restarted.Reconcile(cfg)
	return restarted
}

func TestJobStoreKeepsHistoryAcrossRestarts(t *testing.T) {
	cfg := ownedBy(config.ProjectsConfig{}, "o/a", "o/b")
	m := cacheTestManager(t, t.TempDir(), "o/a", "o/b")
	started := time.Now().Add(-time.Hour)
	m.Snapshot(polledJobs(
		JobSnapshot{Target: "o/a", Number: 1, Status: "complete", Kind: core.JobKindIssue, Started: started, Log: "merged\n", PullRequest: 9, PullRequestState: core.PullRequestMerged, Agent: "codex", Model: "gpt"},
		JobSnapshot{Target: "o/b", Number: 2, Status: "failed", Kind: core.JobKindIssue, Started: started, Log: "boom\n"},
		JobSnapshot{Target: "o/b", Number: 4, Status: "paused", Kind: core.JobKindIssue, Started: started},
		JobSnapshot{Target: "o/a", Number: 5, Status: "queued", Kind: core.JobKindMerge, Started: started},
	))
	m = restartManager(t, m, cfg)

	// Before the first poll every stored job shows, stale.
	list := m.ListJobs(nil, "", nil, nil, "", 0, 0)
	if !slices.Equal(jobNumbers(list), []int{1, 2, 4, 5}) || list["stale"] != true {
		t.Fatalf("stored jobs after a restart = %v", list)
	}
	detail, err := m.GetJob("o/a", 1, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if detail["stale"] != true || detail["scrollback"] != "merged" || detail["pull_request"] != 9 {
		t.Fatalf("stored job detail = %v", detail)
	}

	// The engine recovers #4 without its start time or scrollback, and its
	// first poll no longer reports the queued merge #5.
	m.Snapshot(polledJobs(JobSnapshot{Target: "o/b", Number: 4, Status: "paused", Kind: core.JobKindIssue, WaitReason: "blocked"}))
	list = m.ListJobs(nil, "", nil, nil, "", 0, 0)
	if !slices.Equal(jobNumbers(list), []int{1, 2, 4}) || list["stale"] != false {
		t.Fatalf("jobs after the first poll = %v", list)
	}
	if list["total"] != 3 {
		t.Fatalf("a recovered job was duplicated: %v", list)
	}
	m.Shutdown()
	saved := newJobStore(filepath.Join(m.stateDir, "jobs.json"))
	if got := saved.jobs["o/b#4"]; !got.Job.Started.Equal(started) || got.Job.WaitReason != "blocked" {
		t.Fatalf("a recovered job was not updated in place: %+v", got)
	}
	if _, ok := saved.jobs["o/a#5"]; ok {
		t.Fatal("a queued job the poll no longer reports was kept")
	}
}

func TestJobStoreNeverActsOnStoredJobs(t *testing.T) {
	cfg := ownedBy(config.ProjectsConfig{}, "o/a")
	m := cacheTestManager(t, t.TempDir(), "o/a")
	m.Snapshot(polledJobs(JobSnapshot{Target: "o/a", Number: 1, Status: "active", Kind: core.JobKindIssue, CheckoutDirectory: "/tmp/a", Started: time.Now()}))
	m = restartManager(t, m, cfg)
	if jobs := m.RunningJobs(); len(jobs) != 0 {
		t.Fatalf("a safe restart waits on stored jobs: %v", jobs)
	}
	if _, err := m.JobAction(t.Context(), "retry", "o/a", 1); err == nil {
		t.Fatal("a stored job was retried")
	}
	if _, err := m.ClearJobs(nil, "", "o/a", 1); err == nil {
		t.Fatal("a running job was cleared")
	}
}

func TestClearJobs(t *testing.T) {
	cfg := ownedBy(config.ProjectsConfig{}, "o/a", "o/b")
	m := cacheTestManager(t, t.TempDir(), "o/a", "o/b")
	started := time.Now().Add(-time.Hour)
	jobs := []JobSnapshot{
		{Target: "o/a", Number: 1, Status: "complete", Kind: core.JobKindIssue, Started: started},
		{Target: "o/a", Number: 2, Status: "failed", Kind: core.JobKindIssue, Started: started},
		{Target: "o/b", Number: 3, Status: "complete", Kind: core.JobKindMerge, Started: started},
		{Target: "o/b", Number: 4, Status: "failed", Kind: core.JobKindIssue, Started: started},
		{Target: "o/b", Number: 5, Status: "failed", Kind: core.JobKindIssue, Started: started, Retrying: true},
		{Target: "o/a", Number: 6, Status: "active", Kind: core.JobKindIssue, Started: started},
		{Target: "o/a", Number: 7, Status: "failed", Kind: core.JobKindSchedule, ScheduleID: "coder/nightly", Started: started},
	}
	m.Snapshot(polledJobs(jobs...))
	transcript := filepath.Join(m.stateDir, "jobs", transcriptDirName("o/a#1"))
	m.transcripts.For("o/a#1").System("hello")
	if _, err := os.Stat(transcript); err != nil {
		t.Fatalf("no transcript to clear: %v", err)
	}

	for _, refused := range []int{5, 6, 7} {
		if _, err := m.ClearJobs(nil, "", "", refused); err == nil {
			t.Fatalf("#%d was cleared", refused)
		}
	}
	result, err := m.ClearJobs(nil, "", "o/a", 1)
	if err != nil || result["count"] != 1 {
		t.Fatalf("clearing one job = %v, %v", result, err)
	}
	if _, err := os.Stat(transcript); !os.IsNotExist(err) {
		t.Fatalf("a cleared job's transcript is still there: %v", err)
	}
	result, err = m.ClearJobs([]string{"o/b"}, ClearFailed, "", 0)
	if err != nil || !slices.Equal(result["cleared"].([]string), []string{"o/b#4"}) {
		t.Fatalf("clearing o/b's failed jobs = %v, %v", result, err)
	}
	if _, err := m.ClearJobs(nil, "bogus", "", 0); err == nil {
		t.Fatal("an unknown kind of job was cleared")
	}
	// The engine still holds the cleared runs, and a poll reports them again.
	m.Snapshot(polledJobs(jobs...))
	if list := m.ListJobs(nil, "", nil, nil, "", 0, 0); !slices.Equal(jobNumbers(list), []int{2, 3, 5, 6, 7}) {
		t.Fatalf("jobs after clearing = %v", list)
	}
	result, err = m.ClearJobs(nil, ClearFinished, "", 0)
	if err != nil || !slices.Equal(result["cleared"].([]string), []string{"o/a#2", "o/b#3"}) {
		t.Fatalf("clearing every finished job = %v, %v", result, err)
	}

	// A restart does not bring the cleared runs back, while a later run of
	// the same issue shows.
	m = restartManager(t, m, cfg)
	rerun := jobs[0]
	rerun.Started, rerun.Status = time.Now(), "active"
	m.Snapshot(polledJobs(append(slices.Clone(jobs[1:]), rerun)...))
	if list := m.ListJobs(nil, "", nil, nil, "", 0, 0); !slices.Equal(jobNumbers(list), []int{1, 5, 6, 7}) {
		t.Fatalf("jobs after a restart = %v", list)
	}
}

func TestJobStoreExpiresFinishedJobs(t *testing.T) {
	dir := t.TempDir()
	cfg := ownedBy(config.ProjectsConfig{}, "o/a", "o/b")
	cfg.Agents[0].Projects[1].FinishedTTL = "1d"
	m, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	m.SetEngineEnabled(false)
	m.Reconcile(cfg)
	now := time.Now()
	m.jobs.now = func() time.Time { return now }
	// The test sweeps itself rather than on the first snapshot.
	m.lastSweep.Store(now.UnixNano())
	old := now.Add(-3 * 24 * time.Hour)
	ancient := now.Add(-15 * 24 * time.Hour)
	m.Snapshot(polledJobs(
		JobSnapshot{Target: "o/a", Number: 1, Status: "complete", Kind: core.JobKindIssue, Finished: old},
		JobSnapshot{Target: "o/a", Number: 2, Status: "failed", Kind: core.JobKindIssue, Finished: ancient},
		JobSnapshot{Target: "o/b", Number: 3, Status: "complete", Kind: core.JobKindIssue, Finished: old},
		JobSnapshot{Target: "o/b", Number: 4, Status: "paused", Kind: core.JobKindIssue, Started: ancient},
	))
	// o/a inherits the two-week default and o/b keeps a day.
	if list := m.ListJobs(nil, "", nil, nil, "", 0, 0); !slices.Equal(jobNumbers(list), []int{1, 4}) {
		t.Fatalf("jobs past their finished_ttl are shown: %v", list)
	}
	removed := m.jobs.sweep(m.finishedTTL())
	slices.Sort(removed)
	if !slices.Equal(removed, []string{"o/a#2", "o/b#3"}) {
		t.Fatalf("swept %v", removed)
	}
	status := m.Status(t.Context(), 0, nil)
	if got := status["finished_ttls"].(map[string]string); got["o/a"] != "2w" || got["o/b"] != "1d" || status["default_finished_ttl"] != "2w" {
		t.Fatalf("finished_ttls = %v", status["finished_ttls"])
	}
	if got := status["finished_ttl_sources"].(map[string]string); got["o/a"] != config.ProjectModeSourceGlobal || got["o/b"] != config.ProjectModeSourceProject {
		t.Fatalf("finished_ttl_sources = %v", got)
	}

	// Zero keeps finished jobs until they are cleared.
	cfg.Projects.FinishedTTL = "0"
	cfg.Agents[0].Projects[1].FinishedTTL = ""
	m.Reconcile(cfg)
	m.Snapshot(polledJobs(JobSnapshot{Target: "o/a", Number: 8, Status: "complete", Kind: core.JobKindIssue, Finished: ancient}))
	if list := m.ListJobs(nil, "", nil, nil, "", 0, 0); !slices.Contains(jobNumbers(list), 8) {
		t.Fatalf("a finished job expired with finished_ttl 0: %v", list)
	}
}

func TestJobStoreCapsScrollback(t *testing.T) {
	m := cacheTestManager(t, t.TempDir(), "o/a")
	long := strings.Repeat("0123456789abcdef\n", 4*jobStoreLogBytes/17)
	m.Snapshot(polledJobs(JobSnapshot{Target: "o/a", Number: 1, Status: "complete", Kind: core.JobKindIssue, Log: long + "last line\n"}))
	m.jobs.mu.Lock()
	stored := m.jobs.jobs["o/a#1"].Job.Log
	m.jobs.mu.Unlock()
	if len(stored) > jobStoreLogBytes || !strings.HasSuffix(stored, "last line\n") || !strings.HasPrefix(stored, "0123") {
		t.Fatalf("stored scrollback is %d bytes, ending %q", len(stored), stored[max(0, len(stored)-20):])
	}
}

func TestSetWatchFinishedTTL(t *testing.T) {
	m := newTestManager(t)
	if err := m.AddWatch(Watch{Targets: []string{"o/r"}}); err != nil {
		t.Fatal(err)
	}
	if ok, err := m.SetWatchFinishedTTL("o/r", "3d"); !ok || err != nil {
		t.Fatalf("SetWatchFinishedTTL = %v, %v", ok, err)
	}
	if ttl, source := m.ProjectFinishedTTL("o/r"); ttl != "3d" || source != config.ProjectModeSourceProject {
		t.Fatalf("finished_ttl = %s from %s", ttl, source)
	}
	if _, err := m.SetWatchFinishedTTL("o/r", "-1h"); err == nil {
		t.Fatal("a negative finished_ttl was accepted")
	}
	if ok, _ := m.SetWatchFinishedTTL("o/r", "inherit"); !ok {
		t.Fatal("inherit was refused")
	}
	if ttl, source := m.ProjectFinishedTTL("o/r"); ttl != "2w" || source != config.ProjectModeSourceGlobal {
		t.Fatalf("finished_ttl after inherit = %s from %s", ttl, source)
	}
	if ok, _ := m.SetWatchFinishedTTL("o/missing", "1d"); ok {
		t.Fatal("a project without a runtime watch reported one")
	}
}
