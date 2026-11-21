package config

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/clidefs"
)

// ProjectsConfig is the top-level projects: section in aviary.yaml: the one
// set of Projects settings, shared by every project of every agent. Every
// field is optional and holds only a value that differs from its default, so
// a watch started with defaults writes nothing here. The projects themselves
// are listed under the agents that own them; see AgentProjectsConfig.
type ProjectsConfig struct {
	// Concurrency is how many jobs run at once across every project. 0 means
	// DefaultProjectsConcurrency.
	Concurrency int `yaml:"concurrency,omitempty" json:"concurrency,omitempty"`
	// Interval is the time between GitHub polls, as a Go duration. Unset
	// means DefaultProjectsBrowserInterval for browser delivery and
	// DefaultProjectsInterval otherwise.
	Interval string `yaml:"interval,omitempty" json:"interval,omitempty"`
	// Delivery is how GitHub changes arrive: "browser" (the default) reads
	// the rendered pages in Aviary's browser, signed in as the user; "poll"
	// polls through gh; "webhook" takes webhook deliveries.
	Delivery string `yaml:"delivery,omitempty" json:"delivery,omitempty"`
	// BrowserVision turns on browser delivery's capped screenshot fallback
	// for pages it can no longer read.
	BrowserVision bool `yaml:"browser_vision,omitempty" json:"browser_vision,omitempty"`
	// BrowserVisible shows the browser browser delivery reads with, for
	// debugging. It needs a display.
	BrowserVisible bool `yaml:"browser_visible,omitempty" json:"browser_visible,omitempty"`
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
	// ApprovalMode is the Projects surface's approval-mode override for the
	// CLI runs it launches. Empty follows approval.mode.
	ApprovalMode clidefs.ApprovalMode `yaml:"approval_mode,omitempty" json:"approval_mode,omitempty"`
	// Yolo is the full approval mode, honoured when ApprovalMode is unset.
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
	// ChatModel and ChatFallbacks pick the model of the built-in Projects
	// agent behind the board's prompt chat. Empty uses models.defaults, as
	// for any Aviary agent.
	ChatModel     string   `yaml:"chat_model,omitempty" json:"chat_model,omitempty"`
	ChatFallbacks []string `yaml:"chat_fallbacks,omitempty" json:"chat_fallbacks,omitempty"`
	// TempDirs are where job clones go, tried in order. Empty means the
	// system temp directory.
	TempDirs []string `yaml:"temp_dirs,omitempty" json:"temp_dirs,omitempty"`
	// Webhook configures webhook delivery.
	Webhook *ProjectsWebhookConfig `yaml:"webhook,omitempty" json:"webhook,omitempty"`
}

// AgentProjectsConfig is an agent's projects: section in aviary.yaml: the
// projects the agent owns. Their settings are the shared top-level projects:
// section.
type AgentProjectsConfig struct {
	// List is the watched projects: repositories, project boards and
	// Discussions boards.
	List []ProjectConfig `yaml:"list,omitempty" json:"list,omitempty"`
}

// ProjectConfig is one watched project. The agent whose projects: section
// holds it owns it: that agent's model and fallbacks, in order, are the
// provider and model defaults of the project's jobs, a job running on the
// first one whose quota is not used up.
type ProjectConfig struct {
	// Target is owner/repo, a repository URL, a project board, or a
	// Discussions board.
	Target string `yaml:"target" json:"target"`
	// Reports sends this project's job lifecycle events through an Aviary
	// agent's channels. Unset means no reports.
	Reports *ProjectReportsConfig `yaml:"reports,omitempty" json:"reports,omitempty"`
}

// ProjectReportsConfig is a project's opt-in to job reports.
type ProjectReportsConfig struct {
	// Agent is the Aviary agent whose channels deliver the reports.
	Agent string `yaml:"agent" json:"agent"`
	// Targets are where reports go, in the same form as a scheduled task's
	// target: "<channel type>:<configured channel id>:<delivery id>" or
	// "session:<name>".
	Targets []string `yaml:"targets" json:"targets"`
	// Events limits reports to these job events. Empty means every event in
	// ProjectReportEvents.
	Events []string `yaml:"events,omitempty" json:"events,omitempty"`
}

// Project report events.
const (
	ProjectReportMerged  = "merged"
	ProjectReportFailed  = "failed"
	ProjectReportWaiting = "waiting"
)

// ProjectReportEvents are the job events a project can report.
var ProjectReportEvents = []string{ProjectReportMerged, ProjectReportFailed, ProjectReportWaiting}

// Enabled reports whether reports are on: an agent and at least one target
// are set.
func (r *ProjectReportsConfig) Enabled() bool {
	if r == nil || strings.TrimSpace(r.Agent) == "" {
		return false
	}
	for _, target := range r.Targets {
		if strings.TrimSpace(target) != "" {
			return true
		}
	}
	return false
}

