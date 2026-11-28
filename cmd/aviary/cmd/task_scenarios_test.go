package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/scheduler"
)

// taskReply is everything a task command told the user: its output and its
// error. Tool errors currently print with a nil error (#1663), so assertions
// on rejected input check the message the user sees rather than the error.
func taskReply(out string, err error) string {
	if err != nil {
		return out + err.Error()
	}
	return out
}

// taskListRow is the task list table row for id.
func taskListRow(t *testing.T, id string) string {
	t.Helper()
	out, err := runTaskCmd(t, taskListCmd)
	require.NoError(t, err)
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, id+" ") {
			return line
		}
	}
	return ""
}

// listTasksJSON is task list --json decoded.
func listTasksJSON(t *testing.T) map[string]domain.ScheduledTask {
	t.Helper()
	out, err := runTaskCmd(t, taskListCmd, "--json")
	require.NoError(t, err)
	var tasks []domain.ScheduledTask
	require.NoError(t, json.Unmarshal([]byte(out), &tasks))
	byID := map[string]domain.ScheduledTask{}
	for _, task := range tasks {
		byID[task.ID] = task
	}
	return byID
}

// previewFires is the fire lines a task preview printed.
func previewFires(out string) []string {
	var fires []string
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, "  ") {
			fires = append(fires, strings.TrimSpace(line))
		}
	}
	return fires
}

// setupModelCLI is setupTaskCLI with the bot agent on an OpenAI-compatible
// model served by handler.
func setupModelCLI(t *testing.T, handler http.Handler) *scheduler.Scheduler {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	factory := llm.NewFactory(func(string) (string, error) { return "", nil }).
		WithProviderOptionsResolver(func(string) (llm.ProviderOptions, bool) {
			return llm.ProviderOptions{BaseURI: srv.URL + "/v1"}, true
		})
	return setupCLIWithModel(t, factory, "vllm/test", "bot")
}

func TestTaskCLI_RejectsInvalidInput(t *testing.T) {
	s := setupTaskCLI(t)
	_, err := runTaskCmd(t, taskAddCmd, "bot", "keep", "x", "--every", "1h")
	require.NoError(t, err)

	cases := []struct {
		name string
		cmd  func() (string, error)
		want string
	}{
		{"zero interval", func() (string, error) { return runTaskCmd(t, taskAddCmd, "bot", "a", "x", "--every", "0m") }, `invalid every "0m"`},
		{"bad time", func() (string, error) { return runTaskCmd(t, taskAddCmd, "bot", "a", "x", "--at", "25:99") }, `invalid at "25:99"`},
		{"short cron", func() (string, error) { return runTaskCmd(t, taskAddCmd, "bot", "a", "x", "--schedule", "* * *") }, `invalid cron expression "* * *"`},
		{"unknown timezone", func() (string, error) {
			return runTaskCmd(t, taskAddCmd, "bot", "a", "x", "--every", "1h", "--timezone", "Mars/Olympus")
		}, `invalid timezone "Mars/Olympus"`},
		{"unknown overlap", func() (string, error) {
			return runTaskCmd(t, taskAddCmd, "bot", "a", "x", "--every", "1h", "--overlap", "banana")
		}, `invalid overlap "banana"`},
		{"unknown agent", func() (string, error) { return runTaskCmd(t, taskAddCmd, "nobody", "a", "x", "--every", "1h") }, `agent "nobody" not found`},
		{"rm unknown task", func() (string, error) { return runTaskCmd(t, taskRmCmd, "nosuch") }, `task "nosuch" not found`},
		{"update unknown task", func() (string, error) { return runTaskCmd(t, taskUpdateCmd, "nosuch", "--at", "09:00") }, `task "nosuch" not found`},
		{"preview unknown task", func() (string, error) { return runTaskCmd(t, taskPreviewCmd, "nosuch") }, `task "nosuch" not found`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Contains(t, taskReply(tc.cmd()), tc.want)
		})
	}

	tasks := s.ListTasks()
	require.Len(t, tasks, 1, "rejected input saves nothing")
	assert.Equal(t, "keep", tasks[0].Name)
}

func TestTaskCLI_ListsCronTasks(t *testing.T) {
	setupTaskCLI(t)
	_, err := runTaskCmd(t, taskAddCmd, "bot", "weekly-cron", "x", "--schedule", "0 9 * * 1")
	require.NoError(t, err)
	_, err = runTaskCmd(t, taskAddCmd, "bot", "hourly", "x", "--schedule", "@hourly")
	require.NoError(t, err)

	tasks := listTasksJSON(t)
	assert.Equal(t, domain.TriggerTypeCron, tasks["bot/weekly-cron"].TriggerType)
	assert.Equal(t, "0 9 * * 1", tasks["bot/weekly-cron"].Schedule)
	assert.Equal(t, "@hourly", tasks["bot/hourly"].Schedule)

	// #1664 tracks the expression also printing a second time.
	assert.Contains(t, taskListRow(t, "bot/weekly-cron"), "cron 0 9 * * 1")
	assert.Contains(t, taskListRow(t, "bot/hourly"), "cron @hourly")
}

