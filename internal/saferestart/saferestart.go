// Package saferestart coordinates restarts and upgrades with the subsystems
// that run long jobs, so a restart stops those jobs at a checkpoint, saves
// their sessions, and lets each one resume afterwards.
//
// A subsystem that runs jobs registers a Participant. The server asks every
// participant which jobs a restart would stop, holds new jobs while a restart
// waits for running ones to finish, and runs the shutdown checkpoint right
// before restarting. After the restart, a participant calls TakeMarker to learn
// that Aviary restarted, and why, before resuming each job in its own session.
package saferestart

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/store"
)

// Job is a running job that a restart would stop.
type Job struct {
	Source string `json:"source"`
	ID     string `json:"id"`
	Title  string `json:"title"`
}

// Participant is a subsystem that runs jobs a restart has to stop.
type Participant interface {
	// RunningJobs lists the jobs that are running now. Waiting jobs are not
	// listed: a restart leaves them alone.
	RunningJobs() []Job
	// HoldNewJobs stops (true) or allows (false) starting new jobs.
	HoldNewJobs(hold bool)
	// Checkpoint stops every running job and saves its session so it can be
	// resumed in that same session after the restart.
	Checkpoint(ctx context.Context, marker Marker) error
}

// SkillsUpdater refreshes installed skills, such as the skills of every
// signed-in CLI provider, to match version, the release an upgrade installs.
type SkillsUpdater func(ctx context.Context, version string) error

// Marker records why Aviary restarted. It is written right before the restart
// and read once by participants afterwards.
type Marker struct {
	Reason      string    `json:"reason"`
	FromVersion string    `json:"from_version,omitempty"`
	ToVersion   string    `json:"to_version,omitempty"`
	At          time.Time `json:"at"`
	Jobs        []Job     `json:"jobs,omitempty"`
}

// Message is the notice a resumed job is given.
func (m Marker) Message() string {
	if m.ToVersion != "" {
		return fmt.Sprintf("Aviary restarted to upgrade from %s to %s. Your previous run was stopped at a checkpoint; continue where you left off.", m.FromVersion, m.ToVersion)
	}
	return "Aviary restarted. Your previous run was stopped at a checkpoint; continue where you left off."
}

var (
	mu           sync.Mutex
	participants = map[string]Participant{}
	// order is the participants' names in registration order, the order
	// Checkpoint stops them in.
	order         []string
	skillUpdaters = map[string]SkillsUpdater{}
	held          bool
)

// Register adds a participant under name, replacing any earlier one. A
// participant registered while new jobs are held starts out held.
// Participants are checkpointed in the order they were first registered, so a
// participant whose jobs run on another's engine registers before it: the
// task scheduler checkpoints the project tasks it hands to the Code Loop
// engine before that engine stops, so they resume after the restart instead
// of being canceled (issue #1021).
func Register(name string, p Participant) {
	mu.Lock()
	if _, ok := participants[name]; !ok {
		order = append(order, name)
	}
	participants[name] = p
	hold := held
	mu.Unlock()
	if hold {
		p.HoldNewJobs(true)
	}
}

// Unregister removes the participant registered under name.
func Unregister(name string) {
	mu.Lock()
	delete(participants, name)
	order = slices.DeleteFunc(order, func(n string) bool { return n == name })
	mu.Unlock()
}

// RegisterSkillsUpdater adds a skills refresh that every upgrade runs.
func RegisterSkillsUpdater(name string, fn SkillsUpdater) {
	mu.Lock()
	skillUpdaters[name] = fn
	mu.Unlock()
}

// UnregisterSkillsUpdater removes the skills refresh registered under name.
func UnregisterSkillsUpdater(name string) {
	mu.Lock()
	delete(skillUpdaters, name)
	mu.Unlock()
}

func snapshot() []Participant {
	mu.Lock()
	defer mu.Unlock()
	out := make([]Participant, 0, len(order))
	for _, name := range order {
		out = append(out, participants[name])
	}
	return out
}

// RunningJobs lists every participant's running jobs.
func RunningJobs() []Job {
	jobs := []Job{}
	for _, p := range snapshot() {
		jobs = append(jobs, p.RunningJobs()...)
	}
	return jobs
}

// Hold stops (true) or allows (false) new jobs in every participant.
func Hold(hold bool) {
	mu.Lock()
	held = hold
	mu.Unlock()
	for _, p := range snapshot() {
		p.HoldNewJobs(hold)
	}
}

// Held reports whether new jobs are held.
func Held() bool {
	mu.Lock()
	defer mu.Unlock()
	return held
}

