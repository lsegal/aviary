package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// watchStateColors are the board states' colors, as the web board's pills:
// Scheduled violet, Starting orange, Paused amber, Running blue, Done green,
// Failed red.
var watchStateColors = map[string]lipgloss.AdaptiveColor{
	watchScheduled: {Light: "#7c3aed", Dark: "#a78bfa"},
	watchStarting:  {Light: "#c2410c", Dark: "#fb923c"},
	watchPaused:    {Light: "#b45309", Dark: "#fbbf24"},
	watchRunning:   {Light: "#1d4ed8", Dark: "#60a5fa"},
	watchDone:      tuiGood,
	watchFailed:    tuiBad,
}

var watchStateLabels = map[string]string{
	watchScheduled: "Scheduled",
	watchStarting:  "Starting",
	watchPaused:    "Paused",
	watchRunning:   "Running",
	watchDone:      "Done",
	watchFailed:    "Failed",
}

func watchStateStyle(state string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(watchStateColors[state])
}

// watchHelpLines are the help overlay's lines.
var watchHelpLines = []string{
	"aviary watch keys",
	"",
	"0-6        filter: all, scheduled, starting, paused, running, done, failed",
	"f / F      next / previous filter",
	"b          toggle board / list",
	"↑↓ / j k   select a job",
	"←→ / tab   board column",
	"g / G      first / last job",
	"enter      toggle the selected job's output",
	"l          toggle the log pane",
	"pgup/pgdn  scroll the log pane",
	"r          retry the selected job",
	"p          pause it",
	"u          resume it",
	"s          stop it (asks to confirm)",
	"d          dismiss a failed scheduled run",
	"x          clear a done or failed job (asks to confirm)",
	"m          queue the selected job's pull request for gh-safe-merge",
	"Q          queue jobs: merge 1201 1207 · discuss 4 · fix 12",
	"c          open / close the prompt chat",
	"/          open the prompt chat and type",
	"?          toggle this help",
	"q          quit",
}

// watchChatHelpLines are the help overlay's prompt chat lines.
var watchChatHelpLines = []string{
	"prompt chat keys (shared with the web prompt chat)",
	"",
	"enter          send",
	"shift+enter    new line (alt+enter or ctrl+j where the terminal can't send it)",
	"↑ / ↓          earlier / later inputs",
	"tab            switch between the input and the transcript",
	"esc            back to the board",
	"pgup / pgdn    scroll the transcript",
	"",
	"in the transcript:",
	"↑↓ / j k       scroll; scrolling up stops following new output",
	"g / G          top / bottom, following new output again",
	"n / N          next / previous #123 reference",
	"enter          focus the reference's job on the board",
	"e              expand / collapse tool calls",
	"",
	"z              full screen chat",
	"+ / -          taller / shorter chat pane",
}

func (m watchModel) View() string {
	if m.width == 0 || m.height == 0 {
		return "Starting aviary watch..."
	}
	if m.showHelp {
		board, chat := strings.Join(watchHelpLines, "\n"), strings.Join(watchChatHelpLines, "\n")
		content := lipgloss.JoinHorizontal(lipgloss.Top, board, "    ", chat)
		if lipgloss.Width(content)+6 > m.width {
			content = board + "\n\n" + chat
		}
		box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(tuiAccent).Padding(1, 2).Render(content)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	}
	if m.chat.open && m.chat.full {
		lines := append([]string{m.headerLine()}, m.chatPane(m.chatPaneHeight())...)
		return strings.Join(watchFitLines(append(lines, m.footerLine()), m.height), "\n")
	}
	top := []string{m.headerLine(), m.filterLine()}
	if m.board {
		top = append(top, make([]string, watchBoardSpacing)...)
	}
	bottom := []string{}
	if h := m.chatPaneHeight(); h > 0 {
		bottom = append(bottom, m.chatPane(h)...)
	}
	if h := m.outputPaneHeight(); h > 0 {
		bottom = append(bottom, m.outputPane(h)...)
	}
	if h := m.logPaneHeight(); h > 0 {
		bottom = append(bottom, m.logPane(h)...)
	}
	bottom = append(bottom, m.footerLine())
	bodyHeight := max(1, m.height-len(top)-len(bottom))
	var body []string
	switch {
	case !m.loaded:
		body = []string{tuiDimStyle.Render("Loading...")}
	case m.board:
		body = m.boardView(bodyHeight)
	default:
		body = m.listView(bodyHeight)
	}
	body = watchFitLines(body, bodyHeight)
	return strings.Join(watchFitLines(append(append(top, body...), bottom...), m.height), "\n")
}

