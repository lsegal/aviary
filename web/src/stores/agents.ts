import { defineStore } from "pinia";
import { ref } from "vue";
import { useMCP } from "../composables/useMCP";

export interface Agent {
	id: string;
	name: string;
	model: string;
	fallbacks: string[];
	state: string;
}

export interface AgentConfig {
	name: string;
	model: string;
	fallbacks: string[];
	channels: unknown[];
	tasks: unknown[];
}

export interface AgentUpsert {
	name: string;
	model?: string;
	fallbacks?: string[];
}

export const useAgentsStore = defineStore("agents", () => {
	const agents = ref<Agent[]>([]);
	const loading = ref(false);
	const error = ref<string | null>(null);
	const { callTool } = useMCP();

	async function fetchAgents() {
		loading.value = true;
		error.value = null;
		try {
			const raw = await callTool("agent_list");
			agents.value = (JSON.parse(raw) as Agent[]) ?? [];
		} catch (e) {
			agents.value = [];
			error.value = e instanceof Error ? e.message : String(e);
		} finally {
			loading.value = false;
		}
	}

	async function getAgent(name: string): Promise<AgentConfig> {
		const raw = await callTool("agent_get", { name });
		return JSON.parse(raw) as AgentConfig;
	}

	async function addAgent(data: AgentUpsert): Promise<void> {
		await callTool("agent_add", { ...data, fallbacks: data.fallbacks ?? [] });
		await fetchAgents();
	}

	async function updateAgent(data: AgentUpsert): Promise<void> {
		await callTool("agent_update", {
			...data,
			fallbacks: data.fallbacks ?? [],
		});
		await fetchAgents();
	}

	async function deleteAgent(name: string): Promise<void> {
		await callTool("agent_delete", { name });
		await fetchAgents();
	}

	return {
		agents,
		loading,
		error,
		fetchAgents,
		getAgent,
		addAgent,
		updateAgent,
		deleteAgent,
	};
});
