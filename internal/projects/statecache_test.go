package projects

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/projects/core"
)

// cacheTestManager is a test manager keeping its state, and so its Code Loop
// state cache, in dir, watching targets.
func cacheTestManager(t *testing.T, dir string, targets ...string) *Manager {
	t.Helper()
	m, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	m.SetEngineEnabled(false)
	m.Reconcile(ownedBy(config.ProjectsConfig{}, targets...))
	return m
}

// polledSnapshot is what an engine watching o/a and o/b publishes after a
// complete poll.
func polledSnapshot() core.Snapshot {
	return core.Snapshot{
		Targets:     []string{"o/a", "o/b"},
		IssueCounts: map[string]int{"o/a": 3, "o/b": 5},
		Completed:   4,
		Merged24h:   2,
		Concurrency: 2,
		LastPoll:    time.Now().Add(-time.Minute),
		Quotas:      map[string]string{"codex": "42% left"},
		Jobs: []JobSnapshot{
			{Target: "o/a", Number: 1, Title: "running in a", Status: "active", Kind: core.JobKindIssue, CheckoutDirectory: "/tmp/a"},
			{Target: "o/b", Number: 2, Title: "running in b", Status: "active", Kind: core.JobKindIssue, CheckoutDirectory: "/tmp/b"},
			{Target: "o/b", Number: 3, Title: "done in b", Status: "complete", Kind: core.JobKindIssue},
		},
	}
}

// warmCache leaves a cache in dir as a run watching o/a and o/b saves it.
func warmCache(t *testing.T, dir string) {
	t.Helper()
	m := cacheTestManager(t, dir, "o/a", "o/b")
	m.Snapshot(polledSnapshot())
	m.Shutdown()
	if _, err := os.Stat(filepath.Join(dir, "cache.json")); err != nil {
		t.Fatalf("no cache saved: %v", err)
	}
}

func jobNumbers(list map[string]any) []int {
	var numbers []int
	for _, job := range list["jobs"].([]map[string]any) {
		numbers = append(numbers, job["number"].(int))
	}
	slices.Sort(numbers)
	return numbers
}

func TestStateCacheColdStartServesNothingStale(t *testing.T) {
	m := cacheTestManager(t, t.TempDir(), "o/a")
	status := m.Status(t.Context(), 0, nil)
	if status["stale"] != true || !slices.Equal(status["loading_targets"].([]string), []string{"o/a"}) {
		t.Fatalf("a project with no data yet should show as loading: %v", status)
	}
	if _, ok := status["cached_at"]; ok {
		t.Fatalf("cold start reported a cache: %v", status)
	}
	if list := m.ListJobs(nil, "", nil, nil, "", 0, 0); list["total"] != 0 {
		t.Fatalf("cold start listed jobs: %v", list)
	}
}

func TestStateCacheWarmStartServesCachedState(t *testing.T) {
	dir := t.TempDir()
	warmCache(t, dir)
	m := cacheTestManager(t, dir, "o/a", "o/b")

	status := m.Status(t.Context(), 0, nil)
	if status["stale"] != true || status["cached_at"] == nil {
		t.Fatalf("cached status is not marked stale: %v", status)
	}
	if got := status["issue_counts"].(map[string]int); got["o/a"] != 3 || got["o/b"] != 5 {
		t.Fatalf("issue counts = %v", got)
	}
	if jobs := status["jobs"].(map[string]int); jobs["merged_24h"] != 2 || jobs["completed"] != 4 {
		t.Fatalf("job counts = %v", jobs)
	}
	if status["last_check"] == nil || status["quotas"].(map[string]string)["codex"] != "42% left" {
		t.Fatalf("last check or quota missing: %v", status)
	}
	if states := status["states"].(map[string]int); states[core.BoardRunning] != 2 || states[core.BoardDone] != 1 {
		t.Fatalf("board states = %v", states)
	}
	list := m.ListJobs(nil, "", nil, nil, "", 0, 0)
	if list["stale"] != true || !slices.Equal(jobNumbers(list), []int{1, 2, 3}) {
		t.Fatalf("cached jobs = %v", list)
	}
	for _, job := range list["jobs"].([]map[string]any) {
		if job["stale"] != true || job["actions"].(map[string]any)["stop"] != false {
			t.Fatalf("cached job offers actions: %v", job)
		}
	}
}

func TestStateCacheNeverDrivesActions(t *testing.T) {
	dir := t.TempDir()
	warmCache(t, dir)
	m := cacheTestManager(t, dir, "o/a", "o/b")
	if jobs := m.RunningJobs(); len(jobs) != 0 {
		t.Fatalf("a safe restart waits on cached jobs: %v", jobs)
	}
	if jobs := m.InFlightJobs("o/a"); len(jobs) != 0 {
		t.Fatalf("cached jobs reported in flight: %v", jobs)
	}
	if _, err := m.JobAction(t.Context(), "stop", "o/a", 1); err == nil {
		t.Fatal("a cached job was stopped")
	}
}

