package config

import (
	"strings"
	"time"
)

// ProjectsConfig is the projects: section of aviary.yaml. Every field is
// optional and holds only a value that differs from its default, so a watch
// started with defaults writes nothing here.
type ProjectsConfig struct {
	// List is the watched projects: repositories, project boards and
	// Discussions boards.
	List []ProjectConfig `yaml:"list,omitempty" json:"list,omitempty"`
	// Concurrency is how many jobs run at once across every project. 0 means
	// DefaultProjectsConcurrency.
	Concurrency int `yaml:"concurrency,omitempty" json:"concurrency,omitempty"`
	// Interval is the time between GitHub polls, as a Go duration.
	Interval string `yaml:"interval,omitempty" json:"interval,omitempty"`
	// Delivery is how GitHub changes arrive: "poll" (the default) or
	// "webhook".
	Delivery string `yaml:"delivery,omitempty" json:"delivery,omitempty"`
	// ReadyLabel is the label a repository issue needs to be picked up.
	// Unset means DefaultProjectsReadyLabel; an empty string turns it off.
	ReadyLabel *string `yaml:"ready_label,omitempty" json:"ready_label,omitempty"`
	// ReadyState is the board status that marks an item ready. Empty
	// auto-detects Todo or Ready.
	ReadyState string `yaml:"ready_state,omitempty" json:"ready_state,omitempty"`
	// Filter is the issue search filter, one entry per term group. Empty
	// means DefaultProjectsFilter.
	Filter []string `yaml:"filter,omitempty" json:"filter,omitempty"`
	// AllIssues drops the issue filter.
	AllIssues bool `yaml:"all_issues,omitempty" json:"all_issues,omitempty"`
	// AllowedCommenters are the GitHub logins whose comments steer jobs.
	// Empty means the authenticated gh user.
	AllowedCommenters []string `yaml:"allowed_commenters,omitempty" json:"allowed_commenters,omitempty"`
	// Agents are the coding-CLI specs (AGENT/MODEL:LEVEL) jobs load balance
	// across. Empty means DefaultProjectsAgent.
	Agents []string `yaml:"agents,omitempty" json:"agents,omitempty"`
	// AgentBinaries overrides the executable of a coding CLI by agent name.
	AgentBinaries map[string]string `yaml:"agent_binaries,omitempty" json:"agent_binaries,omitempty"`
	// CLIDefinitions overrides built-in coding-CLI definitions field by field
	// and adds new ones, keyed by agent name. A null field drops it.
	CLIDefinitions map[string]any `yaml:"cli_definitions,omitempty" json:"cli_definitions,omitempty"`
	// Yolo disables the coding CLIs' sandboxes and permission checks.
	Yolo bool `yaml:"yolo,omitempty" json:"yolo,omitempty"`
	// NoMerge asks gh-fix to leave finished pull requests for a human.
	NoMerge bool `yaml:"no_merge,omitempty" json:"no_merge,omitempty"`
	// NoChangelog asks gh-fix to skip changelog entries.
	NoChangelog bool `yaml:"no_changelog,omitempty" json:"no_changelog,omitempty"`
	// UnblockInterval is how often the stuck-job watchdog runs, as a Go
	// duration; "0" turns it off.
	UnblockInterval string `yaml:"unblock_interval,omitempty" json:"unblock_interval,omitempty"`
	// UnblockAgent and UnblockModel pick the watchdog's agent call. Empty
	// means the coding agent.
	UnblockAgent string `yaml:"unblock_agent,omitempty" json:"unblock_agent,omitempty"`
	UnblockModel string `yaml:"unblock_model,omitempty" json:"unblock_model,omitempty"`
	// TempDirs are where job clones go, tried in order. Empty means the
	// system temp directory.
	TempDirs []string `yaml:"temp_dirs,omitempty" json:"temp_dirs,omitempty"`
	// Webhook configures webhook delivery.
	Webhook *ProjectsWebhookConfig `yaml:"webhook,omitempty" json:"webhook,omitempty"`
}

