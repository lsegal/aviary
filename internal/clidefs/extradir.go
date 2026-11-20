package clidefs

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Placeholders an extraDir argument may use: the directory as it is, or
// quoted as a TOML (and JSON) basic string for CLIs that take it inside a
// config value.
const (
	placeholderDir       = "dir"
	placeholderDirQuoted = "dirQuoted"
)

// ExtraDirArgs are the CLI arguments that add dir to the session's workspace
// scope beside its working directory, as the definition's extraDir declares.
// The grant only widens which directories are in scope: the approval mode
// still decides what may be done there, so a read-only session stays
// read-only. A definition that declares none, or an empty dir, contributes
// nothing.
func (d Definition) ExtraDirArgs(dir string) []string {
	if len(d.ExtraDir) == 0 || strings.TrimSpace(dir) == "" {
		return nil
	}
	quoted, _ := json.Marshal(dir)
	args := make([]string, len(d.ExtraDir))
	for i, arg := range d.ExtraDir {
		args[i] = placeholderPattern.ReplaceAllStringFunc(arg, func(match string) string {
			switch strings.Trim(match, "{}") {
			case placeholderDir:
				return dir
			case placeholderDirQuoted:
				return string(quoted)
			}
			return match
		})
	}
	return args
}

func (d Definition) validateExtraDir() error {
	if len(d.ExtraDir) == 0 {
		return nil
	}
	used := false
	for _, arg := range d.ExtraDir {
		for _, match := range placeholderPattern.FindAllStringSubmatch(arg, -1) {
			if match[1] != placeholderDir && match[1] != placeholderDirQuoted {
				return fmt.Errorf(`field "extraDir": %q may only use {%s} or {%s}`, arg, placeholderDir, placeholderDirQuoted)
			}
			used = true
		}
	}
	if !used {
		return fmt.Errorf(`field "extraDir" must name the directory with {%s} or {%s}`, placeholderDir, placeholderDirQuoted)
	}
	return nil
}
