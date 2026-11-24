package core

import "regexp"

// skipCIToken matches the markers GitHub reads anywhere in a head commit's
// message to skip its push and pull_request workflows, in any letter case.
var skipCIToken = regexp.MustCompile(`(?i)\[(?:skip ci|ci skip|no ci|skip actions|actions skip)\]`)

// SkipCITokens returns every skip-CI marker in message, in order, so a merge
// that carried one into the default branch can say which (issue #1176).
func SkipCITokens(message string) []string {
	return skipCIToken.FindAllString(message, -1)
}
