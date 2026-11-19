// Package agents describes the coding-agent CLIs glorp can dispatch work to as
// data rather than as code. Every place that has to know how to talk to an
// agent -- the argv for a fresh run, a resume, and a one-shot vision call, the
// environment its child process needs, how its session ID is established, and
// how its output is decoded -- reads a Definition instead of branching on the
// agent's name, so supporting another CLI is a JSON document rather than a set
// of new `if name == ...` arms.
package agents

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Definition is everything glorp needs to know to invoke one agent CLI. It is
// unmarshalled from the built-in JSON documents embedded in this package and
// from the user's own .glorp.config.json.
type Definition struct {
	// Name is the agent name used in --agent name/model:level.
	Name string `json:"name"`
	// Binary is the default executable the agent is invoked through. It is
	// overridable per agent by the run's own binary flags (issue #489).
	Binary string `json:"binary"`
	// MinVersion is the lowest version of Binary the definition's argv
	// templates are known to work with, as a dotted version such as
	// "0.58.0". Declaring it turns an opaque unrecognized-argument failure
	// from an older CLI into an error naming both versions (issue #535). An
	// empty value checks nothing and costs no process.
	MinVersion string `json:"minVersion,omitempty"`
	// Args holds the argv template for each mode glorp invokes.
	Args Args `json:"args"`
	// Env is extra environment for the child process, layered on top of
	// glorp's own environment.
	Env map[string]string `json:"env,omitempty"`
	// Session says how a session ID is established and what happens to it
	// when a resume fails.
	Session Session `json:"session"`
	// Levels and Models are the optional allow-lists used to validate --agent
	// and to say what was expected when it does not match.
	Levels AllowList `json:"levels"`
	Models AllowList `json:"models"`
	// DefaultModel is the model glorp runs this agent with when --agent
	// names no model of its own (issue #612). Agent CLIs mostly default to
	// their largest model, which is the wrong thing to spend a queue of
	// issues on, so glorp picks the mid-tier one instead of leaving the
	// choice to the CLI. An empty value keeps the old behaviour: no
	// {model} is rendered and the CLI decides for itself, which is what a
	// definition whose catalog is fetched for the signed-in account and
	// cannot be named up front has to do -- muse, cline, and opencode name
	// none for that reason (issue #622), since an id glorp wrote down would
	// fail the dispatch on an account that cannot reach it. A default also
	// has to leave the level to {level}: an id with the reasoning level
	// spelled into it renders beside the fragment that names one, which is
	// why agy's default is the id in its catalog that carries none. It must
	// be admitted by Models when that allow-list names values.
	DefaultModel string `json:"defaultModel,omitempty"`
	// Output names how the agent's stdout is decoded: passed through as it
	// is written, decoded as Claude's streaming envelope, or decoded by the
	// generic JSONL decoder configured with the agent's own field paths.
	Output Output `json:"output"`
	// MissingSession are the phrases this agent prints, beyond the shared
	// defaults, when asked to resume a session it no longer holds; they are
	// matched case-insensitively anywhere in its output. Naming one here adds
	// it for this agent alone rather than loosening detection for the others.
	MissingSession []string `json:"missingSession,omitempty"`
	// Quota names the quota source for the agent.
	Quota Quota `json:"quota,omitempty"`
	// Skills names the skills.sh target the agent's copy of the gh-fix,
	// gh-discuss, and gh-create skills is installed for, so the installers
	// derive their --agent list from the registry instead of a hand-edited one.
	Skills Skills `json:"skills,omitempty"`
	// PromptStdin, when declared, says the agent can read its prompt from
	// stdin instead of argv: an argument that is exactly {prompt} renders as
	// these arguments instead, and the prompt is written to the child's stdin.
	// Absent means the agent cannot; [] means the prompt argument is simply
	// left out (Claude's -p reads stdin then), and ["-"] stands in for it
	// (Codex's exec). Out-of-band calls send their large payload this way, so
	// no OS command-line limit can stop the agent from starting (issue #815).
	PromptStdin *[]string `json:"promptStdin,omitempty"`
	// Doctor names the read-only probes `glorp agents` runs to report on the
	// agent: whether its CLI is signed in, and which models it accepts. An
	// agent that declares none is reported from what the definition already
	// says, and runs no process of its own.
	Doctor Doctor `json:"doctor,omitempty"`
}

// AllowList is the set of values one dimension of --agent -- a reasoning
// level or a model -- admits. Three states are distinct, and JSON spells them
// apart, because "the definition names no allow-list" and "the CLI has no such
// flag at all" are different claims: the field absent (or null) admits any
// value, ["low", "high"] admits exactly those, and the empty list [] admits
// none. Without that last state an agent whose CLI takes no reasoning level
// accepts one and then drops it, since its argv template has no {level}
// fragment to render it into -- indistinguishable, at the --agent prompt, from
// a level that was honoured.
type AllowList struct {
	values  []string
	present bool
}

