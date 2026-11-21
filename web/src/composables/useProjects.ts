import { onMounted, onUnmounted, type Ref, ref, watch } from "vue";
import {
	type BoardFilter,
	type JobDetail,
	normalizeJobDetail,
	normalizeJobList,
	normalizeStatus,
	normalizeUpcoming,
	type ProjectJob,
	type ProjectSchedule,
	type ProjectStatus,
	type ProjectSummary,
	parseToolJSON,
	projectSchedules,
	sortJobs,
	summarizeProjects,
	triggerArgs,
	type UpcomingRun,
} from "../lib/projects";
import { useMCP } from "./useMCP";

/** How often the board and run view re-read the engine, in milliseconds. */
export const PROJECTS_POLL_MS = 2_000;
/** Lines of terminal output shown on a board card. */
export const CARD_OUTPUT_LINES = 12;
export const JOBS_PAGE_SIZE = 12;
// project_job_list's page size when the board reads every job at once.
const ALL_JOBS_LIMIT = 1_000;

// Polls fn every interval while the component is mounted, without
// overlapping calls, and pauses while the tab is hidden.
function usePoll(fn: () => Promise<void>, interval: number) {
	let timer: ReturnType<typeof setTimeout> | null = null;
	let stopped = false;

	async function tick() {
		if (stopped) return;
		if (document.visibilityState !== "hidden") {
			await fn().catch(() => {});
		}
		if (!stopped) timer = setTimeout(tick, interval);
	}

	onMounted(() => {
		stopped = false;
		void tick();
	});
	onUnmounted(() => {
		stopped = true;
		if (timer) clearTimeout(timer);
	});
	return { refresh: fn };
}

/** The watched projects and their live counts, for the sidebar. */
export function useProjectList(interval = 10_000) {
	const { callTool } = useMCP();
	const projects = ref<ProjectSummary[]>([]);
	const available = ref(false);

	const { refresh } = usePoll(async () => {
		try {
			const [statusText, listText] = await Promise.all([
				callTool("project_status", {}),
				callTool("project_job_list", { limit: ALL_JOBS_LIMIT }),
			]);
			projects.value = summarizeProjects(
				normalizeStatus(parseToolJSON(statusText)),
				normalizeJobList(parseToolJSON(listText)).jobs,
			);
			available.value = true;
		} catch {
			available.value = false;
		}
	}, interval);

	return { projects, available, refresh };
}

/**
 * The live status and one page of the jobs of a set of projects, with their
 * recent output: one project's board, or an agent's board of all its
 * projects. No projects means an empty board. A non-empty search narrows the
 * jobs to those with the text in any field, including their output, and
 * total then counts only the matches. In kanban mode it reads every job in
 * the filter instead, without their output.
 */
