// @vitest-environment happy-dom
import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h } from "vue";
import type { MCPToolInfo } from "../composables/useMCP";
import { toolRunStorageKey } from "../lib/systemToolFields";
import SystemToolsView from "./SystemToolsView.vue";

// #1615: System > Tools lists MCP tools and activated skills, and runs a
// tool from a form built from its input schema.

const listTools = vi.fn<() => Promise<MCPToolInfo[]>>();
const callTool =
	vi.fn<(name: string, args?: Record<string, unknown>) => Promise<string>>();

vi.mock("../composables/useMCP", () => ({
	useMCP: () => ({ listTools, callTool }),
}));

const AppLayoutStub = defineComponent({
	setup(_, { slots }) {
		return () =>
			h("div", [
				h("header", slots.header?.()),
				h("nav", slots["header-actions"]?.()),
				h("main", slots.default?.()),
			]);
	},
});

// A native <select> standing in for the radix AppSelect.
const AppSelectStub = defineComponent({
	props: {
		modelValue: { type: String, default: "" },
		options: { type: Array as () => { value: string; label: string }[] },
		ariaLabel: String,
	},
	emits: ["update:modelValue"],
	setup(props, { emit }) {
		return () =>
			h(
				"select",
				{
					"aria-label": props.ariaLabel,
					value: props.modelValue,
					onChange: (e: Event) =>
						emit("update:modelValue", (e.target as HTMLSelectElement).value),
				},
				(props.options ?? []).map((o) =>
					h("option", { value: o.value }, o.label),
				),
			);
	},
});

const TOOLS: MCPToolInfo[] = [
	{ name: "task_list", description: "List tasks" },
	{
		name: "task_run",
		description: "Run a task",
		inputSchema: {
			required: ["name"],
			properties: {
				name: { type: "string", description: "Task name" },
				count: { type: "integer", default: 2 },
				dry: { type: "boolean" },
				mode: { enum: ["fast", "slow"] },
				opts: { type: "object", default: { a: 1 } },
			},
		},
	},
	{ name: "agent_list", description: "  " },
];

const SKILLS = [
	{
		name: "zeta",
		description: "Z skill",
		path: "/z",
		enabled: true,
		source: "user",
	},
	{ name: "alpha", path: "/a", enabled: true, source: "builtin" },
	{
		name: "off",
		description: "Off",
		path: "/o",
		enabled: false,
		source: "user",
	},
];

function deferred<T>() {
	let resolve!: (value: T) => void;
	let reject!: (reason?: unknown) => void;
	const promise = new Promise<T>((res, rej) => {
		resolve = res;
		reject = rej;
	});
	return { promise, resolve, reject };
}

function mountView() {
	return mount(SystemToolsView, {
		global: {
			stubs: {
				AppLayout: AppLayoutStub,
				AppSelect: AppSelectStub,
				SystemSettingsLink: true,
				TranscriptionStatusCard: true,
				teleport: true,
			},
		},
	});
}

beforeEach(() => {
	listTools.mockResolvedValue(TOOLS);
	callTool.mockImplementation(async (name) =>
		name === "skills_list" ? JSON.stringify(SKILLS) : `ran ${name}`,
	);
});

afterEach(() => {
	vi.clearAllMocks();
	window.sessionStorage.clear();
});

