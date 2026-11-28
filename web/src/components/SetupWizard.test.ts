// @vitest-environment happy-dom
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import {
	afterEach,
	beforeEach,
	describe,
	expect,
	it,
	type Mock,
	vi,
} from "vitest";
import { defineComponent, h, type Ref } from "vue";
import SetupWizard from "./SetupWizard.vue";

// #1609: walk every onboarding wizard step with stubbed MCP calls, provider
// auth, available models, and settings store.

// biome-ignore lint/suspicious/noExplicitAny: mocks stand in for functions of any shape.
type AnyMock = Mock<(...args: any[]) => any>;

type ToolHandler = (args?: Record<string, unknown>) => unknown;

const state = vi.hoisted(() => ({
	tools: {} as Record<string, ToolHandler>,
	callTool: null as unknown as AnyMock,
	auth: {} as Record<string, Ref<unknown> | AnyMock>,
	availableModelOptions: null as unknown as Ref<string[]>,
	refreshCredentials: null as unknown as AnyMock,
	settings: {
		config: null as unknown,
		fetchConfig: null as unknown as AnyMock,
		saveConfig: null as unknown as AnyMock,
	},
}));

vi.mock("../composables/useMCP", () => {
	state.callTool = vi.fn(
		async (name: string, args?: Record<string, unknown>) => {
			const handler = state.tools[name];
			const result = handler ? await handler(args) : "[]";
			return typeof result === "string" ? result : JSON.stringify(result);
		},
	);
	return { useMCP: () => ({ callTool: state.callTool }) };
});

vi.mock("../composables/useAvailableModels", async () => {
	const { ref } = await import("vue");
	state.availableModelOptions = ref<string[]>([]);
	state.refreshCredentials = vi.fn(async () => {});
	return {
		useAvailableModels: () => ({
			availableModelOptions: state.availableModelOptions,
			refreshCredentials: state.refreshCredentials,
		}),
	};
});

vi.mock("../composables/useProviderAuth", async (importOriginal) => {
	const actual =
		await importOriginal<typeof import("../composables/useProviderAuth")>();
	const { ref } = await import("vue");
	const auth = {
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
	};
	state.auth = auth;
	return { ...actual, useProviderAuth: () => auth };
});

vi.mock("../stores/settings", () => {
	const store = {
		get config() {
			return state.settings.config;
		},
		fetchConfig: (...args: unknown[]) => state.settings.fetchConfig(...args),
		saveConfig: (...args: unknown[]) => state.settings.saveConfig(...args),
	};
	return { useSettingsStore: () => store };
});

const ModelSelectorStub = defineComponent({
	name: "ModelSelector",
	props: {
		modelValue: { type: [String, Array], default: undefined },
		options: { type: Array, default: () => [] },
		placeholder: { type: String, default: "" },
		multiple: { type: Boolean, default: false },
	},
	emits: ["update:modelValue"],
	setup(props) {
		return () =>
			h("div", {
				class: props.multiple ? "fallback-selector" : "model-selector",
				"data-value": JSON.stringify(props.modelValue),
				"data-options": JSON.stringify(props.options),
			});
	},
});

const RouterLinkStub = defineComponent({
	name: "RouterLink",
	props: { to: { type: String, required: true } },
	setup(props, { slots }) {
		return () => h("a", { href: props.to }, slots.default?.());
	},
});

function authRef<T>(name: string) {
	return state.auth[name] as Ref<T>;
}

function authFn(name: string) {
	return state.auth[name] as AnyMock;
}

function baseConfig() {
	return {
		server: {
			port: 16677,
			tls: { cert: "", key: "" },
			external_access: false,
			no_tls: false,
		},
		agents: [
			{
				name: "existing",
				model: "x/y",
				memory: "",
				fallbacks: [],
				channels: [],
				tasks: [],
			},
		],
		models: { providers: {}, defaults: { model: "", fallbacks: [] } },
		browser: { binary: "", cdp_port: 0, reuse_tabs: true },
		search: { web: { brave_api_key: "" } },
		scheduler: { concurrency: "auto" },
		skills: {},
	} as Record<string, unknown> & {
		agents: Array<Record<string, unknown>>;
		models: { providers: Record<string, Record<string, unknown>> };
	};
}

