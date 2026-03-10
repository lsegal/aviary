import { defineStore } from "pinia";
import { computed, ref } from "vue";
import { useMCP } from "../composables/useMCP";

export interface Job {
	id: string;
	task_id: string;
	agent_id: string;
	agent_name: string;
	prompt: string;
	status: "pending" | "in_progress" | "completed" | "failed";
	attempts: number;
	max_retries: number;
	output?: string;
	locked_at?: string;
	next_retry_at?: string;
	scheduled_for?: string;
	created_at: string;
	updated_at: string;
}

export interface ScheduledTask {
	id: string;
	agent_id: string;
	agent_name: string;
	name: string;
	trigger_type: "cron" | "watch";
	schedule?: string;
	start_at?: string;
	run_once?: boolean;
	watch?: string;
	prompt: string;
	channel?: string;
}

function fmtDate(daysAgo: number): string {
	const d = new Date();
	d.setDate(d.getDate() - daysAgo);
	return d.toISOString().slice(0, 10);
}

export const useJobsStore = defineStore("jobs", () => {
	const { callTool } = useMCP();

	const jobs = ref<Job[]>([]);
	const scheduledTasks = ref<ScheduledTask[]>([]);
	const loading = ref(false);
	const tasksLoading = ref(false);
	const error = ref<string | null>(null);
	const startDate = ref<string>(fmtDate(7));
	const endDate = ref<string>(fmtDate(0));

	async function fetch() {
		loading.value = true;
		error.value = null;
		try {
			const raw = await callTool("job_query", {
				start: startDate.value,
				end: endDate.value,
			});
			jobs.value = (JSON.parse(raw) as Job[] | null) ?? [];
			// Sort newest first.
			jobs.value.sort((a, b) => b.created_at.localeCompare(a.created_at));
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
		} finally {
			loading.value = false;
		}
	}

	async function fetchScheduledTasks() {
		tasksLoading.value = true;
		error.value = null;
		try {
			const raw = await callTool("task_list", {});
			scheduledTasks.value = (JSON.parse(raw) as ScheduledTask[] | null) ?? [];
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
		} finally {
			tasksLoading.value = false;
		}
	}

	async function fetchLogs(jobID: string): Promise<string> {
		try {
			return await callTool("job_logs", { id: jobID });
		} catch (e) {
			return `Error: ${e instanceof Error ? e.message : String(e)}`;
		}
	}

	async function runTaskNow(taskID: string): Promise<Job | null> {
		const raw = await callTool("task_run", { name: taskID });
		await Promise.all([fetch(), fetchScheduledTasks()]);
		return (JSON.parse(raw) as Job | null) ?? null;
	}

	async function runJobNow(jobID: string): Promise<Job | null> {
		const raw = await callTool("job_run_now", { id: jobID });
		await Promise.all([fetch(), fetchScheduledTasks()]);
		return (JSON.parse(raw) as Job | null) ?? null;
	}

	function setPreset(days: number) {
		endDate.value = fmtDate(0);
		startDate.value = fmtDate(days);
		fetch();
	}

	// ── Counts ────────────────────────────────────────────────────────────────

	const pending = computed(() =>
		jobs.value.filter((j) => j.status === "pending"),
	);
	const running = computed(() =>
		jobs.value.filter((j) => j.status === "in_progress"),
	);
	const completed = computed(() =>
		jobs.value.filter((j) => j.status === "completed"),
	);
	const failed = computed(() =>
		jobs.value.filter((j) => j.status === "failed"),
	);

	// ── Daily chart ──────────────────────────────────────────────────────────

	const byDay = computed(() => {
		const m = new Map<
			string,
			{ completed: number; failed: number; running: number }
		>();
		for (const j of jobs.value) {
			const d = j.created_at.slice(0, 10);
			const v = m.get(d) ?? { completed: 0, failed: 0, running: 0 };
			if (j.status === "completed") v.completed++;
			else if (j.status === "failed") v.failed++;
			else v.running++;
			m.set(d, v);
		}
		const result: Array<{
			date: string;
			completed: number;
			failed: number;
			running: number;
		}> = [];
		const cur = new Date(startDate.value);
		const endD = new Date(endDate.value);
		while (cur <= endD) {
			const key = cur.toISOString().slice(0, 10);
			result.push({
				date: key,
				...(m.get(key) ?? { completed: 0, failed: 0, running: 0 }),
			});
			cur.setDate(cur.getDate() + 1);
		}
		return result;
	});

	// ── By agent breakdown ────────────────────────────────────────────────────

	const byAgent = computed(() => {
		const m = new Map<
			string,
			{ completed: number; failed: number; total: number }
		>();
		for (const j of jobs.value) {
			const v = m.get(j.agent_name) ?? { completed: 0, failed: 0, total: 0 };
			v.total++;
			if (j.status === "completed") v.completed++;
			if (j.status === "failed") v.failed++;
			m.set(j.agent_name, v);
		}
		return [...m.entries()]
			.sort((a, b) => b[1].total - a[1].total)
			.map(([name, counts]) => ({ name, ...counts }));
	});

	return {
		jobs,
		scheduledTasks,
		loading,
		tasksLoading,
		error,
		startDate,
		endDate,
		fetch,
		fetchScheduledTasks,
		fetchLogs,
		runTaskNow,
		runJobNow,
		setPreset,
		pending,
		running,
		completed,
		failed,
		byDay,
		byAgent,
	};
});
