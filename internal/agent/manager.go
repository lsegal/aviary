package agent

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/llm"
)

// Manager maintains a registry of AgentRunners and reconciles them with config.
type Manager struct {
	mu      sync.RWMutex
	runners map[string]*AgentRunner // keyed by agent name
	session *SessionManager
	factory *llm.Factory
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

	// Add or update agents.
	for name, ac := range desired {
		if existing, ok := m.runners[name]; ok {
			// Check if config changed (cheap pointer comparison of model string).
			if existing.cfg.Model == ac.Model {
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
			Model:     ac.Model,
			Memory:    ac.Memory,
			State:     domain.AgentStateIdle,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		var provider llm.Provider
		if m.factory != nil {
			if p, err := m.factory.ForModel(ac.Model); err == nil {
				provider = p
			} else {
				slog.Warn("failed to create LLM provider", "agent", name, "model", ac.Model, "err", err)
			}
		}
		m.runners[name] = NewAgentRunner(a, ac, provider)
	}
}

// Get returns the runner for the named agent.
func (m *Manager) Get(name string) (*AgentRunner, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.runners[name]
	return r, ok
}

// List returns a snapshot of all agents.
func (m *Manager) List() []*domain.Agent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*domain.Agent, 0, len(m.runners))
	for _, r := range m.runners {
		out = append(out, r.Agent())
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