let wrapper: VueWrapper | null = null;

async function mountWizard() {
	wrapper = mount(SetupWizard, {
		attachTo: document.body,
		global: {
			stubs: { ModelSelector: ModelSelectorStub, RouterLink: RouterLinkStub },
		},
	});
	await flushPromises();
	return wrapper;
}

function buttonText(b: { text(): string }) {
	return b.text().replace(/\s+/g, " ").trim();
}

function button(w: VueWrapper, text: string) {
	const buttons = w.findAll("button");
	const found =
		buttons.findLast((b) => buttonText(b) === text) ??
		buttons.find((b) => buttonText(b).includes(text));
	if (!found) throw new Error(`no button containing "${text}"`);
	return found;
}

function hasButton(w: VueWrapper, text: string) {
	return w
		.findAll("button")
		.some((b) => b.text().replace(/\s+/g, " ").trim().includes(text));
}

function providerButton(w: VueWrapper, label: string) {
	const found = w
		.findAll(".grid > button")
		.find((b) => b.get("div > p.font-semibold").text() === label);
	if (!found) throw new Error(`no provider "${label}"`);
	return found;
}

function selectorValue(w: VueWrapper, cls = ".model-selector") {
	return JSON.parse(w.get(cls).attributes("data-value") ?? "null");
}

function selectorOptions(w: VueWrapper, cls = ".model-selector") {
	return JSON.parse(w.get(cls).attributes("data-options") ?? "[]");
}

async function choose(w: VueWrapper, label: string) {
	await providerButton(w, label).trigger("click");
	await flushPromises();
	await button(w, "Continue").trigger("click");
	await flushPromises();
}

beforeEach(() => {
	state.tools = {};
	state.callTool.mockClear();
	state.availableModelOptions.value = [];
	state.refreshCredentials.mockClear();
	state.settings.config = baseConfig();
	state.settings.fetchConfig = vi.fn(async () => {});
	state.settings.saveConfig = vi.fn(async (next: unknown) => {
		state.settings.config = next;
	});
	for (const [key, value] of Object.entries(state.auth)) {
		if (typeof value === "function") {
			(value as AnyMock).mockReset();
		} else {
			(value as Ref<unknown>).value = key.endsWith("Seconds")
				? null
				: key.endsWith("TimedOut")
					? false
					: "";
		}
	}
});

afterEach(() => {
	wrapper?.unmount();
	wrapper = null;
	vi.useRealTimers();
	vi.unstubAllGlobals();
});

describe("provider step", () => {
	it("lists onboarding providers and requires a selection to continue", async () => {
		const w = await mountWizard();
		expect(w.text()).toContain("Welcome to Aviary");
		const labels = w
			.findAll(".grid > button > div > p.font-semibold")
			.map((p) => p.text());
		expect(labels).toEqual([
			"Anthropic",
			"OpenAI",
			"Google (Gemini)",
			"Meta (Muse)",
			"vLLM",
			"Ollama",
			"GitHub Copilot",
		]);
		expect(w.text()).toContain("Supports Pro/Max Subscription plans!");
		expect(w.text()).toContain("GPT models via API key or ChatGPT sign-in");
		expect(button(w, "Continue").attributes("disabled")).toBeDefined();

		await providerButton(w, "Meta (Muse)").trigger("click");
		expect(providerButton(w, "Meta (Muse)").classes()).toContain(
			"border-blue-500",
		);
		expect(button(w, "Continue").attributes("disabled")).toBeUndefined();
		expect(state.callTool).toHaveBeenCalledWith("auth_list");
		expect(state.refreshCredentials).toHaveBeenCalled();
	});

	it("emits skip", async () => {
		const w = await mountWizard();
		await button(w, "Skip for now").trigger("click");
		expect(w.emitted("skip")).toHaveLength(1);
	});

	it("marks stored credentials and jumps straight to the agent step", async () => {
		state.tools.auth_list = () => ["anthropic:oauth", "meta:default"];
		const w = await mountWizard();
		const badges = w
			.findAll("span.rounded-full.bg-green-100")
			.map((s) => s.text());
		expect(badges).toEqual(["signed in", "key set"]);

		await providerButton(w, "Anthropic").trigger("click");
		await flushPromises();
		expect(w.text()).toContain("Create your first agent");
		expect(selectorValue(w)).toBe("anthropic/claude-sonnet-5-5");
		// Completed steps show a check mark.
		expect(w.findAll(".rounded-full svg")).toHaveLength(2);
	});

	it("uses the API key model for a stored API key", async () => {
		state.tools.auth_list = () => ["openai:default"];
		const w = await mountWizard();
		await providerButton(w, "OpenAI").trigger("click");
		await flushPromises();
		expect(w.text()).toContain("Create your first agent");
		expect(selectorValue(w)).toBe("openai/gpt-6.2-sol");
	});

	it("survives a failed auth_list", async () => {
		state.tools.auth_list = () => {
			throw new Error("offline");
		};
		const w = await mountWizard();
		expect(w.findAll("span.rounded-full.bg-green-100")).toHaveLength(0);
		await choose(w, "Meta (Muse)");
		expect(w.text()).toContain("Connect Meta (Muse)");
	});
});

