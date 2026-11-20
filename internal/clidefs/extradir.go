package clidefs

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Placeholders an extraDir argument may use for the directory: as it is,
// quoted as a TOML (and JSON) basic string for CLIs that take it inside a
// config value, or in POSIX form (C:\a becomes /c/a) for CLIs whose
// permission rules match paths that way.
const (
	placeholderDir       = "dir"
	placeholderDirQuoted = "dirQuoted"
	placeholderDirPosix  = "dirPosix"
)

var extraDirPlaceholders = []string{placeholderDir, placeholderDirQuoted, placeholderDirPosix}

// ExtraDirArgs are the CLI arguments that add dir to the workspace scope of a
// session running in mode, beside its working directory, as the definition's
// extraDir declares them. A mode left out falls back to the closest less
// permissive mode declared, as ApprovalArgs does. The grant only decides
// which directories are in scope: what may be done there is still the mode's
// to decide, so a read-only session stays read-only. A definition that
// declares none, or an empty dir, contributes nothing.
func (d Definition) ExtraDirArgs(mode ApprovalMode, dir string) []string {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	template := modeArgs(d.ExtraDir, mode)
	if template == nil {
		return nil
	}
	quoted, _ := json.Marshal(dir)
	values := map[string]string{
		placeholderDir:       dir,
		placeholderDirQuoted: string(quoted),
		placeholderDirPosix:  posixPath(dir),
	}
	for i, arg := range template {
		template[i] = placeholderPattern.ReplaceAllStringFunc(arg, func(match string) string {
			if value, ok := values[strings.Trim(match, "{}")]; ok {
				return value
			}
			return match
		})
	}
	return template
}

// posixPath writes a Windows path the way POSIX-style tools do: forward
// slashes, and a drive letter as a leading /c. Other paths are unchanged.
func posixPath(dir string) string {
	if len(dir) >= 2 && dir[1] == ':' {
		dir = "/" + strings.ToLower(dir[:1]) + dir[2:]
	}
	return strings.ReplaceAll(dir, `\`, "/")
}

func (d Definition) validateExtraDir() error {
	for mode, args := range d.ExtraDir {
		if !mode.Valid() {
			return fmt.Errorf(`field "extraDir": unknown mode %q; known modes are %s`, mode, approvalModeNames())
		}
		used := false
		for _, arg := range args {
			for _, match := range placeholderPattern.FindAllStringSubmatch(arg, -1) {
				if !contains(extraDirPlaceholders, match[1]) {
					return fmt.Errorf(`field "extraDir.%s": %q may only use {%s}`, mode, arg, strings.Join(extraDirPlaceholders, "}, {"))
				}
				used = true
			}
		}
		if !used {
			return fmt.Errorf(`field "extraDir.%s" must name the directory with {%s}`, mode, strings.Join(extraDirPlaceholders, "}, {"))
		}
	}
	return nil
}
