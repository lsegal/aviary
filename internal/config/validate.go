package config

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/cliagent"
	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/schedule"
)

// IssueLevel indicates the severity of a validation finding.
type IssueLevel string

// IssueLevel values.
const (
	LevelError   IssueLevel = "ERROR"
	LevelWarning IssueLevel = "WARN"
)

// Issue is a single config validation finding.
type Issue struct {
	Level   IssueLevel
	Field   string
	Message string
}

// Validate checks cfg for errors and warnings and returns all findings.
// authGet, if non-nil, is called to look up credentials from the auth store
// (key is the part after "auth:", e.g. "anthropic:default" or "slack_bot_token").
func Validate(cfg *Config, authGet func(key string) (string, error)) []Issue {
	v := &validator{
		authGet:     authGet,
		checkedAuth: map[string]bool{},
		models:      cfg.Models,
	}
	v.checkServer(cfg.Server)
	v.checkAgents(cfg.Agents, cfg.Models)
	v.checkModels(cfg.Models)
	v.checkBrowser(cfg.Browser)
	v.checkScheduler(cfg.Scheduler)
	v.checkProjects(cfg.Agents)
	v.checkApproval(cfg.Approval, cfg.Agents)
	return v.issues
}

type validator struct {
	issues      []Issue
	authGet     func(string) (string, error)
	checkedAuth map[string]bool // tracks auth keys already reported to avoid duplicate errors
	models      ModelsConfig
}

func (v *validator) errorf(field, format string, args ...any) {
	v.issues = append(v.issues, Issue{LevelError, field, fmt.Sprintf(format, args...)})
}

func (v *validator) warnf(field, format string, args ...any) {
	v.issues = append(v.issues, Issue{LevelWarning, field, fmt.Sprintf(format, args...)})
}

// checkApproval validates the approval modes.
func (v *validator) checkApproval(a ApprovalConfig, agents []AgentConfig) {
	modes := map[string]clidefs.ApprovalMode{"approval.mode": a.Mode, "approval.chat": a.Chat}
	for i, agent := range agents {
		modes[fmt.Sprintf("agents[%d].projects.approval_mode", i)] = agent.Projects.ApprovalMode
	}
	for field, mode := range modes {
		if err := clidefs.ValidateApprovalMode(mode); err != nil {
			v.errorf(field, "%v", err)
		}
	}
}

// checkServer validates ServerConfig.
func (v *validator) checkServer(s ServerConfig) {
	if s.Port != 0 && (s.Port < 1 || s.Port > 65535) {
		v.errorf("server.port", "port %d is out of range; must be 1–65535", s.Port)
	}

	var certSet, keySet bool
	if s.TLS != nil {
		certSet = s.TLS.Cert != ""
		keySet = s.TLS.Key != ""
	}
	if certSet != keySet {
		v.errorf("server.tls", "tls.cert and tls.key must both be set or both be empty (cert set: %v, key set: %v)", certSet, keySet)
	}
	if certSet {
		if _, err := os.Stat(s.TLS.Cert); err != nil {
			v.errorf("server.tls.cert", "cannot read certificate file %q: %v", s.TLS.Cert, err)
		}
	}
	if keySet {
		if _, err := os.Stat(s.TLS.Key); err != nil {
			v.errorf("server.tls.key", "cannot read key file %q: %v", s.TLS.Key, err)
		}
	}
	if s.FailedTaskTimeout != "" {
		d, err := time.ParseDuration(s.FailedTaskTimeout)
		if err != nil {
			v.errorf("server.failed_task_timeout", "invalid duration %q: %v", s.FailedTaskTimeout, err)
		} else if d <= 0 {
			v.errorf("server.failed_task_timeout", "failed_task_timeout must be a positive duration")
		}
	}
}

