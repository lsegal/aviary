import { computed, ref } from "vue";
import {
	normalizeJobList,
	normalizeStatus,
	type ProjectSummary,
	parseToolJSON,
	summarizeProjects,
} from "../lib/projects";
import { useSettingsStore } from "../stores/settings";
import type { useMCP } from "./useMCP";

type CallTool = ReturnType<typeof useMCP>["callTool"];

// project_job_list's page size when the list reads every job at once.
const ALL_JOBS_LIMIT = 1_000;

// One project list shared by every view that shows it: the sidebar, the
// project tabs, the Code Loop page and the overview. A project added,
// removed or polled anywhere, in this tab through a dialog or in another
// through a live /api/ws event, shows in all of them in the same render
// instead of on each view's next poll (issue #1211).
const fetched = ref<ProjectSummary[]>([]);
// Projects added here or announced by a project-added event that the last
// read did not list yet; they show as loading until it does.
const pending = ref<string[]>([]);
// Projects removed that the last read still listed.
const removed = ref<string[]>([]);
const available = ref(false);
// How many mounted lists count each project's scheduled tasks.
let scheduleReaders = 0;
// The MCP client of the views reading the list; useMCP needs a component's
// setup, which a live event or a queued read is not in.
let callTool: CallTool | null = null;

/** The watched projects with their live counts, as every view shows them. */
export const projectList = computed<ProjectSummary[]>(() => {
	const listed = fetched.value.filter((p) => !removed.value.includes(p.name));
	const added = pending.value
		.filter((name) => !listed.some((p) => p.name === name))
		.map((name) => ({
			name,
			issues: 0,
			running: 0,
			runningJobs: [],
			waiting: 0,
			loading: true,
		}));
	return [...listed, ...added];
});

export const projectListAvailable = available;

let inflight: Promise<void> | null = null;
let queued = false;

/**
 * Reads the project list now. A read already under way is followed by one
 * more, so a change made while it ran is never missed.
 */
export function refreshProjectList(): Promise<void> {
	if (inflight) {
		queued = true;
		return inflight;
	}
	inflight = (async () => {
		try {
			do {
				queued = false;
				await readProjectList();
			} while (queued);
		} finally {
			inflight = null;
		}
	})();
	return inflight;
}

async function readProjectList() {
	if (!callTool) return;
	const withSchedules = scheduleReaders > 0;
	try {
		const [statusText, listText, taskText] = await Promise.all([
			callTool("project_status", {}),
			callTool("project_job_list", { limit: ALL_JOBS_LIMIT }),
			withSchedules ? callTool("task_list", {}) : Promise.resolve(""),
		]);
		const list = summarizeProjects(
			normalizeStatus(parseToolJSON(statusText)),
			normalizeJobList(parseToolJSON(listText)).jobs,
			withSchedules ? parseToolJSON(taskText) : undefined,
		);
		fetched.value = list;
		const names = list.map((p) => p.name);
		pending.value = pending.value.filter((n) => !names.includes(n));
		removed.value = removed.value.filter((n) => names.includes(n));
		available.value = true;
	} catch {
		available.value = false;
	}
}

/**
 * Registers a view that shows the list, from its setup, and with
 * withSchedules counts scheduled tasks until the returned release.
 */
export function readProjectListFrom(
	call: CallTool,
	withSchedules: boolean,
): () => void {
	callTool = call;
	if (withSchedules) scheduleReaders++;
	return () => {
		if (withSchedules) scheduleReaders--;
	};
}

/**
 * Lists target under agent at once, as loading, before the server confirms
 * it. It returns an undo for when the add fails.
 */
export function insertProject(target: string, agent = ""): () => void {
	removed.value = removed.value.filter((n) => n !== target);
	const inserted = !projectList.value.some((p) => p.name === target);
	if (inserted) pending.value = [...pending.value, target];
	const owned = addOwner(target, agent);
	return () => {
		if (inserted) pending.value = pending.value.filter((n) => n !== target);
		if (owned) removeOwner(target, agent);
	};
}

/** Drops target from every view at once. */
export function dropProject(target: string) {
	pending.value = pending.value.filter((n) => n !== target);
	if (!removed.value.includes(target))
		removed.value = [...removed.value, target];
	for (const agent of useSettingsStore().config?.agents ?? [])
		removeOwner(target, agent.name);
}

// The sidebar files each project under the agent that owns it in the
// config, so a project added before the config reloads is listed there
// too. It reports whether it changed the config.
function addOwner(target: string, agent: string): boolean {
	if (!agent) return false;
	const settings = useSettingsStore();
	const entry = settings.config?.agents.find((a) => a.name === agent);
	if (!entry || entry.projects?.some((p) => p.target === target)) return false;
	entry.projects = [...(entry.projects ?? []), { target }];
	return true;
}

function removeOwner(target: string, agent: string) {
	const settings = useSettingsStore();
	const entry = settings.config?.agents.find((a) => a.name === agent);
	if (entry?.projects?.some((p) => p.target === target))
		entry.projects = entry.projects.filter((p) => p.target !== target);
}

/** A project event the server sends on /api/ws. */
export interface ProjectEvent {
	type: string;
	target?: string;
	agent_id?: string;
}

let updateTimer: ReturnType<typeof setTimeout> | null = null;

/**
 * Applies a live project event: an added or removed project shows or goes
 * at once, then the list is read for its counts. Updates, which a busy
 * engine sends often, are batched into one read.
 */
export function applyProjectEvent(ev: ProjectEvent): boolean {
	const target = ev.target ?? "";
	switch (ev.type) {
		case "project-added":
			if (target) insertProject(target, ev.agent_id ?? "");
			void refreshProjectList();
			return true;
		case "project-removed":
			if (target) dropProject(target);
			void refreshProjectList();
			return true;
		case "project-updated":
			if (updateTimer) return true;
			updateTimer = setTimeout(() => {
				updateTimer = null;
				void refreshProjectList();
			}, 250);
			return true;
	}
	return false;
}