describe("API key credentials", () => {
	it("saves a key, creates an agent, and links to it", async () => {
		state.availableModelOptions.value = [
			"meta/muse-spark-1.3",
			"meta/muse-lite",
			"anthropic/claude",
		];
		const w = await mountWizard();
		await choose(w, "Meta (Muse)");
		expect(w.text()).toContain("Connect Meta (Muse)");
		expect(w.text()).toContain("Find your key at dev.meta.ai -> API keys.");
		expect(hasButton(w, "Sign in with")).toBe(false);
		const save = button(w, "Continue");
		expect(save.attributes("disabled")).toBeDefined();

		const key = w.get('input[type="password"]');
		expect(key.attributes("placeholder")).toBe("LLM_...");
		await key.setValue("  secret  ");
		expect(save.attributes("disabled")).toBeUndefined();
		await key.trigger("keyup.enter");
		await flushPromises();

		expect(state.callTool).toHaveBeenCalledWith("auth_set", {
			name: "meta:default",
			value: "secret",
		});
		expect(w.text()).toContain("Create your first agent");
		expect(selectorValue(w)).toBe("meta/muse-spark-1.3");
		expect(selectorOptions(w)).toEqual([
			"meta/muse-spark-1.3",
			"meta/muse-lite",
		]);
		expect(selectorOptions(w, ".fallback-selector")).toEqual([
			"meta/muse-lite",
		]);

		const fallback =
			w.getComponent<typeof ModelSelectorStub>(".fallback-selector");
		fallback.vm.$emit("update:modelValue", [
			"meta/muse-lite",
			"meta/muse-spark-1.3",
		]);
		await flushPromises();
		// Choosing a fallback as the primary model drops it from the fallbacks.
		const primary = w.getComponent<typeof ModelSelectorStub>(".model-selector");
		primary.vm.$emit("update:modelValue", "meta/muse-lite");
		await flushPromises();
		expect(selectorValue(w, ".fallback-selector")).toEqual([
			"meta/muse-spark-1.3",
		]);

		await w.get('input[placeholder="default"]').setValue(" my-agent ");
		await button(w, "Create agent").trigger("click");
		await flushPromises();

		const saved = state.settings.saveConfig.mock.calls.at(
			-1,
		)?.[0] as ReturnType<typeof baseConfig>;
		expect(saved.agents.at(-1)).toEqual({
			name: "my-agent",
			model: "meta/muse-lite",
			memory: "",
			fallbacks: ["meta/muse-spark-1.3"],
			channels: [],
			tasks: [],
		});
		expect(w.text()).toContain("You're all set!");
		expect(w.text()).toContain("my-agent");
		expect(w.findAll("a").map((a) => a.attributes("href"))).toEqual([
			"/agents/my-agent/settings",
			"/agents/my-agent/chat",
		]);
	});

	it("falls back to the provider's default models without a catalog", async () => {
		state.availableModelOptions.value = ["anthropic/claude"];
		const w = await mountWizard();
		await choose(w, "Meta (Muse)");
		await w.get('input[type="password"]').setValue("k");
		await button(w, "Continue").trigger("click");
		await flushPromises();
		expect(selectorOptions(w)).toEqual(["meta/muse-spark-1.3"]);
	});

	it("shows a credential save error", async () => {
		state.tools.auth_set = () => {
			throw new Error("vault locked");
		};
		const w = await mountWizard();
		await choose(w, "Meta (Muse)");
		await w.get('input[type="password"]').setValue("k");
		await button(w, "Continue").trigger("click");
		await flushPromises();
		expect(w.text()).toContain("vault locked");
		expect(w.text()).toContain("Connect Meta (Muse)");
		expect(button(w, "Continue").attributes("disabled")).toBeUndefined();
	});

	it("stringifies non-Error failures", async () => {
		state.settings.fetchConfig = vi.fn(() => Promise.reject("nope"));
		const w = await mountWizard();
		await choose(w, "Meta (Muse)");
		await w.get('input[type="password"]').setValue("k");
		await button(w, "Continue").trigger("click");
		await flushPromises();
		expect(w.text()).toContain("nope");
	});

	it("shows Saving while the key is stored", async () => {
		let release!: () => void;
		state.tools.auth_set = () =>
			new Promise<string>((resolve) => {
				release = () => resolve("ok");
			});
		const w = await mountWizard();
		await choose(w, "Meta (Muse)");
		await w.get('input[type="password"]').setValue("k");
		await button(w, "Continue").trigger("click");
		await flushPromises();
		expect(button(w, "Saving...").attributes("disabled")).toBeDefined();
		release();
		await flushPromises();
		expect(w.text()).toContain("Create your first agent");
	});

	it("saves an API key for a sign-in provider via the API key tab", async () => {
		const w = await mountWizard();
		await choose(w, "Anthropic");
		await button(w, "API key").trigger("click");
		await w.get('input[type="password"]').setValue("sk-ant-1");
		await button(w, "Continue").trigger("click");
		await flushPromises();
		expect(state.callTool).toHaveBeenCalledWith("auth_set", {
			name: "anthropic:default",
			value: "sk-ant-1",
		});
	});

	it("navigates back between steps", async () => {
		const w = await mountWizard();
		await choose(w, "Meta (Muse)");
		await w.get('input[type="password"]').setValue("k");
		await button(w, "Continue").trigger("click");
		await flushPromises();
		await button(w, "<- Back").trigger("click");
		expect(w.text()).toContain("Connect Meta (Muse)");
		await button(w, "<- Back").trigger("click");
		expect(w.text()).toContain("Welcome to Aviary");
	});
});

