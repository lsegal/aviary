package agent

import (
	"context"
	"sync"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/llm"
)

// AgentRunner manages an agent's active prompts and lifecycle.
type AgentRunner struct {
	agent    *domain.Agent
	cfg      *config.AgentConfig
	provider llm.Provider // nil until Phase 5 wiring; falls back to stub
	stopCh   chan struct{}
	mu       sync.Mutex
	active   sync.WaitGroup
	canceled bool
}

// NewAgentRunner creates an AgentRunner for the given agent.
func NewAgentRunner(a *domain.Agent, cfg *config.AgentConfig, provider llm.Provider) *AgentRunner {
	return &AgentRunner{
		agent:    a,
		cfg:      cfg,
		provider: provider,
		stopCh:   make(chan struct{}),
	}
}

// Prompt sends a message to the agent and fans out stream events to consumers.
// Each call runs in its own goroutine; multiple concurrent calls are supported.
func (r *AgentRunner) Prompt(ctx context.Context, message string, consumers ...StreamConsumer) {
	r.mu.Lock()
	if r.canceled {
		r.mu.Unlock()
		for _, c := range consumers {
			c(StreamEvent{Type: StreamEventStop, AgentID: r.agent.ID})
		}
		return
	}
	r.active.Add(1)
	r.mu.Unlock()

	go func() {
		defer r.active.Done()

		promptCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		// Stop if stopCh is closed.
		go func() {
			select {
			case <-r.stopCh:
				cancel()
			case <-promptCtx.Done():
			}
		}()

		emit := func(e StreamEvent) {
			e.AgentID = r.agent.ID
			for _, c := range consumers {
				c(e)
			}
		}

		if r.provider == nil {
			// Stub: no LLM provider configured.
			emit(StreamEvent{Type: StreamEventText, Text: "[no LLM provider configured for " + r.agent.Model + "]"})
			emit(StreamEvent{Type: StreamEventDone})
			return
		}

		req := llm.Request{
			Model:    r.agent.Model,
			Messages: []llm.Message{{Role: llm.RoleUser, Content: message}},
			Stream:   true,
		}

		ch, err := r.provider.Stream(promptCtx, req)
		if err != nil {
			emit(StreamEvent{Type: StreamEventError, Err: err})
			return
		}

		for event := range ch {
			switch event.Type {
			case llm.EventTypeText:
				emit(StreamEvent{Type: StreamEventText, Text: event.Text})
			case llm.EventTypeError:
				emit(StreamEvent{Type: StreamEventError, Err: event.Error})
				return
			case llm.EventTypeDone:
				emit(StreamEvent{Type: StreamEventDone})
				return
			}
		}
	}()
}

// Stop cancels all in-flight prompts for this agent.
func (r *AgentRunner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.canceled {
		r.canceled = true
		close(r.stopCh)
	}
}

// Wait blocks until all active prompts finish.
func (r *AgentRunner) Wait() { r.active.Wait() }

// Agent returns the domain agent.
func (r *AgentRunner) Agent() *domain.Agent { return r.agent }

// Config returns the agent's config snapshot.
func (r *AgentRunner) Config() *config.AgentConfig { return r.cfg }
