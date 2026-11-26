package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"

	"github.com/lsegal/aviary/internal/config"
)

// watchStreamCaller calls an Aviary MCP tool in-process, handing each
// progress message it sends to onProgress, and returns its text result.
type watchStreamCaller func(ctx context.Context, name string, args map[string]any, onProgress func(string)) (string, error)

// The prompt chat's focus: the board, the chat's input, or its transcript.
const (
	watchFocusBoard      = ""
	watchFocusInput      = "input"
	watchFocusTranscript = "transcript"
)

// watchChatHistoryLimit is how many of the prompt chat's latest messages the
// TUI reads.
const watchChatHistoryLimit = 200

// The prompt chat pane's share of the screen below the header, in percent,
// and the step + and - change it by.
const (
	watchChatDefaultPct = 45
	watchChatMinPct     = 20
	watchChatMaxPct     = 80
	watchChatPctStep    = 10
)

// watchChatMaxInputLines is the input's tallest height before it scrolls.
const watchChatMaxInputLines = 5

// watchChatEntry is one block of the prompt chat: a user message, a reply, a
// tool call or an error.
type watchChatEntry struct {
	role string
	text string
	tool *watchChatTool
	// status marks a user message this TUI sent that the session read has
	// not returned yet: "sending" while its turn runs, then "sent", or
	// "failed" when its delivery failed.
	status string
}

// watchChatTool is a tool call of a reply, as agent_run's tool progress, the
// session's persisted "[tool]" messages and session_turn carry it.
type watchChatTool struct {
	ID     string         `json:"id,omitempty"`
	Name   string         `json:"name"`
	Args   map[string]any `json:"args,omitempty"`
	Result string         `json:"result,omitempty"`
	Error  string         `json:"error,omitempty"`
	Done   bool           `json:"done,omitempty"`
}

// watchChat is the prompt chat pane's state. Its session is the Code Loop
// agent's one prompt chat session, the one the web board's prompt chat uses.
type watchChat struct {
	open  bool
	focus string
	full  bool
	pct   int
	input textarea.Model

	// history holds the inputs sent, oldest first; histIdx is the one ↑/↓
	// recalled, len(history) while editing a new input kept in draft.
	history []string
	histIdx int
	draft   string

	// entries is the session as the server has it, its running turn
	// included.
	entries  []watchChatEntry
	loaded   bool
	fetching bool
	// gen counts this TUI's sends, so a read started before one is
	// dropped.
	gen int
	// replying is set while the server reports a turn running.
	replying bool

	// turns are this TUI's sends since the session was last read, oldest
	// first. Each one streams on its own, so a send never waits for an
	// earlier reply; the session is read back once none is running.
	turns []watchChatTurn
	// failed are the sends that failed, kept across reads.
	failed []watchChatFailure

	// follow keeps the transcript at its bottom; top is its first line
	// shown while it does not.
	follow bool
	top    int

	expandTools bool
	// ref is the selected #123 reference, -1 for none.
	ref     int
	errText string
}

func newWatchChat() watchChat {
	input := textarea.New()
	input.Prompt = "› "
	input.Placeholder = "Ask the Code Loop agent… (enter sends, shift+enter adds a line)"
	input.ShowLineNumbers = false
	input.CharLimit = 0
	input.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter", "shift+enter", "ctrl+j"))
	input.FocusedStyle.CursorLine = lipgloss.NewStyle()
	input.FocusedStyle.Prompt = tuiSectionStyle
	input.BlurredStyle.Prompt = tuiDimStyle
	input.FocusedStyle.Placeholder = tuiDimStyle
	input.BlurredStyle.Placeholder = tuiDimStyle
	input.SetHeight(1)
	return watchChat{pct: watchChatDefaultPct, input: input, follow: true, ref: -1}
}

// watchChatTurn is one send of this TUI: its message and the reply streamed
// so far, and how its delivery ended.
type watchChatTurn struct {
	ch      chan tea.Msg
	entries []watchChatEntry
	done    bool
	err     string
}

// watchChatFailure is a send that failed. at is where it shows among the
// session's entries; entries is its error, after its message when the
// session does not have that.
type watchChatFailure struct {
	at      int
	entries []watchChatEntry
}

// watchChatDataMsg is one read of the prompt chat session.
type watchChatDataMsg struct {
	gen      int
	entries  []watchChatEntry
	replying bool
	err      error
}

// watchChatChunkMsg is one progress message of this TUI's running turn.
type watchChatChunkMsg struct {
	ch    chan tea.Msg
	chunk string
}

// watchChatDoneMsg ends this TUI's turn.
type watchChatDoneMsg struct {
	ch    chan tea.Msg
	reply string
	err   error
}

// openChat opens the prompt chat at its bottom with focus, and reads it.
func (m watchModel) openChat(focus string) (watchModel, tea.Cmd) {
	if !m.chat.open {
		m.chat.open = true
		m.chat.follow = true
		m.chat.ref = -1
	}
	m = m.focusChat(focus)
	return m.fetchChat()
}

