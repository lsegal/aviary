package projects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lsegal/aviary/internal/auth"
	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/clidoctor"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/process"
	"github.com/lsegal/aviary/internal/projects/browser"
	"github.com/lsegal/aviary/internal/projects/core"
	"github.com/lsegal/aviary/internal/projects/ngrok"
	"github.com/lsegal/aviary/internal/store"
)

// Watch is a set of targets watched for the life of the server without being
// saved to aviary.yaml, as `aviary watch` starts them. Settings overrides the
// shared projects settings for the whole engine while the watch runs; only
// its non-zero fields apply.
type Watch struct {
	Targets []string `json:"targets"`
	// Agent is the Aviary agent the targets run under for the watch. Empty
	// keeps a target's owning agent, or models.defaults for a target no agent
	// owns.
	Agent    string                `json:"agent,omitempty"`
	Settings config.ProjectsConfig `json:"settings,omitzero"`
	// Filters overrides the issue search filter of single targets of the
	// watch, one entry per term group, as a saved project's filter does.
	Filters map[string][]string `json:"filters,omitempty"`
	// Modes sets the mode of single targets of the watch, "watch" or
	// "manual", as a saved project's mode does.
	Modes map[string]string `json:"modes,omitempty"`
	// FinishedTTLs overrides finished_ttl for single targets of the watch,
	// as a saved project's finished_ttl does.
	FinishedTTLs map[string]string `json:"finished_ttls,omitempty"`
}

// Manager owns the Code Loop engine inside the Aviary server. It builds the
// engine from the projects every agent in aviary.yaml owns plus any runtime
// watches,
// applies changed settings to the running engine, and restarts it when a
// change needs a new one, such as a new target.
type Manager struct {
	stateDir string

	// refreshMu serializes refreshes, so two config changes never start two
	// engines.
	refreshMu sync.Mutex
	mu        sync.Mutex
	// cfg is the shared projects settings, list is every agent's projects,
	// and owners maps each saved target to the agent that owns it.
	cfg     config.ProjectsConfig
	list    []config.ProjectConfig
	owners  map[string]string
	browser config.BrowserConfig
	// agents holds the Aviary agents and models: section; a project's jobs
	// take their specs from its owning agent.
	agents    *config.Config
	watches   []Watch
	running   *engineRun
	lastKey   string
	lastError string
	stopped   bool
	// engineOff keeps the manager tracking settings without starting an
	// engine; see SetEngineEnabled.
	engineOff bool
	// held is a safe restart's hold: no engine starts, and a running engine
	// dispatches nothing new, while it is set. The engine reads it from its
	// own goroutine, so it is not guarded by mu.
	held atomic.Bool
	// noticeMu guards restartNotices, the notice each job a safe restart
	// stopped is resumed with, keyed by OWNER/REPO#N; see ResumeAfterRestart.
	noticeMu       sync.Mutex
	restartNotices map[string]string

	identity    Identity
	transcripts *transcriptStore

	// logSource pages core's log ring for project_status; see SetLogSource.
	logSource atomic.Pointer[LogSource]

	snapshot atomic.Pointer[core.Snapshot]
	// jobs is the durable store of every job Code Loop has seen, which the
	// read APIs show beside the live snapshot; see jobStore. lastSweep is
	// when its expired jobs were last removed, as Unix nanoseconds.
	jobs      *jobStore
	lastSweep atomic.Int64
	reports   *reporter
	quotas    atomic.Pointer[map[string]string]

	// loginRetry is the pending retry of an engine start whose gh login
	// lookup failed, and loginRetryDelay the wait before the next one.
	loginRetry      *time.Timer
	loginRetryDelay time.Duration
	// ghRunCommand replaces the engine's gh calls in tests.
	ghRunCommand func(context.Context, ...string) ([]byte, error)
	// sites holds the hosts project_site_fetch may read, and siteClient
	// replaces its HTTP client in tests.
	sites      siteHostCache
	siteClient *http.Client
	// mentionRouter prompts the built-in Code Loop agent; see
	// SetMentionRouter.
	mentionRouter atomic.Pointer[MentionRouter]
	// aviaryRunner runs in-process agent definitions; see
	// SetAviaryAgentRunner.
	aviaryRunner atomic.Pointer[AviaryAgentRunner]
	// usageRecorder receives each job run's token use; see
	// SetUsageRecorder.
	usageRecorder atomic.Pointer[UsageRecorder]
	// scheduleSource lists core's scheduled tasks; see SetScheduleSource.
	scheduleSource atomic.Pointer[ScheduleSource]
	// projectObserver receives project events, and projectStates is what
	// it last heard of each target; see SetProjectObserver.
	projectObserver atomic.Pointer[ProjectObserver]
	projectStates   projectStates
}

// ScheduleSource lists core's scheduled tasks with their next fire and run
// history, as the scheduler's ListTasks does.
type ScheduleSource func() []domain.ScheduledTask

// SetScheduleSource makes job lists and summaries report each schedule's
// next run from list.
func (m *Manager) SetScheduleSource(list ScheduleSource) {
	m.scheduleSource.Store(&list)
}

// scheduledTasks lists core's scheduled tasks by ID, or nil without a
// schedule source.
func (m *Manager) scheduledTasks() map[string]domain.ScheduledTask {
	list := m.scheduleSource.Load()
	if list == nil || *list == nil {
		return nil
	}
	tasks := map[string]domain.ScheduledTask{}
	for _, task := range (*list)() {
		tasks[task.ID] = task
	}
	return tasks
}

// MentionRouter sends a prompt to the built-in Code Loop agent.
type MentionRouter func(ctx context.Context, prompt string) error