// checkAgents validates each AgentConfig, including channels and tasks.
func (v *validator) checkAgents(agents []AgentConfig, models ModelsConfig) {
	names := map[string]int{}

	for i, a := range agents {
		f := fmt.Sprintf("agents[%d]", i)

		if a.Name == "" {
			v.errorf(f+".name", "agent name is required")
		} else {
			if prev, seen := names[a.Name]; seen {
				v.errorf(f+".name", "duplicate agent name %q (also defined at agents[%d])", a.Name, prev)
			} else {
				names[a.Name] = i
			}
			if strings.Contains(a.Name, "/") {
				v.errorf(f+".name", "agent name %q must not contain '/' (it is used as part of the scheduler task key)", a.Name)
			}
		}

		effectiveModel := a.Model
		if effectiveModel == "" && models.Defaults != nil {
			effectiveModel = models.Defaults.Model
		}
		if effectiveModel == "" {
			v.warnf(f+".model", "no model configured; agent will not respond to prompts")
		} else {
			v.checkModel(f+".model", effectiveModel)
		}
		if a.Permissions != nil && !IsValidPermissionsPreset(a.Permissions.Preset) {
			v.errorf(f+".permissions.preset", "invalid permissions preset %q; must be full, standard, or minimal", a.Permissions.Preset)
		}
		if a.Permissions != nil && a.Permissions.CLIBuiltinTools && EffectivePermissionsPreset(a.Permissions) != PermissionsPresetFull {
			v.errorf(f+".permissions.cli_builtin_tools", "cli_builtin_tools requires the full permissions preset")
		}
		if a.Permissions != nil && a.Permissions.Exec != nil && len(a.Permissions.Exec.AllowedCommands) == 0 {
			v.errorf(f+".permissions.exec.allowedCommands", "permissions.exec requires at least one allowedCommands entry")
		}

		for j, ch := range a.Channels {
			v.checkChannel(fmt.Sprintf("%s.channels[%d]", f, j), ch)
		}
		channelKeys := map[string]int{}
		for j, ch := range a.Channels {
			id := strings.TrimSpace(ch.ID)
			if id == "" || strings.TrimSpace(ch.Type) == "" {
				continue
			}
			key := ch.Type + ":" + id
			if prev, seen := channelKeys[key]; seen {
				v.errorf(fmt.Sprintf("%s.channels[%d].id", f, j), "duplicate channel id %q for type %q within agent %q (also at channels[%d])", id, ch.Type, a.Name, prev)
			} else {
				channelKeys[key] = j
			}
		}

		taskNames := map[string]int{}
		for j, t := range a.Tasks {
			tf := fmt.Sprintf("%s.tasks[%d]", f, j)

			if t.Name == "" {
				v.errorf(tf+".name", "task name is required")
			} else {
				if prev, seen := taskNames[t.Name]; seen {
					v.errorf(tf+".name", "duplicate task name %q within agent %q (also at tasks[%d])", t.Name, a.Name, prev)
				} else {
					taskNames[t.Name] = j
				}
			}

			fields := schedule.Fields{
				Trigger: t.Trigger, Schedule: t.Schedule, StartAt: t.StartAt, RunOnce: t.RunOnce,
				Every: t.Every, At: t.At, Days: t.Days, Timezone: t.Timezone,
				Overlap: t.Overlap, Missed: t.Missed,
			}
			if !fields.HasTrigger() && t.Watch == "" {
				v.warnf(tf, "neither a schedule trigger nor 'watch' is set; task will never be triggered")
			}

			taskType := strings.ToLower(strings.TrimSpace(t.Type))
			if taskType == "" {
				taskType = "prompt"
			}
			switch taskType {
			case "prompt", "script", "project":
			default:
				v.errorf(tf+".type", "invalid task type %q; must be \"prompt\", \"script\" or \"project\"", t.Type)
			}

			if fields.HasTrigger() {
				if t.Watch != "" {
					v.errorf(tf+".watch", "watch tasks cannot also have a schedule trigger")
				}
				if _, err := schedule.Parse(fields); err != nil {
					v.errorf(tf+".trigger", "%v (cron accepts standard 5-field cron or 6-field cron with leading seconds, e.g. \"* * * * *\")", err)
				}
			} else if t.RunOnce || t.StartAt != "" || t.Timezone != "" || t.Overlap != "" || t.Missed != "" {
				v.errorf(tf, "run_once, timezone, overlap and missed are only supported for scheduled tasks")
			}

			switch taskType {
			case "prompt":
				if strings.TrimSpace(t.Prompt) == "" {
					v.warnf(tf+".prompt", "prompt is empty; a blank message will be sent to the agent")
				}
			case "script":
				if strings.TrimSpace(t.Prompt) == "" {
					v.errorf(tf+".prompt", "script tasks require non-empty prompt content")
				}
			case "project":
				if strings.TrimSpace(t.Project) == "" {
					v.errorf(tf+".project", "project tasks require the watched project to run against")
				}
				if strings.TrimSpace(t.Prompt) == "" {
					v.errorf(tf+".prompt", "project tasks require non-empty prompt content")
				}
			}
			if taskType != "project" && (t.Project != "" || t.Model != "") {
				v.errorf(tf, "project and model are only supported for project tasks")
			}
			if model := strings.TrimSpace(t.Model); model != "" && taskType == "project" {
				v.checkProjectTaskModel(tf+".model", model)
			}

			if !validTaskTarget(t.Target) {
				v.errorf(tf+".target", "invalid value %q; must be empty, \"silent\", session:<name-or-id>, or <channel>:<configured-id>:<target>", t.Target)
			}
		}
	}
}