// closeChat closes the prompt chat, restoring the board's layout.
func (m watchModel) closeChat() watchModel {
	m.chat.open = false
	m.chat.full = false
	return m.focusChat(watchFocusBoard)
}

func (m watchModel) focusChat(focus string) watchModel {
	m.chat.focus = focus
	if focus == watchFocusInput {
		m.chat.input.Focus()
		m = m.fitChatInput()
	} else {
		m.chat.input.Blur()
	}
	return m
}

// streaming reports whether any of this TUI's turns is running.
func (c watchChat) streaming() bool {
	return slices.ContainsFunc(c.turns, func(t watchChatTurn) bool { return !t.done })
}

// fetchChat reads the prompt chat session, unless a read or one of this
// TUI's own turns is in flight.
func (m watchModel) fetchChat() (watchModel, tea.Cmd) {
	if m.chat.fetching || m.chat.streaming() {
		return m, nil
	}
	m.chat.fetching = true
	return m, m.chatFetchCmd()
}

// chatFetchCmd reads the session's latest messages and its running turn,
// the way the web chat restores a session.
func (m watchModel) chatFetchCmd() tea.Cmd {
	gen := m.chat.gen
	return func() tea.Msg {
		msg := watchChatDataMsg{gen: gen}
		raw, err := m.call(m.ctx, "session_messages", map[string]any{
			"agent":      config.ProjectsAgentName,
			"session_id": config.ProjectsChatSession,
			"order":      "desc",
			"limit":      watchChatHistoryLimit,
		})
		if err != nil {
			msg.err = err
			return msg
		}
		if msg.entries, err = watchChatHistoryEntries(raw); err != nil {
			msg.err = err
			return msg
		}
		raw, err = m.call(m.ctx, "session_turn", map[string]any{
			"agent":      config.ProjectsAgentName,
			"session_id": config.ProjectsChatSession,
		})
		if err == nil {
			msg.entries, msg.replying = watchChatTurnEntries(msg.entries, raw)
		}
		return msg
	}
}

// watchChatHistoryEntries turns a newest-first session_messages result into
// transcript entries, oldest first.
func watchChatHistoryEntries(raw string) ([]watchChatEntry, error) {
	var persisted []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &persisted); err != nil {
			return nil, fmt.Errorf("decode: %w", err)
		}
	}
	slices.Reverse(persisted)
	var entries []watchChatEntry
	for _, p := range persisted {
		switch {
		case p.Role == "tool":
			entries = watchAddTool(entries, watchParseTool(p.Content, true))
		case p.Role == "assistant" && strings.HasPrefix(p.Content, "[tool] "):
			entries = watchAddTool(entries, watchParseTool(strings.TrimPrefix(p.Content, "[tool] "), true))
		case p.Role == "user" || p.Role == "assistant":
			entries = append(entries, watchChatEntry{role: p.Role, text: p.Content})
		}
	}
	return entries, nil
}

// watchChatTurnEntries appends a session_turn result's items to entries when
// a turn is running, and reports whether one is.
func watchChatTurnEntries(entries []watchChatEntry, raw string) ([]watchChatEntry, bool) {
	var turn struct {
		Processing bool `json:"processing"`
		Items      []struct {
			Text     string         `json:"text"`
			MediaURL string         `json:"media_url"`
			Tool     *watchChatTool `json:"tool"`
		} `json:"items"`
	}
	if strings.TrimSpace(raw) == "" || json.Unmarshal([]byte(raw), &turn) != nil || !turn.Processing {
		return entries, false
	}
	for _, item := range turn.Items {
		switch {
		case item.Tool != nil:
			entries = watchAddTool(entries, *item.Tool)
		case item.MediaURL != "":
			entries = append(entries, watchChatEntry{role: "assistant", text: item.MediaURL})
		case item.Text != "":
			entries = watchAddText(entries, item.Text)
		}
	}
	return entries, true
}

// applyChatData takes in a read of the session, which replaces this TUI's
// finished turns. A read started before this TUI's latest send, or landing
// while one of its turns streams, is stale.
func (m watchModel) applyChatData(msg watchChatDataMsg) watchModel {
	m.chat.fetching = false
	if msg.gen != m.chat.gen || m.chat.streaming() {
		return m
	}
	if msg.err != nil {
		m.chat.errText = msg.err.Error()
		return m
	}
	m.chat.failed = append(m.chat.failed, watchChatFailures(msg.entries, m.chat.turns)...)
	slices.SortStableFunc(m.chat.failed, func(a, b watchChatFailure) int { return a.at - b.at })
	m.chat.entries = msg.entries
	m.chat.turns = nil
	m.chat.replying = msg.replying
	m.chat.loaded = true
	return m
}

