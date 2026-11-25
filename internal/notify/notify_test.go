package notify

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/store"
)

// testService is a service with fixed settings and a manual clock, which
// records what it published and showed through the OS.
type testService struct {
	*Service
	settings  store.NotificationPreferences
	now       time.Time
	published []Notification
	system    []Notification
	changes   int
	connected bool
	timers    []func()
}

func newTestService() *testService {
	ts := &testService{settings: DefaultSettings(), now: time.Date(2026, 11, 24, 12, 0, 0, 0, time.Local)}
	s := New()
	s.Settings = func() (store.NotificationPreferences, error) { return ts.settings, nil }
	s.Now = func() time.Time { return ts.now }
	s.AfterFunc = func(_ time.Duration, f func()) { ts.timers = append(ts.timers, f) }
	s.Publish = func(n Notification) { ts.published = append(ts.published, n) }
	s.Changed = func() { ts.changes++ }
	s.Connected = func() bool { return ts.connected }
	s.System = func(n Notification) error { ts.system = append(ts.system, n); return nil }
	ts.Service = s
	return ts
}

func TestDefaultSettings_SoundOffByDefault(t *testing.T) {
	d := DefaultSettings()
	assert.True(t, d.Enabled)
	assert.False(t, d.Sound.Enabled, "sound is off for a new user")
	assert.False(t, d.Server)
	for _, et := range EventTypes {
		assert.Contains(t, d.Events, et.Type)
	}
	assert.Equal(t, d, Normalize(nil))
}

func TestNormalize_DropsUnknownValues(t *testing.T) {
	in := DefaultSettings()
	in.Sound.Type = "kazoo"
	in.Sound.Volume = 250
	in.QuietHours.Start = "25:99"
	in.Events = map[string]store.NotificationEvent{
		TypeChatReply: {Enabled: false, Delivery: "huge", Sound: "kazoo"},
		"made.up":     {Enabled: true},
	}
	out := Normalize(&in)
	assert.Equal(t, "chime", out.Sound.Type)
	assert.Equal(t, 100, out.Sound.Volume)
	assert.Equal(t, "22:00", out.QuietHours.Start)
	assert.Equal(t, store.NotificationEvent{Enabled: false, Delivery: DeliverySmall}, out.Events[TypeChatReply])
	assert.NotContains(t, out.Events, "made.up")
	assert.Equal(t, DefaultSettings().Events[TypeCodeLoopFailed], out.Events[TypeCodeLoopFailed])
}

func TestReadWriteSettings_PersistServerSide(t *testing.T) {
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir("") })

	got, err := ReadSettings()
	require.NoError(t, err)
	assert.False(t, got.Sound.Enabled)

	next := DefaultSettings()
	next.Sound.Enabled = true
	next.Sound.Type = "bell"
	_, err = WriteSettings(&next)
	require.NoError(t, err)
	got, err = ReadSettings()
	require.NoError(t, err)
	assert.True(t, got.Sound.Enabled)
	assert.Equal(t, "bell", got.Sound.Type)

	_, err = WriteSettings(nil)
	require.NoError(t, err)
	got, err = ReadSettings()
	require.NoError(t, err)
	assert.Equal(t, DefaultSettings(), got)
}

func TestEmit_DeliversToEveryMethod(t *testing.T) {
	ts := newTestService()
	ts.settings.Server = true
	ts.settings.Sound.Enabled = true
	ts.settings.Events[TypeCodeLoopFailed] = store.NotificationEvent{Enabled: true, Delivery: DeliveryBig, Sound: "alert"}

	n, outcome := ts.Emit(Event{Type: TypeCodeLoopFailed, Severity: SeverityError, Title: "o/r#1 failed", Link: "/system/watches/o%2Fr/jobs/1"})
	require.Equal(t, Delivered, outcome)
	assert.Equal(t, DeliveryBig, n.Delivery)
	assert.Equal(t, "alert", n.Sound)
	assert.Equal(t, SectionCodeLoop, n.Section)
	require.Len(t, ts.published, 1, "pushed to connected clients")
	require.Len(t, ts.system, 1, "no client connected, so the OS shows it")

	history, unread := ts.History()
	require.Len(t, history, 1, "kept for the bell")
	assert.Equal(t, 1, unread)

	ts.connected = true
	ts.Emit(Event{Type: TypeChatReply, Title: "bot replied"})
	assert.Len(t, ts.published, 2)
	assert.Len(t, ts.system, 1, "a connected client shows it instead of the OS")
}

