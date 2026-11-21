// The System section's pages, in sidebar order. The System item itself is
// the overview, which lists these pages again as cards. Settings and
// Providers & Authentication open from the sidebar's gear menu instead.
export interface SystemPage {
	key: string;
	label: string;
	to: string;
	/** The page's card text on the overview. */
	description: string;
}

export const SYSTEM_PAGES: SystemPage[] = [
	{
		key: "usage",
		label: "Usage",
		to: "/system/usage",
		description: "Token usage and cost by agent, model and session.",
	},
	{
		key: "tools",
		label: "Tools",
		to: "/system/tools",
		description: "Browse and run the MCP tools Aviary exposes.",
	},
	{
		key: "skills",
		label: "Skills",
		to: "/system/skills",
		description: "Install, enable and configure skills.",
	},
	{
		key: "sessions",
		label: "Sessions",
		to: "/system/sessions",
		description: "Every agent's chat sessions and their history.",
	},
	{
		key: "models",
		label: "Models",
		to: "/system/models",
		description: "The models available from configured providers.",
	},
	{
		key: "logs",
		label: "Logs",
		to: "/system/logs",
		description: "The server's live log output.",
	},
	{
		key: "daemons",
		label: "Daemons",
		to: "/system/daemons",
		description: "Background processes and channel connections.",
	},
];

/** The pages the gear menu's Settings opens, as tabs of one page. */
export const SETTINGS_TABS = [
	{ key: "general", label: "General", to: "/system/settings" },
	{
		key: "providers",
		label: "Providers & Authentication",
		to: "/system/providers",
	},
] as const;

/** Whether path is one of Settings' tabs rather than a System page. */
export function isSettingsPath(path: string): boolean {
	return SETTINGS_TABS.some(
		(t) => path === t.to || path.startsWith(`${t.to}/`),
	);
}
