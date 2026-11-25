package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestRunWatchTUI(t *testing.T) {
	tests := []struct {
		name string
		// input is what the user types; cancelled starts the TUI with an
		// ended session.
		input     string
		cancelled bool
	}{
		{name: "q quits", input: "q"},
		{name: "a cancelled session ends it", cancelled: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			setWatchTUITerminal(t, strings.NewReader(tt.input), &out)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancelled {
				cancel()
			}
			if err := runWatchTUI(ctx, newFakeWatchTools().call, nil, "o/r", ""); err != nil {
				t.Fatalf("runWatchTUI = %v", err)
			}
			got := out.String()
			if !strings.HasPrefix(got, watchModifyOtherKeysOn) || !strings.HasSuffix(got, watchModifyOtherKeysOff) {
				t.Errorf("the TUI did not switch modifyOtherKeys on and back off: %q", got)
			}
			if !tt.cancelled && !strings.Contains(got, "\x1b[?1049h") {
				t.Errorf("the TUI did not enter the alternate screen: %q", got)
			}
		})
	}
}

// setWatchTUITerminal runs the TUI on in and out until the test ends.
func setWatchTUITerminal(t *testing.T, in *strings.Reader, out *bytes.Buffer) {
	t.Helper()
	oldIn, oldOut := watchTUIInput, watchTUIOutput
	watchTUIInput, watchTUIOutput = in, out
	t.Cleanup(func() { watchTUIInput, watchTUIOutput = oldIn, oldOut })
}

func TestWatchBuildItemsScheduleEntriesWithoutTasks(t *testing.T) {
	soon, off := "2026-11-23T13:00:00Z", "2026-11-23T12:10:00Z"
	orphan := watchJob{Key: "o/r#7", Target: "o/r", Number: 7, Kind: "schedule", ScheduleID: "bot/orphan", Status: "failed", State: "failed"}
	orphan.NextRun = &struct {
		At     string `json:"at"`
		Paused bool   `json:"paused"`
	}{At: "2026-11-24T00:00:00Z", Paused: true}
	jobs := []watchJob{
		orphan,
		{Key: "o/r#8", Target: "o/r", Number: 8, Kind: "schedule", ScheduleID: "bot/late"},
	}
	tasks := []watchTask{
		{ID: "bot/idle", Name: "idle", Type: "project", Project: "o/r", Enabled: true},
		{ID: "bot/soon", Name: "soon", Type: "project", Project: "o/r", Enabled: true, NextRun: &soon},
		// A disabled task's next run is not shown.
		{ID: "bot/off", Name: "off", Type: "project", Project: "o/r", NextRun: &off},
	}
	items := watchBuildItems(jobs, tasks, nil, []string{"o/r"})
	// Soonest first, then the ones with no next run by name.
	if got := strings.Join(watchItemKeys(items), " "); got != "task:bot/soon o/r#7 o/r#8 task:bot/idle task:bot/off" {
		t.Fatalf("items = %q", got)
	}
	if it := items[1]; it.name != "bot/orphan" || it.project != "o/r" || !it.disabled || it.scheduleState != watchFailed || !it.inFilter(watchFailed) {
		t.Errorf("a schedule entry with no task = %+v", it)
	}
	if it := items[2]; it.scheduleState != watchScheduled || it.disabled || it.nextRun != "" {
		t.Errorf("an idle schedule entry = %+v", it)
	}
	if it := items[4]; !it.disabled || it.nextRun != "" {
		t.Errorf("a disabled task = %+v", it)
	}
}

