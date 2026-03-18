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
	"github.com/lsegal/aviary/internal/memory"
	"github.com/lsegal/aviary/internal/store"
)

// Manager maintains a registry of AgentRunners and reconciles them with config.
type Manager struct {
	mu      sync.RWMutex
	runners map[string]*AgentRunner // keyed by agent name
	order   []string                // agent names in config entry order
	session *SessionManager
	factory *llm.Factory
	memory  *memory.Manager
	cfg     *config.Config // latest reconciled config, for checkpoint timeout
}

// NewManager creates a new Manager with an optional LLM factory.
func NewManager(factory *llm.Factory) *Manager {
	return &Manager{
		runners: make(map[string]*AgentRunner),
		session: NewSessionManager(),
		factory: factory,
		memory:  memory.New(),
	}
}

// Reconcile idempotently adds, updates, or removes agents based on cfg.
// It is safe to call concurrently and from a config watcher goroutine.
func (m *Manager) Reconcile(cfg *config.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cfg = cfg

	desired := make(map[string]*config.AgentConfig, len(cfg.Agents))
	for i := range cfg.Agents {
		ac := &cfg.Agents[i]
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
	newOrder := make([]string, 0, len(cfg.Agents))
	for i := range cfg.Agents {
		newOrder = append(newOrder, cfg.Agents[i].Name)
	}
	m.order = newOrder

	// Add or update agents.
	for name, ac := range desired {
		effectiveModel := config.EffectiveAgentModel(*ac, cfg.Models)
		effectiveFallbacks := config.EffectiveAgentFallbacks(*ac, cfg.Models)
		if existing, ok := m.runners[name]; ok {
			if existing.agent.Model == effectiveModel &&
				slices.Equal(existing.agent.Fallbacks, effectiveFallbacks) &&
				reflect.DeepEqual(existing.cfg, ac) {
				continue
			}
			slog.Info("agent updated", "name", name)
			existing.Stop()
		} else {
			slog.Info("agent started", "name", name)
		}
		a := &domain.Agent{
			ID:        fmt.Sprintf("agent_%s", name),
			Name:      name,
			Model:     effectiveModel,
			Fallbacks: effectiveFallbacks,
			State:     domain.AgentStateIdle,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		var provider llm.Provider
		if m.factory != nil {
			if p, err := m.factory.ForModel(effectiveModel); err == nil {
				provider = p
			} else if effectiveModel != "" {
				slog.Warn("failed to create LLM provider", "agent", name, "model", effectiveModel, "err", err)
			}
		}
		runner := NewAgentRunner(a, ac, provider, m.factory, m.memory)
		m.runners[name] = runner
		go m.recoverCheckpoints(runner)
	}
}

// recoverCheckpoints scans the agent's running/ directory for interrupted
// prompt checkpoints and either re-issues them or sends a timeout notification.
func (m *Manager) recoverCheckpoints(runner *AgentRunner) {
	timeout := config.DefaultFailedTaskTimeout
	if m.cfg != nil {
		timeout = m.cfg.Server.EffectiveFailedTaskTimeout()
	}

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
		cp, err := store.ReadJSON[RunCheckpoint](path)
		if err != nil {
			slog.Warn("agent: ignoring unreadable checkpoint", "path", path, "err", err)
			_ = store.DeleteJSON(path)
			continue
		}

		age := time.Since(cp.CreatedAt)
		if age > timeout {
			slog.Info("agent: checkpoint timed out, notifying session",
				"agent", runner.agent.Name, "session", cp.SessionID, "age", age)
			msg := fmt.Sprintf("I was interrupted %s ago and the recovery window (%s) has passed. Please resend your request if it is still needed.", age.Round(time.Second), timeout)
			runner.appendSessionMessage(cp.SessionID, domain.MessageRoleAssistant, msg, "", "")
			deliverToSession(cp.SessionID, msg)
			_ = store.DeleteJSON(path)
			continue
		}

		slog.Info("agent: recovering interrupted prompt",
			"agent", runner.agent.Name, "session", cp.SessionID,
			"age", age, "retry", cp.RetryCount)
		// Increment retry count and re-write checkpoint before re-issuing.
		cp.RetryCount++
		_ = store.WriteJSON(path, cp)

		ctx := WithSessionID(context.Background(), cp.SessionID)
		checkpointID := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		go runner.recoverPrompt(ctx, checkpointID, path, cp)
	}
}

// Get returns the runner for the named agent.
func (m *Manager) Get(name string) (*AgentRunner, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.runners[name]
	return r, ok
}

// GetByID returns the runner for a concrete agent ID such as "agent_assistant".
func (m *Manager) GetByID(agentID string) (*AgentRunner, bool) {
	name := strings.TrimPrefix(agentID, "agent_")
	return m.Get(name)
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

// Stop stops all agents.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.runners {
		r.Stop()
	}
}
