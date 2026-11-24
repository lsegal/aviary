package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// fakeWatchTools answers MCP tool calls with canned JSON and records them.
type fakeWatchTools struct {
	mu        sync.Mutex
	responses map[string]any
	errs      map[string]error
	calls     []fakeWatchCall
	// logs maps a log_offset to the lines project_status returns from it.
	logs map[int][]string
}

type fakeWatchCall struct {
	name string
	args map[string]any
}

func (f *fakeWatchTools) call(_ context.Context, name string, args map[string]any) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeWatchCall{name, args})
	if err := f.errs[name]; err != nil {
		return "", err
	}
	resp := f.responses[name]
	if name == "project_status" && f.logs != nil {
		status := map[string]any{}
		for k, v := range resp.(map[string]any) {
			status[k] = v
		}
		offset := args["log_offset"].(int)
		lines := f.logs[offset]
		status["recent_logs"] = lines
		status["log_offset"] = offset + len(lines)
		resp = status
	}
	if resp == nil {
		return "", nil
	}
	data, err := json.Marshal(resp)
	return string(data), err
}

func (f *fakeWatchTools) callsTo(name string) []fakeWatchCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []fakeWatchCall
	for _, c := range f.calls {
		if c.name == name {
			out = append(out, c)
		}
	}
	return out
}

// watchStrip drops a rendered string's styling.
func watchStrip(s string) string { return watchANSI.ReplaceAllString(s, "") }

func watchTestNow() time.Time { return time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC) }

func newFakeWatchTools() *fakeWatchTools {
	return &fakeWatchTools{responses: map[string]any{
		"project_status": map[string]any{
			"running":     true,
			"targets":     []string{"o/r"},
			"states":      map[string]int{"starting": 1, "paused": 1, "running": 1, "done": 1, "failed": 1},
			"jobs":        map[string]int{"running": 2, "merged_24h": 3},
			"concurrency": 4,
			"delivery":    "poll",
		},
		"project_job_list": map[string]any{"jobs": []map[string]any{
			{"key": "o/r#1", "target": "o/r", "number": 1, "title": "Queued work", "status": "queued", "state": "starting", "kind": "issue", "actions": map[string]bool{"retry": false, "pause": true, "stop": false}, "retry_unavailable": "already queued"},
			{"key": "o/r#2", "target": "o/r", "number": 2, "title": "Paused work", "status": "paused", "state": "paused", "kind": "issue", "actions": map[string]bool{"retry": true, "resume": true, "stop": true}},
			{"key": "o/r#3", "target": "o/r", "number": 3, "title": "Running work", "status": "active", "state": "running", "kind": "issue", "agent": "claude", "model": "opus", "actions": map[string]bool{"retry": false, "pause": true, "stop": true}},
			{"key": "o/r#4", "target": "o/r", "number": 4, "title": "Done work", "status": "complete", "state": "done", "kind": "issue", "pull_request": 9, "actions": map[string]bool{"retry": true}},
			{"key": "o/r#5", "target": "o/r", "number": 5, "title": "Failed work", "status": "failed", "kind": "issue", "actions": map[string]bool{"retry": true}},
			// A schedule's entry between runs belongs to its task's card.
			{"key": "o/r#900", "target": "o/r", "number": 900, "title": "nightly run", "status": "complete", "state": "scheduled", "kind": "schedule", "schedule_id": "bot/nightly", "last_run": map[string]any{"result": "succeeded"}, "actions": map[string]bool{"retry": true}},
		}},
		"task_list": []map[string]any{
			{"id": "bot/nightly", "name": "nightly", "type": "project", "project": "o/r", "enabled": true},
			{"id": "bot/other", "name": "other", "type": "project", "project": "x/y", "enabled": true},
			{"id": "bot/plain", "name": "plain", "type": "prompt", "enabled": true},
		},
		"task_upcoming": []map[string]any{
			{"task_id": "bot/nightly", "name": "nightly", "project": "o/r", "at": "2026-11-23T12:30:00Z"},
		},
	}}
}

// loadedWatchModel is a model sized for a terminal and fed one poll.
func loadedWatchModel(t *testing.T, tools *fakeWatchTools) watchModel {
	t.Helper()
	m := newWatchModel(context.Background(), tools.call, "o/r", "")
	m.now = watchTestNow
	m = watchUpdate(t, m, tea.WindowSizeMsg{Width: 160, Height: 40})
	return watchPoll(t, m)
}

func watchUpdate(t *testing.T, m watchModel, msg tea.Msg) watchModel {
	t.Helper()
	next, _ := m.Update(msg)
	return next.(watchModel)
}

func watchPoll(t *testing.T, m watchModel) watchModel {
	t.Helper()
	return watchUpdate(t, m, m.fetchCmd()())
}