// SetMentionRouter makes engines started from now on send direct mentions on
// finished or paused jobs to the Code Loop agent through route, which must not
// block on the agent's answer.
func (m *Manager) SetMentionRouter(route MentionRouter) {
	m.mentionRouter.Store(&route)
}

// SetUsageRecorder makes engines started from now on report each agent run's
// token use, each job run's wall time, and each issue's merged or failed
// outcome to record, so Code Loop jobs count toward core usage tracking.
func (m *Manager) SetUsageRecorder(record UsageRecorder) {
	m.usageRecorder.Store(&record)
}

// The backoff between engine starts that failed to resolve the authenticated
// gh login, which a rate limit or network error fails transiently.
var (
	loginRetryInitial = 5 * time.Second
	loginRetryMax     = 5 * time.Minute
)

// errLoginLookup marks an engine start that failed resolving the
// authenticated gh login; the manager retries it with backoff.
var errLoginLookup = errors.New("resolve the authenticated gh user for allowed_commenters")

// engineRun is one running engine and what it needs to shut down.
type engineRun struct {
	engine       *Engine
	settings     engineSettings
	cancel       context.CancelFunc
	done         chan struct{}
	checkpointer *shutdownCheckpointer
	cleanup      []func()
	// signIn and driver are browser delivery's GitHub sign-in and browser,
	// nil in other modes.
	signIn *browser.SignInRecovery
	driver browser.Handover
	// filters are the per-target filters the engine reads, updated live.
	filters *core.FilterOverrides
	// manual is the set of manual-mode targets the engine reads, updated
	// live.
	manual *core.ManualTargets
}

// NewManager returns a manager that keeps its work state and job transcripts
// under stateDir.
func NewManager(stateDir string) (*Manager, error) {
	identity, err := newIdentity()
	if err != nil {
		return nil, err
	}
	m := &Manager{
		stateDir:    stateDir,
		identity:    identity,
		transcripts: newTranscriptStore(filepath.Join(stateDir, "jobs")),
		jobs:        newJobStore(filepath.Join(stateDir, "jobs.json")),
		reports:     newReporter(),
	}
	// A job cleared or expired from the store takes its transcript with it.
	m.jobs.onRemove = func(keys []string) {
		for _, key := range keys {
			m.transcripts.remove(key)
		}
	}
	return m, nil
}

// Identity is this server's instance identity, as signed into its GitHub
// comments.
func (m *Manager) Identity() string { return string(m.identity) }

// statePath is the engine's work state file.
func (m *Manager) statePath() string { return filepath.Join(m.stateDir, "state.json") }

// SetEngineEnabled turns starting the engine on or off. With it off the
// manager still tracks projects and settings, which lets callers exercise the
// API without reaching GitHub. It is on by default.
func (m *Manager) SetEngineEnabled(enabled bool) {
	m.mu.Lock()
	m.engineOff = !enabled
	m.mu.Unlock()
}

// SetBrowser records Aviary's browser settings, which browser delivery reads
// GitHub through. It takes effect on the next Reconcile.
func (m *Manager) SetBrowser(cfg config.BrowserConfig) {
	m.mu.Lock()
	m.browser = cfg
	m.mu.Unlock()
}

// generalApprovalLocked is the general approval mode of the last
// reconciled config, which jobs follow when Code Loop has no override.
func (m *Manager) generalApprovalLocked() clidefs.ApprovalMode {
	if m.agents == nil {
		return ""
	}
	return m.agents.Approval.Mode
}

// Reconcile applies a newly loaded configuration: the agents, the projects
// each one owns, the models: section, and the approval: section. A
// project's jobs run on projects.model and projects.fallbacks when model is
// set, else on its owning agent's model and fallbacks.
func (m *Manager) Reconcile(cfg *config.Config) {
	m.applyConfig(cfg)
	_ = m.refresh()
}

// ReconcileInBackground applies cfg as Reconcile does, but brings the engine
// in line with it in the background: a new project is in the watched targets,
// and shows in project_status as loading, as soon as it returns, while the
// engine restarts onto it and polls it without holding up the caller.
func (m *Manager) ReconcileInBackground(cfg *config.Config) {
	m.applyConfig(cfg)
	go m.refreshInBackground()
}

func (m *Manager) applyConfig(cfg *config.Config) {
	list := cfg.ProjectList()
	m.reports.configure(list)
	m.changeTargets(func() {
		m.agents = &config.Config{Agents: slices.Clone(cfg.Agents), Models: cfg.Models, Approval: cfg.Approval}
		m.cfg = cfg.Projects
		m.list = list
		m.owners = cfg.ProjectOwners()
	})
}

// refreshInBackground is refresh for a goroutine: refresh logs its own
// failures and records them for project_status.
func (m *Manager) refreshInBackground() { _ = m.refresh() }

// AddWatch starts watching targets until RemoveWatch or the server stops.
func (m *Manager) AddWatch(watch Watch) error {
	if err := m.addWatch(watch); err != nil {
		return err
	}
	return m.refresh()
}

// AddWatchInBackground adds watch as AddWatch does, returning once its
// targets are watched; the engine restarts onto them in the background.
func (m *Manager) AddWatchInBackground(watch Watch) error {
	if err := m.addWatch(watch); err != nil {
		return err
	}
	go m.refreshInBackground()
	return nil
}

// addWatch validates watch and records it, without touching the engine. A
// watch whose settings the engine would reject is not recorded.
func (m *Manager) addWatch(watch Watch) error {
	if len(watch.Targets) == 0 {
		return errors.New("a watch needs at least one target")
	}
	for i, target := range watch.Targets {
		target = strings.TrimSpace(target)
		if _, err := parseTarget(target); err != nil {
			return err
		}
		watch.Targets[i] = target
	}
	var err error
	m.changeTargets(func() {
		m.watches = append(m.watches, watch)
		if _, err = m.settingsLocked(); err != nil {
			m.watches = m.watches[:len(m.watches)-1]
		}
	})
	return err
}

