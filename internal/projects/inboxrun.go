package projects

import (
	"context"
	"io"
	"sync"
	"time"
)

// inboxRetryInterval is how often a run whose agent has not reported its
// session yet checks again whether output waiting for it can be relayed.
var inboxRetryInterval = 2 * time.Second

// inboxRun describes a Discussion job's or scheduled run's agent run, which
// output sent to the job is relayed into (issue #1007), the way an issue
// job's watcher relays it.
type inboxRun struct {
	// key is the job's key, subject names it in logs, and intro opens the
	// prompt its resumed session is given with the output.
	key, subject, intro string
	issue               Issue
	output              io.Writer
	// updateSession hears the session and checkout the agent reports.
	updateSession func(AgentSession)
}

// runRelayingInbox runs the agent for r in session. Output sent to the job
// while it runs interrupts the run and resumes the same session with it, as
// often as output arrives, until a run ends on its own. A runner that cannot
// resume sessions runs once and leaves output queued.
func (w *Engine) runRelayingInbox(ctx context.Context, r inboxRun, session AgentSession) error {
	runner := w.runner()
	_, outputSessions := runner.(SessionAgentOutputRunner)
	_, plainSessions := runner.(SessionAgentRunner)
	resumable := outputSessions || plainSessions
	var mu sync.Mutex
	current := session
	update := func(reported AgentSession) {
		mu.Lock()
		if reported.ID != "" {
			current.ID = reported.ID
		}
		if reported.CheckoutDirectory != "" {
			current.CheckoutDirectory = reported.CheckoutDirectory
		}
		mu.Unlock()
		if r.updateSession != nil {
			r.updateSession(reported)
		}
	}
	// tracked is r with the agent's reports also tracked here, so a resumed
	// run continues the session the agent reported.
	tracked := r
	tracked.updateSession = update
	sessionID := func() string {
		mu.Lock()
		defer mu.Unlock()
		return current.ID
	}
	nudge := make(chan struct{}, 1)
	w.liveNudges.Store(r.key, nudge)
	defer w.liveNudges.Delete(r.key)
	for {
		runCtx, cancel := context.WithCancelCause(ctx)
		watching := make(chan struct{})
		go func() {
			defer close(watching)
			if !resumable {
				return
			}
			// A nudge that arrives before the agent reports its session
			// leaves the output queued, so the run checks again until the
			// session is known.
			retry := time.NewTicker(inboxRetryInterval)
			defer retry.Stop()
			for {
				select {
				case <-runCtx.Done():
					return
				case <-nudge:
				case <-retry.C:
					if w.jobInbox().pending(r.key) == 0 {
						continue
					}
				}
				if sessionID() != "" && w.relayInboxTo(r.key, r.subject, r.intro, cancel) {
					return
				}
			}
		}()
		err := w.runAgentSession(runCtx, runner, tracked, session)
		cause := context.Cause(runCtx)
		cancel(nil)
		<-watching
		relayed, ok := workUpdateFor(cause)
		if !ok || ctx.Err() != nil {
			return err
		}
		mu.Lock()
		session = AgentSession{ID: current.ID, Agent: current.Agent, Model: current.Model, CheckoutDirectory: current.CheckoutDirectory, Resume: true, Update: relayed.instruction}
		mu.Unlock()
		w.logf("%s resuming session %s with the output sent to it", r.subject, session.ID)
	}
}

// runAgentSession runs runner's agent on r's issue in session with the
// richest interface it implements, writing to r's output and reporting the
// session to r's updateSession.
func (w *Engine) runAgentSession(ctx context.Context, runner AgentRunner, r inboxRun, session AgentSession) error {
	issue, update, output := r.issue, r.updateSession, r.output
	switch runner := runner.(type) {
	case SessionAgentOutputRunner:
		return runner.RunSessionWithOutput(ctx, issue, session, update, output)
	case SessionAgentRunner:
		return runner.RunSession(ctx, issue, session, update)
	case AgentOutputRunner:
		return runner.RunWithOutput(ctx, issue, output)
	default:
		return runner.Run(ctx, issue)
	}
}