// headerLine names the watch and its targets, then how issues arrive, with
// the web UI's URL on the right when it is on. The delivery text and then
// the URL drop when the line runs out of room.
func (m watchModel) headerLine() string {
	left := tuiTitleStyle.Render("aviary watch")
	if m.title != "" {
		left += " " + tuiDimStyle.Render("·") + " " + m.title
	}
	withDelivery := left
	if delivery := m.deliveryText(); m.hasStatus && delivery != "" {
		withDelivery += tuiDimStyle.Render(" · " + delivery)
	}
	if m.webURL != "" {
		right := tuiSectionStyle.Render(m.webURL)
		for _, l := range []string{withDelivery, left} {
			if gap := m.width - lipgloss.Width(l) - lipgloss.Width(right); gap >= 2 {
				return l + strings.Repeat(" ", gap) + right
			}
		}
	}
	if lipgloss.Width(withDelivery) <= m.width {
		return withDelivery
	}
	return watchTruncate(left, m.width)
}

// deliveryText says how issues arrive and when they were last checked.
func (m watchModel) deliveryText() string {
	s := m.status
	text := s.Delivery
	if s.Delivery == "poll" && s.PollInterval != "" {
		text += " every " + s.PollInterval
	}
	if s.WebhookOnline != nil && !*s.WebhookOnline {
		text += " (offline)"
	}
	if t, err := time.Parse(time.RFC3339, s.LastCheck); err == nil {
		text = strings.TrimSpace(text + " · checked " + t.Local().Format("15:04:05"))
	}
	if !s.Running {
		text = strings.TrimSpace("stopped " + text)
	}
	return text
}

// watchShortLabels are the filter labels on narrower terminals.
var watchShortLabels = map[string]string{
	"":             "All",
	watchScheduled: "Sched",
	watchStarting:  "Start",
	watchPaused:    "Pause",
	watchRunning:   "Run",
	watchDone:      "Done",
	watchFailed:    "Fail",
}

// filterLine is the header's one row of board state: each filter's key,
// label and count, colored by state with the selected one highlighted, the
// view mode, and the busy slots and pull requests merged in the last day on
// the right. It never wraps: as the terminal narrows, the gaps close, the
// right side shrinks and then drops, and the labels shorten and then drop to
// key:count.
func (m watchModel) filterLine() string {
	counts := m.filterCounts()
	mode := "list"
	if m.board {
		mode = "board"
	}
	rights := m.filterRights()
	var line string
	for tier := range 3 {
		chips := make([]string, 0, len(counts)+1)
		for i, n := range counts {
			state := ""
			if i > 0 {
				state = watchFilters[i-1]
			}
			chips = append(chips, m.filterChip(i, state, n, tier))
		}
		chips = append(chips, tuiDimStyle.Render("["+mode+"]"))
		var ok bool
		if line, ok = m.fitFilterLine(chips, rights); ok {
			return line
		}
	}
	return watchTruncate(line, m.width)
}

// filterCounts is the item count of all items, then of each filter.
func (m watchModel) filterCounts() []int {
	counts := []int{len(m.items)}
	for _, state := range watchFilters {
		n := 0
		for _, it := range m.items {
			if it.inFilter(state) {
				n++
			}
		}
		counts = append(counts, n)
	}
	return counts
}

