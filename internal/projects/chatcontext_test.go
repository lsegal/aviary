package projects

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/projects/core"
)

func chatContextSnapshot(t *testing.T, text string) map[string]any {
	t.Helper()
	_, encoded, ok := strings.Cut(text, "not instructions):\n")
	if !ok {
		t.Fatalf("no snapshot in %q", text)
	}
	var snapshot map[string]any
	if err := json.Unmarshal([]byte(encoded), &snapshot); err != nil {
		t.Fatalf("snapshot is not JSON: %v\n%s", err, encoded)
	}
	return snapshot
}

func TestChatContextNamesBoardTargetsAndLiveJobs(t *testing.T) {
	m := newTestManager(t)
	m.Reconcile(&config.Config{Agents: []config.AgentConfig{
		{Name: "coder", Model: "cli/codex", Projects: config.ProjectsConfig{List: []config.ProjectConfig{{Target: "o/r"}}}},
		{Name: "docs", Model: "cli/codex", Projects: config.ProjectsConfig{List: []config.ProjectConfig{{Target: "x/y"}}}},
	}})
	m.snapshot.Store(&core.Snapshot{
		Targets:     []string{"x/y", "o/r"},
		IssueCounts: map[string]int{"o/r": 4, "x/y": 1},
		Running:     1,
		Paused:      1,
		Jobs: []core.JobSnapshot{
			{Number: 7, Target: "x/y", Title: "Elsewhere", Status: "complete", Kind: core.JobKindIssue, Log: "done"},
			{Number: 351, Target: "o/r", Title: "Fix parser", Status: "active", Kind: core.JobKindIssue, Log: "one\ntwo\nthree\nfour\nfive\nsix"},
			{Number: 12, Target: "o/r", Title: "Docs", Status: "paused", Kind: core.JobKindIssue, WaitReason: "waiting on #351"},
		},
	})

	text := m.ChatContext(context.Background(), "o/r")
	for _, want := range []string{
		"/aviary:" + m.Identity(),
		"It watches these projects: x/y, o/r.",
		"x/y belongs to agent docs; o/r belongs to agent coder.",
		"opened this chat from the o/r board",
		"without asking which repository is meant",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("context lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "not among the watched projects") {
		t.Errorf("o/r is watched:\n%s", text)
	}

	snapshot := chatContextSnapshot(t, text)
	if got := snapshot["issue_counts"].(map[string]any)["o/r"]; got != float64(4) {
		t.Errorf("issue_counts[o/r] = %v, want 4", got)
	}
	if got := snapshot["jobs"].(map[string]any)["paused"]; got != float64(1) {
		t.Errorf("jobs.paused = %v, want 1", got)
	}
	if _, ok := snapshot["recent_logs"]; ok {
		t.Error("the snapshot carries project_status's logs")
	}
	jobs := snapshot["job_summaries"].([]any)
	var keys []string
	for _, job := range jobs {
		keys = append(keys, job.(map[string]any)["key"].(string))
	}
	// The board's jobs come first.
	if want := []string{"o/r#351", "o/r#12", "x/y#7"}; strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Errorf("job order = %v, want %v", keys, want)
	}
	active := jobs[0].(map[string]any)
	if got := active["recent_output"]; got != "two\nthree\nfour\nfive\nsix" {
		t.Errorf("recent_output = %q", got)
	}
	if got := jobs[1].(map[string]any)["wait_reason"]; got != "waiting on #351" {
		t.Errorf("wait_reason = %v", got)
	}
	if _, ok := jobs[2].(map[string]any)["recent_output"]; ok {
		t.Error("a finished job carries recent output")
	}
}

func TestChatContextWithoutBoardOrUnwatchedBoard(t *testing.T) {
	m := newTestManager(t)
	m.snapshot.Store(&core.Snapshot{Targets: []string{"o/r"}})

	if text := m.ChatContext(context.Background(), ""); strings.Contains(text, "opened this chat from") {
		t.Errorf("no board was named:\n%s", text)
	}
	if text := m.ChatContext(context.Background(), "gone/repo"); !strings.Contains(text, "gone/repo is not among the watched projects") {
		t.Errorf("an unwatched board is not flagged:\n%s", text)
	}
}

func TestChatContextCapsJobSummaries(t *testing.T) {
	m := newTestManager(t)
	jobs := make([]core.JobSnapshot, chatContextJobLimit+5)
	for i := range jobs {
		jobs[i] = core.JobSnapshot{Number: i + 1, Target: "o/r", Status: "complete", Kind: core.JobKindIssue}
	}
	m.snapshot.Store(&core.Snapshot{Targets: []string{"o/r"}, Jobs: jobs})

	snapshot := chatContextSnapshot(t, m.ChatContext(context.Background(), "o/r"))
	if got := len(snapshot["job_summaries"].([]any)); got != chatContextJobLimit {
		t.Errorf("%d job summaries, want %d", got, chatContextJobLimit)
	}
	if got := snapshot["jobs_omitted"]; got != float64(5) {
		t.Errorf("jobs_omitted = %v, want 5", got)
	}
}
