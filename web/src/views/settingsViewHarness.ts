// Test harness for SettingsView (#1606): mounts the view on an in-memory
// router with its stores and MCP calls replaced by fakes, and hands tests the
// context the view provides to its tabs.
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { vi } from "vitest";
import { defineComponent, h, inject, reactive, ref } from "vue";
import { createMemoryHistory, createRouter, type Router } from "vue-router";
import { settingsViewContextKey } from "../components/settings/context";
import type { AppConfig } from "../stores/settings";

type ToolHandler = (args?: Record<string, unknown>) => unknown;

/** Shared fakes; vi.mock factories in the test file read these. */
export const fakes = {
	tools: {} as Record<string, ToolHandler>,
	callTool: vi.fn(),
	listTools: vi.fn(),
	credentials: ref<string[]>([]),
	refreshCredentials: vi.fn(),
	openItemDialog: vi.fn(),
	previewTrigger: vi.fn(),
	settings: reactive({
		config: null as AppConfig | null,
		fetchConfig: vi.fn(),
		saveConfig: vi.fn(),
	}),
	agentFiles: reactive({
		lists: {} as Record<string, string[]>,
		load: vi.fn(),
		files: (agent: string): string[] => [],
		syncTemplates: vi.fn(),
	}),
	auth: reactive({ minimal: false, getToken: () => "tok" as string | null }),
	chatSessions: { clear: vi.fn() },
	providerAuth: {
		oauthBusy: ref(false),
		anthropicUrl: ref(""),
		anthropicCode: ref(""),
		openAIUrl: ref(""),
		openAICallbackUrl: ref(""),
		openAIRemainingSeconds: ref<number | null>(null),
		openAITimedOut: ref(false),
		geminiUrl: ref(""),
		geminiCallbackUrl: ref(""),
		geminiRemainingSeconds: ref<number | null>(null),
		geminiTimedOut: ref(false),
		copilotUserCode: ref(""),
		copilotVerifyUrl: ref(""),
		clearOAuthState: vi.fn(),
		startAnthropic: vi.fn(),
		completeAnthropic: vi.fn(),
		startOpenAI: vi.fn(),
		completeOpenAI: vi.fn(),
		startGemini: vi.fn(),
		completeGemini: vi.fn(),
		startCopilot: vi.fn(),
		completeCopilot: vi.fn(),
	},
	sockets: [] as FakeWebSocket[],
};

export class FakeWebSocket {
	onmessage: ((e: { data: string }) => unknown) | null = null;
	onclose: (() => void) | null = null;
	closed = false;
	constructor(public url: string) {
		fakes.sockets.push(this);
	}
	close() {
		this.closed = true;
		this.onclose?.();
	}
}

/** Resets every fake to its default behavior. */
export function resetFakes() {
	fakes.tools = {
		skills_list: () => "[]",
		agent_list: () => "[]",
		session_list: () => "[]",
	};
	fakes.callTool.mockReset();
	fakes.callTool.mockImplementation(
		async (name: string, args?: Record<string, unknown>) => {
			const handler = fakes.tools[name];
			if (!handler) return "";
			return handler(args);
		},
	);
	fakes.listTools.mockReset();
	fakes.listTools.mockResolvedValue([]);
	fakes.credentials.value = [];
	fakes.refreshCredentials.mockReset();
	fakes.refreshCredentials.mockResolvedValue(undefined);
	fakes.openItemDialog.mockReset();
	fakes.previewTrigger.mockReset();
	fakes.settings.config = null;
	fakes.settings.fetchConfig.mockReset();
	fakes.settings.saveConfig.mockReset();
	fakes.settings.saveConfig.mockImplementation(async (cfg: AppConfig) => {
		fakes.settings.config = JSON.parse(JSON.stringify(cfg));
	});
	fakes.agentFiles.lists = {};
	fakes.agentFiles.files = (agent: string) => fakes.agentFiles.lists[agent] ?? [];
	fakes.agentFiles.load.mockReset();
	fakes.agentFiles.load.mockImplementation(
		async (agent: string) => fakes.agentFiles.lists[agent] ?? [],
	);
	fakes.agentFiles.syncTemplates.mockReset();
	fakes.auth.minimal = false;
	fakes.auth.getToken = () => "tok";
	fakes.chatSessions.clear.mockReset();
	const p = fakes.providerAuth;
	for (const r of [
		p.anthropicUrl,
		p.anthropicCode,
		p.openAIUrl,
		p.openAICallbackUrl,
		p.geminiUrl,
		p.geminiCallbackUrl,
		p.copilotUserCode,
		p.copilotVerifyUrl,
	])
		r.value = "";
	p.oauthBusy.value = false;
	for (const fn of [
		p.clearOAuthState,
		p.startAnthropic,
		p.completeAnthropic,
		p.startOpenAI,
		p.completeOpenAI,
		p.startGemini,
		p.completeGemini,
		p.startCopilot,
		p.completeCopilot,
	])
		fn.mockReset();
	fakes.sockets = [];
}