// watchChatFailures places the failed turns among a read of the session:
// after their message's reply when the session saved the message, else at
// its end with the message.
func watchChatFailures(entries []watchChatEntry, turns []watchChatTurn) []watchChatFailure {
	var out []watchChatFailure
	from := len(entries)
	for _, t := range slices.Backward(turns) {
		if t.err == "" {
			continue
		}
		failure := watchChatFailure{at: len(entries), entries: []watchChatEntry{
			{role: "user", text: t.entries[0].text, status: "failed"},
			{role: "error", text: t.err},
		}}
		for i := from - 1; i >= 0; i-- {
			if entries[i].role == "user" && entries[i].text == t.entries[0].text {
				at := i + 1
				for at < len(entries) && entries[at].role != "user" {
					at++
				}
				failure = watchChatFailure{at: at, entries: failure.entries[1:]}
				from = i
				break
			}
		}
		out = append(out, failure)
	}
	slices.Reverse(out)
	return out
}

// watchParseTool reads a tool call's JSON; a persisted one has finished.
func watchParseTool(payload string, done bool) watchChatTool {
	var tool watchChatTool
	if err := json.Unmarshal([]byte(payload), &tool); err != nil || tool.Name == "" {
		tool = watchChatTool{Name: "tool", Result: payload}
	}
	tool.Done = tool.Done || done
	return tool
}

// watchAddTool adds a tool call, replacing the earlier entry of the same
// call, so a call and its result show as one line.
func watchAddTool(entries []watchChatEntry, tool watchChatTool) []watchChatEntry {
	if tool.ID != "" {
		for i := len(entries) - 1; i >= 0; i-- {
			if entries[i].tool != nil && entries[i].tool.ID == tool.ID {
				merged := *entries[i].tool
				if tool.Args != nil {
					merged.Args = tool.Args
				}
				merged.Result = firstNonEmpty(tool.Result, merged.Result)
				merged.Error = firstNonEmpty(tool.Error, merged.Error)
				merged.Done = merged.Done || tool.Done
				entries[i].tool = &merged
				return entries
			}
		}
	}
	return append(entries, watchChatEntry{role: "tool", tool: &tool})
}

// watchAddText appends streamed reply text to the reply being written.
func watchAddText(entries []watchChatEntry, text string) []watchChatEntry {
	if n := len(entries); n > 0 && entries[n-1].role == "assistant" {
		entries[n-1].text += text
		return entries
	}
	return append(entries, watchChatEntry{role: "assistant", text: text})
}

// chatProject is the board project the agent answers about: the one target
// when the watch has one, else every project.
func (m watchModel) chatProject() string {
	if m.hasStatus && len(m.status.Targets) == 1 {
		return m.status.Targets[0]
	}
	return ""
}

// sendChat sends the input to the Code Loop agent in the shared prompt chat
// session and streams its reply. A send while a reply is still running goes
// through agent_run all the same, as the web chat's does, and the server
// queues or steers it into the running turn.
func (m watchModel) sendChat() (watchModel, tea.Cmd) {
	text := strings.TrimSpace(m.chat.input.Value())
	if text == "" {
		return m, nil
	}
	if n := len(m.chat.history); n == 0 || m.chat.history[n-1] != text {
		m.chat.history = append(m.chat.history, text)
	}
	m.chat.histIdx, m.chat.draft = len(m.chat.history), ""
	m.chat.input.Reset()
	m.chat.errText = ""
	m.chat.gen++
	m.chat.follow = true
	args := map[string]any{
		"name":                  config.ProjectsAgentName,
		"message":               text,
		"session":               config.ProjectsChatSession,
		"include_tool_progress": true,
	}
	if project := m.chatProject(); project != "" {
		args["project"] = project
	}
	ch := make(chan tea.Msg, 256)
	m.chat.turns = append(slices.Clone(m.chat.turns), watchChatTurn{
		ch:      ch,
		entries: []watchChatEntry{{role: "user", text: text, status: "sending"}},
	})
	ctx, call, stream := m.ctx, m.call, m.stream
	send := func(msg tea.Msg) {
		select {
		case ch <- msg:
		case <-ctx.Done():
		}
	}
	go func() {
		var reply string
		var err error
		if stream != nil {
			reply, err = stream(ctx, "agent_run", args, func(chunk string) { send(watchChatChunkMsg{ch: ch, chunk: chunk}) })
		} else {
			reply, err = call(ctx, "agent_run", args)
		}
		send(watchChatDoneMsg{ch: ch, reply: reply, err: err})
	}()
	return m, watchChatWait(ch)
}

