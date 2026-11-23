import { computed, onMounted, onUnmounted, type Ref, ref, watch } from "vue";
import { parseToolJSON } from "../lib/projects";
import {
	isActive,
	type ScheduledTaskInfo,
	taskRunEntries,
	taskRunStats,
} from "../lib/taskRuns";
import type { Job } from "../stores/jobs";
import { useMCP } from "./useMCP";

// One scheduled task's runs and schedule, kept current while its dashboard
// is open: quickly while a run is queued or running, slowly otherwise.

const ACTIVE_POLL_MS = 3_000;
const IDLE_POLL_MS = 15_000;

export function useTaskDashboard(agent: Ref<string>, task: Ref<string>) {
	const { callTool } = useMCP();

	const jobs = ref<Job[]>([]);
	const info = ref<ScheduledTaskInfo | null>(null);
	const loaded = ref(false);
	const error = ref("");
	const now = ref(Date.now());

	const taskID = computed(() => `${agent.value}/${task.value}`);
	const entries = computed(() =>
		taskRunEntries(jobs.value, info.value?.history ?? []),
	);
	const stats = computed(() => taskRunStats(entries.value));
	/** The run in flight, the oldest running one first. */
	const current = computed(
		() =>
			entries.value.findLast((e) => e.result === "running") ??
			entries.value.findLast((e) => e.result === "queued"),
	);

	async function refresh() {
		const id = taskID.value;
		try {
			// One at a time: the live server's MCP transport prefers sequential calls.
			const list = parseToolJSON(await callTool("job_list", { task: id })) as
				| Job[]
				| null;
			const tasks = parseToolJSON(await callTool("task_list", {})) as
				| ScheduledTaskInfo[]
				| null;
			if (id !== taskID.value) return;
			jobs.value = list ?? [];
			info.value = (tasks ?? []).find((t) => t.id === id) ?? null;
			error.value = "";
		} catch (e) {
			if (id === taskID.value)
				error.value = e instanceof Error ? e.message : String(e);
		} finally {
			if (id === taskID.value) loaded.value = true;
			now.value = Date.now();
		}
	}

	async function act(fn: () => Promise<unknown>) {
		try {
			await fn();
			error.value = "";
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
		}
		await refresh();
		schedule();
	}

	const runNow = () => act(() => callTool("task_run", { name: taskID.value }));
	const stop = (jobID: string) =>
		act(() => callTool("job_stop", { id: jobID }));
	const setEnabled = (enabled: boolean) =>
		act(() => callTool("task_update", { name: taskID.value, enabled }));

	async function fetchLogs(jobID: string): Promise<string> {
		return callTool("job_logs", { id: jobID });
	}

	let timer: ReturnType<typeof setTimeout> | undefined;
	function schedule() {
		clearTimeout(timer);
		const active = entries.value.some(isActive);
		timer = setTimeout(
			async () => {
				if (document.visibilityState === "visible") await refresh();
				else now.value = Date.now();
				schedule();
			},
			active ? ACTIVE_POLL_MS : IDLE_POLL_MS,
		);
	}

	watch(taskID, () => {
		jobs.value = [];
		info.value = null;
		loaded.value = false;
		void refresh().then(schedule);
	});
	onMounted(() => void refresh().then(schedule));
	onUnmounted(() => clearTimeout(timer));

	return {
		jobs,
		info,
		entries,
		stats,
		current,
		loaded,
		error,
		now,
		refresh,
		runNow,
		stop,
		setEnabled,
		fetchLogs,
	};
}

export type TaskDashboard = ReturnType<typeof useTaskDashboard>;
