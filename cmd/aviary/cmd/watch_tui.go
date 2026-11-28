package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// watchToolCaller calls an Aviary MCP tool in-process and returns its text result.
type watchToolCaller func(ctx context.Context, name string, args map[string]any) (string, error)

// watchTUIInput and watchTUIOutput are the terminal the TUI reads keys from
// and draws on: a nil input is the one Bubble Tea picks, standard input or
// the controlling TTY. Tests replace them.
var (
	watchTUIInput  io.Reader
	watchTUIOutput io.Writer = os.Stdout
)

// runWatchTUI runs the full-screen watch TUI until the user quits (q / Ctrl+C) or ctx is cancelled. stream runs the prompt chat's turns; title is a short header such as the watched targets; webURL is the session's web UI URL or "" when it is off.
func runWatchTUI(ctx context.Context, call watchToolCaller, stream watchStreamCaller, title, webURL string) error {
	model := newWatchModel(ctx, call, title, webURL)
	model.stream = stream
	opts := []tea.ProgramOption{tea.WithAltScreen(), tea.WithContext(ctx), tea.WithOutput(watchTUIOutput)}
	if watchTUIInput != nil {
		opts = append(opts, tea.WithInput(watchTUIInput))
	}
	p := tea.NewProgram(model, opts...)
	// Ask the terminal to tell Shift+Enter from Enter, so the prompt chat
	// can add a line with it, and restore its default when the TUI ends.
	_, _ = fmt.Fprint(watchTUIOutput, watchModifyOtherKeysOn)
	defer func() { _, _ = fmt.Fprint(watchTUIOutput, watchModifyOtherKeysOff) }()
	_, err := p.Run()
	if err != nil && ctx.Err() != nil && errors.Is(err, tea.ErrProgramKilled) {
		// A cancelled session ends the TUI the way quitting does.
		return nil
	}
	return err
}

// xterm's modifyOtherKeys mode 1 reports modified keys with no legacy code
// of their own, such as Shift+Enter, as CSI 27;2;13~ and keeps Ctrl+C and
// the other legacy keys as they are.
const (
	watchModifyOtherKeysOn  = "\x1b[>4;1m"
	watchModifyOtherKeysOff = "\x1b[>4m"
)

// watchShiftEnter reports whether msg is Shift+Enter in modifyOtherKeys or
// CSI u (kitty keyboard protocol) form. Bubble Tea v1 has no key for either,
// so it hands them over as an unknown CSI sequence, a byte slice.
func watchShiftEnter(msg tea.Msg) bool {
	v := reflect.ValueOf(msg)
	if v.Kind() != reflect.Slice || v.Type().Elem().Kind() != reflect.Uint8 {
		return false
	}
	seq := string(v.Bytes())
	return seq == "\x1b[27;2;13~" || seq == "\x1b[13;2u"
}

// watchPollInterval is how often the TUI re-reads the engine, as the web
// board's PROJECTS_POLL_MS does.
const watchPollInterval = 2 * time.Second

// watchJobLimit is the page size of project_job_list, enough for every job.
const watchJobLimit = 500

// watchMaxLogLines bounds the log lines the TUI keeps.
const watchMaxLogLines = 2000

// watchOutputLines is the scrollback the output pane reads for its job.
const watchOutputLines = 200

// The board's filters, in the web board's chip order: its scheduled items,
// then the five job states. The empty filter shows everything.
const (
	watchScheduled = "scheduled"
	watchStarting  = "starting"
	watchPaused    = "paused"
	watchRunning   = "running"
	watchDone      = "done"
	watchFailed    = "failed"
)

// watchBoardStates are the five job states, as internal/projects/core's
// BoardStates lists them.
var watchBoardStates = []string{watchStarting, watchPaused, watchRunning, watchDone, watchFailed}

// watchFilters are the filters keys 1 to 6 select; 0 selects all.
var watchFilters = []string{watchScheduled, watchStarting, watchPaused, watchRunning, watchDone, watchFailed}

