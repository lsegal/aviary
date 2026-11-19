package projects

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"

	"github.com/lsegal/aviary/internal/projects/agents"
	"github.com/lsegal/aviary/internal/projects/core"
	"github.com/lsegal/aviary/internal/projects/process"
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

// Text the report prints where a probe could not answer. "unknown" is a real
// answer here -- an agent that declares no sign-in probe is not signed out --
// so it is never rendered as a failure.
const (
	doctorUnknown   = "unknown"
	doctorSignedIn  = "signed in"
	doctorSignedOut = "signed out"
)

// agentReport is what the doctor report knows about one agent after probing.
type agentReport struct {
	name string
	// binary is the executable the definition names, and path is where it was
	// found; an empty path is an agent whose CLI is not installed.
	binary string
	path   string
	// version is the first line of `binary --version`, and versionNote says
	// why it is a problem when the definition declares a minimum it misses.
	version     string
	versionNote string
	// auth is the sign-in state, quota the agent's own quota reading, and
	// models the fully qualified agent/model names --agent accepts.
	auth string
	// quota is the reading, and tracksQuota says the definition names a quota
	// source at all. The two apart are what tell an agent that reports no
	// quota by design from one whose reader could not answer, which look the
	// same on the line and mean opposite things.
	quota       string
	tracksQuota bool
	// quotaErr is why the reader could not answer, when it could not. It is
	// what turns a bare "unavailable" into something actionable: the flag the
	// CLI rejected, the command that is missing, the account that is signed
	// out.
	quotaErr  error
	models    []string
	modelNote string
	// defaultModel is the model aviary runs the agent with when --agent names
	// none, empty for a definition that leaves that to the CLI.
	defaultModel string
	// skillsNote says which of aviary's installed skills are stale, empty when
	// they match the running release or the agent installs none.
	skillsNote string
	// projectsNote says why the agent is gated out of Projects jobs, empty
	// when it can run them.
	projectsNote string
}

// installed reports whether the agent's CLI was found on PATH.
func (r agentReport) installed() bool { return r.path != "" }

// statusKey is the machine-readable form of status(), for a JSON consumer
// such as the settings dashboard's agents tab rather than the
// terminal's ASCII marker.
func (r agentReport) statusKey() string {
	switch {
	case !r.installed():
		return "missing"
	case r.versionNote != "" || r.auth == doctorSignedOut || r.skillsNote != "":
		return "warn"
	default:
		return "ok"
	}
}

// status is the marker the agent's line is prefixed with: missing when the CLI
// is not installed, a warning when it is installed but something about it needs
// attention, and ready otherwise.
func (r agentReport) status() string {
	switch r.statusKey() {
	case "missing":
		return statusMissing
	case "warn":
		return statusWarn
	default:
		return statusReady
	}
}

// agentDoctor probes the registry. Every process it runs is behind a function
// field so the tests can answer for a machine with no agent CLI installed at
// all, which is every machine CI runs on.
type agentDoctor struct {
	registry *agents.Registry
	// lookPath resolves an executable, version reads its reported version,
	// run executes one probe and returns its output and whether it succeeded,
	// and quotaFor reads one agent's quota text.
	lookPath func(string) (string, error)
	version  func(ctx context.Context, binary string) ([]byte, error)
	run      func(ctx context.Context, argv []string, spec agents.Doctor, stdin []string) ([]byte, bool)
	quotaFor func(ctx context.Context, name, binary string) (string, error)
	// skillsNote reports which installed skills are stale.
	skillsNote func() string
}

// newAgentDoctor builds the doctor that talks to the real machine.
func newAgentDoctor(registry *agents.Registry) *agentDoctor {
	return &agentDoctor{
		registry: registry,
		lookPath: exec.LookPath,
		version:  func(ctx context.Context, binary string) ([]byte, error) { return agentVersionCommand(ctx, binary) },
		run:      runDoctorProbe,
		quotaFor: func(ctx context.Context, name, binary string) (string, error) {
			return readAgentQuota(ctx, registry, name, binary)
		},
	}
}