func TestTaskCLI_WeeklyRangePreviewsEveryTimeOnEveryDay(t *testing.T) {
	setupTaskCLI(t)
	out, err := runTaskCmd(t, taskAddCmd, "bot", "weekend", "x", "--days", "sat-sun", "--at", "07:30,19:00", "--timezone", "UTC")
	require.NoError(t, err)
	assert.Contains(t, out, "weekly on Sat, Sun at 07:30, 19:00 (UTC)")
	assert.Equal(t, "sat,sun", listTasksJSON(t)["bot/weekend"].Days)

	out, err = runTaskCmd(t, taskPreviewCmd, "weekend", "--count", "4")
	require.NoError(t, err)
	fires := previewFires(out)
	require.Len(t, fires, 4)
	for _, want := range []string{`^Sat \S+ 07:30 UTC$`, `^Sat \S+ 19:00 UTC$`, `^Sun \S+ 07:30 UTC$`, `^Sun \S+ 19:00 UTC$`} {
		re := regexp.MustCompile(want)
		found := false
		for _, fire := range fires {
			found = found || re.MatchString(fire)
		}
		assert.True(t, found, "no fire matches %s in %q", want, fires)
	}

	out, err = runTaskCmd(t, taskPreviewCmd, "--days", "fri-mon", "--at", "12:00", "--timezone", "UTC", "--count", "4")
	require.NoError(t, err)
	assert.Contains(t, out, "weekly on Fri, Sat, Sun, Mon at 12:00 (UTC)", "ranges wrap around the week")
}

func TestTaskCLI_UpdateSemantics(t *testing.T) {
	setupTaskCLI(t)
	_, err := runTaskCmd(t, taskAddCmd, "bot", "weekend", "x", "--days", "sat,sun", "--at", "07:30")
	require.NoError(t, err)

	_, err = runTaskCmd(t, taskUpdateCmd, "weekend", "--enabled=false")
	require.NoError(t, err)
	assert.Contains(t, taskListRow(t, "bot/weekend"), "disabled")
	_, err = runTaskCmd(t, taskUpdateCmd, "weekend", "--enabled")
	require.NoError(t, err)
	assert.NotContains(t, taskListRow(t, "bot/weekend"), "disabled")
	assert.True(t, listTasksJSON(t)["bot/weekend"].Enabled)

	_, err = runTaskCmd(t, taskUpdateCmd, "weekend", "--days", "")
	require.NoError(t, err)
	task := listTasksJSON(t)["bot/weekend"]
	assert.Equal(t, domain.TriggerTypeDaily, task.TriggerType, "clearing the days of a weekly task makes it daily")
	assert.Contains(t, taskListRow(t, "bot/weekend"), "daily at 07:30")

	_, err = runTaskCmd(t, taskAddCmd, "bot", "hourly", "x", "--every", "1h")
	require.NoError(t, err)
	out, err := runTaskCmd(t, taskUpdateCmd, "hourly", "--every", "")
	assert.Contains(t, taskReply(out, err), "needs a schedule trigger")
	assert.Equal(t, "1h", listTasksJSON(t)["bot/hourly"].Every, "a refused update leaves the task alone")

	// --days/--at alone on an interval task are ignored today; #1665 tracks
	// refusing them or inferring the weekly trigger the way add does.
	_, err = runTaskCmd(t, taskAddCmd, "bot", "explicit", "x", "--every", "1h")
	require.NoError(t, err)
	_, err = runTaskCmd(t, taskUpdateCmd, "explicit", "--trigger", "weekly", "--days", "tue", "--at", "18:15")
	require.NoError(t, err)
	assert.Equal(t, domain.TriggerTypeWeekly, listTasksJSON(t)["bot/explicit"].TriggerType)
	assert.Contains(t, taskListRow(t, "bot/explicit"), "weekly on Tue at 18:15")
}

func TestTaskCLI_AddReplacesTaskAndResetsOmittedPolicies(t *testing.T) {
	s := setupTaskCLI(t)
	_, err := runTaskCmd(t, taskAddCmd, "bot", "digest", "first", "--every", "1h", "--overlap", "skip")
	require.NoError(t, err)
	assert.Equal(t, "skip", listTasksJSON(t)["bot/digest"].Overlap)

	out, err := runTaskCmd(t, taskAddCmd, "bot", "digest", "second", "--every", "2h")
	require.NoError(t, err)
	assert.Contains(t, out, `"digest" updated`)
	tasks := s.ListTasks()
	require.Len(t, tasks, 1)
	task := listTasksJSON(t)["bot/digest"]
	assert.Equal(t, "second", task.Prompt)
	assert.Equal(t, "2h", task.Every)
	assert.Equal(t, "queue", task.Overlap, "re-adding resets an omitted policy to its default")
}

// blockingModel answers no chat request until the request is canceled or
// the model is released.
type blockingModel struct{ started, release chan struct{} }

