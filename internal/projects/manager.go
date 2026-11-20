package projects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lsegal/aviary/internal/auth"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/projects/agents"
	"github.com/lsegal/aviary/internal/projects/browser"
	"github.com/lsegal/aviary/internal/projects/core"
	"github.com/lsegal/aviary/internal/projects/ngrok"
	"github.com/lsegal/aviary/internal/projects/process"
	"github.com/lsegal/aviary/internal/store"
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

	// refreshMu serializes refreshes, so two config changes never start two
	// engines.
	refreshMu sync.Mutex
	mu        sync.Mutex
	cfg       config.ProjectsConfig
	browser   config.BrowserConfig
	// definitions are the coding-CLI definitions from the models: section.
	definitions map[string]any
	watches     []Watch
	running     *engineRun
	lastKey     string
	lastError   string
	stopped     bool
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

	// scheduleRerun fires a schedule again for a board Retry on one of its
	// jobs; see SetScheduleRerun.
	scheduleRerun atomic.Pointer[func(scheduleID string) error]

	snapshot atomic.Pointer[core.Snapshot]
	reports  *reporter
	quotas   atomic.Pointer[map[string]string]

	// loginRetry is the pending retry of an engine start whose gh login
	// lookup failed, and loginRetryDelay the wait before the next one.
	loginRetry      *time.Timer
	loginRetryDelay time.Duration
	// ghRunCommand replaces the engine's gh calls in tests.
	ghRunCommand func(context.Context, ...string) ([]byte, error)
	// mentionRouter prompts the built-in Projects agent; see
	// SetMentionRouter.
	mentionRouter atomic.Pointer[MentionRouter]
	// aviaryRunner runs in-process agent definitions; see
	// SetAviaryAgentRunner.
	aviaryRunner atomic.Pointer[AviaryAgentRunner]
	// usageRecorder receives each job run's token use; see
	// SetUsageRecorder.
	usageRecorder atomic.Pointer[UsageRecorder]
}

// MentionRouter sends a prompt to the built-in Projects agent.
type MentionRouter func(ctx context.Context, prompt string) error

// SetMentionRouter makes engines started from now on send direct mentions on
// finished or paused jobs to the Projects agent through route, which must not
// block on the agent's answer.
func (m *Manager) SetMentionRouter(route MentionRouter) {
	m.mentionRouter.Store(&route)
}

// SetUsageRecorder makes engines started from now on report each agent run's
// token use to record, so Projects jobs count toward core usage tracking.
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

// SetBrowser records Aviary's browser settings, which browser delivery reads
// GitHub through. It takes effect on the next Reconcile.
func (m *Manager) SetBrowser(cfg config.BrowserConfig) {
	m.mu.Lock()
	m.browser = cfg
	m.mu.Unlock()
}

// SetCLIDefinitions records the coding-CLI definitions from core Aviary's
// models: section, the one registry Projects reads its CLIs from. It takes
// effect on the next Reconcile.
func (m *Manager) SetCLIDefinitions(definitions map[string]any) {
	m.mu.Lock()
	m.definitions = definitions
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
	m.cancelLoginRetryLocked()
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
	merged.BrowserVision = merged.BrowserVision || overlay.BrowserVision
	merged.BrowserVisible = merged.BrowserVisible || overlay.BrowserVisible
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
		Definitions:       m.definitions,
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
	gh := GHCLI{Binary: "gh", ReadyState: s.ReadyState, ReadyLabel: s.ReadyLabel, Filter: s.Filter, AllIssues: s.AllIssues, publicRepoCache: &sync.Map{}, selfLoginCache: &sync.Map{}, runCommand: m.ghRunCommand}
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
	if aviary := m.aviaryRunner.Load(); aviary != nil {
		runner.Aviary = *aviary
	}
	if record := m.usageRecorder.Load(); record != nil {
		runner.RecordUsage = *record
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
	if route := m.mentionRouter.Load(); route != nil && *route != nil {
		engine.RouteMention = *route
	}
	engine.noMerge.Store(s.NoMerge)
	engine.noChangelog.Store(s.NoChangelog)
	engine.SetTempDirs(s.TempDirs)
	engine.SetUnblockInterval(s.UnblockInterval)
	engine.SetUnblockAgent(unblock)
	engine.SetScheduleRerun(m.rerunSchedule)
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
		return fmt.Errorf("start Aviary's browser for browser delivery: %w; set projects.delivery to poll to read GitHub through gh instead", err)
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
const aviaryAgent = "aviary"

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

// logComponent is the slog component Projects logs under, which is how
// project_status picks its lines out of core's log ring.
const logComponent = "projects"

// LogSource pages core's log ring: the lines component logged after offset,
// oldest first, and the offset to ask from next time. An offset of zero or
// less asks for the latest limit lines; a limit of zero or less asks for every
// line.
type LogSource func(component string, offset, limit int) ([]string, int)

// SetLogSource sets where project_status reads Projects log lines from.
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

// logsSince returns the Projects log lines after offset, and the offset to ask
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
