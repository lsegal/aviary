package config

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/cronutil"
)

// Project defaults. A project entry in aviary.yaml holds only the values that
// differ from these; normalize strips anything equal to its default before the
// file is written, so a project watched with defaults has no entry at all.
const (
	DefaultProjectJobs       = 4
	DefaultProjectReadyLabel = "ready"
	DefaultProjectDelivery   = ProjectDeliveryBrowser
)

// Project delivery modes: how a watch reads GitHub.
const (
	ProjectDeliveryBrowser = "browser"
	ProjectDeliveryWebhook = "webhook"
	ProjectDeliveryPoll    = "poll"
)

// DefaultProjectFilters is the issue-search filter used when a project sets none.
var DefaultProjectFilters = []string{"assignee:@me author:@me"}

// Events a project can report through an Aviary agent's channels.
const (
	ProjectReportReady    = "ready"
	ProjectReportFailed   = "failed"
	ProjectReportAnswer   = "answer_needed"
	ProjectReportMerged   = "merged"
	defaultReportOnJoined = ProjectReportReady + "," + ProjectReportFailed + "," + ProjectReportAnswer
)

// DefaultProjectReportOn lists the events reported when report_on is unset.
var DefaultProjectReportOn = strings.Split(defaultReportOnJoined, ",")

var projectReportEvents = []string{ProjectReportReady, ProjectReportFailed, ProjectReportAnswer, ProjectReportMerged}

// ProjectConfig holds one watched project's settings that differ from the
// defaults. Every watch flag that can change at runtime has a field here.
type ProjectConfig struct {
	// Target is the watched repository, Project board or Discussions board,
	// for example "lsegal/aviary" or "users/lsegal/projects/4".
	Target string `yaml:"target" json:"target"`
	// Jobs is how many jobs run at once. 0 means DefaultProjectJobs.
	Jobs       int    `yaml:"jobs,omitempty"        json:"jobs,omitempty"`
	ReadyLabel string `yaml:"ready_label,omitempty" json:"ready_label,omitempty"`
	// ReadyState is the board status column that marks an item ready.
	// Empty auto-detects Todo or Ready.
	ReadyState string `yaml:"ready_state,omitempty" json:"ready_state,omitempty"`
	// Delivery is how the watch reads GitHub: browser, webhook or poll.
	Delivery string `yaml:"delivery,omitempty" json:"delivery,omitempty"`
	// Interval overrides the delivery mode's check interval (a Go duration).
	Interval   string   `yaml:"interval,omitempty"    json:"interval,omitempty"`
	Filters    []string `yaml:"filters,omitempty"     json:"filters,omitempty"`
	AllIssues  bool     `yaml:"all_issues,omitempty"  json:"all_issues,omitempty"`
	Commenters []string `yaml:"commenters,omitempty"  json:"commenters,omitempty"`
	// Merge, Changelog and Stack default to true; only false is written.
	Merge     *bool `yaml:"merge,omitempty"     json:"merge,omitempty"`
	Changelog *bool `yaml:"changelog,omitempty" json:"changelog,omitempty"`
	Stack     *bool `yaml:"stack,omitempty"     json:"stack,omitempty"`
	// CLIs is the coding-CLI fallback order as AGENT/MODEL:LEVEL specs, or
	// aviary/<agent> to run on an Aviary agent. Empty uses every signed-in CLI.
	CLIs     []string `yaml:"clis,omitempty"      json:"clis,omitempty"`
	TempDirs []string `yaml:"temp_dirs,omitempty" json:"temp_dirs,omitempty"`
	// ReportTo names the Aviary agent whose channels receive reports.
	ReportTo  string            `yaml:"report_to,omitempty" json:"report_to,omitempty"`
	ReportOn  []string          `yaml:"report_on,omitempty" json:"report_on,omitempty"`
	Schedules []ProjectSchedule `yaml:"schedules,omitempty" json:"schedules,omitempty"`
}

// ProjectSchedule is a scheduled prompt scoped to one project.
type ProjectSchedule struct {
	Schedule string `yaml:"schedule"         json:"schedule"`
	Prompt   string `yaml:"prompt"           json:"prompt"`
	Paused   bool   `yaml:"paused,omitempty" json:"paused,omitempty"`
}

// EffectiveProject returns p with every unset field filled from the defaults.
func EffectiveProject(p ProjectConfig) ProjectConfig {
	on := true
	if p.Jobs <= 0 {
		p.Jobs = DefaultProjectJobs
	}
	if p.ReadyLabel == "" {
		p.ReadyLabel = DefaultProjectReadyLabel
	}
	if p.Delivery == "" {
		p.Delivery = DefaultProjectDelivery
	}
	if len(p.Filters) == 0 && !p.AllIssues {
		p.Filters = slices.Clone(DefaultProjectFilters)
	}
	if p.Merge == nil {
		p.Merge = &on
	}
	if p.Changelog == nil {
		p.Changelog = &on
	}
	if p.Stack == nil {
		p.Stack = &on
	}
	if p.ReportTo != "" && len(p.ReportOn) == 0 {
		p.ReportOn = slices.Clone(DefaultProjectReportOn)
	}
	return p
}