func (m *blockingModel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	select {
	case m.started <- struct{}{}:
	default:
	}
	select {
	case <-r.Context().Done():
	case <-m.release:
	}
}

func TestTaskCLI_StopCancelsRunningAndQueuedRuns(t *testing.T) {
	model := &blockingModel{started: make(chan struct{}, 1), release: make(chan struct{})}
	setupModelCLI(t, model)
	t.Cleanup(func() { close(model.release) })
	_, err := runTaskCmd(t, taskAddCmd, "bot", "slow", "x", "--every", "1h")
	require.NoError(t, err)

	for range 2 {
		out, err := runTaskCmd(t, taskRunCmd, "slow")
		require.NoError(t, err)
		assert.Contains(t, out, `Started task "bot/slow"`)
	}
	select {
	case <-model.started:
	case <-time.After(10 * time.Second):
		t.Fatal("the run never reached the model")
	}

	out, err := runTaskCmd(t, taskStopCmd, "slow")
	require.NoError(t, err)
	assert.Contains(t, out, "stopped 2 pending/running task job(s)")
	assert.Eventually(t, func() bool {
		return strings.Contains(taskListRow(t, "bot/slow"), "canceled")
	}, 10*time.Second, 50*time.Millisecond, "LAST RESULT shows the canceled run")

	out, err = runTaskCmd(t, taskStopCmd)
	require.NoError(t, err)
	assert.Contains(t, out, "no pending or running task jobs to stop")
	out, err = runTaskCmd(t, taskStopCmd, "nosuch")
	require.NoError(t, err)
	assert.Contains(t, out, `no pending or running task jobs matched "nosuch"`)
}

func TestTaskCLI_FailedRunRecordsProviderError(t *testing.T) {
	setupModelCLI(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"model quota exhausted","type":"invalid_request_error"}}`))
	}))
	_, err := runTaskCmd(t, taskAddCmd, "bot", "broken", "x", "--every", "1h")
	require.NoError(t, err)
	_, err = runTaskCmd(t, taskRunCmd, "broken")
	require.NoError(t, err)

	var run domain.TaskRun
	require.Eventually(t, func() bool {
		history := listTasksJSON(t)["bot/broken"].History
		if len(history) == 0 {
			return false
		}
		run = history[len(history)-1]
		return run.Status != domain.TaskRunQueued && run.Status != domain.TaskRunRunning
	}, 15*time.Second, 50*time.Millisecond)
	assert.Equal(t, domain.TaskRunFailed, run.Status)
	assert.Contains(t, run.Summary, "model quota exhausted")
	assert.Contains(t, taskListRow(t, "bot/broken"), "failed")
}

func TestTaskCLI_RemoveKeepsOrDeletesData(t *testing.T) {
	s := setupTaskCLI(t)
	for _, name := range []string{"kept", "purged", "aliased"} {
		_, err := runTaskCmd(t, taskAddCmd, "bot", name, "x", "--every", "1h")
		require.NoError(t, err)
	}

	out, err := runTaskCmd(t, taskRmCmd, "kept")
	require.NoError(t, err)
	assert.Contains(t, out, "kept as a regular chat")

	out, err = runTaskCmd(t, taskRmCmd, "purged", "--delete-data")
	require.NoError(t, err)
	assert.Contains(t, out, "with its session and run history")

	for _, alias := range []string{"remove", "delete"} {
		found, _, err := rootCmd.Find([]string{"task", alias})
		require.NoError(t, err)
		assert.Same(t, taskRmCmd, found, "task %s is rm", alias)
	}
	_, err = runTaskCmd(t, taskRmCmd, "bot/aliased")
	require.NoError(t, err)
	assert.Empty(t, s.ListTasks())
}

func TestTaskCLI_AddWithEmptyNameGeneratesOne(t *testing.T) {
	s := setupTaskCLI(t)
	out, err := runTaskCmd(t, taskAddCmd, "bot", "", "x", "--every", "1h")
	require.NoError(t, err)
	m := regexp.MustCompile(`"(x-[0-9a-f]+)" created`).FindStringSubmatch(out)
	require.Len(t, m, 2, "generated name in %q", out)

	assert.NotEmpty(t, taskListRow(t, "bot/"+m[1]))
	_, err = runTaskCmd(t, taskRmCmd, m[1])
	require.NoError(t, err)
	assert.Empty(t, s.ListTasks())
}

func TestTaskCLI_PreviewLimits(t *testing.T) {
	setupTaskCLI(t)
	out, err := runTaskCmd(t, taskPreviewCmd, "--every", "1m", "--count", "50")
	require.NoError(t, err)
	assert.Len(t, previewFires(out), 20, "count is capped at 20")

	out, err = runTaskCmd(t, taskPreviewCmd, "--every", "1m", "--count", "0")
	require.NoError(t, err)
	assert.Len(t, previewFires(out), 3, "a zero count falls back to the default")

	out, err = runTaskCmd(t, taskPreviewCmd)
	assert.Contains(t, taskReply(out, err), "no schedule trigger to preview")
}