// ValidTaskTarget reports whether target is a valid task delivery target:
// empty, "silent", "session:<name-or-id>", or "<channel>:<configured-id>:<target>".
func ValidTaskTarget(target string) bool {
	return validTaskTarget(target)
}

func validTaskTarget(target string) bool {
	target = strings.TrimSpace(target)
	if target == "" || strings.EqualFold(target, "silent") {
		return true
	}
	if strings.HasPrefix(target, "session:") {
		return strings.TrimSpace(strings.TrimPrefix(target, "session:")) != ""
	}
	parts := strings.SplitN(target, ":", 3)
	return len(parts) == 3 && strings.TrimSpace(parts[0]) != "" && strings.TrimSpace(parts[1]) != "" && strings.TrimSpace(parts[2]) != ""
}

// checkModel validates a "<provider>/<name>" model string and checks for required credentials.
func (v *validator) checkModel(field, model string) {
	parts := strings.SplitN(model, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		v.errorf(field, "invalid model %q: expected format <provider>/<model-name> (e.g. \"anthropic/claude-sonnet-5-5\")", model)
		return
	}

	provider := parts[0]

	switch provider {
	case "anthropic":
		v.checkAuthCredentialEither(field, "anthropic:oauth", "anthropic:default",
			"run 'aviary auth set anthropic:default <your-api-key>' or 'aviary auth login anthropic'")
	case "openai":
		v.checkAuthCredential(field, "openai:default",
			"run 'aviary auth set openai:default <your-api-key>'")
	case "openai-codex":
		v.checkAuthCredential(field, "openai:oauth", "run 'aviary auth login openai'")
	case "google":
		v.checkAuthCredential(field, "gemini:default",
			"run 'aviary auth set gemini:default <your-api-key>'")
	case "google-gemini", "gemini-code-assist":
		v.checkAuthCredential(field, "gemini:oauth",
			"run 'aviary auth login gemini'")
	case "github-copilot":
		v.checkAuthCredentialEither(field, "github-copilot:oauth", "github-copilot:default",
			"run 'aviary auth login github-copilot' or set GH_TOKEN/GITHUB_TOKEN")
	case "meta":
		v.checkAuthCredential(field, "meta:default",
			"run 'aviary auth set meta:default <your-api-key>'")
	case "vllm", "ollama":
	case "cli":
		v.checkCLIModel(field, model)
	default:
		registry := cliRegistry(v.models)
		if _, ok := registry.Lookup(provider); ok {
			v.checkCLIModel(field, model)
			return
		}
		v.errorf(field, "%v", llm.UnknownProviderError(registry, provider, model))
	}
}

// checkProjectTaskModel validates a project task's model override as any
// model is checked, except that a provider reached through a CLI that only
// Projects jobs run on, such as cli/gemini/<model>, is fine: the task runs as
// a Projects job, never as an agent prompt.
func (v *validator) checkProjectTaskModel(field, model string) {
	if _, ok := v.projectsOnlyCLI(model); ok {
		return
	}
	v.checkModel(field, model)
}

