package notify

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/store"
)

// releaseFailure is one run of a scheduled task failing as it does every
// five minutes, with the run's own ID and timing in the error.
func releaseFailure(run int, errText string) Event {
	return Event{
		Type:      TypeCodeLoopFailed,
		Severity:  SeverityError,
		Title:     "Failed: Scheduled prompt \"Aviary/aviary-release\"",
		Body:      fmt.Sprintf("%s (run %d, job 9f%05dab, took %ds)", errText, run, run, run+3),
		Link:      "/agents/Aviary/projects/lsegal%2Faviary",
		Source:    "Scheduled prompt \"Aviary/aviary-release\"",
		PauseTask: "Aviary/aviary-release",
	}
}

func TestEmit_RepeatedFailuresMergeIntoOneEntryDeliveredOnce(t *testing.T) {
	ts := newTestService()
	ts.settings.Server = true
	ts.settings.Repeats.Streak = false

	first, outcome := ts.Emit(releaseFailure(1, "release failed: tag exists"))
	require.Equal(t, Delivered, outcome)
	for run := 2; run <= 7; run++ {
		ts.now = ts.now.Add(5 * time.Minute)
		n, outcome := ts.Emit(releaseFailure(run, "release failed: tag exists"))
		require.Equal(t, Merged, outcome, "run %d", run)
		assert.Equal(t, first.ID, n.ID)
		assert.Equal(t, run, n.Count)
	}

	history, _ := ts.History()
	require.Len(t, history, 1, "one entry for the repeating failure")
	assert.Equal(t, 7, history[0].Count)
	assert.Equal(t, ts.now.UTC(), history[0].Time, "the entry shows the last failure's time")
	assert.Len(t, ts.published, 1, "delivered once")
	assert.Len(t, ts.system, 1, "one system notification")
	assert.Equal(t, 6, ts.changes, "each repeat refreshes the bell")
}

func TestEmit_RepeatedFailureMovesToTheTopOfTheBell(t *testing.T) {
	ts := newTestService()
	ts.settings.Repeats.Streak = false
	ts.Emit(releaseFailure(1, "boom"))
	ts.now = ts.now.Add(time.Minute)
	ts.Emit(Event{Type: TypeChatReply, Title: "bot replied"})
	ts.now = ts.now.Add(5 * time.Minute)
	ts.Emit(releaseFailure(2, "boom"))

	history, _ := ts.History()
	require.Len(t, history, 2)
	assert.Equal(t, TypeCodeLoopFailed, history[0].Type)
	assert.Equal(t, 2, history[0].Count)
}

func TestEmit_RepeatsKeepWhetherTheEntryWasRead(t *testing.T) {
	ts := newTestService()
	ts.settings.Repeats.Streak = false
	ts.Emit(releaseFailure(1, "boom"))
	ts.MarkRead(nil)
	ts.now = ts.now.Add(5 * time.Minute)
	_, outcome := ts.Emit(releaseFailure(2, "boom"))
	require.Equal(t, Merged, outcome)
	_, unread := ts.History()
	assert.Zero(t, unread, "a silent repeat does not raise the unread count again")
}

func TestEmit_ChangedErrorNotifiesAgain(t *testing.T) {
	ts := newTestService()
	ts.settings.Repeats.Streak = false
	ts.Emit(releaseFailure(1, "release failed: tag exists"))
	ts.now = ts.now.Add(5 * time.Minute)
	_, outcome := ts.Emit(releaseFailure(2, "release failed: network unreachable"))
	assert.Equal(t, Delivered, outcome)
	ts.now = ts.now.Add(5 * time.Minute)
	_, outcome = ts.Emit(releaseFailure(3, "release failed: network unreachable"))
	assert.Equal(t, Merged, outcome, "the new error's repeats merge into its own entry")
	assert.Len(t, ts.published, 2)
}

func TestEmit_RecoveryThenFailureNotifiesAgain(t *testing.T) {
	ts := newTestService()
	ts.settings.Repeats.Streak = false
	e := releaseFailure(1, "boom")
	ts.Emit(e)
	ts.Recover(e.Source)
	ts.now = ts.now.Add(5 * time.Minute)
	_, outcome := ts.Emit(releaseFailure(2, "boom"))
	assert.Equal(t, Delivered, outcome)
	assert.Len(t, ts.published, 2)
}

func TestEmit_RepeatAfterTheWindowNotifiesAgain(t *testing.T) {
	ts := newTestService()
	ts.settings.Repeats.Streak = false
	ts.settings.Repeats.WindowMinutes = 60
	ts.Emit(releaseFailure(1, "boom"))
	ts.now = ts.now.Add(61 * time.Minute)
	_, outcome := ts.Emit(releaseFailure(2, "boom"))
	assert.Equal(t, Delivered, outcome)
}