// runDoctorProbe runs one probe under the definition's timeout, reporting its
// combined output and whether it exited zero. A probe that fails is not an
// error the report stops for: it is the answer.
func runDoctorProbe(ctx context.Context, argv []string, spec agents.Doctor, stdin []string) ([]byte, bool) {
	if len(argv) == 0 {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(ctx, spec.TimeoutDuration())
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.WaitDelay = quotaCommandWaitDelay
	if len(stdin) > 0 {
		return runProbeConversation(cmd, spec, stdin)
	}
	// A probe is told nothing, so a CLI that would prompt reads EOF and gives
	// up instead of waiting on a terminal the report does not have.
	cmd.Stdin = strings.NewReader("")
	out, err := process.CombinedOutput(cmd)
	return out, err == nil
}

// probeLineLimit bounds one line of a conversational probe's output. A model
// catalog arrives as a single JSON-RPC response, and a CLI that fronts a
// couple of hundred models writes all of them on that one line.
const probeLineLimit = 4 << 20

// runProbeConversation drives a probe that answers over a stdio protocol
// instead of printing and exiting: it writes the definition's lines, holds the
// pipe open -- an agent speaking JSON-RPC abandons a client that hangs up
// mid-handshake -- and reads stdout until the output carries a model list.
//
// It stops at that first answer rather than waiting for the process to end,
// because it never will: these commands are servers, and a report that waited
// for one would wait for its whole timeout every time it succeeded.
func runProbeConversation(cmd *exec.Cmd, spec agents.Doctor, lines []string) ([]byte, bool) {
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, false
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false
	}
	// Only stdout is read: an agent that logs its startup to stderr would
	// otherwise interleave that noise into the responses being parsed.
	cmd.Stderr = io.Discard
	if err := process.Start(cmd); err != nil {
		return nil, false
	}
	defer func() {
		_ = input.Close()
		_ = process.Stop(cmd)
	}()
	go func() {
		for _, line := range lines {
			if _, err := io.WriteString(input, line+"\n"); err != nil {
				return
			}
		}
	}()
	var collected strings.Builder
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 0, 64<<10), probeLineLimit)
	for scanner.Scan() {
		collected.Write(scanner.Bytes())
		collected.WriteByte('\n')
		if len(spec.ModelsFrom(collected.String())) > 0 {
			break
		}
	}
	return []byte(collected.String()), collected.Len() > 0
}

// readAgentQuota reads one agent's quota through the same readers the watch
// status bar uses, so the report never grows a second opinion about quota.
func readAgentQuota(ctx context.Context, registry *agents.Registry, name, binary string) (string, error) {
	readers := namedQuotaReaders(registry, []string{name}, func(string) string { return binary })
	if len(readers) == 0 {
		return "", nil
	}
	quota, err := readers[0].read(ctx)
	return strings.TrimSpace(quota), err
}

// Report probes every registered agent and returns one entry each, in the
// registry's sorted order. The agents are probed concurrently: each one costs
// a handful of subprocesses, and a report that ran them one CLI at a time
// would take as long as the slowest of them multiplied by the registry.
func (d *agentDoctor) Report(ctx context.Context) []agentReport {
	names := d.registry.Names()
	reports := make([]agentReport, len(names))
	var wait sync.WaitGroup
	for i, name := range names {
		wait.Add(1)
		go func() {
			defer wait.Done()
			reports[i] = d.reportAgent(ctx, name)
		}()
	}
	wait.Wait()
	return reports
}

// agentStatus converts an agentReport into the wire format the settings
// dashboard's agents tab reads -- the same probe `aviary agents`
// renders as text.
func agentStatus(report agentReport) core.AgentStatus {
	status := core.AgentStatus{
		Name:         report.name,
		Installed:    report.installed(),
		Binary:       report.binary,
		Version:      report.version,
		VersionNote:  report.versionNote,
		Auth:         report.auth,
		Quota:        describeQuota(report),
		TracksQuota:  report.tracksQuota,
		Models:       report.models,
		ModelNote:    report.modelNote,
		DefaultModel: report.defaultModel,
		SkillsNote:   report.skillsNote,
		ProjectsNote: report.projectsNote,
		Status:       report.statusKey(),
	}
	if report.quotaErr != nil {
		status.QuotaError = report.quotaErr.Error()
	}
	return status
}

// agentStatuses probes every agent in registry and reports it in the shape
// the settings dashboard's agents tab reads.
func agentStatuses(ctx context.Context, registry *agents.Registry) ([]core.AgentStatus, error) {
	reports := newAgentDoctor(registry).Report(ctx)
	statuses := make([]core.AgentStatus, len(reports))
	for i, report := range reports {
		statuses[i] = agentStatus(report)
	}
	return statuses, nil
}