func TestEmit_SoundUsesDefaultTypeOrNone(t *testing.T) {
	ts := newTestService()
	n, _ := ts.Emit(Event{Type: TypeChatReply, Title: "a"})
	assert.Empty(t, n.Sound, "sound is off by default")

	ts.settings.Sound.Enabled = true
	n, _ = ts.Emit(Event{Type: TypeChatReply, Title: "b"})
	assert.Equal(t, "chime", n.Sound)

	ts.settings.Events[TypeChatReply] = store.NotificationEvent{Enabled: true, Delivery: DeliverySmall, Sound: SoundNone}
	n, _ = ts.Emit(Event{Type: TypeChatReply, Title: "c"})
	assert.Empty(t, n.Sound)
}

func TestEmit_MasterSwitchOffSendsNothing(t *testing.T) {
	ts := newTestService()
	ts.settings.Enabled = false
	ts.settings.Server = true
	for _, typ := range []string{TypeChatReply, TypeCodeLoopFailed, TypeTaskRun, TypeCustom, TypeTest} {
		_, outcome := ts.Emit(Event{Type: typ, Title: "x"})
		assert.Equal(t, Suppressed, outcome, typ)
	}
	assert.Empty(t, ts.published)
	assert.Empty(t, ts.system)
	history, _ := ts.History()
	assert.Empty(t, history)
}

func TestEmit_DisabledEventTypeSendsNothing(t *testing.T) {
	ts := newTestService()
	_, outcome := ts.Emit(Event{Type: TypeCodeLoopDone, Title: "done"})
	assert.Equal(t, Suppressed, outcome, "Code Loop done is off by default")
	_, outcome = ts.Emit(Event{Type: "not.a.type", Title: "x"})
	assert.Equal(t, Suppressed, outcome)
	assert.Empty(t, ts.published)
}

func TestEmit_TestReachesEveryMethodRegardlessOfTypeSettings(t *testing.T) {
	ts := newTestService()
	ts.settings.Server = true
	ts.settings.QuietHours = store.NotificationQuietHours{Enabled: true, Start: "00:00", End: "23:59"}
	for range RateLimit + 2 {
		_, outcome := ts.Emit(Event{Type: TypeTest, Title: "Test notification"})
		assert.Equal(t, Delivered, outcome)
	}
	assert.Len(t, ts.published, RateLimit+2)
	assert.False(t, ts.published[0].Quiet)
}

func TestEmit_DeduplicatesRepeats(t *testing.T) {
	ts := newTestService()
	first, _ := ts.Emit(Event{Type: TypeWorkflowFailed, Title: "triage failed", Link: "/x"})
	ts.MarkRead(nil)
	ts.now = ts.now.Add(10 * time.Second)
	merged, outcome := ts.Emit(Event{Type: TypeWorkflowFailed, Title: "triage failed", Link: "/x", Body: "again"})
	require.Equal(t, Merged, outcome)
	assert.Equal(t, first.ID, merged.ID)
	assert.Equal(t, 2, merged.Count)
	assert.False(t, merged.Read, "a repeat is unread again")
	assert.Len(t, ts.published, 1, "a repeat is not shown again")

	ts.now = ts.now.Add(2 * DedupeWindow)
	_, outcome = ts.Emit(Event{Type: TypeWorkflowFailed, Title: "triage failed", Link: "/x"})
	assert.Equal(t, Delivered, outcome, "outside the window it is new")
}

