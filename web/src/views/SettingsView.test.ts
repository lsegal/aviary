// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
	FakeWebSocket,
	fakes,
	makeConfig,
	mountSettings,
	resetFakes,
} from "./settingsViewHarness";

vi.mock("../composables/useMCP", () => ({
	useMCP: () => ({ callTool: fakes.callTool, listTools: fakes.listTools }),
}));
vi.mock("../composables/useAvailableModels", async () => {
	const { ref } = await import("vue");
	return {
		useAvailableModels: () => ({
			availableModelOptions: ref([]),
			credentials: fakes.credentials,
			refreshCredentials: fakes.refreshCredentials,
		}),
	};
});
vi.mock("../composables/useProviderAuth", async (importOriginal) => ({
	...(await importOriginal<object>()),
	useProviderAuth: () => fakes.providerAuth,
}));
vi.mock("../composables/useTriggerPreview", () => ({
	useTriggerPreview: () => fakes.previewTrigger,
}));
vi.mock("../composables/useAgentNav", () => ({
	useAgentNav: () => ({ agentHome: (id: string) => `/agents/${id}/chat` }),
}));
vi.mock("../composables/useAgentItemDialogs", () => ({
	useAgentItemDialogs: () => ({ open: fakes.openItemDialog }),
}));
vi.mock("../stores/settings", async (importOriginal) => ({
	...(await importOriginal<object>()),
	useSettingsStore: () => fakes.settings,
}));
vi.mock("../stores/agentFiles", async (importOriginal) => ({
	...(await importOriginal<object>()),
	useAgentFilesStore: () => fakes.agentFiles,
}));
vi.mock("../stores/auth", () => ({ useAuthStore: () => fakes.auth }));
vi.mock("../stores/chatSessions", async (importOriginal) => ({
	...(await importOriginal<object>()),
	useChatSessionsStore: () => fakes.chatSessions,
}));

beforeEach(() => {
	resetFakes();
	vi.stubGlobal("WebSocket", FakeWebSocket);
});

afterEach(() => {
	document.body.innerHTML = "";
	vi.unstubAllGlobals();
	vi.useRealTimers();
});


const agent = (name: string, extra: Record<string, unknown> = {}) => ({
	name,
	model: "anthropic/claude",
	...extra,
});

function buttonByText(
	wrapper: Awaited<ReturnType<typeof mountSettings>>["wrapper"],
	text: string,
) {
	const button = wrapper
		.findAll("button")
		.find((b) => b.text().includes(text));
	if (!button) throw new Error(`no button "${text}"`);
	return button;
}

function crumbs(m: Awaited<ReturnType<typeof mountSettings>>) {
	const layout = m.wrapper.findComponent({ name: "AppLayout" });
	return (layout.vm.$attrs.crumbs ?? []) as { label: string; to?: string }[];
}

function dialog(
	m: Awaited<ReturnType<typeof mountSettings>>,
	testId: string,
): Record<string, unknown> {
	const found = m.wrapper
		.findAllComponents({ name: "ConfirmDialog" })
		.find((d) => d.vm.$attrs["test-id"] === testId);
	if (!found) throw new Error(`no dialog ${testId}`);
	return found.vm.$attrs;
}

