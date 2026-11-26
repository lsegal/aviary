package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/llm"
	internalmcp "github.com/lsegal/aviary/internal/mcp"
	"github.com/lsegal/aviary/internal/store"
)

// fakeChatStream streams canned agent_run progress and records the turns.
type fakeChatStream struct {
	mu     sync.Mutex
	chunks []string
	args   []map[string]any
}

func (f *fakeChatStream) stream(_ context.Context, name string, args map[string]any, onProgress func(string)) (string, error) {
	f.mu.Lock()
	f.args = append(f.args, args)
	chunks := f.chunks
	f.mu.Unlock()
	if name != "agent_run" {
		return "", fmt.Errorf("unexpected tool %s", name)
	}
	for _, c := range chunks {
		onProgress(c)
	}
	return "", nil
}

// watchType types text into the focused chat input.
func watchType(t *testing.T, m watchModel, text string) watchModel {
	t.Helper()
	for _, r := range text {
		m = watchUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

// watchSpecialKey sends a non-rune key and returns the model and command.
func watchSpecialKey(t *testing.T, m watchModel, msg tea.KeyMsg) (watchModel, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(watchModel), cmd
}

// watchPump runs cmd and feeds its messages back until the chat turn ends,
// calling each with the model after every streamed message.
func watchPump(t *testing.T, m watchModel, cmd tea.Cmd, each func(watchModel)) watchModel {
	t.Helper()
	for i := 0; cmd != nil && i < 100; i++ {
		msg := cmd()
		if msg == nil {
			return m
		}
		var next tea.Model
		next, cmd = m.Update(msg)
		m = next.(watchModel)
		if each != nil {
			each(m)
		}
		if _, ok := msg.(watchChatDataMsg); ok {
			return m
		}
	}
	return m
}

func chatHistoryResponse(messages ...map[string]any) []map[string]any {
	// session_messages is read newest first.
	out := make([]map[string]any, 0, len(messages))
	for i := len(messages) - 1; i >= 0; i-- {
		out = append(out, messages[i])
	}
	return out
}

func TestWatchTUIChatSendsAndStreamsAReply(t *testing.T) {
	tools := newFakeWatchTools()
	stream := &fakeChatStream{chunks: []string{
		"Job ",
		`[tool]{"id":"t1","name":"project_job_get","args":{"target":"o/r","number":3}}`,
		`[tool]{"id":"t1","name":"project_job_get","result":"ok","done":true}`,
		"**#3** is running.",
	}}
	m := loadedWatchModel(t, tools)
	m.stream = stream.stream

	m, cmd := watchSpecialKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if !m.chat.open || m.chat.focus != watchFocusInput || cmd == nil {
		t.Fatalf("c did not open the chat with its input focused: %+v", m.chat.focus)
	}
	m = watchUpdate(t, m, cmd())
	m = watchType(t, m, "what is running?")
	tools.responses["session_messages"] = chatHistoryResponse(
		map[string]any{"role": "user", "content": "what is running?"},
		map[string]any{"role": "tool", "content": `{"id":"t1","name":"project_job_get","args":{"target":"o/r","number":3},"result":"ok"}`},
		map[string]any{"role": "assistant", "content": "**#3** is running."},
	)
	m, cmd = watchSpecialKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.chat.streaming() || m.chat.input.Value() != "" {
		t.Fatalf("enter did not send: streaming %v input %q", m.chat.streaming(), m.chat.input.Value())
	}
	var midStream []string
	m = watchPump(t, m, cmd, func(m watchModel) {
		if m.chat.streaming() {
			midStream = append(midStream, watchStrip(strings.Join(m.chatLines(m.width).lines, "\n")))
		}
	})
	checkChatAgentRun(t, stream, "what is running?")
	checkStreamedChatReply(t, midStream)
	// Once the turn ends the transcript is the session as the server saved
	// it.
	if m.chat.streaming() || len(m.chat.turns) != 0 || len(tools.callsTo("session_messages")) < 2 {
		t.Fatalf("turn did not finish: streaming %v pending %d", m.chat.streaming(), len(m.chat.turns))
	}
	got := watchStrip(strings.Join(m.chatLines(m.width).lines, "\n"))
	if !strings.Contains(got, "#3 is running.") || strings.Count(got, "project_job_get") != 1 {
		t.Errorf("saved transcript:\n%s", got)
	}
	// e expands tool calls in the transcript.
	m = watchUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m, _ = watchKey(t, m, "e")
	if got := watchStrip(strings.Join(m.chatLines(m.width).lines, "\n")); !strings.Contains(got, "▾ project_job_get") || !strings.Contains(got, "result ok") {
		t.Errorf("expanded tool call:\n%s", got)
	}
}

// checkChatAgentRun checks that the chat ran the Code Loop agent once, in
// its chat session for o/r with tool progress, with message.
func checkChatAgentRun(t *testing.T, stream *fakeChatStream, message string) {
	t.Helper()
	if len(stream.args) != 1 {
		t.Fatalf("agent_run calls = %d", len(stream.args))
	}
	args := stream.args[0]
	if args["name"] != config.ProjectsAgentName || args["session"] != config.ProjectsChatSession || args["include_tool_progress"] != true || args["project"] != "o/r" || args["message"] != message {
		t.Errorf("agent_run args = %v", args)
	}
}

// checkStreamedChatReply checks the transcripts seen while the reply
// streamed: it starts with the first chunk and ends with the question, the
// collapsed tool call and the answer.
func checkStreamedChatReply(t *testing.T, midStream []string) {
	t.Helper()
	if len(midStream) == 0 || !strings.Contains(midStream[0], "Job") {
		t.Fatalf("the reply did not stream: %q", midStream)
	}
	last := midStream[len(midStream)-1]
	for _, want := range []string{"you", "what is running?", "▸ project_job_get number=3 target=o/r ✓", "#3 is running."} {
		if !strings.Contains(last, want) {
			t.Errorf("streamed transcript lacks %q:\n%s", want, last)
		}
	}
	if strings.Contains(last, "result ok") {
		t.Errorf("tool calls are not collapsed by default:\n%s", last)
	}
}

func TestWatchTUIChatInputKeys(t *testing.T) {
	tools := newFakeWatchTools()
	m := loadedWatchModel(t, tools)
	m.stream = (&fakeChatStream{}).stream
	before := watchStrip(m.View())
	m, _ = watchKey(t, m, "/")
	if !m.chat.open || m.chat.focus != watchFocusInput {
		t.Fatal("/ did not focus the chat input")
	}
	// Alt+Enter adds a line instead of sending.
	m = watchType(t, m, "one")
	m = watchUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	m = watchType(t, m, "two")
	if got := m.chat.input.Value(); got != "one\ntwo" || m.chat.streaming() {
		t.Fatalf("alt+enter: value %q streaming %v", got, m.chat.streaming())
	}
	m, cmd := watchSpecialKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = watchPump(t, m, cmd, nil)
	m = watchType(t, m, "three")
	m, cmd = watchSpecialKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = watchPump(t, m, cmd, nil)
	// ↑ recalls earlier inputs, ↓ comes back to the draft.
	m = watchType(t, m, "draft")
	m = watchUpdate(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if got := m.chat.input.Value(); got != "three" {
		t.Errorf("first ↑ = %q", got)
	}
	m = watchUpdate(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if got := m.chat.input.Value(); got != "one\ntwo" {
		t.Errorf("second ↑ = %q", got)
	}
	m.chat.input.CursorEnd()
	for m.chat.input.Line() < m.chat.input.LineCount()-1 {
		m.chat.input.CursorDown()
	}
	m = watchUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = watchUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if got := m.chat.input.Value(); got != "draft" {
		t.Errorf("↓ back to the draft = %q", got)
	}
	// Board keys type into the input while it has focus.
	m = watchType(t, m, "q")
	if !strings.HasSuffix(m.chat.input.Value(), "q") {
		t.Error("q did not type into the input")
	}
	// Esc returns focus to the board; the chat stays open, and board keys
	// work again.
	m = watchUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.chat.focus != watchFocusBoard || !m.chat.open {
		t.Fatalf("esc: focus %q open %v", m.chat.focus, m.chat.open)
	}
	// c closes the chat and restores the board's layout.
	m, _ = watchKey(t, m, "c")
	if m.chat.open || watchStrip(m.View()) != before {
		t.Errorf("closing the chat did not restore the layout:\n%s\nwas\n%s", watchStrip(m.View()), before)
	}
	m, _ = watchKey(t, m, "3")
	if m.filter != watchPaused {
		t.Errorf("board key after closing: filter %q", m.filter)
	}
}

func longChatHistory(n int) []map[string]any {
	var messages []map[string]any
	for i := range n {
		messages = append(messages,
			map[string]any{"role": "user", "content": fmt.Sprintf("question %d", i)},
			map[string]any{"role": "assistant", "content": fmt.Sprintf("answer %d", i)},
		)
	}
	return messages
}

func TestWatchTUIChatScrollFollow(t *testing.T) {
	tools := newFakeWatchTools()
	tools.responses["session_messages"] = chatHistoryResponse(longChatHistory(30)...)
	m := loadedWatchModel(t, tools)
	m, cmd := watchSpecialKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = watchUpdate(t, m, cmd())
	// The chat opens at its bottom.
	view := watchStrip(m.View())
	if !strings.Contains(view, "answer 29") || strings.Contains(view, "more below") {
		t.Fatalf("chat did not open at its bottom:\n%s", view)
	}
	// New output stays in view while at the bottom.
	tools.responses["session_messages"] = chatHistoryResponse(longChatHistory(31)...)
	m = watchUpdate(t, m, m.chatFetchCmd()())
	if view := watchStrip(m.View()); !strings.Contains(view, "answer 30") {
		t.Fatalf("new output not followed:\n%s", view)
	}
	// Scrolling up stops following and shows the marker; new output does
	// not move the view.
	m = watchUpdate(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	if m.chat.follow {
		t.Fatal("pgup kept following")
	}
	scrolled := watchStrip(strings.Join(m.chatPane(m.chatPaneHeight()), "\n"))
	if !strings.Contains(scrolled, "more below") || strings.Contains(scrolled, "answer 30") {
		t.Fatalf("scrolled view:\n%s", scrolled)
	}
	tools.responses["session_messages"] = chatHistoryResponse(longChatHistory(33)...)
	m = watchUpdate(t, m, m.chatFetchCmd()())
	if got := watchStrip(strings.Join(m.chatPane(m.chatPaneHeight()), "\n")); got != scrolled {
		t.Errorf("new output moved the scrolled view:\n%s\nwas\n%s", got, scrolled)
	}
	// Scrolling back to the bottom follows again and hides the marker.
	for range 50 {
		m = watchUpdate(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	}
	view = watchStrip(m.View())
	if !m.chat.follow || strings.Contains(view, "more below") || !strings.Contains(view, "answer 32") {
		t.Fatalf("back at the bottom: follow %v\n%s", m.chat.follow, view)
	}
	// In the transcript, k scrolls up and G follows again.
	m = watchUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m, _ = watchKey(t, m, "k")
	if m.chat.follow {
		t.Error("k kept following")
	}
	m, _ = watchKey(t, m, "G")
	if !m.chat.follow {
		t.Error("G did not follow")
	}
}

func TestWatchTUIChatReferencesJumpToTheBoard(t *testing.T) {
	tools := newFakeWatchTools()
	tools.responses["session_messages"] = chatHistoryResponse(
		map[string]any{"role": "user", "content": "status?"},
		map[string]any{"role": "assistant", "content": "- **#3** is running\n- PR #9 merged\n- `#77` is code, x/y#5 is elsewhere\n\nSee [the board](https://example.com/board) and &#39;."},
	)
	m := loadedWatchModel(t, tools)
	m, cmd := watchSpecialKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = watchUpdate(t, m, cmd())
	view := m.chatLines(m.width)
	var refs []string
	for _, r := range view.refs {
		refs = append(refs, r.text)
	}
	if strings.Join(refs, ",") != "#3,#9,x/y#5" {
		t.Fatalf("refs = %v", refs)
	}
	text := watchStrip(strings.Join(view.lines, "\n"))
	for _, want := range []string{"• #3 is running", "the board (https://example.com/board)", "#77 is code"} {
		if !strings.Contains(text, want) {
			t.Errorf("rendered markdown lacks %q:\n%s", want, text)
		}
	}
	// n selects the newest reference, N goes back; enter focuses the job.
	m = watchUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m, _ = watchKey(t, m, "n")
	m, _ = watchKey(t, m, "N")
	m, _ = watchKey(t, m, "N")
	if m.chat.ref != 0 {
		t.Fatalf("selected ref = %d", m.chat.ref)
	}
	m.filter = watchDone
	m, _ = watchKey(t, m, "enter")
	if it, ok := m.selectedItem(); !ok || it.key != "o/r#3" || m.chat.focus != watchFocusBoard || m.filter != "" {
		t.Errorf("enter on #3: selected %v filter %q focus %q", it.key, m.filter, m.chat.focus)
	}
	// A pull request reference finds the job that opened it.
	m = watchUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = watchUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m.chat.ref = 1
	m, _ = watchKey(t, m, "enter")
	if it, _ := m.selectedItem(); it.key != "o/r#4" {
		t.Errorf("enter on PR #9 selected %q", it.key)
	}
	// A reference with no job says so.
	m = watchUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = watchUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m.chat.ref = 2
	m, _ = watchKey(t, m, "enter")
	if !strings.Contains(m.chat.errText, "no job for x/y#5") {
		t.Errorf("missing job: %q", m.chat.errText)
	}
}

func TestWatchTUIChatMarkdownWraps(t *testing.T) {
	m := newWatchModel(context.Background(), newFakeWatchTools().call, "o/r", "")
	m.chat.entries = []watchChatEntry{{role: "assistant", text: "# Heading\n\nA **bold** paragraph that is long enough to wrap across several lines of a narrow pane.\n\n```\nfunc main() { fmt.Println(\"a very long line of code that must be wrapped too\") }\n```\n1. first item that also wraps around the pane\n> quoted"}}
	view := m.chatLines(30)
	for _, line := range view.lines {
		if w := lipgloss.Width(line); w > 30 {
			t.Errorf("line wider than the pane (%d): %q", w, watchStrip(line))
		}
	}
	text := watchStrip(strings.Join(view.lines, "\n"))
	for _, want := range []string{"Heading", "A bold paragraph", "│ func main()", "1. first item", "▏ quoted"} {
		if !strings.Contains(text, want) {
			t.Errorf("markdown lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "**") || strings.Contains(text, "```") {
		t.Errorf("markdown markers left in:\n%s", text)
	}
}

func TestWatchTUIChatResizeKeepsTheLayout(t *testing.T) {
	tools := newFakeWatchTools()
	tools.responses["session_messages"] = chatHistoryResponse(longChatHistory(20)...)
	m := loadedWatchModel(t, tools)
	m, cmd := watchSpecialKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = watchUpdate(t, m, cmd())
	m, _ = watchKey(t, m, "enter") // types into the input: no output pane
	m = watchUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m, _ = watchKey(t, m, "enter")
	if !m.showOutput {
		t.Fatal("output pane not open")
	}
	check := func(label string, m watchModel) {
		t.Helper()
		checkWatchViewFits(t, label, m)
	}
	for _, size := range [][2]int{{160, 40}, {80, 24}, {60, 12}, {40, 8}, {200, 60}, {30, 6}} {
		m = watchUpdate(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		check("split", m)
		for range 5 {
			m, _ = watchKey(t, m, "+")
		}
		check("tall", m)
		for range 8 {
			m, _ = watchKey(t, m, "-")
		}
		check("short", m)
		m, _ = watchKey(t, m, "z")
		check("full", m)
		m, _ = watchKey(t, m, "z")
		// The chat stays at its bottom across resizes.
		if view := watchStrip(strings.Join(m.chatPane(m.chatPaneHeight()), "\n")); size[1] >= 12 && !strings.Contains(view, "answer 19") {
			t.Errorf("%dx%d: chat lost its bottom:\n%s", size[0], size[1], view)
		}
	}
}

// checkWatchViewFits checks that m's view fills its height, no line is wider
// than it, and the header shows unless the chat is full or the window short.
func checkWatchViewFits(t *testing.T, label string, m watchModel) {
	t.Helper()
	lines := strings.Split(m.View(), "\n")
	if len(lines) != m.height {
		t.Errorf("%s %dx%d: %d lines", label, m.width, m.height, len(lines))
	}
	for _, line := range lines {
		if w := lipgloss.Width(line); w > m.width {
			t.Errorf("%s %dx%d: line %d wide: %q", label, m.width, m.height, w, watchStrip(line))
			break
		}
	}
	if m.height >= 12 && !m.chat.full && !strings.Contains(lines[0], "aviary watch") {
		t.Errorf("%s %dx%d: header gone", label, m.width, m.height)
	}
}

func TestWatchTUIChatHelpAndFooter(t *testing.T) {
	m := loadedWatchModel(t, newFakeWatchTools())
	if !strings.Contains(watchStrip(m.footerLine()), "c chat") {
		t.Errorf("footer = %q", watchStrip(m.footerLine()))
	}
	m, _ = watchKey(t, m, "?")
	help := watchStrip(m.View())
	for _, want := range []string{"open / close the prompt chat", "alt+enter", "next / previous #123 reference", "expand / collapse tool calls"} {
		if !strings.Contains(help, want) {
			t.Errorf("help lacks %q", want)
		}
	}
}

// watchChatModel answers OpenAI-style streaming chat requests with
// "echo: <the last user message>".
type watchChatModel struct{}

func (watchChatModel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(body, &req)
	last := ""
	for _, m := range req.Messages {
		if m.Role == "user" {
			last = watchChatContentText(m.Content, last)
		}
	}
	// The question follows the server's "<user> " envelope.
	if i := strings.LastIndex(last, "> "); i >= 0 {
		last = last[i+2:]
	}
	w.Header().Set("Content-Type", "text/event-stream")
	for _, delta := range []map[string]any{{"role": "assistant", "content": "echo: "}, {"content": last}} {
		data, _ := json.Marshal(map[string]any{
			"id": "c", "object": "chat.completion.chunk", "created": 0, "model": "m",
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}},
		})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	}
	data, _ := json.Marshal(map[string]any{
		"id": "c", "object": "chat.completion.chunk", "created": 0, "model": "m",
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
	})
	_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", data)
}

// watchChatContentText returns a message content's text: the string itself,
// or the last text part of a parts list. It returns prev for content with
// no text.
func watchChatContentText(content any, prev string) string {
	if s, ok := content.(string); ok {
		return s
	}
	parts, _ := content.([]any)
	for _, p := range parts {
		if part, ok := p.(map[string]any); ok && part["type"] == "text" {
			prev = fmt.Sprint(part["text"])
		}
	}
	return prev
}

// TestWatchTUIChatSharesTheWebSession runs the Code Loop agent in-process
// against a scripted model, as a watch session does, and checks that the
// TUI's chat and the web chat's calls see one session both ways.
func TestWatchTUIChatSharesTheWebSession(t *testing.T) {
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir(dataDir) })
	if err := store.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(watchChatModel{})
	t.Cleanup(srv.Close)
	factory := llm.NewFactory(func(string) (string, error) { return "", nil }).
		WithProviderOptionsResolver(func(string) (llm.ProviderOptions, bool) {
			return llm.ProviderOptions{BaseURI: srv.URL + "/v1"}, true
		})
	mgr := agent.NewManager(factory)
	t.Cleanup(mgr.Stop)
	mgr.Reconcile(&config.Config{Agents: []config.AgentConfig{{Name: config.ProjectsAgentName, Model: "vllm/test"}}})
	old := internalmcp.GetDeps()
	t.Cleanup(func() { internalmcp.SetDeps(old) })
	internalmcp.SetDeps(&internalmcp.Deps{Agents: mgr})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	client, err := internalmcp.NewInProcessClient(ctx, internalmcp.NewServer())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	call := func(ctx context.Context, name string, args map[string]any) (string, error) {
		return client.CallToolText(ctx, name, args)
	}

	// The web chat sends a message the way useStream does.
	if _, err := client.CallToolText(ctx, "agent_run", map[string]any{
		"name": config.ProjectsAgentName, "message": "from the web", "session": config.ProjectsChatSession, "include_tool_progress": true,
	}); err != nil {
		t.Fatal(err)
	}

	m := newWatchModel(ctx, call, "o/r", "")
	m.stream = watchStreamCalls(client)
	m = watchUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m, cmd := watchSpecialKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = watchUpdate(t, m, cmd())
	got := watchStrip(strings.Join(m.chatLines(m.width).lines, "\n"))
	if !strings.Contains(got, "from the web") || !strings.Contains(got, "echo: from the web") {
		t.Fatalf("the web's turn is missing from the TUI:\n%s", got)
	}

	// The TUI sends one, streaming the reply.
	m = watchType(t, m, "from the tui")
	m, cmd = watchSpecialKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	streamed := false
	m = watchPumpUntil(t, m, cmd, 10*time.Second, func(m watchModel) {
		if m.chat.streaming() && strings.Contains(watchStrip(strings.Join(m.chatLines(m.width).lines, "\n")), "echo:") {
			streamed = true
		}
	})
	if !streamed {
		t.Error("the TUI's reply did not stream")
	}
	if m.chat.errText != "" {
		t.Fatalf("chat error: %s", m.chat.errText)
	}

	// The web chat reads the session as ChatView does and sees the TUI's
	// turn after its own.
	raw, err := client.CallToolText(ctx, "session_messages", map[string]any{"agent": config.ProjectsAgentName, "session_id": config.ProjectsChatSession})
	if err != nil {
		t.Fatal(err)
	}
	var messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(raw), &messages); err != nil {
		t.Fatal(err)
	}
	var seen []string
	for _, msg := range messages {
		seen = append(seen, msg.Role+": "+msg.Content)
	}
	want := []string{"user: from the web", "assistant: echo: from the web", "user: from the tui", "assistant: echo: from the tui"}
	if strings.Join(seen, "|") != strings.Join(want, "|") {
		t.Errorf("web session messages = %q, want %q", seen, want)
	}
}

// watchPumpUntil is watchPump with a deadline, for turns that run for real.
func watchPumpUntil(t *testing.T, m watchModel, cmd tea.Cmd, limit time.Duration, each func(watchModel)) watchModel {
	t.Helper()
	done := make(chan watchModel, 1)
	go func() { done <- watchPump(t, m, cmd, each) }()
	select {
	case m = <-done:
		return m
	case <-time.After(limit):
		t.Fatal("chat turn did not finish")
		return m
	}
}

func TestWatchTUIChatTranscriptKeepsBoardActionsAway(t *testing.T) {
	tools := newFakeWatchTools()
	m := loadedWatchModel(t, tools)
	m.setFilter(watchPaused)
	m, cmd := watchSpecialKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = watchUpdate(t, m, cmd())
	m = watchUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	for _, key := range []string{"r", "s", "u", "1"} {
		var msg tea.Msg
		m, msg = watchKey(t, m, key)
		if msg != nil || m.confirm != nil || m.filter != watchPaused {
			t.Errorf("%s acted on the board from the transcript", key)
		}
	}
	if len(tools.callsTo("project_job_retry"))+len(tools.callsTo("project_job_resume")) != 0 {
		t.Error("job actions ran from the transcript")
	}
}

// gatedChatStream streams "<message>: one", then waits for the message's
// release before streaming " two" and ending with its error, if any.
type gatedChatStream struct {
	mu      sync.Mutex
	release map[string]chan error
	sent    []string
}

func (g *gatedChatStream) gate(message string) chan error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.release == nil {
		g.release = map[string]chan error{}
	}
	if g.release[message] == nil {
		g.release[message] = make(chan error, 1)
	}
	return g.release[message]
}

func (g *gatedChatStream) stream(ctx context.Context, _ string, args map[string]any, onProgress func(string)) (string, error) {
	message := fmt.Sprint(args["message"])
	g.mu.Lock()
	g.sent = append(g.sent, message)
	g.mu.Unlock()
	onProgress(message + ": one")
	select {
	case err := <-g.gate(message):
		if err != nil {
			return "", err
		}
	case <-ctx.Done():
		return "", ctx.Err()
	}
	onProgress(" two")
	return "", nil
}

// watchChatSend types text into the chat input and sends it, returning the
// model after the turn's first streamed message and the command waiting for
// its next one.
func watchChatSend(t *testing.T, m watchModel, text string) (watchModel, tea.Cmd) {
	t.Helper()
	m = watchType(t, m, text)
	m, cmd := watchSpecialKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("enter did not send %q: %s", text, m.chat.errText)
	}
	next, wait := m.Update(cmd())
	return next.(watchModel), wait
}

// watchChatFinish releases text's turn with err and feeds its remaining
// messages in, returning the model and the command its end returned.
func watchChatFinish(t *testing.T, m watchModel, g *gatedChatStream, wait tea.Cmd, text string, err error) (watchModel, tea.Cmd) {
	t.Helper()
	g.gate(text) <- err
	for {
		msg := wait()
		var next tea.Model
		next, wait = m.Update(msg)
		m = next.(watchModel)
		if _, ok := msg.(watchChatDoneMsg); ok {
			return m, wait
		}
	}
}

func watchChatTranscript(m watchModel) string {
	return watchStrip(strings.Join(m.chatLines(m.width).lines, "\n"))
}

func TestWatchTUIChatSendsWhileAReplyStreams(t *testing.T) {
	tools := newFakeWatchTools()
	g := &gatedChatStream{}
	m := loadedWatchModel(t, tools)
	m.stream = g.stream
	m, cmd := watchSpecialKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = watchUpdate(t, m, cmd())

	m, waitFirst := watchChatSend(t, m, "first")
	if !strings.Contains(watchStrip(m.chatPane(m.chatPaneHeight())[0]), "replying…") {
		t.Fatalf("title does not show the reply running:\n%s", m.chatPane(m.chatPaneHeight())[0])
	}
	// Two more sends while the first reply streams are taken at once.
	m, waitSecond := watchChatSend(t, m, "second")
	m, waitThird := watchChatSend(t, m, "third")
	if m.chat.errText != "" || m.chat.input.Value() != "" {
		t.Fatalf("send during a reply: error %q input %q", m.chat.errText, m.chat.input.Value())
	}
	if got := strings.Join(g.sent, ","); got != "first,second,third" {
		t.Fatalf("agent_run messages = %q", got)
	}
	checkChatTranscriptHas(t, watchChatTranscript(m), "you · sending…\n  first\n\ncode-loop\n  first: one", "you · sending…\n  second\n\ncode-loop\n  second: one", "you · sending…\n  third")
	// The first reply keeps streaming after the later sends.
	m, cmd = watchChatFinish(t, m, g, waitFirst, "first", nil)
	if cmd != nil || !m.chat.streaming() {
		t.Fatal("the session was read back while sends were still running")
	}
	got := watchChatTranscript(m)
	if !strings.Contains(got, "first: one two") || !strings.Contains(got, "you · sent\n  first") {
		t.Errorf("finished first turn:\n%s", got)
	}
	m, _ = watchChatFinish(t, m, g, waitSecond, "second", nil)
	tools.responses["session_messages"] = chatHistoryResponse(
		map[string]any{"role": "user", "content": "first"},
		map[string]any{"role": "assistant", "content": "first: one two"},
		map[string]any{"role": "user", "content": "second"},
		map[string]any{"role": "assistant", "content": "second: one two"},
		map[string]any{"role": "user", "content": "third"},
		map[string]any{"role": "assistant", "content": "third: one two"},
	)
	m, cmd = watchChatFinish(t, m, g, waitThird, "third", nil)
	if cmd == nil {
		t.Fatal("the last turn's end did not read the session back")
	}
	m = watchUpdate(t, m, cmd())
	// The read replaces the sends: every message shows once, in order.
	got = watchChatTranscript(m)
	if len(m.chat.turns) != 0 || strings.Contains(got, "sending") || strings.Contains(got, "· sent") {
		t.Fatalf("pending sends survived the read:\n%s", got)
	}
	want := []string{"first", "first: one two", "second", "second: one two", "third", "third: one two"}
	seen := watchChatTranscriptMessages(got)
	if strings.Join(seen, "|") != strings.Join(want, "|") {
		t.Errorf("transcript messages = %q, want %q", seen, want)
	}
	// ↑ recalls the messages sent during the reply.
	m = watchUpdate(t, m, tea.KeyMsg{Type: tea.KeyUp})
	m = watchUpdate(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if got := m.chat.input.Value(); got != "second" {
		t.Errorf("second ↑ = %q", got)
	}
}

// checkChatTranscriptHas checks that got, with the agent named code-loop,
// has every want.
func checkChatTranscriptHas(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(strings.ReplaceAll(got, config.ProjectsAgentName, "code-loop"), want) {
			t.Errorf("transcript lacks %q:\n%s", want, got)
		}
	}
}

// watchChatTranscriptMessages returns the indented message lines of a
// transcript, trimmed.
func watchChatTranscriptMessages(transcript string) []string {
	var seen []string
	for _, line := range strings.Split(transcript, "\n") {
		if strings.HasPrefix(line, "  ") {
			seen = append(seen, strings.TrimSpace(line))
		}
	}
	return seen
}

func TestWatchTUIChatShowsAFailedSendInPlace(t *testing.T) {
	tools := newFakeWatchTools()
	g := &gatedChatStream{}
	m := loadedWatchModel(t, tools)
	m.stream = g.stream
	m, cmd := watchSpecialKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = watchUpdate(t, m, cmd())

	m, waitFirst := watchChatSend(t, m, "first")
	m, waitSecond := watchChatSend(t, m, "second")
	m, waitThird := watchChatSend(t, m, "third")
	m, _ = watchChatFinish(t, m, g, waitSecond, "second", fmt.Errorf("steer refused"))
	if got := watchChatTranscript(m); !strings.Contains(got, "you · not delivered\n  second\n\n"+config.ProjectsAgentName+"\n  second: one\n\n  ! steer refused") {
		t.Errorf("failed send lacks its error:\n%s", got)
	}
	m, _ = watchChatFinish(t, m, g, waitThird, "third", fmt.Errorf("lost"))
	// The server saved the second message but not the third.
	tools.responses["session_messages"] = chatHistoryResponse(
		map[string]any{"role": "user", "content": "first"},
		map[string]any{"role": "user", "content": "second"},
		map[string]any{"role": "assistant", "content": "first: one two"},
	)
	m, cmd = watchChatFinish(t, m, g, waitFirst, "first", nil)
	m = watchUpdate(t, m, cmd())
	check := func(when string) {
		t.Helper()
		got := watchChatTranscript(m)
		want := []string{"first", "second", "first: one two", "! steer refused", "third", "! lost"}
		var seen []string
		for _, line := range strings.Split(got, "\n") {
			if strings.HasPrefix(line, "  ") {
				seen = append(seen, strings.TrimSpace(line))
			}
		}
		if strings.Join(seen, "|") != strings.Join(want, "|") {
			t.Errorf("%s: transcript messages = %q, want %q", when, seen, want)
		}
	}
	if got := watchChatTranscript(m); !strings.Contains(got, "you · not delivered\n  third") || strings.Count(got, "not delivered") != 1 {
		t.Errorf("the unsaved failed send is not marked:\n%s", got)
	}
	check("after the read")
	// The errors stay in place on later reads.
	m, cmd = m.fetchChat()
	m = watchUpdate(t, m, cmd())
	check("after another read")
}
