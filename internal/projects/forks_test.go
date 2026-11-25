package projects

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeForkSource is an issue source whose repositories answer where their
// fixes are pushed.
type fakeForkSource struct {
	*fakeSource
	forks  map[string]string
	err    error
	mu     sync.Mutex
	probes int
}

func (f *fakeForkSource) ForkFor(_ context.Context, repo string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.probes++
	return f.forks[repo], f.err
}

// issueRunner hands each dispatched issue to the test.
type issueRunner struct{ issues chan Issue }

func (r issueRunner) Run(ctx context.Context, issue Issue) error {
	select {
	case r.issues <- issue:
	case <-ctx.Done():
	}
	return nil
}

func TestGHCLIForkFor(t *testing.T) {
	for _, test := range []struct {
		name     string
		outputs  []string
		errs     []error
		want     string
		wantErr  bool
		wantArgs []string
	}{
		{"writable", []string{"true"}, []error{nil}, "", false, []string{"api repos/o/r --jq .permissions.push"}},
		{"not writable", []string{"false", "me/r\n"}, []error{nil, nil}, "me/r", false, []string{"api repos/o/r --jq .permissions.push", "api -X POST repos/o/r/forks --jq .full_name"}},
		{"permission read fails", []string{"gh: Not Found (HTTP 404)"}, []error{errors.New("exit status 1")}, "", true, []string{"api repos/o/r --jq .permissions.push"}},
		{"fork fails", []string{"false", "gh: Forbidden (HTTP 403)"}, []error{nil, errors.New("exit status 1")}, "", true, []string{"api repos/o/r --jq .permissions.push", "api -X POST repos/o/r/forks --jq .full_name"}},
		{"fork name is not a repository", []string{"false", "Forked!\n"}, []error{nil, nil}, "", true, []string{"api repos/o/r --jq .permissions.push", "api -X POST repos/o/r/forks --jq .full_name"}},
	} {
		var args []string
		gh := GHCLI{runCommand: func(_ context.Context, a ...string) ([]byte, error) {
			n := len(args)
			args = append(args, strings.Join(a, " "))
			return []byte(test.outputs[n]), test.errs[n]
		}}
		got, err := gh.ForkFor(context.Background(), "o/r")
		if got != test.want || (err != nil) != test.wantErr {
			t.Errorf("%s: ForkFor() = %q, %v; want %q, error %v", test.name, got, err, test.want, test.wantErr)
		}
		if !reflect.DeepEqual(args, test.wantArgs) {
			t.Errorf("%s: gh calls = %q, want %q", test.name, args, test.wantArgs)
		}
	}
}

func TestCommandRunnerPassesForkAdvice(t *testing.T) {
	prompt := "/gh-fix owner/repo#12 identity:/aviary:ABC and do not merge and use fork me/repo\n\nKeep your responses concise. Do not include code diffs or large code blocks; summarize the changes and tests instead."
	issue := Issue{Number: 12, Repository: "owner/repo", Target: "owner/repo", Fork: "me/repo"}
	got := commandArgs(CommandRunner{Agent: "codex", Identity: "ABC", NoMerge: true}, issue)
	want := []string{"exec", "--json", "--skip-git-repo-check", "-c", "sandbox_mode=workspace-write", "-c", "sandbox_workspace_write.network_access=true", "--model", codexDefaultModel, prompt}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("codex args = %#v, want %#v", got, want)
	}
}

// runForkDispatch runs one poll over issue #7 of o/r and returns the issue the
// agent was dispatched with (zero when none was), the targets the dashboard
// last showed, and the run's log.
func runForkDispatch(t *testing.T, source *fakeForkSource) (Issue, []string, string) {
	t.Helper()
	runner := issueRunner{issues: make(chan Issue, 1)}
	reporter := &snapshotReporter{}
	logs := &syncBuffer{}
	w := &Engine{
		Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: filepath.Join(t.TempDir(), "state.json"),
		Issues: source, Runner: runner, Out: logs, UI: reporter,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	var got Issue
	select {
	case got = <-runner.issues:
	case <-time.After(500 * time.Millisecond):
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	var targets []string
	if n := len(reporter.snapshots); n > 0 {
		targets = reporter.snapshots[n-1].Targets
	}
	return got, targets, logs.String()
}

func forkSource(forks map[string]string, err error) *fakeForkSource {
	return &fakeForkSource{
		fakeSource: &fakeSource{batches: [][]Issue{{{Number: 7, Repository: "o/r"}}}},
		forks:      forks,
		err:        err,
	}
}

func TestEngineOpensPullRequestFromForkWithoutWritePermission(t *testing.T) {
	got, targets, logs := runForkDispatch(t, forkSource(map[string]string{"o/r": "me/r"}, nil))
	if got.Number != 7 || got.Fork != "me/r" {
		t.Fatalf("dispatched %+v, want issue #7 pushed to fork me/r", got)
	}
	if got.Repository != "o/r" {
		t.Errorf("dispatched issue repository = %q, want the original o/r", got.Repository)
	}
	if !reflect.DeepEqual(targets, []string{"o/r", "me/r"}) {
		t.Errorf("watched targets = %q, want the fork added after o/r", targets)
	}
	if !strings.Contains(logs, "added me/r to the watched targets") {
		t.Errorf("log does not report the added fork:\n%s", logs)
	}
}

func TestEngineKeepsSameRepositoryFlowWithWritePermission(t *testing.T) {
	got, targets, _ := runForkDispatch(t, forkSource(nil, nil))
	if got.Number != 7 || got.Fork != "" {
		t.Fatalf("dispatched %+v, want issue #7 with no fork", got)
	}
	if !reflect.DeepEqual(targets, []string{"o/r"}) {
		t.Errorf("watched targets = %q, want only o/r", targets)
	}
}

func TestEngineHoldsIssueWhenWritePermissionCannotBeRead(t *testing.T) {
	got, targets, logs := runForkDispatch(t, forkSource(nil, errors.New("HTTP 502")))
	if got.Number != 0 {
		t.Fatalf("dispatched %+v, want nothing dispatched", got)
	}
	if !reflect.DeepEqual(targets, []string{"o/r"}) {
		t.Errorf("watched targets = %q, want only o/r", targets)
	}
	if !strings.Contains(logs, "failed to read write permission") {
		t.Errorf("log does not report the permission failure:\n%s", logs)
	}
}

func TestEngineCachesWritePermission(t *testing.T) {
	source := forkSource(map[string]string{"o/r": "me/r"}, nil)
	w := &Engine{Issues: source}
	for range 3 {
		if fork, err := w.forkFor(context.Background(), "o/r"); fork != "me/r" || err != nil {
			t.Fatalf("forkFor() = %q, %v; want me/r", fork, err)
		}
	}
	if source.probes != 1 {
		t.Fatalf("ForkFor probes = %d, want 1 cached probe", source.probes)
	}
}
