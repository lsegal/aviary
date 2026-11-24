import { onMounted, onUnmounted, type Ref, ref, watch } from "vue";
import {
	BOARD_STATES,
	type BoardState,
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
	type UpcomingRun,
} from "../lib/projects";
import { triggerArgs } from "../lib/trigger";
import { useSettingsStore } from "../stores/settings";
import { useMCP } from "./useMCP";
import { useTriggerPreview } from "./useTriggerPreview";

/** How often the board and run view re-read the engine, in milliseconds. */
export const PROJECTS_POLL_MS = 2_000;
/** Lines of terminal output shown on a board card. */
export const CARD_OUTPUT_LINES = 12;
/** Latest transcript events read for a board card's rich output. */
export const CARD_OUTPUT_EVENTS = 40;
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

/**
 * The watched projects and their live counts, for the sidebar. With
 * withSchedules it also counts each project's scheduled tasks.
 */
export function useProjectList(interval = 10_000, withSchedules = false) {
	const { callTool } = useMCP();
	const projects = ref<ProjectSummary[]>([]);
	const available = ref(false);

	const { refresh } = usePoll(async () => {
		try {
			const [statusText, listText, taskText] = await Promise.all([
				callTool("project_status", {}),
				callTool("project_job_list", { limit: ALL_JOBS_LIMIT }),
				withSchedules ? callTool("task_list", {}) : Promise.resolve(""),
			]);
			projects.value = summarizeProjects(
				normalizeStatus(parseToolJSON(statusText)),
				normalizeJobList(parseToolJSON(listText)).jobs,
				withSchedules ? parseToolJSON(taskText) : undefined,
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
 * the filter instead, without their output. A schedule is one entry, never a
 * job per fire, and lists only while it is in a job state, so a finished run
 * is never done. With withSchedules, a project's board reads its schedules'
 * entries apart from the jobs, as scheduleEntries, for its scheduled cards.
 */
export function useProjectBoard(
	targets: Ref<string[]>,
	states: Ref<BoardState[]>,
	search: Ref<string> = ref(""),
	kanban: Ref<boolean> = ref(false),
	withSchedules: Ref<boolean> = ref(false),
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
	/** The board entries of the projects' schedules, one per schedule. */
	const scheduleEntries = ref<ProjectJob[]>([]);

	async function listJobs(projects: string[]) {
		// The kanban view lays every job out in its state's column, so it
		// reads them all instead of one page.
		const all = kanban.value;
		const list = normalizeJobList(
			parseToolJSON(
				await callTool("project_job_list", {
					targets: projects,
					// Naming every job state leaves out schedules between runs.
					states: states.value.length ? states.value : BOARD_STATES,
					kinds: withSchedules.value ? ["issue", "discussion"] : undefined,
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

	async function listScheduleEntries(projects: string[]) {
		if (!withSchedules.value) return [];
		return normalizeJobList(
			parseToolJSON(
				await callTool("project_job_list", {
					targets: projects,
					kinds: ["schedule"],
					limit: ALL_JOBS_LIMIT,
				}),
			),
		).jobs;
	}

	// Typing fires a load per keystroke, so only the latest one may land.
	let latest = 0;
	async function load() {
		const call = ++latest;
		const projects = targets.value;
		const key = projects.join(" ");
		if (!projects.length) {
			jobs.value = [];
			scheduleEntries.value = [];
			total.value = 0;
			unfilteredTotal.value = 0;
			loading.value = false;
			return;
		}
		try {
			const [statusText, list, entries] = await Promise.all([
				// The stat cards count the board's own projects, like its filters.
				callTool("project_status", { targets: projects }),
				listJobs(projects),
				listScheduleEntries(projects),
			]);
			// Every visible list card shows its job's latest transcript events
			// and terminal output, which stream while the job runs; a
			// scheduled card shows its run's only while it runs. Kanban cards
			// show none.
			const running = entries.filter(
				(e) =>
					e.schedule?.state === "starting" || e.schedule?.state === "running",
			);
			await Promise.all(
				(kanban.value ? [] : [...list.jobs, ...running]).map(async (j) => {
					try {
						const detail = normalizeJobDetail(
							parseToolJSON(
								await callTool("project_job_get", {
									target: j.target,
									number: j.number,
									lines: CARD_OUTPUT_LINES,
									events: CARD_OUTPUT_EVENTS,
								}),
							),
						);
						j.output = detail.job.output;
						j.events = detail.events;
					} catch {
						// Keep the card without output until the next poll.
					}
				}),
			);
			if (key !== targets.value.join(" ") || call !== latest) return;
			status.value = normalizeStatus(parseToolJSON(statusText));
			jobs.value = list.jobs;
			scheduleEntries.value = entries;
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

	watch(
		[
			() => targets.value.join(" "),
			() => states.value.join(","),
			kanban,
			withSchedules,
		],
		() => {
			page.value = 0;
			loading.value = true;
			void refresh();
		},
	);
	// A new search starts on the first page of its matches; when the page is
	// already first, the page watcher stays quiet, so load here.
	watch(search, () => {
		if (page.value === 0) void refresh();
		else page.value = 0;
	});
	watch(page, () => void refresh());

	async function action(
		tool: "project_job_retry" | "project_job_stop" | "project_job_dismiss",
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
		scheduleEntries,
		retry: (job: ProjectJob) => action("project_job_retry", job),
		stop: (job: ProjectJob) => action("project_job_stop", job),
		/** Takes a schedule's failed last run off the Failed state. */
		dismiss: (job: ProjectJob) => action("project_job_dismiss", job),
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

/**
 * The projects' scheduled prompts, kept live, with the next runs of the
 * board's one project; a board of several has no "Up next" strip.
 */
export function useProjectSchedules(
	project: Ref<string>,
	targets: Ref<string[]>,
	interval = 5_000,
) {
	const { callTool } = useMCP();
	const schedules = ref<ProjectSchedule[]>([]);
	const upcoming = ref<UpcomingRun[]>([]);
	const agents = ref<string[]>([]);
	const error = ref("");
	const loaded = ref(false);
	// A refresh can overlap a poll; only the latest call's lists are shown,
	// so an older poll can't hide a task a later refresh found.
	let loadSeq = 0;

	const { refresh } = usePoll(async () => {
		const target = project.value;
		const projects = targets.value;
		const key = projects.join(" ");
		if (!projects.length) return;
		const seq = ++loadSeq;
		try {
			const [taskText, upcomingText, agentText] = await Promise.all([
				callTool("task_list", {}),
				target
					? callTool("task_upcoming", {
							project: target,
							limit: UP_NEXT_RUNS,
							per_task: UP_NEXT_PER_SCHEDULE,
						})
					: Promise.resolve(""),
				agents.value.length ? Promise.resolve("") : callTool("agent_list", {}),
			]);
			if (
				seq !== loadSeq ||
				target !== project.value ||
				key !== targets.value.join(" ")
			)
				return;
			const tasks = parseToolJSON(taskText);
			schedules.value = projects.flatMap((t) => projectSchedules(tasks, t));
			upcoming.value = upcomingText
				? normalizeUpcoming(parseToolJSON(upcomingText))
				: [];
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

	watch([project, () => targets.value.join(" ")], () => {
		schedules.value = [];
		upcoming.value = [];
		loaded.value = false;
		void refresh();
	});

	// A task paused or resumed elsewhere, such as from the sidebar, shows on
	// its box at once rather than at the next poll.
	const settings = useSettingsStore();
	watch(
		() =>
			(settings.config?.agents ?? [])
				.flatMap((a) => a.tasks.map((t) => `${a.name}/${t.name}:${t.enabled}`))
				.join("\n"),
		() => {
			for (const s of schedules.value) {
				const task = settings.config?.agents
					.find((a) => a.name === s.agent)
					?.tasks.find((t) => t.name === s.name);
				if (task) s.enabled = task.enabled !== false;
			}
		},
	);

	/**
	 * Saves an existing project scheduled task. New ones are created through
	 * the shared Add task dialog, as any scheduled task is.
	 */
	async function save(s: ProjectSchedule) {
		await callTool("task_update", {
			name: s.id,
			...triggerArgs(s),
			content: s.prompt.trim(),
			model: s.model.trim(),
			project: s.project,
			enabled: s.enabled,
			precompile: s.precompile,
		});
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

	const preview = useTriggerPreview();

	return {
		schedules,
		upcoming,
		agents,
		error,
		loaded,
		refresh,
		save,
		remove,
		runNow,
		preview,
	};
}