// UpdateSkills runs every registered skills refresh for version and joins
// their errors.
func UpdateSkills(ctx context.Context, version string) error {
	mu.Lock()
	names := make([]string, 0, len(skillUpdaters))
	for name := range skillUpdaters {
		names = append(names, name)
	}
	sort.Strings(names)
	fns := make([]SkillsUpdater, 0, len(names))
	for _, name := range names {
		fns = append(fns, skillUpdaters[name])
	}
	mu.Unlock()
	var errs []error
	for i, fn := range fns {
		if err := fn(ctx, version); err != nil {
			errs = append(errs, fmt.Errorf("updating %s skills: %w", names[i], err))
		}
	}
	return errors.Join(errs...)
}

// Checkpoint runs every participant's shutdown checkpoint, in registration
// order, and joins errors.
func Checkpoint(ctx context.Context, marker Marker) error {
	var errs []error
	for _, p := range snapshot() {
		if err := p.Checkpoint(ctx, marker); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// MarkerPath is where the restart marker is stored.
func MarkerPath() string {
	return filepath.Join(store.DataDir(), "restart.json")
}

// WriteMarker saves the restart marker.
func WriteMarker(marker Marker) error {
	return store.WriteJSON(MarkerPath(), marker)
}

// TakeMarker returns and removes the marker of the last safe restart. ok is
// false when Aviary did not restart through a safe restart.
func TakeMarker() (marker Marker, ok bool) {
	marker, err := store.ReadJSON[Marker](MarkerPath())
	if err != nil {
		return Marker{}, false
	}
	_ = store.DeleteJSON(MarkerPath())
	return marker, true
}

// When a restart happens.
const (
	// WhenNow stops running jobs at a checkpoint and restarts right away.
	WhenNow = "now"
	// WhenIdle holds new jobs and restarts once no jobs are running.
	WhenIdle = "idle"
)

// Restart phases reported in Status.
const (
	PhaseIdle     = "idle"     // no restart requested
	PhaseWaiting  = "waiting"  // holding new jobs until running jobs finish
	PhaseDraining = "draining" // waiting for in-flight MCP calls to finish
	PhaseUpdating = "updating" // installing, checkpointing and restarting
)

// Request asks the server for a safe restart, optionally upgrading first.
type Request struct {
	When    string `json:"when,omitempty"`
	Upgrade bool   `json:"upgrade,omitempty"`
	Version string `json:"version,omitempty"`
	// Release installs a release even on a dev build that would otherwise
	// update its source checkout with git.
	Release bool `json:"release,omitempty"`
	// Cancel withdraws a restart that is still waiting for jobs to finish.
	Cancel bool `json:"cancel,omitempty"`
	// Force restarts without waiting for in-flight MCP calls to finish.
	Force bool `json:"force,omitempty"`
	// RemoveStaleLock first removes a stale .git/index.lock that blocks a
	// git-mode update. A lock young enough to belong to a running git
	// command is left alone.
	RemoveStaleLock bool `json:"removeStaleLock,omitempty"`
}

// Status describes the current restart state and the jobs a restart would stop.
type Status struct {
	Phase   string `json:"phase"`
	When    string `json:"when,omitempty"`
	Upgrade bool   `json:"upgrade,omitempty"`
	Version string `json:"version,omitempty"`
	Force   bool   `json:"force,omitempty"`
	// MCPCalls is how many MCP calls a draining restart is waiting on.
	MCPCalls int    `json:"mcpCalls,omitempty"`
	Jobs     []Job  `json:"jobs"`
	Held     bool   `json:"held"`
	Noop     bool   `json:"noop,omitempty"`
	Emulated bool   `json:"emulated,omitempty"`
	Message  string `json:"message,omitempty"`
	Error    string `json:"error,omitempty"`
	// AutoUpdate is set while the self-updater has an update it means to
	// apply on its own and has not yet: why it is waiting, or why its last
	// attempt failed.
	AutoUpdate *AutoUpdate `json:"autoUpdate,omitempty"`
}

// Auto update states reported in AutoUpdate.
const (
	AutoWaitingRestart = "waiting-restart" // waiting for a restart in progress
	AutoApplying       = "applying"        // applying the update now
)

// AutoUpdate describes an automatic update that has not applied yet.
type AutoUpdate struct {
	Version string `json:"version,omitempty"`
	// State is one of the Auto* constants, or "" when the update is not
	// waiting on anything, e.g. after a failed attempt.
	State string `json:"state,omitempty"`
	// Error is why the last attempt to apply the update failed.
	Error string `json:"error,omitempty"`
	// Code classifies Error as one of update's ErrCode* constants, such as
	// git-busy when another git command held the checkout's lock.
	Code string `json:"code,omitempty"`
}