// cliRegistry returns the CLI providers models names: the built-in
// definitions overlaid with models.cli_definitions. Definitions that do not
// load fall back to the built-in set; checkModels reports them on their own.
func cliRegistry(models ModelsConfig) *clidefs.Registry {
	if registry, err := clidefs.Load(models.CLIDefinitions); err == nil {
		return registry
	}
	return clidefs.MustBuiltin()
}

// checkCLIModel checks a CLI provider model string, "<cli>/<model>" or
// "cli/<cli>/<model>", and that the backend's CLI is installed.
func (v *validator) checkCLIModel(field, model string) {
	backend, _, err := cliagent.ParseModel(model)
	if err != nil {
		if name, ok := v.projectsOnlyCLI(model); ok {
			v.warnf(field, "%s is a provider reached through a CLI that runs Projects jobs, but only claude and codex can answer agent prompts; %q will not respond to messages", name, model)
			return
		}
		v.errorf(field, "%v", err)
		return
	}
	if _, err := exec.LookPath(backend.Binary()); err != nil {
		v.errorf(field, "%s CLI not found in PATH; install it to use model %q", backend, model)
	}
}

// projectsOnlyCLI reports the CLI provider a model names when it is one of
// the providers reached through a CLI, built in or from models.cli_definitions, that
// Projects jobs run on but agent turns cannot, such as gemini/<model> or
// cli/gemini/<model>.
func (v *validator) projectsOnlyCLI(model string) (string, bool) {
	rest, _ := strings.CutPrefix(strings.TrimSpace(model), cliagent.ModelPrefix)
	name, _, _ := strings.Cut(rest, "/")
	name, _, _ = strings.Cut(name, ":")
	if name == "" || cliagent.Backend(name).Valid() {
		return "", false
	}
	if _, ok := v.models.CLIDefinitions[name]; ok {
		return name, true
	}
	_, ok := cliRegistry(v.models).Lookup(name)
	return name, ok
}

// CLIBackends returns the CLI backends referenced by any agent model,
// fallback, or model default.
func CLIBackends(cfg *Config) []cliagent.Backend {
	seen := map[cliagent.Backend]bool{}
	var out []cliagent.Backend
	add := func(model string) {
		if b, _, err := cliagent.ParseModel(model); err == nil && !seen[b] {
			seen[b] = true
			out = append(out, b)
		}
	}
	if cfg.Models.Defaults != nil {
		add(cfg.Models.Defaults.Model)
		for _, fb := range cfg.Models.Defaults.Fallbacks {
			add(fb)
		}
	}
	for _, a := range cfg.Agents {
		add(a.Model)
		for _, fb := range a.Fallbacks {
			add(fb)
		}
	}
	return out
}

// checkAuthCredential checks that key is present in the auth store.
// Errors for the same key are only reported once regardless of how many agents reference it.
func (v *validator) checkAuthCredential(field, key, hint string) {
	if v.authGet == nil || v.checkedAuth[key] {
		return
	}
	v.checkedAuth[key] = true

	val, err := v.authGet(key)
	if err != nil || val == "" {
		v.warnf(field, "credential %q not found in auth store — %s", key, hint)
	}
}

// checkAuthCredentialEither checks that at least one of oauthKey or apiKey is
// present in the auth store. If neither is found, a single warning is emitted.
// Deduplication is based on the combination of both keys.
func (v *validator) checkAuthCredentialEither(field, oauthKey, apiKey, hint string) {
	dedupKey := oauthKey + "|" + apiKey
	if v.authGet == nil || v.checkedAuth[dedupKey] {
		return
	}
	v.checkedAuth[dedupKey] = true

	if oauthVal, _ := v.authGet(oauthKey); oauthVal != "" {
		return
	}
	if apiVal, _ := v.authGet(apiKey); apiVal != "" {
		return
	}
	v.warnf(field, "credential %q not found in auth store — %s", apiKey, hint)
}

