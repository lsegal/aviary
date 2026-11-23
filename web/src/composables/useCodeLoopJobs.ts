import { computed, ref } from "vue";
import {
	normalizeJobList,
	type ProjectJob,
	parseToolJSON,
} from "../lib/projects";
import { useMCP } from "./useMCP";

// The Code Loop jobs a task or workflow can send its output to: issue jobs,
// Discussion jobs, and schedules, one entry each. Shared by the target pickers
// and target links so they read the list once.

/** The job kinds that accept task and workflow output. */
export const CODELOOP_TARGET_KINDS = ["issue", "discussion", "schedule"];

const jobs = ref<ProjectJob[]>([]);
const loaded = ref(false);
const loading = ref(false);
let pending: Promise<void> | null = null;

export function useCodeLoopJobs() {
	const { callTool } = useMCP();

	async function load(force = false): Promise<void> {
		if (pending) return pending;
		if (loaded.value && !force) return;
		loading.value = true;
		pending = (async () => {
			try {
				const text = await callTool("project_job_list", {
					kinds: CODELOOP_TARGET_KINDS,
					limit: 1000,
				});
				jobs.value = normalizeJobList(parseToolJSON(text)).jobs;
			} catch {
				jobs.value = [];
			} finally {
				loaded.value = true;
				loading.value = false;
				pending = null;
			}
		})();
		return pending;
	}

	// Any run of a schedule names the schedule, so a target saved from an
	// earlier run still finds its entry.
	function find(project: string, number: number): ProjectJob | undefined {
		const p = project.trim().toLowerCase();
		return jobs.value.find(
			(j) =>
				j.target.toLowerCase() === p &&
				(j.number === number ||
					!!j.schedule?.recentRuns.some((r) => r.number === number)),
		);
	}

	return {
		jobs: computed(() => jobs.value),
		loaded: computed(() => loaded.value),
		loading: computed(() => loading.value),
		load,
		find,
	};
}