// NewAllowList builds a declared allow-list of the given values. Called with
// none it is the empty list, which admits nothing.
func NewAllowList(values ...string) AllowList {
	return AllowList{values: append([]string(nil), values...), present: true}
}

// Values are the admitted values, nil when the list is absent.
func (a AllowList) Values() []string { return append([]string(nil), a.values...) }

// Declared reports whether the definition named a list at all.
func (a AllowList) Declared() bool { return a.present }

// AcceptsNothing reports the declared-but-empty list: the agent takes no value
// for this dimension.
func (a AllowList) AcceptsNothing() bool { return a.present && len(a.values) == 0 }

// Admits reports whether the list allows a value. An absent list allows any.
func (a AllowList) Admits(value string) bool {
	if !a.present {
		return true
	}
	return contains(a.values, value)
}

// UnmarshalJSON reads the three states. null is the absent list, so a config
// override that resets the field to the schema default keeps meaning "admits
// anything" rather than silently meaning "admits nothing".
func (a *AllowList) UnmarshalJSON(raw []byte) error {
	*a = AllowList{}
	if isJSONNull(raw) {
		return nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return err
	}
	a.values = values
	if a.values == nil {
		a.values = []string{}
	}
	a.present = true
	return nil
}

// MarshalJSON writes those same three states back, so a definition survives
// the marshal-and-merge round trip the agent config file makes it take.
func (a AllowList) MarshalJSON() ([]byte, error) {
	if !a.present {
		return []byte("null"), nil
	}
	if a.values == nil {
		return []byte("[]"), nil
	}
	return json.Marshal(a.values)
}

func (a AllowList) validate(field string) error {
	for _, value := range a.values {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("field %q cannot contain an empty value", field)
		}
	}
	return nil
}

// Args are the argv templates for the three shapes of invocation glorp makes:
// a fresh run, a resume of an earlier session, and the one-shot vision call
// browser mode makes on a screenshot.
//
// Resume may be left out by an agent whose Session.Assign is AssignNone: it
// has no session to continue, so a recovery renders Run again rather than a
// resume template that would only ever be a copy of it.
type Args struct {
	Run    []Fragment `json:"run"`
	Resume []Fragment `json:"resume,omitempty"`
	Vision []Fragment `json:"vision,omitempty"`
}

// Fragment is one conditional piece of an argv template. Its arguments are
// appended in order when its condition holds, with {placeholder} substituted
// from the invocation's values. This is deliberately a small explicit model
// rather than an expression language: a fragment can test one named value for
// presence and nothing else.
type Fragment struct {
	// When names the condition, optionally negated with a leading "!". An
	// empty condition always holds.
	When string `json:"when,omitempty"`
	// Args are the arguments contributed when the condition holds.
	Args []string `json:"args"`
}

// Session describes how the agent's session ID comes to exist.
type Session struct {
	// Assign is "glorp" when glorp generates the ID and passes it to the
	// agent (Claude's --session-id), "capture" when the agent assigns its own
	// and prints it (Codex), or "none" when the agent has no resumable
	// session at all.
	Assign string `json:"assign"`
	// Capture is the regular expression that reads the ID out of the agent's
	// stdout when Assign is "capture". Its first capture group is the ID.
	Capture string `json:"capture,omitempty"`
	// ClearOnResumeFailure drops the recorded ID when a resume fails because
	// the session is gone, so the restarted run takes a fresh one. Agents
	// glorp assigns IDs for keep theirs.
	ClearOnResumeFailure bool `json:"clearOnResumeFailure,omitempty"`
}

// Output names the decoder applied to the agent's stdout.
type Output struct {
	// Format selects the decoder: "text" (alias "plain") shows output as it
	// is written, "stream-json" (alias "claude-stream-json") decodes Claude's
	// streaming event envelope, and "jsonl" decodes a line-delimited JSON
	// event stream described by the JSONL block below.
	Format string `json:"format"`
	// JSONL configures the generic decoder, and is only meaningful when
	// Format selects it. Most agents with a --json or streaming mode are read
	// by naming their field paths here rather than by new Go code.
	JSONL *JSONL `json:"jsonl,omitempty"`
}

