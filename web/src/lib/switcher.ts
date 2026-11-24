import { isProtectedAgentFile } from "../stores/agentFiles";
import {
	type AgentChannel,
	type AgentTask,
	isWorkflow,
	taskSessionPath,
} from "../stores/settings";
import { channelName, channelPath } from "./channels";
import { type ProjectSummary, projectPath } from "./projects";

// The breadcrumb leaf's switcher (#1063): on a project, task, channel or
// file page, the leaf opens a list of its siblings to jump straight to one.

/** What a leaf crumb switches between. */
export type SwitcherKind = "project" | "task" | "channel" | "file";

/** The switcher a leaf crumb carries: its kind, owner and current item. */
export interface CrumbSwitcher {
	kind: SwitcherKind;
	/** The owning agent; "" for a runtime watch no agent owns. */
	agent: string;
	current: string;
}

/** A row's type icon. */
export type SwitcherIcon =
	| "repo"
	| "board"
	| "discussions"
	| "task"
	| "workflow"
	| "signal"
	| "slack"
	| "discord"
	| "channel"
	| "file"
	| "builtin";

export interface SwitcherItem {
	key: string;
	label: string;
	to: string | { path: string; query: Record<string, string> };
	icon: SwitcherIcon;
	/** Secondary info, such as the running job count or a paused state. */
	detail?: string;
	current: boolean;
}

export const SWITCHER_TITLES: Record<SwitcherKind, string> = {
	project: "Switch project",
	task: "Switch task",
	channel: "Switch channel",
	file: "Switch file",
};

/** The key that, with Alt (⌥ on a Mac), opens the switcher. */
export const SWITCHER_KEY = "S";

/** The items whose label contains every word of query, ignoring case. */
export function filterSwitcherItems(
	items: SwitcherItem[],
	query: string,
): SwitcherItem[] {
	const words = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
	if (!words.length) return items;
	return items.filter((item) => {
		const label = item.label.toLowerCase();
		return words.every((w) => label.includes(w));
	});
}

/**
 * The kind of a watch target: a project board or Discussions URL, else a
 * repository.
 */
export function projectKind(target: string): "repo" | "board" | "discussions" {
	if (/\/projects\/\d+/.test(target)) return "board";
	if (/\/discussions(\/|$)/.test(target)) return "discussions";
	return "repo";
}

/** The board query a sibling project keeps: its filters and tab. */
const PROJECT_VIEW_QUERY = ["state", "tab", "view"];

export function projectItems(
	sw: CrumbSwitcher,
	projects: ProjectSummary[],
	query: Record<string, string>,
): SwitcherItem[] {
	// A scheduled item or dialog open on the board belongs to its project.
	const view = Object.fromEntries(
		Object.entries(query).filter(([k]) => PROJECT_VIEW_QUERY.includes(k)),
	);
	return projects.map((p) => {
		const path = projectPath(sw.agent, p.name);
		return {
			key: p.name,
			label: p.name,
			to: { path, query: view },
			icon: projectKind(p.name),
			detail: p.running ? `${p.running} running` : undefined,
			current: p.name === sw.current,
		};
	});
}

export function taskItems(
	sw: CrumbSwitcher,
	tasks: AgentTask[],
): SwitcherItem[] {
	return tasks
		.filter((t) => t.name)
		.map((t) => ({
			key: t.name,
			label: t.name,
			// A run belongs to its own task, so a sibling opens on its dashboard.
			to: taskSessionPath(sw.agent, t.name),
			icon: isWorkflow(t) ? "workflow" : "task",
			detail: t.enabled === false ? "Paused" : undefined,
			current: t.name === sw.current,
		}));
}

const CHANNEL_ICONS = new Set<SwitcherIcon>(["signal", "slack", "discord"]);

export function channelItems(
	sw: CrumbSwitcher,
	channels: AgentChannel[],
): SwitcherItem[] {
	return channels.map((c) => {
		const name = channelName(c);
		const type = c.type.toLowerCase() as SwitcherIcon;
		return {
			key: name,
			label: name,
			to: channelPath(sw.agent, name),
			icon: CHANNEL_ICONS.has(type) ? type : "channel",
			detail: c.enabled === false ? "Disabled" : undefined,
			current: name === sw.current,
		};
	});
}

export function fileItems(sw: CrumbSwitcher, files: string[]): SwitcherItem[] {
	return files.map((f) => ({
		key: f,
		label: f,
		to: {
			path: `/agents/${encodeURIComponent(sw.agent)}/files`,
			query: { file: f },
		},
		icon: isProtectedAgentFile(f) ? "builtin" : "file",
		detail: isProtectedAgentFile(f) ? "Built-in" : undefined,
		current: f === sw.current,
	}));
}
