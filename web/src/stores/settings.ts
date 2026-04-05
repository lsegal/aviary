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
	allowed_groups?: string;
	mention_prefixes?: string[];
	exclude_prefixes?: string[];
	respond_to_mentions?: boolean;
	mention_prefix_group_only?: boolean;
	restrict_tools?: string[];
	model?: string;
	fallbacks?: string[];
}

export interface AgentChannel {
	enabled?: boolean;
	type: string;
	token?: string;
	id?: string;
	url?: string;
	disabled_tools?: string[];
	allow_from?: AllowFromEntry[];
	show_typing?: boolean;
	reply_to_replies?: boolean;
	react_to_emoji?: boolean;
	send_read_receipts?: boolean;
	group_chat_history?: number;
	primary?: string;
	model?: string;
	fallbacks?: string[];
}

export interface AgentPermissions {
	preset?: PermissionsPreset;
	tools?: string[];
	disabled_tools?: string[];
	filesystem?: {
		allowed_paths?: string[];
	};
	exec?: {
		allowed_commands?: string[];
		shell_interpolate?: boolean;
		shell?: string;
	};
}

export interface AgentEntry {
	name: string;
	model: string;
	verbose?: boolean;
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
	type?: "prompt" | "script";
	prompt?: string;
	schedule?: string;
	start_at?: string;
	run_once?: boolean;
	watch?: string;
	target?: string;
	from_file?: boolean;
	file?: string;
}

export interface ProviderConfig {
	auth: string;
	base_uri?: string;
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
	headless?: boolean;
	reuse_tabs?: boolean;
}

export interface WebSearchConfig {
	brave_api_key?: string;
}

export interface SearchConfig {
	web: WebSearchConfig;
}

export interface SchedulerConfig {
	concurrency: string | number;
	precompute_tasks?: boolean;
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
		browser: { binary: "", cdp_port: 0, reuse_tabs: true },
		search: { web: { brave_api_key: "" } },
		scheduler: { concurrency: "", precompute_tasks: true },
		skills: {},
	};
}

function parseConfigPayload(raw: string): Partial<AppConfig> {
	const trimmed = raw.trim();
	if (!trimmed) {
		return {};
	}
	try {
		const parsed = JSON.parse(trimmed) as Partial<AppConfig> | null;
		return parsed ?? {};
	} catch (error) {
		// Treat empty/truncated responses as an empty config so the UI remains usable.
		if (error instanceof SyntaxError) {
			return {};
		}
		throw error;
	}
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
			const parsed = parseConfigPayload(raw);
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
						show_typing: ch.show_typing !== false,
						reply_to_replies: ch.reply_to_replies !== false,
						react_to_emoji: ch.react_to_emoji !== false,
						send_read_receipts: ch.send_read_receipts !== false,
						allow_from: (ch.allow_from ?? []).map((entry) => ({
							...entry,
							enabled: entry.enabled !== false,
							// Default respondToMentions to true when absent (omitempty hides false).
							respond_to_mentions: entry.respond_to_mentions !== false,
						})),
					})),
					tasks: (agent.tasks ?? []).map((task) => ({
						...task,
						enabled: task.enabled !== false,
						type: task.type === "script" ? "script" : "prompt",
						prompt: task.prompt ?? "",
					})),
				})),
				models: {
					providers: parsed.models?.providers ?? {},
					defaults: { ...base.models.defaults, ...parsed.models?.defaults },
				},
				browser: {
					...base.browser,
					...parsed.browser,
					reuse_tabs: parsed.browser?.reuse_tabs !== false,
				},
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
