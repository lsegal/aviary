package scheduler

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

// maxRunHistory caps how many fires each task's run history keeps.
const maxRunHistory = 20

// taskState is the persisted scheduling state of one task.
type taskState struct {
	// LastFire is the scheduled time of the task's most recent fire.
	LastFire time.Time `json:"last_fire,omitzero"`
	// Baseline is when the scheduler last settled the task's missed fires,
	// or first saw the task. Fires due before it are never caught up.
	Baseline time.Time        `json:"baseline,omitzero"`
	History  []domain.TaskRun `json:"history,omitempty"`
}

// since returns the time from which missed fires are counted.
func (t *taskState) since() time.Time {
	if t.LastFire.After(t.Baseline) {
		return t.LastFire
	}
	return t.Baseline
}

// stateStore persists every task's scheduling state in one JSON file so the
// last fire and run history survive restarts.
type stateStore struct {
	mu    sync.Mutex
	path  string
	tasks map[string]*taskState
}

func scheduleStatePath() string {
	return filepath.Join(store.SubDir("scheduler"), "schedules.json")
}

func loadStateStore(path string) *stateStore {
	tasks, err := store.ReadJSON[map[string]*taskState](path)
	if err != nil && !os.IsNotExist(err) && !strings.Contains(err.Error(), "no such file") {
		slog.Warn("scheduler: reading schedule state", "path", path, "err", err)
	}
	if tasks == nil {
		tasks = map[string]*taskState{}
	}
	return &stateStore{path: path, tasks: tasks}
}

// update applies fn to key's state, creating it when missing, and saves.
func (s *stateStore) update(key string, fn func(*taskState)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.tasks[key]
	if st == nil {
		st = &taskState{}
		s.tasks[key] = st
	}
	fn(st)
	s.saveLocked()
}

// get returns a copy of key's state.
func (s *stateStore) get(key string) taskState {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.tasks[key]
	if st == nil {
		return taskState{}
	}
	out := *st
	out.History = append([]domain.TaskRun(nil), st.History...)
	return out
}

func (s *stateStore) remove(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[key]; ok {
		delete(s.tasks, key)
		s.saveLocked()
	}
}

func (s *stateStore) saveLocked() {
	if err := store.WriteJSON(s.path, s.tasks); err != nil {
		slog.Warn("scheduler: saving schedule state", "path", s.path, "err", err)
	}
}

// addRun appends run to st's history, dropping the oldest entries beyond
// maxRunHistory.
func (t *taskState) addRun(run domain.TaskRun) {
	t.History = append(t.History, run)
	if n := len(t.History) - maxRunHistory; n > 0 {
		t.History = append([]domain.TaskRun(nil), t.History[n:]...)
	}
}

// finishRun records the outcome of jobID's latest attempt.
func (s *stateStore) finishRun(key, jobID string, status domain.TaskRunStatus, summary string, duration time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.tasks[key]
	if st == nil {
		return
	}
	for i := len(st.History) - 1; i >= 0; i-- {
		if st.History[i].JobID != jobID {
			continue
		}
		st.History[i].Status = status
		st.History[i].Summary = summary
		st.History[i].DurationMS = duration.Milliseconds()
		s.saveLocked()
		return
	}
}

// summarize shortens job output to the first non-empty line, capped for
// display in run history.
func summarize(output string) string {
	const limit = 200
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if r := []rune(line); len(r) > limit {
			return string(r[:limit-1]) + "…"
		}
		return line
	}
	return ""
}