// reportAgent probes one agent. Nothing is run when its CLI is not installed:
// every remaining question is about that binary, and the answers would all be
// the same failure.
func (d *agentDoctor) reportAgent(ctx context.Context, name string) agentReport {
	report := agentReport{name: name, auth: doctorUnknown}
	definition, ok := d.registry.Lookup(name)
	if !ok {
		return report
	}
	report.binary = definition.Binary
	report.projectsNote = definition.ProjectsUnavailable
	report.defaultModel = definition.ModelOrDefault("")
	path, err := d.lookPath(definition.Binary)
	if err != nil {
		report.models, report.modelNote = declaredModels(definition)
		return report
	}
	report.path = path
	report.version, report.versionNote = d.reportVersion(ctx, definition, path)
	report.tracksQuota = definition.Quota.ReaderName() != agents.QuotaNone
	report.quota, report.quotaErr = d.quotaFor(ctx, name, path)
	report.auth = d.reportAuth(ctx, definition, path, report.quota)
	report.models, report.modelNote = d.reportModels(ctx, definition, path)
	if definition.SkillsTarget() != "" && d.skillsNote != nil {
		report.skillsNote = d.skillsNote()
	}
	return report
}

// reportVersion reads the CLI's version and says whether it clears the
// definition's declared minimum. An unreadable version is reported as unknown
// rather than as too old, matching how a dispatch treats it.
func (d *agentDoctor) reportVersion(ctx context.Context, definition agents.Definition, binary string) (version, note string) {
	out, err := d.version(ctx, binary)
	reported := firstLine(strings.TrimSpace(string(out)))
	if err != nil || reported == "" {
		return "", ""
	}
	if strings.TrimSpace(definition.MinVersion) == "" {
		return reported, ""
	}
	supported, comparable := definition.VersionSupported(strings.TrimSpace(string(out)))
	switch {
	case !comparable:
		return reported, fmt.Sprintf("could not be compared with the required %s or newer", definition.MinVersion)
	case !supported:
		return reported, fmt.Sprintf("older than the required %s", definition.MinVersion)
	}
	return reported, ""
}

// reportAuth resolves the sign-in state. A definition's own probe decides it;
// failing that, a quota reading is proof enough, because every quota reader
// aviary has asks the CLI something only a signed-in account can answer. An
// agent with neither is reported as unknown rather than guessed at.
func (d *agentDoctor) reportAuth(ctx context.Context, definition agents.Definition, binary, quota string) string {
	if argv := definition.Doctor.AuthArgv(binary); len(argv) > 0 {
		out, ok := d.run(ctx, argv, definition.Doctor, nil)
		if ok && definition.Doctor.ReportsSignedIn(string(out)) {
			return doctorSignedIn
		}
		return doctorSignedOut
	}
	if strings.TrimSpace(quota) != "" {
		return doctorSignedIn
	}
	return doctorUnknown
}

// reportModels lists the models the agent accepts, preferring what its CLI
// says over what the definition declares: a provider-agnostic CLI's model list
// is whatever it is configured with today, which no static allow-list can
// keep up with.
func (d *agentDoctor) reportModels(ctx context.Context, definition agents.Definition, binary string) ([]string, string) {
	argv := definition.Doctor.ModelsArgv(binary)
	if len(argv) == 0 {
		return declaredModels(definition)
	}
	out, ok := d.run(ctx, argv, definition.Doctor, definition.Doctor.ModelsStdinLines(binary))
	if models := definition.Doctor.ModelsFrom(string(out)); ok && len(models) > 0 {
		return qualify(definition.Name, models), ""
	}
	// A probe that ran and listed nothing is as unanswered as one that failed:
	// a CLI signed out of its provider prints a handshake and no catalog, and
	// reporting that as "any model the CLI accepts" would hide the reason.
	models, note := declaredModels(definition)
	if len(models) == 0 && strings.TrimSpace(definition.Doctor.ModelsNote) == "" {
		note = "could not be listed by " + strings.Join(argv, " ")
	}
	return models, note
}

