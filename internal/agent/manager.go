package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/store"
)

// Manager maintains a registry of AgentRunners and reconciles them with config.
type Manager struct {
	mu      sync.RWMutex
	runners map[string]*AgentRunner // keyed by agent name
	order   []string                // agent names in config entry order
	session *SessionManager
	factory *llm.Factory
	cfg     *config.Config // latest reconciled config, for checkpoint timeout

	recovery sync.WaitGroup // background checkpoint recoveries started by Reconcile
}

// NewManager creates a new Manager with an optional LLM factory.
func NewManager(factory *llm.Factory) *Manager {
	return &Manager{
		runners: make(map[string]*AgentRunner),
		session: NewSessionManager(),
		factory: factory,
	}
}

// Reconcile idempotently adds, updates, or removes agents based on cfg.
// It is safe to call concurrently and from a config watcher goroutine.
func (m *Manager) Reconcile(cfg *config.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cfg = cfg

	// The built-in Code Loop agent runs alongside the configured ones.
	agents := config.EffectiveAgents(cfg)
	desired := make(map[string]*config.AgentConfig, len(agents))
	for i := range agents {
		ac := &agents[i]
		desired[ac.Name] = ac
	}

	// Remove agents no longer in config.
	for name, runner := range m.runners {
		if _, ok := desired[name]; !ok {
			slog.Info("agent removed", "name", name)
			runner.Stop()
			delete(m.runners, name)
		}
	}

	// Rebuild order to match config entry order, dropping removed agents.
	newOrder := make([]string, 0, len(agents))
	for i := range agents {
		newOrder = append(newOrder, agents[i].Name)
	}
	m.order = newOrder

	// Add or update agents. Every runner picks up the chat approval mode,
	// including those whose own config did not change.
	approval := cfg.ChatApprovalMode()
	defer func() {
		for _, runner := range m.runners {
			runner.SetApproval(approval)
		}
	}()
	for name, ac := range desired {
		m.reconcileAgent(name, ac, cfg)
	}
}

// reconcileAgent starts the runner for the configured agent, or replaces its
// runner when the agent's model, fallbacks or config changed. Called with
// m.mu held.
func (m *Manager) reconcileAgent(name string, ac *config.AgentConfig, cfg *config.Config) {
	effectiveModel := config.EffectiveAgentModel(*ac, cfg.Models)
	effectiveFallbacks := config.EffectiveAgentFallbacks(*ac, cfg.Models)
	predecessor, ok := m.runners[name]
	switch {
	case !ok:
		slog.Info("agent started", "name", name)
	case predecessor.agent.Model == effectiveModel &&
		slices.Equal(predecessor.agent.Fallbacks, effectiveFallbacks) &&
		reflect.DeepEqual(predecessor.cfg, ac):
		return
	default:
		// The old runner finishes its in-flight prompts rather than
		// being stopped, so a config change does not cut a running turn
		// short (and have recovery replay it); new prompts reach the new
		// runner.
		slog.Info("agent updated", "name", name)
	}
	a := &domain.Agent{
		ID:        name,
		Name:      name,
		Model:     effectiveModel,
		Fallbacks: effectiveFallbacks,
		State:     domain.AgentStateIdle,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	runner := NewAgentRunner(a, ac, m.providerFor(name, effectiveModel), m.factory)
	if predecessor != nil {
		runner.succeed(predecessor)
	}
	m.runners[name] = runner
	m.recovery.Go(func() { m.recoverCheckpoints(runner) })
}

// providerFor returns the LLM provider for the agent's model, or nil when
// there is no factory or it cannot serve the model.
func (m *Manager) providerFor(name, model string) llm.Provider {
	if m.factory == nil {
		return nil
	}
	p, err := m.factory.ForModel(model)
	if err != nil {
		if model != "" {
			slog.Warn("failed to create LLM provider", "agent", name, "model", model, "err", err)
		}
		return nil
	}
	return p
}

// recoverCheckpoints scans the agent's running/ directory for interrupted
// prompt checkpoints and either re-issues them or sends a timeout notification.
func (m *Manager) recoverCheckpoints(runner *AgentRunner) {
	timeout := m.failedTaskTimeout()
	dir := store.CheckpointDir(runner.agent.ID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return // no running/ dir or unreadable — nothing to recover
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		// Recovery runs in the background, so the runner may already have
		// started a prompt of its own; its checkpoint is not interrupted.
		if runner.ownsCheckpoint(path) {
			continue
		}
		recoverCheckpoint(runner, path, timeout)
	}
}

// failedTaskTimeout returns the recovery window of the reconciled config.
func (m *Manager) failedTaskTimeout() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cfg != nil {
		return m.cfg.Server.EffectiveFailedTaskTimeout()
	}
	return config.DefaultFailedTaskTimeout
}

