package projects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/projects/agents"
	"github.com/lsegal/aviary/internal/projects/core"
	"github.com/lsegal/aviary/internal/projects/ngrok"
	"github.com/lsegal/aviary/internal/projects/process"
)

// Watch is a set of targets watched for the life of the server without being
// saved to aviary.yaml, as `aviary watch` starts them. Settings overrides the
// projects: section's values for the whole engine while the watch runs; only
// its non-zero fields apply.
type Watch struct {
	Targets  []string              `json:"targets"`
	Agents   []string              `json:"agents,omitempty"`
	Settings config.ProjectsConfig `json:"settings,omitzero"`
}

// Manager owns the Projects engine inside the Aviary server. It builds the
// engine from the projects: section of aviary.yaml plus any runtime watches,
// applies changed settings to the running engine, and restarts it when a
// change needs a new one, such as a new target.
type Manager struct {
	stateDir string
	logger   *slog.Logger

	// refreshMu serializes refreshes, so two config changes never start two
	// engines.
	refreshMu sync.Mutex
	mu        sync.Mutex
	cfg       config.ProjectsConfig
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

	logMu    sync.Mutex
	logs     []string
	logTotal int

	snapshot atomic.Pointer[core.Snapshot]
	reports  *reporter
	quotas   atomic.Pointer[map[string]string]
}

// engineRun is one running engine and what it needs to shut down.
type engineRun struct {
	engine       *Engine
	settings     engineSettings
	cancel       context.CancelFunc
	done         chan struct{}
	checkpointer *shutdownCheckpointer
	cleanup      []func()
}

// managerLogLines is how many engine log lines the manager keeps for
// project_status.
const managerLogLines = 500