// filterRights is the filter row's right side, longest first, ending with
// none at all.
func (m watchModel) filterRights() []string {
	if !m.hasStatus {
		return []string{"waiting for status...", ""}
	}
	s := m.status
	slots := fmt.Sprintf("%d/%d", s.Jobs.Running, s.Concurrency)
	return []string{
		fmt.Sprintf("%s slots · %d merged 24h", slots, s.Jobs.Merged24h),
		fmt.Sprintf("%s · %d merged", slots, s.Jobs.Merged24h),
		slots,
		"",
	}
}

// fitFilterLine joins chips with the widest gap and right side that fit the
// width. When none fit it returns the last, tightest attempt and false.
func (m watchModel) fitFilterLine(chips, rights []string) (string, bool) {
	var line string
	for _, right := range rights {
		for _, gap := range []string{"  ", " "} {
			line = strings.Join(chips, gap)
			pad := m.width - lipgloss.Width(line) - lipgloss.Width(right)
			if right == "" && pad >= 0 {
				return line, true
			}
			if right != "" && pad >= 2 {
				return line + strings.Repeat(" ", pad) + tuiDimStyle.Render(right), true
			}
		}
	}
	return line, false
}

// filterChip is a filter's key, label and count: the full label at tier 0,
// the short one at tier 1, and key:count alone at tier 2.
func (m watchModel) filterChip(key int, state string, n, tier int) string {
	var text string
	switch tier {
	case 0:
		label := "All"
		if state != "" {
			label = watchStateLabels[state]
		}
		text = fmt.Sprintf("%d %s %d", key, label, n)
	case 1:
		text = fmt.Sprintf("%d %s %d", key, watchShortLabels[state], n)
	default:
		text = fmt.Sprintf("%d:%d", key, n)
	}
	if m.filter == state {
		style := lipgloss.NewStyle().Bold(true).Reverse(true)
		if state != "" {
			style = style.Foreground(watchStateColors[state])
		}
		return style.Render(" " + text + " ")
	}
	if state == "" {
		return text
	}
	return watchStateStyle(state).Render(text)
}

// listView is one row per item under the filter, scrolled to the cursor.
func (m watchModel) listView(height int) []string {
	items := m.visibleItems()
	if len(items) == 0 {
		return []string{tuiDimStyle.Render(m.emptyText())}
	}
	start := watchScrollStart(m.cursor, len(items), height)
	var lines []string
	for i := start; i < len(items) && len(lines) < height; i++ {
		lines = append(lines, m.listRow(items[i], i == m.cursor))
	}
	return lines
}

func (m watchModel) emptyText() string {
	if m.filter == "" {
		return "No jobs or scheduled tasks yet."
	}
	return "Nothing " + strings.ToLower(watchStateLabels[m.filter]) + "."
}

// listRow is a list item: its state, key, title and details.
func (m watchModel) listRow(it watchItem, selected bool) string {
	state := it.state
	label := watchStateLabels[state]
	if it.scheduled() && it.scheduleState != watchScheduled {
		// A scheduled prompt with a run in flight or failed says so.
		state = it.scheduleState
		label = watchStateLabels[state]
	}
	badge := watchStateStyle(state).Render(fmt.Sprintf("%-9s", label))
	name := m.itemName(it)
	title := m.itemTitle(it)
	if selected {
		name = tuiSelectedStyle.Render(name)
	} else {
		name = tuiLabelStyle.Render(name)
	}
	row := tuiCursor(selected) + " " + badge + " " + name + "  " + title
	if detail := m.itemDetail(it); detail != "" {
		row += "  " + tuiDimStyle.Render(detail)
	}
	return watchTruncate(row, m.width)
}

