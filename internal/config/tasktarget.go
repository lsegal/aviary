package config

import (
	"fmt"
	"strconv"
	"strings"
)

// CodeLoopTargetPrefix starts a delivery target that names a Code Loop job:
// "codeloop:<project>#<number>", where project is the job's watch target as
// configured (OWNER/REPO or a GitHub project URL) and number its issue. The
// number may be followed by "?" and comma-separated delivery options:
// CodeLoopResume and CodeLoopReopen.
const CodeLoopTargetPrefix = "codeloop:"

// The delivery options of a Code Loop target.
const (
	// CodeLoopResume resumes a paused job when output arrives instead of
	// queuing the output until the job runs again.
	CodeLoopResume = "resume"
	// CodeLoopReopen retries a completed job with the output instead of
	// rejecting it.
	CodeLoopReopen = "reopen"
)

// NotifyTarget delivers a run's result as a notification (#1215). With
// "?"+NotifyFailures it notifies only when the run fails.
const NotifyTarget = "notify"

// NotifyFailures is the Notify target's option to skip successful runs.
const NotifyFailures = "failures"

// The kinds of task and workflow delivery targets.
const (
	TargetSilent   = "silent"
	TargetReply    = "reply"
	TargetSession  = "session"
	TargetChannel  = "channel"
	TargetCodeLoop = "codeloop"
	TargetNotify   = "notify"
)

// TaskTarget is a parsed task or workflow delivery target. ParseTaskTarget is
// the one resolver tasks, workflows, validation and delivery all use.
type TaskTarget struct {
	Kind string
	// Session is the session name or ID of a session target, and Channel the
	// channel name of a channel target.
	Session string
	Channel string
	// Project and Number name the job of a Code Loop target, and Resume and
	// Reopen are its CodeLoopResume and CodeLoopReopen options.
	Project string
	Number  int
	Resume  bool
	Reopen  bool
	// FailuresOnly is a Notify target's NotifyFailures option.
	FailuresOnly bool
}

// String writes the target back in its saved form.
func (t TaskTarget) String() string {
	switch t.Kind {
	case TargetReply:
		return ReplyTarget
	case TargetSession:
		return "session:" + t.Session
	case TargetChannel:
		return ChannelTarget(t.Channel)
	case TargetNotify:
		if t.FailuresOnly {
			return NotifyTarget + "?" + NotifyFailures
		}
		return NotifyTarget
	case TargetCodeLoop:
		target := CodeLoopTarget(t.Project, t.Number)
		var options []string
		if t.Resume {
			options = append(options, CodeLoopResume)
		}
		if t.Reopen {
			options = append(options, CodeLoopReopen)
		}
		if len(options) > 0 {
			target += "?" + strings.Join(options, ",")
		}
		return target
	}
	return ""
}

// CodeLoopTarget returns the delivery target that picks project's job number.
func CodeLoopTarget(project string, number int) string {
	return CodeLoopTargetPrefix + strings.TrimSpace(project) + "#" + strconv.Itoa(number)
}

// ParseTaskTarget resolves a delivery target: empty or "silent", "reply",
// "session:<name-or-id>", "channel:<channel name>",
// "codeloop:<project>#<number>[?resume,reopen]", or "notify[?failures]".
func ParseTaskTarget(target string) (TaskTarget, error) {
	target = strings.TrimSpace(target)
	switch {
	case target == "" || strings.EqualFold(target, TargetSilent):
		return TaskTarget{Kind: TargetSilent}, nil
	case IsReplyTarget(target):
		return TaskTarget{Kind: TargetReply}, nil
	case strings.EqualFold(target, NotifyTarget):
		return TaskTarget{Kind: TargetNotify}, nil
	case strings.EqualFold(target, NotifyTarget+"?"+NotifyFailures):
		return TaskTarget{Kind: TargetNotify, FailuresOnly: true}, nil
	case strings.HasPrefix(target, "session:"):
		ref := strings.TrimSpace(strings.TrimPrefix(target, "session:"))
		if ref == "" {
			return TaskTarget{}, fmt.Errorf("session target %q names no session; use session:<name-or-id>", target)
		}
		return TaskTarget{Kind: TargetSession, Session: ref}, nil
	case strings.HasPrefix(target, CodeLoopTargetPrefix):
		return parseCodeLoopTarget(target)
	}
	if name, ok := ChannelTargetName(target); ok && name != "" {
		return TaskTarget{Kind: TargetChannel, Channel: name}, nil
	}
	return TaskTarget{}, fmt.Errorf("invalid target %q; must be empty, \"silent\", \"reply\" (workflows), session:<name-or-id>, channel:<channel name>, codeloop:<owner/repo>#<number>, \"notify\", or \"notify?failures\"", target)
}

func parseCodeLoopTarget(target string) (TaskTarget, error) {
	ref := strings.TrimSpace(strings.TrimPrefix(target, CodeLoopTargetPrefix))
	hash := strings.LastIndex(ref, "#")
	if hash < 0 {
		return TaskTarget{}, fmt.Errorf("codeloop target %q names no job; use codeloop:<owner/repo>#<number>", target)
	}
	project := strings.TrimSpace(ref[:hash])
	rawNumber, rawOptions, hasOptions := strings.Cut(ref[hash+1:], "?")
	number, err := strconv.Atoi(strings.TrimSpace(rawNumber))
	if project == "" || strings.ContainsAny(project, " \t\r\n") {
		return TaskTarget{}, fmt.Errorf("codeloop target %q names no project; use codeloop:<owner/repo>#<number>", target)
	}
	if err != nil || number <= 0 {
		return TaskTarget{}, fmt.Errorf("codeloop target %q needs a positive job number; use codeloop:<owner/repo>#<number>", target)
	}
	parsed := TaskTarget{Kind: TargetCodeLoop, Project: project, Number: number}
	if !hasOptions {
		return parsed, nil
	}
	for option := range strings.SplitSeq(rawOptions, ",") {
		switch strings.ToLower(strings.TrimSpace(option)) {
		case CodeLoopResume:
			parsed.Resume = true
		case CodeLoopReopen:
			parsed.Reopen = true
		default:
			return TaskTarget{}, fmt.Errorf("codeloop target %q has unknown option %q; options are %q and %q", target, strings.TrimSpace(option), CodeLoopResume, CodeLoopReopen)
		}
	}
	return parsed, nil
}

// HasProject reports whether any agent in agents owns project.
func HasProject(agents []AgentConfig, project string) bool {
	project = strings.TrimSpace(project)
	for _, a := range agents {
		for _, p := range a.Projects {
			if strings.EqualFold(strings.TrimSpace(p.Target), project) {
				return true
			}
		}
	}
	return false
}
