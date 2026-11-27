package projects

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/lsegal/aviary/internal/store"
)

// TargetData counts what removing a project with its data deletes: its job
// records in the work state, its job transcripts and the clones recorded for
// its jobs.
type TargetData struct {
	Jobs        int `json:"jobs"`
	Transcripts int `json:"transcripts"`
	Clones      int `json:"clones"`
}

// TargetData counts target's job records, transcripts and clones.
func (m *Manager) TargetData(target string) (TargetData, error) {
	m.mu.Lock()
	settings, _ := m.settingsLocked()
	m.mu.Unlock()
	var counts TargetData
	work, err := loadScopedWorkState(m.statePath(), settings.Targets)
	if err != nil {
		return counts, err
	}
	for key := range work {
		if isTargetJob(key, target) {
			counts.Jobs++
		}
	}
	counts.Transcripts = len(m.transcripts.dirsOf(target))
	reg, err := m.cloneRegistryForTarget()
	if err != nil {
		return counts, err
	}
	for _, rec := range reg.list() {
		if cloneOfTarget(rec, target) && isDir(rec.Path) {
			counts.Clones++
		}
	}
	return counts, nil
}

// PurgeTargetInBackground deletes target's job records, transcripts and
// unused clones once the engine has stopped target's jobs. The caller has
// already removed target from the watched projects.
func (m *Manager) PurgeTargetInBackground(target string) {
	go func() {
		if err := m.purgeTarget(target); err != nil {
			m.logf("could not delete %s's data: %v", target, err)
		}
	}()
}

// purgeTarget waits for the restart that drops target, since the engine
// that still runs target's jobs saves their records as it stops them, then
// deletes target's data with restarts held off. The work state goes first,
// so a clone its jobs recorded no longer counts as in use; a clone a job of
// the running engine still holds or is being handed is never removed.
func (m *Manager) purgeTarget(target string) error {
	_ = m.refresh()
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	m.mu.Lock()
	settings, err := m.settingsLocked()
	m.mu.Unlock()
	if err != nil {
		return err
	}
	if slices.Contains(settings.Targets, target) {
		return fmt.Errorf("%s is watched again", target)
	}
	engine := m.engine()
	if engine != nil && slices.Contains(engine.Targets, target) {
		return fmt.Errorf("the engine still runs %s's jobs", target)
	}
	jobs, err := purgeTargetWorkState(m.statePath(), target)
	if err != nil {
		return err
	}
	transcripts := m.transcripts.purge(target)
	m.jobs.purgeTarget(target)
	if engine == nil {
		engine = &Engine{StatePath: m.statePath()}
		engine.SetTempDirs(settings.TempDirs)
	}
	reg, err := engine.cloneRegistry()
	if err != nil {
		return err
	}
	clones := engine.removeUnusedClones(reg, func(rec cloneRecord) bool { return cloneOfTarget(rec, target) })
	m.logf("deleted %s's data: %d job record(s), %d transcript(s), %d clone(s)", target, jobs, transcripts, len(clones.Removed))
	return nil
}

// cloneRegistryForTarget is the running engine's clone registry, which
// holds records it has not saved yet, or the saved one when none runs.
func (m *Manager) cloneRegistryForTarget() (*cloneRegistry, error) {
	if engine := m.engine(); engine != nil {
		return engine.cloneRegistry()
	}
	return loadCloneRegistry(clonesPathFor(m.statePath()))
}

// isTargetJob reports whether the work state key names one of target's
// jobs.
func isTargetJob(key, target string) bool {
	number, ok := strings.CutPrefix(key, target+"#")
	if !ok {
		return false
	}
	_, err := strconv.Atoi(number)
	return err == nil
}

// cloneOfTarget reports whether rec was cloned for one of target's jobs.
func cloneOfTarget(rec cloneRecord, target string) bool {
	return isTargetJob(rec.Job, target) || (rec.Job == "" && rec.Repository == target)
}

// purgeTargetWorkState removes target's job records from the work state at
// path, leaving every other record as it is.
func purgeTargetWorkState(path, target string) (int, error) {
	if path == "" {
		return 0, nil
	}
	workStateMu.Lock()
	defer workStateMu.Unlock()
	raw, err := store.ReadJSON[map[string]json.RawMessage](path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	removed := 0
	for key := range raw {
		if workKeyTarget(key) == target {
			delete(raw, key)
			removed++
		}
	}
	if removed == 0 {
		return 0, nil
	}
	return removed, store.WriteJSON(path, raw)
}

// dirsOf lists the stored transcript directories of target's jobs.
func (s *transcriptStore) dirsOf(target string) []string {
	if s == nil || s.dir == "" {
		return nil
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}
	prefix := transcriptDirName(target+"#") + "_"
	var dirs []string
	for _, entry := range entries {
		number, ok := strings.CutPrefix(entry.Name(), prefix)
		if !ok || !entry.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(number); err == nil {
			dirs = append(dirs, filepath.Join(s.dir, entry.Name()))
		}
	}
	return dirs
}

// purge forgets target's open transcripts and deletes the stored ones,
// returning how many it deleted.
func (s *transcriptStore) purge(target string) int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	for key := range s.logs {
		if isTargetJob(key, target) {
			delete(s.logs, key)
		}
	}
	s.mu.Unlock()
	removed := 0
	for _, dir := range s.dirsOf(target) {
		if os.RemoveAll(dir) == nil {
			removed++
		}
	}
	return removed
}