func TestStateCacheStaleClearsPerProjectAfterLivePoll(t *testing.T) {
	dir := t.TempDir()
	warmCache(t, dir)
	m := cacheTestManager(t, dir, "o/a", "o/b")

	// The engine recovered #1 as paused and has listed only o/a so far.
	m.Snapshot(core.Snapshot{
		Targets:     []string{"o/a", "o/b"},
		IssueCounts: map[string]int{"o/a": 7},
		Jobs:        []JobSnapshot{{Target: "o/a", Number: 1, Status: "paused", Kind: core.JobKindIssue}},
	})
	status := m.Status(t.Context(), 0, nil)
	if !slices.Equal(status["stale_targets"].([]string), []string{"o/b"}) {
		t.Fatalf("stale targets = %v", status["stale_targets"])
	}
	if status := m.Status(t.Context(), 0, []string{"o/a"}); status["stale"] != false {
		t.Fatalf("o/a is live but its board is stale: %v", status)
	}
	if got := status["issue_counts"].(map[string]int); got["o/a"] != 7 || got["o/b"] != 5 {
		t.Fatalf("issue counts = %v", got)
	}
	list := m.ListJobs(nil, "", nil, nil, "", 0, 0)
	if !slices.Equal(jobNumbers(list), []int{1, 2, 3}) {
		t.Fatalf("jobs = %v", list)
	}
	for _, job := range list["jobs"].([]map[string]any) {
		if job["number"] == 1 && (job["status"] != "paused" || job["stale"] != nil) {
			t.Fatalf("recovered job shows its cached state: %v", job)
		}
	}

	// The poll completes: nothing is stale and the cached jobs are gone.
	m.Snapshot(core.Snapshot{
		Targets:     []string{"o/a", "o/b"},
		IssueCounts: map[string]int{"o/a": 7, "o/b": 6},
		LastPoll:    time.Now(),
		Jobs:        []JobSnapshot{{Target: "o/a", Number: 1, Status: "paused", Kind: core.JobKindIssue}},
	})
	status = m.Status(t.Context(), 0, nil)
	if status["stale"] != false || len(status["stale_targets"].([]string)) != 0 || status["cached_at"] != nil {
		t.Fatalf("status still stale after a live poll: %v", status)
	}
	if list := m.ListJobs(nil, "", nil, nil, "", 0, 0); !slices.Equal(jobNumbers(list), []int{1}) || list["stale"] != false {
		t.Fatalf("jobs after a live poll = %v", list)
	}
}

func TestStateCacheIgnoresACorruptOrIncompatibleCache(t *testing.T) {
	for name, content := range map[string]string{
		"corrupt":      "{not json",
		"old version":  `{"version": 0, "cached_at": "2026-11-01T00:00:00Z", "snapshot": {"Targets": ["o/a"], "IssueCounts": {"o/a": 9}}}`,
		"next version": `{"version": 99, "cached_at": "2026-11-01T00:00:00Z", "snapshot": {"Targets": ["o/a"], "IssueCounts": {"o/a": 9}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "cache.json"), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			m := cacheTestManager(t, dir, "o/a")
			status := m.Status(t.Context(), 0, nil)
			if len(status["stale_targets"].([]string)) != 0 || status["cached_at"] != nil || len(status["issue_counts"].(map[string]int)) != 0 {
				t.Fatalf("an unusable cache was served: %v", status)
			}
			if !slices.Equal(status["loading_targets"].([]string), []string{"o/a"}) {
				t.Fatalf("loading targets = %v", status["loading_targets"])
			}
		})
	}
}

func TestStateCacheReconcilesConfigChanges(t *testing.T) {
	dir := t.TempDir()
	warmCache(t, dir)
	// o/b was removed from aviary.yaml and o/c added since the cache was saved.
	m := cacheTestManager(t, dir, "o/a", "o/c")
	status := m.Status(t.Context(), 0, nil)
	if !slices.Equal(status["targets"].([]string), []string{"o/a", "o/c"}) {
		t.Fatalf("targets = %v", status["targets"])
	}
	if !slices.Equal(status["stale_targets"].([]string), []string{"o/a"}) || !slices.Equal(status["loading_targets"].([]string), []string{"o/c"}) {
		t.Fatalf("stale %v, loading %v", status["stale_targets"], status["loading_targets"])
	}
	if got := status["issue_counts"].(map[string]int); got["o/b"] != 0 {
		t.Fatalf("a removed project's count is shown: %v", got)
	}
	if list := m.ListJobs(nil, "", nil, nil, "", 0, 0); !slices.Equal(jobNumbers(list), []int{1}) {
		t.Fatalf("a removed project's jobs are shown: %v", list)
	}
}

func TestStateCacheKeepsOnlyCompletePolls(t *testing.T) {
	dir := t.TempDir()
	warmCache(t, dir)
	m := cacheTestManager(t, dir, "o/a", "o/b")
	// A snapshot from before the first poll must not replace the cache.
	m.Snapshot(core.Snapshot{Targets: []string{"o/a", "o/b"}, IssueCounts: map[string]int{"o/a": 1}})
	m.Shutdown()
	restarted := cacheTestManager(t, dir, "o/a", "o/b")
	if got := restarted.Status(t.Context(), 0, nil)["issue_counts"].(map[string]int); got["o/a"] != 3 || got["o/b"] != 5 {
		t.Fatalf("a partial snapshot replaced the cache: %v", got)
	}
}