// JSONL says where in one event of a line-delimited JSON stream the decoder
// finds what it renders. Every field is a path of dot-separated object keys,
// where a key suffixed with [] steps into an array and continues into each of
// its elements: "message.content[].text" reads the text of every content
// block. A path naming nothing in an event contributes nothing, so an event
// shape the definition does not describe is passed over rather than failing
// the stream.
type JSONL struct {
	// Type is the path to the event's type, used only to skip the types in
	// Ignore. An empty path ignores nothing.
	Type string `json:"type,omitempty"`
	// Text is the path to the human-readable text an event carries.
	Text string `json:"text,omitempty"`
	// ToolName and ToolInput are the paths to a tool call's name and to its
	// input object, rendered as the "Running: ..." progress lines glorp shows
	// for Claude. Paths that share a prefix are paired element by element, so
	// a name and its own input come from the same block.
	ToolName  string `json:"toolName,omitempty"`
	ToolInput string `json:"toolInput,omitempty"`
	// ToolNamePrefix narrows what counts as a tool call: only a name
	// starting with it is one, and the prefix is trimmed off what is
	// rendered. A stream whose name field doubles as a kind shared with
	// non-tool turns ("tool.read_file" beside "model.meta.response") is read
	// by naming the prefix that tells them apart. It needs ToolName.
	ToolNamePrefix string `json:"toolNamePrefix,omitempty"`
	// Ignore lists the event types dropped before anything is read out of
	// them, for the bookkeeping events a stream repeats on every turn.
	Ignore []string `json:"ignore,omitempty"`
	// TextDelta says the text an event carries is a fragment of a message
	// rather than a whole one, as a CLI that streams token-sized deltas
	// emits. The decoder then buffers what Text names and flushes it as one
	// line on the next event carrying no text or at the end of the stream,
	// instead of terminating a line per event. It needs Text.
	TextDelta bool `json:"textDelta,omitempty"`
	// Events overrides the paths above for one event type, keyed by the
	// value Type names. An envelope that spreads one logical event over
	// several typed events cannot be described by a single set of paths: a
	// path pointed at where one type keeps its tool name reads something
	// else entirely, or nothing, on every other type. Naming the type the
	// paths belong to is what lets the decoder read each shape as itself. It
	// needs Type.
	Events map[string]JSONLEvent `json:"events,omitempty"`
}

// JSONLEvent are the paths the decoder reads out of one event type, replacing
// the ones the JSONL block names for every other type. A field left empty
// keeps what the block already says, so an override names only what differs.
// Text is overridden on its own, while ToolName carries ToolInput and
// ToolNamePrefix with it: a call's name, its input, and what marks it as a
// call are one description, and pairing a name from one event type with an
// input path meant for another renders another call's arguments.
type JSONLEvent struct {
	// Text is the path to the human-readable text this event type carries.
	Text string `json:"text,omitempty"`
	// ToolName, ToolInput, and ToolNamePrefix describe this event type's
	// tool call exactly as the JSONL block's own fields of those names do.
	ToolName       string `json:"toolName,omitempty"`
	ToolInput      string `json:"toolInput,omitempty"`
	ToolNamePrefix string `json:"toolNamePrefix,omitempty"`
}

// Skills names where an agent reads glorp's skills from.
type Skills struct {
	// Target is the skills.sh target id `skills add --agent` takes: a
	// dedicated one such as "codex" or "claude-code", or "universal" for a
	// CLI skills.sh has no dedicated id for.
	Target string `json:"target,omitempty"`
}

// Quota names the quota source for the agent. An agent that declares none
// reports no quota at all, which the UI shows as untracked; that is the
// deliberate default, and it costs no process on any poll.
type Quota struct {
	// Reader selects the reader: "none" (the default), "codex", "claude", or
	// the generic "command" reader below.
	Reader string `json:"reader,omitempty"`
	// Command is the argv the "command" reader runs, whose stdout it parses
	// as JSON. The {binary} placeholder substitutes the executable the agent
	// itself was resolved to, so --agent-binary reaches the quota call too.
	Command []string `json:"command,omitempty"`
	// PercentUsed and ResetAt are dotted paths into that JSON naming the
	// percentage of the window already consumed and the time it resets.
	// Either may be omitted when the format does not reference it.
	PercentUsed string `json:"percentUsed,omitempty"`
	ResetAt     string `json:"resetAt,omitempty"`
	// Format is the status-bar template, with {percentUsed}, {percentLeft},
	// and {resetAt} substituted from the fields above.
	Format string `json:"format,omitempty"`
	// Timeout bounds one read, as a Go duration string. A quota call is a
	// status-bar nicety, so it is never allowed to hang a poll.
	Timeout string `json:"timeout,omitempty"`
}

// Quota reader names.
const (
	QuotaNone    = "none"
	QuotaCodex   = "codex"
	QuotaClaude  = "claude"
	QuotaCommand = "command"
)

// DefaultQuotaTimeout bounds one generic quota read when the definition names
// no timeout of its own.
const DefaultQuotaTimeout = 30 * time.Second

// defaultQuotaFormat is the template the generic reader renders when the
// definition names none, matching the shape the built-in readers report in.
const defaultQuotaFormat = "{percentLeft}% left"

// quotaPlaceholders is every {name} a quota format may substitute.
var quotaPlaceholders = []string{"percentUsed", "percentLeft", "resetAt"}

