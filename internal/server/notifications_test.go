package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/mcp"
	"github.com/lsegal/aviary/internal/notify"
	"github.com/lsegal/aviary/internal/projects/core"
	"github.com/lsegal/aviary/internal/store"
)

// notifyServer is a server whose notification service uses settings and
// records what it publishes.
func notifyServer(t *testing.T, settings store.NotificationPreferences) (*Server, *[]notify.Notification) {
	t.Helper()
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir("") })
	var published []notify.Notification
	n := notify.New()
	n.Settings = func() (store.NotificationPreferences, error) { return settings, nil }
	n.Publish = func(note notify.Notification) { published = append(published, note) }
	n.AfterFunc = func(time.Duration, func()) {
		// Never fire delayed callbacks: the test drives notifications directly.
	}
	s := &Server{cfg: &config.Config{Agents: []config.AgentConfig{{Name: "bot", Projects: []config.ProjectConfig{{Target: "acme/app"}}}}}, notifier: n}
	return s, &published
}

func TestDeliverOutput_NotifyTargetSendsRunSummary(t *testing.T) {
	s, published := notifyServer(t, notify.DefaultSettings())
	source := domain.OutputSource{Task: "bot/digest", JobID: "job-1"}
	delivery, err := s.deliverOutput(source, "bot", "notify", "  3 new issues today\nmore detail")
	require.NoError(t, err)
	assert.Equal(t, domain.DeliveryDelivered, delivery.Status)
	require.Len(t, *published, 1)
	n := (*published)[0]
	assert.Equal(t, notify.TypeTaskRun, n.Type)
	assert.Equal(t, "Task bot/digest finished", n.Title)
	assert.Equal(t, "3 new issues today\nmore detail", n.Body)
	assert.Equal(t, "/agents/bot/tasks/digest/runs/job-1", n.Link)
}

func TestDeliverOutput_NotifyFailuresOnlySkipsSuccess(t *testing.T) {
	s, published := notifyServer(t, notify.DefaultSettings())
	delivery, err := s.deliverOutput(domain.OutputSource{Task: "bot/digest", JobID: "job-1"}, "bot", "notify?failures", "all good")
	require.NoError(t, err)
	assert.Equal(t, domain.DeliverySkipped, delivery.Status)
	assert.Empty(t, *published)
}

func TestDeliverOutput_NotifyTargetReportsTurnedOff(t *testing.T) {
	settings := notify.DefaultSettings()
	settings.Enabled = false
	s, published := notifyServer(t, settings)
	delivery, err := s.deliverOutput(domain.OutputSource{Task: "bot/digest", JobID: "job-1"}, "bot", "notify", "x")
	require.NoError(t, err)
	assert.Contains(t, delivery.Message, "turned off")
	assert.Empty(t, *published)
}

func TestObserveRun_NotifiesFailures(t *testing.T) {
	s, published := notifyServer(t, notify.DefaultSettings())

	s.observeRun(domain.Job{ID: "j1", TaskID: "bot/digest", OutputChannel: "notify?failures"}, domain.TaskRunFailed, "boom")
	s.observeRun(domain.Job{ID: "j2", TaskID: "bot/digest", OutputChannel: "notify"}, domain.TaskRunCompleted, "ok")
	s.observeRun(domain.Job{ID: "j3", TaskID: "bot/digest", OutputChannel: "notify"}, domain.TaskRunRetrying, "retrying")
	s.observeRun(domain.Job{ID: "j4", TaskID: "bot/plain", OutputChannel: "session:main"}, domain.TaskRunFailed, "plain task")
	s.observeRun(domain.Job{ID: "j5", TaskID: "bot/triage", TriggerContext: &domain.TriggerContext{}}, domain.TaskRunFailed, "workflow broke")

	require.Len(t, *published, 2)
	assert.Equal(t, notify.TypeTaskRun, (*published)[0].Type)
	assert.Equal(t, notify.SeverityError, (*published)[0].Severity)
	assert.Equal(t, "Task bot/digest failed", (*published)[0].Title)
	assert.Equal(t, "boom", (*published)[0].Body)
	assert.Equal(t, notify.TypeWorkflowFailed, (*published)[1].Type)
	assert.Equal(t, "Workflow bot/triage failed", (*published)[1].Title)
	assert.Equal(t, "/agents/bot/tasks/triage/runs/j5", (*published)[1].Link)
}

