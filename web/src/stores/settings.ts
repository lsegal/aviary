import { defineStore } from "pinia";
import { ref } from "vue";
import { useMCP } from "../composables/useMCP";

export interface TLSConfig {
	cert: string;
	key: string;
}

export interface ServerConfig {
	port: number;
	tls: TLSConfig;
}

export interface AgentEntry {
	name: string;
	model: string;
	memory?: string;
	rules?: string;
	fallbacks: string[];
	channels: unknown[];
	tasks: AgentTask[];
}

export interface AgentTask {
	name: string;
	prompt: string;
	schedule?: string;
	start_at?: string;
	run_once?: boolean;
	watch?: string;
	channel?: string;
}

export interface ProviderConfig {
	auth: string;
}

export interface ModelDefaults {
	model: string;
	fallbacks: string[];
}

export interface ModelsConfig {
	providers: Record<string, ProviderConfig>;
	defaults: ModelDefaults;
}

export interface BrowserConfig {
	binary: string;
	cdp_port: number;
}

export interface SchedulerConfig {
	concurrency: string | number;
}

export interface AppConfig {
	server: ServerConfig;
	agents: AgentEntry[];
	models: ModelsConfig;
	browser: BrowserConfig;
	scheduler: SchedulerConfig;
}

function defaultConfig(): AppConfig {
	return {
		server: { port: 16677, tls: { cert: "", key: "" } },
		agents: [],
		models: { providers: {}, defaults: { model: "", fallbacks: [] } },
		browser: { binary: "", cdp_port: 9222 },
		scheduler: { concurrency: "auto" },
	};
}

export const useSettingsStore = defineStore("settings", () => {
	const config = ref<AppConfig | null>(null);
	const loading = ref(false);
	const saving = ref(false);
	const error = ref<string | null>(null);
	const { callTool } = useMCP();

	async function fetchConfig() {
		loading.value = true;
		error.value = null;
		try {
			const raw = await callTool("config_get");
			const parsed = JSON.parse(raw) as Partial<AppConfig>;
			const base = defaultConfig();
			// Merge to ensure all keys exist even if absent in stored config.
			config.value = {
				server: {
					...base.server,
					...parsed.server,
					tls: { ...base.server.tls, ...(parsed.server?.tls ?? {}) },
				},
				agents: parsed.agents ?? [],
				models: {
					providers: parsed.models?.providers ?? {},
					defaults: { ...base.models.defaults, ...parsed.models?.defaults },
				},
				browser: { ...base.browser, ...parsed.browser },
				scheduler: { ...base.scheduler, ...parsed.scheduler },
			};
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
		} finally {
			loading.value = false;
		}
	}

	async function saveConfig(updated: AppConfig): Promise<void> {
		saving.value = true;
		error.value = null;
		try {
			await callTool("config_save", { config: JSON.stringify(updated) });
			config.value = updated;
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
			throw e;
		} finally {
			saving.value = false;
		}
	}

	return { config, loading, saving, error, fetchConfig, saveConfig };
});