// RemoveWatch stops runtime watches of target. It reports whether one was
// removed.
func (m *Manager) RemoveWatch(target string) bool {
	removed := m.removeWatch(target)
	if removed {
		_ = m.refresh()
	}
	return removed
}

// RemoveWatchInBackground removes target's runtime watches as RemoveWatch
// does, stopping its jobs in the background.
func (m *Manager) RemoveWatchInBackground(target string) bool {
	removed := m.removeWatch(target)
	if removed {
		go m.refreshInBackground()
	}
	return removed
}

func (m *Manager) removeWatch(target string) bool {
	removed := false
	m.changeTargets(func() {
		kept := m.watches[:0]
		for _, watch := range m.watches {
			watch.Targets = slices.DeleteFunc(watch.Targets, func(t string) bool {
				if t == target {
					removed = true
					return true
				}
				return false
			})
			if len(watch.Targets) > 0 {
				kept = append(kept, watch)
			}
		}
		m.watches = kept
	})
	return removed
}

// Shutdown checkpoints every running job, committing and pushing its work,
// then stops the engine and every subprocess it started. It is safe to call
// more than once.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	m.stopped = true
	m.cancelLoginRetryLocked()
	run := m.running
	m.running = nil
	m.mu.Unlock()
	m.stopRun(run)
	m.jobs.close()
	process.ReapAll()
}

// engineSettings is everything the engine is built from.
type engineSettings struct {
	Targets      []string            `json:"targets"`
	TargetAgents map[string][]string `json:"target_agents"`
	Agents       []string            `json:"agents"`
	// Definitions are core's cli_definitions when the settings were built, so
	// a change to them restarts the run on the new CLI providers.
	Definitions map[string]any `json:"definitions"`
	Interval    time.Duration  `json:"interval"`
	Delivery    string         `json:"delivery"`
	ReadyState  string         `json:"ready_state"`
	Filter      string         `json:"filter"`
	// TargetFilters are the targets with their own filter override, which
	// replaces Filter and AllIssues for them.
	TargetFilters map[string]string `json:"-"`
	// ManualTargets are the targets in manual mode, which are never
	// searched for issues.
	ManualTargets []string `json:"-"`
	// DefaultMode is the shared mode of the projects without their own.
	DefaultMode       string               `json:"-"`
	AllIssues         bool                 `json:"all_issues"`
	Approval          clidefs.ApprovalMode `json:"approval"`
	Webhook           config.ProjectsWebhookConfig
	Browser           BrowserOptions `json:"browser"`
	Concurrency       int            `json:"-"`
	AllowedCommenters []string       `json:"-"`
	NoMerge           bool           `json:"-"`
	NoChangelog       bool           `json:"-"`
	UnblockInterval   time.Duration  `json:"-"`
	UnblockAgent      string         `json:"-"`
	UnblockModel      string         `json:"-"`
	TempDirs          []string       `json:"-"`
}

// restartKey is the part of the settings a running engine cannot change; the
// fields tagged "-" are applied live.
func (s engineSettings) restartKey() string {
	data, _ := json.Marshal(s)
	return string(data)
}

// settingsLocked resolves the engine settings from the config and watches.
func (m *Manager) settingsLocked() (engineSettings, error) {
	cfg := m.effectiveSettingsLocked()
	targets := config.AgentProjectsConfig(m.list).Targets()
	watchAgents := map[string]string{}
	for _, watch := range m.watches {
		for _, target := range watch.Targets {
			if !slices.Contains(targets, target) {
				targets = append(targets, target)
			}
			if agent := strings.TrimSpace(watch.Agent); agent != "" {
				watchAgents[target] = agent
			}
		}
	}
	targetAgents, defaultSpecs, err := m.jobSpecsLocked(cfg, targets, watchAgents)
	if err != nil {
		return engineSettings{}, err
	}
	s := engineSettings{
		Targets:           targets,
		TargetAgents:      targetAgents,
		Agents:            defaultSpecs,
		Definitions:       llm.CLIProviderDefinitions(),
		Interval:          cfg.EffectiveInterval(),
		Delivery:          cfg.EffectiveDelivery(),
		ReadyState:        strings.TrimSpace(cfg.ReadyState),
		Filter:            cfg.EffectiveFilter(),
		TargetFilters:     m.targetFiltersLocked(targets),
		ManualTargets:     m.manualTargetsLocked(targets),
		DefaultMode:       cfg.EffectiveMode(),
		AllIssues:         cfg.AllIssues,
		Approval:          cfg.EffectiveApprovalMode(m.generalApprovalLocked()),
		Concurrency:       cfg.EffectiveConcurrency(),
		AllowedCommenters: cfg.AllowedCommenters,
		NoMerge:           cfg.NoMerge,
		NoChangelog:       cfg.NoChangelog,
		UnblockInterval:   defaultUnblockInterval,
		UnblockAgent:      strings.TrimSpace(cfg.UnblockAgent),
		UnblockModel:      strings.TrimSpace(cfg.UnblockModel),
		TempDirs:          cfg.TempDirs,
	}
	if cfg.Webhook != nil {
		s.Webhook = *cfg.Webhook
	}
	if s.Delivery == DeliveryBrowser {
		s.Browser = BrowserOptionsFrom(m.browser)
		s.Browser.Vision, s.Browser.Headed = cfg.BrowserVision, cfg.BrowserVisible
		if _, _, err := ResolveDelivery(s.Delivery, s.Interval, true, s.Browser); err != nil {
			return s, fmt.Errorf("projects: %w", err)
		}
	}
	if s.Webhook.Path == "" {
		s.Webhook.Path = config.DefaultProjectsWebhookPath
	}
	if s.Webhook.Listen == "" {
		s.Webhook.Listen = ":0"
	}
	if s.Webhook.NgrokBinary == "" {
		s.Webhook.NgrokBinary = ngrok.DefaultBinary
	}
	if strings.TrimSpace(cfg.UnblockInterval) != "" {
		interval, err := parseUnblockInterval(cfg.UnblockInterval)
		if err != nil {
			return s, fmt.Errorf("projects.unblock_interval: %w", err)
		}
		s.UnblockInterval = interval
	}
	return s, nil
}