// NewManager returns a manager that keeps its work state and job transcripts
// under stateDir.
func NewManager(stateDir string) (*Manager, error) {
	identity, err := newIdentity()
	if err != nil {
		return nil, err
	}
	m := &Manager{
		stateDir:    stateDir,
		logger:      slog.Default().With("component", "projects"),
		identity:    identity,
		transcripts: newTranscriptStore(filepath.Join(stateDir, "jobs")),
		reports:     newReporter(),
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

// Reconcile applies a newly loaded projects: section.
func (m *Manager) Reconcile(cfg config.ProjectsConfig) {
	m.reports.configure(cfg)
	m.mu.Lock()
	m.cfg = cfg
	m.mu.Unlock()
	_ = m.refresh()
}

// AddWatch starts watching targets until RemoveWatch or the server stops.
func (m *Manager) AddWatch(watch Watch) error {
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
	m.mu.Lock()
	m.watches = append(m.watches, watch)
	m.mu.Unlock()
	return m.refresh()
}

// RemoveWatch stops runtime watches of target. It reports whether one was
// removed.
func (m *Manager) RemoveWatch(target string) bool {
	m.mu.Lock()
	removed := false
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
	m.mu.Unlock()
	if removed {
		_ = m.refresh()
	}
	return removed
}

// Shutdown checkpoints every running job, committing and pushing its work,
// then stops the engine and every subprocess it started. It is safe to call
// more than once.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	m.stopped = true
	run := m.running
	m.running = nil
	m.mu.Unlock()
	m.stopRun(run)
	process.ReapAll()
}

// engineSettings is everything the engine is built from.
type engineSettings struct {
	Targets           []string            `json:"targets"`
	TargetAgents      map[string][]string `json:"target_agents"`
	Agents            []string            `json:"agents"`
	AgentBinaries     map[string]string   `json:"agent_binaries"`
	Definitions       map[string]any      `json:"definitions"`
	Interval          time.Duration       `json:"interval"`
	Delivery          string              `json:"delivery"`
	ReadyLabel        string              `json:"ready_label"`
	ReadyState        string              `json:"ready_state"`
	Filter            string              `json:"filter"`
	AllIssues         bool                `json:"all_issues"`
	Yolo              bool                `json:"yolo"`
	Webhook           config.ProjectsWebhookConfig
	Concurrency       int           `json:"-"`
	AllowedCommenters []string      `json:"-"`
	NoMerge           bool          `json:"-"`
	NoChangelog       bool          `json:"-"`
	UnblockInterval   time.Duration `json:"-"`
	UnblockAgent      string        `json:"-"`
	UnblockModel      string        `json:"-"`
	TempDirs          []string      `json:"-"`
}

// restartKey is the part of the settings a running engine cannot change; the
// fields tagged "-" are applied live.
func (s engineSettings) restartKey() string {
	data, _ := json.Marshal(s)
	return string(data)
}

// mergeProjects lays overlay's non-zero values over base.
func mergeProjects(base, overlay config.ProjectsConfig) config.ProjectsConfig {
	merged := base
	if overlay.Concurrency > 0 {
		merged.Concurrency = overlay.Concurrency
	}
	if overlay.Interval != "" {
		merged.Interval = overlay.Interval
	}
	if overlay.Delivery != "" {
		merged.Delivery = overlay.Delivery
	}
	if overlay.ReadyLabel != nil {
		merged.ReadyLabel = overlay.ReadyLabel
	}
	if overlay.ReadyState != "" {
		merged.ReadyState = overlay.ReadyState
	}
	if len(overlay.Filter) > 0 {
		merged.Filter = overlay.Filter
	}
	merged.AllIssues = merged.AllIssues || overlay.AllIssues
	if len(overlay.AllowedCommenters) > 0 {
		merged.AllowedCommenters = overlay.AllowedCommenters
	}
	if len(overlay.Agents) > 0 {
		merged.Agents = overlay.Agents
	}
	merged.Yolo = merged.Yolo || overlay.Yolo
	merged.NoMerge = merged.NoMerge || overlay.NoMerge
	merged.NoChangelog = merged.NoChangelog || overlay.NoChangelog
	if overlay.UnblockInterval != "" {
		merged.UnblockInterval = overlay.UnblockInterval
	}
	if overlay.UnblockAgent != "" {
		merged.UnblockAgent = overlay.UnblockAgent
	}
	if overlay.UnblockModel != "" {
		merged.UnblockModel = overlay.UnblockModel
	}
	if len(overlay.TempDirs) > 0 {
		merged.TempDirs = overlay.TempDirs
	}
	if overlay.Webhook != nil {
		merged.Webhook = overlay.Webhook
	}
	return merged
}

// settingsLocked resolves the engine settings from the config and watches.
func (m *Manager) settingsLocked() (engineSettings, error) {
	cfg := m.cfg
	targets := cfg.Targets()
	targetAgents := map[string][]string{}
	for _, project := range cfg.List {
		if len(project.Agents) > 0 {
			targetAgents[strings.TrimSpace(project.Target)] = project.Agents
		}
	}
	for _, watch := range m.watches {
		cfg = mergeProjects(cfg, watch.Settings)
		for _, target := range watch.Targets {
			if !slices.Contains(targets, target) {
				targets = append(targets, target)
			}
			if len(watch.Agents) > 0 {
				targetAgents[target] = watch.Agents
			}
		}
	}
	s := engineSettings{
		Targets:           targets,
		TargetAgents:      targetAgents,
		Agents:            cfg.EffectiveAgents(),
		AgentBinaries:     cfg.AgentBinaries,
		Definitions:       cfg.CLIDefinitions,
		Interval:          cfg.EffectiveInterval(),
		Delivery:          cfg.EffectiveDelivery(),
		ReadyLabel:        cfg.EffectiveReadyLabel(),
		ReadyState:        strings.TrimSpace(cfg.ReadyState),
		Filter:            cfg.EffectiveFilter(),
		AllIssues:         cfg.AllIssues,
		Yolo:              cfg.Yolo,
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
		m.setError(err.Error())
		m.logf("projects engine failed to start: %v", err)
		return err
	}
	m.mu.Lock()
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
	if _, err := run.engine.ApplySettings(ctx, update); err != nil {
		m.logf("projects settings not applied: %v", err)
		return err
	}
	return nil
}

// startRun builds and starts an engine.
func (m *Manager) startRun(s engineSettings) (*engineRun, error) {
	registry, err := agents.Load(s.Definitions)
	if err != nil {
		return nil, err
	}
	setAgentRegistry(registry)
	for _, spec := range allAgentSpecs(s) {
		parsed, err := parseAgentSpecIn(registry, spec)
		if err != nil {
			return nil, fmt.Errorf("agent %q: %w", spec, err)
		}
		if parsed.Name == aviaryAgent && parsed.Model == "" {
			return nil, fmt.Errorf("agent %q: name the Aviary agent to run, as aviary/<agent>", spec)
		}
	}
	s.AgentBinaries = withAviaryBinary(s.AgentBinaries)
	unblock := unblockConfig{Agent: s.UnblockAgent, Model: s.UnblockModel}
	if err := validateUnblockAgent(registry, unblock); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &engineRun{settings: s, cancel: cancel, done: make(chan struct{})}
	fail := func(err error) (*engineRun, error) {
		cancel()
		for _, cleanup := range run.cleanup {
			cleanup()
		}
		return nil, err
	}
	gh := GHCLI{Binary: "gh", ReadyState: s.ReadyState, ReadyLabel: s.ReadyLabel, Filter: s.Filter, AllIssues: s.AllIssues, publicRepoCache: &sync.Map{}, selfLoginCache: &sync.Map{}}
	allowed := s.AllowedCommenters
	if len(allowed) == 0 {
		login, err := gh.resolveSelfLogin(ctx)
		if err != nil {
			return fail(fmt.Errorf("resolve the authenticated gh user for allowed_commenters: %w", err))
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
		AgentBinaries:     s.AgentBinaries,
		Agents:            s.Agents,
		Agent:             s.Agents[0],
		TargetAgents:      s.TargetAgents,
		Repo:              s.Targets[0],
		Identity:          m.identity,
		Yolo:              s.Yolo,
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
	quota := combinedQuotaReader(namedQuotaReaders(registry, quotaAgentNames(s), runner.binary))
	events := make(chan WebhookEvent, 1)
	engine = &Engine{
		Repo:              s.Targets[0],
		Targets:           s.Targets,
		Interval:          s.Interval,
		UseWebhooks:       s.Delivery == "webhook",
		Events:            events,
		Concurrency:       s.Concurrency,
		StatePath:         m.statePath(),
		ReadyState:        s.ReadyState,
		Issues:            gh,
		Discussions:       gh,
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
	engine.noMerge.Store(s.NoMerge)
	engine.noChangelog.Store(s.NoChangelog)
	engine.SetTempDirs(s.TempDirs)
	engine.SetUnblockInterval(s.UnblockInterval)
	engine.SetUnblockAgent(unblock)
	run.engine = engine
	if engine.UseWebhooks {
		if err := m.startWebhooks(ctx, run, gh, events); err != nil {
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
	listener, err := listenForWebhooks(s.Listen)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: WebhookHandler{Events: events, Secret: s.Secret, WebhookPath: s.Path}, ReadHeaderTimeout: 10 * time.Second}
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
		if _, err := gh.ConfigureWebhook(ctx, target, endpoint, s.Secret); err != nil {
			if errors.Is(err, errProjectWebhookUnavailable) || errors.Is(err, errWebhookPartiallyConfigured) {
				m.logf("%v", err)
				continue
			}
			return err
		}
		m.logf("configured GitHub webhook for %s", target)
	}
	supervisor.webhookPath = s.Path
	supervisor.reconciler = newWebhookReconciler(gh, run.settings.Targets, endpoint, s.Secret, m.logf)
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

// aviaryAgent is the built-in definition that runs a job on an Aviary agent:
// aviary/<agent> runs `aviary agent run <agent>` through this executable.
const aviaryAgent = "aviary"

// withAviaryBinary points the aviary definition at the running executable
// unless agent_binaries names another.
func withAviaryBinary(binaries map[string]string) map[string]string {
	if binaries[aviaryAgent] != "" {
		return binaries
	}
	executable, err := os.Executable()
	if err != nil {
		return binaries
	}
	merged := map[string]string{aviaryAgent: executable}
	for name, binary := range binaries {
		merged[name] = binary
	}
	return merged
}

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
}

// SetReportDelivery sets how projects that opted in to reports send them
// through an agent's channels.
func (m *Manager) SetReportDelivery(deliver ReportDelivery) {
	m.reports.setDelivery(deliver)
}

// Log receives one engine log line.
func (m *Manager) Log(line string) {
	m.logMu.Lock()
	m.logs = append(m.logs, line)
	if len(m.logs) > managerLogLines {
		m.logs = append([]string(nil), m.logs[len(m.logs)-managerLogLines:]...)
	}
	m.logTotal++
	m.logMu.Unlock()
	m.logger.Info(line)
}

func (m *Manager) logf(format string, args ...any) {
	m.Log(time.Now().Format("2006-01-02 15:04:05") + " " + fmt.Sprintf(format, args...))
}

// logsSince returns the log lines after the first offset ever logged, and the
// offset to ask from next time.
func (m *Manager) logsSince(offset, limit int) ([]string, int) {
	m.logMu.Lock()
	defer m.logMu.Unlock()
	first := m.logTotal - len(m.logs)
	start := max(offset-first, 0)
	if offset <= 0 {
		start = max(len(m.logs)-limit, 0)
	}
	lines := append([]string{}, m.logs[min(start, len(m.logs)):]...)
	if limit > 0 && len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines, m.logTotal
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
