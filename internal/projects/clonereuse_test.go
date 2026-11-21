package projects

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

// reuseFixture is a registry holding one recorded clone of o/r, made for job
// o/r#1, under a sanctioned root.
func reuseFixture(t *testing.T) (*cloneRegistry, string, cloneUsage) {
	t.Helper()
	root := t.TempDir()
	path := makeClone(t, root, "r-gh-fix-1", "o/r")
	reg, err := loadCloneRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.record(cloneRecord{Path: path, Repository: "o/r", Issue: 1, Job: "o/r#1"}); err != nil {
		t.Fatal(err)
	}
	usage := cloneUsage{roots: []string{root}, work: map[string]workState{"o/r#1": {Status: "done", CheckoutDirectory: path}}}
	return reg, path, usage
}

func TestPickReusableCloneReusesAFreeClone(t *testing.T) {
	reg, path, usage := reuseFixture(t)
	rec, ok := pickReusableClone(reg, "O/R", usage)
	if !ok || rec.Path != path {
		t.Fatalf("pickReusableClone = %#v, %v; want the finished job's clone %s", rec, ok, path)
	}
	if _, ok := pickReusableClone(reg, "o/other", usage); ok {
		t.Fatal("a clone of another repository was handed out")
	}
}

func TestPickReusableCloneSkipsBusyClones(t *testing.T) {
	cases := map[string]func(*cloneUsage, string){
		"job active": func(u *cloneUsage, _ string) {
			u.work["o/r#1"] = workState{Status: "active", CheckoutDirectory: u.work["o/r#1"].CheckoutDirectory}
		},
		"job paused":   func(u *cloneUsage, _ string) { u.work["o/r#1"] = workState{Status: "paused"} },
		"agent in it":  func(u *cloneUsage, path string) { u.live = []checkpointRun{{Number: 9, Checkout: path}} },
		"leased":       func(u *cloneUsage, path string) { u.leased = map[string]string{cloneKey(path): "o/r#2"} },
		"state broken": func(u *cloneUsage, _ string) { u.workErr = os.ErrPermission },
	}
	for name, busy := range cases {
		t.Run(name, func(t *testing.T) {
			reg, path, usage := reuseFixture(t)
			busy(&usage, path)
			if rec, ok := pickReusableClone(reg, "o/r", usage); ok {
				t.Fatalf("busy clone handed out: %#v", rec)
			}
			// project_clone_list must agree that the clone is in use.
			if usage.workErr == nil {
				snapshot, err := listClones(reg, usage)
				if err != nil || len(snapshot.Clones) != 1 || !snapshot.Clones[0].InUse {
					t.Fatalf("listClones = %#v, %v; want the clone in use", snapshot, err)
				}
			}
		})
	}
}

func TestAdoptKeepsAReusedClonesNewJob(t *testing.T) {
	reg, path, usage := reuseFixture(t)
	if err := reg.remove(path); err != nil {
		t.Fatal(err)
	}
	if err := reg.record(cloneRecord{Path: path, Repository: "o/r", Issue: 2, Job: "o/r#2"}); err != nil {
		t.Fatal(err)
	}
	reg.adopt(usage.roots, usage.work)
	if rec, _ := reg.get(path); rec.Job != "o/r#2" || rec.Issue != 2 {
		t.Fatalf("adopt rewrote the reused clone's record to %#v", rec)
	}
}

