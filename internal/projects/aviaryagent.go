package projects

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/transcript"
)

// AviaryAgentRun is one turn an in-process agent definition, such as the
// built-in aviary agent, runs on one of Aviary's own agents.
type AviaryAgentRun struct {
	// Agent names the Aviary agent: the model of an aviary/<agent> spec.
	Agent string
	// Model overrides the agent's default model for the turn; empty uses it.
	Model string
	// Prompt is the turn's message.
	Prompt string
	// MediaURL is an image the turn sees, for a vision call.
	MediaURL string
	// Workdir is granted to the agent's file tools and exec for the turn,
	// such as a job's clone.
	Workdir string
	// Event receives each event of the turn's stream.
	Event func(agent.StreamEvent)
	// Hold holds the turn's new tool calls while engaged, such as during the
	// shutdown checkpoint.
	Hold *agent.TurnHold
	// Usage names the turn's session in Usage, such as the job's issue.
	Usage agent.UsageSession
}

// AviaryAgentRunner runs one turn through core's agent runner and returns once
// it ends, with the error it ended on. Cancelling ctx stops the turn.
type AviaryAgentRunner func(ctx context.Context, run AviaryAgentRun) error

// SetAviaryAgentRunner makes engines started from now on run in-process agent
// definitions through run. Without one such an agent cannot be dispatched.
func (m *Manager) SetAviaryAgentRunner(run AviaryAgentRunner) {
	m.aviaryRunner.Store(&run)
}

// errNoAviaryRunner is reported when an in-process agent is dispatched outside
// the server, which is the only place core's agents run.
var errNoAviaryRunner = errors.New("in-process agents run only inside the aviary server")

// inProcessTurn is one turn runInProcess runs.
type inProcessTurn struct {
	// model, when set, overrides the agent's default model.
	model    string
	prompt   string
	mediaURL string
	workdir  string
	hold     *agent.TurnHold
	usage    agent.UsageSession
	// output receives the turn's readable output.
	output io.Writer
	// log, which may be nil, receives its structured events.
	log *transcript.Log
}

// runInProcess runs one turn of an in-process agent definition, with the
// turn's model overriding the agent's default model when set, writing its
// readable output to the turn's output and its structured events to its log.
// It returns the turn's error, so a stopped turn reports context.Canceled.
func (r CommandRunner) runInProcess(ctx context.Context, spec agentSpec, turn inProcessTurn) error {
	if r.Aviary == nil {
		return errNoAviaryRunner
	}
	if strings.TrimSpace(spec.Model) == "" {
		return fmt.Errorf("agent %q: name the Aviary agent to run, as %s/<agent>", spec.Name, spec.Name)
	}
	events := &inProcessEvents{output: turn.output, log: turn.log}
	err := r.Aviary(ctx, AviaryAgentRun{
		Agent:    spec.Model,
		Model:    turn.model,
		Prompt:   turn.prompt,
		MediaURL: turn.mediaURL,
		Workdir:  turn.workdir,
		Event:    events.add,
		Hold:     turn.hold,
		Usage:    turn.usage,
	})
	events.flush()
	return err
}

// inProcessEvents turns a core agent turn's stream into the job's transcript
// events and the readable lines its scrollback shows, joining the reply's text
// fragments into one message per stretch of text between tool calls.
type inProcessEvents struct {
	mu     sync.Mutex
	output io.Writer
	log    *transcript.Log
	ids    agent.ToolCallIDs
	text   strings.Builder
}

