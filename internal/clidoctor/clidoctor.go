// Package clidoctor is aviary's one probe for coding CLIs. It reads a CLI's
// agent definition and asks the installed binary what the definition says to
// ask -- where it is, its version against the declared minimum, whether it is
// signed in, which models it takes, and its quota -- so `aviary doctor`,
// Settings → Providers and the Projects doctor all report from the same code.
package clidoctor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/process"
)

// Text a report carries where a probe could not answer. Unknown is a real
// answer here -- an agent that declares no sign-in probe is not signed out --
// so it is never rendered as a failure.
const (
	Unknown   = "unknown"
	SignedIn  = "signed in"
	SignedOut = "signed out"
)

// Status keys, the machine-readable summary of a report.
const (
	StatusOK      = "ok"
	StatusWarn    = "warn"
	StatusMissing = "missing"
)

// Report is what the doctor knows about one CLI after probing it.
type Report struct {
	Name string
	// Binary is the executable the definition names, and Path is where it was
	// found; an empty Path is a CLI that is not installed.
	Binary string
	Path   string
	// Version is the first line of `binary --version`, and VersionNote says
	// why it is a problem when the definition declares a minimum it misses.
	Version     string
	VersionNote string
	// Auth is the sign-in state, and AuthDetail how the CLI is signed in, or
	// why the probe says it is not, when the definition says how to tell.
	Auth       string
	AuthDetail string
	// Quota is the reading, and TracksQuota says the definition names a quota
	// source at all. The two apart are what tell a CLI that reports no quota
	// by design from one whose reader could not answer, which look the same
	// on the line and mean opposite things. QuotaErr is why the reader could
	// not answer, when it could not.
	Quota       string
	TracksQuota bool
	QuotaErr    error
	// Models are the fully qualified agent/model names --agent accepts, and
	// ModelNote qualifies or replaces that list.
	Models    []string
	ModelNote string
	// DefaultModel is the model aviary runs the agent with when --agent names
	// none, empty for a definition that leaves that to the CLI.
	DefaultModel string
	// SkillsNote says which of aviary's installed skills are stale, empty when
	// they match the running release or the agent installs none.
	SkillsNote string
	// ProjectsNote says why the agent is gated out of Projects jobs, empty
	// when it can run them.
	ProjectsNote string
}

// Installed reports whether the CLI was found on PATH.
func (r Report) Installed() bool { return r.Path != "" }

// StatusKey summarizes the report: missing when the CLI is not installed, a
// warning when it is installed but something about it needs attention, and ok
// otherwise.
func (r Report) StatusKey() string {
	switch {
	case !r.Installed():
		return StatusMissing
	case r.VersionNote != "" || r.Auth == SignedOut || r.SkillsNote != "":
		return StatusWarn
	default:
		return StatusOK
	}
}

// Doctor probes the CLIs a registry defines. Every process it runs is behind
// a function field so tests can answer for a machine with no CLI installed at
// all, which is every machine CI runs on.
type Doctor struct {
	Registry *clidefs.Registry
	// LookPath resolves an executable, Version reads its reported version,
	// and Run executes one probe and returns its output and whether it
	// succeeded.
	LookPath func(string) (string, error)
	Version  func(ctx context.Context, binary string) ([]byte, error)
	Run      func(ctx context.Context, argv []string, spec clidefs.Doctor, stdin []string) ([]byte, bool)
	// Quota reads one agent's quota text, and SkillsNote reports which of
	// aviary's skills installed for the agent are stale. Either may be nil,
	// and the report then leaves that field empty.
	Quota      func(ctx context.Context, name, binary string) (string, error)
	SkillsNote func(name string) string
}

// New builds the doctor that talks to the real machine.
func New(registry *clidefs.Registry) *Doctor {
	return &Doctor{
		Registry: registry,
		LookPath: exec.LookPath,
		Version:  ReadVersion,
		Run:      RunProbe,
	}
}

// ReadVersion runs `binary --version` and returns its combined output.
func ReadVersion(ctx context.Context, binary string) ([]byte, error) {
	return process.CombinedOutput(exec.CommandContext(ctx, binary, "--version"))
}

// Report probes every registered CLI and returns one entry each, in the
// registry's sorted order. The CLIs are probed concurrently: each one costs a
// handful of subprocesses, and a report that ran them one CLI at a time would
// take as long as the slowest of them multiplied by the registry.
func (d *Doctor) Report(ctx context.Context) []Report {
	names := d.Registry.Names()
	reports := make([]Report, len(names))
	var wait sync.WaitGroup
	for i, name := range names {
		wait.Add(1)
		go func() {
			defer wait.Done()
			reports[i] = d.Probe(ctx, name)
		}()
	}
	wait.Wait()
	return reports
}

// Probe probes one CLI. Nothing is run when it is not installed: every
// remaining question is about that binary, and the answers would all be the
// same failure.
func (d *Doctor) Probe(ctx context.Context, name string) Report {
	report := Report{Name: name, Auth: Unknown}
	definition, ok := d.Registry.Lookup(name)
	if !ok {
		return report
	}
	report.Binary = definition.Binary
	report.ProjectsNote = definition.ProjectsUnavailable
	report.DefaultModel = definition.ModelOrDefault("")
	path, err := d.LookPath(definition.Binary)
	if err != nil {
		report.Models, report.ModelNote = declaredModels(definition)
		return report
	}
	report.Path = path
	report.Version, report.VersionNote = d.probeVersion(ctx, definition, path)
	report.TracksQuota = definition.Quota.ReaderName() != clidefs.QuotaNone
	if d.Quota != nil {
		report.Quota, report.QuotaErr = d.Quota(ctx, name, path)
	}
	report.Auth, report.AuthDetail = d.probeAuth(ctx, definition, path, report.Quota)
	report.Models, report.ModelNote = d.probeModels(ctx, definition, path)
	if definition.SkillsTarget() != "" && d.SkillsNote != nil {
		report.SkillsNote = d.SkillsNote(name)
	}
	return report
}