// watchKey sends a key and runs the command it returns, feeding the result
// back in.
func watchKey(t *testing.T, m watchModel, key string) (watchModel, tea.Msg) {
	t.Helper()
	var msg tea.KeyMsg
	switch key {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "right":
		msg = tea.KeyMsg{Type: tea.KeyRight}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	next, cmd := m.Update(msg)
	m = next.(watchModel)
	if cmd == nil {
		return m, nil
	}
	out := cmd()
	return m, out
}

func watchItemKeys(items []watchItem) []string {
	var keys []string
	for _, it := range items {
		keys = append(keys, it.key)
	}
	return keys
}

func TestWatchTUIClassifiesIntoTheSixFilters(t *testing.T) {
	m := loadedWatchModel(t, newFakeWatchTools())
	want := map[string]string{
		"":             "task:bot/nightly o/r#3 o/r#5 o/r#2 o/r#1 o/r#4",
		watchScheduled: "task:bot/nightly",
		watchStarting:  "o/r#1",
		watchPaused:    "o/r#2",
		watchRunning:   "o/r#3",
		watchDone:      "o/r#4",
		watchFailed:    "o/r#5",
	}
	for filter, keys := range want {
		m.setFilter(filter)
		if got := strings.Join(watchItemKeys(m.visibleItems()), " "); got != keys {
			t.Errorf("filter %q = %q, want %q", filter, got, keys)
		}
	}
	sched := m.items[0]
	if sched.nextRun != "2026-11-23T12:30:00Z" || !sched.hasJob || sched.job.Number != 900 {
		t.Errorf("scheduled item = %+v", sched)
	}
}

func TestWatchTUIScheduledPromptShowsUnderItsRunsState(t *testing.T) {
	tools := newFakeWatchTools()
	list := tools.responses["project_job_list"].(map[string]any)
	jobs := list["jobs"].([]map[string]any)
	jobs[5]["state"], jobs[5]["status"] = "running", "active"
	m := loadedWatchModel(t, tools)
	m.setFilter(watchRunning)
	if got := strings.Join(watchItemKeys(m.visibleItems()), " "); got != "task:bot/nightly o/r#3" {
		t.Errorf("running = %q", got)
	}
	// On the board it stays in the Scheduled column.
	m.setFilter("")
	cols := m.columns()
	if cols[0].state != watchScheduled || len(cols[0].items) != 1 || len(cols[3].items) != 1 {
		t.Errorf("columns = %+v", cols)
	}
}

func TestWatchTUIKeysSelectFilters(t *testing.T) {
	m := loadedWatchModel(t, newFakeWatchTools())
	m, _ = watchKey(t, m, "4")
	if m.filter != watchRunning {
		t.Errorf("4 selects %q", m.filter)
	}
	m, _ = watchKey(t, m, "f")
	if m.filter != watchDone {
		t.Errorf("f selects %q", m.filter)
	}
	m, _ = watchKey(t, m, "F")
	m, _ = watchKey(t, m, "0")
	if m.filter != "" {
		t.Errorf("0 selects %q", m.filter)
	}
}

func TestWatchTUIBoardRendersColumns(t *testing.T) {
	m := loadedWatchModel(t, newFakeWatchTools())
	m, _ = watchKey(t, m, "b")
	if !m.board {
		t.Fatal("b did not switch to the board")
	}
	view := watchStrip(m.View())
	lines := strings.Split(view, "\n")
	if len(lines) != 40 {
		t.Errorf("view has %d lines, want 40", len(lines))
	}
	var header string
	for _, line := range lines {
		if strings.Contains(line, "Scheduled 1") {
			header = line
			break
		}
	}
	prev := -1
	for _, col := range []string{"Scheduled 1", "Starting 1", "Paused 1", "Running 1", "Done 1", "Failed 1"} {
		i := strings.Index(header, col)
		if i <= prev {
			t.Fatalf("column %q not in order in %q", col, header)
		}
		prev = i
	}
	if !strings.Contains(view, "#3 Running work") || !strings.Contains(view, "o/r/nightly") {
		t.Errorf("board misses cards:\n%s", view)
	}
	// Right moves to the Starting column's job.
	m, _ = watchKey(t, m, "right")
	if it, ok := m.selectedItem(); !ok || it.key != "o/r#1" {
		t.Errorf("selected %+v", it)
	}
}

func TestWatchTUIStatsLine(t *testing.T) {
	m := loadedWatchModel(t, newFakeWatchTools())
	got := watchStrip(m.statsLine())
	want := "2/4 slots · 1 starting · 1 paused · 1 running · 1 done · 1 failed · 3 merged 24h · poll"
	if got != want {
		t.Errorf("stats = %q, want %q", got, want)
	}
}

func TestWatchTUIHeaderShowsWebURL(t *testing.T) {
	tools := newFakeWatchTools()
	m := newWatchModel(context.Background(), tools.call, "o/r", "https://localhost:16677/projects")
	m = watchUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 20})
	if got := watchStrip(m.headerLine()); !strings.HasSuffix(got, "https://localhost:16677/projects") || !strings.HasPrefix(got, "aviary watch · o/r") {
		t.Errorf("header = %q", got)
	}
}