describe("agent creation", () => {
	async function toAgentStep() {
		state.tools.auth_list = () => ["meta:default"];
		const w = await mountWizard();
		await providerButton(w, "Meta (Muse)").trigger("click");
		await flushPromises();
		return w;
	}

	it("requires a name and model", async () => {
		const w = await toAgentStep();
		const create = button(w, "Create agent");
		expect(create.attributes("disabled")).toBeUndefined();
		await w.get('input[placeholder="default"]').setValue("  ");
		expect(create.attributes("disabled")).toBeDefined();
	});

	it("does not duplicate an existing agent", async () => {
		const w = await toAgentStep();
		await w.get('input[placeholder="default"]').setValue("existing");
		await button(w, "Create agent").trigger("click");
		await flushPromises();
		const saved = state.settings.saveConfig.mock.calls[0][0] as ReturnType<
			typeof baseConfig
		>;
		expect(saved.agents.map((a) => a.name)).toEqual(["existing"]);
		expect(w.text()).toContain("You're all set!");
	});

	it("starts from an empty config when none is loaded", async () => {
		const w = await toAgentStep();
		state.settings.config = null;
		await button(w, "Create agent").trigger("click");
		await flushPromises();
		const saved = state.settings.saveConfig.mock.calls[0][0] as ReturnType<
			typeof baseConfig
		>;
		expect(saved.agents).toEqual([
			{
				name: "default",
				model: "meta/muse-spark-1.3",
				memory: "",
				fallbacks: [],
				channels: [],
				tasks: [],
			},
		]);
		expect(saved.server).toEqual({
			port: 0,
			tls: { cert: "", key: "" },
			external_access: false,
			no_tls: false,
		});
	});

	it("shows a creation error and stays on the step", async () => {
		const w = await toAgentStep();
		let fail!: (e: unknown) => void;
		state.settings.saveConfig = vi.fn(
			() =>
				new Promise((_, reject) => {
					fail = reject;
				}),
		);
		await button(w, "Create agent").trigger("click");
		await flushPromises();
		expect(button(w, "Creating...").attributes("disabled")).toBeDefined();
		fail(new Error("disk full"));
		await flushPromises();
		expect(w.text()).toContain("disk full");
		expect(w.text()).toContain("Create your first agent");
	});

	it("stringifies a non-Error creation failure", async () => {
		const w = await toAgentStep();
		state.settings.saveConfig = vi.fn(() => Promise.reject(42));
		await button(w, "Create agent").trigger("click");
		await flushPromises();
		expect(w.text()).toContain("42");
	});
});

