package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// watchFramePath is the landing page's aviary watch mock: a frame of the
// real TUI, so the mock follows the TUI's layout, colors and labels.
const watchFramePath = "../../../docs/site/.vitepress/theme/components/watch-tui-frame.json"

// watchFrameSpan is a run of one style in a frame row.
type watchFrameSpan struct {
	Text      string `json:"t"`
	FG        string `json:"fg,omitempty"`
	Bold      bool   `json:"b,omitempty"`
	Underline bool   `json:"u,omitempty"`
	Reverse   bool   `json:"r,omitempty"`
}

type watchFrame struct {
	Cols int                `json:"cols"`
	Rows [][]watchFrameSpan `json:"rows"`
}

// TestWatchTUILandingFrame renders the landing page's aviary watch mock
// from the TUI model and checks it matches the committed frame. Run it with
// AVIARY_UPDATE_WATCH_FRAME=1 to rewrite the frame after a TUI change.
func TestWatchTUILandingFrame(t *testing.T) {
	profile, dark := lipgloss.ColorProfile(), lipgloss.HasDarkBackground()
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)
	t.Cleanup(func() {
		lipgloss.SetColorProfile(profile)
		lipgloss.SetHasDarkBackground(dark)
	})

	const cols, rows = 168, 39
	data, err := json.MarshalIndent(watchFrameFromANSI(watchLandingView(t, cols, rows), cols), "", "\t")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if os.Getenv("AVIARY_UPDATE_WATCH_FRAME") != "" {
		if err := os.WriteFile(watchFramePath, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(watchFramePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), data) {
		t.Fatalf("%s is out of date with the TUI; rerun with AVIARY_UPDATE_WATCH_FRAME=1", filepath.Base(watchFramePath))
	}
}

// watchLandingView is the TUI with a job in every board state, the selected
// job's output open and the prompt chat mid-conversation.
func watchLandingView(t *testing.T, cols, rows int) string {
	t.Helper()
	const target = "acme/storefront"
	progress := func(done, total int) *struct {
		Done    int `json:"done"`
		Total   int `json:"total"`
		Percent int `json:"percent"`
	} {
		return &struct {
			Done    int `json:"done"`
			Total   int `json:"total"`
			Percent int `json:"percent"`
		}{done, total, done * 100 / total}
	}
	job := func(n int, title, status string) watchJob {
		return watchJob{Target: target, Number: n, Title: title, Status: status, Agent: "claude", Model: "opus", Started: "2026-11-23T11:" + strconv.Itoa(10+n%50) + ":00Z"}
	}
	queued := job(219, "Show stock on product cards", "queued")
	queued.QueuePosition = 1
	running := job(214, "Fix cart total rounding", "active")
	running.Progress = progress(4, 7)
	running.PullRequest = 231
	running.PullRequestState = "draft"
	running2 := job(216, "Add order export to CSV", "active")
	running2.Agent, running2.Model = "codex", "gpt-5"
	running2.Started = "2026-11-23T11:02:00Z"
	running2.Progress = progress(2, 6)
	running2.PullRequest = 233
	running2.PullRequestState = "draft"
	running3 := job(217, "Paginate the admin orders table", "active")
	running3.Started = "2026-11-23T11:01:00Z"
	running3.Progress = progress(1, 5)
	paused := job(208, "Upgrade the payments SDK", "waiting")
	paused.WaitReason = "waiting on #207"
	done := job(205, "Search ignores accents", "complete")
	done.Progress = progress(6, 6)
	done.PullRequest = 229
	done.PullRequestState = "merged"
	done2 := job(203, "Dark mode for checkout", "complete")
	done2.Progress = progress(5, 5)
	done2.PullRequest = 226
	done2.PullRequestState = "merged"
	paused2 := job(212, "Rename the checkout feature flag", "waiting")
	paused2.WaitReason = "answer needed"
	failed := job(211, "Flaky login e2e test", "failed")
	failed.Progress = progress(3, 5)
	failed.PullRequest = 230
	failed.PullRequestState = "open"
	nightly := "2026-11-24T02:00:00Z"
	weekly := "2026-11-25T09:00:00Z"

	m := newWatchModel(t.Context(), (&fakeWatchTools{}).call, target, "https://localhost:16677/projects")
	m.now = watchTestNow
	m = watchUpdate(t, m, tea.WindowSizeMsg{Width: cols, Height: rows})
	status := watchStatus{
		Running:      true,
		Targets:      []string{target},
		States:       map[string]int{watchScheduled: 2, watchStarting: 1, watchPaused: 2, watchRunning: 3, watchDone: 2, watchFailed: 1},
		Concurrency:  3,
		Delivery:     "poll",
		PollInterval: "1m",
	}
	status.Jobs.Running = 3
	status.Jobs.Merged24h = 2
	m = watchUpdate(t, m, watchDataMsg{
		status:  &status,
		jobs:    []watchJob{queued, running, running2, running3, paused, paused2, done, done2, failed},
		jobsOK:  true,
		tasks:   []watchTask{{ID: "t1", Name: "nightly-triage", Type: "project", Project: target, Enabled: true, NextRun: &nightly}, {ID: "t2", Name: "weekly-deps", Type: "project", Project: target, Enabled: true, NextRun: &weekly}},
		tasksOK: true,
	})
	// Board view, logs hidden, the running column's first job selected
	// with its output open.
	for _, key := range []string{"b", "l", "4", "0"} {
		m = watchUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	}
	m = watchUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = watchUpdate(t, m, watchDataMsg{outputKey: m.outputKey, outputOK: true, output: []string{
		"▸ Read the issue and its comments",
		"▸ Bash gh pr create --draft --title \"Fix cart total rounding\"",
		"▸ Grep \"func Total\" internal/cart",
		"▸ Read internal/cart/total.go",
		"▸ Edit internal/cart/total.go: round each line item to cents before summing",
		"▸ Bash go test ./internal/cart/...",
		"  ok   acme/storefront/internal/cart  0.412s",
		"▸ Bash git commit -m \"Checkpoint issue #214 progress\"",
		"▸ Edit internal/cart/total_test.go: add the 3 × $19.99 case from the issue",
	}})
	m = watchUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = watchUpdate(t, m, watchChatDataMsg{gen: m.chat.gen, entries: []watchChatEntry{
		{role: "user", text: "Why did #211 fail?"},
		{role: "tool", tool: &watchChatTool{Name: "project_job_output", Args: map[string]any{"number": 211, "target": target}, Done: true}},
		{role: "assistant", text: "CI failed in `login.spec.ts`: the e2e test timed out waiting for the redirect, the same flake as #198. The fix itself passed every other check. Want me to retry it?"},
		{role: "user", text: "Yes, retry it."},
		{role: "tool", tool: &watchChatTool{Name: "project_job_retry", Args: map[string]any{"number": 211, "target": target}}},
	}, replying: true})
	return m.View()
}

