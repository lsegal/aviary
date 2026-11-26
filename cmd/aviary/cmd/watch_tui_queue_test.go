package cmd

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestParseWatchQueue(t *testing.T) {
	skill, numbers, err := parseWatchQueue("merge 1201, #1207")
	if err != nil || skill != "gh-safe-merge" || !slices.Equal(numbers, []int{1201, 1207}) {
		t.Fatalf("parse = %q %v %v", skill, numbers, err)
	}
	if skill, _, _ := parseWatchQueue("d 4"); skill != "gh-discuss" {
		t.Errorf("d selects %q", skill)
	}
	for _, bad := range []string{"", "merge", "rebase 3", "merge x"} {
		if _, _, err := parseWatchQueue(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

// selectWatchJob moves the cursor to the job keyed key in the list.
func selectWatchJob(t *testing.T, m watchModel, key string) watchModel {
	t.Helper()
	for i, it := range m.visibleItems() {
		if it.key == key {
			m.cursor = i
			m.remember()
			return m
		}
	}
	t.Fatalf("no item %s", key)
	return m
}

func TestWatchTUISafeMergesSelectedPullRequest(t *testing.T) {
	tools := newFakeWatchTools()
	tools.responses["project_job_queue"] = map[string]any{"results": []map[string]any{{"number": 9, "queued": true, "position": 1}}}
	m := selectWatchJob(t, loadedWatchModel(t, tools), "o/r#4")
	m, msg := watchKey(t, m, "m")
	calls := tools.callsTo("project_job_queue")
	if len(calls) != 1 || calls[0].args["skill"] != "gh-safe-merge" || calls[0].args["target"] != "o/r" || !slices.Equal(calls[0].args["numbers"].([]int), []int{9}) {
		t.Fatalf("calls = %+v", calls)
	}
	m = watchUpdate(t, m, msg)
	if !strings.Contains(m.message, "#9 queued (priority #1)") {
		t.Fatalf("message = %q", m.message)
	}

	// A job with no pull request has nothing to merge.
	m = selectWatchJob(t, m, "o/r#3")
	m, _ = watchKey(t, m, "m")
	if !strings.Contains(m.errText, "no open pull request") || len(tools.callsTo("project_job_queue")) != 1 {
		t.Fatalf("errText = %q", m.errText)
	}
}

func TestWatchTUISafeMergeRespectsNoMerge(t *testing.T) {
	tools := newFakeWatchTools()
	tools.responses["project_status"].(map[string]any)["no_merge"] = true
	m := selectWatchJob(t, loadedWatchModel(t, tools), "o/r#4")
	m, _ = watchKey(t, m, "m")
	if !strings.Contains(m.errText, "no_merge") || len(tools.callsTo("project_job_queue")) != 0 {
		t.Fatalf("errText = %q, calls %d", m.errText, len(tools.callsTo("project_job_queue")))
	}
}

func TestWatchTUIQueuePrompt(t *testing.T) {
	tools := newFakeWatchTools()
	tools.responses["project_job_queue"] = map[string]any{"results": []map[string]any{{"number": 7, "queued": false, "owned_by": "o/r#3"}}}
	m := loadedWatchModel(t, tools)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Q")})
	m = next.(watchModel)
	if m.queueInput == nil {
		t.Fatal("Q did not open the queue prompt")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("discuss 7")})
	m = next.(watchModel)
	if !strings.Contains(watchStrip(m.footerLine()), "queue: discuss 7") {
		t.Fatalf("footer = %q", watchStrip(m.footerLine()))
	}
	m, msg := watchKey(t, m, "enter")
	if m.queueInput != nil {
		t.Fatal("enter left the prompt open")
	}
	calls := tools.callsTo("project_job_queue")
	if len(calls) != 1 || calls[0].args["skill"] != "gh-discuss" {
		t.Fatalf("calls = %+v", calls)
	}
	m = watchUpdate(t, m, msg)
	if !strings.Contains(m.message, "#7 already owned by o/r#3") {
		t.Fatalf("message = %q", m.message)
	}
}

func TestWatchTUIShowsQueuedJobKinds(t *testing.T) {
	tools := newFakeWatchTools()
	list := tools.responses["project_job_list"].(map[string]any)
	list["jobs"] = append(list["jobs"].([]map[string]any),
		map[string]any{"key": "o/r#1207", "target": "o/r", "number": 1207, "title": "Bump deps", "status": "queued", "state": "starting", "kind": "merge", "queue_position": 2},
		map[string]any{"key": "o/r#1208", "target": "o/r", "number": 1208, "title": "Docs", "status": "failed", "state": "failed", "kind": "merge", "summary": "Skipped: required checks are failing"},
	)
	m := loadedWatchModel(t, tools)
	view := watchStrip(m.View())
	for _, want := range []string{"safe merge queued, priority #2", "Skipped: required checks are failing"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
}
