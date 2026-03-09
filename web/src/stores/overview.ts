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
	const fetched = ref(false);
	const error = ref<string | null>(null);
	const lastChecked = ref<Date | null>(null);

	async function fetchAll() {
		loading.value = true;
		error.value = null;
		try {
			const [agentsRes, jobsRes, issuesRes] = await Promise.allSettled([
				callTool("agent_list"),
				callTool("job_list"),
				callTool("config_validate"),
			]);
			if (agentsRes.status === "fulfilled") {
				agents.value = (JSON.parse(agentsRes.value) as Agent[]) ?? [];
			} else {
				error.value =
					agentsRes.reason instanceof Error
						? agentsRes.reason.message
						: String(agentsRes.reason);
			}
			if (jobsRes.status === "fulfilled") {
				jobs.value = (JSON.parse(jobsRes.value) as Job[] | null) ?? [];
			}
			if (issuesRes.status === "fulfilled") {
				issues.value = (JSON.parse(issuesRes.value) as DoctorIssue[]) ?? [];
				lastChecked.value = new Date();
			}
		} finally {
			loading.value = false;
			fetched.value = true;
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
		fetched,
		error,
		lastChecked,
		fetchAll,
		recheck,
	};
});