// quotaReaders is every reader name the schema accepts.
var quotaReaders = []string{QuotaNone, QuotaCodex, QuotaClaude, QuotaCommand}

// ReaderName is the reader in force, resolving the empty default to "none".
func (q Quota) ReaderName() string {
	if strings.TrimSpace(q.Reader) == "" {
		return QuotaNone
	}
	return q.Reader
}

// FormatTemplate is the template the generic reader renders.
func (q Quota) FormatTemplate() string {
	if strings.TrimSpace(q.Format) == "" {
		return defaultQuotaFormat
	}
	return q.Format
}

// TimeoutDuration bounds one generic quota read. Validate has already
// rejected an unparseable value, so a bad one here falls back rather than
// reporting a second time.
func (q Quota) TimeoutDuration() time.Duration {
	if strings.TrimSpace(q.Timeout) == "" {
		return DefaultQuotaTimeout
	}
	timeout, err := time.ParseDuration(q.Timeout)
	if err != nil || timeout <= 0 {
		return DefaultQuotaTimeout
	}
	return timeout
}

// Argv renders the generic reader's command, substituting {binary} with the
// executable the agent was resolved to.
func (q Quota) Argv(binary string) []string { return substituteBinary(q.Command, binary) }

// substituteBinary renders an argv template that names the agent's executable
// as {binary}, so a --agent-binary override reaches every command a definition
// declares rather than only the one it was written for.
func substituteBinary(argv []string, binary string) []string {
	if len(argv) == 0 {
		return nil
	}
	rendered := make([]string, 0, len(argv))
	for _, arg := range argv {
		rendered = append(rendered, strings.ReplaceAll(arg, "{binary}", binary))
	}
	return rendered
}

// validate reports the first thing wrong with the quota block. Fields that
// belong to the generic reader are rejected on the others rather than
// ignored, because a "quota" block that silently does nothing looks exactly
// like a working one until the status bar stays empty.
func (q Quota) validate() error {
	reader := q.ReaderName()
	if !contains(quotaReaders, reader) {
		return fmt.Errorf(`field "quota.reader": %q must be %s`, q.Reader, JoinOr(quotaReaders))
	}
	if reader != QuotaCommand {
		for field, set := range map[string]bool{
			"quota.command": len(q.Command) > 0, "quota.percentUsed": q.PercentUsed != "",
			"quota.resetAt": q.ResetAt != "", "quota.format": q.Format != "",
			"quota.timeout": q.Timeout != "",
		} {
			if set {
				return fmt.Errorf(`field %q is only meaningful when "quota.reader" is %q`, field, QuotaCommand)
			}
		}
		return nil
	}
	if len(q.Command) == 0 {
		return fmt.Errorf(`field "quota.command" is required when "quota.reader" is %q`, QuotaCommand)
	}
	for _, arg := range q.Command {
		if strings.TrimSpace(arg) == "" {
			return fmt.Errorf(`field "quota.command" cannot contain an empty argument`)
		}
	}
	for _, match := range placeholderPattern.FindAllStringSubmatch(q.FormatTemplate(), -1) {
		if !contains(quotaPlaceholders, match[1]) {
			return fmt.Errorf(`field "quota.format": unknown placeholder %s; known placeholders are {%s}`, match[0], strings.Join(sorted(quotaPlaceholders), "}, {"))
		}
	}
	if q.PercentUsed == "" && strings.Contains(q.FormatTemplate(), "{percent") {
		return fmt.Errorf(`field "quota.percentUsed" is required when "quota.format" substitutes a percentage`)
	}
	if q.ResetAt == "" && strings.Contains(q.FormatTemplate(), "{resetAt}") {
		return fmt.Errorf(`field "quota.resetAt" is required when "quota.format" substitutes {resetAt}`)
	}
	if strings.TrimSpace(q.Timeout) != "" {
		timeout, err := time.ParseDuration(q.Timeout)
		if err != nil {
			return fmt.Errorf(`field "quota.timeout": %w`, err)
		}
		if timeout <= 0 {
			return fmt.Errorf(`field "quota.timeout": %q must be positive`, q.Timeout)
		}
	}
	return nil
}