// itemName is a job's target#number, or a scheduled prompt's project/name.
func (m watchModel) itemName(it watchItem) string {
	if it.scheduled() {
		if it.project != "" {
			return it.project + "/" + it.name
		}
		return it.name
	}
	return watchJobKey(it.job)
}

func (m watchModel) itemTitle(it watchItem) string {
	if it.scheduled() {
		if it.hasJob && it.job.Summary != "" {
			return it.job.Summary
		}
		return it.job.Title
	}
	return it.job.Title
}

// itemDetail is a job's agent, progress, pull request and wait reason, or
// a scheduled prompt's next run and last result.
func (m watchModel) itemDetail(it watchItem) string {
	if it.scheduled() {
		return m.scheduledDetail(it)
	}
	job := it.job
	var parts []string
	if kind := watchKindLabel(job); kind != "" {
		parts = append(parts, kind)
	}
	if job.Agent != "" {
		cli := job.Agent
		if job.Model != "" {
			cli += "/" + job.Model
		}
		parts = append(parts, cli)
	}
	if job.Progress != nil && job.Progress.Total > 0 {
		parts = append(parts, fmt.Sprintf("%d/%d tasks", job.Progress.Done, job.Progress.Total))
	}
	if job.PullRequest > 0 {
		parts = append(parts, strings.TrimSuffix(fmt.Sprintf("PR #%d %s", job.PullRequest, job.PullRequestState), " "))
	}
	if job.WaitReason != "" {
		parts = append(parts, m.waitDetail(job))
	}
	// A finished merge or discuss job's last line says why it ended.
	if (job.Kind == "merge" || job.Kind == "discuss") && job.Summary != "" && (job.Status == "complete" || job.Status == "failed") {
		parts = append(parts, job.Summary)
	}
	if job.Stale {
		parts = append(parts, "stale")
	}
	return strings.Join(parts, " · ")
}

// scheduledDetail is a scheduled item's next run and last result.
func (m watchModel) scheduledDetail(it watchItem) string {
	var parts []string
	switch {
	case it.disabled:
		parts = append(parts, "paused")
	case it.nextRun != "":
		parts = append(parts, "next "+watchUntil(it.nextRun, m.now()))
	}
	if it.hasJob && it.job.LastRun != nil {
		last := "last run " + it.job.LastRun.Result
		if it.job.LastRun.Dismissed {
			last += " (dismissed)"
		}
		parts = append(parts, last)
	}
	return strings.Join(parts, " · ")
}

// waitDetail is a waiting job's reason, with when it wakes if that is still
// ahead.
func (m watchModel) waitDetail(job watchJob) string {
	wait := job.WaitReason
	if job.WakeAt == "" {
		return wait
	}
	if t, err := time.Parse(time.RFC3339, job.WakeAt); err == nil && t.After(m.now()) {
		wait += " · wakes " + watchUntil(job.WakeAt, m.now())
	}
	return wait
}

// watchBoardGutter is the blank cells between adjacent board columns, and
// watchBoardSpacing the blank rows between the filters and the board.
const (
	watchBoardGutter  = 2
	watchBoardSpacing = 2
)

// boardView lays the columns side by side, each a header and two-line cards,
// scrolled to its cursor in the selected column. The headers name the
// states without counts, which the filter row already shows.
func (m watchModel) boardView(height int) []string {
	cols := m.columns()
	gap := watchBoardGutter
	width := max(1, (m.width-gap*(len(cols)-1))/len(cols))
	blocks := make([][]string, len(cols))
	for c, col := range cols {
		blocks[c] = m.boardColumn(col, c == m.col, width, height)
	}
	out := make([]string, 0, height)
	for row := range height {
		var b strings.Builder
		for c, lines := range blocks {
			cell := ""
			if row < len(lines) {
				cell = lines[row]
			}
			b.WriteString(watchPad(cell, width))
			if c < len(blocks)-1 {
				b.WriteString(strings.Repeat(" ", gap))
			}
		}
		// Too narrow for every column at one cell: cut the row, never wrap.
		out = append(out, strings.TrimRight(watchTruncate(b.String(), m.width), " "))
	}
	return out
}