// watchStatus is the part of project_status the TUI shows.
type watchStatus struct {
	Identity    string         `json:"identity"`
	Running     bool           `json:"running"`
	Targets     []string       `json:"targets"`
	IssueCounts map[string]int `json:"issue_counts"`
	States      map[string]int `json:"states"`
	Jobs        struct {
		Running   int `json:"running"`
		Queued    int `json:"queued"`
		Paused    int `json:"paused"`
		Completed int `json:"completed"`
		Merged24h int `json:"merged_24h"`
		Failed    int `json:"failed"`
	} `json:"jobs"`
	Concurrency   int      `json:"concurrency"`
	NoMerge       bool     `json:"no_merge"`
	Delivery      string   `json:"delivery"`
	PollInterval  string   `json:"poll_interval"`
	LastCheck     string   `json:"last_check"`
	Error         string   `json:"error"`
	Stale         bool     `json:"stale"`
	WebhookOnline *bool    `json:"webhook_online"`
	RecentLogs    []string `json:"recent_logs"`
	LogOffset     int      `json:"log_offset"`
}

// watchJob is one entry of project_job_list: a job, or a schedule's one
// entry, its latest run with the schedule's last and next run.
type watchJob struct {
	Key              string          `json:"key"`
	Target           string          `json:"target"`
	Number           int             `json:"number"`
	Title            string          `json:"title"`
	Status           string          `json:"status"`
	State            string          `json:"state"`
	Kind             string          `json:"kind"`
	Agent            string          `json:"agent"`
	Model            string          `json:"model"`
	Repository       string          `json:"repository"`
	ScheduleID       string          `json:"schedule_id"`
	Summary          string          `json:"summary"`
	QueuePosition    int             `json:"queue_position"`
	PullRequest      int             `json:"pull_request"`
	PullRequestState string          `json:"pull_request_state"`
	Started          string          `json:"started"`
	WaitReason       string          `json:"wait_reason"`
	WakeAt           string          `json:"wake_at"`
	Stale            bool            `json:"stale"`
	Actions          map[string]bool `json:"actions"`
	RetryUnavailable string          `json:"retry_unavailable"`
	Progress         *struct {
		Done    int `json:"done"`
		Total   int `json:"total"`
		Percent int `json:"percent"`
	} `json:"progress"`
	LastRun *struct {
		Result    string `json:"result"`
		Dismissed bool   `json:"dismissed"`
	} `json:"last_run"`
	NextRun *struct {
		At     string `json:"at"`
		Paused bool   `json:"paused"`
	} `json:"next_run"`
}

// watchTask is a task_list entry; project tasks are the board's scheduled
// prompts.
type watchTask struct {
	ID          string  `json:"id"`
	AgentName   string  `json:"agent_name"`
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Project     string  `json:"project"`
	Enabled     bool    `json:"enabled"`
	Description string  `json:"description"`
	NextRun     *string `json:"next_run"`
}

// watchUpcoming is a task_upcoming entry: one next fire of a task.
type watchUpcoming struct {
	TaskID  string `json:"task_id"`
	Name    string `json:"name"`
	Project string `json:"project"`
	At      string `json:"at"`
}

// watchItem is one box on the board: a job, or a project's scheduled prompt
// with its schedule's latest run, if it has run.
type watchItem struct {
	key string
	// job is the job, or a schedule's latest run; hasJob is false for a
	// scheduled prompt that has not run yet.
	job    watchJob
	hasJob bool
	// state is the job's board state, or watchScheduled for a scheduled
	// prompt.
	state string
	// scheduleState is a scheduled prompt's schedule state: starting or
	// running while a run is in flight, failed while its last run failed,
	// and scheduled otherwise.
	scheduleState string
	// name, project, nextRun and disabled describe a scheduled prompt.
	name     string
	project  string
	nextRun  string
	disabled bool
}

// scheduled reports whether the item is a scheduled prompt.
func (it watchItem) scheduled() bool { return it.state == watchScheduled }