// watchChatWait reads the next message of a running turn.
func watchChatWait(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

// chatTurn is the index of the turn streaming on ch, -1 for none.
func (m watchModel) chatTurn(ch chan tea.Msg) int {
	return slices.IndexFunc(m.chat.turns, func(t watchChatTurn) bool { return t.ch == ch })
}

// applyChatChunk adds one progress message to the reply of the turn
// streaming on ch: text, a "[tool]" call, or a "[media]" URL.
func (m watchModel) applyChatChunk(msg watchChatChunkMsg) watchModel {
	i := m.chatTurn(msg.ch)
	if i < 0 {
		return m
	}
	m.chat.turns = slices.Clone(m.chat.turns)
	t := &m.chat.turns[i]
	switch chunk := msg.chunk; {
	case strings.HasPrefix(chunk, "[tool]"):
		t.entries = watchAddTool(t.entries, watchParseTool(strings.TrimPrefix(chunk, "[tool]"), false))
	case strings.HasPrefix(chunk, "[media]"):
		t.entries = append(t.entries, watchChatEntry{role: "assistant", text: strings.TrimPrefix(chunk, "[media]")})
	case chunk != "":
		t.entries = watchAddText(t.entries, chunk)
	}
	return m
}

// finishChat ends the turn streaming on msg's channel. Once none is running
// it reads the session back, which then holds the replies as the server
// saved them.
func (m watchModel) finishChat(msg watchChatDoneMsg) (watchModel, tea.Cmd) {
	i := m.chatTurn(msg.ch)
	if i < 0 {
		return m.fetchChat()
	}
	m.chat.turns = slices.Clone(m.chat.turns)
	t := &m.chat.turns[i]
	t.done = true
	t.entries = slices.Clone(t.entries)
	t.entries[0].status = "sent"
	streamed := len(t.entries) > 1
	if msg.err != nil {
		t.err = msg.err.Error()
		t.entries[0].status = "failed"
	} else if !streamed && strings.TrimSpace(msg.reply) != "" {
		t.entries = watchAddText(t.entries, msg.reply)
	}
	return m.fetchChat()
}

// chatEntries are the transcript's blocks: the session with the failed sends
// in place, then this TUI's turns not read back yet, each with its error if
// it failed.
func (m watchModel) chatEntries() []watchChatEntry {
	if len(m.chat.failed) == 0 && len(m.chat.turns) == 0 {
		return m.chat.entries
	}
	out := make([]watchChatEntry, 0, len(m.chat.entries))
	next := 0
	for _, f := range m.chat.failed {
		at := min(f.at, len(m.chat.entries))
		if at > next {
			out = append(out, m.chat.entries[next:at]...)
			next = at
		}
		out = append(out, f.entries...)
	}
	out = append(out, m.chat.entries[next:]...)
	for _, t := range m.chat.turns {
		out = append(out, t.entries...)
		if t.err != "" {
			out = append(out, watchChatEntry{role: "error", text: t.err})
		}
	}
	return out
}

// updateChatInput handles a key while the chat's input has focus, then fits
// the input to its soft-wrapped text.
func (m watchModel) updateChatInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.chat.input.SetWidth(max(4, m.width))
	next, cmd := m.editChatInput(msg)
	return next.fitChatInput(), cmd
}