func TestObserveCodeLoopJob_LinksTheJobCard(t *testing.T) {
	settings := notify.DefaultSettings()
	settings.Events[notify.TypeCodeLoopDone] = store.NotificationEvent{Enabled: true, Delivery: notify.DeliverySmall}
	s, published := notifyServer(t, settings)
	job := core.JobSnapshot{Target: "acme/app", Repository: "acme/app", Number: 7, Title: "Fix login", Kind: core.JobKindIssue}
	s.observeCodeLoopJob(job, config.ProjectReportFailed, "failed text")
	s.observeCodeLoopJob(job, config.ProjectReportWaiting, "waiting text")
	s.observeCodeLoopJob(job, config.ProjectReportMerged, "done text")
	watched := core.JobSnapshot{Target: "other/repo", Number: 3, Kind: core.JobKindIssue}
	s.observeCodeLoopJob(watched, config.ProjectReportFailed, "x")

	require.Len(t, *published, 4)
	assert.Equal(t, notify.TypeCodeLoopFailed, (*published)[0].Type)
	assert.Equal(t, "Failed: acme/app#7 Fix login", (*published)[0].Title)
	assert.Equal(t, "/agents/bot/projects/acme%2Fapp/jobs/7", (*published)[0].Link)
	assert.Equal(t, notify.TypeCodeLoopBlocked, (*published)[1].Type)
	assert.Equal(t, notify.TypeCodeLoopDone, (*published)[2].Type)
	assert.Equal(t, "/system/watches/other%2Frepo/jobs/3", (*published)[3].Link)
}

func TestChatReplies_NotifyUsersChatsOnceTheTurnEnds(t *testing.T) {
	var got []notify.Event
	c := newChatReplies(func(e notify.Event) { got = append(got, e) })
	sessions := map[string]*domain.Session{
		"s-main": {ID: "s-main", Name: "main", Type: domain.SessionTypeMain},
		"s-user": {ID: "s-user", Title: "Trip plans", Type: domain.SessionTypeUser},
		"s-chan": {ID: "s-chan", Type: domain.SessionTypeChannel},
		"s-task": {ID: "s-task", Type: domain.SessionTypeTask},
	}
	c.session = func(_, id string) (*domain.Session, error) {
		if s, ok := sessions[id]; ok {
			return s, nil
		}
		return nil, errors.New("missing")
	}
	processing := map[string]bool{"s-user": true}
	c.processing = func(_, id string) bool { return processing[id] }

	c.message("bot", "s-user", "assistant")
	c.message("bot", "s-user", "assistant")
	assert.Empty(t, got, "nothing while the turn runs")
	processing["s-user"] = false
	c.turnState("bot", "s-user", false)
	require.Len(t, got, 1, "one notification per turn")
	assert.Equal(t, "bot replied", got[0].Title)
	assert.Equal(t, "in Trip plans", got[0].Body)
	assert.Equal(t, "/agents/bot/chat/s-user", got[0].Link)

	c.turnState("bot", "s-user", false)
	c.message("bot", "s-main", "user")
	c.message("bot", "s-chan", "assistant")
	c.message("bot", "s-task", "assistant")
	c.message("bot", "s-gone", "assistant")
	assert.Len(t, got, 1, "no reply, or not the user's chat")

	c.message("bot", "s-main", "assistant")
	require.Len(t, got, 2, "a reply outside a turn notifies at once")
	assert.Equal(t, "in Main Chat", got[1].Body)
}

func notificationsRequest(t *testing.T, h http.HandlerFunc, method, path, body string) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return out
}

