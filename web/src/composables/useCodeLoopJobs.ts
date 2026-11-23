import { computed, ref } from "vue";
import {
	normalizeJobList,
	type ProjectJob,
	parseToolJSON,
} from "../lib/projects";
import { useMCP } from "./useMCP";

// The Code Loop issue jobs a task or workflow can send its output to, shared
// by the target pickers and target links so they read the list once.

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
					kinds: ["issue"],
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

	function find(project: string, number: number): ProjectJob | undefined {
		const p = project.trim().toLowerCase();
		return jobs.value.find(
			(j) => j.number === number && j.target.toLowerCase() === p,
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