export function useProjectBoard(
	targets: Ref<string[]>,
	filter: Ref<BoardFilter>,
	search: Ref<string> = ref(""),
	kanban: Ref<boolean> = ref(false),
) {
	const { callTool } = useMCP();
	const status = ref<ProjectStatus | null>(null);
	const jobs = ref<ProjectJob[]>([]);
	const total = ref(0);
	/** The jobs in the current filter before the search narrowed them. */
	const unfilteredTotal = ref(0);
	const page = ref(0);
	const error = ref("");
	const loading = ref(true);

	async function listJobs(projects: string[]) {
		// The kanban view lays every job out in its state's column, so it
		// reads them all instead of one page.
		const all = kanban.value;
		const list = normalizeJobList(
			parseToolJSON(
				await callTool("project_job_list", {
					targets: projects,
					state: filter.value === "all" ? undefined : filter.value,
					query: search.value.trim() || undefined,
					offset: all ? 0 : page.value * JOBS_PAGE_SIZE,
					limit: all ? ALL_JOBS_LIMIT : JOBS_PAGE_SIZE,
				}),
			),
		);
		return {
			jobs: sortJobs(list.jobs),
			total: list.total,
			unfilteredTotal: list.unfilteredTotal,
		};
	}

	// Typing fires a load per keystroke, so only the latest one may land.
	let latest = 0;
	async function load() {
		const call = ++latest;
		const projects = targets.value;
		const key = projects.join(" ");
		if (!projects.length) {
			jobs.value = [];
			total.value = 0;
			unfilteredTotal.value = 0;
			loading.value = false;
			return;
		}
		try {
			const [statusText, list] = await Promise.all([
				callTool("project_status", {}),
				listJobs(projects),
			]);
			// Every visible list card shows its job's latest terminal output,
			// which streams while the job runs; kanban cards show none.
			await Promise.all(
				(kanban.value ? [] : list.jobs).map(async (j) => {
					try {
						const detail = normalizeJobDetail(
							parseToolJSON(
								await callTool("project_job_get", {
									target: j.target,
									number: j.number,
									lines: CARD_OUTPUT_LINES,
									events: 1,
								}),
							),
						);
						j.output = detail.job.output;
					} catch {
						// Keep the card without output until the next poll.
					}
				}),
			);
			if (key !== targets.value.join(" ") || call !== latest) return;
			status.value = normalizeStatus(parseToolJSON(statusText));
			jobs.value = list.jobs;
			total.value = list.total;
			unfilteredTotal.value = list.unfilteredTotal;
			error.value = "";
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
		} finally {
			loading.value = false;
		}
	}

	const { refresh } = usePoll(load, PROJECTS_POLL_MS);

	watch([() => targets.value.join(" "), filter, kanban], () => {
		page.value = 0;
		loading.value = true;
		void refresh();
	});
	// A new search starts on the first page of its matches; when the page is
	// already first, the page watcher stays quiet, so load here.
	watch(search, () => {
		if (page.value === 0) void refresh();
		else page.value = 0;
	});
	watch(page, () => void refresh());

	async function action(
		tool: "project_job_retry" | "project_job_stop",
		job: ProjectJob,
	) {
		await callTool(tool, { target: job.target, number: job.number });
		await refresh();
	}

	async function checkNow() {
		// One check reads every project, so a board of several asks once.
		const [target] = targets.value;
		await callTool(
			"project_refresh",
			targets.value.length === 1 ? { target } : {},
		);
		await refresh();
	}

	/** Opens GitHub's sign-in page in Aviary's browser. */
	async function signInToGitHub() {
		await callTool("project_github_signin", {});
		await refresh();
	}

	/** Reads GitHub through gh polling instead of Aviary's browser. */
	async function switchToPolling() {
		await callTool("project_github_use_polling", {});
		await refresh();
	}

	/** Imports the GitHub sign-in from a profile signed in elsewhere. */
	async function importProfile(profile: string) {
		await callTool("project_github_import", { profile });
		await refresh();
	}

	return {
		status,
		jobs,
		total,
		unfilteredTotal,
		page,
		error,
		loading,
		refresh,
		checkNow,
		signInToGitHub,
		switchToPolling,
		importProfile,
		retry: (job: ProjectJob) => action("project_job_retry", job),
		stop: (job: ProjectJob) => action("project_job_stop", job),
	};
}

/** One job's state, typed event timeline and raw output, kept live. */
export function useProjectJob(target: Ref<string>, number: Ref<number>) {
	const { callTool } = useMCP();
	const detail = ref<JobDetail | null>(null);
	const error = ref("");
	// The footer names who may steer the job; the setting rarely changes, so
	// it is read once rather than polled.
	const allowedCommenters = ref<string[]>([]);
	onMounted(async () => {
		try {
			allowedCommenters.value = normalizeStatus(
				parseToolJSON(await callTool("project_status", {})),
			).allowedCommenters;
		} catch {
			// The footer still explains how to steer without the names.
		}
	});

	const { refresh } = usePoll(async () => {
		const key = `${target.value}#${number.value}`;
		try {
			const next = normalizeJobDetail(
				parseToolJSON(
					await callTool("project_job_get", {
						target: target.value,
						number: number.value,
						lines: -1,
						events: -1,
					}),
				),
			);
			if (key !== `${target.value}#${number.value}`) return;
			detail.value = next;
			error.value = "";
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
		}
	}, PROJECTS_POLL_MS);

	watch([target, number], () => {
		detail.value = null;
		void refresh();
	});

	async function action(tool: string) {
		await callTool(tool, { target: target.value, number: number.value });
		await refresh();
	}

	return {
		detail,
		error,
		allowedCommenters,
		refresh,
		retry: () => action("project_job_retry"),
		stop: () => action("project_job_stop"),
	};
}