// FindProject returns the configured entry for target, matched
// case-insensitively, or a zero entry for target when none is configured.
func FindProject(cfg *Config, target string) (ProjectConfig, bool) {
	for _, p := range cfg.Projects {
		if strings.EqualFold(p.Target, target) {
			return p, true
		}
	}
	return ProjectConfig{Target: target}, false
}

// normalizeProjects strips values equal to their defaults and drops entries
// left with nothing but a target, so aviary.yaml only records differences.
func normalizeProjects(cfg *Config) {
	var kept []ProjectConfig
	for _, p := range cfg.Projects {
		p.Target = strings.TrimSpace(p.Target)
		if p.Jobs == DefaultProjectJobs {
			p.Jobs = 0
		}
		if p.ReadyLabel == DefaultProjectReadyLabel {
			p.ReadyLabel = ""
		}
		if p.Delivery == DefaultProjectDelivery {
			p.Delivery = ""
		}
		if slices.Equal(p.Filters, DefaultProjectFilters) {
			p.Filters = nil
		}
		p.Merge = falseOnly(p.Merge)
		p.Changelog = falseOnly(p.Changelog)
		p.Stack = falseOnly(p.Stack)
		if slices.Equal(p.ReportOn, DefaultProjectReportOn) {
			p.ReportOn = nil
		}
		p.Filters = nilIfEmpty(p.Filters)
		p.Commenters = nilIfEmpty(p.Commenters)
		p.CLIs = nilIfEmpty(p.CLIs)
		p.TempDirs = nilIfEmpty(p.TempDirs)
		p.ReportOn = nilIfEmpty(p.ReportOn)
		if len(p.Schedules) == 0 {
			p.Schedules = nil
		}
		if p.Target == "" || isBareProject(p) {
			continue
		}
		kept = append(kept, p)
	}
	cfg.Projects = kept
}

func falseOnly(b *bool) *bool {
	if b == nil || *b {
		return nil
	}
	return b
}

func nilIfEmpty(s []string) []string {
	var out []string
	for _, v := range s {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func isBareProject(p ProjectConfig) bool {
	p.Target = ""
	return p.Jobs == 0 && p.ReadyLabel == "" && p.ReadyState == "" && p.Delivery == "" &&
		p.Interval == "" && p.Filters == nil && !p.AllIssues && p.Commenters == nil &&
		p.Merge == nil && p.Changelog == nil && p.Stack == nil && p.CLIs == nil &&
		p.TempDirs == nil && p.ReportTo == "" && p.ReportOn == nil && p.Schedules == nil
}

// checkProjects validates the projects section.
func (v *validator) checkProjects(projects []ProjectConfig) {
	seen := map[string]bool{}
	for i, p := range projects {
		field := fmt.Sprintf("projects[%d]", i)
		key := strings.ToLower(strings.TrimSpace(p.Target))
		if key == "" {
			v.errorf(field+".target", "target is required")
		} else if seen[key] {
			v.errorf(field+".target", "%q is listed more than once", p.Target)
		}
		seen[key] = true
		if p.Jobs < 0 {
			v.errorf(field+".jobs", "must be 0 or more")
		}
		switch p.Delivery {
		case "", ProjectDeliveryBrowser, ProjectDeliveryWebhook, ProjectDeliveryPoll:
		default:
			v.errorf(field+".delivery", "must be browser, webhook or poll, not %q", p.Delivery)
		}
		if p.Interval != "" {
			if d, err := time.ParseDuration(p.Interval); err != nil || d <= 0 {
				v.errorf(field+".interval", "must be a positive duration such as 30s, not %q", p.Interval)
			}
		}
		for _, ev := range p.ReportOn {
			if !slices.Contains(projectReportEvents, ev) {
				v.errorf(field+".report_on", "unknown event %q (use %s)", ev, strings.Join(projectReportEvents, ", "))
			}
		}
		if len(p.ReportOn) > 0 && p.ReportTo == "" {
			v.warnf(field+".report_on", "has no effect without report_to")
		}
		for j, s := range p.Schedules {
			sf := fmt.Sprintf("%s.schedules[%d]", field, j)
			if strings.TrimSpace(s.Schedule) == "" {
				v.errorf(sf+".schedule", "schedule is required")
			} else if _, err := cronutil.Parser().Parse(s.Schedule); err != nil {
				v.errorf(sf+".schedule", "invalid cron expression %q: %v", s.Schedule, err)
			}
			if strings.TrimSpace(s.Prompt) == "" {
				v.errorf(sf+".prompt", "prompt is required")
			}
		}
	}
}
