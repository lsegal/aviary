// Package version is aviary's one version parser. It reads the version a
// CLI provider reports from `--version` and the release tags `aviary update`
// compares, so the doctor, the Projects minimum-version gate, and the updater
// never disagree about what a version is.
package version

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// pattern finds the first dotted numeric version in a line of a CLI's
// --version output. CLIs print it in every shape imaginable -- bare "0.58.0",
// "gemini 0.58.0", "codex-cli 0.5.0 (rust)" -- so the number is read out of
// whatever surrounds it rather than the whole line being parsed.
var pattern = regexp.MustCompile(`\b(\d+(?:\.\d+)*)\b`)

// Find returns the dotted version text carries, such as "2.1.312" from
// "2.1.312 (Claude Code)", or "" when it carries none.
func Find(text string) string {
	match := pattern.FindStringSubmatch(text)
	if match == nil {
		return ""
	}
	return match[1]
}

// Parse reads the version in text, returning its numeric components. A
// prerelease or build suffix is ignored: it never makes a version older than
// the release it precedes by enough to matter here, and treating
// "1.2.0-beta.1" as unreadable would block a dispatch that works. ok is false
// when the text carries no version at all.
func Parse(text string) ([]int, bool) {
	found := Find(text)
	if found == "" {
		return nil, false
	}
	parts := strings.Split(found, ".")
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

// Compare orders two versions, returning -1, 0, or 1, and false when either of
// them carries no version. Components missing from the shorter version count
// as zero, so "1.2" and "1.2.0" are the same version.
func Compare(a, b string) (int, bool) {
	left, ok := Parse(a)
	if !ok {
		return 0, false
	}
	right, ok := Parse(b)
	if !ok {
		return 0, false
	}
	return compareComponents(left, right), true
}

// ParseRelease reads a release tag, which has to be exactly X.Y.Z with an
// optional leading "v". A build that is not a clean release -- "dev", a
// commit hash, "1.2.3-dirty" -- is rejected, because the updater must never
// offer to replace one with a release it cannot be ordered against.
func ParseRelease(raw string) ([]int, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(raw), "v")
	components, ok := Parse(trimmed)
	if !ok || Find(trimmed) != trimmed || len(components) != 3 {
		return nil, fmt.Errorf("%q is not X.Y.Z semver", raw)
	}
	return components, nil
}

// CompareReleases orders two release tags ParseRelease accepted.
func CompareReleases(a, b []int) int { return compareComponents(a, b) }

func compareComponents(left, right []int) int {
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
				return -1
			}
			return 1
		}
	}
	return 0
}