// declaredModels is what the definition alone can say about an agent's models:
// its allow-list when it names one, then the models it knows the CLI accepts,
// and otherwise a note, because "the definition validates any model" and "the
// agent takes no model" are different answers and neither is a list.
//
// The known list is qualified with a note rather than presented as the whole
// truth: it is what aviary has been told, not what the CLI enforces, and a
// model that shipped after this definition did still works.
func declaredModels(definition agents.Definition) ([]string, string) {
	switch {
	case definition.Models.AcceptsNothing():
		return nil, "not accepted by this agent"
	case definition.Models.Declared():
		return qualify(definition.Name, definition.Models.Values()), ""
	case len(definition.Doctor.KnownModels) > 0:
		return qualify(definition.Name, definition.Doctor.KnownModels), modelsNote(definition.Doctor)
	case strings.TrimSpace(definition.Doctor.ModelsNote) != "":
		// A definition that cannot list its models can still say what to
		// write after --agent, which is what the field is read for.
		return nil, strings.TrimSpace(definition.Doctor.ModelsNote)
	}
	return nil, "any model the CLI accepts"
}

// knownModelsNote qualifies a list aviary declared rather than read from the
// CLI, so nobody reads it as the CLI's own catalog.
const knownModelsNote = "known to aviary; the CLI may accept others"

// modelsNote is the label the report puts on that list: the definition's own
// when it wrote one, because a CLI that routes to a provider needs to say
// which provider the list belongs to, and the general caveat otherwise.
func modelsNote(doctor agents.Doctor) string {
	if note := strings.TrimSpace(doctor.ModelsNote); note != "" {
		return note
	}
	return knownModelsNote
}

// qualify renders models as the agent/model names --agent takes, which is the
// whole point of listing them.
func qualify(name string, models []string) []string {
	qualified := make([]string, 0, len(models))
	for _, model := range models {
		qualified = append(qualified, name+"/"+model)
	}
	return qualified
}

// doctorFieldWidth is the column the report's field values line up in.
const doctorFieldWidth = 8

// writeAgentReports renders the report. It is a plain block per agent rather
// than a table: the model list is the longest thing on the page and the one
// people are here to copy, so it gets its own lines instead of being wrapped
// into a cell.
func writeAgentReports(out io.Writer, reports []agentReport) {
	for i, report := range reports {
		if i > 0 {
			_, _ = fmt.Fprintln(out)
		}
		_, _ = fmt.Fprintf(out, "%-*s %s\n", statusWidth, report.status(), report.name)
		writeAgentField(out, "binary", describeBinary(report))
		if report.projectsNote != "" {
			writeAgentField(out, "projects", "unavailable: "+report.projectsNote)
		}
		if !report.installed() {
			continue
		}
		writeAgentField(out, "auth", report.auth)
		writeAgentField(out, "quota", describeQuota(report))
		if report.defaultModel != "" {
			writeAgentField(out, "default", qualify(report.name, []string{report.defaultModel})[0])
		}
		if report.skillsNote != "" {
			writeAgentField(out, "skills", report.skillsNote)
		}
		writeAgentModels(out, report)
	}
}

// describeQuota reads the agent's quota line. An agent that names no quota
// source is not tracked, which is the deliberate default; one that names a
// source and could not be read is unavailable, which is a problem worth
// telling apart from the default.
func describeQuota(report agentReport) string {
	if text := quotaReading(report.quota, report.tracksQuota, report.quotaErr); text != "" {
		return text
	}
	return "not tracked"
}

// describeBinary says where the CLI is, or that it is not installed, with the
// version and any complaint about it on the same line.
func describeBinary(report agentReport) string {
	if !report.installed() {
		return fmt.Sprintf("%s (not installed)", orDefault(report.binary, doctorUnknown))
	}
	text := report.path
	if report.version != "" {
		text += ", version " + report.version
	}
	if report.versionNote != "" {
		text += " (" + report.versionNote + ")"
	}
	return text
}

// writeAgentModels prints the models one per line, so every name can be copied
// straight into --agent, with the count on the header line for the agents that
// front dozens of them.
func writeAgentModels(out io.Writer, report agentReport) {
	if len(report.models) == 0 {
		writeAgentField(out, "models", orDefault(report.modelNote, doctorUnknown))
		return
	}
	header := fmt.Sprintf("%d available", len(report.models))
	if note := strings.TrimSpace(report.modelNote); note != "" {
		header = fmt.Sprintf("%d %s", len(report.models), note)
	}
	writeAgentField(out, "models", header)
	for _, model := range report.models {
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