// checkChannel validates a ChannelConfig.
func (v *validator) checkChannel(field string, ch ChannelConfig) {
	switch ch.Type {
	case "slack", "discord", "signal":
		// valid
	case "":
		v.errorf(field+".type", "channel type is required; must be \"slack\", \"discord\", or \"signal\"")
	default:
		v.errorf(field+".type", "unknown channel type %q; must be \"slack\", \"discord\", or \"signal\"", ch.Type)
	}

	if !BoolOr(ch.Enabled, true) {
		return
	}

	if ch.ShowTyping != nil && ch.Type != "" && ch.Type != "signal" && ch.Type != "slack" {
		v.errorf(field+".show_typing", "show_typing is only supported for signal and Slack; %s apps cannot send typing indicators, including in direct messages", ch.Type)
	}

	enabledEntries := 0
	for i, entry := range ch.AllowFrom {
		if !BoolOr(entry.Enabled, true) {
			continue
		}
		enabledEntries++
		ef := fmt.Sprintf("%s.allowFrom[%d]", field, i)
		if strings.TrimSpace(entry.From) == "" {
			v.errorf(ef+".from", "allowFrom entry must have a non-empty \"from\" field")
		}
	}
	if enabledEntries == 0 {
		v.warnf(field+".allowFrom", "no enabled allowFrom entries; all incoming messages will be rejected until at least one entry is enabled")
	}

	if ch.Token != "" && strings.HasPrefix(ch.Token, "auth:") {
		if !validAuthRef(ch.Token) {
			v.errorf(field+".token", "malformed auth reference %q; expected format auth:<key>", ch.Token)
		} else if v.authGet != nil {
			key := strings.TrimPrefix(ch.Token, "auth:")
			val, err := v.authGet(key)
			if err != nil || val == "" {
				v.warnf(field+".token", "auth reference %q not found in credential store — run 'aviary auth set %s <token>'", ch.Token, key)
			}
		}
	}
	if ch.URL != "" && strings.HasPrefix(ch.URL, "auth:") {
		if !validAuthRef(ch.URL) {
			v.errorf(field+".url", "malformed auth reference %q; expected format auth:<key>", ch.URL)
		} else if v.authGet != nil {
			key := strings.TrimPrefix(ch.URL, "auth:")
			val, err := v.authGet(key)
			if err != nil || val == "" {
				v.warnf(field+".url", "auth reference %q not found in credential store — run 'aviary auth set %s <token>'", ch.URL, key)
			}
		}
	}

	if strings.TrimSpace(ch.ID) == "" {
		v.errorf(field+".id", "channel id is required; use a stable unique identifier per agent and channel type")
	}

	if ch.Type == "signal" && ch.ID != "" && !strings.HasPrefix(ch.ID, "+") {
		v.warnf(field+".id", "signal id %q does not look like E.164 format; expected a leading '+' (e.g. +15551234567)", ch.ID)
	}
}

// checkModels validates ModelsConfig provider auth refs and default model strings.
func (v *validator) checkModels(m ModelsConfig) {
	if m.Defaults != nil {
		if m.Defaults.Model != "" {
			v.checkModel("models.defaults.model", m.Defaults.Model)
		}
		for i, fb := range m.Defaults.Fallbacks {
			v.checkModel(fmt.Sprintf("models.defaults.fallbacks[%d]", i), fb)
		}
	}
	for k, p := range m.Providers {
		pf := fmt.Sprintf("models.providers[%q].auth", k)
		if p.Auth == "" {
			continue
		}
		if !strings.HasPrefix(p.Auth, "auth:") {
			continue // literal value — no format check needed
		}
		if !validAuthRef(p.Auth) {
			v.errorf(pf, "malformed auth reference %q; expected format auth:<key>", p.Auth)
			continue
		}
		if v.authGet != nil {
			key := strings.TrimPrefix(p.Auth, "auth:")
			val, err := v.authGet(key)
			if err != nil || val == "" {
				v.errorf(pf, "auth reference %q not found in credential store — run 'aviary auth set %s <api-key>'", p.Auth, key)
			}
		}
	}
}

