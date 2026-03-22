import { defineStore } from "pinia";
import { computed, ref } from "vue";
import { useMCP } from "../composables/useMCP";

export interface Job {
	id: string;
	task_id: string;
	agent_id: string;
	session_id?: string;
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

export interface TaskCompileStep {
	kind: string;
	deterministic: boolean;
	tool?: string;
	description: string;
}

export interface TaskCompileStage {
	name: string;
	status: string;
	system_prompt?: string;
	user_prompt?: string;
	response?: string;
	error?: string;
	started_at: string;
	finished_at?: string;
}

export interface TaskCompile {
	id: string;
	agent_id: string;
	task_name?: string;
	requested_task_type?: string;
	result_task_type?: string;
	trigger?: string;
	target?: string;
	prompt?: string;
	run_discovery?: boolean;
	needs_discovery?: boolean;
	deterministic_steps?: number;
	validated?: boolean;
	status: "succeeded" | "skipped" | "failed";
	reason?: string;
	steps?: TaskCompileStep[];
	script?: string;
	stages?: TaskCompileStage[];
	created_at: string;
	updated_at: string;
}

export interface ScheduledTask {
	id: string;
	agent_id: string;
	agent_name: string;
	name: string;
	type?: "prompt" | "script";
	trigger_type: "cron" | "watch";
	schedule?: string;
	start_at?: string;
	run_once?: boolean;
	watch?: string;
	prompt?: string;
	script?: string;
	target?: string;
}

function isScheduledTask(value: unknown): value is ScheduledTask {
	if (!value || typeof value !== "object") return false;
	const task = value as Record<string, unknown>;
	return (
		typeof task.id === "string" &&
		typeof task.agent_id === "string" &&
		typeof task.agent_name === "string" &&
		typeof task.name === "string" &&
		(task.type === undefined ||
			task.type === "prompt" ||
			task.type === "script") &&
		(task.prompt === undefined || typeof task.prompt === "string") &&
		(task.script === undefined || typeof task.script === "string") &&
		(task.trigger_type === "cron" || task.trigger_type === "watch")
	);
}

function parseScheduledTasks(raw: string): ScheduledTask[] {
	const parsed = JSON.parse(raw) as unknown;
	if (!Array.isArray(parsed)) return [];
	return parsed.filter(isScheduledTask);
}

function fmtDate(daysAgo: number): string {
	const d = new Date();
	d.setDate(d.getDate() - daysAgo);
	return d.toISOString().slice(0, 10);
}

export const useJobsStore = defineStore("jobs", () => {
	const { callTool } = useMCP();

	const jobs = ref<Job[]>([]);
	const taskCompiles = ref<TaskCompile[]>([]);
	const scheduledTasks = ref<ScheduledTask[]>([]);
	const loading = ref(false);
	const compilesLoading = ref(false);
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

	async function fetchTaskCompiles() {
		compilesLoading.value = true;
		error.value = null;
		try {
			const raw = await callTool("task_compile_query", {
				start: startDate.value,
				end: endDate.value,
			});
			taskCompiles.value = (JSON.parse(raw) as TaskCompile[] | null) ?? [];
			taskCompiles.value.sort((a, b) =>
				b.created_at.localeCompare(a.created_at),
			);
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
		} finally {
			compilesLoading.value = false;
		}
	}

	async function fetchScheduledTasks() {
		tasksLoading.value = true;
		error.value = null;
		try {
			const raw = await callTool("task_list", {});
			scheduledTasks.value = parseScheduledTasks(raw);
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
		} finally {
			tasksLoading.value = false;
		}
	}

	async function refreshAll() {
		// Streamable HTTP MCP requests on the live server are more reliable when
		// this page initializes sequentially instead of issuing both calls at once.
		await fetch();
		await fetchTaskCompiles();
		await fetchScheduledTasks();
	}

	async function fetchLogs(jobID: string): Promise<string> {
		try {
			return await callTool("job_logs", { id: jobID });
		} catch (e) {
			return `Error: ${e instanceof Error ? e.message : String(e)}`;
		}
	}

	async function fetchSessionMessages(agentID: string, sessionID: string) {
		try {
			const raw = await callTool("session_messages", {
				agent: agentID,
				session_id: sessionID,
				order: "asc",
			});
			return (JSON.parse(raw) as any[]) ?? [];
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
			return [];
		}
	}

	async function fetchTaskCompile(id: string): Promise<TaskCompile | null> {
		try {
			const raw = await callTool("task_compile_get", { id });
			return (JSON.parse(raw) as TaskCompile | null) ?? null;
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
			return null;
		}
	}

	async function runTaskNow(taskID: string): Promise<Job | null> {
		const raw = await callTool("task_run", { name: taskID });
		await refreshAll();
		return (JSON.parse(raw) as Job | null) ?? null;
	}

	async function runJobNow(jobID: string): Promise<Job | null> {
		const raw = await callTool("job_run_now", { id: jobID });
		await refreshAll();
		return (JSON.parse(raw) as Job | null) ?? null;
	}

	function setPreset(days: number) {
		endDate.value = fmtDate(0);
		startDate.value = fmtDate(days);
		fetch();
		fetchTaskCompiles();
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
			const v = m.get(j.agent_id) ?? { completed: 0, failed: 0, total: 0 };
			v.total++;
			if (j.status === "completed") v.completed++;
			if (j.status === "failed") v.failed++;
			m.set(j.agent_id, v);
		}
		return [...m.entries()]
			.sort((a, b) => b[1].total - a[1].total)
			.map(([name, counts]) => ({ name, ...counts }));
	});

	return {
		jobs,
		taskCompiles,
		scheduledTasks,
		loading,
		compilesLoading,
		tasksLoading,
		error,
		startDate,
		endDate,
		fetch,
		fetchTaskCompiles,
		fetchScheduledTasks,
		refreshAll,
		fetchLogs,
		fetchTaskCompile,
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
