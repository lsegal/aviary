package projects

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/clidoctor"
	"github.com/lsegal/aviary/internal/codingskills"
	"github.com/lsegal/aviary/internal/projects/core"
)

// Status markers the report prefixes each agent with, in the shape a doctor
// command is read at a glance: ready, installed but incomplete, and missing.
// They are ASCII because the report is printed to whatever console the user
// happens to be on, including a Windows one that would render check marks as
// mojibake.
const (
	statusReady   = "[ok]"
	statusWarn    = "[!]"
	statusMissing = "[x]"

	// statusWidth pads the markers to one column, so the agent names line up
	// however many of each the registry produces.
	statusWidth = 4
)

// newAgentDoctor builds core's CLI doctor with the two answers only Projects
// can give: the agent's quota, read through the same readers the watch status
// bar uses, and which of aviary's skills installed for it are stale.
func newAgentDoctor(registry *clidefs.Registry) *clidoctor.Doctor {
	doctor := clidoctor.New(registry)
	doctor.Quota = func(ctx context.Context, name, binary string) (string, error) {
		return readAgentQuota(ctx, registry, name, binary)
	}
	doctor.SkillsNote = func(name string) string {
		for _, cli := range codingskills.CLIsFromRegistry(registry) {
			if cli.ID == name {
				return codingskills.Default().StaleNote(cli)
			}
		}
		return ""
	}
	return doctor
}

// status is the marker an agent's line is prefixed with.
func status(report clidoctor.Report) string {
	switch report.StatusKey() {
	case clidoctor.StatusMissing:
		return statusMissing
	case clidoctor.StatusWarn:
		return statusWarn
	default:
		return statusReady
	}
}

// readAgentQuota reads one agent's quota through the same readers the watch
// status bar uses, so the report never grows a second opinion about quota.
func readAgentQuota(ctx context.Context, registry *clidefs.Registry, name, binary string) (string, error) {
	readers := namedQuotaReaders(registry, []string{name}, func(string) string { return binary })
	if len(readers) == 0 {
		return "", nil
	}
	quota, err := readers[0].read(ctx)
	return strings.TrimSpace(quota), err
}