// boardColumn is one board column's lines: its header and the cards that
// fit height, scrolled to the cursor when the column is selected.
func (m watchModel) boardColumn(col watchColumn, selected bool, width, height int) []string {
	// One style, so the selected column's underline never wraps
	// already-styled text that truncation could cut mid-escape.
	style := watchStateStyle(col.state).Bold(true).MaxWidth(width)
	cursor, start := -1, 0
	if selected {
		style = style.Underline(true)
		cursor = m.cursor
		if cursor >= 0 {
			start = watchScrollStart(cursor, len(col.items), max(1, (height-1)/2))
		}
	}
	lines := []string{style.Render(watchStateLabels[col.state])}
	for i := start; i < len(col.items) && len(lines)+2 <= height; i++ {
		lines = append(lines, m.boardCard(col.items[i], i == cursor, width)...)
	}
	if len(col.items) == 0 {
		lines = append(lines, tuiDimStyle.Render(watchTruncate("—", width)))
	}
	return lines
}

// boardCard is an item's two board lines: its name and title, then its
// details. The selected card is shifted right by the "> " caret.
func (m watchModel) boardCard(it watchItem, selected bool, width int) []string {
	name := m.itemName(it)
	if !it.scheduled() {
		name = fmt.Sprintf("#%d", it.job.Number)
	}
	first := name + " " + m.itemTitle(it)
	second := m.itemDetail(it)
	if !it.scheduled() {
		second = strings.TrimSuffix(it.job.Target+" · "+second, " · ")
	} else if it.scheduleState != watchScheduled {
		second = strings.TrimSuffix(watchStateLabels[it.scheduleState]+" · "+second, " · ")
	}
	// Only the selected card carries the caret, so the rest sit flush with
	// their column header and truncate at the full column width.
	if !selected {
		return []string{watchTruncate(first, width), tuiDimStyle.Render(watchTruncate(second, width))}
	}
	first = tuiSelectedStyle.Render(watchTruncate(tuiCursor(true)+" "+first, width))
	return []string{first, tuiDimStyle.Render(watchTruncate("  "+second, width))}
}

// watchMinPane is the shortest a log or output pane gets before it hides.
const watchMinPane = 3

// paneRoom is the height left for the output and log panes: the screen
// below the topRows, less the footer, the chat pane and one board line.
func (m watchModel) paneRoom() int {
	return max(0, m.height-m.topRows()-2-m.chatPaneHeight())
}

// topRows is the rows above the list or board: the header and filters,
// plus the blank rows above the board's column headers.
func (m watchModel) topRows() int {
	if m.board {
		return 2 + watchBoardSpacing
	}
	return 2
}

// outputPaneHeight is the output pane's height while it is open: a third of
// the screen, less when the chat pane leaves no room for it.
func (m watchModel) outputPaneHeight() int {
	if !m.showOutput || (m.chat.open && m.chat.full) {
		return 0
	}
	h := min(max(4, (m.height-4)/3), m.paneRoom())
	if h < watchMinPane {
		return 0
	}
	return h
}

// logPaneHeight is the log pane's height, title included: a third of the
// screen while it is shown, less when the panes above leave no room for it.
func (m watchModel) logPaneHeight() int {
	if !m.showLogs || (m.chat.open && m.chat.full) {
		return 0
	}
	h := min(max(4, (m.height-4)/3), m.paneRoom()-m.outputPaneHeight())
	if h < watchMinPane {
		return 0
	}
	return h
}

// logPane is the engine's log, its newest lines at the bottom unless
// scrolled back.
func (m watchModel) logPane(height int) []string {
	title := "Logs"
	if m.logScroll > 0 {
		title += fmt.Sprintf(" (scrolled back %d)", m.logScroll)
	}
	end := max(0, len(m.logs)-m.logScroll)
	start := max(0, end-(height-1))
	return m.pane(title, watchSplitLines(strings.Join(m.logs[start:end], "\n")), height)
}