// targetFiltersLocked maps each of targets with a filter override to it: a
// runtime watch's override, else the saved project's.
func (m *Manager) targetFiltersLocked(targets []string) map[string]string {
	saved := config.AgentProjectsConfig(m.list).Filters()
	filters := map[string]string{}
	for _, target := range targets {
		if filter := saved[target]; filter != "" {
			filters[target] = filter
		}
		for _, watch := range m.watches {
			if filter := config.JoinFilter(watch.Filters[target]); filter != "" && slices.Contains(watch.Targets, target) {
				filters[target] = filter
			}
		}
	}
	return filters
}

// projectModeLocked returns target's effective mode and its source: a
// runtime watch's override, else the saved project's, else the shared mode.
func (m *Manager) projectModeLocked(target string, saved map[string]string, global config.ProjectsConfig) (mode, source string) {
	override := saved[target]
	for _, watch := range m.watches {
		if mode, ok := watch.Modes[target]; ok && slices.Contains(watch.Targets, target) {
			override = mode
		}
	}
	return global.ModeFor(override)
}

// projectModesLocked maps every one of targets to its effective mode and to
// that mode's source, "project" or "global".
func (m *Manager) projectModesLocked(targets []string) (modes, sources map[string]string) {
	saved := config.AgentProjectsConfig(m.list).Modes()
	global := m.effectiveSettingsLocked()
	modes = make(map[string]string, len(targets))
	sources = make(map[string]string, len(targets))
	for _, target := range targets {
		modes[target], sources[target] = m.projectModeLocked(target, saved, global)
	}
	return modes, sources
}

// manualTargetsLocked lists the targets whose effective mode is manual.
func (m *Manager) manualTargetsLocked(targets []string) []string {
	modes, _ := m.projectModesLocked(targets)
	var manual []string
	for _, target := range targets {
		if modes[target] == config.ProjectModeManual {
			manual = append(manual, target)
		}
	}
	return manual
}

// ProjectMode returns target's effective mode, "watch" or "manual", and its
// source: "project" for the project's own override, else "global".
func (m *Manager) ProjectMode(target string) (mode, source string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	modes, sources := m.projectModesLocked([]string{target})
	return modes[target], sources[target]
}

// ProjectModes maps every one of targets to its effective mode and to that
// mode's source.
func (m *Manager) ProjectModes(targets []string) (modes, sources map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.projectModesLocked(targets)
}

// SetWatchMode sets or, with an empty mode or "inherit", clears the mode
// override of target's runtime watch. It reports whether target has a
// runtime watch.
func (m *Manager) SetWatchMode(target, mode string) (bool, error) {
	mode, err := config.NormalizeProjectModeOverride(mode)
	if err != nil {
		return false, err
	}
	m.mu.Lock()
	found := false
	for i := range m.watches {
		if !slices.Contains(m.watches[i].Targets, target) {
			continue
		}
		found = true
		modes := maps.Clone(m.watches[i].Modes)
		if modes == nil {
			modes = map[string]string{}
		}
		if mode == "" {
			delete(modes, target)
		} else {
			modes[target] = mode
		}
		m.watches[i].Modes = modes
	}
	m.mu.Unlock()
	if !found {
		return false, nil
	}
	return true, m.refresh()
}