// editChatInput applies a key to the chat's input.
func (m watchModel) editChatInput(msg tea.KeyMsg) (watchModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m.focusChat(watchFocusBoard), nil
	case "tab":
		return m.focusChat(watchFocusTranscript), nil
	case "enter":
		return m.sendChat()
	case "pgup":
		m.scrollChat(-m.chatPage())
		return m, nil
	case "pgdown":
		m.scrollChat(m.chatPage())
		return m, nil
	case "up":
		if len(m.chat.history) > 0 && m.chat.input.Line() == 0 && m.chat.input.LineInfo().RowOffset == 0 && m.chat.histIdx > 0 {
			if m.chat.histIdx == len(m.chat.history) {
				m.chat.draft = m.chat.input.Value()
			}
			m.chat.histIdx--
			m.chat.input.SetValue(m.chat.history[m.chat.histIdx])
			return m, nil
		}
	case "down":
		info := m.chat.input.LineInfo()
		if m.chat.histIdx < len(m.chat.history) && m.chat.input.Line() == m.chat.input.LineCount()-1 && info.RowOffset == info.Height-1 {
			m.chat.histIdx++
			if m.chat.histIdx == len(m.chat.history) {
				m.chat.input.SetValue(m.chat.draft)
			} else {
				m.chat.input.SetValue(m.chat.history[m.chat.histIdx])
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.chat.input, cmd = m.chat.input.Update(msg)
	return m, cmd
}

// updateChatTranscript handles a key while the chat's transcript has focus,
// reporting whether it used it.
func (m watchModel) updateChatTranscript(key string) (watchModel, tea.Cmd, bool) {
	switch key {
	case "esc":
		return m.focusChat(watchFocusBoard), nil, true
	case "tab", "i", "/":
		return m.focusChat(watchFocusInput), nil, true
	case "up", "k":
		m.scrollChat(-1)
	case "down", "j":
		m.scrollChat(1)
	case "pgup", "ctrl+u":
		m.scrollChat(-m.chatPage())
	case "pgdown", "ctrl+d":
		m.scrollChat(m.chatPage())
	case "home", "g":
		m.scrollChat(-len(m.chatLines(m.width).lines))
	case "end", "G":
		m.chat.follow = true
	case "n":
		m.selectChatRef(1)
	case "N":
		m.selectChatRef(-1)
	case "enter":
		m2, cmd := m.jumpToChatRef()
		return m2, cmd, true
	case "e":
		m.chat.expandTools = !m.chat.expandTools
	default:
		return m, nil, false
	}
	return m, nil, true
}

// updateChatLayout handles the keys that resize the chat pane, from the
// board or the transcript, reporting whether it used key.
func (m *watchModel) updateChatLayout(key string) bool {
	switch key {
	case "z":
		m.chat.full = !m.chat.full
	case "+", "=":
		m.chat.pct = min(watchChatMaxPct, m.chat.pct+watchChatPctStep)
	case "-", "_":
		m.chat.pct = max(watchChatMinPct, m.chat.pct-watchChatPctStep)
	default:
		return false
	}
	return true
}

// chatPage is how far a page key scrolls the transcript.
func (m watchModel) chatPage() int {
	return max(1, m.chatTranscriptHeight()-1)
}

// scrollChat moves the transcript by delta lines. Scrolling up stops it
// following its bottom; reaching the bottom follows it again.
func (m *watchModel) scrollChat(delta int) {
	total := len(m.chatLines(m.width).lines)
	h := m.chatTranscriptHeight()
	bottom := max(0, total-h)
	top := m.chat.top
	if m.chat.follow {
		top = bottom
	}
	top = max(0, min(top+delta, bottom))
	m.chat.top = top
	m.chat.follow = top >= bottom
}

// selectChatRef moves the selected #123 reference by step, scrolling it into
// view.
func (m *watchModel) selectChatRef(step int) {
	view := m.chatLines(m.width)
	if len(view.refs) == 0 {
		m.chat.ref = -1
		m.chat.errText = "no #123 references in the chat"
		return
	}
	m.chat.errText = ""
	switch {
	case m.chat.ref < 0 && step > 0:
		// From nothing, n starts at the newest reference in view.
		m.chat.ref = len(view.refs) - 1
	case m.chat.ref < 0:
		m.chat.ref = len(view.refs) - 1
	default:
		m.chat.ref = (m.chat.ref + step + len(view.refs)) % len(view.refs)
	}
	line := view.refs[m.chat.ref].line
	h := m.chatTranscriptHeight()
	top := m.chat.top
	if m.chat.follow {
		top = max(0, len(view.lines)-h)
	}
	if line < top || line >= top+h {
		m.scrollChat(line - h/2 - top)
	}
}

// jumpToChatRef focuses the selected reference's job on the board.
func (m watchModel) jumpToChatRef() (watchModel, tea.Cmd) {
	view := m.chatLines(m.width)
	if m.chat.ref < 0 || m.chat.ref >= len(view.refs) {
		m.chat.errText = "select a #123 reference with n first"
		return m, nil
	}
	ref := view.refs[m.chat.ref]
	it, ok := m.findRefItem(ref)
	if !ok {
		m.chat.errText = fmt.Sprintf("no job for %s on the board", ref.text)
		return m, nil
	}
	if !it.inFilter(m.filter) {
		m.filter = ""
	}
	m.selected = it.key
	m.restoreSelection()
	m.chat.full = false
	m.chat.errText = ""
	m.errText = ""
	m.message = "focused " + it.key
	return m.focusChat(watchFocusBoard), nil
}

// findRefItem finds the board job an issue or pull request reference names.
func (m watchModel) findRefItem(ref watchChatRef) (watchItem, bool) {
	for _, pr := range []bool{false, true} {
		for _, it := range m.items {
			if !it.hasJob || (ref.target != "" && !strings.EqualFold(it.job.Target, ref.target)) {
				continue
			}
			if (!pr && it.job.Number == ref.number) || (pr && it.job.PullRequest == ref.number) {
				return it, true
			}
		}
	}
	return watchItem{}, false
}

// watchChatRef is a #123 or owner/repo#123 reference in the transcript.
type watchChatRef struct {
	text   string
	target string
	number int
	line   int
}

// watchChatView is the transcript rendered to a width: its lines and the
// references on them, in order.
type watchChatView struct {
	lines []string
	refs  []watchChatRef
}

var (
	watchChatUserStyle  = lipgloss.NewStyle().Bold(true).Foreground(tuiAccent)
	watchChatAgentStyle = lipgloss.NewStyle().Bold(true).Foreground(watchStateColors[watchScheduled])
	watchChatCodeStyle  = lipgloss.NewStyle().Foreground(watchStateColors[watchStarting])
	watchChatRefStyle   = lipgloss.NewStyle().Bold(true).Underline(true).Foreground(watchStateColors[watchRunning])
	watchChatLinkStyle  = lipgloss.NewStyle().Underline(true)
)

// chatLines renders the transcript to width.
func (m watchModel) chatLines(width int) watchChatView {
	r := &watchChatRenderer{width: max(10, width), selected: m.chat.ref, expand: m.chat.expandTools}
	entries := m.chatEntries()
	for i, entry := range entries {
		if i > 0 {
			r.lines = append(r.lines, "")
		}
		prev := ""
		if i > 0 {
			prev = entries[i-1].role
		}
		r.entry(entry, i == 0, prev)
	}
	return watchChatView{lines: r.lines, refs: r.refs}
}

// watchChatSendStatus is the suffix a user message's header shows for its
// delivery status.
func watchChatSendStatus(status string) string {
	switch status {
	case "sending":
		return tuiDimStyle.Render(" · sending…")
	case "sent":
		return tuiDimStyle.Render(" · sent")
	case "failed":
		return tuiErrorStyle.Render(" · not delivered")
	}
	return ""
}

// entry renders one transcript entry; first marks the first entry and prev
// is the previous entry's role.
func (r *watchChatRenderer) entry(entry watchChatEntry, first bool, prev string) {
	switch entry.role {
	case "user":
		r.lines = append(r.lines, watchChatUserStyle.Render("you")+watchChatSendStatus(entry.status))
		r.markdown(entry.text, "  ")
	case "error":
		r.wrapped("  "+tuiErrorStyle.Render("! "), "    ", tuiErrorStyle.Render(watchANSI.ReplaceAllString(entry.text, "")), false)
	case "assistant":
		if first || prev != "tool" {
			r.lines = append(r.lines, watchChatAgentStyle.Render(config.ProjectsAgentName))
		}
		r.markdown(entry.text, "  ")
	case "tool":
		if !first && prev != "user" {
			// Tool calls and reply text run together under one header.
			r.lines = r.lines[:len(r.lines)-1]
		} else {
			r.lines = append(r.lines, watchChatAgentStyle.Render(config.ProjectsAgentName))
		}
		r.tool(*entry.tool)
	}
}

// watchChatRenderer renders chat Markdown for the terminal: bold, inline and
// fenced code, headings, lists, quotes, links and #123 references, wrapped to
// width.
type watchChatRenderer struct {
	width    int
	selected int
	expand   bool
	lines    []string
	refs     []watchChatRef
}

var (
	watchMDList    = regexp.MustCompile(`^(\s*)([-*+]|\d+[.)])\s+(.*)$`)
	watchMDHeading = regexp.MustCompile(`^#{1,6}\s+(.*)$`)
	// watchMDInline matches inline code, bold, links and issue references.
	watchMDInline = regexp.MustCompile("`[^`]+`|\\*\\*[^*]+\\*\\*|__[^_]+__|\\[[^\\]]+\\]\\([^)\\s]+\\)|(?:[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)?#[0-9]+\\b")
)

// markdown renders text, each line under indent.
func (r *watchChatRenderer) markdown(text, indent string) {
	inCode := false
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = watchANSI.ReplaceAllString(line, "")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCode = !inCode
			continue
		}
		switch {
		case inCode:
			r.wrapped(indent+tuiDimStyle.Render("│ "), indent+tuiDimStyle.Render("│ "), watchChatCodeStyle.Render(strings.ReplaceAll(line, "\t", "    ")), true)
		case trimmed == "":
			r.lines = append(r.lines, "")
		case watchMDHeading.MatchString(trimmed):
			body := watchMDHeading.FindStringSubmatch(trimmed)[1]
			r.wrapped(indent, indent, r.inlineStyled(body, tuiTitleStyle), false)
		case strings.HasPrefix(trimmed, ">"):
			bar := indent + tuiDimStyle.Render("▏ ")
			r.wrapped(bar, bar, r.inlineStyled(strings.TrimSpace(strings.TrimPrefix(trimmed, ">")), tuiDimStyle), false)
		case watchMDList.MatchString(line):
			parts := watchMDList.FindStringSubmatch(line)
			bullet := parts[2]
			if !strings.ContainsAny(bullet[:1], "0123456789") {
				bullet = "•"
			}
			lead := indent + strings.Repeat(" ", min(len(parts[1]), 8)) + bullet + " "
			r.wrapped(lead, strings.Repeat(" ", lipgloss.Width(lead)), r.inline(parts[3]), false)
		default:
			r.wrapped(indent, indent, r.inline(trimmed), false)
		}
	}
}

