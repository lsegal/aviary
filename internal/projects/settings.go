package projects

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/projects/agents"
	"github.com/lsegal/aviary/internal/projects/core"
)

// maxConcurrencyPermits bounds how high --concurrency can be raised at
// runtime. It is far above any concurrency a real instance
// would use; it only exists so concurrencySemaphore can pre-allocate a
// fixed-capacity channel instead of an unbounded one.
const maxConcurrencyPermits = 256

// concurrencySemaphore is a counting semaphore whose capacity can change
// while acquire/release calls are in flight, which a plain buffered channel
// cannot do. Live settings changes need to grow or shrink the
// number of concurrently dispatched agents without restarting aviary.
type concurrencySemaphore struct {
	mu     sync.Mutex
	limit  int
	debt   int
	tokens chan struct{}
}

func newConcurrencySemaphore(limit int) *concurrencySemaphore {
	s := &concurrencySemaphore{tokens: make(chan struct{}, maxConcurrencyPermits)}
	s.resize(limit)
	return s
}

// acquire blocks until a slot is available or ctx is done.
func (s *concurrencySemaphore) acquire(ctx context.Context) error {
	select {
	case <-s.tokens:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// release returns a slot. A shrink that happened while the slot was held
// withdraws it instead of returning it to the pool.
func (s *concurrencySemaphore) release() {
	s.mu.Lock()
	if s.debt > 0 {
		s.debt--
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	s.tokens <- struct{}{}
}

// resize changes the number of available slots. Growing mints new tokens
// immediately; shrinking withdraws idle tokens first and records the rest as
// debt to withdraw from future releases, so slots already in use are never
// revoked out from under a running task.
func (s *concurrencySemaphore) resize(newLimit int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delta := newLimit - s.limit
	s.limit = newLimit
	if delta > 0 {
		for i := 0; i < delta; i++ {
			s.tokens <- struct{}{}
		}
		return
	}
	for i := 0; i < -delta; i++ {
		select {
		case <-s.tokens:
		default:
			s.debt++
		}
	}
}

// SettingsUpdate and SettingsSnapshot live in package core so the browser
// dashboard's settings modal can speak them without importing the
// root package.
type SettingsUpdate = core.SettingsUpdate

// SettingsSnapshot is the engine's live settings.
type SettingsSnapshot = core.SettingsSnapshot

type settingsRequest struct {
	update SettingsUpdate
	done   chan settingsResult
}

type settingsResult struct {
	snapshot SettingsSnapshot
	err      error
}

func (w *Engine) settingsRequestsChan() chan settingsRequest {
	w.settingsOnce.Do(func() { w.settingsRequests = make(chan settingsRequest) })
	return w.settingsRequests
}

// ApplySettings validates and applies update, returning the resulting
// settings snapshot. It hands the update to the running Run loop, which
// owns the mutated fields, so it only takes effect once Run is looping. An
// update that changes nothing is a read, answered from CurrentSettings
// without waiting on the loop at all.
func (w *Engine) ApplySettings(ctx context.Context, update SettingsUpdate) (SettingsSnapshot, error) {
	if update == (SettingsUpdate{}) {
		return w.CurrentSettings(ctx)
	}
	if err := w.checkReady(ctx); err != nil {
		return SettingsSnapshot{}, err
	}
	request := settingsRequest{update: update, done: make(chan settingsResult, 1)}
	select {
	case w.settingsRequestsChan() <- request:
	case <-ctx.Done():
		return SettingsSnapshot{}, ctx.Err()
	}
	select {
	case result := <-request.done:
		return result.snapshot, result.err
	case <-ctx.Done():
		return SettingsSnapshot{}, ctx.Err()
	}
}

// CurrentSettings reports the live settings snapshot without changing
// anything. It reads the snapshot the Run loop last published rather than
// asking the loop, which may be in the middle of a poll, so the settings
// modal renders immediately. Before Run has published one,
// nothing has changed the settings yet, so they are read as configured.
func (w *Engine) CurrentSettings(ctx context.Context) (SettingsSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return SettingsSnapshot{}, err
	}
	if snapshot := w.settingsView.Load(); snapshot != nil {
		return *snapshot, nil
	}
	return w.settingsSnapshot(), nil
}

// publishSettings records the current settings for CurrentSettings and
// returns them. Like settingsSnapshot, it must only be called from the Run
// loop's goroutine.
func (w *Engine) publishSettings() SettingsSnapshot {
	snapshot := w.settingsSnapshot()
	w.settingsView.Store(&snapshot)
	return snapshot
}

// validateSettingsUpdate checks update in isolation, before it reaches the
// Run loop, so malformed requests fail fast with a useful message instead of
// silently corrupting state owned by that loop.
func validateSettingsUpdate(registry *agents.Registry, update SettingsUpdate) error {
	if update.Concurrency != nil && (*update.Concurrency < 1 || *update.Concurrency > maxConcurrencyPermits) {
		return fmt.Errorf("concurrency must be between 1 and %d", maxConcurrencyPermits)
	}
	if update.ActiveAgents != nil {
		if len(*update.ActiveAgents) == 0 {
			return fmt.Errorf("activeAgents must not be empty")
		}
		for _, spec := range *update.ActiveAgents {
			if strings.TrimSpace(spec) == "" {
				return fmt.Errorf("agent must not be empty")
			}
			// The registry answers an unknown agent with the agents that do
			// exist, so the dashboard and the settings API reject a typo with
			// the same list the CLI prints.
			if _, err := parseAgentSpecIn(registry, spec); err != nil {
				return err
			}
		}
	}
	if update.TempDirs != nil {
		if _, err := normalizeTempDirs(*update.TempDirs); err != nil {
			return err
		}
	}
	if update.UnblockInterval != nil {
		if _, err := parseUnblockInterval(*update.UnblockInterval); err != nil {
			return err
		}
	}
	return nil
}

// applySettingsRequest mutates the fields owned by the Run loop and returns
// the resulting snapshot. It must only be called from the Run loop's
// goroutine, the same rule handleJobAction's callers already follow.
func (w *Engine) applySettingsRequest(update SettingsUpdate, sem *concurrencySemaphore) settingsResult {
	if err := validateSettingsUpdate(w.registry(), update); err != nil {
		return settingsResult{err: err}
	}
	if update.UnblockAgent != nil || update.UnblockModel != nil {
		config := w.unblockSettings()
		if update.UnblockAgent != nil {
			config.Agent = strings.TrimSpace(*update.UnblockAgent)
		}
		if update.UnblockModel != nil {
			config.Model = strings.TrimSpace(*update.UnblockModel)
		}
		if err := validateUnblockAgent(w.registry(), config); err != nil {
			return settingsResult{err: err}
		}
		w.SetUnblockAgent(config)
	}
	if update.UnblockInterval != nil {
		interval, _ := parseUnblockInterval(*update.UnblockInterval)
		if interval != w.UnblockInterval() {
			w.SetUnblockInterval(interval)
			if interval > 0 {
				w.logf("checking for stuck jobs every %s", formatInterval(interval))
			} else {
				w.logf("stuck-job watchdog turned off")
			}
		}
	}
	if update.Concurrency != nil {
		w.Concurrency = *update.Concurrency
		sem.resize(w.Concurrency)
	}
	if update.ReadyState != nil {
		w.ReadyState = strings.TrimSpace(*update.ReadyState)
	}
	if update.AllowedCommenters != nil {
		w.AllowedCommenters = splitAllowedCommenters(strings.Join(*update.AllowedCommenters, ","))
		allowed := append([]string(nil), w.AllowedCommenters...)
		w.allowedCommenters.Store(&allowed)
	}
	if update.NoMerge != nil {
		w.noMerge.Store(*update.NoMerge)
	}
	if update.NoChangelog != nil {
		w.noChangelog.Store(*update.NoChangelog)
	}
	if update.AutoUpdate != nil {
		w.autoUpdate.Store(*update.AutoUpdate)
	}
	if update.TempDirs != nil {
		dirs, _ := normalizeTempDirs(*update.TempDirs)
		w.SetTempDirs(dirs)
	}
	if update.ActiveAgents != nil {
		trimmed := make([]string, len(*update.ActiveAgents))
		for i, spec := range *update.ActiveAgents {
			trimmed[i] = strings.TrimSpace(spec)
		}
		w.agentOverride.Store(&trimmed)
	}
	return settingsResult{snapshot: w.publishSettings()}
}

// registry returns the agent registry this run was configured with, falling
// back to the run's own when the instance carries none. Every settings
// surface reads the agent list from here rather than from the two names aviary
// used to ship, so a .aviary.config.json agent is selectable without a code
// change.
func (w *Engine) registry() *agents.Registry {
	if w.Registry != nil {
		return w.Registry
	}
	return agentRegistry()
}

// agentOptions lists the registered agents with the models and levels each
// accepts, for the dashboard's agent selector.
func (w *Engine) agentOptions() []core.AgentOption {
	registry := w.registry()
	names := registry.Names()
	options := make([]core.AgentOption, 0, len(names))
	for _, name := range names {
		definition, ok := registry.Lookup(name)
		if !ok {
			continue
		}
		options = append(options, core.AgentOption{
			Name:   name,
			Models: definition.Models.Values(),
			Levels: definition.Levels.Values(),
		})
	}
	return options
}

// settingsSnapshot reads the fields applySettingsRequest owns. Like that
// function, it must only be called from the Run loop's goroutine.
func (w *Engine) settingsSnapshot() SettingsSnapshot {
	return SettingsSnapshot{
		Concurrency: w.Concurrency,
		ReadyState:  w.ReadyState,
		// ReadyStateDefault surfaces the value projectReadyState falls back to
		// when ReadyState is unset, so the settings UI can show
		// what's actually in effect instead of leaving the field blank.
		ReadyStateDefault: projectReadyState(w.ReadyState, ""),
		AllowedCommenters: append([]string(nil), w.AllowedCommenters...),
		NoMerge:           w.noMerge.Load(),
		NoChangelog:       w.noChangelog.Load(),
		AutoUpdate:        w.autoUpdate.Load(),
		TempDirs:          w.TempDirs(),
		TempDirDefault:    filepath.Clean(os.TempDir()),
		Agents:            w.registry().Names(),
		AgentOptions:      w.agentOptions(),
		ConfiguredAgents:  w.configuredAgents(),
		UnblockInterval:   unblockIntervalText(w.UnblockInterval()),
		UnblockAgent:      w.unblockSettings().Agent,
		UnblockModel:      w.unblockSettings().Model,
		UnblockDefault:    w.unblockDefault(),
	}
}

// unblockIntervalText renders the watchdog interval the way the dashboard and
// the config file take it back.
func unblockIntervalText(interval time.Duration) string {
	if interval <= 0 {
		return "0"
	}
	return formatInterval(interval)
}

// unblockDefault is the coding agent spec the unblock call falls back to.
func (w *Engine) unblockDefault() string {
	runner, ok := w.runner().(CommandRunner)
	if !ok {
		return ""
	}
	return runner.specForSession(AgentSession{}).String()
}

// runner returns the AgentRunner new dispatches should use, applying any
// live agent override on top of the configured CommandRunner.
// Unlike the fields applySettingsRequest owns, this is read from goroutines
// spawned by the Run loop as well as the loop itself, so the override is
// stored behind an atomic pointer rather than left to single-goroutine
// ownership.
func (w *Engine) runner() AgentRunner {
	override := w.agentOverride.Load()
	if runner, ok := w.Runner.(CommandRunner); ok {
		runner.NoMerge = w.noMerge.Load()
		runner.NoChangelog = w.noChangelog.Load()
		if allowed := w.allowedCommenters.Load(); allowed != nil {
			runner.AllowedCommenters = *allowed
		}
		if override != nil && len(*override) > 0 {
			runner.Agent = (*override)[0]
			runner.Agents = append([]string(nil), *override...)
		}
		return runner
	}
	return w.Runner
}

// configuredAgents returns the agent specs new dispatches may use: the live
// override when one is set, otherwise the CommandRunner's configured agents.
// It reports nothing for runners that carry no agent configuration at all.
func (w *Engine) configuredAgents() []string {
	if override := w.agentOverride.Load(); override != nil && len(*override) > 0 {
		return append([]string(nil), *override...)
	}
	runner, ok := w.Runner.(CommandRunner)
	if !ok {
		return nil
	}
	var specs []string
	if len(runner.Agents) > 0 {
		specs = append(specs, runner.Agents...)
	} else if runner.Agent != "" {
		specs = append(specs, runner.Agent)
	}
	targets := make([]string, 0, len(runner.TargetAgents))
	for target := range runner.TargetAgents {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	for _, target := range targets {
		for _, spec := range runner.TargetAgents[target] {
			if !slices.Contains(specs, spec) {
				specs = append(specs, spec)
			}
		}
	}
	return specs
}

// agentStillConfigured reports whether a persisted session's agent is one the
// current configuration would still dispatch to. Work state
// written by an earlier run must not pin an issue to an agent the -agent flag
// no longer includes. Runners that expose no agent configuration can't
// contradict the persisted value, so their sessions resume unchanged.
func (w *Engine) agentStillConfigured(agent string) bool {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		return false
	}
	configured := w.configuredAgents()
	if len(configured) == 0 {
		return true
	}
	for _, candidate := range configured {
		if strings.TrimSpace(candidate) == agent {
			return true
		}
	}
	return false
}
