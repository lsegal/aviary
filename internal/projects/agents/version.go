package agents

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// versionPattern finds the first dotted numeric version in a line of a CLI's
// --version output. CLIs print it in every shape imaginable -- bare "0.58.0",
// "gemini 0.58.0", "codex-cli 0.5.0 (rust)" -- so the number is read out of
// whatever surrounds it rather than the whole line being parsed.
var versionPattern = regexp.MustCompile(`\b(\d+(?:\.\d+)*)\b`)

// ParseVersion reads the version a binary reported, returning its numeric
// components. A prerelease or build suffix is ignored: it never makes a
// version older than the release it precedes by enough to matter here, and
// treating "1.2.0-beta.1" as unreadable would block a dispatch that works.
// ok is false when the text carries no version at all, which is the case the
// caller warns about rather than failing on.
func ParseVersion(text string) ([]int, bool) {
	match := versionPattern.FindStringSubmatch(text)
	if match == nil {
		return nil, false
	}
	parts := strings.Split(match[1], ".")
	components := make([]int, 0, len(parts))
	for _, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil {
			return nil, false
		}
		components = append(components, number)
	}
	return components, true
}

// CompareVersions orders two reported versions, returning -1, 0, or 1, and
// false when either of them carries no version. Components missing from the
// shorter version count as zero, so "1.2" and "1.2.0" are the same version.
func CompareVersions(a, b string) (int, bool) {
	left, ok := ParseVersion(a)
	if !ok {
		return 0, false
	}
	right, ok := ParseVersion(b)
	if !ok {
		return 0, false
	}
	for i := 0; i < len(left) || i < len(right); i++ {
		var l, r int
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		if l != r {
			if l < r {
				return -1, true
			}
			return 1, true
		}
	}
	return 0, true
}

// VersionSupported reports whether a binary reporting this version satisfies
// the definition's declared minimum. comparable is false when the definition
// declares no minimum, or when the reported text carries no version glorp can
// read; in both cases the dispatch goes ahead, in the second one with a
// warning, because refusing to run an agent whose version cannot be read
// would break every CLI that prints its version some way glorp has not seen.
func (d Definition) VersionSupported(reported string) (supported, comparable bool) {
	if strings.TrimSpace(d.MinVersion) == "" {
		return true, false
	}
	order, ok := CompareVersions(reported, d.MinVersion)
	if !ok {
		return true, false
	}
	return order >= 0, true
}

// VersionTooOldError is the message a dispatch fails with when the installed
// binary is older than the definition requires. It names both versions and
// what to do about it, because the alternative -- letting the CLI reject
// arguments it has never heard of -- reports an unrecognized-argument error
// that says nothing about the version being the cause (issue #535).
func (d Definition) VersionTooOldError(binary, reported string) error {
	return fmt.Errorf("agent %q requires %s %s or newer, but %s reports %s; upgrade %s, or point glorp at a newer install with --agent-binary %s=PATH",
		d.Name, d.Binary, d.MinVersion, binary, strings.TrimSpace(reported), d.Binary, d.Name)
}
