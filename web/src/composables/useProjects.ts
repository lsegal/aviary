import { onMounted, onUnmounted, type Ref, ref, watch } from "vue";
import {
	type BoardFilter,
	filterStatuses,
	type JobDetail,
	normalizeJobDetail,
	normalizeJobList,
	normalizeStatus,
	type ProjectJob,
	type ProjectStatus,
	type ProjectSummary,
	parseToolJSON,
	sortJobs,
} from "../lib/projects";
import { useMCP } from "./useMCP";

/** How often the board and run view re-read the engine, in milliseconds. */
export const PROJECTS_POLL_MS = 2_000;
/** Lines of terminal output shown on a board card. */
export const CARD_OUTPUT_LINES = 12;
export const JOBS_PAGE_SIZE = 12;

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
			const status = normalizeStatus(
				parseToolJSON(await callTool("project_status", {})),
			);
			projects.value = status.projects;
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

	async function load() {
		const name = project.value;
		if (!name) {
			loading.value = false;
			return;
		}
		try {
			const [statusText, listText] = await Promise.all([
				callTool("project_status", { project: name }),
				callTool("project_job_list", {
					project: name,
					status: filterStatuses(filter.value),
					offset: page.value * JOBS_PAGE_SIZE,
					limit: JOBS_PAGE_SIZE,
				}),
			]);
			const list = normalizeJobList(parseToolJSON(listText));
			const sorted = sortJobs(list.jobs);
			// Running jobs stream their terminal output onto the card.
			await Promise.all(
				sorted
					.filter((j) => j.status === "running" && j.id)
					.map(async (j) => {
						try {
							const detail = normalizeJobDetail(
								parseToolJSON(
									await callTool("project_job_get", {
										id: j.id,
										lines: CARD_OUTPUT_LINES,
									}),
								),
							);
							if (detail.output.length > 0) j.output = detail.output;
						} catch {
							// Keep whatever output the list returned.
						}
					}),
			);
			if (name !== project.value) return;
			status.value = normalizeStatus(parseToolJSON(statusText));
			jobs.value = sorted;
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
		id: string,
	) {
		await callTool(tool, { id });
		await refresh();
	}

	async function checkNow() {
		await callTool("project_refresh", { project: project.value });
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
		retry: (id: string) => action("project_job_retry", id),
		stop: (id: string) => action("project_job_stop", id),
	};
}

/** One job's state, typed event timeline and raw output, kept live. */
export function useProjectJob(id: Ref<string>) {
	const { callTool } = useMCP();
	const detail = ref<JobDetail | null>(null);
	const error = ref("");

	const { refresh } = usePoll(async () => {
		const jobID = id.value;
		try {
			const next = normalizeJobDetail(
				parseToolJSON(await callTool("project_job_get", { id: jobID })),
			);
			if (jobID !== id.value) return;
			detail.value = next;
			error.value = "";
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
		}
	}, PROJECTS_POLL_MS);

	watch(id, () => {
		detail.value = null;
		void refresh();
	});

	async function action(tool: string) {
		await callTool(tool, { id: id.value });
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
