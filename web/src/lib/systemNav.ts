import ChartBarIcon from "@heroicons/vue/24/outline/ChartBarIcon";
import ChatBubbleBottomCenterTextIcon from "@heroicons/vue/24/outline/ChatBubbleBottomCenterTextIcon";
import CommandLineIcon from "@heroicons/vue/24/outline/CommandLineIcon";
import CpuChipIcon from "@heroicons/vue/24/outline/CpuChipIcon";
import EyeIcon from "@heroicons/vue/24/outline/EyeIcon";
import PuzzlePieceIcon from "@heroicons/vue/24/outline/PuzzlePieceIcon";
import SignalIcon from "@heroicons/vue/24/outline/SignalIcon";
import WrenchScrewdriverIcon from "@heroicons/vue/24/outline/WrenchScrewdriverIcon";
import { type Component, markRaw } from "vue";

/**
 * A System page's identity, the same wherever the page appears: overview
 * card, sidebar, rail, mobile row and page header (#896).
 */
export interface SystemPageIdentity {
	icon: Component;
	/** The icon's stroke color, shown alone in navigation. */
	iconClass: string;
	/** The soft tile behind the icon on cards and page headers. */
	tileClass: string;
}

// The System section's pages, in sidebar order. The System item itself is
// the overview, which lists these pages again as cards. Settings and
// Providers & Authentication open from the sidebar's gear menu instead.
export interface SystemPage extends SystemPageIdentity {
	key: string;
	label: string;
	to: string;
	/** The page's card text on the overview. */
	description: string;
}

// Muted accents: a soft tile with a darker stroke in each theme. Class names
// stay literal so Tailwind finds them. Amber's 600 is too light on its 50
// tile, so it uses 700.
export const ACCENTS = {
	sky: {
		iconClass: "text-sky-600 dark:text-sky-400",
		tileClass: "bg-sky-50 dark:bg-sky-500/15",
	},
	amber: {
		iconClass: "text-amber-700 dark:text-amber-400",
		tileClass: "bg-amber-50 dark:bg-amber-500/15",
	},
	violet: {
		iconClass: "text-violet-600 dark:text-violet-400",
		tileClass: "bg-violet-50 dark:bg-violet-500/15",
	},
	teal: {
		iconClass: "text-teal-600 dark:text-teal-400",
		tileClass: "bg-teal-50 dark:bg-teal-500/15",
	},
	indigo: {
		iconClass: "text-indigo-600 dark:text-indigo-400",
		tileClass: "bg-indigo-50 dark:bg-indigo-500/15",
	},
	slate: {
		iconClass: "text-slate-600 dark:text-slate-300",
		tileClass: "bg-slate-100 dark:bg-slate-500/20",
	},
	emerald: {
		iconClass: "text-emerald-600 dark:text-emerald-400",
		tileClass: "bg-emerald-50 dark:bg-emerald-500/15",
	},
	gray: {
		iconClass: "text-gray-500 dark:text-gray-400",
		tileClass: "bg-gray-100 dark:bg-gray-500/15",
	},
} as const;

export const SYSTEM_PAGES: SystemPage[] = [
	{
		key: "usage",
		label: "Usage",
		to: "/system/usage",
		description: "Token usage and cost by agent, model and session.",
		icon: markRaw(ChartBarIcon),
		...ACCENTS.sky,
	},
	{
		key: "tools",
		label: "Tools",
		to: "/system/tools",
		description: "Browse and run the MCP tools Aviary exposes.",
		icon: markRaw(WrenchScrewdriverIcon),
		...ACCENTS.amber,
	},
	{
		key: "skills",
		label: "Skills",
		to: "/system/skills",
		description: "Install, enable and configure skills.",
		icon: markRaw(PuzzlePieceIcon),
		...ACCENTS.violet,
	},
	{
		key: "sessions",
		label: "Sessions",
		to: "/system/sessions",
		description: "Every agent's chat sessions and their history.",
		icon: markRaw(ChatBubbleBottomCenterTextIcon),
		...ACCENTS.teal,
	},
	{
		key: "models",
		label: "Models",
		to: "/system/models",
		description: "The models available from configured providers.",
		icon: markRaw(CpuChipIcon),
		...ACCENTS.indigo,
	},
	{
		key: "logs",
		label: "Logs",
		to: "/system/logs",
		description: "The server's live log output.",
		icon: markRaw(CommandLineIcon),
		...ACCENTS.slate,
	},
	{
		key: "daemons",
		label: "Daemons",
		to: "/system/daemons",
		description: "Background processes and channel connections.",
		icon: markRaw(SignalIcon),
		...ACCENTS.emerald,
	},
];

/** A runtime watch's identity: one generic icon, as watches vary. */
export const WATCH_IDENTITY: SystemPageIdentity = {
	icon: markRaw(EyeIcon),
	...ACCENTS.gray,
};

/** The identity of the System page or runtime watch with key, if any. */
export function systemPageIdentity(
	key: string,
): SystemPageIdentity | undefined {
	if (key.startsWith("watch:")) return WATCH_IDENTITY;
	return SYSTEM_PAGES.find((p) => p.key === key);
}

/** The pages the gear menu's Settings opens, as tabs of one page. */
export const SETTINGS_TABS = [
	{ key: "general", label: "General", to: "/system/settings" },
	{
		key: "providers",
		label: "Providers & Authentication",
		to: "/system/providers",
	},
	{ key: "display", label: "Display", to: "/system/display" },
	{ key: "notifications", label: "Notifications", to: "/system/notifications" },
] as const;

/** Whether path is one of Settings' tabs rather than a System page. */
export function isSettingsPath(path: string): boolean {
	return SETTINGS_TABS.some(
		(t) => path === t.to || path.startsWith(`${t.to}/`),
	);
}