func TestWatchItemActionAvailable(t *testing.T) {
	ran := watchJob{Target: "o/r", Number: 3, Status: "active", State: "running"}
	stale := ran
	stale.Stale = true
	tests := []struct {
		name string
		item watchItem
		verb string
		ok   bool
		why  string
	}{
		{name: "scheduled prompt that never ran", item: watchItem{state: watchScheduled}, verb: "retry", why: "it has not run yet"},
		{name: "job with no number", item: watchItem{hasJob: true, job: watchJob{Target: "o/r"}, state: watchRunning}, verb: "stop", why: "it has not run yet"},
		{name: "stale job", item: watchItem{hasJob: true, job: stale, state: watchRunning}, verb: "retry", why: "waiting for the first poll since the restart"},
		{name: "retry the list does not rule out", item: watchItem{hasJob: true, job: ran, state: watchRunning}, verb: "retry", ok: true},
		{name: "dismiss a failed schedule", item: watchItem{hasJob: true, job: ran, state: watchScheduled, scheduleState: watchFailed}, verb: "dismiss", ok: true},
		{name: "dismiss a running schedule", item: watchItem{hasJob: true, job: ran, state: watchScheduled, scheduleState: watchRunning}, verb: "dismiss", why: "only a failed scheduled run can be dismissed"},
		{name: "dismiss a failed job", item: watchItem{hasJob: true, job: watchJob{Number: 5, Status: "failed"}, state: watchFailed}, verb: "dismiss", why: "only a failed scheduled run can be dismissed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, why := tt.item.actionAvailable(tt.verb)
			if ok != tt.ok || why != tt.why {
				t.Errorf("actionAvailable(%q) = %v, %q; want %v, %q", tt.verb, ok, why, tt.ok, tt.why)
			}
		})
	}
}

func TestWatchTUIFailedToolKeepsItsLastData(t *testing.T) {
	tests := []struct {
		tool string
		// resp replaces the tool's response, else the tool fails.
		resp any
		want string
		kept func(watchModel) bool
	}{
		{tool: "project_status", want: "project_status: boom", kept: func(m watchModel) bool { return m.hasStatus && m.status.Concurrency == 4 }},
		{tool: "project_job_list", resp: "not a job list", want: "project_job_list: decode: ", kept: func(m watchModel) bool { return len(m.jobs) == 6 }},
		{tool: "task_list", want: "task_list: boom", kept: func(m watchModel) bool { return len(m.items) > 0 && m.items[0].key == "task:bot/nightly" }},
		{tool: "task_upcoming", want: "task_upcoming: boom", kept: func(m watchModel) bool { return m.items[0].nextRun == "2026-11-23T12:30:00Z" }},
		{tool: "project_job_get", want: "project_job_get: boom", kept: func(m watchModel) bool { return strings.Join(m.output, "|") == "building" }},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			tools := newFakeWatchTools()
			tools.responses["project_job_get"] = map[string]any{"scrollback": "building\n"}
			m := loadedWatchModel(t, tools)
			m.setFilter(watchRunning)
			m, msg := watchKey(t, m, "enter")
			m = watchUpdate(t, m, msg)
			if tt.resp != nil {
				tools.responses[tt.tool] = tt.resp
			} else {
				tools.errs = map[string]error{tt.tool: errors.New("boom")}
			}
			m = watchPoll(t, m)
			if !strings.Contains(m.errText, tt.want) {
				t.Errorf("errText = %q, want %q", m.errText, tt.want)
			}
			if !tt.kept(m) {
				t.Errorf("a failed %s dropped what the TUI showed", tt.tool)
			}
		})
	}
}

func TestWatchTUIApplyDataLogOffsets(t *testing.T) {
	many := make([]string, watchMaxLogLines+3)
	for i := range many {
		many[i] = fmt.Sprintf("line %d", i)
	}
	tests := []struct {
		name       string
		logs       []string
		offset     int
		fromOffset int
		status     watchStatus
		wantLogs   []string
		wantOffset int
	}{
		{
			name: "poll from an older offset is dropped", logs: []string{"a", "b"}, offset: 2, fromOffset: 1,
			status: watchStatus{RecentLogs: []string{"b"}, LogOffset: 2}, wantLogs: []string{"a", "b"}, wantOffset: 2,
		},
		{
			name: "offset going back replaces the log", logs: []string{"a", "b", "c"}, offset: 3, fromOffset: 3,
			status: watchStatus{RecentLogs: []string{"x"}, LogOffset: 1}, wantLogs: []string{"x"}, wantOffset: 1,
		},
		{
			name: "log keeps its newest lines", fromOffset: 0,
			status: watchStatus{RecentLogs: many, LogOffset: len(many)}, wantLogs: many[3:], wantOffset: len(many),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := watchModel{logs: tt.logs, logOffset: tt.offset}
			status := tt.status
			m = m.applyData(watchDataMsg{status: &status, fromOffset: tt.fromOffset})
			if strings.Join(m.logs, ",") != strings.Join(tt.wantLogs, ",") || m.logOffset != tt.wantOffset {
				t.Errorf("logs %d lines starting %q, offset %d; want %d lines, offset %d", len(m.logs), firstNonEmpty(m.logs...), m.logOffset, len(tt.wantLogs), tt.wantOffset)
			}
			if !m.hasStatus {
				t.Error("the status was not taken in")
			}
		})
	}
}

