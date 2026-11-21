package clidefs

import (
	"fmt"
	"strings"
)

// Placeholders a permissions argument may use. A command rule's pattern is
// written {commandSpec}: "(pattern)", or nothing when the pattern is "*", so
// "Bash{commandSpec}" names every command or just the matching ones. A path
// rule's pattern is {path}, or {pathPosix} in POSIX form, and the directory
// its globs sit under is {dir} or {dirPosix}.
const (
	placeholderCommandSpec = "commandSpec"
	placeholderPath        = "path"
	placeholderPathPosix   = "pathPosix"
)

var (
	commandPlaceholders = []string{placeholderCommandSpec}
	pathPlaceholders    = []string{placeholderPath, placeholderPathPosix, placeholderDir, placeholderDirPosix}
)

// PermissionTemplates are the CLI arguments that pass one of an agent's
// permission rules on to the CLI session in its own allow and deny terms,
// one template per kind of rule. A kind left out passes nothing for it.
//
// Enforce, when set, replaces the mode's approval arguments for an agent
// whose rules are narrower than allowing every command and the whole Aviary
// directory, so the CLI refuses what those rules leave out instead of
// skipping the check that would (#577).
type PermissionTemplates struct {
	Enforce      []string `json:"enforce,omitempty"`
	AllowCommand []string `json:"allowCommand,omitempty"`
	DenyCommand  []string `json:"denyCommand,omitempty"`
	AllowPath    []string `json:"allowPath,omitempty"`
	DenyPath     []string `json:"denyPath,omitempty"`
}

// Rule is one of an agent's ordered permission rules: a pattern it allows,
// or denies when Deny is set.
type Rule struct {
	Pattern string
	Deny    bool
}

// PathRule is a filesystem Rule with its pattern already expanded to an
// absolute path, and Dir the directory the pattern's globs sit under.
type PathRule struct {
	Rule
	Dir string
}

// PermissionArgs are the CLI arguments that carry an agent's exec and
// filesystem rules into a session running in mode, as the definition's
// permissions declare them. A mode left out falls back as Approval does.
//
// The CLI weighs a deny above every allow, where the agent's rules let the
// last match win, so a deny followed by a narrower allow comes out more
// restrictive in the CLI than in Aviary, never less.
func (d Definition) PermissionArgs(mode ApprovalMode, commands []Rule, paths []PathRule) []string {
	templates, ok := modeTemplates(d.Permissions, mode)
	if !ok {
		return nil
	}
	var args []string
	for _, rule := range commands {
		pattern := strings.TrimSpace(rule.Pattern)
		if pattern == "" {
			continue
		}
		spec := "(" + pattern + ")"
		if pattern == "*" {
			spec = ""
		}
		template := templates.AllowCommand
		if rule.Deny {
			template = templates.DenyCommand
		}
		args = append(args, fillTemplate(template, map[string]string{placeholderCommandSpec: spec})...)
	}
	for _, rule := range paths {
		if strings.TrimSpace(rule.Pattern) == "" {
			continue
		}
		template := templates.AllowPath
		if rule.Deny {
			template = templates.DenyPath
		}
		args = append(args, fillTemplate(template, map[string]string{
			placeholderPath:      rule.Pattern,
			placeholderPathPosix: posixPath(rule.Pattern),
			placeholderDir:       rule.Dir,
			placeholderDirPosix:  posixPath(rule.Dir),
		})...)
	}
	return args
}

// EnforceArgs are the arguments that replace the approval arguments of a
// session running in mode when the agent's rules are narrower than its
// mode would otherwise let the CLI go, or nil when the mode passes its
// approval arguments unchanged. A mode left out falls back as Approval does.
func (d Definition) EnforceArgs(mode ApprovalMode) []string {
	templates, ok := modeTemplates(d.Permissions, mode)
	if !ok || len(templates.Enforce) == 0 {
		return nil
	}
	return append([]string(nil), templates.Enforce...)
}

// modeTemplates returns byMode's templates for mode, or for the closest less
// permissive mode it declares.
func modeTemplates(byMode map[ApprovalMode]PermissionTemplates, mode ApprovalMode) (PermissionTemplates, bool) {
	mode = mode.Effective()
	fallback := false
	for i := len(ApprovalModes) - 1; i >= 0; i-- {
		fallback = fallback || ApprovalModes[i] == mode
		if templates, ok := byMode[ApprovalModes[i]]; fallback && ok {
			return templates, true
		}
	}
	return PermissionTemplates{}, false
}

// fillTemplate returns a copy of template with its placeholders replaced.
func fillTemplate(template []string, values map[string]string) []string {
	out := make([]string, len(template))
	for i, arg := range template {
		out[i] = placeholderPattern.ReplaceAllStringFunc(arg, func(match string) string {
			if value, ok := values[strings.Trim(match, "{}")]; ok {
				return value
			}
			return match
		})
	}
	return out
}

func (d Definition) validatePermissions() error {
	for mode, templates := range d.Permissions {
		if !mode.Valid() {
			return fmt.Errorf(`field "permissions": unknown mode %q; known modes are %s`, mode, approvalModeNames())
		}
		for _, arg := range templates.Enforce {
			if placeholderPattern.MatchString(arg) {
				return fmt.Errorf(`field "permissions.%s.enforce": %q cannot contain a placeholder`, mode, arg)
			}
		}
		for _, field := range []struct {
			name    string
			args    []string
			allowed []string
		}{
			{"allowCommand", templates.AllowCommand, commandPlaceholders},
			{"denyCommand", templates.DenyCommand, commandPlaceholders},
			{"allowPath", templates.AllowPath, pathPlaceholders},
			{"denyPath", templates.DenyPath, pathPlaceholders},
		} {
			for _, arg := range field.args {
				for _, match := range placeholderPattern.FindAllStringSubmatch(arg, -1) {
					if !contains(field.allowed, match[1]) {
						return fmt.Errorf(`field "permissions.%s.%s": %q may only use {%s}`, mode, field.name, arg, strings.Join(field.allowed, "}, {"))
					}
				}
			}
		}
	}
	return nil
}