// wrapped adds text wrapped to the width after first on its first line and
// rest on the others; hard breaks long words, as code needs.
func (r *watchChatRenderer) wrapped(first, rest, text string, hard bool) {
	width := max(4, r.width-lipgloss.Width(first))
	var out string
	if hard {
		out = ansi.Hardwrap(text, width, true)
	} else {
		out = ansi.Wrap(text, width, "")
	}
	start := len(r.lines)
	for i, line := range strings.Split(out, "\n") {
		lead := rest
		if i == 0 {
			lead = first
		}
		r.lines = append(r.lines, strings.TrimRight(lead+line, " "))
	}
	r.placeRefs(start)
}

// placeRefs sets the line of every reference added since the transcript's
// line start that does not have one yet.
func (r *watchChatRenderer) placeRefs(start int) {
	line := start
	for i := range r.refs {
		if r.refs[i].line >= 0 {
			continue
		}
		for line < len(r.lines) && !strings.Contains(watchANSI.ReplaceAllString(r.lines[line], ""), r.refs[i].text) {
			line++
		}
		r.refs[i].line = min(line, len(r.lines)-1)
	}
}

// inline styles bold, code, links and references in one line of Markdown.
func (r *watchChatRenderer) inline(text string) string {
	return r.inlineStyled(text, lipgloss.NewStyle())
}

