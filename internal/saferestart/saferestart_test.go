package saferestart

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

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