// Reports reports whether event is reported.
func (r *ProjectReportsConfig) Reports(event string) bool {
	if !r.Enabled() {
		return false
	}
	if len(r.Events) == 0 {
		return true
	}
	for _, e := range r.Events {
		if strings.EqualFold(strings.TrimSpace(e), event) {
			return true
		}
	}
	return false
}

// ValidateProjectReports checks report settings against this config: the
// agent must exist, at least one target is needed, and every event must be
// one of ProjectReportEvents. Nil settings, meaning no reports, are valid.
func (c *Config) ValidateProjectReports(r *ProjectReportsConfig) error {
	if r == nil {
		return nil
	}
	agent := strings.TrimSpace(r.Agent)
	if agent == "" {
		return fmt.Errorf("reports need an agent")
	}
	if !slices.ContainsFunc(c.Agents, func(a AgentConfig) bool { return a.Name == agent }) {
		return fmt.Errorf("reports agent %q is not configured", agent)
	}
	if !r.Enabled() {
		return fmt.Errorf("reports need at least one target")
	}
	for _, event := range r.Events {
		if !slices.Contains(ProjectReportEvents, strings.ToLower(strings.TrimSpace(event))) {
			return fmt.Errorf("unknown report event %q; use one of %s", event, strings.Join(ProjectReportEvents, ", "))
		}
	}
	return nil
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
	// DefaultProjectsBrowserInterval is browser delivery's poll interval.
	DefaultProjectsBrowserInterval = 20 * time.Second
	DefaultProjectsDelivery        = "browser"
	DefaultProjectsReadyLabel      = "ready"
	DefaultProjectsFilter          = "assignee:@me"
	DefaultProjectsWebhookPath     = "/webhook"
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
	if p.EffectiveDelivery() == "browser" {
		return DefaultProjectsBrowserInterval
	}
	return DefaultProjectsInterval
}

// EffectiveDelivery returns "browser", "poll" or "webhook".
func (p ProjectsConfig) EffectiveDelivery() string {
	switch mode := strings.ToLower(strings.TrimSpace(p.Delivery)); mode {
	case "poll", "webhook":
		return mode
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

// Targets returns every watched target, in order, without duplicates.
func (p AgentProjectsConfig) Targets() []string {
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
func (p AgentProjectsConfig) IsZero() bool {
	return len(p.List) == 0
}

// IsZero reports whether the section holds nothing, so Save leaves it out.
func (p ProjectsConfig) IsZero() bool {
	return p.Concurrency == 0 && p.Interval == "" && p.Delivery == "" &&
		p.ReadyLabel == nil && p.ReadyState == "" && len(p.Filter) == 0 && !p.AllIssues &&
		len(p.AllowedCommenters) == 0 &&
		p.ApprovalMode == "" && !p.Yolo && !p.NoMerge && !p.NoChangelog &&
		p.UnblockInterval == "" && p.UnblockAgent == "" && p.UnblockModel == "" &&
		p.ChatModel == "" && len(p.ChatFallbacks) == 0 && len(p.TempDirs) == 0 && p.Webhook == nil
}

// Override returns p with a runtime watch's non-zero settings laid over it.
func (p ProjectsConfig) Override(overlay ProjectsConfig) ProjectsConfig {
	merged := p
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
	if overlay.ApprovalMode != "" {
		merged.ApprovalMode = overlay.ApprovalMode
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
	if overlay.ChatModel != "" {
		merged.ChatModel = overlay.ChatModel
	}
	if len(overlay.ChatFallbacks) > 0 {
		merged.ChatFallbacks = overlay.ChatFallbacks
	}
	if len(overlay.TempDirs) > 0 {
		merged.TempDirs = overlay.TempDirs
	}
	if overlay.Webhook != nil {
		merged.Webhook = overlay.Webhook
	}
	return merged
}

// ProjectList returns every agent's projects, in agent order.
func (c *Config) ProjectList() []ProjectConfig {
	var list []ProjectConfig
	for _, agent := range c.Agents {
		list = append(list, agent.Projects.List...)
	}
	return list
}

// ProjectOwners maps every watched target to the name of the agent that owns
// it. A target listed under several agents belongs to the first.
func (c *Config) ProjectOwners() map[string]string {
	owners := map[string]string{}
	for _, agent := range c.Agents {
		for _, target := range agent.Projects.Targets() {
			if _, ok := owners[target]; !ok {
				owners[target] = agent.Name
			}
		}
	}
	return owners
}

// ProjectOwner returns the name of the agent that owns target, or "" when no
// agent does.
func (c *Config) ProjectOwner(target string) string {
	return c.ProjectOwners()[strings.TrimSpace(target)]
}