// ProjectConfig is one watched project.
type ProjectConfig struct {
	// Target is owner/repo, a repository URL, a project board, or a
	// Discussions board.
	Target string `yaml:"target" json:"target"`
	// Agents are this project's coding-CLI specs in fallback order: a job
	// runs on the first one whose quota is not used up. Empty uses the
	// section's Agents.
	Agents []string `yaml:"agents,omitempty" json:"agents,omitempty"`
}

// ProjectsWebhookConfig configures the webhook listener and its tunnel.
type ProjectsWebhookConfig struct {
	Listen      string `yaml:"listen,omitempty"       json:"listen,omitempty"`
	Path        string `yaml:"path,omitempty"         json:"path,omitempty"`
	Secret      string `yaml:"secret,omitempty"       json:"secret,omitempty"`
	NgrokBinary string `yaml:"ngrok_binary,omitempty" json:"ngrok_binary,omitempty"`
}

// Projects defaults, applied by the consuming code so they never reach the
// YAML file.
const (
	DefaultProjectsConcurrency = 3
	DefaultProjectsInterval    = 30 * time.Second
	DefaultProjectsDelivery    = "poll"
	DefaultProjectsReadyLabel  = "ready"
	DefaultProjectsFilter      = "assignee:@me author:@me"
	DefaultProjectsAgent       = "codex"
	DefaultProjectsWebhookPath = "/webhook"
)

// EffectiveConcurrency returns the job limit.
func (p ProjectsConfig) EffectiveConcurrency() int {
	if p.Concurrency > 0 {
		return p.Concurrency
	}
	return DefaultProjectsConcurrency
}

// EffectiveInterval returns the poll interval.
func (p ProjectsConfig) EffectiveInterval() time.Duration {
	if d, err := time.ParseDuration(strings.TrimSpace(p.Interval)); err == nil && d > 0 {
		return d
	}
	return DefaultProjectsInterval
}

// EffectiveDelivery returns "poll" or "webhook".
func (p ProjectsConfig) EffectiveDelivery() string {
	if strings.EqualFold(strings.TrimSpace(p.Delivery), "webhook") {
		return "webhook"
	}
	return DefaultProjectsDelivery
}

// EffectiveReadyLabel returns the ready label, empty when turned off.
func (p ProjectsConfig) EffectiveReadyLabel() string {
	if p.ReadyLabel == nil {
		return DefaultProjectsReadyLabel
	}
	return strings.TrimSpace(*p.ReadyLabel)
}

// EffectiveFilter returns the issue search filter.
func (p ProjectsConfig) EffectiveFilter() string {
	if filter := strings.TrimSpace(strings.Join(p.Filter, " ")); filter != "" {
		return filter
	}
	return DefaultProjectsFilter
}

// EffectiveAgents returns the agent specs jobs load balance across.
func (p ProjectsConfig) EffectiveAgents() []string {
	var specs []string
	for _, spec := range p.Agents {
		if spec = strings.TrimSpace(spec); spec != "" {
			specs = append(specs, spec)
		}
	}
	if len(specs) == 0 {
		return []string{DefaultProjectsAgent}
	}
	return specs
}

// Targets returns every watched target, in order, without duplicates.
func (p ProjectsConfig) Targets() []string {
	var targets []string
	seen := map[string]bool{}
	for _, project := range p.List {
		target := strings.TrimSpace(project.Target)
		if target == "" || seen[target] {
			continue
		}
		seen[target] = true
		targets = append(targets, target)
	}
	return targets
}

// IsZero reports whether the section holds nothing, so Save leaves it out.
func (p ProjectsConfig) IsZero() bool {
	return len(p.List) == 0 && p.Concurrency == 0 && p.Interval == "" && p.Delivery == "" &&
		p.ReadyLabel == nil && p.ReadyState == "" && len(p.Filter) == 0 && !p.AllIssues &&
		len(p.AllowedCommenters) == 0 && len(p.Agents) == 0 && len(p.AgentBinaries) == 0 &&
		len(p.CLIDefinitions) == 0 && !p.Yolo && !p.NoMerge && !p.NoChangelog &&
		p.UnblockInterval == "" && p.UnblockAgent == "" && p.UnblockModel == "" &&
		len(p.TempDirs) == 0 && p.Webhook == nil
}