describe("catalog", () => {
	it("shows skeletons and a disabled refresh button while loading", async () => {
		const pending = deferred<MCPToolInfo[]>();
		listTools.mockReturnValue(pending.promise);
		const wrapper = mountView();
		await flushPromises();
		const refresh = wrapper.get("nav button");
		expect(refresh.text()).toBe("Refreshing…");
		expect(refresh.attributes("disabled")).toBeDefined();
		expect(wrapper.text()).not.toContain("Tool Groups");
		pending.resolve(TOOLS);
		await flushPromises();
		expect(refresh.text()).toBe("Refresh");
		expect(refresh.attributes("disabled")).toBeUndefined();
	});

	it("renders grouped tools and enabled skills sorted by name", async () => {
		const wrapper = mountView();
		await flushPromises();
		expect(callTool).toHaveBeenCalledWith("skills_list");

		const counts = wrapper.findAll("p.text-3xl").map((p) => p.text());
		expect(counts).toEqual(["2", "3", "2"]);

		const groups = wrapper.findAll("h3").map((h3) => h3.text());
		expect(groups).toEqual(["Agent", "Tasks", "Activated Skills"]);
		expect(wrapper.findAll("code").map((c) => c.text())).toEqual([
			"agent_list",
			"task_list",
			"task_run",
		]);
		expect(wrapper.text()).toContain("1 tool");
		expect(wrapper.text()).toContain("2 tools");
		expect(wrapper.text()).toContain(
			"No description available from the MCP server.",
		);

		const skillNames = wrapper
			.findAll("span.text-sm.font-semibold")
			.map((s) => s.text());
		expect(skillNames).toEqual(["alpha", "zeta"]);
		expect(wrapper.text()).toContain(
			"No description available for this skill.",
		);
		expect(wrapper.text()).not.toContain("Off");
		expect(wrapper.text()).toContain("2 active");
	});

	it("says when no skills are enabled", async () => {
		callTool.mockResolvedValue("null");
		const wrapper = mountView();
		await flushPromises();
		expect(wrapper.text()).toContain(
			"No skills are enabled in the current configuration.",
		);
	});

	it("shows load errors and clears the catalog", async () => {
		const wrapper = mountView();
		await flushPromises();
		listTools.mockRejectedValueOnce(new Error("MCP down"));
		await wrapper.get("nav button").trigger("click");
		await flushPromises();
		expect(wrapper.text()).toContain("MCP down");
		expect(wrapper.findAll("code")).toHaveLength(0);
		expect(wrapper.findAll("p.text-3xl").map((p) => p.text())).toEqual([
			"0",
			"0",
			"0",
		]);

		callTool.mockRejectedValueOnce("plain failure");
		await wrapper.get("nav button").trigger("click");
		await flushPromises();
		expect(wrapper.text()).toContain("plain failure");
		expect(wrapper.text()).not.toContain("MCP down");
	});
});