/** Next runs the board's "Up next" strip shows, and the most per schedule. */
export const UP_NEXT_RUNS = 5;
export const UP_NEXT_PER_SCHEDULE = 3;

/** A project's scheduled prompts and their next runs, kept live. */
export function useProjectSchedules(project: Ref<string>, interval = 5_000) {
	const { callTool } = useMCP();
	const schedules = ref<ProjectSchedule[]>([]);
	const upcoming = ref<UpcomingRun[]>([]);
	const agents = ref<string[]>([]);
	const error = ref("");
	const loaded = ref(false);

	const { refresh } = usePoll(async () => {
		const target = project.value;
		if (!target) return;
		try {
			const [taskText, upcomingText, agentText] = await Promise.all([
				callTool("task_list", {}),
				callTool("task_upcoming", {
					project: target,
					limit: UP_NEXT_RUNS,
					per_task: UP_NEXT_PER_SCHEDULE,
				}),
				agents.value.length ? Promise.resolve("") : callTool("agent_list", {}),
			]);
			if (target !== project.value) return;
			schedules.value = projectSchedules(parseToolJSON(taskText), target);
			upcoming.value = normalizeUpcoming(parseToolJSON(upcomingText));
			if (agentText) {
				const list = parseToolJSON(agentText);
				agents.value = Array.isArray(list)
					? list
							.map((a) =>
								a && typeof a === "object"
									? String((a as { name?: unknown }).name ?? "")
									: "",
							)
							.filter(Boolean)
					: [];
			}
			error.value = "";
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
		} finally {
			loaded.value = true;
		}
	}, interval);

	watch(project, () => {
		schedules.value = [];
		upcoming.value = [];
		loaded.value = false;
		void refresh();
	});

	/** Creates the schedule when it has no id yet, otherwise updates it. */
	async function save(s: ProjectSchedule) {
		const fields = {
			...triggerArgs(s),
			content: s.prompt.trim(),
			model: s.model.trim(),
		};
		if (s.id) {
			await callTool("task_update", {
				name: s.id,
				...fields,
				project: s.project,
				enabled: s.enabled,
			});
		} else {
			await callTool("task_schedule", {
				...fields,
				agent: s.agent,
				name: s.name.trim(),
				type: "project",
				project: s.project,
			});
			if (!s.enabled) {
				await callTool("task_update", {
					name: `${s.agent}/${s.name.trim()}`,
					enabled: false,
				});
			}
		}
		await refresh();
	}

	async function setEnabled(s: ProjectSchedule, enabled: boolean) {
		await callTool("task_update", { name: s.id, enabled });
		await refresh();
	}

	async function remove(s: ProjectSchedule) {
		await callTool("task_delete", { name: s.id });
		await refresh();
	}

	async function runNow(s: ProjectSchedule) {
		await callTool("task_run", { name: s.id });
		await refresh();
	}

	/** The next 3 fires of the schedule as edited, saved or not. */
	async function preview(s: ProjectSchedule): Promise<string[]> {
		const args = triggerArgs(s);
		const result = parseToolJSON(
			await callTool(
				"task_preview",
				Object.fromEntries(Object.entries(args).filter(([, v]) => v)),
			),
		);
		const next = (result as { next?: unknown }).next;
		return Array.isArray(next) ? next.map(String) : [];
	}

	return {
		schedules,
		upcoming,
		agents,
		error,
		loaded,
		refresh,
		save,
		setEnabled,
		remove,
		runNow,
		preview,
	};
}
