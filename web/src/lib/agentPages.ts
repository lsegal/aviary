// An agent's pages, in sidebar order. Settings holds its General and
// Permissions sections; each channel's settings open from its own entry.
export const AGENT_PAGES = [
	{ key: "chat", label: "Chats" },
	{ key: "projects", label: "Code Loop" },
	{ key: "channels", label: "Channels" },
	{ key: "tasks", label: "Scheduled Tasks" },
	{ key: "files", label: "Files" },
	{ key: "settings", label: "Agent Settings" },
] as const;

/** An agent page's path, such as /agents/<agent>/channels. */
export function agentPagePath(agent: string, page: string): string {
	return `/agents/${encodeURIComponent(agent)}/${page}`;
}