describe("Anthropic sign-in", () => {
	it("opens the authorization page and completes with a code", async () => {
		authFn("startAnthropic").mockImplementation(async () => {
			authRef<string>("anthropicUrl").value = "https://claude.ai/oauth?x=1";
		});
		const w = await mountWizard();
		await choose(w, "Anthropic");
		expect(w.text()).toContain(
			"Sign in with your Claude Pro or Claude Max account",
		);
		await button(w, "Sign in with Anthropic").trigger("click");
		await flushPromises();
		expect(w.get('a[href="https://claude.ai/oauth?x=1"]').text()).toBe(
			"https://claude.ai/oauth?x=1",
		);

		const complete = button(w, "Complete sign-in");
		expect(complete.attributes("disabled")).toBeDefined();
		const code = w.get('input[placeholder="Paste code here..."]');
		await code.trigger("keyup.enter");
		expect(authFn("completeAnthropic")).not.toHaveBeenCalled();
		await code.setValue("abc#123");
		expect(complete.attributes("disabled")).toBeUndefined();
		await code.trigger("keyup.enter");
		await flushPromises();

		expect(authFn("completeAnthropic")).toHaveBeenCalled();
		expect(authFn("clearOAuthState")).toHaveBeenCalled();
		expect(w.text()).toContain("Create your first agent");
		expect(selectorValue(w)).toBe("anthropic/claude-sonnet-5-5");
	});

	it("reports start and completion errors", async () => {
		authFn("startAnthropic").mockRejectedValueOnce(new Error("popup blocked"));
		const w = await mountWizard();
		await choose(w, "Anthropic");
		await button(w, "Sign in with Anthropic").trigger("click");
		await flushPromises();
		expect(w.text()).toContain("popup blocked");

		authRef<string>("anthropicUrl").value = "https://claude.ai/oauth";
		authRef<string>("anthropicCode").value = "bad";
		authFn("completeAnthropic").mockRejectedValueOnce("invalid code");
		await flushPromises();
		await button(w, "Complete sign-in").trigger("click");
		await flushPromises();
		expect(w.text()).toContain("invalid code");
		expect(w.text()).toContain("Connect Anthropic");
	});

	it("shows progress labels while busy", async () => {
		let release!: () => void;
		authFn("startAnthropic").mockImplementation(
			() =>
				new Promise<void>((resolve) => {
					release = resolve;
				}),
		);
		const w = await mountWizard();
		await choose(w, "Anthropic");
		await button(w, "Sign in with Anthropic").trigger("click");
		expect(button(w, "Opening...").attributes("disabled")).toBeDefined();
		release();
		await flushPromises();
		expect(hasButton(w, "Sign in with Anthropic")).toBe(true);
	});
});