/** A full config with the given overrides, as the server would return it. */
export function makeConfig(overrides: Partial<AppConfig> = {}): AppConfig {
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
		...overrides,
	} as AppConfig;
}

const Blank = defineComponent({ render: () => null });

function makeRouter(): Router {
	const settings = { requiresAuth: true, view: "settings" };
	return createRouter({
		history: createMemoryHistory(),
		routes: [
			{
				path: "/agents/:agent?/:subtab(settings|files|tasks)?/:section(permissions)?",
				component: Blank,
				meta: settings,
			},
			{
				path: "/agents/:agent/channels/:channel/settings",
				component: Blank,
				meta: settings,
			},
			{ path: "/agents/:agent/:rest(.*)*", component: Blank },
			{ path: "/projects/settings", component: Blank, meta: settings },
			{
				path: "/system/:tab(settings|providers|display|notifications|sessions|skills)",
				component: Blank,
				meta: settings,
			},
			{ path: "/:rest(.*)*", component: Blank },
		],
	});
}

// biome-ignore lint/suspicious/noExplicitAny: the view's context is untyped
export type SettingsCtx = any;

export interface Mounted {
	wrapper: VueWrapper;
	ctx: SettingsCtx;
	router: Router;
	settle: () => Promise<void>;
}

/** Mounts SettingsView at path once the server holds config. */
export async function mountSettings(
	path: string,
	config: AppConfig | null = makeConfig(),
): Promise<Mounted> {
	fakes.settings.fetchConfig.mockImplementation(async () => {
		fakes.settings.config = config ? JSON.parse(JSON.stringify(config)) : null;
	});
	const router = makeRouter();
	await router.push(path);
	await router.isReady();
	let ctx: SettingsCtx;
	const CtxProbe = defineComponent({
		setup() {
			ctx = inject(settingsViewContextKey);
			return () => h("div", { "data-testid": "ctx-probe" });
		},
	});
	const slotStub = (name: string) =>
		defineComponent({
			name,
			setup(_, { slots }) {
				return () =>
					h("div", { "data-stub": name }, [
						slots["header-status"]?.(),
						slots.header?.(),
						slots["header-actions"]?.(),
						slots.default?.(),
					]);
			},
		});
	const { default: SettingsView } = await import("./SettingsView.vue");
	const wrapper = mount(SettingsView, {
		attachTo: document.body,
		global: {
			plugins: [router],
			stubs: {
				AppLayout: slotStub("AppLayout"),
				SettingsGeneralTab: CtxProbe,
				SettingsAgentsTab: true,
				SettingsSkillsTab: true,
				SettingsSessionsTab: true,
				SettingsProvidersTab: true,
				SettingsProjectsTab: true,
				SettingsDisplayTab: true,
				SettingsNotificationsTab: true,
				AgentFileActions: true,
				SystemSettingsLink: true,
				DeleteDataOption: true,
				Skeleton: true,
				ConfirmDialog: slotStub("ConfirmDialog"),
			},
		},
	});
	const settle = async () => {
		for (let i = 0; i < 4; i++) {
			await flushPromises();
		}
	};
	await settle();
	return { wrapper, ctx, router, settle };
}
