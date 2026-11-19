package agents

import (
	"regexp"
	"strings"
)

// Mode names one of the three argv templates a definition carries.
type Mode string

const (
	// ModeRun is a fresh dispatch.
	ModeRun Mode = "run"
	// ModeResume continues an earlier session.
	ModeResume Mode = "resume"
	// ModeVision is browser mode's one-shot screenshot read.
	ModeVision Mode = "vision"
)

// Values are the substitutions one invocation makes. A value left empty is
// absent: fragments guarded on it are skipped rather than rendering an empty
// argument.
type Values struct {
	// Prompt is the text the agent is asked to act on.
	Prompt string
	// Session is the session ID being assigned or resumed.
	Session string
	// Model and Level are the agent spec's model and reasoning level.
	Model, Level string
	// Image is the screenshot path a vision call reads.
	Image string
	// Settings and SessionName carry Claude's Remote Control payload and the
	// name a run appears under.
	Settings, SessionName string
	// Yolo and RemoteControl mirror the run's own flags.
	Yolo, RemoteControl bool
	// PromptStdin renders the prompt argument as the definition's
	// promptStdin arguments, because the caller writes the prompt to stdin.
	PromptStdin bool
	// OneShot marks an out-of-band call, such as the prompt agent or the
	// unblock agent, which runs outside any checkout.
	OneShot bool
	// NoCheckout marks a call that runs outside any checkout: every one-shot
	// call, and a dispatched run whose checkout directory does not exist yet.
	NoCheckout bool
}

func (v Values) value(name string) string {
	switch name {
	case placeholderPrompt:
		return v.Prompt
	case placeholderSession:
		return v.Session
	case placeholderModel:
		return v.Model
	case placeholderLevel:
		return v.Level
	case placeholderImage:
		return v.Image
	case placeholderSettings:
		return v.Settings
	case placeholderSessionName:
		return v.SessionName
	}
	return ""
}

// holds reports whether a condition name (already stripped of its negation) is
// satisfied by the values.
func (v Values) holds(condition string) bool {
	switch condition {
	case "yolo":
		return v.Yolo
	case "remoteControl":
		return v.RemoteControl
	case "oneShot":
		return v.OneShot
	case "noCheckout":
		return v.NoCheckout
	}
	return v.value(condition) != ""
}

// Render builds the argv for one mode. An unknown mode, or a mode the
// definition declares no template for, renders nothing, which the caller
// reports rather than running the agent with a bare prompt.
func (d Definition) Render(mode Mode, values Values) []string {
	var fragments []Fragment
	switch mode {
	case ModeRun:
		fragments = d.Args.Run
	case ModeResume:
		fragments = d.resumeFragments()
	case ModeVision:
		fragments = d.Args.Vision
	}
	if len(fragments) == 0 {
		return nil
	}
	args := make([]string, 0, len(fragments)+4)
	for _, fragment := range fragments {
		if !fragmentApplies(fragment, values) {
			continue
		}
		for _, arg := range fragment.Args {
			if values.PromptStdin && d.PromptStdin != nil && arg == "{"+placeholderPrompt+"}" {
				args = append(args, (*d.PromptStdin)...)
				continue
			}
			args = append(args, substitute(arg, values))
		}
	}
	return args
}

// Supports reports whether the definition declares a template for a mode.
func (d Definition) Supports(mode Mode) bool {
	switch mode {
	case ModeRun:
		return len(d.Args.Run) > 0
	case ModeResume:
		return len(d.resumeFragments()) > 0
	case ModeVision:
		return len(d.Args.Vision) > 0
	}
	return false
}

// resumeFragments is the template a resume renders. An agent that declares no
// resumable session (session.assign "none") may leave args.resume out, and
// falls back to its run template: glorp only ever resumes such an agent to
// recover work, and the recovery prompt makes that re-run do the recovering.
// The fallback is explicit here because rendering nothing fails the job.
func (d Definition) resumeFragments() []Fragment {
	if len(d.Args.Resume) == 0 && d.Session.Assign == AssignNone {
		return d.Args.Run
	}
	return d.Args.Resume
}

func fragmentApplies(fragment Fragment, values Values) bool {
	condition := fragment.When
	if condition == "" {
		return true
	}
	if negated := strings.HasPrefix(condition, "!"); negated {
		return !values.holds(strings.TrimPrefix(condition, "!"))
	}
	return values.holds(condition)
}

func substitute(arg string, values Values) string {
	return placeholderPattern.ReplaceAllStringFunc(arg, func(match string) string {
		name := strings.Trim(match, "{}")
		if !contains(knownPlaceholders, name) {
			return match
		}
		return values.value(name)
	})
}

// SessionPattern compiles the definition's session-capture expression, or
// returns nil when the agent does not print its session ID. The definition has
// already been validated, so a pattern that is present compiles.
func (d Definition) SessionPattern() *regexp.Regexp {
	if d.Session.Assign != AssignCapture || d.Session.Capture == "" {
		return nil
	}
	expr, err := regexp.Compile(d.Session.Capture)
	if err != nil {
		return nil
	}
	return expr
}

// AssignsSessionID reports whether glorp generates the session ID and hands it
// to the agent, rather than reading one back from the agent's own output.
func (d Definition) AssignsSessionID() bool { return d.Session.Assign == AssignGlorp }

// CapturesSessionID reports whether the agent prints a session ID for glorp to
// read.
func (d Definition) CapturesSessionID() bool { return d.Session.Assign == AssignCapture }