describe("OpenAI sign-in", () => {
	it("shows the callback countdown and completes", async () => {
		authFn("startOpenAI").mockImplementation(async () => {
			authRef<string>("openAIUrl").value = "https://auth.openai.com/x";
			authRef<string>("openAICallbackUrl").value = "http://localhost:1455/cb";
			authRef<number>("openAIRemainingSeconds").value = 125;
		});
		const w = await mountWizard();
		await choose(w, "OpenAI");
		expect(w.text()).toContain("Connect OpenAI");
		expect(w.text()).toContain("Supports Plus/Pro Subscription plans!");
		await button(w, "Sign in with OpenAI").trigger("click");
		await flushPromises();
		expect(w.text()).toContain("http://localhost:1455/cb");
		expect(w.text()).toContain("Callback expires in 2:05.");

		await button(w, "I've authorized - Continue").trigger("click");
		await flushPromises();
		expect(authFn("completeOpenAI")).toHaveBeenCalled();
		expect(w.text()).toContain("Create your first agent");
		expect(selectorValue(w)).toBe("openai-codex/gpt-6.2-astra");
	});

	it("disables completion once the callback times out", async () => {
		authRef<string>("openAIUrl").value = "https://auth.openai.com/x";
		const w = await mountWizard();
		await choose(w, "OpenAI");
		// Selecting a provider clears earlier OAuth state through the stub,
		// which is a no-op here, so the URL stays.
		authRef<boolean>("openAITimedOut").value = true;
		await flushPromises();
		expect(w.text()).toContain("This OpenAI callback timed out");
		expect(button(w, "Timed out").attributes("disabled")).toBeDefined();
		authRef<boolean>("openAITimedOut").value = false;
		authRef<number | null>("openAIRemainingSeconds").value = null;
		await flushPromises();
		expect(w.text()).toContain("Callback expires in .");
	});

	it("reports start and completion errors", async () => {
		authFn("startOpenAI").mockRejectedValueOnce(new Error("port 1455 busy"));
		const w = await mountWizard();
		await choose(w, "OpenAI");
		await button(w, "Sign in with OpenAI").trigger("click");
		await flushPromises();
		expect(w.text()).toContain("port 1455 busy");

		authRef<string>("openAIUrl").value = "https://auth.openai.com/x";
		authFn("completeOpenAI").mockRejectedValueOnce("not authorized yet");
		await flushPromises();
		await button(w, "I've authorized - Continue").trigger("click");
		await flushPromises();
		expect(w.text()).toContain("not authorized yet");
	});
});

describe("Google sign-in", () => {
	it("offers sign-in and API key methods", async () => {
		const w = await mountWizard();
		await choose(w, "Google (Gemini)");
		expect(w.text()).toContain("Connect Google (Gemini)");
		expect(hasButton(w, "Sign in with Google (Gemini)")).toBe(true);
		await button(w, "API key").trigger("click");
		expect(w.get('input[type="password"]').attributes("placeholder")).toBe(
			"AIza...",
		);
		await button(w, "Sign in with Google (Gemini)").trigger("click");
		expect(w.find('input[type="password"]').exists()).toBe(false);
	});
});