func TestPrepareReusedCloneLeavesNothingFromThePreviousJob(t *testing.T) {
	root := t.TempDir()
	origin := filepath.Join(root, "origin")
	if err := os.Mkdir(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, origin, "init", "-q", "-b", "main")
	writeCheckpointFile(t, filepath.Join(origin, ".gitignore"), "*.log\nnode_modules/\n")
	writeCheckpointFile(t, filepath.Join(origin, "README"), "one\n")
	gitIn(t, origin, "add", ".")
	gitIn(t, origin, "commit", "-q", "-m", "one")
	clone := filepath.Join(root, "r-gh-fix-1")
	gitIn(t, root, "clone", "-q", origin, clone)

	// The previous job's leftovers.
	gitIn(t, clone, "checkout", "-q", "-b", "fix/issue-1-old")
	writeCheckpointFile(t, filepath.Join(clone, "README"), "edited\n")
	gitIn(t, clone, "commit", "-q", "-am", "old work")
	writeCheckpointFile(t, filepath.Join(clone, "README"), "dirty\n")
	writeCheckpointFile(t, filepath.Join(clone, "stray.txt"), "untracked\n")
	writeCheckpointFile(t, filepath.Join(clone, "debug.log"), "ignored\n")
	if err := os.MkdirAll(filepath.Join(clone, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeCheckpointFile(t, filepath.Join(clone, "node_modules", "pkg", "index.js"), "dep\n")
	writeCheckpointFile(t, filepath.Join(clone, ".git", "hooks", "pre-commit"), "#!/bin/sh\nexit 1\n")
	gitIn(t, clone, "config", "user.name", "previous job")
	gitIn(t, clone, "remote", "add", "fork", "https://github.com/someone/r.git")
	gitIn(t, clone, "stash", "-q", "--include-untracked")
	writeCheckpointFile(t, filepath.Join(clone, "stray.txt"), "untracked again\n")

	// main moves on after the previous job.
	writeCheckpointFile(t, filepath.Join(origin, "README"), "two\n")
	gitIn(t, origin, "commit", "-q", "-am", "two")
	tip := gitIn(t, origin, "rev-parse", "HEAD")

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := prepareReusedClone(ctx, runCheckpointGit, clone); err != nil {
		t.Fatal(err)
	}

	if got := gitIn(t, clone, "rev-parse", "--abbrev-ref", "HEAD"); got != "main" {
		t.Fatalf("branch = %q, want a fresh main", got)
	}
	if got := gitIn(t, clone, "rev-parse", "HEAD"); got != tip {
		t.Fatalf("HEAD = %s, want the latest origin/main %s", got, tip)
	}
	if got := gitIn(t, clone, "rev-parse", "--abbrev-ref", "main@{upstream}"); got != "origin/main" {
		t.Fatalf("main tracks %q, want origin/main", got)
	}
	if got := gitIn(t, clone, "for-each-ref", "--format=%(refname:short)", "refs/heads/"); got != "main" {
		t.Fatalf("local branches = %q, want only main", got)
	}
	if got := gitIn(t, clone, "status", "--porcelain", "--ignored"); got != "!! node_modules/" {
		t.Fatalf("status = %q, want a clean tree keeping only node_modules", got)
	}
	if got := gitIn(t, clone, "stash", "list"); got != "" {
		t.Fatalf("stashes = %q, want none", got)
	}
	if got := gitIn(t, clone, "remote"); got != "origin" {
		t.Fatalf("remotes = %q, want only origin", got)
	}
	config, err := os.ReadFile(filepath.Join(clone, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config), "previous job") || strings.Contains(string(config), "fix/issue-1-old") {
		t.Fatalf("local config kept the previous job's settings:\n%s", config)
	}
	if _, err := os.Stat(filepath.Join(clone, ".git", "hooks", "pre-commit")); !os.IsNotExist(err) {
		t.Fatalf("the previous job's hook survived: %v", err)
	}
	if gitOriginRepository(clone) != "" {
		t.Fatal("a local origin read as a GitHub repository")
	}
}

func TestCheckoutStateForJobDetail(t *testing.T) {
	present := t.TempDir()
	removed := filepath.Join(t.TempDir(), "gone")
	now := time.Now()
	older := core.JobSnapshot{Target: "o/r", Number: 1, CheckoutDirectory: present, Started: now.Add(-time.Hour)}
	newer := core.JobSnapshot{Target: "o/r", Number: 2, CheckoutDirectory: present, Started: now}
	jobs := []core.JobSnapshot{older, newer}
	cases := []struct {
		name string
		job  core.JobSnapshot
		want string
	}{
		{"no clone yet", core.JobSnapshot{Target: "o/r", Number: 3}, "none"},
		{"present", newer, "present"},
		{"reused by a later job", older, "reused"},
		{"removed", core.JobSnapshot{Target: "o/r", Number: 4, CheckoutDirectory: removed}, "removed"},
	}
	for _, tc := range cases {
		state, holder := checkoutState(tc.job, jobs)
		if state != tc.want {
			t.Errorf("%s: state = %q, want %q", tc.name, state, tc.want)
		}
		if (state == "reused") != (holder != nil) || (holder != nil && holder.Number != 2) {
			t.Errorf("%s: holder = %#v", tc.name, holder)
		}
	}
}

func TestEngineHandsAFinishedJobsCloneToTheNextJob(t *testing.T) {
	dir := t.TempDir()
	root := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	clone := makeClone(t, root, "r-gh-fix-5", "o/r")
	reg, err := loadCloneRegistry(clonesPathFor(statePath))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.record(cloneRecord{Path: clone, Repository: "o/r", Issue: 5, Job: "o/r#5", PullRequest: 50}); err != nil {
		t.Fatal(err)
	}
	if err := saveWorkState(statePath, map[int]workState{5: {Status: "done", CheckoutDirectory: clone}}); err != nil {
		t.Fatal(err)
	}
	var prepared []string
	git := func(_ context.Context, dir string, args ...string) (string, error) {
		prepared = append(prepared, dir)
		switch strings.Join(args, " ") {
		case "config --local --get remote.origin.url":
			return "https://github.com/o/r.git", nil
		case "for-each-ref --format=%(refname:short) refs/heads/":
			return "main", nil
		}
		return "", nil
	}
	runner := &fakeSessionRunner{agent: "codex", sessions: make(chan AgentSession, 1)}
	w := &Engine{
		Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: statePath,
		Issues: &fakeSource{batches: [][]Issue{{{Number: 7, Target: "o/r"}}}}, Runner: runner,
		checkpointGit: git,
	}
	w.SetTempDirs([]string{root})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()

	select {
	case session := <-runner.sessions:
		if !session.ReusedClone || session.CheckoutDirectory != clone || session.Resume {
			t.Fatalf("session = %#v, want a fresh run handed %s", session, clone)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the job was not started")
	}
	if len(prepared) == 0 || prepared[0] != clone {
		t.Fatalf("git ran in %v, want the reused clone prepared first", prepared)
	}
	state, err := loadWorkState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if state[7].CheckoutDirectory != clone || state[5].CheckoutDirectory != "" {
		t.Fatalf("work state = %#v, want the clone moved from #5 to #7", state)
	}
	snapshot, err := w.ListClones(context.Background())
	if err != nil || len(snapshot.Clones) != 1 {
		t.Fatalf("ListClones = %#v, %v", snapshot, err)
	}
	if entry := snapshot.Clones[0]; !entry.InUse || entry.Issue != 7 || entry.PullRequest != 0 {
		t.Fatalf("clone entry = %#v, want in use by #7 with #5's pull request dropped", entry)
	}
}

func TestReusedCloneRunNamesTheClone(t *testing.T) {
	clone := t.TempDir()
	runner := CommandRunner{Agent: "claude", Repo: "o/r", CloneRoot: func() (string, error) { return t.TempDir(), nil }}
	session, err := runner.resolveRunDir(AgentSession{Agent: "claude", CheckoutDirectory: clone, ReusedClone: true})
	if err != nil {
		t.Fatal(err)
	}
	args := commandArgsForSession(runner, Issue{Number: 7, Target: "o/r"}, session)
	prompt := args[len(args)-1]
	if !strings.HasSuffix(prompt, "Reuse clone: "+clone) || strings.Contains(prompt, "Clone directory:") {
		t.Fatalf("prompt does not hand over the reused clone: %q", prompt)
	}
}
