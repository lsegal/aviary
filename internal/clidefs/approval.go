package clidefs

import (
	"fmt"
	"strings"
)

// ApprovalMode is how much an agent may do without asking, in aviary's own
// terms. Aviary owns the mode; each CLI provider's definition translates it
// into that CLI's native permission flags at launch, so no CLI settings file
// is ever written to set it.
type ApprovalMode string

const (
	// ApprovalReadOnly lets the agent read and inspect but not change files
	// or run commands that change anything.
	ApprovalReadOnly ApprovalMode = "read-only"
	// ApprovalAuto lets the agent work inside its workspace without asking,
	// with the CLI's own sandbox or safety checks refusing riskier actions.
	ApprovalAuto ApprovalMode = "auto"
	// ApprovalFull turns the CLI's sandbox and permission checks off.
	ApprovalFull ApprovalMode = "full"
)

// DefaultApprovalMode is the mode used when no setting names one.
const DefaultApprovalMode = ApprovalAuto

// ApprovalModes lists every mode from least to most permissive.
var ApprovalModes = []ApprovalMode{ApprovalReadOnly, ApprovalAuto, ApprovalFull}

// Valid reports whether m is one of ApprovalModes. The empty mode is not.
func (m ApprovalMode) Valid() bool {
	for _, mode := range ApprovalModes {
		if m == mode {
			return true
		}
	}
	return false
}

// Effective returns m, or DefaultApprovalMode when m is empty or unknown.
func (m ApprovalMode) Effective() ApprovalMode {
	if m.Valid() {
		return m
	}
	return DefaultApprovalMode
}

// ValidateApprovalMode accepts an empty mode (unset) or one of ApprovalModes.
func ValidateApprovalMode(m ApprovalMode) error {
	if m == "" || m.Valid() {
		return nil
	}
	return fmt.Errorf("%q must be one of %s", m, approvalModeNames())
}

func approvalModeNames() string {
	names := make([]string, len(ApprovalModes))
	for i, mode := range ApprovalModes {
		names[i] = string(mode)
	}
	return strings.Join(names, ", ")
}

// ApprovalArgs are the CLI arguments that put the agent in mode. A definition
// that declares no arguments for mode falls back to the closest less
// permissive mode it does declare, so the translation is never more
// permissive than asked; with none of those either it contributes nothing,
// and the CLI runs in its own default mode.
func (d Definition) ApprovalArgs(mode ApprovalMode) []string {
	mode = mode.Effective()
	fallback := false
	for i := len(ApprovalModes) - 1; i >= 0; i-- {
		fallback = fallback || ApprovalModes[i] == mode
		if args, ok := d.Approval[ApprovalModes[i]]; fallback && ok {
			return append([]string(nil), args...)
		}
	}
	return nil
}

func (d Definition) validateApproval() error {
	for mode, args := range d.Approval {
		if !mode.Valid() {
			return fmt.Errorf(`field "approval": unknown mode %q; known modes are %s`, mode, approvalModeNames())
		}
		for _, arg := range args {
			if placeholderPattern.MatchString(arg) {
				return fmt.Errorf(`field "approval.%s": %q cannot contain a placeholder`, mode, arg)
			}
		}
	}
	return nil
}
