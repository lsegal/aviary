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
	/** Why the latest attempt failed; cleared when an attempt completes. */
	error?: string;
	locked_at?: string;
	next_retry_at?: string;
	scheduled_for?: string;
	created_at: string;
	updated_at: string;
}

export interface SessionMessage {
	id?: string;
	role?: "assistant" | "user" | "tool" | string;
	timestamp?: string;
	content?: string | null;
	media_urls?: string[] | null;
	[key: string]: unknown;
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

/** How long a finished job ran, from its creation to its last update. */
export function jobDurationMs(job: Job): number {
	return (
		new Date(job.updated_at).getTime() - new Date(job.created_at).getTime()
	);
}

/** A duration as 850ms, 4.2s or 3m 12s. */
export function formatDuration(ms: number): string {
	if (ms < 1000) return `${Math.round(ms)}ms`;
	if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
	return `${Math.floor(ms / 60_000)}m ${Math.floor((ms % 60_000) / 1000)}s`;
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
	/** The agent whose runs the store holds; empty holds none. */
	const agent = ref("");
	const loading = ref(false);
	const compilesLoading = ref(false);
	const error = ref<string | null>(null);
	const startDate = ref<string>(fmtDate(7));
	const endDate = ref<string>(fmtDate(0));

	/** Points the store at an agent's runs, clearing another agent's. */
	function setAgent(name: string) {
		if (agent.value === name) return;
		agent.value = name;
		jobs.value = [];
		taskCompiles.value = [];
	}

	async function fetch() {
		if (!agent.value) return;
		const name = agent.value;
		loading.value = true;
		error.value = null;
		try {
			const raw = await callTool("job_query", {
				start: startDate.value,
				end: endDate.value,
				agent: name,
			});
			if (agent.value !== name) return;
			jobs.value = ((JSON.parse(raw) as Job[] | null) ?? []).filter(
				(j) => j.agent_id === name,
			);
			// Sort newest first.
			jobs.value.sort((a, b) => b.created_at.localeCompare(a.created_at));
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
		} finally {
			loading.value = false;
		}
	}

	async function fetchTaskCompiles() {
		if (!agent.value) return;
		const name = agent.value;
		compilesLoading.value = true;
		error.value = null;
		try {
			const raw = await callTool("task_compile_query", {
				start: startDate.value,
				end: endDate.value,
			});
			if (agent.value !== name) return;
			taskCompiles.value = (
				(JSON.parse(raw) as TaskCompile[] | null) ?? []
			).filter((c) => c.agent_id === name);
			taskCompiles.value.sort((a, b) =>
				b.created_at.localeCompare(a.created_at),
			);
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
		} finally {
			compilesLoading.value = false;
		}
	}

	async function refreshAll() {
		// Streamable HTTP MCP requests on the live server are more reliable when
		// this page initializes sequentially instead of issuing both calls at once.
		await fetch();
		await fetchTaskCompiles();
	}

	async function fetchLogs(jobID: string): Promise<string> {
		try {
			return await callTool("job_logs", { id: jobID });
		} catch (e) {
			return `Error: ${e instanceof Error ? e.message : String(e)}`;
		}
	}

	async function fetchSessionMessages(
		agentID: string,
		sessionID: string,
	): Promise<SessionMessage[]> {
		try {
			const raw = await callTool("session_messages", {
				agent: agentID,
				session_id: sessionID,
				order: "asc",
			});
			return (JSON.parse(raw) as SessionMessage[]) ?? [];
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

	async function stopJob(jobID: string): Promise<void> {
		await callTool("job_stop", { id: jobID });
		await refreshAll();
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

	return {
		jobs,
		taskCompiles,
		agent,
		setAgent,
		loading,
		compilesLoading,
		error,
		startDate,
		endDate,
		fetch,
		fetchTaskCompiles,
		refreshAll,
		fetchLogs,
		fetchTaskCompile,
		runTaskNow,
		runJobNow,
		stopJob,
		fetchSessionMessages,
		setPreset,
		pending,
		running,
		completed,
		failed,
		byDay,
	};
});
