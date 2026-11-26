package saferestart

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/store"
)

type stubParticipant struct {
	jobs []Job
	held []bool
}

func (s *stubParticipant) RunningJobs() []Job                       { return s.jobs }
func (s *stubParticipant) HoldNewJobs(hold bool)                    { s.held = append(s.held, hold) }
func (s *stubParticipant) Checkpoint(context.Context, Marker) error { s.jobs = nil; return nil }

func TestRegisterWhileHeldStartsHeld(t *testing.T) {
	t.Cleanup(func() { Hold(false); Unregister("late") })
	Hold(true)
	p := &stubParticipant{}
	Register("late", p)
	assert.Equal(t, []bool{true}, p.held)
	assert.True(t, Held())
}

func TestRunningJobsAndCheckpointCoverEveryParticipant(t *testing.T) {
	a := &stubParticipant{jobs: []Job{{Source: "a", ID: "1"}}}
	b := &stubParticipant{jobs: []Job{{Source: "b", ID: "2"}}}
	Register("a", a)
	Register("b", b)
	t.Cleanup(func() { Unregister("a"); Unregister("b") })

	assert.Equal(t, []Job{{Source: "a", ID: "1"}, {Source: "b", ID: "2"}}, RunningJobs())
	assert.NoError(t, Checkpoint(context.Background(), Marker{Reason: "restart"}))
	assert.Empty(t, RunningJobs())
}

func TestMarkerIsTakenOnce(t *testing.T) {
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir("") })

	_, ok := TakeMarker()
	assert.False(t, ok)
	assert.NoError(t, WriteMarker(Marker{Reason: "upgrade", FromVersion: "v1.2.3", ToVersion: "v1.3.0"}))
	m, ok := TakeMarker()
	assert.True(t, ok)
	assert.Contains(t, m.Message(), "from v1.2.3 to v1.3.0")
	_, ok = TakeMarker()
	assert.False(t, ok)
}

type orderedParticipant struct {
	stubParticipant
	name string
	log  *[]string
}

func (o *orderedParticipant) Checkpoint(context.Context, Marker) error {
	*o.log = append(*o.log, o.name)
	return nil
}

func TestCheckpointRunsInRegistrationOrder(t *testing.T) {
	// The task scheduler registers before the Code Loop engine that runs its
	// project tasks; it must checkpoint them first, although "projects" sorts
	// before "tasks" (issue #1021).
	var log []string
	Register("tasks", &orderedParticipant{name: "tasks", log: &log})
	Register("projects", &orderedParticipant{name: "projects", log: &log})
	Register("tasks", &orderedParticipant{name: "tasks", log: &log})
	t.Cleanup(func() { Unregister("tasks"); Unregister("projects") })

	assert.NoError(t, Checkpoint(context.Background(), Marker{Reason: "restart"}))
	assert.Equal(t, []string{"tasks", "projects"}, log, "re-registering keeps a participant's place")

	Unregister("tasks")
	Register("tasks", &orderedParticipant{name: "tasks", log: &log})
	log = nil
	assert.NoError(t, Checkpoint(context.Background(), Marker{Reason: "restart"}))
	assert.Equal(t, []string{"projects", "tasks"}, log)
}

type failingParticipant struct {
	stubParticipant
	err error
}

func (f *failingParticipant) Checkpoint(context.Context, Marker) error { return f.err }

func TestCheckpointJoinsErrorsAndKeepsGoing(t *testing.T) {
	errA := errors.New("a failed")
	errC := errors.New("c failed")
	b := &stubParticipant{jobs: []Job{{Source: "b", ID: "1"}}}
	Register("a", &failingParticipant{err: errA})
	Register("b", b)
	Register("c", &failingParticipant{err: errC})
	t.Cleanup(func() { Unregister("a"); Unregister("b"); Unregister("c") })

	err := Checkpoint(context.Background(), Marker{Reason: "restart"})
	require.Error(t, err)
	assert.ErrorIs(t, err, errA)
	assert.ErrorIs(t, err, errC)
	assert.Equal(t, "a failed\nc failed", err.Error())
	assert.Empty(t, b.jobs, "a failing participant does not stop later ones from checkpointing")
}

func TestCheckpointWithoutParticipantsSucceeds(t *testing.T) {
	assert.NoError(t, Checkpoint(context.Background(), Marker{Reason: "restart"}))
}

func TestHoldTogglesEveryParticipant(t *testing.T) {
	a := &stubParticipant{}
	b := &stubParticipant{}
	Register("a", a)
	Register("b", b)
	t.Cleanup(func() { Hold(false); Unregister("a"); Unregister("b") })

	Hold(true)
	assert.True(t, Held())
	Hold(false)
	assert.False(t, Held())
	assert.Equal(t, []bool{true, false}, a.held)
	assert.Equal(t, []bool{true, false}, b.held)
}

