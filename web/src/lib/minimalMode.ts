// Minimal mode is an `aviary watch` session's web UI: only Code Loop and
// Scheduled Tasks, with no chat, agent or channel settings, or System pages.
// The server refuses the other pages' API calls too; this only keeps the UI
// from offering them.

/** The agent pages a minimal-mode session shows. */
export const MINIMAL_AGENT_PAGES: ReadonlySet<string> = new Set([
	"projects",
	"tasks",
]);

// The paths a minimal-mode session serves: an agent's Code Loop boards and
// jobs, its Scheduled Tasks and their dashboards, the boards' shared
// settings, Settings → Display (#1093), and runtime watches.
const MINIMAL_PATHS = [
	/^\/agents\/[^/]+\/projects(\/|$)/,
	/^\/agents\/[^/]+\/tasks(\/|$)/,
	/^\/projects\/settings\/?$/,
	/^\/system\/display\/?$/,
	/^\/system\/watches\//,
];

/** Whether a minimal-mode session serves the page at path. */
export function minimalAllowsPath(path: string): boolean {
	return MINIMAL_PATHS.some((re) => re.test(path));
}