// SetWatchFinishedTTL sets or, with an empty value or "inherit", clears the
// finished_ttl override of target's runtime watch. It reports whether target
// has a runtime watch.
func (m *Manager) SetWatchFinishedTTL(target, ttl string) (bool, error) {
	ttl, err := config.NormalizeFinishedTTLOverride(ttl)
	if err != nil {
		return false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	found := false
	for i := range m.watches {
		if !slices.Contains(m.watches[i].Targets, target) {
			continue
		}
		found = true
		ttls := maps.Clone(m.watches[i].FinishedTTLs)
		if ttls == nil {
			ttls = map[string]string{}
		}
		if ttl == "" {
			delete(ttls, target)
		} else {
			ttls[target] = ttl
		}
		m.watches[i].FinishedTTLs = ttls
	}
	return found, nil
}

// SetWatchFilter sets or, with an empty filter, clears the filter override
// of target's runtime watch. It reports whether target has a runtime watch.
func (m *Manager) SetWatchFilter(target string, filter []string) (bool, error) {
	m.mu.Lock()
	found := false
	for i := range m.watches {
		if !slices.Contains(m.watches[i].Targets, target) {
			continue
		}
		found = true
		filters := maps.Clone(m.watches[i].Filters)
		if filters == nil {
			filters = map[string][]string{}
		}
		if config.JoinFilter(filter) == "" {
			delete(filters, target)
		} else {
			filters[target] = slices.Clone(filter)
		}
		m.watches[i].Filters = filters
	}
	m.mu.Unlock()
	if !found {
		return false, nil
	}
	return true, m.refresh()
}

// jobSpecsLocked resolves each target's job specs, in fallback order, from
// the settings' model override or else the Aviary agent that owns it, and
// the spec of jobs no target owns: the first target's first.
func (m *Manager) jobSpecsLocked(settings config.ProjectsConfig, targets []string, watchAgents map[string]string) (map[string][]string, []string, error) {
	agents := m.modelConfigLocked(settings)
	targetAgents := map[string][]string{}
	for _, target := range targets {
		name := watchAgents[target]
		if name == "" {
			name = m.owners[target]
		}
		if name == "" {
			// Like any Aviary agent without a model, a runtime watch no agent
			// owns takes models.defaults; only its CLI models can run a job.
			specs := config.ProjectJobSpecs(agents, "")
			if len(specs) == 0 {
				return nil, nil, fmt.Errorf("projects: %s is owned by no agent and models.defaults names no cli/ model; watch it with an agent", target)
			}
			targetAgents[target] = specs
			continue
		}
		specs := config.ProjectJobSpecs(agents, name)
		if len(specs) == 0 {
			return nil, nil, fmt.Errorf("projects: %s belongs to agent %q, which is not configured or has no model", target, name)
		}
		targetAgents[target] = specs
	}
	var defaultSpecs []string
	if len(targets) > 0 {
		defaultSpecs = targetAgents[targets[0]][:1]
	}
	return targetAgents, defaultSpecs, nil
}

// modelConfigLocked is the config job models resolve against: the agents,
// models: and approval: sections with settings, the projects settings the
// watches leave in force, so projects.model and projects.fallbacks apply.
func (m *Manager) modelConfigLocked(settings config.ProjectsConfig) *config.Config {
	agents := config.Config{}
	if m.agents != nil {
		agents = *m.agents
	}
	agents.Projects = settings
	return &agents
}

// effectiveSettingsLocked is the projects settings with every runtime
// watch's settings laid over them, as settingsLocked resolves them.
func (m *Manager) effectiveSettingsLocked() config.ProjectsConfig {
	cfg := m.cfg
	for _, watch := range m.watches {
		cfg = cfg.Override(watch.Settings)
	}
	return cfg
}

// ProjectModel is the model a project's jobs run on: the specs in fallback
// order and where they come from, "project override", "from agent <name>",
// or "from models.defaults".
type ProjectModel struct {
	Model     string   `json:"model"`
	Fallbacks []string `json:"fallbacks"`
	Source    string   `json:"source"`
}

// String renders the model as project status shows it, such as
// "claude/opus (project override)".
func (p ProjectModel) String() string {
	if p.Model == "" {
		return ""
	}
	return p.Model + " (" + p.Source + ")"
}

// ProjectModels returns the effective job model of every watched target.
func (m *Manager) ProjectModels() map[string]ProjectModel {
	m.mu.Lock()
	defer m.mu.Unlock()
	settings, err := m.settingsLocked()
	if err != nil {
		return map[string]ProjectModel{}
	}
	return m.projectModelsLocked(settings)
}

// projectModelsLocked returns the effective job model of every target of
// settings.
func (m *Manager) projectModelsLocked(settings engineSettings) map[string]ProjectModel {
	cfg := m.modelConfigLocked(m.effectiveSettingsLocked())
	models := make(map[string]ProjectModel, len(settings.Targets))
	for _, target := range settings.Targets {
		specs := settings.TargetAgents[target]
		model := ProjectModel{Fallbacks: []string{}, Source: config.ProjectModelSource(cfg, m.projectAgentLocked(target))}
		if len(specs) > 0 {
			model.Model, model.Fallbacks = specs[0], append(model.Fallbacks, specs[1:]...)
		}
		models[target] = model
	}
	return models
}

// runsOn reports whether a job running agent with model runs on one of the
// project model's specs.
func (p ProjectModel) runsOn(agent, model string) bool {
	for _, value := range append([]string{p.Model}, p.Fallbacks...) {
		spec, err := parseAgentSpec(value)
		if err == nil && spec.Name == agent && (model == "" || spec.Model == model) {
			return true
		}
	}
	return false
}

// refresh brings the running engine in line with the settings: nothing when
// they match, a live update when only live settings changed, and a restart
// otherwise.
func (m *Manager) refresh() error {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return nil
	}
	settings, err := m.settingsLocked()
	if err != nil {
		m.lastError = err.Error()
		m.mu.Unlock()
		m.logf("projects settings rejected: %v", err)
		return err
	}
	key := settings.restartKey()
	run := m.running
	if run != nil && key == m.lastKey {
		m.mu.Unlock()
		return m.applyLive(run, settings)
	}
	m.running = nil
	m.lastKey = key
	engineOff := m.engineOff || m.held.Load()
	if len(settings.Targets) == 0 || engineOff {
		m.cancelLoginRetryLocked()
	}
	m.mu.Unlock()
	if run != nil {
		m.logf("projects settings changed; restarting the engine (running jobs are checkpointed and resume)")
		m.stopRun(run)
	}
	if len(settings.Targets) == 0 || engineOff {
		m.setError("")
		m.snapshot.Store(nil)
		return nil
	}
	next, err := m.startRun(settings)
	if err != nil {
		if errors.Is(err, errLoginLookup) {
			m.retryLoginLater(err)
			return err
		}
		m.setError(err.Error())
		m.logf("projects engine failed to start: %v", err)
		return err
	}
	m.mu.Lock()
	m.cancelLoginRetryLocked()
	if m.stopped || m.running != nil {
		m.mu.Unlock()
		m.stopRun(next)
		return nil
	}
	m.running = next
	m.lastError = ""
	m.mu.Unlock()
	return nil
}

