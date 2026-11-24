package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// The watch TUI queues jobs as the web board's Queue menu does (#1095): m
// queues the selected fix job's pull request for gh-safe-merge, and Q opens
// a prompt that takes a skill and numbers, such as "merge 1201 1207".

// watchQueueSkills maps the queue prompt's first word to its skill.
var watchQueueSkills = map[string]string{
	"merge":   "gh-safe-merge",
	"m":       "gh-safe-merge",
	"discuss": "gh-discuss",
	"d":       "gh-discuss",
	"fix":     "gh-fix",
	"f":       "gh-fix",
}

// watchQueueResult is one entry of project_job_queue's results.
type watchQueueResult struct {
	Number   int    `json:"number"`
	Queued   bool   `json:"queued"`
	OwnedBy  string `json:"owned_by"`
	Reason   string `json:"reason"`
	Position int    `json:"position"`
}

// watchQueueMsg is a finished project_job_queue call.
type watchQueueMsg struct {
	skill   string
	results []watchQueueResult
	err     error
}

// parseWatchQueue reads the queue prompt: a skill word, then numbers with an
// optional leading "#", separated by spaces or commas.
func parseWatchQueue(text string) (string, []int, error) {
	fields := strings.FieldsFunc(text, func(r rune) bool { return r == ' ' || r == ',' })
	if len(fields) == 0 {
		return "", nil, fmt.Errorf("type merge, discuss or fix, then numbers")
	}
	skill, ok := watchQueueSkills[strings.ToLower(fields[0])]
	if !ok {
		return "", nil, fmt.Errorf("%q is not merge, discuss or fix", fields[0])
	}
	var numbers []int
	for _, field := range fields[1:] {
		n, err := strconv.Atoi(strings.TrimPrefix(field, "#"))
		if err != nil || n <= 0 {
			return "", nil, fmt.Errorf("%q is not a number", field)
		}
		numbers = append(numbers, n)
	}
	if len(numbers) == 0 {
		return "", nil, fmt.Errorf("name at least one number to queue")
	}
	return skill, numbers, nil
}

// queueTarget is the project a queue call goes to: the selected job's, or
// the only watched project.
func (m watchModel) queueTarget() string {
	if it, ok := m.selectedItem(); ok && it.hasJob && it.job.Target != "" {
		return it.job.Target
	}
	if m.hasStatus && len(m.status.Targets) == 1 {
		return m.status.Targets[0]
	}
	return ""
}

// queueCmd calls project_job_queue.
func (m watchModel) queueCmd(target, skill string, numbers []int) tea.Cmd {
	args := map[string]any{"skill": skill, "numbers": numbers}
	if target != "" {
		args["target"] = target
	}
	return func() tea.Msg {
		var out struct {
			Results []watchQueueResult `json:"results"`
		}
		err := m.callJSON("project_job_queue", args, &out)
		return watchQueueMsg{skill: skill, results: out.Results, err: err}
	}
}

// safeMergeSelected queues the selected fix job's open pull request.
func (m watchModel) safeMergeSelected() (tea.Model, tea.Cmd) {
	it, ok := m.selectedItem()
	switch {
	case !ok || !it.hasJob:
		m.errText = "no job selected"
		return m, nil
	case m.status.NoMerge:
		m.message, m.errText = "", "safe merge is off: no_merge leaves pull requests for a human"
		return m, nil
	case it.job.Kind != "issue" || it.job.PullRequest <= 0 || it.job.PullRequestState == "merged":
		m.message, m.errText = "", fmt.Sprintf("%s has no open pull request to safe merge", watchJobKey(it.job))
		return m, nil
	}
	return m, m.queueCmd(it.job.Target, "gh-safe-merge", []int{it.job.PullRequest})
}

// openQueue opens the queue prompt.
func (m watchModel) openQueue() (tea.Model, tea.Cmd) {
	input := textinput.New()
	input.Prompt = "queue: "
	input.Placeholder = "merge 1201 1207 · discuss 4 · fix 12"
	m.queueInput = &input
	return m, m.queueInput.Focus()
}

// updateQueueInput edits the queue prompt.
func (m watchModel) updateQueueInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.queueInput = nil
		m.message = "queue cancelled"
		return m, nil
	case tea.KeyEnter:
		text := m.queueInput.Value()
		m.queueInput = nil
		skill, numbers, err := parseWatchQueue(text)
		if err != nil {
			m.message, m.errText = "", "queue: "+err.Error()
			return m, nil
		}
		return m, m.queueCmd(m.queueTarget(), skill, numbers)
	}
	input, cmd := m.queueInput.Update(msg)
	m.queueInput = &input
	return m, cmd
}

// applyQueue reports what a queue call did.
func (m watchModel) applyQueue(msg watchQueueMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.message, m.errText = "", "queue: "+msg.err.Error()
		return m, nil
	}
	var parts []string
	for _, r := range msg.results {
		switch {
		case r.Queued && r.Position > 0:
			parts = append(parts, fmt.Sprintf("#%d queued (#%d in the merge queue)", r.Number, r.Position))
		case r.Queued:
			parts = append(parts, fmt.Sprintf("#%d queued", r.Number))
		case r.OwnedBy != "":
			parts = append(parts, fmt.Sprintf("#%d already owned by %s", r.Number, r.OwnedBy))
		default:
			parts = append(parts, fmt.Sprintf("#%d: %s", r.Number, r.Reason))
		}
	}
	m.errText = ""
	m.message = msg.skill + ": " + strings.Join(parts, "; ")
	return m.refresh()
}

// watchKindLabel names a queued job's kind in its row, or "" for others.
func watchKindLabel(job watchJob) string {
	switch job.Kind {
	case "merge":
		label := "safe merge"
		if job.QueuePosition > 0 && job.Status == "queued" {
			label += fmt.Sprintf(" #%d in queue", job.QueuePosition)
		}
		return label
	case "discuss":
		return "discuss"
	}
	return ""
}
