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

	async function loadSection<T>(
		loader: () => Promise<string>,
		assign: (value: T) => void,
	): Promise<void> {
		try {
			assign((JSON.parse(await loader()) as T) ?? ([] as T));
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
		}
	}

	async function fetchAll() {
		loading.value = true;
		error.value = null;
		try {
			// MCP streamable HTTP sessions are reliable here when tool calls are
			// issued in sequence; concurrent overview requests can leave the page
			// stuck on the initial spinner against the live server.
			await loadSection<Agent[]>(
				() => callTool("agent_list"),
				(value) => {
					agents.value = value;
				},
			);
			await loadSection<Job[] | null>(
				() => callTool("job_list"),
				(value) => {
					jobs.value = value ?? [];
				},
			);
			await loadSection<DoctorIssue[]>(
				() => callTool("config_validate"),
				(value) => {
					issues.value = value;
					lastChecked.value = new Date();
				},
			);
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