// outputPane is the selected job's scrollback, newest lines at the bottom.
func (m watchModel) outputPane(height int) []string {
	title := "Output"
	if m.outputKey != "" {
		title += " · " + m.outputKey
	}
	lines := m.output
	if m.outputKey == "" {
		lines = []string{"Select a job that has run to see its output."}
	} else if len(lines) == 0 {
		lines = []string{"waiting for output..."}
	}
	return m.pane(title, lines[max(0, len(lines)-(height-1)):], height)
}

// pane is a titled rule and up to height-1 lines under it.
func (m watchModel) pane(title string, lines []string, height int) []string {
	rule := "── " + title + " "
	rule += strings.Repeat("─", max(0, m.width-lipgloss.Width(rule)))
	out := []string{tuiSectionStyle.Render(watchTruncate(rule, m.width))}
	for _, line := range lines {
		out = append(out, watchTruncate(line, m.width))
	}
	return watchFitLines(out, height)
}

// footerLine is the confirm prompt, or the last error or message, with the
// key hint.
func (m watchModel) footerLine() string {
	hint := tuiHelpStyle.Render("? help · q quit")
	var text string
	switch {
	case m.chat.open && m.chat.focus == watchFocusInput && m.confirm == nil:
		hint = tuiHelpStyle.Render("ctrl+c quit")
		text = tuiHelpStyle.Render("enter send · shift+enter new line · ↑↓ history · tab transcript · esc board")
	case m.chat.open && m.chat.focus == watchFocusTranscript && m.confirm == nil:
		text = tuiHelpStyle.Render("↑↓ scroll · G follow · n/N reference · enter jump · e tools · z full · tab input · esc board")
	case m.queueInput != nil:
		text = m.queueInput.View()
	case m.confirm != nil:
		text = tuiErrorStyle.Render(fmt.Sprintf("%s %s? y/n", m.confirm.verb, watchJobKey(m.confirm.item.job)))
	case m.errText != "":
		text = tuiErrorStyle.Render(m.errText)
	case m.hasStatus && m.status.Error != "":
		text = tuiErrorStyle.Render(m.status.Error)
	case m.message != "":
		text = tuiSuccessStyle.Render(m.message)
	default:
		text = tuiHelpStyle.Render("0-6 filter · b board · enter output · l logs · c chat · r retry · p pause · u resume · s stop")
	}
	gap := m.width - lipgloss.Width(text) - lipgloss.Width(hint)
	if gap < 2 {
		return watchTruncate(text, m.width)
	}
	return text + strings.Repeat(" ", gap) + hint
}

// watchScrollStart is the first of n rows to show in height rows so the
// cursor stays in view.
func watchScrollStart(cursor, n, height int) int {
	if height <= 0 || n <= height {
		return 0
	}
	return max(0, min(cursor-height/2, n-height))
}

// watchFitLines pads or cuts lines to exactly height.
func watchFitLines(lines []string, height int) []string {
	if len(lines) > height {
		return lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

// watchTruncate cuts s, which may be styled, to width cells.
func watchTruncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(s)
}

// watchPad pads s, which may be styled, with spaces to width cells.
func watchPad(s string, width int) string {
	s = watchTruncate(s, width)
	return s + strings.Repeat(" ", max(0, width-lipgloss.Width(s)))
}

// watchUntil is a compact countdown to an RFC 3339 time, such as "in 14m".
func watchUntil(at string, now time.Time) string {
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return at
	}
	d := t.Sub(now)
	switch {
	case d <= 0:
		return "now"
	case d < time.Minute:
		return fmt.Sprintf("in %ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("in %dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("in %dh", int(d.Hours()))
	}
	return fmt.Sprintf("in %dd", int(d.Hours()/24))
}