// inlineStyled is inline with base under every part, so a heading's or
// bold run's references keep its style around them.
func (r *watchChatRenderer) inlineStyled(text string, base lipgloss.Style) string {
	var b strings.Builder
	plain := func(s string) {
		if s != "" {
			b.WriteString(base.Render(s))
		}
	}
	last := 0
	for _, loc := range watchMDInline.FindAllStringIndex(text, -1) {
		token := text[loc[0]:loc[1]]
		plain(text[last:loc[0]])
		last = loc[1]
		switch {
		case strings.HasPrefix(token, "`"):
			b.WriteString(watchChatCodeStyle.Inherit(base).Render(strings.Trim(token, "`")))
		case strings.HasPrefix(token, "**"), strings.HasPrefix(token, "__"):
			b.WriteString(r.inlineStyled(token[2:len(token)-2], base.Bold(true)))
		case strings.HasPrefix(token, "["):
			label, href, _ := strings.Cut(token[1:len(token)-1], "](")
			b.WriteString(watchChatLinkStyle.Inherit(base).Render(label))
			if href != label {
				b.WriteString(tuiDimStyle.Render(" (" + href + ")"))
			}
		default:
			if loc[0] > 0 && watchRefBlocked(text[loc[0]-1]) {
				plain(token)
				continue
			}
			b.WriteString(r.ref(token, base))
		}
	}
	plain(text[last:])
	return b.String()
}

// watchRefBlocked reports whether a # after c is no issue reference, as in
// an HTML entity or a URL fragment.
func watchRefBlocked(c byte) bool {
	return c == '&' || c == '/' || c == '#' || c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// ref records and styles an issue reference, reversed while selected.
func (r *watchChatRenderer) ref(token string, base lipgloss.Style) string {
	target, digits, _ := strings.Cut(token, "#")
	number, _ := strconv.Atoi(digits)
	style := watchChatRefStyle.Inherit(base)
	if len(r.refs) == r.selected {
		style = style.Reverse(true)
	}
	r.refs = append(r.refs, watchChatRef{text: token, target: target, number: number, line: -1})
	return style.Render(token)
}

// tool renders a tool call: one line collapsed, with its arguments and
// result when expanded.
func (r *watchChatRenderer) tool(t watchChatTool) {
	status := "…"
	switch {
	case t.Error != "":
		status = tuiErrorStyle.Render("✗")
	case t.Done || t.Result != "":
		status = tuiSuccessStyle.Render("✓")
	}
	marker := "▸"
	if r.expand {
		marker = "▾"
	}
	summary := watchToolArgsSummary(t.Args)
	line := "  " + tuiDimStyle.Render(marker+" ") + tuiLabelStyle.Render(t.Name)
	if summary != "" {
		line += " " + tuiDimStyle.Render(summary)
	}
	r.lines = append(r.lines, watchTruncate(line+" "+status, r.width))
	if !r.expand {
		return
	}
	detail := func(label, text string) {
		text = strings.TrimSpace(watchANSI.ReplaceAllString(text, ""))
		if text == "" {
			return
		}
		lines := strings.Split(text, "\n")
		const maxLines = 12
		if len(lines) > maxLines {
			lines = append(lines[:maxLines], fmt.Sprintf("… %d more lines", len(lines)-maxLines))
		}
		r.wrapped("    "+tuiDimStyle.Render(label+" "), "      ", tuiDimStyle.Render(lines[0]), true)
		for _, l := range lines[1:] {
			r.wrapped("      ", "      ", tuiDimStyle.Render(l), true)
		}
	}
	if len(t.Args) > 0 {
		args, _ := json.Marshal(t.Args)
		detail("args", string(args))
	}
	detail("result", t.Result)
	detail("error", t.Error)
}

// watchToolArgsSummary is a tool call's arguments on one line, such as
// "number=12 target=o/r".
func watchToolArgsSummary(args map[string]any) string {
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := fmt.Sprint(args[k])
		if s, ok := args[k].(string); ok {
			v = strings.Join(strings.Fields(s), " ")
		}
		if len(v) > 40 {
			v = v[:37] + "..."
		}
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, " ")
}