// inFilter reports whether the item shows under filter, as the web board
// filters: a scheduled prompt shows under Scheduled and under its schedule's
// job state, so a running or failed schedule shows with running or failed
// jobs.
func (it watchItem) inFilter(filter string) bool {
	switch {
	case filter == "":
		return true
	case filter == watchScheduled:
		return it.scheduled()
	case it.scheduled():
		return it.scheduleState == filter
	}
	return it.state == filter
}

// watchJobBoardState is a job's board state: the state project_job_list
// reports, or one derived from its status as core's BoardState does.
func watchJobBoardState(job watchJob) string {
	if slices.Contains(watchBoardStates, job.State) {
		return job.State
	}
	switch job.Status {
	case "queued":
		return watchStarting
	case "waiting", "paused":
		return watchPaused
	case "complete", "completed":
		return watchDone
	case "failed":
		return watchFailed
	}
	return watchRunning
}

// watchStatusOrder orders jobs running and errored first, as the web
// board's sortJobs does.
var watchStatusOrder = map[string]int{"active": 0, "stopping": 0, "failed": 1, "waiting": 2, "paused": 3, "queued": 4, "complete": 5}

// watchBuildItems turns the job list and the project tasks into the board's
// items. Each project task of the watched targets is one scheduled item
// carrying its schedule's entry from the job list; a schedule entry with no
// task is a scheduled item of its own. Other jobs keep their board state.
func watchBuildItems(jobs []watchJob, tasks []watchTask, upcoming []watchUpcoming, targets []string) []watchItem {
	entries := map[string]watchJob{}
	var items []watchItem
	for _, job := range jobs {
		if job.Kind == "schedule" && job.ScheduleID != "" {
			entries[job.ScheduleID] = job
			continue
		}
		items = append(items, watchItem{key: watchJobKey(job), job: job, hasJob: true, state: watchJobBoardState(job)})
	}
	slices.SortStableFunc(items, func(a, b watchItem) int {
		if d := watchStatusOrder[a.job.Status] - watchStatusOrder[b.job.Status]; d != 0 {
			return d
		}
		return strings.Compare(b.job.Started, a.job.Started)
	})
	scheduled := watchScheduledTaskItems(tasks, watchNextRuns(upcoming), entries, targets)
	for _, entry := range entries {
		it := watchItem{key: watchJobKey(entry), job: entry, hasJob: true, state: watchScheduled, scheduleState: watchScheduleState(entry), name: entry.ScheduleID, project: entry.Target}
		if entry.NextRun != nil {
			it.nextRun, it.disabled = entry.NextRun.At, entry.NextRun.Paused
		}
		scheduled = append(scheduled, it)
	}
	slices.SortFunc(scheduled, watchCompareScheduled)
	return append(scheduled, items...)
}

// watchNextRuns maps each task ID to its first upcoming fire.
func watchNextRuns(upcoming []watchUpcoming) map[string]string {
	next := map[string]string{}
	for _, u := range upcoming {
		if _, ok := next[u.TaskID]; !ok {
			next[u.TaskID] = u.At
		}
	}
	return next
}

// watchScheduledTaskItems builds one scheduled item per project task of the
// watched targets, taking its schedule entry out of entries.
func watchScheduledTaskItems(tasks []watchTask, next map[string]string, entries map[string]watchJob, targets []string) []watchItem {
	var scheduled []watchItem
	for _, task := range tasks {
		if task.Type != "project" || (len(targets) > 0 && !slices.Contains(targets, task.Project)) {
			continue
		}
		it := watchItem{key: "task:" + task.ID, state: watchScheduled, scheduleState: watchScheduled, name: task.Name, project: task.Project, disabled: !task.Enabled}
		it.nextRun = next[task.ID]
		if it.nextRun == "" && task.NextRun != nil && task.Enabled {
			it.nextRun = *task.NextRun
		}
		if entry, ok := entries[task.ID]; ok {
			delete(entries, task.ID)
			it.job, it.hasJob, it.scheduleState = entry, true, watchScheduleState(entry)
		}
		scheduled = append(scheduled, it)
	}
	return scheduled
}