// checkBrowser validates BrowserConfig.
func (v *validator) checkBrowser(b BrowserConfig) {
	if b.Binary != "" {
		if _, err := os.Stat(b.Binary); os.IsNotExist(err) {
			if _, err2 := exec.LookPath(b.Binary); err2 != nil {
				v.errorf("browser.binary", "binary %q not found as an absolute path or on PATH", b.Binary)
			}
		}
	}
	if b.CDPPort != 0 && (b.CDPPort < 1 || b.CDPPort > 65535) {
		v.errorf("browser.cdp_port", "CDP port %d is out of range; must be 1–65535", b.CDPPort)
	}
}

// checkScheduler validates SchedulerConfig.
func (v *validator) checkScheduler(s SchedulerConfig) {
	switch val := s.Concurrency.(type) {
	case nil:
		// not set; defaults to "auto" — fine
	case string:
		if val != "auto" && val != "" {
			v.errorf("scheduler.concurrency", "invalid string value %q; must be \"auto\" or a positive integer", val)
		}
	case int:
		if val <= 0 {
			v.warnf("scheduler.concurrency", "concurrency %d is not positive; will behave as \"auto\" (GOMAXPROCS)", val)
		}
	default:
		v.errorf("scheduler.concurrency", "unexpected type %T (value %v); must be \"auto\" or a positive integer", s.Concurrency, s.Concurrency)
	}
}

// validAuthRef reports whether ref has the form "auth:<key>" with a non-empty key.
// The key may itself contain ":" characters.
func validAuthRef(ref string) bool {
	if !strings.HasPrefix(ref, "auth:") {
		return false
	}
	return strings.TrimSpace(strings.TrimPrefix(ref, "auth:")) != ""
}

// UniqueProviderModels returns a map of provider name → one representative
// model string (e.g. "anthropic" → "anthropic/claude-sonnet-5-5") drawn from
// all agents and model defaults. CLI provider models, "cli/..." and
// "<cli>/...", are excluded because they run through a local CLI rather than
// a remote endpoint; see CLIBackends.
func UniqueProviderModels(cfg *Config) map[string]string {
	registry := cliRegistry(cfg.Models)
	seen := map[string]string{}
	add := func(model string) {
		if model == "" {
			return
		}
		idx := strings.Index(model, "/")
		if idx < 0 {
			return
		}
		provider := model[:idx]
		if provider == "cli" {
			return
		}
		if _, ok := registry.Lookup(provider); ok && !llm.IsAPIProvider(provider) {
			return
		}
		if _, ok := seen[provider]; !ok {
			seen[provider] = model
		}
	}
	if cfg.Models.Defaults != nil {
		add(cfg.Models.Defaults.Model)
		for _, fb := range cfg.Models.Defaults.Fallbacks {
			add(fb)
		}
	}
	for _, a := range cfg.Agents {
		add(a.Model)
		for _, fb := range a.Fallbacks {
			add(fb)
		}
	}
	return seen
}

// checkProjects rejects a project owned by more than one agent: a project has
// exactly one parent agent.
func (v *validator) checkProjects(agents []AgentConfig) {
	owners := map[string]string{}
	for i, agent := range agents {
		for j, project := range agent.Projects.List {
			target := strings.TrimSpace(project.Target)
			if target == "" {
				v.errorf(fmt.Sprintf("agents[%d].projects.list[%d].target", i, j), "a project needs a target")
				continue
			}
			if owner, ok := owners[target]; ok && owner != agent.Name {
				v.errorf(fmt.Sprintf("agents[%d].projects.list[%d]", i, j), "%s is already owned by agent %q; a project belongs to exactly one agent", target, owner)
				continue
			}
			owners[target] = agent.Name
		}
	}
	// A project task runs against a project of its own agent's.
	for i, agent := range agents {
		for j, task := range agent.Tasks {
			project := strings.TrimSpace(task.Project)
			if project == "" {
				continue
			}
			if owner, ok := owners[project]; ok && owner != agent.Name {
				v.warnf(fmt.Sprintf("agents[%d].tasks[%d].project", i, j), "%s belongs to agent %q, not %q; its runs use %q's models", project, owner, agent.Name, owner)
			}
		}
	}
}