// chatPaneHeight is the chat pane's height, title and input included.
func (m watchModel) chatPaneHeight() int {
	if !m.chat.open {
		return 0
	}
	avail := max(0, m.height-2)
	if m.chat.full {
		return avail
	}
	// Leave the topRows, one board line and the footer.
	avail = max(0, m.height-m.topRows()-2)
	return min(avail, max(min(6, avail), avail*m.chat.pct/100))
}

// chatInputHeight is the input's height in the chat pane: its soft-wrapped
// rows, up to watchChatMaxInputLines.
func (m watchModel) chatInputHeight() int {
	input := m.chat.input
	input.SetWidth(max(4, m.width))
	rows := 0
	for _, line := range strings.Split(input.Value(), "\n") {
		rows += watchWrapRows([]rune(line), input.Width())
	}
	return max(1, min(watchChatMaxInputLines, rows))
}

// fitChatInput sizes the input to the pane's width and its wrapped rows, and
// scrolls it to keep the cursor row visible.
func (m watchModel) fitChatInput() watchModel {
	m.chat.input.SetWidth(max(4, m.width))
	// The textarea's view state is shared by its copies, and it only scrolls
	// the view as far as needed to show the cursor. When the height changes,
	// reset a copy to scroll back to the top, so that rows that fit again
	// show, then let the update below scroll down to the cursor.
	if h := m.chatInputHeight(); h != m.chat.input.Height() {
		top := m.chat.input
		top.Reset()
		m.chat.input.SetHeight(h)
	}
	// The textarea scrolls only as far as the text its last View laid out,
	// so lay out the new text first.
	_ = m.chat.input.View()
	m.chat.input, _ = m.chat.input.Update(nil)
	return m
}

// watchWrapRows is how many rows the textarea soft-wraps a line into at
// width. It follows the word wrap of bubbles' textarea, including the extra
// row it adds when a line fills the width exactly.
func watchWrapRows(runes []rune, width int) int {
	rows, row, word, spaces := 1, 0, 0, 0
	for _, r := range runes {
		if unicode.IsSpace(r) {
			spaces++
		} else {
			word += uniseg.StringWidth(string(r))
		}
		if spaces > 0 {
			if row+word+spaces > width {
				rows++
				row = 0
			}
			row += word + spaces
			word, spaces = 0, 0
		} else if word+runewidth.RuneWidth(r) > width {
			// A word as wide as the line takes a row of its own.
			if row > 0 {
				rows++
			}
			row, word = word, 0
		}
	}
	if row+word+spaces >= width {
		rows++
	}
	return rows
}

// chatTranscriptHeight is the transcript's height in the chat pane.
func (m watchModel) chatTranscriptHeight() int {
	return max(1, m.chatPaneHeight()-1-m.chatInputHeight())
}

// chatPane is the prompt chat: a titled rule, the transcript at its bottom
// unless scrolled up, with a more-below marker then, and the input.
func (m watchModel) chatPane(height int) []string {
	title := "Prompt chat · " + config.ProjectsAgentName
	switch {
	case m.chat.streaming() || m.chat.replying:
		title += " · replying…"
	case !m.chat.loaded && m.chat.fetching:
		title += " · loading…"
	}
	if m.chat.focus == watchFocusTranscript {
		title += " · [transcript]"
	}
	if m.chat.full {
		title += " · [full screen]"
	}
	out := m.pane(title, nil, 1)
	inputH := m.chatInputHeight()
	h := max(1, height-1-inputH)
	view := m.chatLines(m.width)
	lines := view.lines
	if len(lines) == 0 {
		lines = []string{tuiDimStyle.Render("No messages yet. Ask the Code Loop agent about jobs, file an issue or steer work.")}
	}
	bottom := max(0, len(lines)-h)
	top := bottom
	if !m.chat.follow {
		top = min(m.chat.top, bottom)
	}
	shown := watchFitLines(slices.Clone(lines[top:min(len(lines), top+h)]), h)
	if top < bottom {
		shown[h-1] = watchChatMarker(m.width)
	}
	for _, line := range shown {
		out = append(out, watchTruncate(line, m.width))
	}
	if m.chat.errText != "" {
		out[len(out)-1] = tuiErrorStyle.Render(watchTruncate("! "+m.chat.errText, m.width))
	}
	input := m.chat.input
	input.SetWidth(max(4, m.width))
	input.SetHeight(inputH)
	for _, line := range strings.Split(input.View(), "\n") {
		out = append(out, watchTruncate(line, m.width))
	}
	return watchFitLines(out, height)
}

// watchChatMarker is the line saying the transcript has more below.
func watchChatMarker(width int) string {
	label := " ↓ more below · G or end to follow "
	pad := max(0, (width-lipgloss.Width(label))/2)
	return watchTruncate(tuiSectionStyle.Render(strings.Repeat("─", pad)+label+strings.Repeat("─", pad)), width)
}
