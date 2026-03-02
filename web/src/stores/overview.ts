import { defineStore } from "pinia";
import { ref } from "vue";
import { useMCP } from "../composables/useMCP";
import type { Agent } from "./agents";
import type { Job } from "./tasks";

export interface DoctorIssue {
	level: "ERROR" | "WARN";
	field: string;
	message: string;
}

export const useOverviewStore = defineStore("overview", () => {
	const { callTool } = useMCP();

	const agents = ref<Agent[]>([]);
	const jobs = ref<Job[]>([]);
	const issues = ref<DoctorIssue[]>([]);
	const loading = ref(false);
	const error = ref<string | null>(null);
	const lastChecked = ref<Date | null>(null);

	async function fetchAll() {
		loading.value = true;
		error.value = null;
		try {
			const [agentsRaw, jobsRaw, issuesRaw] = await Promise.all([
				callTool("agent_list"),
				callTool("job_list"),
				callTool("config_validate"),
			]);
			agents.value = (JSON.parse(agentsRaw) as Agent[]) ?? [];
			jobs.value = (JSON.parse(jobsRaw) as Job[] | null) ?? [];
			issues.value = (JSON.parse(issuesRaw) as DoctorIssue[]) ?? [];
			lastChecked.value = new Date();
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
		} finally {
			loading.value = false;
		}
	}

	async function recheck() {
		try {
			const raw = await callTool("config_validate");
			issues.value = (JSON.parse(raw) as DoctorIssue[]) ?? [];
			lastChecked.value = new Date();
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
		}
	}

	return {
		agents,
		jobs,
		issues,
		loading,
		error,
		lastChecked,
		fetchAll,
		recheck,
	};
});
