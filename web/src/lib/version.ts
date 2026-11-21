const fullSHA = /^[0-9a-f]{40}$/i;

// shortVersion abbreviates a dev build's commit SHA for display; release
// versions pass through unchanged.
export function shortVersion(version: string): string {
	return fullSHA.test(version) ? version.slice(0, 7) : version;
}