// Doctor names the read-only commands `glorp agents` runs to report on one
// agent. Both are optional and neither is ever run by a dispatch: the report
// is the only caller, so a definition that declares nothing here still lists,
// and an agent glorp has never heard of gains a sign-in check and a model list
// by naming two commands in a config file rather than by a code change.
//
// A command must be non-interactive and must not change anything. A CLI whose
// only sign-in check starts a device-code flow has no usable probe, and is
// better left undeclared -- the report says the state is unknown, which is
// true, instead of logging somebody in for asking.
type Doctor struct {
	// Auth is the argv whose exit status reports whether the CLI is signed
	// in, with {binary} substituted as everywhere else. A command that exits
	// zero is signed in unless SignedIn says otherwise.
	Auth []string `json:"auth,omitempty"`
	// SignedIn is a regular expression the auth command's output has to match
	// for the agent to count as signed in. It is for the CLIs that report a
	// signed-out account on a zero exit status, where the exit code alone
	// says nothing. It needs Auth.
	SignedIn string `json:"signedIn,omitempty"`
	// Models is the argv that lists the models the agent accepts, one per
	// line on its stdout, or in JSON when ModelsJSON says where. It is what
	// makes a provider-agnostic CLI usable from --agent: the report renders
	// each name as the fully qualified agent/model name rather than leaving
	// the caller to guess it.
	Models []string `json:"models,omitempty"`
	// ModelsStdin is what the probe writes to that command's stdin, one line
	// each, for a CLI whose model list is only reachable over a stdio
	// protocol: several of them answer a JSON-RPC handshake rather than a
	// listing subcommand, and a probe that cannot talk back cannot ask. The
	// probe holds the pipe open and stops reading the moment the output
	// yields a list, so an agent that would otherwise sit waiting for a
	// client is answered and then shut down. It needs Models.
	ModelsStdin []string `json:"modelsStdin,omitempty"`
	// ModelsJSON is where the model ids are in that command's output, as a
	// dotted path whose `[]` walks an array and whose `[key=value]` walks
	// only the elements a field marks -- "models[visibility=list].slug",
	// "result.models.availableModels[].modelId". The whole output is read as
	// one document first and then line by line, so it fits both a catalog
	// command and an agent that prints a JSON-RPC response per line. It needs
	// Models, and it replaces the line reading ModelPattern does.
	ModelsJSON string `json:"modelsJSON,omitempty"`
	// ModelPattern narrows what counts as a model in that output, for a
	// command that decorates its list. Only a line the expression matches is
	// a model, and its first capture group, when it has one, is the model id.
	// It needs Models.
	ModelPattern string `json:"modelPattern,omitempty"`
	// KnownModels are model names the definition itself carries, for a CLI
	// with no way at all to be asked. No built-in declares any: a list
	// written into glorp is stale the morning after a vendor ships, which is
	// what issue #566 was, so a definition that can reach its CLI declares
	// Models instead. It survives for a config file describing a CLI glorp
	// has never seen, and it is deliberately not an allow-list -- nothing
	// validates against it -- so the report labels it as what glorp was told
	// rather than as what the CLI takes.
	KnownModels []string `json:"knownModels,omitempty"`
	// ModelsNote replaces the label the report puts on the models field. It
	// is what a definition says instead of a list: the caveat on a known list
	// that belongs to one provider out of many, or, for a CLI that neither
	// lists its models nor has a list worth freezing, what to write after
	// --agent and why the report cannot enumerate it.
	ModelsNote string `json:"modelsNote,omitempty"`
	// Timeout bounds one probe, as a Go duration string. The report is a
	// diagnostic, so a CLI that hangs is abandoned and reported as unknown
	// rather than allowed to hold the whole listing up.
	Timeout string `json:"timeout,omitempty"`
}

// DefaultDoctorTimeout bounds one probe when the definition names none.
const DefaultDoctorTimeout = 20 * time.Second

// AuthArgv and ModelsArgv render the probes against the executable the agent
// was resolved to.
func (d Doctor) AuthArgv(binary string) []string { return substituteBinary(d.Auth, binary) }

// ModelsArgv renders the model-listing probe.
func (d Doctor) ModelsArgv(binary string) []string { return substituteBinary(d.Models, binary) }

// ModelsStdinLines is what that probe writes to the command's stdin, with
// {binary} substituted as everywhere else.
func (d Doctor) ModelsStdinLines(binary string) []string {
	return substituteBinary(d.ModelsStdin, binary)
}

// TimeoutDuration bounds one probe. Validate has already rejected an
// unparseable value, so a bad one here falls back rather than reporting twice.
func (d Doctor) TimeoutDuration() time.Duration {
	if strings.TrimSpace(d.Timeout) == "" {
		return DefaultDoctorTimeout
	}
	timeout, err := time.ParseDuration(d.Timeout)
	if err != nil || timeout <= 0 {
		return DefaultDoctorTimeout
	}
	return timeout
}

// ReportsSignedIn reads the auth probe's output. With no SignedIn expression
// the exit status has already decided, so any output counts.
func (d Doctor) ReportsSignedIn(output string) bool {
	if strings.TrimSpace(d.SignedIn) == "" {
		return true
	}
	expr, err := regexp.Compile(d.SignedIn)
	if err != nil {
		return false
	}
	return expr.MatchString(output)
}

