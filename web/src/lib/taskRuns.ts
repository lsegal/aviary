import { formatDuration, type Job, jobDurationMs } from "../stores/jobs";

// One scheduled task's runs, as its dashboard shows them: its jobs, plus the
// fires its run history records that never became a job (skipped by the
// overlap policy, or missed while Aviary was stopped).

export type TaskRunResult =
	| "queued"
	| "running"
	| "succeeded"
	| "fallback"
	| "failed"
	| "canceled"
	| "skipped";

/** One fire of a task, from the scheduler's task_list history. */
export interface TaskFire {
	fire_time: string;
	reason: string;
	status: string;
	job_id?: string;
	summary?: string;
	duration_ms?: number;
}

/** The scheduler's view of a task, from task_list. */
export interface ScheduledTaskInfo {
	id: string;
	agent_name: string;
	name: string;
	enabled: boolean;
	description?: string;
	next_run?: string;
	history?: TaskFire[];
}

export interface TaskRunEntry {
	/** The job's ID, or `fire-<time>` for a fire with no job. */
	id: string;
	job?: Job;
	result: TaskRunResult;
	/** schedule, missed, watch or manual; empty when unknown. */
	trigger: string;
	startedAt: string;
	/** How long a finished run took; undefined while it is active. */
	durationMs?: number;
	/** Why a failed, skipped or missed run did not succeed. */
	error?: string;
	/** Whether the precompiled script failed and the model ran the task. */
	scriptFailed?: boolean;
}

const JOB_RESULTS: Record<Job["status"], TaskRunResult> = {
	pending: "queued",
	in_progress: "running",
	completed: "succeeded",
	failed: "failed",
	canceled: "canceled",
};

export const RESULT_LABELS: Record<TaskRunResult, string> = {
	queued: "Queued",
	running: "Running",
	succeeded: "Succeeded",
	fallback: "Succeeded via fallback",
	failed: "Failed",
	canceled: "Canceled",
	skipped: "Skipped",
};

const TRIGGER_LABELS: Record<string, string> = {
	schedule: "schedule",
	missed: "catch-up",
	watch: "file watch",
	manual: "manual",
};

/** What fired a run, as its history shows it. */
export function triggerLabel(trigger: string): string {
	return TRIGGER_LABELS[trigger] ?? (trigger || "—");
}

export function isActive(entry: TaskRunEntry): boolean {
	return entry.result === "queued" || entry.result === "running";
}

/** A task's runs, newest first. */
export function taskRunEntries(
	jobs: Job[],
	history: TaskFire[] = [],
): TaskRunEntry[] {
	const reasons = new Map(
		history.filter((f) => f.job_id).map((f) => [f.job_id, f.reason]),
	);
	const out: TaskRunEntry[] = jobs.map((job) => {
		const scriptFailed = !!job.script_error;
		let result = JOB_RESULTS[job.status] ?? "queued";
		if (result === "succeeded" && scriptFailed) result = "fallback";
		const active = result === "queued" || result === "running";
		return {
			id: job.id,
			job,
			result,
			trigger: job.trigger || reasons.get(job.id) || "",
			startedAt: job.created_at,
			durationMs: active ? undefined : jobDurationMs(job),
			error: result === "failed" ? job.error || undefined : undefined,
			scriptFailed,
		};
	});
	for (const fire of history) {
		if (fire.job_id) continue;
		const skipped = fire.status === "skipped" || fire.status === "missed";
		if (!skipped && fire.status !== "failed") continue;
		out.push({
			id: `fire-${fire.fire_time}`,
			result: skipped ? "skipped" : "failed",
			trigger: fire.reason,
			startedAt: fire.fire_time,
			error:
				fire.summary ||
				(fire.status === "missed"
					? "missed while Aviary was stopped"
					: undefined),
		});
	}
	return out.sort((a, b) => b.startedAt.localeCompare(a.startedAt));
}

export interface TaskRunStats {
	queued: number;
	running: number;
	succeeded: number;
	/** Succeeded runs whose precompiled script failed first. */
	fallback: number;
	failed: number;
	/** Runs whose precompiled script failed, whatever the fallback did. */
	scriptFailures: number;
	/** Share of finished runs that succeeded, by script or fallback, 0–100; null before any finish. */
	successRate: number | null;
	last?: TaskRunEntry;
	avgDurationMs: number | null;
}

const FINISHED: TaskRunResult[] = ["succeeded", "fallback", "failed"];

export function taskRunStats(entries: TaskRunEntry[]): TaskRunStats {
	const count = (r: TaskRunResult) =>
		entries.filter((e) => e.result === r).length;
	const succeeded = count("succeeded");
	const fallback = count("fallback");
	const failed = count("failed");
	const ok = succeeded + fallback;
	const timed = entries.filter((e) => e.job && FINISHED.includes(e.result));
	return {
		queued: count("queued"),
		running: count("running"),
		succeeded,
		fallback,
		failed,
		scriptFailures: entries.filter((e) => e.scriptFailed).length,
		successRate: ok + failed ? Math.round((ok / (ok + failed)) * 100) : null,
		last: entries.find((e) => e.job && !isActive(e)),
		avgDurationMs: timed.length
			? timed.reduce((sum, e) => sum + (e.durationMs ?? 0), 0) / timed.length
			: null,
	};
}

/** A run's duration so far: its final one, or the time since it started. */
export function runDurationText(entry: TaskRunEntry, now: number): string {
	if (entry.durationMs !== undefined) return formatDuration(entry.durationMs);
	if (entry.result !== "running") return "—";
	return formatDuration(Math.max(0, now - Date.parse(entry.startedAt)));
}
