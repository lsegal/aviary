const fullSHA = /^[0-9a-f]{40}$/i;
const release = /^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/;

// shortVersion abbreviates a dev build's commit SHA for display; release
// versions pass through unchanged.
export function shortVersion(version: string): string {
	return fullSHA.test(version) ? version.slice(0, 7) : version;
}

// isReleaseVersion reports whether a version was stamped by a release build
// (vX.Y.Z); dev builds report "dev", a commit SHA, or nothing.
export function isReleaseVersion(version: string): boolean {
	return release.test(version);
}

// releaseNotesURL is the GitHub release page for a release version, or "" for
// a dev build, which has no release (#1405).
export function releaseNotesURL(version: string): string {
	return isReleaseVersion(version)
		? `https://github.com/lsegal/aviary/releases/tag/${encodeURIComponent(version)}`
		: "";
}