func TestEmit_RateLimitsAndGroupsBursts(t *testing.T) {
	ts := newTestService()
	for i := range 10 {
		ts.Emit(Event{Type: TypeCodeLoopFailed, Severity: SeverityError, Title: "job " + string(rune('A'+i)) + " failed"})
	}
	assert.Len(t, ts.published, RateLimit, "only the first few of a burst are delivered at once")
	require.Len(t, ts.timers, 1, "the rest wait for the window to end")

	ts.now = ts.now.Add(RateWindow)
	ts.timers[0]()
	require.Len(t, ts.published, RateLimit+1, "the rest arrive as one notification")
	group := ts.published[RateLimit]
	assert.Equal(t, 10-RateLimit, group.Count)
	assert.Contains(t, group.Title, "7 more")
	assert.Contains(t, group.Body, "job D failed")
	assert.Contains(t, group.Body, "and 2 more")

	_, outcome := ts.Emit(Event{Type: TypeCodeLoopFailed, Title: "later failed"})
	assert.Equal(t, Delivered, outcome, "a new window delivers again")
	_, outcome = ts.Emit(Event{Type: TypeChatReply, Title: "other types are not limited by it"})
	assert.Equal(t, Delivered, outcome)
}

func TestEmit_QuietHoursKeepHistoryOnly(t *testing.T) {
	ts := newTestService()
	ts.settings.Server = true
	ts.settings.QuietHours = store.NotificationQuietHours{Enabled: true, Start: "11:00", End: "13:00"}
	n, outcome := ts.Emit(Event{Type: TypeChatReply, Title: "shh"})
	require.Equal(t, Delivered, outcome)
	assert.True(t, n.Quiet)
	assert.Empty(t, ts.system)
	history, _ := ts.History()
	assert.Len(t, history, 1)
}

func TestInQuietHours_SpansMidnight(t *testing.T) {
	q := store.NotificationQuietHours{Enabled: true, Start: "22:00", End: "07:00"}
	at := func(h, m int) time.Time { return time.Date(2026, 1, 1, h, m, 0, 0, time.Local) }
	assert.True(t, inQuietHours(q, at(23, 0)))
	assert.True(t, inQuietHours(q, at(6, 59)))
	assert.False(t, inQuietHours(q, at(7, 0)))
	assert.False(t, inQuietHours(q, at(12, 0)))
	q.Enabled = false
	assert.False(t, inQuietHours(q, at(23, 0)))
}

func TestHistory_MarkReadAndClear(t *testing.T) {
	ts := newTestService()
	a, _ := ts.Emit(Event{Type: TypeChatReply, Title: "a"})
	ts.Emit(Event{Type: TypeChatReply, Title: "b"})
	ts.MarkRead([]string{a.ID})
	history, unread := ts.History()
	require.Len(t, history, 2)
	assert.Equal(t, "b", history[0].Title, "newest first")
	assert.Equal(t, 1, unread)
	ts.MarkRead(nil)
	_, unread = ts.History()
	assert.Equal(t, 0, unread)
	ts.Clear()
	history, _ = ts.History()
	assert.Empty(t, history)
	assert.Equal(t, 3, ts.changes)
}

func TestHistory_KeepsTheLatest(t *testing.T) {
	ts := newTestService()
	for i := range HistoryLimit + 5 {
		ts.now = ts.now.Add(time.Hour)
		ts.Emit(Event{Type: TypeTest, Title: string(rune(i))})
	}
	history, _ := ts.History()
	assert.Len(t, history, HistoryLimit)
}

func TestEmit_SystemErrorIsLoggedNotFatal(t *testing.T) {
	ts := newTestService()
	ts.settings.Server = true
	ts.System = func(Notification) error { return errors.New("no osascript") }
	_, outcome := ts.Emit(Event{Type: TypeChatReply, Title: "x"})
	assert.Equal(t, Delivered, outcome)
}

func TestDarwinCommand(t *testing.T) {
	n := Notification{Type: TypeChatReply, Title: `bot "replied"`, Body: "hi; do shell script", Link: "/agents/bot/chat/s1"}
	none := func(string) (string, error) { return "", errors.New("missing") }
	args := darwinCommand(n, "https://localhost:16677/", none)
	assert.Equal(t, "osascript", args[0])
	assert.Equal(t, []string{`bot "replied"`, "hi; do shell script"}, args[len(args)-2:], "text is passed as arguments")

	found := func(string) (string, error) { return "/opt/homebrew/bin/terminal-notifier", nil }
	args = darwinCommand(n, "https://localhost:16677/", found)
	assert.Equal(t, "/opt/homebrew/bin/terminal-notifier", args[0])
	assert.Contains(t, args, "https://localhost:16677/agents/bot/chat/s1")
}