func TestMarkerMessage(t *testing.T) {
	upgrade := Marker{Reason: "upgrade", FromVersion: "v1.2.3", ToVersion: "v1.3.0"}
	assert.Equal(t,
		"Aviary restarted to upgrade from v1.2.3 to v1.3.0. Your previous run was stopped at a checkpoint; continue where you left off.",
		upgrade.Message())

	restart := Marker{Reason: "restart", FromVersion: "v1.2.3"}
	assert.Equal(t,
		"Aviary restarted. Your previous run was stopped at a checkpoint; continue where you left off.",
		restart.Message(), "a plain restart does not mention versions")
}

func TestUpdateSkillsRunsUpdatersInNameOrder(t *testing.T) {
	type call struct{ name, version string }
	var calls []call
	ctx := context.WithValue(context.Background(), ctxKey{}, "marker")
	updater := func(name string) SkillsUpdater {
		return func(got context.Context, version string) error {
			assert.Equal(t, "marker", got.Value(ctxKey{}), "the caller's context is passed through")
			calls = append(calls, call{name, version})
			return nil
		}
	}
	RegisterSkillsUpdater("gemini", updater("gemini"))
	RegisterSkillsUpdater("claude", updater("claude"))
	RegisterSkillsUpdater("codex", updater("codex"))
	t.Cleanup(func() {
		UnregisterSkillsUpdater("gemini")
		UnregisterSkillsUpdater("claude")
		UnregisterSkillsUpdater("codex")
	})

	require.NoError(t, UpdateSkills(ctx, "v2.0.0"))
	assert.Equal(t, []call{{"claude", "v2.0.0"}, {"codex", "v2.0.0"}, {"gemini", "v2.0.0"}}, calls)
}

type ctxKey struct{}

func TestUpdateSkillsJoinsErrorsWithUpdaterNames(t *testing.T) {
	errClaude := errors.New("token expired")
	errGemini := errors.New("disk full")
	var ran []string
	RegisterSkillsUpdater("claude", func(context.Context, string) error { ran = append(ran, "claude"); return errClaude })
	RegisterSkillsUpdater("codex", func(context.Context, string) error { ran = append(ran, "codex"); return nil })
	RegisterSkillsUpdater("gemini", func(context.Context, string) error { ran = append(ran, "gemini"); return errGemini })
	t.Cleanup(func() {
		UnregisterSkillsUpdater("claude")
		UnregisterSkillsUpdater("codex")
		UnregisterSkillsUpdater("gemini")
	})

	err := UpdateSkills(context.Background(), "v2.0.0")
	require.Error(t, err)
	assert.ErrorIs(t, err, errClaude)
	assert.ErrorIs(t, err, errGemini)
	assert.Equal(t, "updating claude skills: token expired\nupdating gemini skills: disk full", err.Error())
	assert.Equal(t, []string{"claude", "codex", "gemini"}, ran, "a failing updater does not stop the rest")
}

func TestUpdateSkillsSkipsUnregisteredUpdaters(t *testing.T) {
	var ran []string
	RegisterSkillsUpdater("claude", func(context.Context, string) error { ran = append(ran, "claude"); return nil })
	RegisterSkillsUpdater("codex", func(context.Context, string) error { ran = append(ran, "codex"); return errors.New("boom") })
	t.Cleanup(func() { UnregisterSkillsUpdater("claude"); UnregisterSkillsUpdater("codex") })

	UnregisterSkillsUpdater("codex")
	UnregisterSkillsUpdater("missing") // removing an unknown name is a no-op
	require.NoError(t, UpdateSkills(context.Background(), "v2.0.0"))
	assert.Equal(t, []string{"claude"}, ran)
}

func TestRegisterSkillsUpdaterReplacesEarlierOne(t *testing.T) {
	var ran []string
	RegisterSkillsUpdater("claude", func(context.Context, string) error { ran = append(ran, "old"); return nil })
	RegisterSkillsUpdater("claude", func(context.Context, string) error { ran = append(ran, "new"); return nil })
	t.Cleanup(func() { UnregisterSkillsUpdater("claude") })

	require.NoError(t, UpdateSkills(context.Background(), "v2.0.0"))
	assert.Equal(t, []string{"new"}, ran)
}

func TestUpdateSkillsWithoutUpdatersSucceeds(t *testing.T) {
	assert.NoError(t, UpdateSkills(context.Background(), "v2.0.0"))
}