// recoverCheckpoint re-issues the interrupted prompt checkpointed at path, or
// gives it up and tells its session when it is past the recovery window or
// retry limit. A checkpoint recovered within the cooldown is left alone.
func recoverCheckpoint(runner *AgentRunner, path string, timeout time.Duration) {
	cp, err := store.ReadJSON[RunCheckpoint](path)
	if err != nil {
		slog.Warn("agent: ignoring unreadable checkpoint", "path", path, "err", err)
		_ = store.DeleteJSON(path)
		return
	}

	age := time.Since(cp.CreatedAt)
	if age > timeout {
		slog.Info("agent: checkpoint timed out, notifying session",
			"agent", runner.agent.Name, "session", cp.SessionID, "age", age)
		abandonCheckpoint(runner, path, cp, fmt.Sprintf("I was interrupted %s ago and the recovery window (%s) has passed. Please resend your request if it is still needed.", age.Round(time.Second), timeout))
		return
	}
	if cp.RetryCount >= maxCheckpointRecoveryRetries {
		slog.Info("agent: checkpoint retry limit reached, notifying session",
			"agent", runner.agent.Name, "session", cp.SessionID,
			"retry", cp.RetryCount, "limit", maxCheckpointRecoveryRetries)
		abandonCheckpoint(runner, path, cp, fmt.Sprintf("I was interrupted and retry recovery %d times without finishing, so I stopped retrying. Please resend your request if it is still needed.", cp.RetryCount))
		return
	}
	if !cp.LastRecoveredAt.IsZero() {
		sinceLastRecovery := time.Since(cp.LastRecoveredAt)
		if sinceLastRecovery < checkpointRecoveryCooldown {
			slog.Debug("agent: skipping recent checkpoint recovery",
				"agent", runner.agent.Name, "session", cp.SessionID,
				"retry", cp.RetryCount, "since_last_recovery", sinceLastRecovery)
			return
		}
	}

	slog.Info("agent: recovering interrupted prompt",
		"agent", runner.agent.Name, "session", cp.SessionID,
		"age", age, "retry", cp.RetryCount)
	// Increment retry count and re-write checkpoint before re-issuing.
	cp.RetryCount++
	cp.LastRecoveredAt = time.Now()
	_ = store.WriteJSON(path, cp)

	ctx := WithSessionID(context.Background(), cp.SessionID)
	name := filepath.Base(path)
	checkpointID := strings.TrimSuffix(name, filepath.Ext(name))
	runner.recoverPrompt(ctx, checkpointID, path, cp)
}

// abandonCheckpoint tells the checkpoint's session msg in place of
// recovering its prompt, and removes the checkpoint.
func abandonCheckpoint(runner *AgentRunner, path string, cp RunCheckpoint, msg string) {
	runner.appendSessionMessage(cp.SessionID, domain.MessageRoleAssistant, msg, nil, "")
	deliverToSession(runner.agent.ID, cp.SessionID, msg)
	_ = store.DeleteJSON(path)
}

// Get returns the runner for the named agent.
func (m *Manager) Get(name string) (*AgentRunner, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.runners[name]
	return r, ok
}

// GetByID returns the runner for a concrete agent ID.
func (m *Manager) GetByID(agentID string) (*AgentRunner, bool) {
	return m.Get(agentID)
}

// List returns a snapshot of all agents in config entry order.
func (m *Manager) List() []*domain.Agent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*domain.Agent, 0, len(m.runners))
	for _, name := range m.order {
		if r, ok := m.runners[name]; ok {
			out = append(out, r.Agent())
		}
	}
	return out
}

// WaitRecovery blocks until the checkpoint recoveries started by Reconcile
// have finished.
func (m *Manager) WaitRecovery() { m.recovery.Wait() }

// Stop stops all agents for Aviary's shutdown or restart. Their in-flight
// prompts are interrupted, not stopped by request, and resume on restart.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.runners {
		r.Interrupt()
	}
}
