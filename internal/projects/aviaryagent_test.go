package projects

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/transcript"
)

// #435: an aviary/<agent> job runs its turn through the injected core runner
// in the job's directory instead of spawning `aviary agent run`, and records
// the turn's events and the checkout it announces.
func TestAviaryAgentJobRunsInProcess(t *testing.T) {
	runDir, checkout := t.TempDir(), t.TempDir()
	var got AviaryAgentRun
	store := newTranscriptStore("")
	runner := CommandRunner{
		Agent: "aviary/reviewer", Repo: "o/r", Transcripts: store,
		Aviary: func(_ context.Context, run AviaryAgentRun) error {
			got = run
			run.Event(agent.StreamEvent{Type: agent.StreamEventText, Text: "Cloning "})
			run.Event(agent.StreamEvent{Type: agent.StreamEventText, Text: "now\nAVIARY_CHECKOUT_DIRECTORY=" + checkout})
			run.Event(agent.StreamEvent{Type: agent.StreamEventTool, Tool: &agent.ToolEvent{Name: "exec", Args: map[string]any{"command": "go test"}}})
			run.Event(agent.StreamEvent{Type: agent.StreamEventTool, Tool: &agent.ToolEvent{Name: "exec", Result: "ok", Done: true}})
			run.Event(agent.StreamEvent{Type: agent.StreamEventFile, File: &agent.FileChangeEvent{Tool: "file_write", Path: checkout + "/a.go", Diff: "+x"}})
			run.Event(agent.StreamEvent{Type: agent.StreamEventText, Text: "Done."})
			run.Event(agent.StreamEvent{Type: agent.StreamEventDone})
			return nil
		},
	}
	issue := Issue{Number: 7}
	var announced string
	if _, err := runner.runOnce(context.Background(), issue, AgentSession{RunDir: runDir}, func(s AgentSession) {
		if s.CheckoutDirectory != "" {
			announced = s.CheckoutDirectory
		}
	}, nil); err != nil {
		t.Fatal(err)
	}
	if got.Agent != "reviewer" || got.Workdir != runDir || !strings.HasPrefix(got.Prompt, "/gh-fix o/r#7") {
		t.Fatalf("run = %+v", got)
	}
	if announced != checkout {
		t.Fatalf("announced checkout = %q, want %q", announced, checkout)
	}
	events := store.For(issueKey(issue)).Events(0)
	var kinds []transcript.Kind
	for _, event := range events {
		kinds = append(kinds, event.Kind)
	}
	want := []transcript.Kind{transcript.KindMessage, transcript.KindToolCall, transcript.KindToolResult, transcript.KindFileChange, transcript.KindMessage}
	if !slices.Equal(kinds, want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	if call, result := events[1], events[2]; call.CallID == "" || result.CallID != call.CallID || result.Output != "ok" || call.Input["command"] != "go test" {
		t.Fatalf("call = %+v, result = %+v", call, result)
	}
	if change := events[3]; change.Path != checkout+"/a.go" || change.Diff != "+x" {
		t.Fatalf("file change = %+v", change)
	}
	if raw := store.For(issueKey(issue)).Raw(0); !strings.Contains(raw, "Running: exec") || !strings.Contains(raw, "Editing: ") {
		t.Fatalf("raw output = %q", raw)
	}
}

// Stopping a job cancels its turn, which reports context.Canceled.
func TestAviaryAgentJobStopsWithItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	runner := CommandRunner{
		Agent: "aviary/reviewer", Repo: "o/r",
		Aviary: func(ctx context.Context, run AviaryAgentRun) error {
			close(started)
			<-ctx.Done()
			run.Event(agent.StreamEvent{Type: agent.StreamEventStop})
			return context.Canceled
		},
	}
	done := make(chan error, 1)
	go func() {
		_, err := runner.runOnce(ctx, Issue{Number: 7}, AgentSession{RunDir: t.TempDir()}, nil, nil)
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

// Outside the server no core runner is injected, so the job fails rather than
// falling back to a subprocess.
func TestAviaryAgentJobNeedsTheServer(t *testing.T) {
	runner := CommandRunner{Agent: "aviary/reviewer", Repo: "o/r"}
	_, err := runner.runOnce(context.Background(), Issue{Number: 7}, AgentSession{RunDir: t.TempDir()}, nil, nil)
	if !errors.Is(err, errNoAviaryRunner) {
		t.Fatalf("error = %v", err)
	}
}

// The unblock watchdog and vision calls run in-process too, the vision call
// passing its screenshot as the turn's media.
func TestAviaryAgentOneShotCallsRunInProcess(t *testing.T) {
	var runs []AviaryAgentRun
	runner := CommandRunner{
		Agent: "aviary/reviewer",
		Aviary: func(_ context.Context, run AviaryAgentRun) error {
			runs = append(runs, run)
			run.Event(agent.StreamEvent{Type: agent.StreamEventText, Text: "[1,2]"})
			return nil
		},
	}
	text, err := runner.runUnblock(context.Background(), agentSpec{Name: "aviary", Model: "reviewer"}, "unblock these")
	if err != nil || strings.TrimSpace(text) != "[1,2]" {
		t.Fatalf("unblock = %q, %v", text, err)
	}
	vision := visionAgentRunner{runner: runner}
	text, err = vision.RunInProcess(context.Background(), vision.VisionAgent(), "read it", "data:image/png;base64,AA==")
	if err != nil || strings.TrimSpace(text) != "[1,2]" {
		t.Fatalf("vision = %q, %v", text, err)
	}
	if len(runs) != 2 || runs[0].Prompt != "unblock these" || runs[1].Agent != "reviewer" || runs[1].MediaURL != "data:image/png;base64,AA==" {
		t.Fatalf("runs = %+v", runs)
	}
}

// #459: the shutdown checkpoint holds in-process turns, which have no process
// to pause: a turn already running is held by pauseAll, and one that starts
// while the checkpoint has everything paused is held from its start.
func TestCheckpointHoldsInProcessTurns(t *testing.T) {
	active := &activeAgents{}
	holds := make(chan *agent.TurnHold, 2)
	release := make(chan struct{})
	runner := CommandRunner{
		Agent: "aviary/reviewer", Repo: "o/r", Active: active,
		Aviary: func(_ context.Context, run AviaryAgentRun) error {
			holds <- run.Hold
			<-release
			return nil
		},
	}
	errs := make(chan error, 2)
	start := func(number int) {
		go func() {
			_, err := runner.runOnce(context.Background(), Issue{Number: number}, AgentSession{RunDir: t.TempDir()}, nil, nil)
			errs <- err
		}()
	}
	start(1)
	running := <-holds
	if running == nil || running.Engaged() {
		t.Fatalf("running turn hold = %+v, want a released hold", running)
	}
	if runs := active.pauseAll(); len(runs) != 1 {
		t.Fatalf("paused runs = %+v", runs)
	}
	if !running.Engaged() {
		t.Fatal("checkpoint did not hold the running in-process turn")
	}
	start(2)
	if late := <-holds; late == nil || !late.Engaged() {
		t.Fatal("in-process turn started during the checkpoint is not held")
	}
	close(release)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}