// retryLoginLater schedules another engine start after the gh login lookup
// failed, doubling the wait each time up to loginRetryMax.
func (m *Manager) retryLoginLater(err error) {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	delay := m.loginRetryDelay
	if delay <= 0 {
		delay = loginRetryInitial
	}
	m.loginRetryDelay = min(2*delay, loginRetryMax)
	if m.loginRetry != nil {
		m.loginRetry.Stop()
	}
	m.loginRetry = time.AfterFunc(delay, func() { _ = m.refresh() })
	m.lastError = fmt.Sprintf("waiting %s to retry starting the engine: %v", delay, err)
	m.mu.Unlock()
	m.logf("projects engine waiting %s to retry starting: %v", delay, err)
}

// cancelLoginRetryLocked drops a pending login retry and resets its backoff.
// m.mu must be held.
func (m *Manager) cancelLoginRetryLocked() {
	if m.loginRetry != nil {
		m.loginRetry.Stop()
		m.loginRetry = nil
	}
	m.loginRetryDelay = 0
}

func (m *Manager) setError(text string) {
	m.mu.Lock()
	m.lastError = text
	m.mu.Unlock()
}

// applyLive pushes the settings a running engine accepts without a restart.
func (m *Manager) applyLive(run *engineRun, s engineSettings) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	interval := unblockIntervalText(s.UnblockInterval)
	update := SettingsUpdate{
		Concurrency:     &s.Concurrency,
		NoMerge:         &s.NoMerge,
		NoChangelog:     &s.NoChangelog,
		TempDirs:        &s.TempDirs,
		UnblockInterval: &interval,
		UnblockAgent:    &s.UnblockAgent,
		UnblockModel:    &s.UnblockModel,
	}
	if len(s.AllowedCommenters) > 0 {
		update.AllowedCommenters = &s.AllowedCommenters
	}
	run.filters.Set(s.TargetFilters)
	// A project switched back to watch mode is searched for work at once.
	if left := run.manual.Set(s.ManualTargets); len(left) > 0 {
		m.logf("%s switched to watch mode; checking for issues now", strings.Join(left, ", "))
		run.engine.nudgePoll()
	}
	if _, err := run.engine.ApplySettings(ctx, update); err != nil {
		m.logf("projects settings not applied: %v", err)
		return err
	}
	return nil
}

// startRun builds and starts an engine.
func (m *Manager) startRun(s engineSettings) (*engineRun, error) {
	registry, err := llm.CLIProviderRegistry()
	if err != nil {
		return nil, err
	}
	for _, spec := range allAgentSpecs(s) {
		parsed, err := parseAgentSpecIn(registry, spec)
		if err != nil {
			return nil, fmt.Errorf("agent %q: %w", spec, err)
		}
		if parsed.Name == aviaryAgent && parsed.Model == "" {
			return nil, fmt.Errorf("agent %q: name the Aviary agent to run, as aviary/<agent>", spec)
		}
	}
	unblock := unblockConfig{Agent: s.UnblockAgent, Model: s.UnblockModel}
	if err := validateUnblockAgent(registry, unblock); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &engineRun{settings: s, cancel: cancel, done: make(chan struct{}), filters: core.NewFilterOverrides(s.TargetFilters), manual: core.NewManualTargets(s.ManualTargets)}
	fail := func(err error) (*engineRun, error) {
		cancel()
		for _, cleanup := range run.cleanup {
			cleanup()
		}
		return nil, err
	}
	gh := GHCLI{Binary: "gh", ReadyState: s.ReadyState, Filter: s.Filter, AllIssues: s.AllIssues, FilterOverrides: run.filters, publicRepoCache: &sync.Map{}, selfLoginCache: &sync.Map{}, runCommand: m.ghRunCommand}
	allowed := s.AllowedCommenters
	if len(allowed) == 0 {
		login, err := gh.resolveSelfLogin(ctx)
		if err != nil {
			return fail(fmt.Errorf("%w: %w", errLoginLookup, err))
		}
		allowed = []string{login}
	}
	selfLogin, err := gh.resolveSelfLogin(ctx)
	if err != nil {
		m.logf("resolve authenticated gh user: %v; comments mentioning it will not trigger rechecks", err)
	}
	active := &activeAgents{}
	run.checkpointer = newShutdownCheckpointer(active, m.logf)
	var agentCursor *atomic.Uint64
	if len(s.Agents) > 1 {
		agentCursor = &atomic.Uint64{}
	}
	var engine *Engine
	runner := CommandRunner{
		Agents:            s.Agents,
		Agent:             s.Agents[0],
		TargetAgents:      s.TargetAgents,
		Repo:              s.Targets[0],
		Identity:          m.identity,
		Approval:          s.Approval,
		NoMerge:           s.NoMerge,
		NoChangelog:       s.NoChangelog,
		Definitions:       registry,
		AllowedCommenters: allowed,
		agentCursor:       agentCursor,
		Active:            active,
		Transcripts:       m.transcripts,
		CloneRoot:         func() (string, error) { return engine.CloneRoot() },
		Quotas:            m.latestQuotas,
	}
	if aviary := m.aviaryRunner.Load(); aviary != nil {
		runner.Aviary = *aviary
	}
	if record := m.usageRecorder.Load(); record != nil {
		runner.RecordUsage = *record
	}
	quota := clidoctor.CombinedQuota(clidoctor.QuotaReaders(registry, quotaAgentNames(s), runner.binary))
	events := make(chan WebhookEvent, 1)
	engine = &Engine{
		Repo:              s.Targets[0],
		Targets:           s.Targets,
		Manual:            run.manual,
		Interval:          s.Interval,
		UseWebhooks:       s.Delivery == "webhook",
		Events:            events,
		Concurrency:       s.Concurrency,
		StatePath:         m.statePath(),
		ReadyState:        s.ReadyState,
		Issues:            gh,
		Discussions:       gh,
		Candidates:        gh,
		Status:            gh,
		Comments:          gh,
		Mentions:          gh,
		Projects:          gh,
		Identity:          m.identity,
		AllowedCommenters: allowed,
		SelfLogin:         selfLogin,
		UI:                m,
		Quota: func(ctx context.Context) map[string]string {
			readings := quota(ctx)
			m.quotas.Store(&readings)
			return readings
		},
		Runner:        runner,
		Active:        active,
		Registry:      registry,
		Out:           io.Discard,
		Transcripts:   m.transcripts,
		Held:          m.heldForRestart,
		RestartNotice: m.restartNotice,
	}
	if route := m.mentionRouter.Load(); route != nil && *route != nil {
		engine.RouteMention = *route
	}
	engine.RecordUsage = runner.RecordUsage
	engine.noMerge.Store(s.NoMerge)
	engine.noChangelog.Store(s.NoChangelog)
	engine.SetTempDirs(s.TempDirs)
	engine.SetUnblockInterval(s.UnblockInterval)
	engine.SetUnblockAgent(unblock)
	run.engine = engine
	// Once a run's work is checkpointed, its issue's ownership is handed back
	// so the next instance can pick it up at once (issue #420).
	run.checkpointer.SetRescind(engine.rescindOwnership)
	if engine.UseWebhooks {
		if err := m.startWebhooks(ctx, run, gh, events); err != nil {
			return fail(err)
		}
	}
	if s.Delivery == DeliveryBrowser {
		if err := m.startBrowser(ctx, run, gh); err != nil {
			return fail(err)
		}
	}
	go func() {
		defer close(run.done)
		if err := engine.Run(ctx); err != nil && ctx.Err() == nil {
			m.setError(err.Error())
			m.logf("projects engine stopped: %v", err)
		}
	}()
	return run, nil
}