describe("loading and saving", () => {
	it("loads the config into the General tab", async () => {
		const m = await mountSettings(
			"/system/settings",
			makeConfig({
				server: {
					port: 8080,
					tls: { cert: "", key: "" },
					external_access: false,
					no_tls: false,
				},
				browser: { binary: "", cdp_port: 9222 },
				scheduler: { concurrency: 4 },
				agents: [agent("alpha")],
			}),
		);
		expect(fakes.settings.fetchConfig).toHaveBeenCalled();
		expect(m.ctx.activeTab).toBe("general");
		expect(m.ctx.serverPortInput).toBe("8080");
		expect(m.ctx.cdpPortInput).toBe("9222");
		expect(m.ctx.concurrencyInput).toBe("4");
		// Unset booleans hydrate as on, as the server treats them.
		expect(m.ctx.draft.server.auto_update).toBe(true);
		expect(m.ctx.draft.browser.reuse_tabs).toBe(true);
		expect(m.ctx.draft.scheduler.precompute_tasks).toBe(true);
		expect(m.ctx.sessionAgent).toBe("alpha");
		expect(m.wrapper.find('[data-testid="settings-tabs"]').exists()).toBe(true);
		expect(
			m.wrapper.findAll('[role="tab"]').map((t) => t.attributes("data-testid")),
		).toEqual([
			"settings-tab-general",
			"settings-tab-providers",
			"settings-tab-display",
			"settings-tab-notifications",
		]);
		expect(crumbs(m).map((c) => c.label)).toEqual(["System", "Settings"]);
		expect(fakes.sockets[0].url).toContain("/api/ws?token=tok");
		m.wrapper.unmount();
		expect(fakes.sockets[0].closed).toBe(true);
	});

	it("shows only Display and Notifications in a minimal session", async () => {
		fakes.auth.minimal = true;
		fakes.auth.getToken = () => null;
		const m = await mountSettings("/system/display");
		expect(
			m.wrapper.findAll('[role="tab"]').map((t) => t.text()),
		).toEqual(["Display", "Notifications"]);
		expect(fakes.sockets[0].url).not.toContain("token");
		// Display has no Save.
		expect(m.wrapper.text()).not.toContain("Save Changes");
		m.wrapper.unmount();
	});

	it("shows the load error", async () => {
		const m = await mountSettings("/system/settings");
		fakes.settings.fetchConfig.mockRejectedValueOnce(new Error("boom"));
		await buttonByText(m.wrapper, "Reload").trigger("click");
		await m.settle();
		expect(m.wrapper.text()).toContain("boom");
		fakes.settings.fetchConfig.mockRejectedValueOnce("plain");
		await buttonByText(m.wrapper, "Reload").trigger("click");
		await m.settle();
		expect(m.wrapper.text()).toContain("plain");
		m.wrapper.unmount();
	});

	it("saves the normalized draft and flashes the notice", async () => {
		const m = await mountSettings(
			"/system/settings",
			makeConfig({ agents: [agent("alpha")] }),
		);
		const input = document.createElement("input");
		input.value = "80a80";
		m.ctx.updateServerPortInput({ target: input });
		expect(input.value).toBe("8080");
		expect(m.ctx.draft.server.port).toBe(8080);
		input.value = "99999";
		m.ctx.updateCDPPortInput({ target: input });
		expect(m.ctx.draft.browser.cdp_port).toBe(0);
		input.value = "9222";
		m.ctx.updateCDPPortInput({ target: input });
		m.ctx.concurrencyInput = "3";
		m.ctx.draft.search.web.brave_api_key = "  auth:brave  ";
		m.ctx.draft.approval = { mode: "ask", chat: "" };
		vi.useFakeTimers();
		await buttonByText(m.wrapper, "Save Changes").trigger("click");
		await vi.runAllTicks();
		await Promise.resolve();
		await Promise.resolve();
		const saved = fakes.settings.saveConfig.mock.calls[0][0];
		expect(saved.server.port).toBe(8080);
		expect(saved.browser.cdp_port).toBe(9222);
		expect(saved.scheduler.concurrency).toBe(3);
		expect(saved.search.web.brave_api_key).toBe("auth:brave");
		expect(saved.approval).toEqual({ mode: "ask", chat: undefined });
		await m.wrapper.vm.$nextTick();
		expect(m.wrapper.text()).toContain("Settings saved");
		expect(m.wrapper.text()).toContain("Revert");
		vi.advanceTimersByTime(3300);
		await m.wrapper.vm.$nextTick();
		expect(m.wrapper.text()).not.toContain("Settings saved");
		vi.useRealTimers();

		// Nothing changed since: the save says so instead of writing again.
		await buttonByText(m.wrapper, "Save Changes").trigger("click");
		await m.settle();
		expect(fakes.settings.saveConfig).toHaveBeenCalledTimes(1);
		expect(m.wrapper.text()).toContain("Settings already up to date.");
		m.wrapper.unmount();
	});

	it("normalizes concurrency and shows a save error", async () => {
		const m = await mountSettings("/system/settings");
		m.ctx.concurrencyInput = "auto";
		m.ctx.draft.server.auto_update = false;
		m.ctx.draft.server.disable_token_auth = true;
		m.ctx.draft.browser.reuse_tabs = false;
		m.ctx.draft.scheduler.precompute_tasks = false;
		m.ctx.draft.projects = { repos: [] };
		fakes.settings.saveConfig.mockRejectedValueOnce(new Error("disk full"));
		await buttonByText(m.wrapper, "Save Changes").trigger("click");
		await m.settle();
		const saved = fakes.settings.saveConfig.mock.calls[0][0];
		expect(saved.scheduler.concurrency).toBe("");
		expect(saved.server.auto_update).toBe(false);
		expect(saved.server.disable_token_auth).toBe(true);
		expect(saved.browser.reuse_tabs).toBe(false);
		expect(saved.scheduler.precompute_tasks).toBe(false);
		expect(saved.projects).toEqual({ repos: [] });
		expect(m.wrapper.text()).toContain("disk full");

		m.ctx.concurrencyInput = "0";
		m.ctx.draft.projects = {};
		await buttonByText(m.wrapper, "Save Changes").trigger("click");
		await m.settle();
		const second = fakes.settings.saveConfig.mock.calls[1][0];
		expect(second.scheduler.concurrency).toBe("");
		expect(second.projects).toBeUndefined();
		m.wrapper.unmount();
	});

	it("reverts to the latest backup", async () => {
		const m = await mountSettings("/system/settings");
		m.ctx.draft.server.port = 1234;
		await buttonByText(m.wrapper, "Save Changes").trigger("click");
		await m.settle();
		await buttonByText(m.wrapper, "Revert").trigger("click");
		await m.settle();
		expect(fakes.callTool).toHaveBeenCalledWith("config_restore_latest_backup");
		expect(m.wrapper.text()).toContain("Settings reverted from latest backup.");
		expect(m.wrapper.text()).not.toContain("Revert");

		m.ctx.draft.server.port = 4321;
		await buttonByText(m.wrapper, "Save Changes").trigger("click");
		await m.settle();
		fakes.tools.config_restore_latest_backup = () => {
			throw new Error("no backup");
		};
		await buttonByText(m.wrapper, "Revert").trigger("click");
		await m.settle();
		expect(m.wrapper.text()).toContain("no backup");
		m.wrapper.unmount();
	});

	it("saves with Ctrl+S only when the draft changed", async () => {
		const m = await mountSettings("/system/settings");
		window.dispatchEvent(new KeyboardEvent("keydown", { key: "s", ctrlKey: true }));
		await m.settle();
		expect(fakes.settings.saveConfig).not.toHaveBeenCalled();
		window.dispatchEvent(new KeyboardEvent("keydown", { key: "x" }));
		m.ctx.draft.server.port = 7000;
		window.dispatchEvent(new KeyboardEvent("keydown", { key: "S", metaKey: true }));
		await m.settle();
		expect(fakes.settings.saveConfig).toHaveBeenCalledTimes(1);
		m.wrapper.unmount();
	});

	it("imports runtime agents when the config has none", async () => {
		fakes.tools.agent_list = () =>
			JSON.stringify([{ name: "rt", model: "m", fallbacks: ["f"] }, {}]);
		const m = await mountSettings("/system/settings", makeConfig());
		expect(m.ctx.draft.agents.map((a: { name: string }) => a.name)).toEqual([
			"rt",
			"",
		]);
		expect(m.ctx.draft.agents[0].fallbacks).toEqual(["f"]);
		expect(m.ctx.sessionAgent).toBe("rt");
		m.wrapper.unmount();

		fakes.tools.agent_list = () => {
			throw new Error("down");
		};
		const n = await mountSettings("/system/settings", null);
		expect(n.ctx.draft.agents).toEqual([]);
		n.wrapper.unmount();
	});

	it("follows the host OS and session frames over the socket", async () => {
		const m = await mountSettings(
			"/system/sessions",
			makeConfig({ agents: [agent("alpha")] }),
		);
		const ws = fakes.sockets[0];
		expect(m.ctx.execShellPlaceholder).toBe("/bin/sh -lc");
		for (const [goos, shell] of [
			["windows", "powershell.exe -NoProfile -Command"],
			["darwin", "/bin/zsh -lc"],
			["linux", "/bin/bash -lc"],
		]) {
			await ws.onmessage?.({ data: JSON.stringify({ goos }) });
			expect(m.ctx.execShellPlaceholder).toBe(shell);
		}
		fakes.callTool.mockClear();
		await ws.onmessage?.({ data: JSON.stringify({ type: "session_message" }) });
		expect(fakes.callTool).toHaveBeenCalledWith("session_list", {
			agent: "alpha",
		});
		await ws.onmessage?.({ data: "not json" });
		m.wrapper.unmount();
	});

	it("takes coding skills and sidebar task saves into the draft", async () => {
		const m = await mountSettings(
			"/system/settings",
			makeConfig({
				agents: [
					agent("alpha", { tasks: [{ name: "t1", prompt: "p" }] }),
					agent("beta"),
				],
			}),
		);
		fakes.settings.config = {
			...(fakes.settings.config as object),
			coding_skills: { provider: "x" },
		} as never;
		await m.settle();
		expect(m.ctx.draft.coding_skills).toEqual({ provider: "x" });
		const config = JSON.parse(JSON.stringify(fakes.settings.config));
		config.agents[0].tasks.push({ name: "t2", prompt: "q" });
		config.agents.push(agent("gamma"));
		fakes.settings.config = config;
		await m.settle();
		expect(
			m.ctx.draft.agents[0].tasks.map((t: { name: string }) => t.name),
		).toEqual(["t1", "t2"]);
		// Already saved, so no unsaved change and Ctrl+S has nothing to do.
		window.dispatchEvent(new KeyboardEvent("keydown", { key: "s", ctrlKey: true }));
		await m.settle();
		expect(fakes.settings.saveConfig).not.toHaveBeenCalled();
		m.wrapper.unmount();
	});
});