describe("run modal", () => {
	async function openRun(name: string) {
		const wrapper = mountView();
		await flushPromises();
		await wrapper.get(`[data-testid="run-tool-${name}"]`).trigger("click");
		return wrapper;
	}

	const output = (wrapper: Awaited<ReturnType<typeof openRun>>) =>
		wrapper.get('[data-testid="tool-run-output"]');

	it("builds a form from the input schema with defaults", async () => {
		const wrapper = await openRun("task_run");
		expect(wrapper.text()).toContain("Run Tool");
		expect(wrapper.findAll("label").map((l) => l.text())).toEqual([
			"count",
			"dry",
			"mode",
			"name",
			"opts",
		]);
		expect(wrapper.text()).toContain("Required");
		const inputs = wrapper.findAll<HTMLInputElement>("input[type=text]");
		expect(inputs.map((i) => i.element.value)).toEqual(["2", ""]);
		expect(inputs[1].attributes("placeholder")).toBe("Task name");
		expect(wrapper.get("textarea").element.value).toBe('{\n  "a": 1\n}');
		const selects = wrapper.findAll("select");
		expect(selects.map((s) => s.attributes("aria-label"))).toEqual([
			"dry",
			"mode",
		]);
		expect(selects[1].findAll("option").map((o) => o.text())).toEqual([
			"Choose mode",
			"fast",
			"slow",
		]);
		expect(wrapper.text()).toContain("Task name Type: string");
		expect(output(wrapper).text()).toBe("Run the tool to see its output.");
		expect(
			JSON.parse(
				window.sessionStorage.getItem(toolRunStorageKey("task_run")) ?? "",
			),
		).toEqual({
			values: {
				count: "2",
				dry: "",
				mode: "",
				name: "",
				opts: '{\n  "a": 1\n}',
			},
			output: "",
		});
	});

	it("says when a tool declares no arguments", async () => {
		const wrapper = await openRun("agent_list");
		expect(wrapper.text()).toContain(
			"This tool does not declare any input arguments.",
		);
	});

	it("requires required fields", async () => {
		const wrapper = await openRun("task_run");
		await wrapper.get("button.bg-blue-600").trigger("click");
		await flushPromises();
		expect(wrapper.text()).toContain("name is required.");
		expect(callTool).not.toHaveBeenCalledWith("task_run", expect.anything());
	});

	it("reports invalid field values", async () => {
		const wrapper = await openRun("task_run");
		await wrapper.findAll("input[type=text]")[1].setValue("build");
		await wrapper.findAll("input[type=text]")[0].setValue("1.5");
		await wrapper.get("button.bg-blue-600").trigger("click");
		await flushPromises();
		expect(wrapper.text()).toContain("count must be an integer.");
	});

	it("runs the tool with parsed arguments and persists the result", async () => {
		const pending = deferred<string>();
		const wrapper = await openRun("task_run");
		callTool.mockReturnValueOnce(pending.promise);
		const inputs = wrapper.findAll("input[type=text]");
		await inputs[1].setValue("  build  ");
		// The stubbed teleport re-renders the modal, so query fresh elements.
		await wrapper.findAll("select")[0].setValue("true");
		await wrapper.findAll("select")[1].setValue("slow");

		const run = () => wrapper.get("button.bg-blue-600");
		await run().trigger("click");
		await flushPromises();
		expect(run().text()).toBe("Running…");
		expect(run().attributes("disabled")).toBeDefined();
		expect(callTool).toHaveBeenLastCalledWith("task_run", {
			count: 2,
			dry: true,
			mode: "slow",
			name: "build",
			opts: { a: 1 },
		});
		pending.resolve("task output");
		await flushPromises();
		expect(run().text()).toBe("Run Tool");
		expect(output(wrapper).text()).toBe("task output");
		expect(wrapper.text()).not.toContain("Showing stale output");
		expect(
			JSON.parse(
				window.sessionStorage.getItem(toolRunStorageKey("task_run")) ?? "",
			),
		).toMatchObject({
			values: { name: "  build  ", mode: "slow" },
			output: "task output",
		});
	});

	it("restores stored values and marks earlier output stale", async () => {
		window.sessionStorage.setItem(
			toolRunStorageKey("task_run"),
			JSON.stringify({
				values: { name: "deploy", count: 7 },
				output: "previous output",
			}),
		);
		const pending = deferred<string>();
		const wrapper = await openRun("task_run");
		const inputs = wrapper.findAll<HTMLInputElement>("input[type=text]");
		// A non-string stored value falls back to the schema default.
		expect(inputs.map((i) => i.element.value)).toEqual(["2", "deploy"]);
		expect(output(wrapper).text()).toBe("previous output");
		expect(wrapper.text()).toContain("Showing stale output");

		callTool.mockReturnValueOnce(pending.promise);
		await wrapper.get("button.bg-blue-600").trigger("click");
		expect(wrapper.text()).toContain("Showing stale output");
		expect(output(wrapper).classes()).toContain("text-gray-400");
		pending.reject("tool exploded");
		await flushPromises();
		expect(wrapper.text()).toContain("tool exploded");
		expect(wrapper.text()).not.toContain("Showing stale output");
		expect(output(wrapper).text()).toBe("previous output");
	});

	it("shows Error messages from a failed run", async () => {
		const wrapper = await openRun("task_list");
		callTool.mockRejectedValueOnce(new Error("permission denied"));
		await wrapper.get("button.bg-blue-600").trigger("click");
		await flushPromises();
		expect(callTool).toHaveBeenLastCalledWith("task_list", {});
		expect(wrapper.text()).toContain("permission denied");
	});

	it("closes", async () => {
		const wrapper = await openRun("task_list");
		const close = wrapper.findAll("button").find((b) => b.text() === "Close");
		await close?.trigger("click");
		expect(wrapper.text()).not.toContain("Run Tool");
		expect(wrapper.find('[data-testid="tool-run-output"]').exists()).toBe(
			false,
		);
	});
});