// startWebhooks starts the webhook listener, its ngrok tunnel, and the
// repository webhooks that point at it.
func (m *Manager) startWebhooks(ctx context.Context, run *engineRun, gh GHCLI, events chan WebhookEvent) error {
	s := run.settings.Webhook
	secret, err := resolveWebhookSecret(s.Secret)
	if err != nil {
		return err
	}
	listener, err := listenForWebhooks(s.Listen)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: WebhookHandler{Events: events, Secret: secret, WebhookPath: s.Path}, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			m.logf("webhook server: %v", err)
		}
	}()
	run.cleanup = append(run.cleanup, func() { _ = server.Close() })
	output := logWriter{logf: m.logf}
	listenAddr := listener.Addr().String()
	m.logf("webhook server listening on %s%s", listenAddr, s.Path)
	ngrokAddr := ngrokTargetAddr(listenAddr)
	tunnel, err := ngrok.Start(ctx, s.NgrokBinary, ngrokAddr, output)
	if err != nil {
		return err
	}
	supervisor := &tunnelSupervisor{tunnel: tunnel}
	run.cleanup = append(run.cleanup, func() { _ = supervisor.Close() })
	endpoint, err := ngrok.WebhookURL(tunnel.URL(), s.Path)
	if err != nil {
		return err
	}
	m.logf("ngrok tunnel ready at %s", tunnel.URL())
	for _, target := range run.settings.Targets {
		if _, err := gh.ConfigureWebhook(ctx, target, endpoint, secret); err != nil {
			if errors.Is(err, errProjectWebhookUnavailable) || errors.Is(err, errWebhookPartiallyConfigured) {
				m.logf("%v", err)
				continue
			}
			return err
		}
		m.logf("configured GitHub webhook for %s", target)
	}
	supervisor.webhookPath = s.Path
	supervisor.reconciler = newWebhookReconciler(gh, run.settings.Targets, endpoint, secret, m.logf)
	supervisor.start = func(ctx context.Context) (webhookTunnel, error) {
		tunnel, err := ngrok.Start(ctx, s.NgrokBinary, ngrokAddr, output)
		if err != nil {
			return nil, err
		}
		return tunnel, nil
	}
	supervisor.probe, supervisor.logf = probeTunnel, m.logf
	run.engine.Webhooks = supervisor.reconcile
	run.engine.WebhookOnline = supervisor.online
	return nil
}

// startBrowserDriver launches the browser browser delivery reads through. It
// is a variable so tests can run the manager without a browser.
var startBrowserDriver = browser.Start

// openWebhookAuthStore opens the credential store that auth:<key> webhook
// secrets resolve from; tests replace it.
var openWebhookAuthStore = func() (auth.Store, error) {
	return auth.NewFileStore(filepath.Join(store.SubDir(store.DirAuth), "credentials.json"))
}

// resolveWebhookSecret resolves an auth:<key> webhook secret through the
// credential store, the same way core resolves its other secrets. A plain
// value is returned unchanged.
func resolveWebhookSecret(secret string) (string, error) {
	if !strings.HasPrefix(secret, "auth:") {
		return secret, nil
	}
	st, err := openWebhookAuthStore()
	if err != nil {
		return "", fmt.Errorf("projects webhook secret: %w", err)
	}
	resolved, err := auth.Resolve(st, secret)
	if err != nil {
		return "", fmt.Errorf("projects webhook secret: %w", err)
	}
	return resolved, nil
}

// startBrowser starts Aviary's browser for browser delivery and points the
// engine's GitHub reads at it.
func (m *Manager) startBrowser(ctx context.Context, run *engineRun, gh GHCLI) error {
	options := run.settings.Browser
	driver, err := startBrowserDriver(ctx, options.Config())
	if err != nil {
		return fmt.Errorf("start Aviary's browser for browser delivery: %w; set delivery: poll in an agent's projects settings to read GitHub through gh instead", err)
	}
	// The browser closes cleanly with the engine, including when Aviary exits.
	run.cleanup = append(run.cleanup, func() { _ = driver.Close() })
	run.signIn = ApplyBrowserSources(run.engine, driver, options, gh)
	run.driver = driver
	m.logf("browser delivery reading GitHub through Aviary's browser (profile %s)", describeProfile(options.Profile))
	return nil
}