func TestWatchTUIActionKeysCallTools(t *testing.T) {
	tools := newFakeWatchTools()
	m := loadedWatchModel(t, tools)
	m.setFilter(watchPaused)
	for _, tc := range []struct{ key, tool string }{
		{"r", "project_job_retry"},
		{"u", "project_job_resume"},
	} {
		var msg tea.Msg
		m, msg = watchKey(t, m, tc.key)
		if msg == nil {
			t.Fatalf("%s sent nothing; err %q", tc.key, m.errText)
		}
		m = watchUpdate(t, m, msg)
		calls := tools.callsTo(tc.tool)
		if len(calls) != 1 || calls[0].args["target"] != "o/r" || calls[0].args["number"] != 2 {
			t.Errorf("%s calls = %+v", tc.key, calls)
		}
		if !strings.Contains(m.message, "sent for o/r#2") {
			t.Errorf("message = %q", m.message)
		}
	}
	// A paused job cannot pause again; the list's actions say so.
	m, msg := watchKey(t, m, "p")
	if msg != nil || len(tools.callsTo("project_job_pause")) != 0 || !strings.Contains(m.errText, "cannot pause") {
		t.Errorf("pause on a paused job: msg %v err %q", msg, m.errText)
	}
	// Stop asks first: n cancels, y stops.
	m, msg = watchKey(t, m, "s")
	if msg != nil || m.confirm == nil || !strings.Contains(watchStrip(m.footerLine()), "stop o/r#2? y/n") {
		t.Fatalf("stop did not ask: %v", m.confirm)
	}
	m, _ = watchKey(t, m, "n")
	if m.confirm != nil || len(tools.callsTo("project_job_stop")) != 0 {
		t.Error("n did not cancel the stop")
	}
	m, _ = watchKey(t, m, "s")
	m, msg = watchKey(t, m, "y")
	if _, ok := msg.(watchActionMsg); !ok || len(tools.callsTo("project_job_stop")) != 1 {
		t.Errorf("y did not stop: %v", msg)
	}
	// Retry refused by the list shows its reason.
	m.setFilter(watchStarting)
	m, _ = watchKey(t, m, "r")
	if !strings.Contains(m.errText, "already queued") {
		t.Errorf("retry on queued = %q", m.errText)
	}
	m, msg = watchKey(t, m, "p")
	if msg == nil || tools.callsTo("project_job_pause")[0].args["number"] != 1 {
		t.Errorf("pause calls = %+v", tools.callsTo("project_job_pause"))
	}
}

func TestWatchTUIActionErrorShowsInStatusLine(t *testing.T) {
	tools := newFakeWatchTools()
	tools.errs = map[string]error{"project_job_retry": errors.New("boom")}
	m := loadedWatchModel(t, tools)
	m.setFilter(watchDone)
	m, msg := watchKey(t, m, "r")
	m = watchUpdate(t, m, msg)
	if got := watchStrip(m.footerLine()); !strings.Contains(got, "retry o/r#4: boom") {
		t.Errorf("footer = %q", got)
	}
}

func TestWatchTUIToolErrorsKeepLastData(t *testing.T) {
	tools := newFakeWatchTools()
	m := loadedWatchModel(t, tools)
	tools.errs = map[string]error{"project_job_list": errors.New("down")}
	m = watchPoll(t, m)
	if len(m.items) != 6 || !strings.Contains(m.errText, "project_job_list: down") {
		t.Errorf("items %d err %q", len(m.items), m.errText)
	}
	if !strings.Contains(watchStrip(m.View()), "project_job_list: down") {
		t.Error("view does not show the error")
	}
}

func TestWatchTUILogsAppendByOffset(t *testing.T) {
	tools := newFakeWatchTools()
	tools.logs = map[int][]string{0: {"one", "two"}, 2: {"three"}}
	m := loadedWatchModel(t, tools)
	if m.logOffset != 2 || strings.Join(m.logs, ",") != "one,two" {
		t.Fatalf("logs %v offset %d", m.logs, m.logOffset)
	}
	m = watchPoll(t, m)
	m = watchPoll(t, m)
	if strings.Join(m.logs, ",") != "one,two,three" || m.logOffset != 3 {
		t.Errorf("logs %v offset %d", m.logs, m.logOffset)
	}
	offsets := []any{}
	for _, c := range tools.callsTo("project_status") {
		offsets = append(offsets, c.args["log_offset"])
	}
	if len(offsets) != 3 || offsets[0] != 0 || offsets[1] != 2 || offsets[2] != 3 {
		t.Errorf("offsets = %v", offsets)
	}
	if view := watchStrip(m.View()); !strings.Contains(view, "Logs") || !strings.Contains(view, "three") {
		t.Errorf("view misses logs:\n%s", view)
	}
	m, _ = watchKey(t, m, "l")
	if strings.Contains(watchStrip(m.View()), "three") {
		t.Error("l did not hide the logs")
	}
}