// watchCompareScheduled orders scheduled items soonest first, then by name;
// ones with no next run last.
func watchCompareScheduled(a, b watchItem) int {
	switch {
	case a.nextRun == "" && b.nextRun != "":
		return 1
	case a.nextRun != "" && b.nextRun == "":
		return -1
	}
	if c := strings.Compare(a.nextRun, b.nextRun); c != 0 {
		return c
	}
	return strings.Compare(a.name, b.name)
}

// watchScheduleState is a schedule entry's state: scheduled between runs,
// else its latest run's job state.
func watchScheduleState(entry watchJob) string {
	if entry.State == watchScheduled || entry.State == "" {
		return watchScheduled
	}
	return watchJobBoardState(entry)
}

func watchJobKey(job watchJob) string {
	if job.Key != "" {
		return job.Key
	}
	return fmt.Sprintf("%s#%d", job.Target, job.Number)
}

// watchActions are the job actions, their keys and the tools they call.
var watchActions = []struct{ key, verb, tool string }{
	{"r", "retry", "project_job_retry"},
	{"p", "pause", "project_job_pause"},
	{"u", "resume", "project_job_resume"},
	{"s", "stop", "project_job_stop"},
	{"d", "dismiss", "project_job_dismiss"},
	{"x", "clear", "project_job_clear"},
}

// actionAvailable reports whether the item accepts verb, from the actions
// project_job_list reports, as the web board offers them: retry unless the
// list says no, the rest only when it says yes. Dismiss takes a schedule's
// failed last run off the Failed state.
func (it watchItem) actionAvailable(verb string) (bool, string) {
	if !it.hasJob || it.job.Number == 0 {
		return false, "it has not run yet"
	}
	if it.job.Stale {
		return false, "waiting for the first poll since the restart"
	}
	switch verb {
	case "retry":
		if ok, set := it.job.Actions["retry"]; set && !ok {
			return false, firstNonEmpty(it.job.RetryUnavailable, "the job is not retryable now")
		}
		return true, ""
	case "dismiss":
		if it.scheduled() && it.scheduleState == watchFailed {
			return true, ""
		}
		return false, "only a failed scheduled run can be dismissed"
	case "clear":
		// Clearing removes a done or failed job from the board and the
		// job store (issue #1460); a schedule's runs are dismissed instead.
		if state := watchJobBoardState(it.job); !it.scheduled() && (state == watchDone || state == watchFailed) {
			return true, ""
		}
		return false, "only a done or failed job can be cleared"
	}
	if it.job.Actions[verb] {
		return true, ""
	}
	return false, fmt.Sprintf("a %s job cannot %s", watchJobBoardState(it.job), verb)
}

// watchModel is the watch TUI's bubbletea model.
type watchModel struct {
	ctx    context.Context
	call   watchToolCaller
	stream watchStreamCaller
	title  string
	webURL string
	now    func() time.Time

	width, height int

	status    watchStatus
	hasStatus bool
	jobs      []watchJob
	tasks     []watchTask
	upcoming  []watchUpcoming
	items     []watchItem
	loaded    bool

	logs      []string
	logOffset int
	logScroll int
	showLogs  bool

	// filter is "" for all, or one of watchFilters.
	filter string
	board  bool
	// cursor is the selected row: of the list, or of column col's items on
	// the board. selected keeps the selection on the same item across
	// refreshes.
	cursor   int
	col      int
	selected string

	// output is the selected job's scrollback while the output pane is
	// open on outputKey.
	showOutput bool
	outputKey  string
	output     []string

	showHelp bool
	confirm  *watchPendingAction
	// queueInput is the queue prompt while it is open.
	queueInput *textinput.Model

	// chat is the prompt chat pane.
	chat watchChat

	fetching bool
	refetch  bool
	message  string
	errText  string
}

// watchPendingAction is a stop waiting for the user's y/n.
type watchPendingAction struct {
	tool, verb string
	item       watchItem
}

type watchTickMsg time.Time

