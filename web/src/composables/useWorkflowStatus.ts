import { computed, onMounted, onUnmounted, type Ref, ref, watch } from "vue";
import { parseToolJSON } from "../lib/projects";
import { skippedReplyReason } from "../lib/taskRuns";
import type { Job } from "../stores/jobs";
import { useMCP } from "./useMCP";

// The live state of an agent's workflows, from their recent runs: whether
// each is triggering (a run queued) or running, and how its last run ended.
// It polls quickly while a run is active and slowly otherwise.

/** A workflow's live state: idle, a run queued, or a run in progress. */
export type WorkflowActivity = "idle" | "triggering" | "running";

export interface WorkflowStatus {
	activity: WorkflowActivity;
	/**
	 * How the latest finished run ended, if any run finished; fallback means
	 * its precompiled script failed and the model ran it, and no_reply that it
	 * chose not to reply.
	 */
	last?: "succeeded" | "fallback" | "no_reply" | "failed";
	lastAt?: string;
	/** Why the latest finished run chose not to reply. */
	lastSkipReason?: string;
}

const ACTIVE_POLL_MS = 2_000;
const IDLE_POLL_MS = 10_000;

function day(daysAgo: number): string {
	const d = new Date();
	d.setDate(d.getDate() - daysAgo);
	return d.toISOString().slice(0, 10);
}

/** Workflow statuses from a set of jobs, keyed by "<agent>/<task>". */
export function workflowStatuses(jobs: Job[]): Map<string, WorkflowStatus> {
	const out = new Map<string, WorkflowStatus>();
	const sorted = [...jobs].sort((a, b) =>
		a.created_at.localeCompare(b.created_at),
	);
	for (const job of sorted) {
		const s = out.get(job.task_id) ?? { activity: "idle" };
		if (job.status === "in_progress") s.activity = "running";
		else if (job.status === "pending" && s.activity !== "running")
			s.activity = "triggering";
		else if (job.status === "completed" || job.status === "failed") {
			const skipReason =
				job.status === "completed" ? skippedReplyReason(job) : undefined;
			s.last =
				job.status === "failed"
					? "failed"
					: job.script_error
						? "fallback"
						: skipReason !== undefined
							? "no_reply"
							: "succeeded";
			s.lastAt = job.updated_at;
			s.lastSkipReason = skipReason;
		}
		out.set(job.task_id, s);
	}
	return out;
}

export function useWorkflowStatus(agent: Ref<string>) {
	const { callTool } = useMCP();
	const jobs = ref<Job[]>([]);
	const statuses = computed(() => workflowStatuses(jobs.value));

	function statusOf(task: string): WorkflowStatus {
		return statuses.value.get(`${agent.value}/${task}`) ?? { activity: "idle" };
	}

	async function refresh() {
		const name = agent.value;
		if (!name) return;
		try {
			const list = parseToolJSON(
				await callTool("job_query", {
					agent: name,
					start: day(7),
					end: day(0),
				}),
			) as Job[] | null;
			if (name === agent.value) jobs.value = list ?? [];
		} catch {
			// Keep the last known state; the next poll retries.
		}
	}

	let timer: ReturnType<typeof setTimeout> | undefined;
	function schedule() {
		clearTimeout(timer);
		const active = [...statuses.value.values()].some(
			(s) => s.activity !== "idle",
		);
		timer = setTimeout(
			async () => {
				if (document.visibilityState === "visible") await refresh();
				schedule();
			},
			active ? ACTIVE_POLL_MS : IDLE_POLL_MS,
		);
	}

	watch(agent, () => {
		jobs.value = [];
		void refresh().then(schedule);
	});
	onMounted(() => void refresh().then(schedule));
	onUnmounted(() => clearTimeout(timer));

	return { statusOf, refresh };
}