describe("providers and secrets", () => {
	it("lists configured providers and the ones left to add", async () => {
		fakes.credentials.value = [
			"anthropic:oauth",
			"openai:default",
			"brave_search_api_key",
			"custom:oauth",
		];
		const m = await mountSettings(
			"/system/providers",
			makeConfig({
				models: {
					providers: { vllm: { auth: "", base_uri: " http://gpu:8000 " } },
					defaults: { model: "", fallbacks: [] },
				},
			}),
		);
		expect(m.ctx.activeTab).toBe("providers");
		const keys = m.ctx.configuredProviders.map((p: { key: string }) => p.key);
		expect(keys).toContain("anthropic:oauth");
		expect(keys).toContain("openai:default");
		expect(keys).toContain("vllm:endpoint");
		const vllm = m.ctx.configuredProviders.find(
			(p: { key: string }) => p.key === "vllm:endpoint",
		);
		expect(vllm.baseURI).toBe("http://gpu:8000");
		const options = m.ctx.availableProviderOptions.map(
			(p: { key: string }) => p.key,
		);
		expect(options).not.toContain("anthropic:oauth");
		expect(options).toContain("anthropic:apikey");
		expect(options).toContain("vllm:endpoint");
		expect(m.ctx.extraSecrets).toEqual(["brave_search_api_key", "custom:oauth"]);
		expect(m.ctx.webSearchSecretOptions).toEqual(["brave_search_api_key"]);

		// Picking an endpoint provider fills in its base URI.
		m.ctx.providerApiKeyValue = "stale";
		m.ctx.providerAddSelection = "vllm:endpoint";
		await m.settle();
		expect(m.ctx.providerBaseURIValue).toBe("http://gpu:8000");
		expect(m.ctx.providerApiKeyValue).toBe("");
		m.ctx.providerAddSelection = "ollama:endpoint";
		await m.settle();
		expect(m.ctx.providerBaseURIValue).toBe("http://127.0.0.1:11434");
		m.ctx.providerAddSelection = "anthropic:apikey";
		await m.settle();
		expect(m.ctx.providerBaseURIValue).toBe("");
		m.wrapper.unmount();
	});

	it("stores an API key", async () => {
		const m = await mountSettings("/system/providers");
		m.ctx.providerAddSelection = "anthropic:apikey";
		await m.settle();
		await m.ctx.addProviderApiKey();
		expect(fakes.callTool).not.toHaveBeenCalledWith(
			"auth_set",
			expect.anything(),
		);
		m.ctx.providerApiKeyValue = " sk-1 ";
		await m.ctx.addProviderApiKey();
		expect(fakes.callTool).toHaveBeenCalledWith("auth_set", {
			name: "anthropic:default",
			value: "sk-1",
		});
		expect(m.ctx.providerAddSelection).toBe("");
		expect(fakes.refreshCredentials).toHaveBeenCalled();
		await m.settle();
		expect(m.wrapper.text()).toContain("anthropic API key stored.");

		fakes.tools.auth_set = () => {
			throw new Error("denied");
		};
		m.ctx.providerAddSelection = "openai:apikey";
		await m.settle();
		m.ctx.providerApiKeyValue = "k";
		await m.ctx.addProviderApiKey();
		await m.settle();
		expect(m.wrapper.text()).toContain("denied");
		m.wrapper.unmount();
	});

	it("saves and removes an endpoint provider", async () => {
		const m = await mountSettings("/system/providers");
		m.ctx.providerAddSelection = "anthropic:endpoint";
		await m.ctx.addProviderEndpoint();
		expect(fakes.settings.saveConfig).not.toHaveBeenCalled();

		m.ctx.providerAddSelection = "vllm:endpoint";
		await m.settle();
		m.ctx.providerBaseURIValue = "http://gpu:9000";
		m.ctx.providerApiKeyValue = "secret";
		await m.ctx.addProviderEndpoint();
		const saved = fakes.settings.saveConfig.mock.calls[0][0];
		expect(saved.models.providers.vllm).toEqual({
			auth: "auth:vllm:default",
			base_uri: "http://gpu:9000",
		});
		expect(fakes.callTool).toHaveBeenCalledWith("auth_set", {
			name: "vllm:default",
			value: "secret",
		});
		await m.settle();
		expect(m.wrapper.text()).toContain("endpoint settings saved.");

		// Clearing both removes the endpoint and its key, even if the delete fails.
		fakes.tools.auth_delete = () => {
			throw new Error("missing");
		};
		m.ctx.providerAddSelection = "vllm:endpoint";
		await m.settle();
		m.ctx.providerBaseURIValue = "";
		m.ctx.providerApiKeyValue = "";
		await m.ctx.addProviderEndpoint();
		expect(fakes.settings.saveConfig.mock.calls[1][0].models.providers).toEqual(
			{},
		);

		fakes.settings.saveConfig.mockRejectedValueOnce(new Error("readonly"));
		m.ctx.providerAddSelection = "ollama:endpoint";
		await m.settle();
		await m.ctx.addProviderEndpoint();
		await m.settle();
		expect(m.wrapper.text()).toContain("readonly");
		m.wrapper.unmount();
	});

	it("removes credentials and provider connections", async () => {
		fakes.credentials.value = ["vllm:default"];
		const m = await mountSettings(
			"/system/providers",
			makeConfig({
				models: {
					providers: { vllm: { auth: "", base_uri: "http://x" } },
					defaults: { model: "", fallbacks: [] },
				},
			}),
		);
		await m.ctx.deleteProviderCredential("anthropic:oauth");
		expect(fakes.callTool).toHaveBeenCalledWith("auth_delete", {
			name: "anthropic:oauth",
		});
		await m.settle();
		expect(m.wrapper.text()).toContain("Provider credential removed.");

		fakes.tools.auth_delete = () => {
			throw new Error("gone");
		};
		await m.ctx.deleteProviderCredential("x");
		await m.settle();
		expect(m.wrapper.text()).toContain("gone");

		await m.ctx.deleteProviderConnection("vllm");
		expect(fakes.settings.saveConfig.mock.calls[0][0].models.providers).toEqual(
			{},
		);
		await m.settle();
		expect(m.wrapper.text()).toContain("Provider connection removed.");
		await m.ctx.deleteProviderConnection("openai");

		fakes.settings.saveConfig.mockRejectedValueOnce(new Error("locked"));
		await m.ctx.deleteProviderConnection("vllm");
		await m.settle();
		expect(m.wrapper.text()).toContain("locked");
		m.wrapper.unmount();
	});

	it("stores and deletes plain secrets", async () => {
		const m = await mountSettings("/system/providers");
		await m.ctx.addSecret();
		m.ctx.secretName = " auth:github ";
		m.ctx.secretValue = "ghp";
		await m.ctx.addSecret();
		expect(fakes.callTool).toHaveBeenCalledWith("auth_set", {
			name: "github",
			value: "ghp",
		});
		expect(m.ctx.secretName).toBe("");
		await m.settle();
		expect(m.wrapper.text()).toContain("Secret stored: github");
		await m.ctx.deleteSecret("github");
		await m.settle();
		expect(m.wrapper.text()).toContain("Secret deleted: github");

		fakes.tools.auth_set = () => {
			throw new Error("set failed");
		};
		fakes.tools.auth_delete = () => {
			throw new Error("delete failed");
		};
		m.ctx.secretName = "x";
		await m.ctx.addSecret();
		await m.settle();
		expect(m.wrapper.text()).toContain("set failed");
		await m.ctx.deleteSecret("x");
		await m.settle();
		expect(m.wrapper.text()).toContain("delete failed");
		m.wrapper.unmount();
	});

	it("adds a web search secret from its modal", async () => {
		fakes.credentials.value = ["brave_search_api_key", "brave_search_api_key_2"];
		const m = await mountSettings("/system/settings");
		m.ctx.openWebSearchSecretModal();
		await m.settle();
		expect(m.ctx.webSearchSecretModalName).toBe("brave_search_api_key_3");
		expect(document.body.textContent).toContain("Store a Brave Search API key");
		const cancel = [...document.body.querySelectorAll("button")].find(
			(b) => b.textContent?.trim() === "Cancel",
		);
		cancel?.click();
		await m.settle();
		expect(m.ctx.webSearchSecretModalOpen).toBe(false);

		m.ctx.openWebSearchSecretModal();
		await m.ctx.saveWebSearchSecret();
		expect(fakes.callTool).not.toHaveBeenCalledWith(
			"auth_set",
			expect.anything(),
		);
		m.ctx.webSearchSecretModalValue = "BSA1";
		await m.ctx.saveWebSearchSecret();
		expect(fakes.callTool).toHaveBeenCalledWith("auth_set", {
			name: "brave_search_api_key_3",
			value: "BSA1",
		});
		expect(m.ctx.draft.search.web.brave_api_key).toBe(
			"auth:brave_search_api_key_3",
		);
		expect(m.ctx.webSearchSecretRef).toBe("auth:brave_search_api_key_3");
		expect(m.ctx.webSearchSecretModalOpen).toBe(false);

		fakes.tools.auth_set = () => {
			throw new Error("nope");
		};
		m.ctx.openWebSearchSecretModal();
		m.ctx.webSearchSecretModalValue = "BSA2";
		await m.ctx.saveWebSearchSecret();
		expect(m.ctx.webSearchSecretModalError).toBe("nope");
		expect(m.ctx.webSearchSecretModalOpen).toBe(true);
		m.wrapper.unmount();
	});

	it("adds channel token secrets", async () => {
		fakes.credentials.value = ["slack_bot_token", "slack_bot_token_2"];
		const m = await mountSettings("/system/settings");
		const slack = { type: "slack", token: "", url: "" };
		m.ctx.openChannelTokenSecretModal(slack, "url");
		expect(m.ctx.webSearchSecretModalName).toBe("slack_app_token");
		expect(m.ctx.secretModalValueLabel).toBe("App-level token");
		expect(m.ctx.secretModalValuePlaceholder).toBe("xapp-...");
		m.ctx.webSearchSecretModalValue = "xapp-1";
		await m.ctx.saveWebSearchSecret();
		expect(slack.url).toBe("auth:slack_app_token");

		m.ctx.openChannelTokenSecretModal(slack, "token");
		expect(m.ctx.webSearchSecretModalName).toBe("slack_bot_token_3");
		expect(m.ctx.secretModalDescription).toContain("Slack bot token");
		expect(m.ctx.secretModalValuePlaceholder).toBe("xoxb-...");
		m.ctx.webSearchSecretModalValue = "xoxb-1";
		await m.ctx.saveWebSearchSecret();
		expect(slack.token).toBe("auth:slack_bot_token_3");

		const discord = { type: "discord", token: "" };
		m.ctx.openChannelTokenSecretModal(discord, "token");
		expect(m.ctx.webSearchSecretModalName).toBe("discord_bot_token");
		expect(m.ctx.secretModalDescription).toContain("Discord");
		expect(m.ctx.secretModalValuePlaceholder).toBe("Discord bot token");
		m.ctx.closeWebSearchSecretModal();
		m.wrapper.unmount();
	});

	it("starts and completes provider sign-ins", async () => {
		const p = fakes.providerAuth;
		p.startAnthropic.mockResolvedValue("Open the Anthropic link");
		p.startOpenAI.mockResolvedValue({ browser_opened: true });
		p.startGemini.mockResolvedValue({ browser_opened: false });
		p.startCopilot.mockResolvedValue(undefined);
		const m = await mountSettings("/system/providers");

		await m.ctx.addProviderOAuth();
		m.ctx.providerAddSelection = "nobody:oauth";
		await m.ctx.addProviderOAuth();
		expect(p.startAnthropic).not.toHaveBeenCalled();

		m.ctx.providerAddSelection = "anthropic:oauth";
		await m.ctx.addProviderOAuth();
		expect(p.startAnthropic).toHaveBeenCalled();
		expect(m.ctx.oauthBusy).toBe(false);
		await m.settle();
		expect(m.wrapper.text()).toContain("Anthropic authorization started.");

		m.ctx.providerAddSelection = "openai:oauth";
		await m.ctx.addProviderOAuth();
		expect(p.startOpenAI).toHaveBeenCalled();
		m.ctx.providerAddSelection = "gemini:oauth";
		await m.ctx.addProviderOAuth();
		expect(p.startGemini).toHaveBeenCalled();
		m.ctx.providerAddSelection = "github-copilot:oauth";
		await m.ctx.addProviderOAuth();
		await m.settle();
		expect(m.wrapper.text()).toContain("GitHub Copilot step 2 is ready below.");

		for (const provider of ["anthropic", "openai", "google", "github-copilot"]) {
			await m.ctx.reauthorizeProvider(provider);
		}
		await m.ctx.reauthorizeProvider("unknown");
		expect(p.startAnthropic).toHaveBeenCalledTimes(2);
		expect(p.startOpenAI).toHaveBeenCalledTimes(2);
		expect(p.startGemini).toHaveBeenCalledTimes(2);
		expect(p.startCopilot).toHaveBeenCalledTimes(2);

		// The page's own login wrappers report the browser state.
		p.startOpenAI.mockResolvedValueOnce({ browser_open_error: "no browser" });
		m.ctx.providerAddSelection = "openai:oauth";
		await m.ctx.addProviderOAuth();
		await m.settle();
		expect(m.wrapper.text()).toContain("OpenAI Codex authorization started.");
		p.startGemini.mockResolvedValueOnce({ browser_opened: true });
		m.ctx.providerAddSelection = "gemini:oauth";
		await m.ctx.addProviderOAuth();

		p.completeAnthropic.mockResolvedValue("");
		await m.ctx.completeAnthropic();
		expect(p.completeAnthropic).not.toHaveBeenCalled();
		m.ctx.anthropicCode = "code";
		await m.ctx.completeAnthropic();
		expect(m.ctx.anthropicCode).toBe("");
		await m.settle();
		expect(m.wrapper.text()).toContain("Anthropic OAuth completed.");

		p.completeOpenAI.mockResolvedValue("");
		await m.ctx.completeOpenAI();
		expect(p.clearOAuthState).toHaveBeenCalled();
		await m.settle();
		expect(m.wrapper.text()).toContain("OpenAI Codex OAuth completed.");
		p.completeGemini.mockResolvedValue("Gemini done");
		await m.ctx.completeGemini();
		await m.settle();
		expect(m.wrapper.text()).toContain("Gemini done");
		p.completeCopilot.mockResolvedValue("");
		m.ctx.copilotUserCode = "ABCD";
		await m.ctx.completeCopilot();
		expect(m.ctx.copilotUserCode).toBe("");
		await m.settle();
		expect(m.wrapper.text()).toContain("GitHub Copilot login completed.");
		m.wrapper.unmount();
	});

	it("reports sign-in errors", async () => {
		const p = fakes.providerAuth;
		const fail = (msg: string) => () => Promise.reject(new Error(msg));
		p.startAnthropic.mockImplementation(fail("a-start"));
		p.startOpenAI.mockImplementation(fail("o-start"));
		p.startGemini.mockImplementation(fail("g-start"));
		p.startCopilot.mockImplementation(fail("c-start"));
		p.completeAnthropic.mockImplementation(fail("a-done"));
		p.completeOpenAI.mockImplementation(fail("o-done"));
		p.completeGemini.mockImplementation(fail("g-done"));
		p.completeCopilot.mockImplementation(fail("c-done"));
		const m = await mountSettings("/system/providers");
		const errorAfter = async (fn: () => Promise<void>) => {
			await fn();
			await m.settle();
			return m.wrapper.text();
		};
		m.ctx.providerAddSelection = "anthropic:oauth";
		expect(await errorAfter(() => m.ctx.addProviderOAuth())).toContain(
			"a-start",
		);
		expect(await errorAfter(() => m.ctx.reauthorizeProvider("openai"))).toContain(
			"o-start",
		);
		expect(await errorAfter(() => m.ctx.reauthorizeProvider("google"))).toContain(
			"g-start",
		);
		expect(await errorAfter(() => m.ctx.startCopilot())).toContain("c-start");
		m.ctx.anthropicCode = "c";
		expect(await errorAfter(() => m.ctx.completeAnthropic())).toContain(
			"a-done",
		);
		expect(await errorAfter(() => m.ctx.completeOpenAI())).toContain("o-done");
		expect(await errorAfter(() => m.ctx.completeGemini())).toContain("g-done");
		expect(await errorAfter(() => m.ctx.completeCopilot())).toContain("c-done");
		m.wrapper.unmount();
	});
});