describe("GitHub Copilot sign-in", () => {
	async function toDeviceCode() {
		authFn("startCopilot").mockImplementation(async () => {
			authRef<string>("copilotUserCode").value = "ABCD-1234";
			authRef<string>("copilotVerifyUrl").value =
				"https://github.com/login/device";
		});
		const w = await mountWizard();
		await choose(w, "GitHub Copilot");
		expect(w.text()).toContain("Sign in with your GitHub account");
		await button(w, "Sign in with GitHub").trigger("click");
		await flushPromises();
		return w;
	}

	it("shows the device code, copies it, and completes", async () => {
		const writeText = vi.fn(async () => {});
		vi.stubGlobal("navigator", { clipboard: { writeText } });
		const w = await toDeviceCode();
		const code = w.get<HTMLInputElement>("input[readonly]");
		expect(code.element.value).toBe("ABCD-1234");
		expect(w.findAll('a[href="https://github.com/login/device"]')).toHaveLength(
			2,
		);

		const select = vi.spyOn(code.element, "select");
		await code.trigger("click");
		expect(select).toHaveBeenCalled();

		vi.useFakeTimers();
		await button(w, "Copy").trigger("click");
		await flushPromises();
		expect(writeText).toHaveBeenCalledWith("ABCD-1234");
		expect(hasButton(w, "Copied")).toBe(true);
		vi.advanceTimersByTime(1500);
		await flushPromises();
		expect(hasButton(w, "Copied")).toBe(false);
		vi.useRealTimers();

		await button(w, "I've authorized - Continue").trigger("click");
		await flushPromises();
		expect(authFn("completeCopilot")).toHaveBeenCalled();
		expect(w.text()).toContain("Create your first agent");
		expect(selectorValue(w)).toBe("github-copilot/gpt-5.4");
	});

	it("falls back to execCommand when the clipboard API fails", async () => {
		vi.stubGlobal("navigator", {
			clipboard: {
				writeText: vi.fn(() => Promise.reject(new Error("denied"))),
			},
		});
		const execCommand = vi.fn(() => true);
		Object.defineProperty(document, "execCommand", {
			value: execCommand,
			configurable: true,
		});
		const w = await toDeviceCode();
		await button(w, "Copy").trigger("click");
		await flushPromises();
		expect(execCommand).toHaveBeenCalledWith("copy");
		expect(document.body.querySelector('input[style*="-9999px"]')).toBeNull();
		expect(hasButton(w, "Copied")).toBe(true);
	});

	it("skips copying an empty code", async () => {
		const writeText = vi.fn(async () => {});
		vi.stubGlobal("navigator", { clipboard: { writeText } });
		const w = await toDeviceCode();
		authRef<string>("copilotUserCode").value = "";
		await flushPromises();
		// The device-code panel hides without a code; call the copy path by
		// restoring the panel and clearing the code in between.
		authRef<string>("copilotUserCode").value = "X";
		await flushPromises();
		const copy = button(w, "Copy");
		authRef<string>("copilotUserCode").value = "";
		await copy.trigger("click");
		await flushPromises();
		expect(writeText).not.toHaveBeenCalled();
	});

	it("reports start and completion errors", async () => {
		authFn("startCopilot").mockRejectedValueOnce(new Error("rate limited"));
		const w = await mountWizard();
		await choose(w, "GitHub Copilot");
		await button(w, "Sign in with GitHub").trigger("click");
		await flushPromises();
		expect(w.text()).toContain("rate limited");

		authRef<string>("copilotUserCode").value = "ABCD-1234";
		authFn("completeCopilot").mockRejectedValueOnce("expired_token");
		await flushPromises();
		await button(w, "I've authorized - Continue").trigger("click");
		await flushPromises();
		expect(w.text()).toContain("expired_token");
	});
});