// agentStatus converts a clidoctor.Report into the wire format the settings
// dashboard's agents tab reads -- the same probe `aviary agents`
// renders as text.
func agentStatus(report clidoctor.Report) core.AgentStatus {
	status := core.AgentStatus{
		Name:         report.Name,
		Installed:    report.Installed(),
		Binary:       report.Binary,
		Path:         report.Path,
		Version:      report.Version,
		VersionNote:  report.VersionNote,
		Auth:         report.Auth,
		Quota:        describeQuota(report),
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

// LoadCLIDefinitions builds the coding-CLI registry from the models: section's
// cli_definitions. The provider doctor and Projects dispatch both load it
// here, so a binary override in a definition is the executable the doctor
// probes and the one jobs run. The aviary definition points at the running
// executable unless cli_definitions names another.
func LoadCLIDefinitions(definitions map[string]any) (*clidefs.Registry, error) {
	return clidefs.Load(withAviaryExecutable(definitions))
}

// withAviaryExecutable sets the aviary definition's binary to the running
// executable when definitions do not set one.
func withAviaryExecutable(definitions map[string]any) map[string]any {
	override, _ := definitions[aviaryAgent].(map[string]any)
	if override["binary"] != nil {
		return definitions
	}
	executable, err := os.Executable()
	if err != nil {
		return definitions
	}
	merged := make(map[string]any, len(definitions)+1)
	for name, definition := range definitions {
		merged[name] = definition
	}
	aviary := map[string]any{"binary": executable}
	for field, value := range override {
		if field != "binary" {
			aviary[field] = value
		}
	}
	merged[aviaryAgent] = aviary
	return merged
}

// CLIStatuses is the coding-CLI doctor core Aviary's providers report: every
// definition -- the built-ins overlaid with definitions, the models:
// section's cli_definitions -- probed for install, version, sign-in, quota
// and models.
func CLIStatuses(ctx context.Context, definitions map[string]any) ([]core.AgentStatus, error) {
	registry, err := LoadCLIDefinitions(definitions)
	if err != nil {
		return nil, err
	}
	return agentStatuses(ctx, registry)
}

// agentStatuses probes every agent in registry and reports it in the shape
// the settings dashboard's agents tab reads.
func agentStatuses(ctx context.Context, registry *clidefs.Registry) ([]core.AgentStatus, error) {
	reports := newAgentDoctor(registry).Report(ctx)
	statuses := make([]core.AgentStatus, len(reports))
	for i, report := range reports {
		statuses[i] = agentStatus(report)
	}
	return statuses, nil
}

// doctorFieldWidth is the column the report's field values line up in.
const doctorFieldWidth = 8

// writeAgentReports renders the report. It is a plain block per agent rather
// than a table: the model list is the longest thing on the page and the one
// people are here to copy, so it gets its own lines instead of being wrapped
// into a cell.
func writeAgentReports(out io.Writer, reports []clidoctor.Report) {
	for i, report := range reports {
		if i > 0 {
			_, _ = fmt.Fprintln(out)
		}
		_, _ = fmt.Fprintf(out, "%-*s %s\n", statusWidth, status(report), report.Name)
		writeAgentField(out, "binary", describeBinary(report))
		if report.ProjectsNote != "" {
			writeAgentField(out, "projects", "unavailable: "+report.ProjectsNote)
		}
		if !report.Installed() {
			continue
		}
		writeAgentField(out, "auth", report.Auth)
		writeAgentField(out, "quota", describeQuota(report))
		if report.DefaultModel != "" {
			writeAgentField(out, "default", clidoctor.Qualify(report.Name, []string{report.DefaultModel})[0])
		}
		if report.SkillsNote != "" {
			writeAgentField(out, "skills", report.SkillsNote)
		}
		writeAgentModels(out, report)
	}
}

// describeQuota reads the agent's quota line. An agent that names no quota
// source is not tracked, which is the deliberate default; one that names a
// source and could not be read is unavailable, which is a problem worth
// telling apart from the default.
func describeQuota(report clidoctor.Report) string {
	if text := quotaReading(report.Quota, report.TracksQuota, report.QuotaErr); text != "" {
		return text
	}
	return "not tracked"
}

// describeBinary says where the CLI is, or that it is not installed, with the
// version and any complaint about it on the same line.
func describeBinary(report clidoctor.Report) string {
	if !report.Installed() {
		return fmt.Sprintf("%s (not installed)", orDefault(report.Binary, clidoctor.Unknown))
	}
	text := report.Path
	if report.Version != "" {
		text += ", version " + report.Version
	}
	if report.VersionNote != "" {
		text += " (" + report.VersionNote + ")"
	}
	return text
}

// writeAgentModels prints the models one per line, so every name can be copied
// straight into --agent, with the count on the header line for the agents that
// front dozens of them.
func writeAgentModels(out io.Writer, report clidoctor.Report) {
	if len(report.Models) == 0 {
		writeAgentField(out, "models", orDefault(report.ModelNote, clidoctor.Unknown))
		return
	}
	header := fmt.Sprintf("%d available", len(report.Models))
	if note := strings.TrimSpace(report.ModelNote); note != "" {
		header = fmt.Sprintf("%d %s", len(report.Models), note)
	}
	writeAgentField(out, "models", header)
	for _, model := range report.Models {
		_, _ = fmt.Fprintf(out, "  %-*s  %s\n", doctorFieldWidth, "", model)
	}
}

func writeAgentField(out io.Writer, name, value string) {
	_, _ = fmt.Fprintf(out, "  %-*s  %s\n", doctorFieldWidth, name, value)
}

func orDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