// watchDataMsg is one poll's results; each part is nil when its tool failed,
// so the TUI keeps what it showed.
type watchDataMsg struct {
	status     *watchStatus
	jobs       []watchJob
	jobsOK     bool
	tasks      []watchTask
	tasksOK    bool
	upcoming   []watchUpcoming
	upcomingOK bool
	fromOffset int
	outputKey  string
	output     []string
	outputOK   bool
	errs       []string
}

// watchActionMsg is a finished job action.
type watchActionMsg struct {
	verb string
	key  string
	err  error
}

func newWatchModel(ctx context.Context, call watchToolCaller, title, webURL string) watchModel {
	return watchModel{ctx: ctx, call: call, title: title, webURL: webURL, now: time.Now, showLogs: true, chat: newWatchChat()}
}

func (m watchModel) Init() tea.Cmd {
	return tea.Batch(m.fetchCmd(), watchTick())
}

func watchTick() tea.Cmd {
	return tea.Tick(watchPollInterval, func(t time.Time) tea.Msg { return watchTickMsg(t) })
}

// callJSON calls a tool and decodes its JSON result into out.
func (m watchModel) callJSON(name string, args map[string]any, out any) error {
	text, err := m.call(m.ctx, name, args)
	if err != nil {
		return err
	}
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(text), out); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	return nil
}

// fetchCmd reads the engine once: its status and new log lines, every job,
// the project tasks and their next fires, and the output pane's job.
func (m watchModel) fetchCmd() tea.Cmd {
	offset := m.logOffset
	outputKey, target, number := "", "", 0
	if it, ok := m.selectedItem(); ok && m.showOutput && it.hasJob && it.job.Number > 0 {
		outputKey, target, number = it.key, it.job.Target, it.job.Number
	}
	return func() tea.Msg {
		msg := watchDataMsg{fromOffset: offset, outputKey: outputKey}
		// fetch calls tool and records its error, reporting whether it worked.
		fetch := func(tool string, args map[string]any, out any) bool {
			if err := m.callJSON(tool, args, out); err != nil {
				msg.errs = append(msg.errs, tool+": "+err.Error())
				return false
			}
			return true
		}
		var status watchStatus
		if fetch("project_status", map[string]any{"log_offset": offset}, &status) {
			msg.status = &status
		}
		var list struct {
			Jobs []watchJob `json:"jobs"`
		}
		if fetch("project_job_list", map[string]any{"limit": watchJobLimit}, &list) {
			msg.jobs, msg.jobsOK = list.Jobs, true
		}
		msg.tasksOK = fetch("task_list", map[string]any{}, &msg.tasks)
		msg.upcomingOK = fetch("task_upcoming", map[string]any{"limit": 50, "per_task": 1}, &msg.upcoming)
		if outputKey == "" {
			return msg
		}
		var detail struct {
			Scrollback string `json:"scrollback"`
		}
		args := map[string]any{"target": target, "number": number, "lines": watchOutputLines, "events": 1}
		if fetch("project_job_get", args, &detail) {
			msg.output, msg.outputOK = watchSplitLines(detail.Scrollback), true
		}
		return msg
	}
}

// refresh starts a fetch now, or right after the one in flight.
func (m watchModel) refresh() (watchModel, tea.Cmd) {
	if m.fetching {
		m.refetch = true
		return m, nil
	}
	m.fetching = true
	return m, m.fetchCmd()
}

// applyData takes in one poll's results.
func (m watchModel) applyData(msg watchDataMsg) watchModel {
	if msg.status != nil {
		m.status, m.hasStatus = *msg.status, true
		// Lines logged since the offset this poll asked from append; a poll
		// sent before the log moved on is dropped, and an offset that went
		// backwards means the log restarted, so the lines replace it.
		switch {
		case msg.fromOffset != m.logOffset:
		case msg.status.LogOffset < m.logOffset:
			m.logs = append([]string(nil), msg.status.RecentLogs...)
			m.logOffset = msg.status.LogOffset
		default:
			m.logs = append(m.logs, msg.status.RecentLogs...)
			m.logOffset = msg.status.LogOffset
		}
		if extra := len(m.logs) - watchMaxLogLines; extra > 0 {
			m.logs = slices.Clone(m.logs[extra:])
		}
	}
	if msg.jobsOK {
		m.jobs = msg.jobs
	}
	if msg.tasksOK {
		m.tasks = msg.tasks
	}
	if msg.upcomingOK {
		m.upcoming = msg.upcoming
	}
	if msg.outputOK && msg.outputKey == m.outputKey {
		m.output = msg.output
	}
	var targets []string
	if m.hasStatus {
		targets = m.status.Targets
	}
	m.items = watchBuildItems(m.jobs, m.tasks, m.upcoming, targets)
	m.loaded = true
	m.errText = strings.Join(msg.errs, "; ")
	m.restoreSelection()
	return m
}