func TestWatchTUICursorKeys(t *testing.T) {
	// The list reads task:bot/nightly o/r#3 o/r#5 o/r#2 o/r#1 o/r#4; the
	// board's columns are Scheduled, Starting, Paused, Running, Done, Failed.
	tests := []struct {
		name  string
		board bool
		keys  []string
		want  string
	}{
		{name: "down and j", keys: []string{"down", "j"}, want: "o/r#5"},
		{name: "up and k", keys: []string{"G", "up", "k"}, want: "o/r#2"},
		{name: "end", keys: []string{"end"}, want: "o/r#4"},
		{name: "home and g", keys: []string{"G", "home", "down", "g"}, want: "task:bot/nightly"},
		{name: "down stops at the last item", keys: []string{"G", "down", "j"}, want: "o/r#4"},
		{name: "columns move only on the board", keys: []string{"down", "right", "tab", "left"}, want: "o/r#3"},
		{name: "board tab", board: true, keys: []string{"tab", "tab"}, want: "o/r#2"},
		{name: "board shift+tab and left", board: true, keys: []string{"tab", "tab", "tab", "shift+tab", "left"}, want: "o/r#1"},
		{name: "board stops at the last column", board: true, keys: []string{"right", "right", "right", "right", "right", "right", "tab"}, want: "o/r#5"},
		{name: "board stops at the first column", board: true, keys: []string{"left", "shift+tab"}, want: "task:bot/nightly"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := loadedWatchModel(t, newFakeWatchTools())
			if tt.board {
				m, _ = watchKey(t, m, "b")
			}
			for _, key := range tt.keys {
				m, _ = watchKey(t, m, key)
			}
			if it, ok := m.selectedItem(); !ok || it.key != tt.want || m.selected != tt.want {
				t.Errorf("selected %q (remembered %q), want %q", it.key, m.selected, tt.want)
			}
		})
	}
}

func TestWatchTUIOutputPaneFollowsTheCursor(t *testing.T) {
	tools := newFakeWatchTools()
	m := loadedWatchModel(t, tools)
	m, _ = watchKey(t, m, "down")
	m, msg := watchKey(t, m, "enter")
	m = watchUpdate(t, m, msg)
	if m.outputKey != "o/r#3" {
		t.Fatalf("output pane on %q", m.outputKey)
	}
	m, _ = watchKey(t, m, "j")
	if m.outputKey != "o/r#5" || m.output != nil {
		t.Errorf("after moving, the output pane is on %q holding %q", m.outputKey, m.output)
	}
}

func TestWatchTUILogScrollKeys(t *testing.T) {
	m := loadedWatchModel(t, newFakeWatchTools())
	for i := range 100 {
		m.logs = append(m.logs, fmt.Sprintf("log %d", i))
	}
	page := m.logPaneHeight() - 1
	if page < 1 {
		t.Fatalf("log pane height %d", m.logPaneHeight())
	}
	m, _ = watchKey(t, m, "pgup")
	if m.logScroll != page || !strings.Contains(watchStrip(m.View()), fmt.Sprintf("Logs (scrolled back %d)", page)) {
		t.Errorf("pgup scrolled to %d, want %d", m.logScroll, page)
	}
	for range 100 {
		m, _ = watchKey(t, m, "pgup")
	}
	if m.logScroll != 99 {
		t.Errorf("pgup scrolled past the oldest line: %d", m.logScroll)
	}
	m.logScroll = page + 1
	m, _ = watchKey(t, m, "pgdown")
	if m.logScroll != 1 {
		t.Errorf("pgdown scrolled to %d, want 1", m.logScroll)
	}
	m, _ = watchKey(t, m, "pgdown")
	if m.logScroll != 0 || strings.Contains(watchStrip(m.View()), "scrolled back") {
		t.Errorf("pgdown did not stop at the newest lines: %d", m.logScroll)
	}
}

