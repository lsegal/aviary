import { onMounted, onUnmounted, type Ref, ref, watch } from "vue";
import {
	type BoardFilter,
	filterStatuses,
	isRunning,
	type JobDetail,
	normalizeJobDetail,
	normalizeJobList,
	normalizeStatus,
	type ProjectJob,
	type ProjectStatus,
	type ProjectSummary,
	parseToolJSON,
	sortJobs,
	summarizeProjects,
} from "../lib/projects";
import { useMCP } from "./useMCP";

/** How often the board and run view re-read the engine, in milliseconds. */
export const PROJECTS_POLL_MS = 2_000;
/** Lines of terminal output shown on a board card. */
export const CARD_OUTPUT_LINES = 12;
export const JOBS_PAGE_SIZE = 12;
// project_job_list's page size when the board filters by several statuses
// and pages the merged result itself.
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

/** A project's live status and one page of its jobs with their recent output. */
export function useProjectBoard(
	project: Ref<string>,
	filter: Ref<BoardFilter>,
) {
	const { callTool } = useMCP();
	const status = ref<ProjectStatus | null>(null);
	const jobs = ref<ProjectJob[]>([]);
	const total = ref(0);
	const page = ref(0);
	const error = ref("");
	const loading = ref(true);

	async function listJobs(target: string) {
		const statuses = filterStatuses(filter.value);
		if (!statuses) {
			const list = normalizeJobList(
				parseToolJSON(
					await callTool("project_job_list", {
						target,
						offset: page.value * JOBS_PAGE_SIZE,
						limit: JOBS_PAGE_SIZE,
					}),
				),
			);
			return { jobs: sortJobs(list.jobs), total: list.total };
		}
		// project_job_list filters by one status, so a tab that shows several
		// merges them and pages the result here.
		const lists = await Promise.all(
			statuses.map(async (s) =>
				normalizeJobList(
					parseToolJSON(
						await callTool("project_job_list", {
							target,
							status: s,
							limit: ALL_JOBS_LIMIT,
						}),
					),
				),
			),
		);
		const all = sortJobs(lists.flatMap((l) => l.jobs));
		const start = page.value * JOBS_PAGE_SIZE;
		return { jobs: all.slice(start, start + JOBS_PAGE_SIZE), total: all.length };
	}

	async function load() {
		const target = project.value;
		if (!target) {
			loading.value = false;
			return;
		}
		try {
			const [statusText, list] = await Promise.all([
				callTool("project_status", {}),
				listJobs(target),
			]);
			// Running jobs stream their terminal output onto the card.
			await Promise.all(
				list.jobs
					.filter((j) => isRunning(j.status))
					.map(async (j) => {
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
			if (target !== project.value) return;
			status.value = normalizeStatus(parseToolJSON(statusText));
			jobs.value = list.jobs;
			total.value = list.total;
			error.value = "";
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
		} finally {
			loading.value = false;
		}
	}

	const { refresh } = usePoll(load, PROJECTS_POLL_MS);

	watch([project, filter], () => {
		page.value = 0;
		loading.value = true;
		void refresh();
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
		await callTool("project_refresh", { target: project.value });
		await refresh();
	}

	return {
		status,
		jobs,
		total,
		page,
		error,
		loading,
		refresh,
		checkNow,
		retry: (job: ProjectJob) => action("project_job_retry", job),
		stop: (job: ProjectJob) => action("project_job_stop", job),
	};
}

/** One job's state, typed event timeline and raw output, kept live. */
export function useProjectJob(target: Ref<string>, number: Ref<number>) {
	const { callTool } = useMCP();
	const detail = ref<JobDetail | null>(null);
	const error = ref("");

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
		refresh,
		retry: () => action("project_job_retry"),
		stop: () => action("project_job_stop"),
	};
}
