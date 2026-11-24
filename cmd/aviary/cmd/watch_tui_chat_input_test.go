package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

// watchMsgRecorder records the messages a Bubble Tea program reads from its
// input, quitting after the first.
type watchMsgRecorder struct{ msgs *[]tea.Msg }

func (r watchMsgRecorder) Init() tea.Cmd { return nil }
func (r watchMsgRecorder) View() string  { return "" }
func (r watchMsgRecorder) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case tea.WindowSizeMsg:
		return r, nil
	}
	*r.msgs = append(*r.msgs, msg)
	return r, tea.Quit
}

// TestWatchShiftEnterFromTerminalInput feeds the bytes terminals send for
// Shift+Enter through Bubble Tea's input reader.
func TestWatchShiftEnterFromTerminalInput(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		want        bool
	}{
		{"modifyOtherKeys", "\x1b[27;2;13~", true},
		{"CSI u", "\x1b[13;2u", true},
		{"enter", "\r", false},
		{"ctrl+enter", "\x1b[27;5;13~", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var msgs []tea.Msg
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			p := tea.NewProgram(watchMsgRecorder{&msgs}, tea.WithContext(ctx), tea.WithInput(bytes.NewBufferString(tc.input)), tea.WithOutput(&bytes.Buffer{}), tea.WithoutRenderer(), tea.WithoutSignalHandler())
			if _, err := p.Run(); err != nil {
				t.Fatal(err)
			}
			if len(msgs) == 0 {
				t.Fatal("no message read")
			}
			if got := watchShiftEnter(msgs[0]); got != tc.want {
				t.Errorf("watchShiftEnter(%T %v) = %v", msgs[0], msgs[0], got)
			}
		})
	}
}

// watchShiftEnterMsg stands in for Bubble Tea's unknown CSI sequence message.
type watchShiftEnterMsg []byte

func TestWatchTUIChatShiftEnterAddsALine(t *testing.T) {
	m := loadedWatchModel(t, newFakeWatchTools())
	stream := &fakeChatStream{}
	m.stream = stream.stream
	m, _ = watchKey(t, m, "/")
	// Shift+Enter alone sends nothing.
	m = watchUpdate(t, m, watchShiftEnterMsg("\x1b[27;2;13~"))
	if m.chat.streaming || len(stream.args) != 0 {
		t.Fatal("shift+enter alone sent the input")
	}
	m.chat.input.Reset()
	m = watchType(t, m, "a")
	m = watchUpdate(t, m, watchShiftEnterMsg("\x1b[27;2;13~"))
	m = watchType(t, m, "b")
	m = watchUpdate(t, m, watchShiftEnterMsg("\x1b[13;2u"))
	m = watchType(t, m, "c")
	if got := m.chat.input.Value(); got != "a\nb\nc" || m.chat.streaming {
		t.Fatalf("shift+enter: value %q streaming %v", got, m.chat.streaming)
	}
	m, cmd := watchSpecialKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	watchPump(t, m, cmd, nil)
	if len(stream.args) != 1 || stream.args[0]["message"] != "a\nb\nc" {
		t.Fatalf("enter sent %v", stream.args)
	}
}

// TestWatchWrapRowsMatchesTextarea checks watchWrapRows against the rows
// the textarea itself wraps a line into.
func TestWatchWrapRowsMatchesTextarea(t *testing.T) {
	lines := []string{
		"",
		"short",
		"exactly ten",
		strings.Repeat("x", 9),
		strings.Repeat("x", 10),
		strings.Repeat("x", 11),
		strings.Repeat("x", 35),
		strings.Repeat("word ", 12),
		strings.Repeat("ab cde fghij ", 7),
		"a  very   spaced    line     with      gaps",
		"wide 漢字漢字漢字漢字漢字漢字 text",
		strings.Repeat("漢", 17),
		"trailing space ",
	}
	for _, width := range []int{4, 10, 11, 23, 40} {
		for _, line := range lines {
			ta := textarea.New()
			ta.ShowLineNumbers = false
			ta.Prompt = ""
			ta.CharLimit = 0
			ta.SetWidth(width)
			ta.SetValue(line)
			ta.CursorEnd()
			if got, want := watchWrapRows([]rune(line), ta.Width()), ta.LineInfo().Height; got != want {
				t.Errorf("width %d, %q: rows %d, textarea %d", width, line, got, want)
			}
		}
	}
}

func TestWatchTUIChatInputGrowsWithWrappedRows(t *testing.T) {
	m := loadedWatchModel(t, newFakeWatchTools())
	m = watchUpdate(t, m, tea.WindowSizeMsg{Width: 60, Height: 40})
	m, _ = watchKey(t, m, "/")
	// A line about three times the input's width shows as three rows.
	words := strings.Fields(strings.Repeat("alpha beta gamma delta ", 7))
	m = watchType(t, m, strings.Join(words, " "))
	if h := m.chatInputHeight(); h != 3 {
		t.Fatalf("input height = %d, want 3", h)
	}
	pane := m.chatPane(m.chatPaneHeight())
	input := pane[len(pane)-3:]
	var shown []string
	for _, row := range input {
		row = watchStrip(row)
		if w := len([]rune(row)); w > m.width {
			t.Errorf("row wider than the pane (%d > %d): %q", w, m.width, row)
		}
		shown = append(shown, strings.Fields(strings.TrimPrefix(row, "› "))...)
	}
	if strings.Join(shown, " ") != strings.Join(words, " ") {
		t.Errorf("input rows = %q", input)
	}
	// Past the cap, the input stops growing and keeps the cursor row shown.
	m = watchType(t, m, strings.Repeat(" more words here", 30)+" END")
	if h := m.chatInputHeight(); h != watchChatMaxInputLines {
		t.Fatalf("capped input height = %d", h)
	}
	pane = m.chatPane(m.chatPaneHeight())
	last := watchStrip(strings.Join(pane[len(pane)-watchChatMaxInputLines:], "\n"))
	if !strings.Contains(last, "END") {
		t.Errorf("cursor row not shown:\n%s", last)
	}
	// Sending shrinks the input back to one row.
	m.stream = (&fakeChatStream{}).stream
	m, _ = watchSpecialKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if h := m.chatInputHeight(); h != 1 || m.chat.input.Height() != 1 {
		t.Errorf("input height after send = %d / %d", h, m.chat.input.Height())
	}
}
