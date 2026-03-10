package config

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
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
// (key is the part after "auth:", e.g. "anthropic:default").
func Validate(cfg *Config, authGet func(key string) (string, error)) []Issue {
	v := &validator{
		authGet:     authGet,
		checkedAuth: map[string]bool{},
	}
	v.checkServer(cfg.Server)
	v.checkAgents(cfg.Agents, cfg.Models)
	v.checkModels(cfg.Models)
	v.checkBrowser(cfg.Browser)
	v.checkScheduler(cfg.Scheduler)
	return v.issues
}

type validator struct {
	issues      []Issue
	authGet     func(string) (string, error)
	checkedAuth map[string]bool // tracks auth keys already reported to avoid duplicate errors
}

func (v *validator) errorf(field, format string, args ...any) {
	v.issues = append(v.issues, Issue{LevelError, field, fmt.Sprintf(format, args...)})
}

func (v *validator) warnf(field, format string, args ...any) {
	v.issues = append(v.issues, Issue{LevelWarning, field, fmt.Sprintf(format, args...)})
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

		for j, ch := range a.Channels {
			v.checkChannel(fmt.Sprintf("%s.channels[%d]", f, j), ch)
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

			if t.Schedule == "" && t.Watch == "" {
				v.warnf(tf, "neither 'schedule' nor 'watch' is set; task will never be triggered")
			}

			if t.StartAt != "" {
				if _, err := time.Parse(time.RFC3339, t.StartAt); err != nil {
					v.errorf(tf+".start_at", "invalid RFC3339 timestamp %q: %v", t.StartAt, err)
				}
				if t.Watch != "" {
					v.errorf(tf+".start_at", "start_at is only supported for scheduled tasks, not watch tasks")
				}
			}

			if t.RunOnce {
				switch {
				case t.Watch != "":
					v.errorf(tf+".run_once", "run_once is only supported for scheduled tasks, not watch tasks")
				case t.Schedule == "" && t.StartAt == "":
					v.errorf(tf+".run_once", "run_once requires either schedule or start_at")
				}
			}

			if t.Schedule != "" {
				c := cron.New(cron.WithSeconds())
				if _, err := c.AddFunc(t.Schedule, func() {}); err != nil {
					v.errorf(tf+".schedule", "invalid cron expression %q: %v (aviary uses 6-field format with leading seconds field, e.g. \"0 * * * * *\" for every minute)", t.Schedule, err)
				}
			}

			if t.Prompt == "" {
				v.warnf(tf+".prompt", "prompt is empty; a blank message will be sent to the agent")
			}

			switch t.Channel {
			case "", "slack", "discord", "last":
				// valid
			default:
				v.errorf(tf+".channel", "invalid value %q; must be \"slack\", \"discord\", \"last\", or empty (silent)", t.Channel)
			}
		}
	}
}

// checkModel validates a "<provider>/<name>" model string and checks for required credentials.
func (v *validator) checkModel(field, model string) {
	parts := strings.SplitN(model, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		v.errorf(field, "invalid model %q: expected format <provider>/<model-name> (e.g. \"anthropic/claude-sonnet-4-5\")", model)
		return
	}

	provider, name := parts[0], parts[1]

	switch provider {
	case "anthropic":
		v.checkAuthCredentialEither(field, "anthropic:oauth", "anthropic:default",
			"run 'aviary auth set anthropic:default <your-api-key>' or 'aviary auth login anthropic'")
	case "openai":
		v.checkAuthCredential(field, "openai:default",
			"run 'aviary auth set openai:default <your-api-key>'")
	case "openai-codex":
		v.checkAuthCredential(field, "openai:oauth", "run 'aviary auth login openai'")
	case "google", "gemini", "google-gemini-cli":
		v.checkAuthCredentialEither(field, "gemini:oauth", "gemini:default",
			"run 'aviary auth set gemini:default <your-api-key>' or 'aviary auth login gemini'")
	case "stdio":
		if _, err := exec.LookPath(name); err != nil {
			v.errorf(field, "stdio command %q not found in PATH: %v", name, err)
		}
	default:
		v.errorf(field, "unknown provider %q in model %q; must be anthropic, openai, google, or stdio", provider, model)
	}
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

	if len(ch.AllowFrom) == 0 {
		v.warnf(field+".allowFrom", "empty allowFrom list will silently reject all incoming messages; add allowFrom entries or use [{from: \"*\"}] to allow everyone")
	}
	for i, entry := range ch.AllowFrom {
		ef := fmt.Sprintf("%s.allowFrom[%d]", field, i)
		if strings.TrimSpace(entry.From) == "" {
			v.errorf(ef+".from", "allowFrom entry must have a non-empty \"from\" field")
		}
	}

	if ch.Token != "" && strings.HasPrefix(ch.Token, "auth:") {
		if !validAuthRef(ch.Token) {
			v.errorf(field+".token", "malformed auth reference %q; expected format auth:<provider>:<name>", ch.Token)
		} else if v.authGet != nil {
			key := strings.TrimPrefix(ch.Token, "auth:")
			val, err := v.authGet(key)
			if err != nil || val == "" {
				v.warnf(field+".token", "auth reference %q not found in credential store — run 'aviary auth set %s <token>'", ch.Token, key)
			}
		}
	}

	if ch.Type == "signal" && ch.Phone != "" && !strings.HasPrefix(ch.Phone, "+") {
		v.warnf(field+".phone", "phone %q does not look like E.164 format; expected a leading '+' (e.g. +15551234567)", ch.Phone)
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
			v.errorf(pf, "malformed auth reference %q; expected format auth:<provider>:<name>", p.Auth)
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

// validAuthRef reports whether ref has the form "auth:<provider>:<name>"
// with both provider and name non-empty.
func validAuthRef(ref string) bool {
	if !strings.HasPrefix(ref, "auth:") {
		return false
	}
	rest := strings.TrimPrefix(ref, "auth:")
	parts := strings.SplitN(rest, ":", 2)
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}

// UniqueProviderModels returns a map of provider name → one representative
// model string (e.g. "anthropic" → "anthropic/claude-sonnet-4-5") drawn from
// all agents and model defaults. "stdio" providers are excluded because they
// have no remote endpoint to ping.
func UniqueProviderModels(cfg *Config) map[string]string {
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
		if provider == "stdio" {
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