// ModelsFrom reads the model list out of the probe's output, one model per
// line, in the order the CLI printed them and without duplicates. A line the
// ModelPattern rejects is dropped, and its first capture group, when it has
// one, is what the line contributes.
func (d Doctor) ModelsFrom(output string) []string {
	var expr *regexp.Regexp
	if pattern := strings.TrimSpace(d.ModelPattern); pattern != "" {
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return nil
		}
		expr = compiled
	}
	seen := make(map[string]bool)
	models := make([]string, 0, 16)
	for _, candidate := range d.modelCandidates(output) {
		model := strings.TrimSpace(candidate)
		if expr != nil {
			match := expr.FindStringSubmatch(model)
			if match == nil {
				continue
			}
			if len(match) > 1 {
				model = strings.TrimSpace(match[1])
			} else {
				model = strings.TrimSpace(match[0])
			}
		}
		if model == "" || seen[model] {
			continue
		}
		seen[model] = true
		models = append(models, model)
	}
	return models
}

// modelCandidates is what the probe's output offers before any pattern
// narrows it: the ids ModelsJSON points at when the definition named a path,
// and every line otherwise.
func (d Doctor) modelCandidates(output string) []string {
	if path := strings.TrimSpace(d.ModelsJSON); path != "" {
		return modelsFromJSON(path, output)
	}
	return strings.Split(output, "\n")
}

// validate reports the first thing wrong with the doctor block. A field that
// belongs to a probe the definition does not declare is rejected rather than
// ignored, on the same grounds as the quota block: a probe that silently does
// nothing looks exactly like a working one.
func (d Doctor) validate() error {
	for field, argv := range map[string][]string{"doctor.auth": d.Auth, "doctor.models": d.Models} {
		for _, arg := range argv {
			if strings.TrimSpace(arg) == "" {
				return fmt.Errorf("field %q cannot contain an empty argument", field)
			}
		}
	}
	for _, model := range d.KnownModels {
		if strings.TrimSpace(model) == "" {
			return fmt.Errorf(`field "doctor.knownModels" cannot contain an empty value`)
		}
	}
	if len(d.Auth) == 0 && strings.TrimSpace(d.SignedIn) != "" {
		return fmt.Errorf(`field "doctor.signedIn" is only meaningful alongside "doctor.auth"`)
	}
	for field, declared := range map[string]bool{
		"doctor.modelPattern": strings.TrimSpace(d.ModelPattern) != "",
		"doctor.modelsJSON":   strings.TrimSpace(d.ModelsJSON) != "",
		"doctor.modelsStdin":  len(d.ModelsStdin) > 0,
	} {
		if declared && len(d.Models) == 0 {
			return fmt.Errorf(`field %q is only meaningful alongside "doctor.models"`, field)
		}
	}
	for _, line := range d.ModelsStdin {
		if strings.TrimSpace(line) == "" {
			return fmt.Errorf(`field "doctor.modelsStdin" cannot contain an empty line`)
		}
	}
	if path := strings.TrimSpace(d.ModelsJSON); path != "" {
		if _, err := parseModelPath(path); err != nil {
			return fmt.Errorf(`field "doctor.modelsJSON": %w`, err)
		}
	}
	for field, pattern := range map[string]string{"doctor.signedIn": d.SignedIn, "doctor.modelPattern": d.ModelPattern} {
		if strings.TrimSpace(pattern) == "" {
			continue
		}
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("field %q: %w", field, err)
		}
	}
	if strings.TrimSpace(d.Timeout) != "" {
		timeout, err := time.ParseDuration(d.Timeout)
		if err != nil {
			return fmt.Errorf(`field "doctor.timeout": %w`, err)
		}
		if timeout <= 0 {
			return fmt.Errorf(`field "doctor.timeout": %q must be positive`, d.Timeout)
		}
	}
	return nil
}

// Assign values.
const (
	AssignGlorp   = "glorp"
	AssignCapture = "capture"
	AssignNone    = "none"
)

// Output formats. Each canonical value has an alias spelled the way the agent
// documentation spells it, so a definition may say either.
const (
	FormatText       = "text"
	FormatStreamJSON = "stream-json"
	FormatJSONL      = "jsonl"

	formatPlainAlias      = "plain"
	formatClaudeJSONAlias = "claude-stream-json"
)

// outputFormats maps every accepted spelling to its canonical format.
var outputFormats = map[string]string{
	FormatText:            FormatText,
	formatPlainAlias:      FormatText,
	FormatStreamJSON:      FormatStreamJSON,
	formatClaudeJSONAlias: FormatStreamJSON,
	FormatJSONL:           FormatJSONL,
}

// outputFormatNames lists the accepted spellings for an error message.
func outputFormatNames() []string {
	names := make([]string, 0, len(outputFormats))
	for name := range outputFormats {
		names = append(names, name)
	}
	return sorted(names)
}

// DefaultMissingSessionPatterns are the messages agents print when asked to
// resume a session they no longer have on disk (sessions expire, and a glorp
// work state file routinely outlives the agent's local conversation history).
// A definition that names none is detected by these.
var DefaultMissingSessionPatterns = []string{
	"no conversation found",
	"no session found",
	"session not found",
	"could not find session",
	"unable to find session",
}

