package projects

import (
	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/projects/core"

	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestConcurrencySemaphoreResizeGrowAllowsMoreAcquires(t *testing.T) {
	sem := newConcurrencySemaphore(1)
	ctx := context.Background()
	if err := sem.acquire(ctx); err != nil {
		t.Fatal(err)
	}
	acquired := make(chan error, 1)
	go func() { acquired <- sem.acquire(ctx) }()
	select {
	case <-acquired:
		t.Fatal("second acquire succeeded before resize grew capacity")
	case <-time.After(20 * time.Millisecond):
	}
	sem.resize(2)
	select {
	case err := <-acquired:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("second acquire did not unblock after growing capacity")
	}
}

func TestConcurrencySemaphoreResizeShrinkWithholdsReleasedSlot(t *testing.T) {
	sem := newConcurrencySemaphore(2)
	ctx := context.Background()
	if err := sem.acquire(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sem.acquire(ctx); err != nil {
		t.Fatal(err)
	}
	// Shrink to 1 while both slots are held; nothing to withdraw immediately,
	// so the debt should be paid off by the next release instead of the one
	// after that.
	sem.resize(1)
	sem.release()
	sem.release()
	acquired := make(chan error, 1)
	go func() { acquired <- sem.acquire(ctx) }()
	select {
	case err := <-acquired:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("acquire after shrink+two releases should have succeeded once")
	}
	second := make(chan error, 1)
	go func() { second <- sem.acquire(context.Background()) }()
	select {
	case <-second:
		t.Fatal("a second concurrent acquire succeeded after shrinking to 1")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestValidateSettingsUpdateRejectsBadValues(t *testing.T) {
	cases := []struct {
		name   string
		update SettingsUpdate
	}{
		{"concurrency too low", SettingsUpdate{Concurrency: intPtr(0)}},
		{"concurrency too high", SettingsUpdate{Concurrency: intPtr(maxConcurrencyPermits + 1)}},
		{"no active agents", SettingsUpdate{ActiveAgents: agentsPtr()}},
		{"empty agent", SettingsUpdate{ActiveAgents: agentsPtr("   ")}},
		{"unknown agent provider", SettingsUpdate{ActiveAgents: agentsPtr("bogus")}},
		{"bad agent level", SettingsUpdate{ActiveAgents: agentsPtr("codex:extreme")}},
		{"one good one bad agent", SettingsUpdate{ActiveAgents: agentsPtr("codex", "bogus")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateSettingsUpdate(llm.CLIProviders(), tc.update); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestEngineApplySettingsConcurrencyTakesEffectLive(t *testing.T) {
	dir := t.TempDir()
	src := &fakeSource{batches: [][]Issue{{{Number: 1}, {Number: 2}}}}
	r := &fakeRunner{release: make(chan struct{}), dispatched: make(chan int, 2)}
	logs := &syncBuffer{}
	w := &Engine{Repo: "o/r", Interval: time.Millisecond, Concurrency: 1, StatePath: filepath.Join(dir, "state"), Issues: src, Runner: r, Out: logs}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	<-r.dispatched
	select {
	case <-r.dispatched:
		cancel()
		<-done
		t.Fatal("second issue dispatched before concurrency was raised")
	case <-time.After(20 * time.Millisecond):
	}

	snapshot, err := w.ApplySettings(ctx, SettingsUpdate{Concurrency: intPtr(2)})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Concurrency != 2 {
		t.Fatalf("snapshot concurrency = %d, want 2", snapshot.Concurrency)
	}
	select {
	case <-r.dispatched:
	case <-time.After(time.Second):
		cancel()
		<-done
		t.Fatal("second issue was not dispatched after raising concurrency")
	}
	close(r.release)
	cancel()
	<-done
}

func TestEngineApplySettingsReadyStateAndAllowedCommenters(t *testing.T) {
	dir := t.TempDir()
	src := &fakeSource{batches: [][]Issue{{}}}
	r := &fakeRunner{release: make(chan struct{})}
	defer close(r.release)
	logs := &syncBuffer{}
	w := &Engine{Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: filepath.Join(dir, "state"), Issues: src, Runner: r, Out: logs, ReadyState: "Ready"}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	commenters := []string{"alice", "bob"}
	snapshot, err := w.ApplySettings(ctx, SettingsUpdate{ReadyState: strPtr("Agent Ready"), AllowedCommenters: &commenters})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ReadyState != "Agent Ready" {
		t.Fatalf("readyState = %q", snapshot.ReadyState)
	}
	if len(snapshot.AllowedCommenters) != 2 || snapshot.AllowedCommenters[0] != "alice" || snapshot.AllowedCommenters[1] != "bob" {
		t.Fatalf("allowedCommenters = %v", snapshot.AllowedCommenters)
	}

	cancel()
	<-done
}

func TestEngineApplySettingsNoMergeChangesNewDispatches(t *testing.T) {
	dir := t.TempDir()
	src := &fakeSource{batches: [][]Issue{{}}}
	w := &Engine{Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: filepath.Join(dir, "state"), Issues: src, Runner: CommandRunner{Agent: "codex"}, Out: &syncBuffer{}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	noMerge := true
	snapshot, err := w.ApplySettings(ctx, SettingsUpdate{NoMerge: &noMerge})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.NoMerge {
		t.Fatal("snapshot noMerge = false, want true")
	}
	if !w.runner().(CommandRunner).NoMerge {
		t.Fatal("runner noMerge = false, want true")
	}
	cancel()
	<-done
}

func TestEngineApplySettingsAutoUpdate(t *testing.T) {
	dir := t.TempDir()
	src := &fakeSource{batches: [][]Issue{{}}}
	w := &Engine{Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: filepath.Join(dir, "state"), Issues: src, Runner: CommandRunner{Agent: "codex"}, Out: &syncBuffer{}}
	w.autoUpdate.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	snapshot, err := w.ApplySettings(ctx, SettingsUpdate{})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.AutoUpdate {
		t.Fatal("snapshot autoUpdate = false, want the default true")
	}
	off := false
	snapshot, err = w.ApplySettings(ctx, SettingsUpdate{AutoUpdate: &off})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.AutoUpdate || w.autoUpdate.Load() {
		t.Fatalf("after turning off: snapshot=%t live=%t", snapshot.AutoUpdate, w.autoUpdate.Load())
	}
	cancel()
	<-done
}

func TestEngineApplySettingsNoChangelogChangesNewDispatches(t *testing.T) {
	dir := t.TempDir()
	src := &fakeSource{batches: [][]Issue{{}}}
	w := &Engine{Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: filepath.Join(dir, "state"), Issues: src, Runner: CommandRunner{Agent: "codex"}, Out: &syncBuffer{}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	noChangelog := true
	snapshot, err := w.ApplySettings(ctx, SettingsUpdate{NoChangelog: &noChangelog})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.NoChangelog {
		t.Fatal("snapshot noChangelog = false, want true")
	}
	if !w.runner().(CommandRunner).NoChangelog {
		t.Fatal("runner noChangelog = false, want true")
	}
	cancel()
	<-done
}

func TestEngineApplySettingsAllowedCommentersChangesNewDispatches(t *testing.T) {
	dir := t.TempDir()
	src := &fakeSource{batches: [][]Issue{{}}}
	w := &Engine{Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: filepath.Join(dir, "state"), Issues: src, Runner: CommandRunner{Agent: "codex", AllowedCommenters: []string{"alice"}}, Out: &syncBuffer{}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	if got := w.runner().(CommandRunner).AllowedCommenters; !reflect.DeepEqual(got, []string{"alice"}) {
		t.Fatalf("runner allowed commenters before = %v, want [alice]", got)
	}
	allowed := []string{"bob", "carol"}
	if _, err := w.ApplySettings(ctx, SettingsUpdate{AllowedCommenters: &allowed}); err != nil {
		t.Fatal(err)
	}
	if got := w.runner().(CommandRunner).AllowedCommenters; !reflect.DeepEqual(got, allowed) {
		t.Fatalf("runner allowed commenters = %v, want %v", got, allowed)
	}
	cancel()
	<-done
}

func TestEngineApplySettingsReadyStateDefaultReflectsUnsetFallback(t *testing.T) {
	dir := t.TempDir()
	src := &fakeSource{batches: [][]Issue{{}}}
	r := &fakeRunner{release: make(chan struct{})}
	defer close(r.release)
	logs := &syncBuffer{}
	w := &Engine{Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: filepath.Join(dir, "state"), Issues: src, Runner: r, Out: logs}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	snapshot, err := w.CurrentSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ReadyState != "" {
		t.Fatalf("readyState = %q, want unset", snapshot.ReadyState)
	}
	if snapshot.ReadyStateDefault != "Todo" {
		t.Fatalf("readyStateDefault = %q, want %q", snapshot.ReadyStateDefault, "Todo")
	}

	snapshot, err = w.ApplySettings(ctx, SettingsUpdate{ReadyState: strPtr("Agent Ready")})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ReadyStateDefault != "Agent Ready" {
		t.Fatalf("readyStateDefault after configuring = %q, want %q", snapshot.ReadyStateDefault, "Agent Ready")
	}

	cancel()
	<-done
}

// TestEngineApplySettingsFailsFastBeforeRunIsReady checks that a settings
// change made before Run starts fails with core.ErrNotReady, bounded by
// notReadyWait, instead of blocking forever on a channel Run isn't reading
// yet -- the hang reported in issue #579.
func TestEngineApplySettingsFailsFastBeforeRunIsReady(t *testing.T) {
	w := &Engine{notReadyWait: 20 * time.Millisecond}
	start := time.Now()
	_, err := w.ApplySettings(context.Background(), SettingsUpdate{Concurrency: intPtr(2)})
	if !errors.Is(err, core.ErrNotReady) {
		t.Fatalf("ApplySettings error = %v, want core.ErrNotReady", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("ApplySettings took %s, want it bounded by notReadyWait", elapsed)
	}
}

// TestEngineHandleJobActionFailsFastBeforeRunIsReady mirrors
// TestEngineApplySettingsFailsFastBeforeRunIsReady for the job-action path
// .
func TestEngineHandleJobActionFailsFastBeforeRunIsReady(t *testing.T) {
	w := &Engine{notReadyWait: 20 * time.Millisecond}
	start := time.Now()
	err := w.handleJobAction(context.Background(), core.JobAction{Action: "retry", Target: "o/r", Number: 1})
	if !errors.Is(err, core.ErrNotReady) {
		t.Fatalf("handleJobAction error = %v, want core.ErrNotReady", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("handleJobAction took %s, want it bounded by notReadyWait", elapsed)
	}
}

// TestEngineApplySettingsSucceedsOnceRunBecomesReady checks a settings
// request that arrives just before Run reaches readiness still succeeds once
// it gets there, rather than failing merely because it arrived first (issue
// #579).
func TestEngineApplySettingsSucceedsOnceRunBecomesReady(t *testing.T) {
	dir := t.TempDir()
	src := &fakeSource{batches: [][]Issue{{}}}
	r := &fakeRunner{release: make(chan struct{})}
	defer close(r.release)
	logs := &syncBuffer{}
	w := &Engine{Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: filepath.Join(dir, "state"), Issues: src, Runner: r, Out: logs, notReadyWait: 5 * time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	if _, err := w.ApplySettings(ctx, SettingsUpdate{Concurrency: intPtr(2)}); err != nil {
		t.Fatalf("ApplySettings = %v, want success once Run becomes ready", err)
	}

	cancel()
	<-done
}

// TestEngineReadsSettingsBeforeRunStarts checks that reading settings answers
// at once from the configured values even before Run is looping, rather
// than waiting for it the way a change must.
func TestEngineReadsSettingsBeforeRunStarts(t *testing.T) {
	w := &Engine{Concurrency: 3, ReadyState: "Agent Ready", AllowedCommenters: []string{"octocat"}, notReadyWait: 5 * time.Second}
	start := time.Now()
	for _, read := range []func() (SettingsSnapshot, error){
		func() (SettingsSnapshot, error) { return w.CurrentSettings(context.Background()) },
		func() (SettingsSnapshot, error) { return w.ApplySettings(context.Background(), SettingsUpdate{}) },
	} {
		snapshot, err := read()
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Concurrency != 3 || snapshot.ReadyState != "Agent Ready" || len(snapshot.AllowedCommenters) != 1 {
			t.Fatalf("snapshot = %#v, want the configured settings", snapshot)
		}
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("reading settings took %s, want it immediate", elapsed)
	}
}

func TestEngineRunnerAppliesLiveAgentOverride(t *testing.T) {
	w := &Engine{Runner: CommandRunner{Agent: "codex", Agents: []string{"codex"}}}
	if got := w.runner().(CommandRunner).AgentName(); got != "codex" {
		t.Fatalf("agent before override = %q", got)
	}
	override := []string{"claude/opus"}
	w.agentOverride.Store(&override)
	runner, ok := w.runner().(CommandRunner)
	if !ok {
		t.Fatal("runner() did not return a CommandRunner")
	}
	if got := runner.AgentName(); got != "claude/opus" {
		t.Fatalf("agent after override = %q, want claude/opus", got)
	}
}

// TestEngineRunnerAppliesLiveMultiAgentOverride checks a multiselect override
// round robins across every agent it names, not just the first.
func TestEngineRunnerAppliesLiveMultiAgentOverride(t *testing.T) {
	w := &Engine{Runner: CommandRunner{Agent: "codex", Agents: []string{"codex"}}}
	override := []string{"codex", "claude/opus", "muse"}
	w.agentOverride.Store(&override)
	runner, ok := w.runner().(CommandRunner)
	if !ok {
		t.Fatal("runner() did not return a CommandRunner")
	}
	if got := strings.Join(runner.Agents, ","); got != "codex,claude/opus,muse" {
		t.Fatalf("runner.Agents = %q, want every overridden agent", got)
	}
}

func intPtr(v int) *int               { return &v }
func strPtr(v string) *string         { return &v }
func agentsPtr(v ...string) *[]string { return &v }

func TestEngineAgentStillConfigured(t *testing.T) {
	commandRunner := CommandRunner{Agent: "codex", Agents: []string{"codex", "claude/opus:high"}}
	single := CommandRunner{Agent: "claude"}
	cases := []struct {
		name     string
		aviary   *Engine
		override string
		agent    string
		want     bool
	}{
		{name: "listed", aviary: &Engine{Runner: commandRunner}, agent: "codex", want: true},
		{name: "listed with spec", aviary: &Engine{Runner: commandRunner}, agent: "claude/opus:high", want: true},
		{name: "retired", aviary: &Engine{Runner: commandRunner}, agent: "claude", want: false},
		{name: "empty", aviary: &Engine{Runner: commandRunner}, agent: "", want: false},
		{name: "single agent", aviary: &Engine{Runner: single}, agent: "claude", want: true},
		{name: "single agent mismatch", aviary: &Engine{Runner: single}, agent: "codex", want: false},
		{name: "override wins", aviary: &Engine{Runner: commandRunner}, override: "claude", agent: "codex", want: false},
		{name: "override matches", aviary: &Engine{Runner: commandRunner}, override: "claude", agent: "claude", want: true},
		{name: "unconfigured runner", aviary: &Engine{Runner: &fakeSessionRunner{agent: "claude"}}, agent: "codex", want: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if test.override != "" {
				override := []string{test.override}
				test.aviary.agentOverride.Store(&override)
			}
			if got := test.aviary.agentStillConfigured(test.agent); got != test.want {
				t.Fatalf("agentStillConfigured(%q) = %t, want %t", test.agent, got, test.want)
			}
		})
	}
}

// registryForSettings builds a registry holding a built-in agent and one an
// imaginary config file added, so the settings surfaces can be checked to
// report both.
func registryForSettings(t *testing.T) *clidefs.Registry {
	t.Helper()
	definition := func(name string) clidefs.Definition {
		return clidefs.Definition{
			Name: name, Binary: name,
			Args:    clidefs.Args{Run: []clidefs.Fragment{{Args: []string{"{prompt}"}}}, Resume: []clidefs.Fragment{{Args: []string{"{prompt}"}}}},
			Session: clidefs.Session{Assign: clidefs.AssignNone},
			Output:  clidefs.Output{Format: clidefs.FormatText},
		}
	}
	codex := definition("codex")
	codex.Levels = clidefs.NewAllowList("low", "medium", "high")
	muse := definition("muse")
	muse.Models = clidefs.NewAllowList("muse-1", "muse-2")
	registry, err := clidefs.NewRegistry(codex, muse)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

// TestSettingsSnapshotListsEveryRegisteredAgent checks the settings API
// reports the registry rather than the agents this run happens to dispatch
// with, which is what makes a .aviary.config.json agent selectable in the
// dashboard with no code change.
func TestSettingsSnapshotListsEveryRegisteredAgent(t *testing.T) {
	w := &Engine{
		Registry: registryForSettings(t),
		Runner:   CommandRunner{Agent: "codex", Agents: []string{"codex"}},
	}
	snapshot := w.settingsSnapshot()
	if got := strings.Join(snapshot.Agents, ","); got != "codex,muse" {
		t.Fatalf("agents = %q, want every registered agent", got)
	}
	if got := strings.Join(snapshot.ConfiguredAgents, ","); got != "codex" {
		t.Fatalf("configuredAgents = %q, want only what --agent selected", got)
	}
	if len(snapshot.AgentOptions) != 2 {
		t.Fatalf("agentOptions = %v, want one per registered agent", snapshot.AgentOptions)
	}
	if got := strings.Join(snapshot.AgentOptions[0].Levels, ","); got != "low,medium,high" {
		t.Fatalf("codex levels = %q", got)
	}
	if got := strings.Join(snapshot.AgentOptions[1].Models, ","); got != "muse-1,muse-2" {
		t.Fatalf("muse models = %q", got)
	}
}

// TestSettingsValidatesAgainstTheRunsRegistry checks the live --agent override
// accepts a config-defined agent and rejects an unknown one with the list the
// CLI would print, so the dashboard and the settings API answer a typo the
// same way the command line does.
func TestSettingsValidatesAgainstTheRunsRegistry(t *testing.T) {
	registry := registryForSettings(t)
	if err := validateSettingsUpdate(registry, SettingsUpdate{ActiveAgents: agentsPtr("muse/muse-2")}); err != nil {
		t.Fatalf("config-defined agent rejected: %v", err)
	}
	err := validateSettingsUpdate(registry, SettingsUpdate{ActiveAgents: agentsPtr("claude")})
	if err == nil {
		t.Fatal("expected an agent outside the run's registry to be rejected")
	}
	if !strings.Contains(err.Error(), "known agents are codex, muse") {
		t.Fatalf("error = %v, want it to list the registry's agents", err)
	}
	if err := validateSettingsUpdate(registry, SettingsUpdate{ActiveAgents: agentsPtr("muse/muse-9")}); err == nil {
		t.Fatal("expected a model outside the agent's list to be rejected")
	}
}

// TestAgentStillConfiguredIgnoresTheRegistry checks the resume path keeps
// asking what --agent selected rather than what the registry defines (issue
// #489). An agent that is still registered but is no longer dispatched to must
// not hold a persisted session, and an agent a config file dropped entirely
// must not either.
func TestAgentStillConfiguredIgnoresTheRegistry(t *testing.T) {
	w := &Engine{
		Registry: registryForSettings(t),
		Runner:   CommandRunner{Agent: "codex", Agents: []string{"codex"}},
	}
	if w.agentStillConfigured("muse") {
		t.Fatal("a registered agent absent from --agent should not keep a session")
	}
	if !w.agentStillConfigured("codex") {
		t.Fatal("the configured agent should keep its session")
	}
	// A config-defined agent that disappeared from config is gone from the
	// registry too, and is likewise not configured.
	dropped := &Engine{Runner: CommandRunner{Agent: "codex", Agents: []string{"codex"}}}
	if dropped.agentStillConfigured("muse") {
		t.Fatal("an agent no longer in config should not keep a session")
	}
	// It is still dispatchable while --agent names it, registry or not: the
	// registry rejects it at parse time, not here.
	kept := &Engine{Runner: CommandRunner{Agent: "muse", Agents: []string{"muse"}}}
	if !kept.agentStillConfigured("muse") {
		t.Fatal("an agent --agent still names should keep its session")
	}
}
