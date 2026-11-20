package agents

import (
	"fmt"
	"strings"

	"github.com/lsegal/aviary/internal/version"
)

// VersionSupported reports whether a binary reporting this version satisfies
// the definition's declared minimum. comparable is false when the definition
// declares no minimum, or when the reported text carries no version aviary can
// read; in both cases the dispatch goes ahead, in the second one with a
// warning, because refusing to run an agent whose version cannot be read
// would break every CLI that prints its version some way aviary has not seen.
func (d Definition) VersionSupported(reported string) (supported, comparable bool) {
	if strings.TrimSpace(d.MinVersion) == "" {
		return true, false
	}
	order, ok := version.Compare(reported, d.MinVersion)
	if !ok {
		return true, false
	}
	return order >= 0, true
}

// VersionTooOldError is the message a dispatch fails with when the installed
// binary is older than the definition requires. It names both versions and
// what to do about it, because the alternative -- letting the CLI reject
// arguments it has never heard of -- reports an unrecognized-argument error
// that says nothing about the version being the cause.
func (d Definition) VersionTooOldError(binary, reported string) error {
	return fmt.Errorf("agent %q requires %s %s or newer, but %s reports %s; upgrade %s, or point aviary at a newer install with --agent-binary %s=PATH",
		d.Name, d.Binary, d.MinVersion, binary, strings.TrimSpace(reported), d.Binary, d.Name)
}