// jsonlPathPattern is the shape a JSONL field path may take: dot-separated
// object keys, each optionally stepping into an array with a [] suffix.
var jsonlPathPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*(\[\])?(\.[A-Za-z_][A-Za-z0-9_-]*(\[\])?)*$`)

// Placeholder names accepted inside a fragment's arguments.
const (
	placeholderPrompt      = "prompt"
	placeholderSession     = "session"
	placeholderModel       = "model"
	placeholderLevel       = "level"
	placeholderImage       = "image"
	placeholderSettings    = "settings"
	placeholderSessionName = "sessionName"
)

// knownPlaceholders is every {name} a fragment may substitute.
var knownPlaceholders = []string{
	placeholderPrompt, placeholderSession, placeholderModel, placeholderLevel,
	placeholderImage, placeholderSettings, placeholderSessionName,
}

// knownConditions is every name a fragment's `when` may test. The value
// placeholders test as present when non-empty; yolo and remoteControl test the
// run's own settings, oneShot tests for an out-of-band call, and noCheckout
// tests for a call running outside any checkout.
var knownConditions = append([]string{"yolo", "remoteControl", "oneShot", "noCheckout"}, knownPlaceholders...)

// placeholderPattern finds {name} spans inside a template argument.
var placeholderPattern = regexp.MustCompile(`\{([A-Za-z][A-Za-z0-9]*)\}`)

// skillsTargetPattern is the shape a skills.sh target id may take. The set of
// ids skills.sh knows grows without glorp, so the shape is checked and the
// value is not: an id glorp has never heard of is the user's to pass on.
var skillsTargetPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// namePattern is the shape an agent name may take. It has to survive being
// written in --agent name/model:level, so it excludes the separators that
// syntax uses.
var namePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// Validate reports the first thing wrong with the definition, naming the field
// it was found in. A definition that does not validate is never registered: a
// silently dropped agent looks exactly like a typo in --agent.
func (d Definition) Validate() error {
	if strings.TrimSpace(d.Name) == "" {
		return fmt.Errorf(`field "name" is required`)
	}
	if !namePattern.MatchString(d.Name) {
		return fmt.Errorf(`field "name": %q must be letters, digits, dot, dash, or underscore`, d.Name)
	}
	if strings.TrimSpace(d.Binary) == "" {
		return fmt.Errorf(`field "binary" is required`)
	}
	if min := strings.TrimSpace(d.MinVersion); min != "" {
		if _, ok := ParseVersion(min); !ok {
			return fmt.Errorf(`field "minVersion": %q must be a dotted version such as "0.58.0"`, d.MinVersion)
		}
	}
	if len(d.Args.Run) == 0 {
		return fmt.Errorf(`field "args.run" is required`)
	}
	// A resume template is required of every agent glorp can hold a session
	// ID for. An agent that declares session.assign "none" has no session to
	// resume, so it may leave args.resume out and have a recovery re-run its
	// run template instead of duplicating it verbatim.
	if len(d.Args.Resume) == 0 && d.Session.Assign != AssignNone {
		return fmt.Errorf(`field "args.resume" is required unless "session.assign" is %q`, AssignNone)
	}
	for mode, fragments := range map[string][]Fragment{
		"args.run": d.Args.Run, "args.resume": d.Args.Resume, "args.vision": d.Args.Vision,
	} {
		if err := validateFragments(mode, fragments); err != nil {
			return err
		}
	}
	if d.PromptStdin != nil {
		for _, arg := range *d.PromptStdin {
			if placeholderPattern.MatchString(arg) {
				return fmt.Errorf(`field "promptStdin": %q cannot contain a placeholder`, arg)
			}
		}
		if !hasPromptArg(d.Args.Run) {
			return fmt.Errorf(`field "promptStdin" needs an argument that is exactly {prompt} in "args.run"`)
		}
	}
	switch d.Session.Assign {
	case AssignGlorp, AssignNone:
		if d.Session.Capture != "" {
			return fmt.Errorf(`field "session.capture" is only meaningful when "session.assign" is %q`, AssignCapture)
		}
	case AssignCapture:
		if strings.TrimSpace(d.Session.Capture) == "" {
			return fmt.Errorf(`field "session.capture" is required when "session.assign" is %q`, AssignCapture)
		}
		expr, err := regexp.Compile(d.Session.Capture)
		if err != nil {
			return fmt.Errorf(`field "session.capture": %w`, err)
		}
		if expr.NumSubexp() < 1 {
			return fmt.Errorf(`field "session.capture": %q has no capture group for the session ID`, d.Session.Capture)
		}
	default:
		return fmt.Errorf(`field "session.assign": %q must be %q, %q, or %q`, d.Session.Assign, AssignGlorp, AssignCapture, AssignNone)
	}
	if err := d.Output.validate(); err != nil {
		return err
	}
	for _, pattern := range d.MissingSession {
		if strings.TrimSpace(pattern) == "" {
			return fmt.Errorf(`field "missingSession" cannot contain an empty value`)
		}
	}
	if err := d.Levels.validate("levels"); err != nil {
		return err
	}
	if err := d.Models.validate("models"); err != nil {
		return err
	}
	if model := strings.TrimSpace(d.DefaultModel); model != "" && !d.AcceptsModel(model) {
		return fmt.Errorf(`field "defaultModel": %q is not one this agent accepts; %s`, model, d.ModelError())
	}
	if err := d.Quota.validate(); err != nil {
		return err
	}
	if err := d.Doctor.validate(); err != nil {
		return err
	}
	if target := d.Skills.Target; target != "" && !skillsTargetPattern.MatchString(target) {
		return fmt.Errorf(`field "skills.target": %q must be a skills.sh target id: lowercase letters, digits, and dashes`, target)
	}
	for key := range d.Env {
		if strings.TrimSpace(key) == "" || strings.Contains(key, "=") {
			return fmt.Errorf(`field "env": %q is not a usable variable name`, key)
		}
	}
	return nil
}

// hasPromptArg reports whether a template passes the prompt as an argument of
// its own, which is the argument promptStdin replaces.
func hasPromptArg(fragments []Fragment) bool {
	for _, fragment := range fragments {
		if contains(fragment.Args, "{"+placeholderPrompt+"}") {
			return true
		}
	}
	return false
}

// ReadsPromptFromStdin reports whether the agent can take its prompt on stdin.
func (d Definition) ReadsPromptFromStdin() bool { return d.PromptStdin != nil }

func validateFragments(mode string, fragments []Fragment) error {
	for i, fragment := range fragments {
		if condition := strings.TrimPrefix(fragment.When, "!"); condition != "" && !contains(knownConditions, condition) {
			return fmt.Errorf(`field %q[%d].when: unknown condition %q; known conditions are %s`, mode, i, fragment.When, strings.Join(sorted(knownConditions), ", "))
		}
		if len(fragment.Args) == 0 {
			return fmt.Errorf(`field %q[%d].args cannot be empty`, mode, i)
		}
		for _, arg := range fragment.Args {
			for _, match := range placeholderPattern.FindAllStringSubmatch(arg, -1) {
				if !contains(knownPlaceholders, match[1]) {
					return fmt.Errorf(`field %q[%d].args: unknown placeholder %s; known placeholders are {%s}`, mode, i, match[0], strings.Join(sorted(knownPlaceholders), "}, {"))
				}
			}
		}
	}
	return nil
}

// SkillsTarget returns the skills.sh target id skills are installed for on
// this agent's behalf, empty when the definition names none.
func (d Definition) SkillsTarget() string { return d.Skills.Target }

// AcceptsLevel and AcceptsModel report whether the definition's allow-lists
// admit a value.
func (d Definition) AcceptsLevel(level string) bool { return d.Levels.Admits(level) }

// AcceptsModel reports whether the definition's model allow-list admits a model.
func (d Definition) AcceptsModel(model string) bool { return d.Models.Admits(model) }

// ModelOrDefault is the model one dispatch runs with: the one the --agent spec
// named, or the definition's managed default when it named none. It is the
// single place the fallback happens, so the model glorp chose is the one the
// argv, the dashboard, and the persisted work state all agree on rather than
// something only the agent's own CLI knows.
func (d Definition) ModelOrDefault(model string) string {
	if strings.TrimSpace(model) != "" {
		return model
	}
	return strings.TrimSpace(d.DefaultModel)
}

// LevelError says why AcceptsLevel refused, in the shape --agent reports it.
// An agent that takes no level at all is named rather than being told to pick
// from an empty set, which is the whole point of declaring the empty list.
func (d Definition) LevelError() error {
	if d.Levels.AcceptsNothing() {
		return fmt.Errorf("agent %s takes no reasoning level", d.Name)
	}
	return fmt.Errorf("agent level must be %s", JoinOr(d.Levels.Values()))
}

// ModelError is LevelError for the model dimension.
func (d Definition) ModelError() error {
	if d.Models.AcceptsNothing() {
		return fmt.Errorf("agent %s takes no model", d.Name)
	}
	return fmt.Errorf("agent model must be %s", JoinOr(d.Models.Values()))
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func sorted(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

// JoinOr renders a list the way an error message wants to read it: "low,
// medium, or high".
func JoinOr(values []string) string {
	switch len(values) {
	case 0:
		return ""
	case 1:
		return values[0]
	case 2:
		return values[0] + " or " + values[1]
	}
	return strings.Join(values[:len(values)-1], ", ") + ", or " + values[len(values)-1]
}
