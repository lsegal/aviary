import { defineStore } from "pinia";
import { ref } from "vue";
import { useMCP } from "../composables/useMCP";

export interface Job {
	id: string;
	task_id: string;
	agent_name: string;
	status: string;
	attempts: number;
	created_at: string;
	updated_at: string;
}

export const useTasksStore = defineStore("tasks", () => {
	const jobs = ref<Job[]>([]);
	const loading = ref(false);
	const runningTaskID = ref<string | null>(null);
	const runError = ref<string | null>(null);
	const lastStartedJob = ref<Job | null>(null);
	const { callTool } = useMCP();

	async function fetchJobs(taskID = "") {
		loading.value = true;
		try {
			const raw = await callTool("job_list", taskID ? { task: taskID } : {});
			jobs.value = (JSON.parse(raw) as Job[] | null) ?? [];
		} catch {
			jobs.value = [];
		} finally {
			loading.value = false;
		}
	}

	async function runTask(taskID: string) {
		runningTaskID.value = taskID;
		runError.value = null;
		try {
			const raw = await callTool("task_run", { name: taskID });
			lastStartedJob.value = (JSON.parse(raw) as Job | null) ?? null;
			await fetchJobs();
			return lastStartedJob.value;
		} catch (error) {
			runError.value = error instanceof Error ? error.message : String(error);
			throw error;
		} finally {
			runningTaskID.value = null;
		}
	}

	return {
		jobs,
		loading,
		runningTaskID,
		runError,
		lastStartedJob,
		fetchJobs,
		runTask,
	};
});