describe("self-hosted providers", () => {
	it("loads the configured base URI and discovers models", async () => {
		state.settings.config = baseConfig();
		(
			state.settings.config as ReturnType<typeof baseConfig>
		).models.providers.vllm = { auth: "", base_uri: " http://gpu:8000 " };
		state.tools.models_list = () => ["vllm/llama", "vllm/qwen"];
		const w = await mountWizard();
		await providerButton(w, "vLLM").trigger("click");
		await flushPromises();
		expect(state.callTool).toHaveBeenCalledWith("models_list", {
			provider: "vllm",
			base_uri: "http://gpu:8000",
			auth: undefined,
		});
		await button(w, "Continue").trigger("click");
		await flushPromises();
		expect(w.text()).toContain("Connected to vLLM.");
		expect(w.get<HTMLInputElement>('input[type="text"]').element.value).toBe(
			"http://gpu:8000",
		);
		expect(button(w, "Continue").attributes("disabled")).toBeUndefined();
		expect(w.find('button[title="Endpoint OK"]').exists()).toBe(true);

		await button(w, "Continue").trigger("click");
		await flushPromises();
		const saved = state.settings.saveConfig.mock.calls[0][0] as ReturnType<
			typeof baseConfig
		>;
		expect(saved.models.providers.vllm).toEqual({
			auth: "",
			base_uri: "http://gpu:8000",
		});
		expect(state.callTool).not.toHaveBeenCalledWith(
			"auth_set",
			expect.anything(),
		);
		expect(w.text()).toContain("Create your first agent");
		expect(selectorOptions(w)).toEqual(["vllm/llama", "vllm/qwen"]);
		expect(w.text()).toContain("Select a model discovered from the endpoint.");

		state.tools.models_list = () => ["vllm/new"];
		await button(w, "Refresh models").trigger("click");
		await flushPromises();
		expect(selectorOptions(w)).toEqual(["vllm/new"]);
	});

	it("validates the endpoint and stores a bearer token", async () => {
		let fail = true;
		state.tools.models_list = () => {
			if (fail) throw new Error("connection refused");
			return ["ollama/llama3"];
		};
		state.settings.config = null;
		const w = await mountWizard();
		await choose(w, "Ollama");
		expect(w.text()).toContain("connection refused");
		expect(w.find('button[title="Endpoint check failed"]').exists()).toBe(true);
		const uri = w.get<HTMLInputElement>('input[type="text"]');
		expect(uri.attributes("placeholder")).toBe("http://127.0.0.1:11434");

		await uri.setValue("http://box:11434");
		await flushPromises();
		expect(w.text()).not.toContain("connection refused");
		expect(w.find('button[title="Test endpoint"]').exists()).toBe(true);

		fail = false;
		await uri.trigger("blur");
		await flushPromises();
		expect(w.text()).toContain("Connected to Ollama.");

		await w.get('input[type="password"]').setValue(" tok ");
		await w.get('button[title="Endpoint OK"]').trigger("click");
		await flushPromises();
		expect(state.callTool).toHaveBeenLastCalledWith("models_list", {
			provider: "ollama",
			base_uri: "http://box:11434",
			auth: "tok",
		});

		await uri.trigger("keyup.enter");
		await flushPromises();
		const saved = state.settings.saveConfig.mock.calls[0][0] as ReturnType<
			typeof baseConfig
		>;
		expect(saved.models.providers.ollama).toEqual({
			auth: "auth:ollama:default",
			base_uri: "http://box:11434",
		});
		expect(saved.agents).toEqual([]);
		expect(state.callTool).toHaveBeenCalledWith("auth_set", {
			name: "ollama:default",
			value: "tok",
		});
		expect(selectorValue(w)).toBe("ollama/llama3");
	});

	it("uses the placeholder base URI when the field is empty", async () => {
		const w = await mountWizard();
		await choose(w, "vLLM");
		await w.get('input[type="text"]').setValue("");
		await button(w, "Continue").trigger("click");
		await flushPromises();
		const saved = state.settings.saveConfig.mock.calls[0][0] as ReturnType<
			typeof baseConfig
		>;
		expect(saved.models.providers.vllm.base_uri).toBe("http://127.0.0.1:8000");
	});

	it("shows loading state and ignores tests while loading", async () => {
		let release!: (v: string) => void;
		state.tools.models_list = () =>
			new Promise<string>((resolve) => {
				release = resolve;
			});
		const w = await mountWizard();
		await providerButton(w, "vLLM").trigger("click");
		await flushPromises();
		await button(w, "Continue").trigger("click");
		await flushPromises();
		const test = w.get('button[title="Test endpoint"]');
		expect(test.attributes("disabled")).toBeDefined();
		expect(test.find("svg.animate-spin").exists()).toBe(true);
		const calls = state.callTool.mock.calls.length;
		await w.get('input[type="text"]').trigger("blur");
		expect(state.callTool.mock.calls.length).toBe(calls);
		release('["vllm/a"]');
		await flushPromises();
		expect(test.attributes("disabled")).toBeUndefined();
	});

	it("survives a failed config load", async () => {
		state.settings.fetchConfig = vi.fn(() => Promise.reject(new Error("401")));
		const w = await mountWizard();
		await choose(w, "vLLM");
		expect(w.text()).toContain("Connect vLLM");
	});

	it("stringifies a non-Error model discovery failure", async () => {
		state.tools.models_list = () => Promise.reject("bad gateway");
		const w = await mountWizard();
		await choose(w, "vLLM");
		expect(w.text()).toContain("bad gateway");
	});
});