func TestNotificationsHandlers_NotifyListReadClear(t *testing.T) {
	s, published := notifyServer(t, notify.DefaultSettings())

	out := notificationsRequest(t, s.notificationsHandler, http.MethodPost, "/api/notifications",
		`{"title":"Deploy done","body":"v2 is live","severity":"success","link":"https://evil.example","type":"codeloop.failed"}`)
	assert.Equal(t, string(notify.Delivered), out["outcome"])
	require.Len(t, *published, 1)
	assert.Equal(t, notify.TypeCustom, (*published)[0].Type, "the endpoint sends custom notifications only")
	assert.Empty(t, (*published)[0].Link, "links stay inside the web UI")

	notificationsRequest(t, s.notificationsTestHandler, http.MethodPost, "/api/notifications/test", "")
	out = notificationsRequest(t, s.notificationsHandler, http.MethodGet, "/api/notifications", "")
	assert.EqualValues(t, 2, out["unread"])
	assert.Len(t, out["notifications"], 2)

	list, _ := s.notifier.History()
	out = notificationsRequest(t, s.notificationsReadHandler, http.MethodPost, "/api/notifications/read", `{"ids":["`+list[0].ID+`"]}`)
	assert.EqualValues(t, 1, out["unread"])
	out = notificationsRequest(t, s.notificationsReadHandler, http.MethodPost, "/api/notifications/read", "")
	assert.EqualValues(t, 0, out["unread"])

	out = notificationsRequest(t, s.notificationsHandler, http.MethodDelete, "/api/notifications", "")
	assert.Empty(t, out["notifications"])

	rec := httptest.NewRecorder()
	s.notificationsHandler(rec, httptest.NewRequest(http.MethodPost, "/api/notifications", strings.NewReader(`{"body":"no title"}`)))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestNotificationPreferencesHandler_SaveReadReset(t *testing.T) {
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir("") })
	get := func() store.NotificationPreferences {
		rec := httptest.NewRecorder()
		notificationPreferencesHandler(rec, httptest.NewRequest(http.MethodGet, "/api/preferences/notifications", nil))
		require.Equal(t, http.StatusOK, rec.Code)
		var prefs store.NotificationPreferences
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &prefs))
		return prefs
	}
	prefs := get()
	assert.False(t, prefs.Sound.Enabled, "sound is off by default")
	assert.True(t, prefs.Enabled)

	prefs.Sound.Enabled = true
	prefs.Sound.Volume = 30
	prefs.Events[notify.TypeChatReply] = store.NotificationEvent{Enabled: false, Delivery: notify.DeliveryBig}
	body, _ := json.Marshal(prefs)
	rec := httptest.NewRecorder()
	notificationPreferencesHandler(rec, httptest.NewRequest(http.MethodPut, "/api/preferences/notifications", strings.NewReader(string(body))))
	require.Equal(t, http.StatusOK, rec.Code)

	saved := get()
	assert.True(t, saved.Sound.Enabled)
	assert.Equal(t, 30, saved.Sound.Volume)
	assert.False(t, saved.Events[notify.TypeChatReply].Enabled)

	rec = httptest.NewRecorder()
	notificationPreferencesHandler(rec, httptest.NewRequest(http.MethodDelete, "/api/preferences/notifications", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, notify.DefaultSettings(), get())

	rec = httptest.NewRecorder()
	notificationPreferencesHandler(rec, httptest.NewRequest(http.MethodPut, "/api/preferences/notifications", strings.NewReader("{")))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestObserveRun_RepeatedFailuresMergeUntilASuccess(t *testing.T) {
	settings := notify.DefaultSettings()
	settings.Repeats.Streak = false
	s, published := notifyServer(t, settings)
	job := func(id string) domain.Job {
		return domain.Job{ID: id, TaskID: "bot/release", TriggerContext: &domain.TriggerContext{}}
	}

	s.observeRun(job("j1"), domain.TaskRunFailed, "tag v1 exists")
	s.observeRun(job("j2"), domain.TaskRunFailed, "tag v1 exists")
	require.Len(t, *published, 1, "a repeat with another run ID merges")
	history, _ := s.notifier.History()
	require.Len(t, history, 1)
	assert.Equal(t, 2, history[0].Count)

	s.observeRun(job("j3"), domain.TaskRunCompleted, "ok")
	s.observeRun(job("j4"), domain.TaskRunFailed, "tag v1 exists")
	assert.Len(t, *published, 2, "a failure after a success notifies again")
}

func TestObserveCodeLoopJob_ScheduledFailuresAreOneSource(t *testing.T) {
	settings := notify.DefaultSettings()
	settings.Repeats.StreakCount = 3
	s, published := notifyServer(t, settings)
	run := func(n int) core.JobSnapshot {
		return core.JobSnapshot{Target: "acme/app", Number: n, Kind: core.JobKindSchedule, ScheduleID: "bot/release"}
	}
	for n := 1; n <= 4; n++ {
		s.observeCodeLoopJob(run(1000+n), config.ProjectReportFailed, "Scheduled prompt failed. exit 1")
	}
	require.Len(t, *published, 2, "the first failure and one in-a-row notification")
	assert.Equal(t, `Scheduled prompt "bot/release" has failed 3 times in a row`, (*published)[1].Title)
	assert.Equal(t, "bot/release", (*published)[1].PauseTask)

	s.observeCodeLoopJob(run(2000), config.ProjectReportMerged, "done")
	s.observeCodeLoopJob(run(2001), config.ProjectReportFailed, "Scheduled prompt failed. exit 1")
	assert.Len(t, *published, 3, "a failure after a successful run notifies again")
}

func TestRecordTaskScriptResult_NotifiesOncePerFailureStreak(t *testing.T) {
	s, published := notifyServer(t, notify.DefaultSettings())
	base := t.TempDir()
	t.Setenv("AVIARY_CONFIG_BASE_DIR", base)
	old := mcp.GetDeps()
	t.Cleanup(func() { mcp.SetDeps(old) })
	mcp.SetDeps(&mcp.Deps{})
	require.NoError(t, config.Save("", &config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "test/x", Tasks: []config.TaskConfig{{
		Name: "release", Type: "project", Project: "acme/app", Prompt: "Release", Every: "5m",
		Precompile: new(true), Compiled: `print(1)`, CompiledKey: "k", ScriptVerified: true,
	}}}}}))
	broken := errors.New("<string>:3: gh: Not Found (HTTP 404)")

	for range 4 {
		require.NoError(t, s.recordTaskScriptResult("bot", "release", broken))
	}
	// A rate limit neither counts nor notifies.
	require.NoError(t, s.recordTaskScriptResult("bot", "release", errors.New("gh: API rate limit exceeded (HTTP 403)")))
	require.Len(t, *published, 1, "a failure streak notifies once")
	n := (*published)[0]
	assert.Equal(t, notify.TypeScriptFailing, n.Type)
	assert.Equal(t, notify.SeverityWarning, n.Severity)
	assert.Equal(t, "Task bot/release's compiled script is failing", n.Title)
	assert.Contains(t, n.Body, "gh 404")
	assert.Equal(t, "/agents/bot/tasks/release", n.Link)

	cfg, err := config.Load("")
	require.NoError(t, err)
	task := cfg.Agents[0].Tasks[0]
	assert.Equal(t, 4, task.ScriptFailures)
	assert.True(t, *task.Precompile)
	assert.Equal(t, `print(1)`, task.Compiled)

	// A completed run ends the streak; the next failure, a later run,
	// notifies again.
	s.notifier.Now = func() time.Time { return time.Now().Add(time.Hour) }
	require.NoError(t, s.recordTaskScriptResult("bot", "release", nil))
	require.NoError(t, s.recordTaskScriptResult("bot", "release", broken))
	assert.Len(t, *published, 2)
}