describe("sessions", () => {
	it("lists, creates, stops, removes and clears sessions", async () => {
		const rows = [
			{ id: "s1", name: "main", updated_at: "", deletable: false },
			{ id: "s2", name: "other", updated_at: "", deletable: true },
		];
		fakes.tools.session_list = () => JSON.stringify(rows);
		const m = await mountSettings(
			"/system/sessions",
			makeConfig({ agents: [agent("alpha"), agent("beta")] }),
		);
		expect(m.ctx.activeTab).toBe("sessions");
		expect(m.ctx.sessionAgent).toBe("alpha");
		expect(m.ctx.sessions).toHaveLength(2);
		expect(crumbs(m).map((c) => c.label)).toEqual(["System", "Sessions"]);

		await m.ctx.createSession();
		expect(fakes.callTool).toHaveBeenCalledWith("session_create", {
			agent: "alpha",
		});
		await m.ctx.stopSession("s2");
		expect(fakes.callTool).toHaveBeenCalledWith("session_stop", {
			agent: "alpha",
			session_id: "s2",
		});

		m.ctx.askRemoveSession(rows[1]);
		expect(m.ctx.removeKind).toBe("remove");
		await m.settle();
		await (dialog(m, "remove-session-dialog").action as () => Promise<void>)();
		expect(fakes.callTool).toHaveBeenCalledWith("session_remove", {
			agent: "alpha",
			session_id: "s2",
		});
		m.ctx.askRemoveSession(rows[0]);
		expect(m.ctx.removeKind).toBe("clear");
		await m.settle();
		expect(dialog(m, "remove-session-dialog").title).toBe("Clear Main Chat?");
		await (dialog(m, "remove-session-dialog").action as () => Promise<void>)();
		expect(fakes.chatSessions.clear).toHaveBeenCalledWith("alpha", "s1");
		m.ctx.removeTarget = null;
		await (dialog(m, "remove-session-dialog").action as () => Promise<void>)();

		m.ctx.sessionAgent = "beta";
		await m.settle();
		expect(fakes.callTool).toHaveBeenCalledWith("session_list", {
			agent: "beta",
		});
		m.ctx.sessionAgent = "";
		await m.settle();
		expect(m.ctx.sessions).toEqual([]);
		await m.ctx.loadSessions();
		await m.ctx.createSession();
		m.wrapper.unmount();
	});

	it("shows session errors", async () => {
		fakes.tools.session_list = () => {
			throw new Error("list failed");
		};
		fakes.tools.session_create = () => {
			throw new Error("create failed");
		};
		fakes.tools.session_stop = () => {
			throw new Error("stop failed");
		};
		const m = await mountSettings(
			"/system/sessions",
			makeConfig({ agents: [agent("alpha")] }),
		);
		expect(m.wrapper.text()).toContain("list failed");
		expect(m.ctx.sessions).toEqual([]);
		await m.ctx.createSession();
		await m.settle();
		expect(m.wrapper.text()).toContain("create failed");
		await m.ctx.stopSession("s");
		await m.settle();
		expect(m.wrapper.text()).toContain("stop failed");
		m.wrapper.unmount();
	});

	it("loads sessions when the route moves to Sessions", async () => {
		const m = await mountSettings(
			"/system/settings",
			makeConfig({ agents: [agent("alpha")] }),
		);
		fakes.callTool.mockClear();
		await m.router.push("/system/sessions");
		await m.settle();
		expect(fakes.callTool).toHaveBeenCalledWith("session_list", {
			agent: "alpha",
		});
		m.wrapper.unmount();
	});

	it("picks the first agent when Sessions opens without one", async () => {
		fakes.tools.agent_list = () => "[]";
		const m = await mountSettings("/system/settings", makeConfig());
		m.ctx.draft.agents.push(agent("late"));
		await m.router.push("/system/sessions");
		await m.settle();
		expect(m.ctx.sessionAgent).toBe("late");
		m.wrapper.unmount();
	});
});