func TestWatchTUIQuitKeys(t *testing.T) {
	tests := []struct {
		name  string
		setup func(watchModel) watchModel
		key   string
	}{
		{name: "ctrl+c", setup: func(m watchModel) watchModel { return m }, key: "ctrl+c"},
		{name: "q with the help open", setup: func(m watchModel) watchModel { m.showHelp = true; return m }, key: "q"},
		{name: "ctrl+c while a stop waits for y/n", setup: func(m watchModel) watchModel {
			m.setFilter(watchRunning)
			m, _ = watchKey(t, m, "s")
			return m
		}, key: "ctrl+c"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools := newFakeWatchTools()
			m := tt.setup(loadedWatchModel(t, tools))
			_, msg := watchKey(t, m, tt.key)
			if _, ok := msg.(tea.QuitMsg); !ok {
				t.Errorf("%s = %T, want quit", tt.key, msg)
			}
			if calls := tools.callsTo("project_job_stop"); len(calls) != 0 {
				t.Errorf("quitting stopped a job: %+v", calls)
			}
		})
	}
}

func TestWatchTUIActionWithNoSelection(t *testing.T) {
	tests := []struct {
		name  string
		model func(watchModel) watchModel
	}{
		{name: "empty board", model: func(m watchModel) watchModel { return m }},
		{name: "column past the filter's one", model: func(m watchModel) watchModel {
			m.board, m.filter, m.col = true, watchDone, 2
			return m
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools := newFakeWatchTools()
			m := newWatchModel(context.Background(), tools.call, "o/r", "")
			if tt.name != "empty board" {
				m = watchPoll(t, m)
			}
			m = tt.model(m)
			m, msg := watchKey(t, m, "r")
			if msg != nil || m.errText != "no job selected" || len(tools.callsTo("project_job_retry")) != 0 {
				t.Errorf("r with nothing selected: msg %v, err %q", msg, m.errText)
			}
		})
	}
}

func TestWatchTUIInitFetches(t *testing.T) {
	tools := newFakeWatchTools()
	m := newWatchModel(context.Background(), tools.call, "o/r", "")
	batch, ok := m.Init()().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("Init = %v, want a fetch and a tick", batch)
	}
	// The first command is the fetch; the second waits for the next tick.
	data, ok := batch[0]().(watchDataMsg)
	if !ok || data.status == nil || !data.jobsOK || len(tools.callsTo("project_status")) != 1 {
		t.Errorf("Init's fetch = %+v", data)
	}
}

func TestWatchTUITickFetchesTheOpenChat(t *testing.T) {
	for _, chat := range []bool{false, true} {
		t.Run(fmt.Sprintf("chat open %v", chat), func(t *testing.T) {
			m := loadedWatchModel(t, newFakeWatchTools())
			if chat {
				var msg tea.Msg
				m, msg = watchKey(t, m, "c")
				m = watchUpdate(t, m, msg)
			}
			if m.chat.fetching {
				t.Fatal("the chat is still fetching")
			}
			m = watchUpdate(t, m, watchTickMsg(time.Now()))
			if !m.fetching || m.chat.fetching != chat {
				t.Errorf("tick: fetching %v, chat fetching %v; want true, %v", m.fetching, m.chat.fetching, chat)
			}
		})
	}
}

func TestWatchTUIRefetchesAfterTheFetchInFlight(t *testing.T) {
	tools := newFakeWatchTools()
	m := loadedWatchModel(t, tools)
	m.fetching = true
	next, cmd := m.Update(watchActionMsg{verb: "retry", key: "o/r#4"})
	m = next.(watchModel)
	if cmd != nil || !m.refetch {
		t.Fatalf("an action during a fetch started another: cmd %v refetch %v", cmd, m.refetch)
	}
	before := len(tools.callsTo("project_status"))
	next, cmd = m.Update(watchDataMsg{})
	m = next.(watchModel)
	if cmd == nil || !m.fetching || m.refetch {
		t.Fatalf("the finished fetch did not start the queued one: fetching %v refetch %v", m.fetching, m.refetch)
	}
	if _, ok := cmd().(watchDataMsg); !ok || len(tools.callsTo("project_status")) != before+1 {
		t.Error("the queued fetch did not read the engine")
	}
}

func TestWatchTUIIgnoresUnknownMessages(t *testing.T) {
	m := loadedWatchModel(t, newFakeWatchTools())
	view := m.View()
	next, cmd := m.Update(struct{ other string }{"x"})
	if cmd != nil || next.(watchModel).View() != view {
		t.Error("an unknown message changed the TUI")
	}
}