func (m watchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m.fitChatInput(), nil
	case watchTickMsg:
		next := watchTick()
		if m.fetching {
			return m, next
		}
		m.fetching = true
		cmds := []tea.Cmd{m.fetchCmd(), next}
		if m.chat.open {
			var chatCmd tea.Cmd
			m, chatCmd = m.fetchChat()
			cmds = append(cmds, chatCmd)
		}
		return m, tea.Batch(cmds...)
	case watchChatDataMsg:
		return m.applyChatData(msg), nil
	case watchChatChunkMsg:
		m = m.applyChatChunk(msg)
		return m, watchChatWait(msg.ch)
	case watchChatDoneMsg:
		return m.finishChat(msg)
	case watchDataMsg:
		m.fetching = false
		m = m.applyData(msg)
		if m.refetch {
			m.refetch = false
			return m.refresh()
		}
		return m, nil
	case watchActionMsg:
		if msg.err != nil {
			m.message = ""
			m.errText = fmt.Sprintf("%s %s: %v", msg.verb, msg.key, msg.err)
			return m, nil
		}
		m.errText = ""
		m.message = fmt.Sprintf("%s sent for %s", msg.verb, msg.key)
		return m.refresh()
	case watchQueueMsg:
		return m.applyQueue(msg)
	case tea.KeyMsg:
		return m.updateKey(msg)
	}
	if watchShiftEnter(msg) {
		// Ctrl+J is the input's portable new-line key.
		return m.updateKey(tea.KeyMsg{Type: tea.KeyCtrlJ})
	}
	return m, nil
}

func (m watchModel) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if next, cmd, handled := m.updateOverlayKey(msg, key); handled {
		return next, cmd
	}
	if m.chat.open {
		if next, cmd, handled := m.updateChatKey(msg, key); handled {
			return next, cmd
		}
	}
	if next, cmd, handled := m.updateBoardNavKey(key); handled {
		return next, cmd
	}
	return m.updateBoardKey(key)
}

// updateOverlayKey handles a key while a stop confirmation, the queue input
// or the help is up. It reports whether one of them took the key.
func (m watchModel) updateOverlayKey(msg tea.KeyMsg, key string) (tea.Model, tea.Cmd, bool) {
	if m.confirm != nil {
		pending := *m.confirm
		m.confirm = nil
		if key == "y" || key == "Y" {
			return m, m.actionCmd(pending.tool, pending.verb, pending.item), true
		}
		m.message = pending.verb + " cancelled"
		return m, nil, true
	}
	if m.queueInput != nil {
		next, cmd := m.updateQueueInput(msg)
		return next, cmd, true
	}
	if m.showHelp {
		// Any key closes the help; q still quits.
		m.showHelp = false
		if key == "q" {
			return m, tea.Quit, true
		}
		return m, nil, true
	}
	return m, nil, false
}

// updateChatKey handles a key while the chat is open. It reports whether the
// chat took the key, so it does not reach the board.
func (m watchModel) updateChatKey(msg tea.KeyMsg, key string) (tea.Model, tea.Cmd, bool) {
	switch m.chat.focus {
	case watchFocusInput:
		next, cmd := m.updateChatInput(msg)
		return next, cmd, true
	case watchFocusTranscript:
		if next, cmd, ok := m.updateChatTranscript(key); ok {
			return next, cmd, true
		}
	}
	if m.updateChatLayout(key) {
		return m, nil, true
	}
	boardHidden := m.chat.full || m.chat.focus == watchFocusTranscript
	if boardHidden && !watchChatPassthroughKey(key) {
		// Board keys act only from the board: the full-screen
		// chat hides it, and the transcript has the focus.
		return m, nil, true
	}
	return m, nil, false
}

