import { defineStore } from "pinia";
import { ref } from "vue";
import { useMCP } from "../composables/useMCP";
import type { PermissionsPreset } from "../lib/toolPermissions";

export interface TLSConfig {
	cert: string;
	key: string;
}

export interface ServerConfig {
	port: number;
	tls: TLSConfig;
	external_access: boolean;
	no_tls: boolean;
}

export interface AllowFromEntry {
	enabled?: boolean;
	from: string;
	allowedGroups?: string;
	mentionPrefixes?: string[];
	excludePrefixes?: string[];
	respondToMentions?: boolean;
	mentionPrefixGroupOnly?: boolean;
	restrictTools?: string[];
	model?: string;
	fallbacks?: string[];
}

export interface AgentChannel {
	enabled?: boolean;
	type: string;
	token?: string;
	id?: string;
	url?: string;
	disabledTools?: string[];
	allowFrom?: AllowFromEntry[];
	showTyping?: boolean;
	replyToReplies?: boolean;
	reactToEmoji?: boolean;
	sendReadReceipts?: boolean;
	group_chat_history?: number;
	model?: string;
	fallbacks?: string[];
}

export interface AgentPermissions {
	preset?: PermissionsPreset;
	tools?: string[];
	disabledTools?: string[];
	filesystem?: {
		allowedPaths?: string[];
	};
	exec?: {
		allowedCommands?: string[];
		shellInterpolate?: boolean;
		shell?: string;
	};
}

export interface AgentEntry {
	name: string;
	model: string;
	working_dir?: string;
	memory?: string;
	rules?: string;
	fallbacks: string[];
	permissions?: AgentPermissions;
	channels: AgentChannel[];
	tasks: AgentTask[];
}

export interface AgentTask {
	enabled?: boolean;
	name: string;
	prompt: string;
	schedule?: string;
	start_at?: string;
	run_once?: boolean;
	watch?: string;
	target?: string;
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

export interface WebSearchConfig {
	brave_api_key?: string;
}

export interface SearchConfig {
	web: WebSearchConfig;
}

export interface SchedulerConfig {
	concurrency: string | number;
}

export interface SkillConfig {
	enabled?: boolean;
	settings?: Record<string, unknown>;
}

export interface AppConfig {
	server: ServerConfig;
	agents: AgentEntry[];
	models: ModelsConfig;
	browser: BrowserConfig;
	search: SearchConfig;
	scheduler: SchedulerConfig;
	skills: Record<string, SkillConfig>;
}

function defaultConfig(): AppConfig {
	return {
		server: {
			port: 0,
			tls: { cert: "", key: "" },
			external_access: false,
			no_tls: false,
		},
		agents: [],
		models: { providers: {}, defaults: { model: "", fallbacks: [] } },
		browser: { binary: "", cdp_port: 0 },
		search: { web: { brave_api_key: "" } },
		scheduler: { concurrency: "" },
		skills: {},
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
				agents: (parsed.agents ?? []).map((agent) => ({
					...agent,
					channels: (agent.channels ?? []).map((ch) => ({
						...ch,
						enabled: ch.enabled !== false,
						// Default these to true when absent.
						showTyping: ch.showTyping !== false,
						replyToReplies: ch.replyToReplies !== false,
						reactToEmoji: ch.reactToEmoji !== false,
						sendReadReceipts: ch.sendReadReceipts !== false,
						allowFrom: (ch.allowFrom ?? []).map((entry) => ({
							...entry,
							enabled: entry.enabled !== false,
							// Default respondToMentions to true when absent (omitempty hides false).
							respondToMentions: entry.respondToMentions !== false,
						})),
					})),
					tasks: (agent.tasks ?? []).map((task) => ({
						...task,
						enabled: task.enabled !== false,
					})),
				})),
				models: {
					providers: parsed.models?.providers ?? {},
					defaults: { ...base.models.defaults, ...parsed.models?.defaults },
				},
				browser: { ...base.browser, ...parsed.browser },
				search: {
					web: { ...base.search.web, ...(parsed.search?.web ?? {}) },
				},
				scheduler: { ...base.scheduler, ...parsed.scheduler },
				skills: parsed.skills ?? {},
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