// probeVersion reads the CLI's version and says whether it clears the
// definition's declared minimum. An unreadable version is reported as unknown
// rather than as too old, matching how a dispatch treats it.
func (d *Doctor) probeVersion(ctx context.Context, definition clidefs.Definition, binary string) (version, note string) {
	out, err := d.Version(ctx, binary)
	reported := FirstLine(string(out))
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

// probeAuth resolves the sign-in state. A definition's own probe decides it;
// failing that, a quota reading is proof enough, because every quota reader
// aviary has asks the CLI something only a signed-in account can answer. A
// CLI with neither is reported as unknown rather than guessed at.
func (d *Doctor) probeAuth(ctx context.Context, definition clidefs.Definition, binary, quota string) (state, detail string) {
	if argv := definition.Doctor.AuthArgv(binary); len(argv) > 0 {
		out, ok := d.Run(ctx, argv, definition.Doctor, nil)
		if ok && definition.Doctor.ReportsSignedIn(string(out)) {
			return SignedIn, definition.Doctor.AuthDetailFrom(string(out))
		}
		return SignedOut, ""
	}
	if strings.TrimSpace(quota) != "" {
		return SignedIn, ""
	}
	return Unknown, ""
}

// probeModels lists the models the CLI accepts, preferring what it says over
// what the definition declares: a provider-agnostic CLI's model list is
// whatever it is configured with today, which no static allow-list can keep
// up with.
func (d *Doctor) probeModels(ctx context.Context, definition clidefs.Definition, binary string) ([]string, string) {
	argv := definition.Doctor.ModelsArgv(binary)
	if len(argv) == 0 {
		return declaredModels(definition)
	}
	out, ok := d.Run(ctx, argv, definition.Doctor, definition.Doctor.ModelsStdinLines(binary))
	if models := definition.Doctor.ModelsFrom(string(out)); ok && len(models) > 0 {
		return Qualify(definition.Name, models), ""
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

// declaredModels is what the definition alone can say about a CLI's models:
// its allow-list when it names one, then the models it knows the CLI accepts,
// and otherwise a note, because "the definition validates any model" and "the
// agent takes no model" are different answers and neither is a list.
//
// The known list is qualified with a note rather than presented as the whole
// truth: it is what aviary has been told, not what the CLI enforces, and a
// model that shipped after this definition did still works.
func declaredModels(definition clidefs.Definition) ([]string, string) {
	switch {
	case definition.Models.AcceptsNothing():
		return nil, "not accepted by this agent"
	case definition.Models.Declared():
		return Qualify(definition.Name, definition.Models.Values()), ""
	case len(definition.Doctor.KnownModels) > 0:
		return Qualify(definition.Name, definition.Doctor.KnownModels), modelsNote(definition.Doctor)
	case strings.TrimSpace(definition.Doctor.ModelsNote) != "":
		// A definition that cannot list its models can still say what to
		// write after --agent, which is what the field is read for.
		return nil, strings.TrimSpace(definition.Doctor.ModelsNote)
	}
	return nil, "any model the CLI accepts"
}

// KnownModelsNote qualifies a list aviary declared rather than read from the
// CLI, so nobody reads it as the CLI's own catalog.
const KnownModelsNote = "known to aviary; the CLI may accept others"

// modelsNote is the label the report puts on that list: the definition's own
// when it wrote one, because a CLI that routes to a provider needs to say
// which provider the list belongs to, and the general caveat otherwise.
func modelsNote(doctor clidefs.Doctor) string {
	if note := strings.TrimSpace(doctor.ModelsNote); note != "" {
		return note
	}
	return KnownModelsNote
}

// Qualify renders models as the agent/model names --agent takes, which is the
// whole point of listing them.
func Qualify(name string, models []string) []string {
	qualified := make([]string, 0, len(models))
	for _, model := range models {
		qualified = append(qualified, name+"/"+model)
	}
	return qualified
}

// FirstLine is the part of a --version output worth quoting back: CLIs that
// print a banner or an update notice alongside the number would otherwise
// paste all of it into a report.
func FirstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(line)
}

// probeWaitDelay is how long a cancelled probe is given to let go of its
// output pipes before it is abandoned.
const probeWaitDelay = time.Second

// RunProbe runs one probe under the definition's timeout, reporting its
// combined output and whether it exited zero. A probe that fails is not an
// error the report stops for: it is the answer.
func RunProbe(ctx context.Context, argv []string, spec clidefs.Doctor, stdin []string) ([]byte, bool) {
	if len(argv) == 0 {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(ctx, spec.TimeoutDuration())
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.WaitDelay = probeWaitDelay
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
func runProbeConversation(cmd *exec.Cmd, spec clidefs.Doctor, lines []string) ([]byte, bool) {
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