// watchChatPassthroughKey reports whether key still reaches the board while
// the board is hidden behind the chat.
func watchChatPassthroughKey(key string) bool {
	return key == "c" || key == "q" || key == "?"
}

// updateBoardNavKey handles the board's cursor, filter and log scroll keys.
// It reports whether key was one of them.
func (m watchModel) updateBoardNavKey(key string) (tea.Model, tea.Cmd, bool) {
	switch key {
	case "0", "1", "2", "3", "4", "5", "6":
		n := int(key[0] - '0')
		if n == 0 {
			m.setFilter("")
		} else {
			m.setFilter(watchFilters[n-1])
		}
	case "f":
		m.setFilter(watchCycleFilter(m.filter, 1))
	case "F":
		m.setFilter(watchCycleFilter(m.filter, -1))
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "home", "g":
		m.moveCursor(-len(m.items))
	case "end", "G":
		m.moveCursor(len(m.items))
	case "left", "shift+tab":
		m.moveColumn(-1)
	case "right", "tab":
		m.moveColumn(1)
	case "pgup":
		m.logScroll = min(m.logScroll+max(1, m.logPaneHeight()-1), max(0, len(m.logs)-1))
	case "pgdown":
		m.logScroll = max(0, m.logScroll-max(1, m.logPaneHeight()-1))
	default:
		return m, nil, false
	}
	return m, nil, true
}

// updateBoardKey handles the board's view toggles and job action keys.
func (m watchModel) updateBoardKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q":
		return m, tea.Quit
	case "?":
		m.showHelp = true
	case "c":
		if m.chat.open {
			return m.closeChat(), nil
		}
		return m.openChat(watchFocusInput)
	case "/":
		return m.openChat(watchFocusInput)
	case "b":
		m.board = !m.board
		m.restoreSelection()
	case "m":
		return m.safeMergeSelected()
	case "Q":
		return m.openQueue()
	case "l":
		m.showLogs = !m.showLogs
		m.logScroll = 0
	case "enter":
		m.showOutput = !m.showOutput
		m.output = nil
		m.outputKey = ""
		if m.showOutput {
			m.syncOutputKey()
			return m.refresh()
		}
	default:
		for _, action := range watchActions {
			if key == action.key {
				return m.startAction(action.verb, action.tool)
			}
		}
	}
	return m, nil
}

// startAction runs verb on the selected item, after a y/n for stop.
func (m watchModel) startAction(verb, tool string) (tea.Model, tea.Cmd) {
	it, ok := m.selectedItem()
	if !ok {
		m.errText = "no job selected"
		return m, nil
	}
	if ok, why := it.actionAvailable(verb); !ok {
		m.message = ""
		m.errText = fmt.Sprintf("cannot %s %s: %s", verb, watchJobKey(it.job), why)
		return m, nil
	}
	if verb == "stop" || verb == "clear" {
		m.confirm = &watchPendingAction{tool: tool, verb: verb, item: it}
		return m, nil
	}
	return m, m.actionCmd(tool, verb, it)
}

// actionCmd calls a job action tool on the item's job.
func (m watchModel) actionCmd(tool, verb string, it watchItem) tea.Cmd {
	args := map[string]any{"target": it.job.Target, "number": it.job.Number}
	key := watchJobKey(it.job)
	return func() tea.Msg {
		_, err := m.call(m.ctx, tool, args)
		return watchActionMsg{verb: verb, key: key, err: err}
	}
}

func watchCycleFilter(filter string, step int) string {
	all := append([]string{""}, watchFilters...)
	i := slices.Index(all, filter)
	return all[(i+step+len(all))%len(all)]
}

func (m *watchModel) setFilter(filter string) {
	m.filter = filter
	m.col = 0
	m.restoreSelection()
}

