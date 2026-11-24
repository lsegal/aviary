import { taskSessionPath } from "../stores/settings";
import { AGENT_PAGES, agentPagePath } from "./agentPages";
import { channelPath } from "./channels";
import { projectPath } from "./projects";
import type { CrumbSwitcher } from "./switcher";
import { SYSTEM_PAGES } from "./systemNav";

/** One segment of a page's breadcrumb (#1036). */
export interface Crumb {
	label: string;
	/** Where the crumb leads; the current page has none. */
	to?: string;
	/** The leaf's switcher between its siblings (#1063). */
	switcher?: CrumbSwitcher;
}

/** What the route alone cannot tell: names and where an agent opens. */
export interface CrumbContext {
	/** Where an agent's crumb leads: its first page in the user's order. */
	agentHome(agent: string): string;
	/** A chat's display name, when its session is known. */
	chatLabel(agent: string, id: string): string | undefined;
}

const decode = (s: string) => {
	try {
		return decodeURIComponent(s);
	} catch {
		return s;
	}
};

/**
 * The breadcrumb of the page at path, from the agent or System down to the
 * current page, or [] for a page outside both. Settings pages build their
 * own from the form they show (see SettingsView). file is the file the
 * Files page has open, from its ?file.
 */
export function routeCrumbs(
	path: string,
	ctx: CrumbContext,
	file = "",
): Crumb[] {
	return linkAncestors(buildCrumbs(path, ctx, file), path);
}

function buildCrumbs(path: string, ctx: CrumbContext, file: string): Crumb[] {
	const agentMatch = /^\/agents\/([^/]+)\/([^/]+)(?:\/(.*?))?\/?$/.exec(path);
	if (agentMatch) {
		const agent = decode(agentMatch[1]);
		const pageKey = agentMatch[2];
		const rest = (agentMatch[3] ?? "").split("/").filter(Boolean);
		const page = AGENT_PAGES.find((p) => p.key === pageKey);
		if (!page) return [];
		const crumbs: Crumb[] = [
			{ label: agent, to: ctx.agentHome(agent) },
			{ label: page.label, to: agentPagePath(agent, page.key) },
		];
		const [item, sub, subItem] = rest;
		if (page.key === "files" && file)
			crumbs.push({
				label: file,
				switcher: { kind: "file", agent, current: file },
			});
		if (!item) return crumbs;
		if (page.key === "chat") {
			crumbs.push({ label: ctx.chatLabel(agent, decode(item)) ?? "Chat" });
		} else if (page.key === "projects") {
			const project = decode(item);
			crumbs.push({
				label: project,
				to: projectPath(agent, project),
				switcher: { kind: "project", agent, current: project },
			});
			if (sub === "jobs" && subItem) crumbs.push({ label: `#${subItem}` });
		} else if (page.key === "channels") {
			const channel = decode(item);
			crumbs.push({
				label: channel,
				to: channelPath(agent, channel),
				switcher: { kind: "channel", agent, current: channel },
			});
		} else if (page.key === "tasks") {
			const task = decode(item);
			crumbs.push({
				label: task,
				to: taskSessionPath(agent, task),
				switcher: { kind: "task", agent, current: task },
			});
			if (sub === "runs" && subItem)
				crumbs.push({ label: `run ${decode(subItem)}` });
		}
		return crumbs;
	}
	const system = { label: "System", to: "/system/overview" };
	const watch = /^\/system\/watches\/([^/]+)(?:\/jobs\/(\d+))?\/?$/.exec(path);
	if (watch) {
		const project = decode(watch[1]);
		const crumbs: Crumb[] = [
			system,
			{
				label: `Watch: ${project}`,
				to: projectPath("", project),
				switcher: { kind: "project", agent: "", current: project },
			},
		];
		if (watch[2]) crumbs.push({ label: `#${watch[2]}` });
		return crumbs;
	}
	const page = SYSTEM_PAGES.find(
		(p) => path === p.to || path.startsWith(`${p.to}/`),
	);
	return page ? [system, { label: page.label, to: page.to }] : [];
}

/**
 * Drops the link of the current page and of any crumb that leads back to
 * it, so only ancestors link. Only the current page's crumb switches.
 */
function linkAncestors(crumbs: Crumb[], path: string): Crumb[] {
	const here = decode(path).replace(/\/$/, "");
	return crumbs.map((c, i) => {
		const leaf = i === crumbs.length - 1;
		if (leaf) return { label: c.label, switcher: c.switcher };
		return !c.to || decode(c.to) === here
			? { label: c.label }
			: { label: c.label, to: c.to };
	});
}
