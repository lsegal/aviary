package clidoctor

import (
	"context"

	"github.com/lsegal/aviary/internal/clidefs"
)

// Status reports one coding CLI's install, sign-in, and quota state in the
// wire format provider_cli_list and the settings dashboard read, mirroring
// what the doctor prints.
type Status struct {
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Binary    string `json:"binary"`
	// Path is where Binary was found on PATH, empty when it is not installed.
	Path        string `json:"path,omitempty"`
	Version     string `json:"version,omitempty"`
	VersionNote string `json:"versionNote,omitempty"`
	Auth        string `json:"auth"`
	Quota       string `json:"quota"`
	// TracksQuota says the definition names a quota source at all, telling an
	// agent that reports no quota by design apart from one whose reader
	// could not answer -- the same distinction QuotaText draws for the
	// terminal report.
	TracksQuota bool   `json:"tracksQuota"`
	QuotaError  string `json:"quotaError,omitempty"`
	// Models are the fully qualified agent/model names --agent accepts, and
	// ModelNote explains a missing or partial list.
	Models    []string `json:"models,omitempty"`
	ModelNote string   `json:"modelNote,omitempty"`
	// DefaultModel is the model aviary runs this agent with when --agent names
	// none, empty for an agent whose definition leaves the choice to its own
	// CLI.
	DefaultModel string `json:"defaultModel,omitempty"`
	// SkillsNote says which of aviary's installed skills are stale for the
	// running release, empty when they are current.
	SkillsNote string `json:"skillsNote,omitempty"`
	// ProjectsNote says why the agent is gated out of Projects jobs, empty
	// when it can run them.
	ProjectsNote string `json:"projectsNote,omitempty"`
	// Status is one of "ok", "warn", or "missing", matching the marker
	// `aviary agents` prefixes each block with.
	Status string `json:"status"`
}

// QuotaText is the report's quota line. An agent that names no quota source
// is not tracked, which is the deliberate default; one that names a source
// and could not be read is unavailable, which is a problem worth telling
// apart from the default.
func (r Report) QuotaText() string {
	if text := QuotaReading(r.Quota, r.TracksQuota, r.QuotaErr); text != "" {
		return text
	}
	return "not tracked"
}

// StatusOf converts a report into the wire format.
func StatusOf(report Report) Status {
	status := Status{
		Name:         report.Name,
		Installed:    report.Installed(),
		Binary:       report.Binary,
		Path:         report.Path,
		Version:      report.Version,
		VersionNote:  report.VersionNote,
		Auth:         report.Auth,
		Quota:        report.QuotaText(),
		TracksQuota:  report.TracksQuota,
		Models:       report.Models,
		ModelNote:    report.ModelNote,
		DefaultModel: report.DefaultModel,
		SkillsNote:   report.SkillsNote,
		ProjectsNote: report.ProjectsNote,
		Status:       report.StatusKey(),
	}
	if report.QuotaErr != nil {
		status.QuotaError = report.QuotaErr.Error()
	}
	return status
}

// Statuses probes every coding CLI -- the built-ins overlaid with
// definitions, the models: section's cli_definitions -- for install, version,
// sign-in, quota and models.
func Statuses(ctx context.Context, definitions map[string]any) ([]Status, error) {
	registry, err := clidefs.FromConfig(definitions)
	if err != nil {
		return nil, err
	}
	return New(registry).Statuses(ctx), nil
}

// Statuses probes every CLI the doctor's registry defines and reports each
// in the wire format.
func (d *Doctor) Statuses(ctx context.Context) []Status {
	reports := d.Report(ctx)
	statuses := make([]Status, len(reports))
	for i, report := range reports {
		statuses[i] = StatusOf(report)
	}
	return statuses
}
