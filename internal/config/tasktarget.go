package config

import (
	"fmt"
	"strconv"
	"strings"
)

// CodeLoopTargetPrefix starts a delivery target that names a Code Loop job:
// "codeloop:<project>#<number>", where project is the job's watch target as
// configured (OWNER/REPO or a GitHub project URL) and number its issue.
const CodeLoopTargetPrefix = "codeloop:"

// The kinds of task and workflow delivery targets.
const (
	TargetSilent   = "silent"
	TargetReply    = "reply"
	TargetSession  = "session"
	TargetChannel  = "channel"
	TargetCodeLoop = "codeloop"
)

// TaskTarget is a parsed task or workflow delivery target. ParseTaskTarget is
// the one resolver tasks, workflows, validation and delivery all use.
type TaskTarget struct {
	Kind string
	// Session is the session name or ID of a session target, and Channel the
	// channel name of a channel target.
	Session string
	Channel string
	// Project and Number name the job of a Code Loop target.
	Project string
	Number  int
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
	case TargetCodeLoop:
		return CodeLoopTarget(t.Project, t.Number)
	}
	return ""
}

// CodeLoopTarget returns the delivery target that picks project's job number.
func CodeLoopTarget(project string, number int) string {
	return CodeLoopTargetPrefix + strings.TrimSpace(project) + "#" + strconv.Itoa(number)
}

// ParseTaskTarget resolves a delivery target: empty or "silent", "reply",
// "session:<name-or-id>", "channel:<channel name>", or
// "codeloop:<project>#<number>".
func ParseTaskTarget(target string) (TaskTarget, error) {
	target = strings.TrimSpace(target)
	switch {
	case target == "" || strings.EqualFold(target, TargetSilent):
		return TaskTarget{Kind: TargetSilent}, nil
	case IsReplyTarget(target):
		return TaskTarget{Kind: TargetReply}, nil
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
	return TaskTarget{}, fmt.Errorf("invalid target %q; must be empty, \"silent\", \"reply\" (workflows), session:<name-or-id>, channel:<channel name>, or codeloop:<owner/repo>#<number>", target)
}

func parseCodeLoopTarget(target string) (TaskTarget, error) {
	ref := strings.TrimSpace(strings.TrimPrefix(target, CodeLoopTargetPrefix))
	hash := strings.LastIndex(ref, "#")
	if hash < 0 {
		return TaskTarget{}, fmt.Errorf("codeloop target %q names no job; use codeloop:<owner/repo>#<number>", target)
	}
	project := strings.TrimSpace(ref[:hash])
	number, err := strconv.Atoi(strings.TrimSpace(ref[hash+1:]))
	if project == "" || strings.ContainsAny(project, " \t\r\n") {
		return TaskTarget{}, fmt.Errorf("codeloop target %q names no project; use codeloop:<owner/repo>#<number>", target)
	}
	if err != nil || number <= 0 {
		return TaskTarget{}, fmt.Errorf("codeloop target %q needs a positive job number; use codeloop:<owner/repo>#<number>", target)
	}
	return TaskTarget{Kind: TargetCodeLoop, Project: project, Number: number}, nil
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