func TestWatchTUIOutputPaneReadsTheSelectedJob(t *testing.T) {
	tools := newFakeWatchTools()
	tools.responses["project_job_get"] = map[string]any{"scrollback": "\x1b[32mbuilding\x1b[0m\r\nprogress 10%\rprogress 100%\n"}
	m := loadedWatchModel(t, tools)
	m.setFilter(watchRunning)
	m, msg := watchKey(t, m, "enter")
	m = watchUpdate(t, m, msg)
	calls := tools.callsTo("project_job_get")
	if len(calls) != 1 || calls[0].args["number"] != 3 {
		t.Fatalf("job_get calls = %+v", calls)
	}
	if strings.Join(m.output, "|") != "building|progress 100%" {
		t.Errorf("output = %q", m.output)
	}
	if !strings.Contains(watchStrip(m.View()), "Output · o/r#3") {
		t.Error("view misses the output pane")
	}
}

func TestWatchTUIQuitAndHelp(t *testing.T) {
	m := loadedWatchModel(t, newFakeWatchTools())
	m, _ = watchKey(t, m, "?")
	if !strings.Contains(m.View(), "aviary watch keys") {
		t.Error("? does not show help")
	}
	m, _ = watchKey(t, m, "x")
	if m.showHelp {
		t.Error("a key does not close help")
	}
	_, msg := watchKey(t, m, "q")
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("q = %T", msg)
	}
}

func TestWatchTUIResizeAndTick(t *testing.T) {
	tools := newFakeWatchTools()
	m := loadedWatchModel(t, tools)
	m = watchUpdate(t, m, tea.WindowSizeMsg{Width: 60, Height: 12})
	for _, line := range strings.Split(m.View(), "\n") {
		if w := lipgloss.Width(line); w > 60 {
			t.Errorf("line is %d wide: %q", w, watchStrip(line))
		}
	}
	if n := len(strings.Split(m.View(), "\n")); n != 12 {
		t.Errorf("view has %d lines", n)
	}
	next, cmd := m.Update(watchTickMsg(time.Now()))
	if !next.(watchModel).fetching || cmd == nil {
		t.Error("tick did not start a fetch")
	}
	// A tick while a fetch is in flight only schedules the next tick.
	_, _ = next.Update(watchTickMsg(time.Now()))
}

func TestWatchTUIAllProjectsBoardListsEverySchedule(t *testing.T) {
	// The all-projects board passes every watched target: each project's
	// tasks are one scheduled item, and a schedule's finished run is never
	// counted as Done (#1108).
	jobs := []watchJob{
		{Key: "a/one#1", Target: "a/one", Number: 1, Status: "active", State: "running", Kind: "issue"},
		{Key: "b/two#sched", Target: "b/two", ScheduleID: "bot/release", Status: "merged", State: "scheduled", Kind: "schedule"},
	}
	tasks := []watchTask{
		{ID: "bot/nightly", Name: "nightly", Type: "project", Project: "a/one", Enabled: true},
		{ID: "bot/release", Name: "release", Type: "project", Project: "b/two", Enabled: true},
		{ID: "bot/other", Name: "other", Type: "project", Project: "c/unwatched", Enabled: true},
	}
	m := watchModel{items: watchBuildItems(jobs, tasks, nil, []string{"a/one", "b/two"})}
	counts := map[string][]string{}
	for _, filter := range append([]string{""}, watchFilters...) {
		m.setFilter(filter)
		counts[filter] = watchItemKeys(m.visibleItems())
	}
	if got := strings.Join(counts[watchScheduled], " "); got != "task:bot/nightly task:bot/release" {
		t.Errorf("scheduled = %q", got)
	}
	if got := strings.Join(counts[""], " "); got != "task:bot/nightly task:bot/release a/one#1" {
		t.Errorf("all = %q", got)
	}
	if len(counts[watchDone]) != 0 {
		t.Errorf("done = %q", counts[watchDone])
	}
	projects := []string{}
	for _, it := range m.items[:2] {
		projects = append(projects, it.project)
	}
	if got := strings.Join(projects, " "); got != "a/one b/two" {
		t.Errorf("projects = %q", got)
	}
}