func (p *inProcessEvents) add(e agent.StreamEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e.Type == agent.StreamEventText {
		p.text.WriteString(e.Text)
		return
	}
	switch e.Type {
	case agent.StreamEventTool:
		if e.Tool == nil {
			return
		}
		p.flushText()
		id := p.ids.ID(e.Tool)
		if !e.Tool.Done {
			p.record(transcript.Event{Kind: transcript.KindToolCall, CallID: id, Name: e.Tool.Name, Input: e.Tool.Args}, "Running: "+e.Tool.Name)
			return
		}
		result := transcript.Event{Kind: transcript.KindToolResult, CallID: id, Name: e.Tool.Name, Output: e.Tool.Result}
		if e.Tool.Error != "" {
			result.Output, result.IsError = e.Tool.Error, true
		}
		p.record(result, "")
	case agent.StreamEventFile:
		if e.File == nil {
			return
		}
		p.flushText()
		p.record(transcript.Event{Kind: transcript.KindFileChange, Name: e.File.Tool, Path: e.File.Path, Diff: e.File.Diff}, "Editing: "+e.File.Path)
	case agent.StreamEventMedia:
		if e.MediaURL == "" {
			return
		}
		p.flushText()
		p.record(transcript.Event{Kind: transcript.KindToolResult, Name: "media", Images: []string{e.MediaURL}}, "")
	case agent.StreamEventError:
		p.flushText()
		if e.Err != nil && !errors.Is(e.Err, context.Canceled) {
			p.record(transcript.Event{Kind: transcript.KindSystem, Text: e.Err.Error(), IsError: true}, "Agent error: "+e.Err.Error())
		}
	case agent.StreamEventDone, agent.StreamEventStop:
		p.flushText()
	}
}

// flush records the reply text the turn ended on.
func (p *inProcessEvents) flush() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.flushText()
}

func (p *inProcessEvents) flushText() {
	text := strings.TrimSpace(p.text.String())
	p.text.Reset()
	if text != "" {
		p.record(transcript.Event{Kind: transcript.KindMessage, Text: text}, text)
	}
}

func (p *inProcessEvents) record(event transcript.Event, line string) {
	p.log.Add(event)
	if line != "" && p.output != nil {
		_, _ = fmt.Fprintln(p.output, line)
	}
}

// runInProcessJob runs a job's turn on an in-process agent definition in the
// job's run directory, which the turn is granted, recording its transcript and
// the checkout it announces the way runOnce does for a child process.
func (r CommandRunner) runInProcessJob(ctx context.Context, issue Issue, session AgentSession, updateSession func(AgentSession), jobOutput io.Writer) error {
	spec := r.specForSession(session)
	session, err := r.resolveRunDir(session)
	if err != nil {
		return err
	}
	workdir, _ := sessionRunDir(session)
	active := r.Active.add(issue, session.CheckoutDirectory)
	defer r.Active.remove(active)
	hold := r.Active.startedInProcess(active)
	output := io.Writer(io.Discard)
	if jobOutput != nil {
		output = io.MultiWriter(output, jobOutput)
	}
	if r.Output != nil {
		output = io.MultiWriter(output, r.Output)
	}
	outputTail := &agentOutputTailWriter{output: output, limit: agentOutputTailLimit}
	output = outputTail
	var metadataOutput *sessionMetadataCaptureWriter
	if updateSession != nil {
		reportSession := updateSession
		update := func(update AgentSession) {
			r.Active.setCheckout(active, update.CheckoutDirectory)
			reportSession(update)
		}
		metadataOutput = &sessionMetadataCaptureWriter{output: output, onUpdate: update, checkout: &checkoutCapture{onUpdate: update}}
		output = metadataOutput
	}
	// The readable output is the job's raw output; the events are recorded
	// as they arrive rather than decoded back out of it.
	var log *transcript.Log
	if r.Transcripts != nil {
		log = r.Transcripts.For(issueKey(issue))
	}
	if recorder := r.transcriptRecorder(issue, ""); recorder != nil {
		output = io.MultiWriter(recorder, output)
	}
	runErr := r.runInProcess(ctx, spec, inProcessTurn{
		model:   session.Model,
		prompt:  jobPrompt(r, issue, session),
		workdir: workdir,
		hold:    hold,
		usage:   agent.UsageSession{Name: jobUsageName(issue), Kind: domain.UsageKindJob, Owner: issueKey(issue)},
		output:  output,
		log:     log,
	})
	if metadataOutput != nil {
		metadataOutput.Flush()
	}
	if runErr != nil {
		return r.agentFailure(issue, []string{spec.String()}, outputTail.Tail(), runErr)
	}
	return nil
}
