package core

import (
	"fmt"
	"strconv"
	"strings"
)

// DefaultReleaseRepo is glorp's own repository, whose release pages the
// version display and the update notice link to (issue #825).
const DefaultReleaseRepo = "lsegal/glorp"

// ReleasePageURL is the GitHub release page of version in repo. A development
// or untagged build, whose version is not plain dotted numbers, has no
// release page, so it links to the repository's releases index instead of a
// tag URL that would 404 (issue #825).
func ReleasePageURL(repo, version string) string {
	if repo == "" {
		repo = DefaultReleaseRepo
	}
	index := fmt.Sprintf("https://github.com/%s/releases", repo)
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if version == "" {
		return index
	}
	for _, field := range strings.Split(version, ".") {
		if _, err := strconv.Atoi(field); err != nil {
			return index
		}
	}
	return index + "/tag/v" + version
}