func TestEmit_RepeatsDeliveredEachTimeWhenNotSuppressed(t *testing.T) {
	ts := newTestService()
	ts.settings.Repeats.Suppress = false
	ts.settings.Repeats.Streak = false
	for run := 1; run <= 3; run++ {
		ts.now = ts.now.Add(5 * time.Minute)
		_, outcome := ts.Emit(releaseFailure(run, "boom"))
		assert.Equal(t, Delivered, outcome)
	}
	assert.Len(t, ts.published, 3)
}

func TestEmit_StreakNotifiesOncePerRunOfFailures(t *testing.T) {
	ts := newTestService()
	ts.settings.Server = true

	var alerts []Notification
	for run := 1; run <= 12; run++ {
		ts.now = ts.now.Add(5 * time.Minute)
		ts.Emit(releaseFailure(run, "boom"))
	}
	for _, n := range ts.published {
		if n.PauseTask != "" {
			alerts = append(alerts, n)
		}
	}
	require.Len(t, ts.published, 2, "the first failure and the in-a-row notification")
	require.Len(t, alerts, 1)
	assert.Equal(t, `Scheduled prompt "Aviary/aviary-release" has failed 5 times in a row`, alerts[0].Title)
	assert.Equal(t, "Aviary/aviary-release", alerts[0].PauseTask)
	assert.Equal(t, SeverityError, alerts[0].Severity)
	assert.Len(t, ts.system, 2)

	// A success ends the streak; the next streak notifies once more.
	ts.Recover(releaseFailure(0, "").Source)
	for run := 13; run <= 18; run++ {
		ts.now = ts.now.Add(5 * time.Minute)
		ts.Emit(releaseFailure(run, "boom"))
	}
	assert.Len(t, ts.published, 4)
	assert.NotEmpty(t, ts.published[3].PauseTask)
}

func TestEmit_StreakOffSendsNoInARowNotification(t *testing.T) {
	ts := newTestService()
	ts.settings.Repeats.Streak = false
	for run := 1; run <= 10; run++ {
		ts.Emit(releaseFailure(run, "boom"))
	}
	assert.Len(t, ts.published, 1)
}

func TestEmit_StreakCountsAcrossChangingErrors(t *testing.T) {
	ts := newTestService()
	ts.settings.Repeats.StreakCount = 3
	ts.Emit(releaseFailure(1, "one"))
	ts.Emit(releaseFailure(2, "two"))
	n, outcome := ts.Emit(releaseFailure(3, "three"))
	assert.Equal(t, Delivered, outcome)
	assert.Contains(t, n.Title, "3 times in a row", "the third failure in a row is the in-a-row notification")
	assert.Len(t, ts.published, 3)
	ts.now = ts.now.Add(time.Minute)
	_, outcome = ts.Emit(releaseFailure(4, "three"))
	assert.Equal(t, Merged, outcome, "repeats merge into the in-a-row notification")
}

func TestEmit_SourcesAreTrackedSeparately(t *testing.T) {
	ts := newTestService()
	a := releaseFailure(1, "boom")
	b := a
	b.Source, b.Title = "Task bot/other", "Task bot/other failed"
	ts.Emit(a)
	_, outcome := ts.Emit(b)
	assert.Equal(t, Delivered, outcome)
}

func TestNormalizeError_IgnoresVolatileDetails(t *testing.T) {
	a := normalizeError("Run 41 failed at 2026-11-26T19:48:25Z: exit status 1 (job 1a2b3c4d5e, 12.5s)")
	b := normalizeError("run 42 failed at 2026-11-26T19:53:25Z:  exit status 1 (job 9f8e7d6c5b, 3.1s)")
	assert.Equal(t, a, b)
	assert.NotEqual(t, a, normalizeError("run 42 failed: permission denied"))
	assert.Equal(t, "decade", normalizeError("decade"), "hex-looking words without digits are kept")
}

func TestNormalize_Repeats(t *testing.T) {
	assert.Equal(t, DefaultRepeats(), *Normalize(&store.NotificationPreferences{}).Repeats, "missing repeats get the defaults")
	in := DefaultSettings()
	in.Repeats = &store.NotificationRepeats{Suppress: false, WindowMinutes: 0, Streak: true, StreakCount: 1000}
	out := Normalize(&in)
	assert.False(t, out.Repeats.Suppress)
	assert.Equal(t, 1, out.Repeats.WindowMinutes)
	assert.Equal(t, MaxStreakCount, out.Repeats.StreakCount)
	assert.True(t, DefaultRepeats().Suppress, "repeats are suppressed by default")
}