describe("skills", () => {
	it("loads installed skills and edits their settings", async () => {
		fakes.tools.skills_list = () =>
			JSON.stringify([
				{
					name: "gh",
					settings_schema: {
						properties: {
							repo_list: { type: "array", placeholder: "a, b" },
							token: { type: "string", title: "Token" },
						},
					},
				},
			]);
		const m = await mountSettings(
			"/system/skills",
			makeConfig({
				skills: {
					keep: { enabled: true },
					drop: { settings: { blank: " ", list: [" ", 3], n: 5, z: null } },
				} as never,
			}),
		);
		expect(m.ctx.activeTab).toBe("skills");
		const [skill] = m.ctx.installedSkills;
		const entries = m.ctx.skillSettingEntries(skill);
		expect(entries.map(([k]: [string]) => k)).toEqual(["repo_list", "token"]);
		expect(m.ctx.skillSettingInputKind(entries[0][1])).toBe("array");
		expect(m.ctx.skillSettingInputKind(entries[1][1])).toBe("string");
		expect(m.ctx.skillSettingLabel("repo_list", entries[0][1])).toBe(
			"repo list",
		);
		expect(m.ctx.skillSettingLabel("token", entries[1][1])).toBe("Token");
		expect(m.ctx.skillSettingPlaceholder(entries[0][1])).toBe("a, b");
		expect(m.ctx.skillSettingPlaceholder(entries[1][1])).toBe("");
		expect(m.ctx.skillSettingEntries({})).toEqual([]);

		const input = document.createElement("input");
		input.value = " ghp ";
		m.ctx.setSkillStringSetting("gh", "token", { target: input });
		expect(m.ctx.skillStringSetting("gh", "token")).toBe("ghp");
		input.value = "a, , b";
		m.ctx.setSkillArraySetting("gh", "repo_list", { target: input });
		expect(m.ctx.skillArraySetting("gh", "repo_list")).toBe("a, b");
		expect(m.ctx.skillStringSetting("gh", "repo_list")).toBe("");
		expect(m.ctx.skillArraySetting("gh", "token")).toBe("");

		m.ctx.draft.skills.bare = {};
		expect(m.ctx.skillConfig("bare").settings).toEqual({});

		await buttonByText(m.wrapper, "Save Changes").trigger("click");
		await m.settle();
		const saved = fakes.settings.saveConfig.mock.calls[0][0];
		expect(saved.skills).toEqual({
			drop: { enabled: false, settings: { n: 5 } },
			gh: {
				enabled: false,
				settings: { repo_list: ["a", "b"], token: "ghp" },
			},
			keep: { enabled: true, settings: {} },
		});

		input.value = "";
		m.ctx.setSkillStringSetting("gh", "token", { target: input });
		m.ctx.setSkillArraySetting("gh", "repo_list", { target: input });
		expect(m.ctx.skillConfig("gh").settings).toEqual({});
		m.wrapper.unmount();
	});

	it("falls back to no skills when listing fails", async () => {
		fakes.tools.skills_list = () => {
			throw new Error("x");
		};
		const m = await mountSettings("/system/settings");
		expect(m.ctx.installedSkills).toEqual([]);
		expect(m.ctx.skillsLoading).toBe(false);
		// Opening Skills with none loaded asks again.
		fakes.tools.skills_list = () => JSON.stringify([{ name: "late" }]);
		await m.router.push("/system/skills");
		await m.settle();
		expect(m.ctx.installedSkills).toHaveLength(1);
		m.wrapper.unmount();
	});
});