// visibleItems are the list's items under the filter.
func (m watchModel) visibleItems() []watchItem {
	var out []watchItem
	for _, it := range m.items {
		if it.inFilter(m.filter) {
			out = append(out, it)
		}
	}
	return out
}

// watchColumn is one board column: a filter and its items.
type watchColumn struct {
	state string
	items []watchItem
}

// columns are the board's columns: Scheduled and the five job states with
// every filter, each item in its own column, or the filter's one column.
// A scheduled prompt stays in the Scheduled column while it runs, as the web
// board's card never moves columns.
func (m watchModel) columns() []watchColumn {
	if m.filter != "" {
		return []watchColumn{{state: m.filter, items: m.visibleItems()}}
	}
	cols := make([]watchColumn, 0, len(watchFilters))
	for _, state := range watchFilters {
		col := watchColumn{state: state}
		for _, it := range m.items {
			if it.state == state {
				col.items = append(col.items, it)
			}
		}
		cols = append(cols, col)
	}
	return cols
}

// currentItems are the items the cursor moves over: the list, or the
// selected board column.
func (m watchModel) currentItems() []watchItem {
	if !m.board {
		return m.visibleItems()
	}
	cols := m.columns()
	if m.col >= len(cols) {
		return nil
	}
	return cols[m.col].items
}

func (m watchModel) selectedItem() (watchItem, bool) {
	items := m.currentItems()
	if m.cursor < 0 || m.cursor >= len(items) {
		return watchItem{}, false
	}
	return items[m.cursor], true
}

func (m *watchModel) moveCursor(delta int) {
	n := len(m.currentItems())
	m.cursor = max(0, min(m.cursor+delta, n-1))
	m.remember()
}

func (m *watchModel) moveColumn(delta int) {
	if !m.board {
		return
	}
	n := len(m.columns())
	m.col = max(0, min(m.col+delta, n-1))
	m.cursor = max(0, min(m.cursor, len(m.currentItems())-1))
	m.remember()
}

// remember records the selected item, and moves the output pane to it.
func (m *watchModel) remember() {
	if it, ok := m.selectedItem(); ok {
		m.selected = it.key
	}
	m.syncOutputKey()
}

func (m *watchModel) syncOutputKey() {
	if !m.showOutput {
		return
	}
	key := ""
	if it, ok := m.selectedItem(); ok && it.hasJob {
		key = it.key
	}
	if key != m.outputKey {
		m.outputKey, m.output = key, nil
	}
}

// restoreSelection finds the remembered item again after the items or the
// view changed, in the board's columns when it moved between them, and
// otherwise keeps the cursor in range.
func (m *watchModel) restoreSelection() {
	if m.selected != "" && m.findSelected() {
		m.syncOutputKey()
		return
	}
	if m.board {
		m.col = max(0, min(m.col, len(m.columns())-1))
	}
	m.cursor = max(0, min(m.cursor, len(m.currentItems())-1))
	m.remember()
}

// findSelected moves the cursor onto the remembered item, in the board's
// columns or the list, and reports whether it is still shown.
func (m *watchModel) findSelected() bool {
	if !m.board {
		i := slices.IndexFunc(m.visibleItems(), func(it watchItem) bool { return it.key == m.selected })
		if i < 0 {
			return false
		}
		m.cursor = i
		return true
	}
	for c, col := range m.columns() {
		if i := slices.IndexFunc(col.items, func(it watchItem) bool { return it.key == m.selected }); i >= 0 {
			m.col, m.cursor = c, i
			return true
		}
	}
	return false
}

// watchANSI matches terminal escape sequences, which would break the
// layout of a pane.
var watchANSI = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[@-_]`)

// watchSplitLines splits scrollback into lines without escape sequences or
// carriage returns.
func watchSplitLines(text string) []string {
	text = strings.TrimRight(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if j := strings.LastIndex(line, "\r"); j >= 0 {
			line = line[j+1:]
		}
		lines[i] = watchANSI.ReplaceAllString(line, "")
	}
	return lines
}
