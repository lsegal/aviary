package core

import "testing"

func TestChecklistProgress(t *testing.T) {
	tests := []struct {
		name              string
		body              string
		done, total, want int
		has               bool
	}{
		{name: "no checklist", body: "Fixes the thing.\n\nCloses #7", done: 0, total: 0},
		{name: "empty body", body: "", done: 0, total: 0},
		{name: "partial", body: "## Plan\n- [x] parse\n- [X] render\n* [ ] test\n+ [ ] docs\n1. [ ] ship\n", done: 2, total: 5, want: 40, has: true},
		{name: "complete", body: "- [x] one\r\n- [x] two\r\n", done: 2, total: 2, want: 100, has: true},
		{name: "plan grew", body: "- [x] one\n- [x] two\n- [ ] three\n- [ ] four\n", done: 2, total: 4, want: 50, has: true},
		{name: "rounds down", body: "- [x] one\n- [x] two\n- [ ] three\n", done: 2, total: 3, want: 66, has: true},
		{name: "indented sub-item", body: "- [x] one\n  - [ ] one a\n", done: 1, total: 2, want: 50, has: true},
		{name: "ignores code fences and non-items", body: "```md\n- [x] example\n```\n~~~\n- [ ] example\n~~~\n- [] not an item\n-[x] not an item\n[x] not an item\n- [ ]\n", done: 0, total: 1, want: 0, has: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			done, total := ChecklistProgress(tt.body)
			if done != tt.done || total != tt.total {
				t.Fatalf("ChecklistProgress() = %d/%d, want %d/%d", done, total, tt.done, tt.total)
			}
			percent, has := ProgressPercent(done, total)
			if percent != tt.want || has != tt.has {
				t.Fatalf("ProgressPercent(%d, %d) = %d, %v; want %d, %v", done, total, percent, has, tt.want, tt.has)
			}
		})
	}
}

func TestJobSnapshotProgressFollowsPullRequestChecklist(t *testing.T) {
	open := func(done, total int) OriginatingWorkState {
		return OriginatingWorkState{PullRequests: []PullRequestWorkState{{Number: 9, State: "open", TasksDone: done, TasksTotal: total}}}
	}
	var job JobSnapshot
	if job.SetPullRequest(open(0, 0), "o/r"); job.HasProgress() {
		t.Fatalf("pull request without a checklist shows progress %d%%", job.Progress)
	}
	if !job.SetPullRequest(open(2, 5), "o/r") || !job.HasProgress() || job.Progress != 40 {
		t.Fatalf("partial checklist progress = %d%% (has %v), want 40%%", job.Progress, job.HasProgress())
	}
	if job.SetPullRequest(open(2, 5), "o/r") {
		t.Fatal("re-reading the same checklist reported a change")
	}
	// The plan grew: the percentage goes down, with nothing clamping it.
	if !job.SetPullRequest(open(2, 8), "o/r") || job.Progress != 25 {
		t.Fatalf("grown plan progress = %d%%, want 25%%", job.Progress)
	}
	merged := OriginatingWorkState{PullRequests: []PullRequestWorkState{{Number: 9, State: "closed", Merged: true, TasksDone: 7, TasksTotal: 8}}}
	if job.SetPullRequest(merged, "o/r"); job.Progress != 100 {
		t.Fatalf("merged pull request progress = %d%%, want 100%%", job.Progress)
	}
	if job.SetPullRequest(OriginatingWorkState{}, "o/r"); job.HasProgress() || job.Progress != 0 {
		t.Fatalf("job whose pull request went away shows progress %d%%", job.Progress)
	}
}

func TestJobSnapshotFinish(t *testing.T) {
	job := JobSnapshot{}
	job.SetPullRequest(OriginatingWorkState{PullRequests: []PullRequestWorkState{{Number: 9, State: "open", TasksDone: 3, TasksTotal: 4}}}, "o/r")
	job.Finish()
	if job.Progress != 100 || !job.HasProgress() {
		t.Fatalf("finished job progress = %d%%, want 100%%", job.Progress)
	}
	var none JobSnapshot
	if none.Finish(); none.HasProgress() {
		t.Fatal("finished job without a checklist shows progress")
	}
}