// watchFrameFromANSI splits a rendered view into rows of styled spans.
func watchFrameFromANSI(view string, cols int) watchFrame {
	frame := watchFrame{Cols: cols}
	var b watchFrameRowBuilder
	for _, line := range strings.Split(view, "\n") {
		frame.Rows = append(frame.Rows, b.row(line))
	}
	return frame
}

// watchFrameRowBuilder turns rendered lines into styled spans, carrying the
// current style from one line to the next as a terminal does.
type watchFrameRowBuilder struct {
	cur   watchFrameSpan
	spans []watchFrameSpan
	text  strings.Builder
}

// row returns line's spans, merging neighbors of the same style.
func (b *watchFrameRowBuilder) row(line string) []watchFrameSpan {
	b.spans = []watchFrameSpan{}
	for i := 0; i < len(line); {
		if line[i] != 0x1b {
			b.text.WriteByte(line[i])
			i++
			continue
		}
		loc := watchANSI.FindStringIndex(line[i:])
		if loc == nil || loc[0] != 0 {
			i++
			continue
		}
		seq := line[i : i+loc[1]]
		i += loc[1]
		if !strings.HasPrefix(seq, "\x1b[") || !strings.HasSuffix(seq, "m") {
			continue
		}
		b.flush()
		b.cur = watchApplySGR(b.cur, strings.Split(seq[2:len(seq)-1], ";"))
	}
	b.flush()
	return b.spans
}

// flush ends the pending text as a span in the current style.
func (b *watchFrameRowBuilder) flush() {
	if b.text.Len() == 0 {
		return
	}
	span := b.cur
	span.Text = b.text.String()
	b.text.Reset()
	if n := len(b.spans); n > 0 && watchFrameSameStyle(b.spans[n-1], span) {
		b.spans[n-1].Text += span.Text
		return
	}
	b.spans = append(b.spans, span)
}

// watchFrameSameStyle reports whether two spans are styled alike.
func watchFrameSameStyle(a, b watchFrameSpan) bool {
	return a.FG == b.FG && a.Bold == b.Bold && a.Underline == b.Underline && a.Reverse == b.Reverse
}

// watchApplySGR applies one SGR sequence's parameters to a span's style.
func watchApplySGR(s watchFrameSpan, params []string) watchFrameSpan {
	for i := 0; i < len(params); i++ {
		switch params[i] {
		case "", "0":
			s = watchFrameSpan{}
		case "1":
			s.Bold = true
		case "22":
			s.Bold = false
		case "4":
			s.Underline = true
		case "24":
			s.Underline = false
		case "7":
			s.Reverse = true
		case "27":
			s.Reverse = false
		case "39":
			s.FG = ""
		case "38":
			if i+4 < len(params) && params[i+1] == "2" {
				rgb := make([]byte, 3)
				for j := range rgb {
					v, _ := strconv.Atoi(params[i+2+j])
					rgb[j] = byte(v)
				}
				s.FG = "#" + strings.ToLower(strconv.FormatUint(uint64(rgb[0])<<16|uint64(rgb[1])<<8|uint64(rgb[2])|1<<24, 16)[1:])
				i += 4
			}
		}
	}
	return s
}