// stopRun checkpoints and stops one engine, waiting a bounded time for it.
func (m *Manager) stopRun(run *engineRun) {
	if run == nil {
		return
	}
	run.checkpointer.Checkpoint()
	run.cancel()
	select {
	case <-run.done:
	case <-time.After(shutdownDrainBudget):
		m.logf("projects engine was still stopping after %s; stopping its processes and going ahead", shutdownDrainBudget)
	}
	for _, cleanup := range run.cleanup {
		cleanup()
	}
}

// aviaryAgent is the built-in definition that runs a job on an Aviary agent
// in-process: aviary/<agent> runs a turn on <agent> through core's agent
// runner.
const aviaryAgent = clidefs.Aviary

// allAgentSpecs lists every agent spec the settings may dispatch to.
func allAgentSpecs(s engineSettings) []string {
	specs := append([]string(nil), s.Agents...)
	for _, target := range s.Targets {
		for _, spec := range s.TargetAgents[target] {
			if !slices.Contains(specs, spec) {
				specs = append(specs, spec)
			}
		}
	}
	return specs
}

// quotaAgentNames lists the agent names whose quota is read.
func quotaAgentNames(s engineSettings) []string {
	var names []string
	for _, spec := range allAgentSpecs(s) {
		if name := agentProvider(spec); !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}

// StatusCLI is one CLI provider in project_status: the models its jobs run
// on, from the provider/model agent specs, and the provider's own quota. A
// provider/model pair is a model of its provider, not a second provider.
type StatusCLI struct {
	Name   string   `json:"name"`
	Models []string `json:"models"`
	Quota  string   `json:"quota"`
}

// statusCLIs lists each provider once: those the agent specs name, in spec
// order, then any other provider with a quota reading, by name.
func statusCLIs(specs []string, quotas map[string]string) []StatusCLI {
	clis := []StatusCLI{}
	index := map[string]int{}
	add := func(name string) int {
		if i, ok := index[name]; ok {
			return i
		}
		index[name] = len(clis)
		clis = append(clis, StatusCLI{Name: name, Models: []string{}, Quota: quotas[name]})
		return len(clis) - 1
	}
	for _, value := range specs {
		spec, err := parseAgentSpec(value)
		if err != nil {
			spec = agentSpec{Name: value}
		}
		name, model := spec.Name, spec.Model
		if name == "" {
			continue
		}
		i := add(name)
		if model != "" && !slices.Contains(clis[i].Models, model) {
			clis[i].Models = append(clis[i].Models, model)
		}
	}
	extra := make([]string, 0, len(quotas))
	for name := range quotas {
		if _, ok := index[name]; !ok && name != "" {
			extra = append(extra, name)
		}
	}
	slices.Sort(extra)
	for _, name := range extra {
		add(name)
	}
	return clis
}

func (m *Manager) latestQuotas() map[string]string {
	if readings := m.quotas.Load(); readings != nil {
		return *readings
	}
	return nil
}

// Snapshot receives the engine's published state.
func (m *Manager) Snapshot(snapshot core.Snapshot) {
	m.snapshot.Store(&snapshot)
	m.reports.observe(snapshot)
	m.jobs.observe(snapshot)
	m.sweepJobsInBackground()
	m.mu.Lock()
	owners := m.owners
	m.mu.Unlock()
	var events []ProjectEvent
	for _, target := range m.projectStates.changed(snapshot) {
		events = append(events, ProjectEvent{Type: ProjectUpdated, Target: target, Agent: owners[target]})
	}
	m.emit(events)
}

// SetReportDelivery sets how projects that opted in to reports send them
// through an agent's channels.
func (m *Manager) SetReportDelivery(deliver ReportDelivery) {
	m.reports.setDelivery(deliver)
}

// SetJobObserver sets what learns every job whose status moves to a new
// reportable event: failed, waiting or paused, or complete. Notifications
// use it.
func (m *Manager) SetJobObserver(observe JobObserver) {
	m.reports.setObserver(observe)
}

// logComponent is the slog component Code Loop logs under, which is how
// project_status picks its lines out of core's log ring.
const logComponent = "projects"

// LogSource pages core's log ring: the lines component logged after offset,
// oldest first, and the offset to ask from next time. An offset of zero or
// less asks for the latest limit lines; a limit of zero or less asks for every
// line.
type LogSource func(component string, offset, limit int) ([]string, int)

// SetLogSource sets where project_status reads Code Loop log lines from.
// Without one it reports none.
func (m *Manager) SetLogSource(source LogSource) {
	m.logSource.Store(&source)
}

// Log receives one engine log line.
func (m *Manager) Log(line string) {
	slog.Info(line, "component", logComponent)
}

func (m *Manager) logf(format string, args ...any) {
	m.Log(fmt.Sprintf(format, args...))
}

// logsSince returns the Code Loop log lines after offset, and the offset to ask
// from next time.
func (m *Manager) logsSince(offset, limit int) ([]string, int) {
	source := m.logSource.Load()
	if source == nil {
		return nil, offset
	}
	return (*source)(logComponent, offset, limit)
}

// engine returns the running engine, or nil.
func (m *Manager) engine() *Engine {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running == nil {
		return nil
	}
	return m.running.engine
}

// logWriter turns written output into log lines.
type logWriter struct {
	logf func(string, ...any)
}

func (w logWriter) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			w.logf("%s", line)
		}
	}
	return len(p), nil
}
