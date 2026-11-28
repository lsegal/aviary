// @vitest-environment happy-dom
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import {
	afterEach,
	beforeEach,
	describe,
	expect,
	it,
	type Mock,
	vi,
} from "vitest";
import { nextTick } from "vue";
import { createMemoryHistory, createRouter, type Router } from "vue-router";
import {
	type AgentItemDialog,
	useAgentItemDialogs,
} from "../../composables/useAgentItemDialogs";
import { useCompiledPanelOpen } from "../../composables/useTaskCompiles";
import type { DataOwner } from "../../lib/deleteData";
import { useAgentFilesStore } from "../../stores/agentFiles";
import {
	type AgentTask,
	type AppConfig,
	useSettingsStore,
} from "../../stores/settings";
import AgentItemDialogs from "./AgentItemDialogs.vue";

// #1611: the sidebar's add, edit and delete dialogs for an agent's tasks
// and files. Children are stubbed to plain inputs so each test drives the
// dialog's own state, and stores and MCP calls are faked.

const callTool = vi.hoisted(() => vi.fn());
vi.mock("../../composables/useMCP", () => ({
	useMCP: () => ({ callTool }),
}));

vi.mock("radix-vue", async () => {
	const { defineComponent, h } = await import("vue");
	const pass = (name: string, tag = "div") =>
		defineComponent({
			name,
			inheritAttrs: false,
			setup(_, { slots, attrs }) {
				return () => h(tag, attrs, slots.default?.());
			},
		});
	return {
		DialogRoot: defineComponent({
			name: "DialogRoot",
			props: { open: Boolean },
			emits: ["update:open"],
			setup(props, { slots, emit }) {
				return () =>
					props.open
						? h("div", { "data-testid": "dialog-root" }, [
								h("button", {
									type: "button",
									"data-testid": "dialog-dismiss",
									onClick: () => emit("update:open", false),
								}),
								slots.default?.(),
							])
						: null;
			},
		}),
		DialogPortal: pass("DialogPortal"),
		DialogOverlay: pass("DialogOverlay"),
		DialogContent: pass("DialogContent"),
		DialogTitle: pass("DialogTitle", "h2"),
		DialogDescription: pass("DialogDescription", "p"),
		DialogClose: pass("DialogClose", "button"),
	};
});

vi.mock("../ui/ConfirmDialog.vue", async () => {
	const { defineComponent, h, ref } = await import("vue");
	return {
		default: defineComponent({
			props: {
				open: Boolean,
				title: String,
				action: Function,
				testId: String,
			},
			emits: ["update:open"],
			setup(props, { slots, emit }) {
				const error = ref("");
				return () =>
					props.open
						? h("div", { "data-testid": props.testId }, [
								h("h2", props.title),
								slots.default?.(),
								error.value ? h("p", { role: "alert" }, error.value) : null,
								h("button", {
									"data-testid": "confirm",
									onClick: async () => {
										try {
											await (props.action as () => Promise<void>)();
										} catch (e) {
											error.value = (e as Error).message;
										}
									},
								}),
								h("button", {
									"data-testid": "confirm-cancel",
									onClick: () => emit("update:open", false),
								}),
							])
						: null;
			},
		}),
	};
});

vi.mock("../ui/DeleteDataOption.vue", async () => {
	const { defineComponent, h } = await import("vue");
	return {
		default: defineComponent({
			props: { modelValue: Boolean, name: String, testId: String },
			emits: ["update:modelValue"],
			setup(props, { emit }) {
				return () =>
					h("input", {
						type: "checkbox",
						"data-testid": props.testId,
						"data-name": props.name,
						checked: props.modelValue,
						onChange: (e: Event) =>
							emit("update:modelValue", (e.target as HTMLInputElement).checked),
					});
			},
		}),
	};
});

vi.mock("../ui/SegmentedControl.vue", async () => {
	const { defineComponent, h } = await import("vue");
	return {
		default: defineComponent({
			props: { modelValue: String, label: String, options: Array },
			emits: ["update:modelValue"],
			setup(props, { emit }) {
				type Option = { value: string; label: string; disabled?: boolean };
				return () =>
					h(
						"div",
						{
							"data-testid": `seg-${props.label}`,
							"data-value": props.modelValue,
						},
						(props.options as Option[]).map((o) =>
							h(
								"button",
								{
									type: "button",
									"data-testid": `seg-${props.label}-${o.value}`,
									disabled: o.disabled,
									onClick: () => emit("update:modelValue", o.value),
								},
								o.label,
							),
						),
					);
			},
		}),
	};
});

vi.mock("../ui/select", async () => {
	const { defineComponent, h } = await import("vue");
	const select = (name: string) =>
		defineComponent({
			name,
			inheritAttrs: false,
			props: { modelValue: String, options: Array, disabled: Boolean },
			emits: ["update:modelValue"],
			setup(props, { emit, attrs }) {
				return () =>
					h("input", {
						id: attrs.id,
						"data-testid": attrs["data-testid"] ?? attrs.id,
						"data-options": JSON.stringify(props.options),
						disabled: props.disabled,
						value: props.modelValue,
						onInput: (e: Event) =>
							emit("update:modelValue", (e.target as HTMLInputElement).value),
					});
			},
		});
	return { AppCombobox: select("AppCombobox"), AppSelect: select("AppSelect") };
});

vi.mock("../TriggerEditor.vue", async () => {
	const { defineComponent, h } = await import("vue");
	return {
		default: defineComponent({
			props: { modelValue: Object, preview: Function, optional: Boolean },
			emits: ["update:modelValue"],
			setup(props, { emit }) {
				return () =>
					h("input", {
						"data-testid": "trigger",
						"data-optional": String(props.optional),
						value: (props.modelValue as { trigger: string }).trigger,
						onInput: (e: Event) =>
							emit("update:modelValue", {
								...(props.modelValue as object),
								trigger: (e.target as HTMLInputElement).value,
							}),
					});
			},
		}),
	};
});

vi.mock("../TaskPromptSection.vue", async () => {
	const { defineComponent, h } = await import("vue");
	return {
		default: defineComponent({
			props: {
				prompt: String,
				precompile: Boolean,
				showPrecompile: Boolean,
				type: String,
				agent: String,
				savedTask: Object,
			},
			emits: ["update:prompt", "update:precompile"],
			setup(props, { emit }) {
				return () =>
					h(
						"div",
						{
							"data-testid": "prompt-section",
							"data-type": props.type,
							"data-agent": props.agent,
							"data-saved": props.savedTask
								? (props.savedTask as AgentTask).name
								: "",
						},
						[
							h("textarea", {
								"data-testid": "prompt",
								value: props.prompt,
								onInput: (e: Event) =>
									emit(
										"update:prompt",
										(e.target as HTMLTextAreaElement).value,
									),
							}),
							props.showPrecompile
								? h("input", {
										type: "checkbox",
										"data-testid": "precompile",
										checked: props.precompile,
										onChange: (e: Event) =>
											emit(
												"update:precompile",
												(e.target as HTMLInputElement).checked,
											),
									})
								: null,
						],
					);
			},
		}),
	};
});

type SaveTasks = (
	agent: string,
	update: (tasks: AgentTask[]) => AgentTask[],
	deleteData?: DataOwner[],
) => Promise<void>;

let router: Router;
let wrapper: VueWrapper | undefined;
let settings: ReturnType<typeof useSettingsStore>;
let files: ReturnType<typeof useAgentFilesStore>;
let saveAgentTasks: Mock<SaveTasks>;
let renameTask: Mock<
	(agent: string, from: string, to: string) => Promise<void>
>;
const dialogs = useAgentItemDialogs();

function config(agents: unknown[], precompute?: boolean): AppConfig {
	return {
		agents,
		scheduler: precompute === undefined ? {} : { precompute_tasks: precompute },
	} as unknown as AppConfig;
}

function tasksOf(agent: string): AgentTask[] {
	return settings.config?.agents.find((a) => a.name === agent)?.tasks ?? [];
}

const AGENTS = () => [
	{
		name: "alice",
		channels: [
			{ type: "slack", id: "s1", name: "ops" },
			{ type: "discord", id: "" },
		],
		projects: [{ target: "acme/web" }],
		tasks: [
			{ name: "daily", type: "prompt", prompt: "Summarize", target: "" },
			{ name: "build", type: "script", prompt: "make" },
			{
				name: "nightly",
				type: "project",
				project: "acme/web",
				prompt: "Triage acme/web issues in ~/src/web",
				model: "big-model",
				precompile: true,
				compiled: "x",
				trigger: "daily",
				at: "02:00",
			},
			{
				name: "old",
				type: "project",
				project: "acme/gone",
				prompt: "Check",
				target: "channel:removed",
			},
		],
	},
	{
		name: "bob",
		channels: [{ type: "telegram", id: "t1" }],
		projects: [{ target: "bob/api" }, { target: "" }],
		tasks: [{ name: "daily", type: "prompt" }],
	},
];

async function mountDialogs(precompute?: boolean) {
	settings.config = config(AGENTS(), precompute);
	wrapper = mount(AgentItemDialogs, {
		global: { plugins: [router] },
		attachTo: document.body,
	});
	await flushPromises();
	return wrapper;
}

async function open(d: AgentItemDialog) {
	dialogs.open(d);
	await flushPromises();
}

const w = () => wrapper as VueWrapper;
const byId = (id: string) => w().find(`[data-testid="${id}"]`);
const options = (id: string) =>
	JSON.parse(byId(id).attributes("data-options") ?? "[]") as {
		value: string;
		label: string;
	}[];

async function setName(name: string) {
	await w().find("#agent-item-name").setValue(name);
}

async function submit() {
	await w().find("form").trigger("submit");
	await flushPromises();
}

beforeEach(async () => {
	setActivePinia(createPinia());
	settings = useSettingsStore();
	files = useAgentFilesStore();
	saveAgentTasks = vi.fn<SaveTasks>(async (agent, update) => {
		const a = settings.config?.agents.find((x) => x.name === agent);
		if (a) a.tasks = update(a.tasks ?? []);
	});
	renameTask = vi.fn(async () => {});
	settings.saveAgentTasks = saveAgentTasks;
	settings.renameTask = renameTask;
	callTool.mockReset();
	callTool.mockResolvedValue({});
	router = createRouter({
		history: createMemoryHistory(),
		routes: [{ path: "/:any(.*)*", component: { render: () => null } }],
	});
	await router.push("/");
	await router.isReady();
	useCompiledPanelOpen().value = false;
});

afterEach(() => {
	dialogs.close();
	wrapper?.unmount();
	wrapper = undefined;
	vi.restoreAllMocks();
});

describe("file dialogs", () => {
	it("creates a file and opens it", async () => {
		files.create = vi.fn(async () => "NOTES.md");
		const push = vi.spyOn(router, "push");
		await mountDialogs();
		expect(byId("dialog-root").exists()).toBe(false);
		await open({ kind: "file", agent: "alice", mode: "create" });

		expect(byId("file-dialog").exists()).toBe(true);
		expect(w().find("h2").text()).toBe("Add file");
		expect(w().text()).toContain("A new markdown file in alice's directory.");
		expect(w().find("label[for=agent-item-name]").text()).toBe("File name");
		expect(w().find("#agent-item-name").attributes("placeholder")).toBe(
			"IDENTITY.md",
		);
		expect(w().text()).toContain("Root-level");
		const create = w().find("button[type=submit]");
		expect(create.text()).toBe("Create");
		expect(create.attributes("disabled")).toBeDefined();

		await setName("notes");
		expect(create.attributes("disabled")).toBeUndefined();
		await submit();

		expect(files.create).toHaveBeenCalledWith("alice", "notes");
		expect(push).toHaveBeenCalledWith({
			path: "/agents/alice/files",
			query: { file: "NOTES.md" },
		});
		expect(dialogs.dialog.value).toBeNull();
	});

	it("ignores a submit with a blank name", async () => {
		files.create = vi.fn(async () => "x.md");
		await mountDialogs();
		await open({ kind: "file", agent: "alice", mode: "create" });
		await setName("   ");
		await submit();
		expect(files.create).not.toHaveBeenCalled();
		expect(dialogs.dialog.value).not.toBeNull();
	});

	it("shows a failed create and stays open", async () => {
		files.create = vi.fn(async () => {
			throw new Error("File name is required.");
		});
		await mountDialogs();
		await open({ kind: "file", agent: "alice", mode: "create" });
		await setName("x");
		await submit();
		expect(w().find("[role=alert]").text()).toBe("File name is required.");
		expect(dialogs.dialog.value).not.toBeNull();
		expect(
			w().find("button[type=submit]").attributes("disabled"),
		).toBeUndefined();
	});

	it("reports a non-Error rejection as text", async () => {
		files.create = vi.fn(() => Promise.reject("boom"));
		await mountDialogs();
		await open({ kind: "file", agent: "alice", mode: "create" });
		await setName("x");
		await submit();
		expect(w().find("[role=alert]").text()).toBe("boom");
	});

	it("renames the shown file and follows it", async () => {
		files.rename = vi.fn(async () => "NEW.md");
		await router.push({
			path: "/agents/alice/files",
			query: { file: "OLD.md" },
		});
		const replace = vi.spyOn(router, "replace");
		await mountDialogs();
		await open({ kind: "file", agent: "alice", mode: "edit", name: "OLD.md" });

		expect(w().find("h2").text()).toBe("Rename file");
		expect(w().text()).toContain("Renames OLD.md, keeping its content.");
		expect(
			(w().find("#agent-item-name").element as HTMLInputElement).value,
		).toBe("OLD.md");
		expect(w().find("button[type=submit]").text()).toBe("Save");
		await setName("new");
		await submit();

		expect(files.rename).toHaveBeenCalledWith("alice", "OLD.md", "new");
		expect(replace).toHaveBeenCalledWith({
			path: "/agents/alice/files",
			query: { file: "NEW.md" },
		});
	});

	it("renames a file that is not shown without navigating", async () => {
		files.rename = vi.fn(async () => "NEW.md");
		const replace = vi.spyOn(router, "replace");
		await mountDialogs();
		await open({ kind: "file", agent: "alice", mode: "edit", name: "OLD.md" });
		await setName("new");
		await submit();
		expect(files.rename).toHaveBeenCalled();
		expect(replace).not.toHaveBeenCalled();
	});

	it("closes on dismiss unless busy", async () => {
		let finish = () => {};
		files.create = vi.fn(
			() =>
				new Promise<string>((resolve) => {
					finish = () => resolve("A.md");
				}),
		);
		await mountDialogs();
		await open({ kind: "file", agent: "alice", mode: "create" });
		await setName("a");
		await w().find("form").trigger("submit");
		await nextTick();
		expect(
			w().find("button[type=submit]").attributes("disabled"),
		).toBeDefined();
		// A second submit while busy is ignored.
		await w().find("form").trigger("submit");
		expect(files.create).toHaveBeenCalledTimes(1);
		await byId("dialog-dismiss").trigger("click");
		expect(dialogs.dialog.value).not.toBeNull();
		finish();
		await flushPromises();
		expect(dialogs.dialog.value).toBeNull();

		await open({ kind: "file", agent: "alice", mode: "create" });
		await byId("dialog-dismiss").trigger("click");
		expect(dialogs.dialog.value).toBeNull();
	});

	it("deletes a shown file and leaves its page", async () => {
		files.remove = vi.fn(async () => {});
		await router.push({ path: "/agents/alice/files", query: { file: "A.md" } });
		const replace = vi.spyOn(router, "replace");
		await mountDialogs();
		await open({ kind: "file", agent: "alice", mode: "delete", name: "A.md" });

		expect(byId("dialog-root").exists()).toBe(false);
		const confirm = byId("delete-item-dialog");
		expect(confirm.find("h2").text()).toBe("Delete file?");
		expect(confirm.text()).toContain("A.md");
		expect(byId("task-delete-data").exists()).toBe(false);
		await byId("confirm").trigger("click");
		await flushPromises();

		expect(files.remove).toHaveBeenCalledWith("alice", "A.md");
		expect(dialogs.dialog.value).toBeNull();
		expect(replace).toHaveBeenCalledWith({ path: "/agents/alice/files" });
	});

	it("does nothing on delete without a name and closes on cancel", async () => {
		files.remove = vi.fn(async () => {});
		await mountDialogs();
		await open({ kind: "file", agent: "alice", mode: "delete" });
		await byId("confirm").trigger("click");
		await flushPromises();
		expect(files.remove).not.toHaveBeenCalled();
		await byId("confirm-cancel").trigger("click");
		expect(dialogs.dialog.value).toBeNull();
	});
});

describe("task create", () => {
	it("creates an agent task with precompile and opens it", async () => {
		const push = vi.spyOn(router, "push");
		await mountDialogs();
		await open({ kind: "task", agent: "alice", mode: "create" });

		expect(byId("task-dialog").exists()).toBe(true);
		expect(w().find("h2").text()).toBe("Add task");
		expect(w().text()).toContain("A scheduled task for alice.");
		expect(w().find("label[for=agent-item-name]").text()).toBe("Task name");
		expect(options("agent-task-agent").map((o) => o.value)).toEqual([
			"alice",
			"bob",
		]);
		expect(byId("agent-task-agent").attributes("disabled")).toBeUndefined();
		// Only channels with a type and ID are offered.
		expect(options("task-send-to")).toEqual([
			{ value: "", label: "Task session only" },
			{ value: "channel:ops", label: "ops · slack" },
		]);
		expect(byId("trigger").attributes("data-optional")).toBe("true");
		expect(byId("trigger").attributes("value")).toBe("");
		expect((byId("precompile").element as HTMLInputElement).checked).toBe(true);
		expect(byId("task-model-override").exists()).toBe(false);

		await setName(" brief ");
		await byId("prompt").setValue("Brief me");
		await byId("task-send-to").setValue("channel:ops");
		await submit();

		expect(saveAgentTasks).toHaveBeenCalledTimes(1);
		const created = tasksOf("alice").find((t) => t.name === "brief");
		expect(created).toMatchObject({
			name: "brief",
			type: "prompt",
			prompt: "Brief me",
			target: "channel:ops",
			enabled: true,
			precompile: true,
			from_file: true,
		});
		expect(created).not.toHaveProperty("project");
		expect(callTool).toHaveBeenCalledWith("config_task_convert_to_script", {
			agent: "alice",
			task: "brief",
		});
		expect(push).toHaveBeenCalledWith({
			path: "/agents/alice/tasks",
			query: { task: "brief" },
		});
		expect(dialogs.dialog.value).toBeNull();
	});

	it("calls onCreated instead of navigating, and survives a failed precompile", async () => {
		const push = vi.spyOn(router, "push");
		const err = vi.spyOn(console, "error").mockImplementation(() => {});
		callTool.mockRejectedValue(new Error("offline"));
		const onCreated = vi.fn();
		await mountDialogs();
		await open({ kind: "task", agent: "alice", mode: "create", onCreated });
		await setName("x");
		await byId("prompt").setValue("Do it");
		await submit();
		expect(onCreated).toHaveBeenCalledWith("alice", "x");
		expect(push).not.toHaveBeenCalled();
		expect(err).toHaveBeenCalledWith("precompute task", expect.any(Error));
		expect(dialogs.dialog.value).toBeNull();
	});

	it("skips compiling an empty prompt or with precompile off", async () => {
		await mountDialogs(false);
		await open({ kind: "task", agent: "alice", mode: "create" });
		expect((byId("precompile").element as HTMLInputElement).checked).toBe(
			false,
		);
		await setName("quiet");
		await byId("prompt").setValue("hello");
		await submit();
		expect(callTool).not.toHaveBeenCalled();
		expect(tasksOf("alice").find((t) => t.name === "quiet")).not.toHaveProperty(
			"precompile",
		);
	});

	it("creates a script task without precompile", async () => {
		await mountDialogs();
		await open({ kind: "task", agent: "alice", mode: "create" });
		await byId("seg-Type-script").trigger("click");
		expect(byId("prompt-section").attributes("data-type")).toBe("script");
		// Creating still shows the choice, off for a script.
		expect((byId("precompile").element as HTMLInputElement).checked).toBe(
			false,
		);
		expect(w().text()).not.toContain("changes its type");
	});

	it("creates under another agent and clears its Send To", async () => {
		await mountDialogs();
		await open({ kind: "task", agent: "alice", mode: "create" });
		await byId("task-send-to").setValue("channel:ops");
		await byId("agent-task-agent").setValue("bob");
		await flushPromises();
		expect((byId("task-send-to").element as HTMLInputElement).value).toBe("");
		expect(options("task-send-to").map((o) => o.value)).toEqual([
			"",
			"channel:t1",
		]);
		expect(w().text()).toContain("A scheduled task for bob.");
		await setName("daily");
		await submit();
		expect(w().find("[role=alert]").text()).toBe(
			"A task named daily already exists.",
		);
		expect(saveAgentTasks).not.toHaveBeenCalled();
	});

	it("requires an agent", async () => {
		settings.config = config([]);
		wrapper = mount(AgentItemDialogs, {
			global: { plugins: [router] },
			attachTo: document.body,
		});
		await open({ kind: "task", agent: "", mode: "create" });
		expect(options("agent-task-agent")).toEqual([]);
		await setName("x");
		await submit();
		expect(w().find("[role=alert]").text()).toBe("Choose an agent.");
	});

	it("switches to a project task on a watched project", async () => {
		await mountDialogs();
		await open({ kind: "task", agent: "alice", mode: "create" });
		await byId("seg-Type-script").trigger("click");
		await byId("seg-Target-project").trigger("click");
		await flushPromises();

		expect(w().find("h2").text()).toBe("Add project scheduled task");
		expect(byId("seg-Type").attributes("data-value")).toBe("prompt");
		expect(byId("seg-Type-script").attributes("disabled")).toBeDefined();
		expect(byId("trigger").attributes("value")).toBe("daily");
		expect(byId("trigger").attributes("data-optional")).toBe("false");
		expect(byId("agent-task-agent").exists()).toBe(false);
		expect(options("task-project").map((o) => o.value)).toEqual([
			"acme/web",
			"bob/api",
		]);
		expect((byId("task-project").element as HTMLInputElement).value).toBe(
			"acme/web",
		);
		expect(w().text()).toContain("Runs in an isolated clone of the project");
		expect(w().text()).toContain(
			"A scheduled task that runs its prompt against acme/web.",
		);
		expect(byId("prompt-section").attributes("data-type")).toBe("project");

		// The project's owner runs the task.
		await byId("task-project").setValue("bob/api");
		await flushPromises();
		expect(byId("prompt-section").attributes("data-agent")).toBe("bob");
		await setName("api-check");
		await byId("prompt").setValue("Check the API");
		await submit();
		expect(saveAgentTasks.mock.calls[0]?.[0]).toBe("bob");
		expect(tasksOf("bob").find((t) => t.name === "api-check")).toMatchObject({
			type: "project",
			project: "bob/api",
			trigger: "daily",
			precompile: true,
		});
	});

	it("requires a project for a project task", async () => {
		settings.config = config([{ name: "solo", tasks: [] }]);
		wrapper = mount(AgentItemDialogs, {
			global: { plugins: [router] },
			attachTo: document.body,
		});
		await open({ kind: "task", agent: "solo", mode: "create" });
		await byId("seg-Target-project").trigger("click");
		await flushPromises();
		expect(w().text()).toContain("No watched projects yet.");
		expect(w().text()).toContain("against a watched project.");
		await setName("x");
		await submit();
		expect(w().find("[role=alert]").text()).toBe("Choose a watched project.");
	});

	it("opens on a board's project with a daily trigger", async () => {
		await mountDialogs();
		await open({
			kind: "task",
			agent: "bob",
			mode: "create",
			project: "other/repo",
		});
		expect(w().find("h2").text()).toBe("Add project scheduled task");
		expect(byId("trigger").attributes("value")).toBe("daily");
		expect(options("task-project").map((o) => o.value)).toEqual([
			"other/repo",
			"acme/web",
			"bob/api",
		]);
		expect((byId("task-project").element as HTMLInputElement).value).toBe(
			"other/repo",
		);
		expect(byId("prompt-section").attributes("data-agent")).toBe("bob");
	});
});

describe("task edit", () => {
	it("saves nothing for an unchanged task and closes", async () => {
		await mountDialogs();
		await open({ kind: "task", agent: "alice", mode: "edit", name: "daily" });
		expect(w().find("h2").text()).toBe("Edit task");
		expect(w().text()).toContain(
			"Renames daily or changes its type, target, trigger and prompt.",
		);
		expect(byId("agent-task-agent").attributes("disabled")).toBeDefined();
		expect(
			(w().find("#agent-item-name").element as HTMLInputElement).value,
		).toBe("daily");
		expect(byId("prompt-section").attributes("data-saved")).toBe("daily");
		// An unset precompile opens off, as it runs.
		expect((byId("precompile").element as HTMLInputElement).checked).toBe(
			false,
		);
		await submit();
		expect(saveAgentTasks).not.toHaveBeenCalled();
		expect(renameTask).not.toHaveBeenCalled();
		expect(callTool).not.toHaveBeenCalled();
		expect(dialogs.dialog.value).toBeNull();
	});

	it("saves a changed prompt, renames the shown task and recompiles", async () => {
		await router.push({
			path: "/agents/alice/tasks",
			query: { task: "daily" },
		});
		const replace = vi.spyOn(router, "replace");
		await mountDialogs();
		await open({ kind: "task", agent: "alice", mode: "edit", name: "daily" });
		await byId("prompt").setValue("Summarize more");
		await byId("precompile").setValue(true);
		await setName("morning");
		await submit();

		expect(saveAgentTasks).toHaveBeenCalledTimes(1);
		expect(tasksOf("alice").find((t) => t.name === "daily")).toMatchObject({
			prompt: "Summarize more",
			precompile: true,
		});
		// Untouched tasks pass through.
		expect(tasksOf("alice").find((t) => t.name === "build")).toMatchObject({
			type: "script",
		});
		expect(renameTask).toHaveBeenCalledWith("alice", "daily", "morning");
		expect(replace).toHaveBeenCalledWith({
			path: "/agents/alice/tasks",
			query: { task: "morning" },
		});
		expect(callTool).toHaveBeenCalledWith("config_task_convert_to_script", {
			agent: "alice",
			task: "morning",
		});
	});

	it("logs a failed recompile without failing the save", async () => {
		const err = vi.spyOn(console, "error").mockImplementation(() => {});
		callTool.mockRejectedValue(new Error("down"));
		await mountDialogs();
		await open({ kind: "task", agent: "alice", mode: "edit", name: "nightly" });
		await byId("prompt").setValue("Triage again");
		await submit();
		expect(err).toHaveBeenCalledWith("precompute task", expect.any(Error));
		expect(dialogs.dialog.value).toBeNull();
	});

	it("renames a task that is not shown without navigating", async () => {
		const replace = vi.spyOn(router, "replace");
		await mountDialogs();
		await open({ kind: "task", agent: "alice", mode: "edit", name: "build" });
		expect(byId("prompt-section").attributes("data-type")).toBe("script");
		expect(w().text()).toContain("trigger and script.");
		// An edited script shows no precompile choice.
		expect(byId("precompile").exists()).toBe(false);
		await setName("compile");
		await submit();
		expect(renameTask).toHaveBeenCalledWith("alice", "build", "compile");
		expect(replace).not.toHaveBeenCalled();
	});

	it("rejects a rename onto another task", async () => {
		await mountDialogs();
		await open({ kind: "task", agent: "alice", mode: "edit", name: "build" });
		await setName("daily");
		await submit();
		expect(w().find("[role=alert]").text()).toBe(
			"A task named daily already exists.",
		);
	});

	it("reports a task deleted while editing", async () => {
		await mountDialogs();
		await open({ kind: "task", agent: "alice", mode: "edit", name: "daily" });
		const a = settings.config?.agents.find((x) => x.name === "alice");
		if (a) a.tasks = a.tasks?.filter((t) => t.name !== "daily");
		await submit();
		expect(w().find("[role=alert]").text()).toBe(
			"Task daily no longer exists.",
		);
	});

	it("edits a precompiled project task and widens for its compiled script", async () => {
		await mountDialogs();
		await open({ kind: "task", agent: "alice", mode: "edit", name: "nightly" });
		expect(byId("task-dialog").classes()).toContain("max-w-2xl");
		useCompiledPanelOpen().value = true;
		await nextTick();
		expect(byId("task-dialog").classes()).toContain("max-w-5xl");

		expect(byId("task-model-override").text()).toContain("big-model");
		expect((byId("precompile").element as HTMLInputElement).checked).toBe(true);
		expect(options("task-project").map((o) => o.value)).toEqual(["acme/web"]);
		expect(byId("trigger").attributes("value")).toBe("daily");

		// Leaving Project drops its project and model; precompile stays on, so
		// its compiled script is kept.
		await byId("seg-Target-agent").trigger("click");
		await submit();
		const saved = tasksOf("alice").find((t) => t.name === "nightly");
		expect(saved?.type).toBe("prompt");
		expect(saved).not.toHaveProperty("project");
		expect(saved).not.toHaveProperty("model");
		expect(saved?.compiled).toBe("x");
		expect(saved?.precompile).toBe(true);
	});

	it("turning precompile off drops the compiled script", async () => {
		await mountDialogs();
		await open({ kind: "task", agent: "alice", mode: "edit", name: "nightly" });
		await byId("precompile").setValue(false);
		await submit();
		const saved = tasksOf("alice").find((t) => t.name === "nightly");
		expect(saved).not.toHaveProperty("precompile");
		expect(saved).not.toHaveProperty("compiled");
		expect(saved?.project).toBe("acme/web");
		expect(callTool).not.toHaveBeenCalled();
	});

	it("keeps an unwatched project and an unknown Send To", async () => {
		await mountDialogs();
		await open({ kind: "task", agent: "alice", mode: "edit", name: "old" });
		expect(options("task-project").map((o) => o.value)).toEqual([
			"acme/web",
			"acme/gone",
		]);
		expect(options("task-send-to").map((o) => o.value)).toEqual([
			"",
			"channel:ops",
			"channel:removed",
		]);
	});

	it("resets precompile with the type", async () => {
		await mountDialogs();
		await open({ kind: "task", agent: "alice", mode: "edit", name: "daily" });
		await byId("seg-Type-script").trigger("click");
		await flushPromises();
		expect(byId("precompile").exists()).toBe(false);
		await byId("seg-Type-prompt").trigger("click");
		await flushPromises();
		// Back to the saved task's own setting, unset is off.
		expect((byId("precompile").element as HTMLInputElement).checked).toBe(
			false,
		);
	});
});

describe("task duplicate", () => {
	it("suggests the next free name and creates the copy", async () => {
		await mountDialogs();
		await open({
			kind: "task",
			agent: "alice",
			mode: "create",
			duplicate: "daily",
		});
		expect(w().find("h2").text()).toBe("Duplicate daily");
		expect(w().text()).toContain("A copy of alice/daily");
		expect(
			(w().find("#agent-item-name").element as HTMLInputElement).value,
		).toBe("daily-2");
		expect((byId("prompt").element as HTMLTextAreaElement).value).toBe(
			"Summarize",
		);
		await submit();
		expect(tasksOf("alice").find((t) => t.name === "daily-2")).toMatchObject({
			prompt: "Summarize",
		});
	});

	it("follows the destination agent until renamed", async () => {
		await mountDialogs();
		await open({
			kind: "task",
			agent: "alice",
			mode: "create",
			duplicate: "build",
		});
		expect(
			(w().find("#agent-item-name").element as HTMLInputElement).value,
		).toBe("build-2");
		await byId("agent-task-agent").setValue("bob");
		await flushPromises();
		expect(
			(w().find("#agent-item-name").element as HTMLInputElement).value,
		).toBe("build");
		await setName("mine");
		await byId("agent-task-agent").setValue("alice");
		await flushPromises();
		expect(
			(w().find("#agent-item-name").element as HTMLInputElement).value,
		).toBe("mine");
	});

	it("warns about the source repo and replaces it", async () => {
		await mountDialogs();
		await open({
			kind: "task",
			agent: "alice",
			mode: "create",
			duplicate: "nightly",
		});
		expect(byId("task-model-override").text()).toContain("big-model");
		expect(byId("task-duplicate-repo-warning").exists()).toBe(false);
		await byId("task-project").setValue("bob/api");
		await flushPromises();
		const warning = byId("task-duplicate-repo-warning");
		expect(warning.text()).toContain("acme/web");
		expect(warning.text()).toContain("references");
		expect(warning.text()).toContain("bob/api");
		await byId("task-duplicate-replace").trigger("click");
		expect((byId("prompt").element as HTMLTextAreaElement).value).toBe(
			"Triage bob/api issues in ~/src/api",
		);
		expect(byId("task-duplicate-repo-warning").exists()).toBe(false);

		await setName("api-nightly");
		await submit();
		expect(tasksOf("bob").find((t) => t.name === "api-nightly")).toMatchObject({
			model: "big-model",
			project: "bob/api",
			enabled: true,
		});
	});

	it("keeps the mentions for one project and asks again for another", async () => {
		settings.config = config([
			{
				name: "alice",
				projects: [{ target: "acme/web" }, { target: "acme/app" }],
				tasks: [
					{
						name: "n",
						type: "project",
						project: "acme/web",
						prompt: "See acme/web",
					},
				],
			},
			{ name: "bob", projects: [{ target: "bob/api" }], tasks: [] },
		]);
		wrapper = mount(AgentItemDialogs, {
			global: { plugins: [router] },
			attachTo: document.body,
		});
		await open({
			kind: "task",
			agent: "alice",
			mode: "create",
			duplicate: "n",
		});
		await byId("task-project").setValue("acme/app");
		await flushPromises();
		expect(byId("task-duplicate-repo-warning").text()).toContain("1 reference");
		await byId("task-duplicate-keep").trigger("click");
		expect(byId("task-duplicate-repo-warning").exists()).toBe(false);
		await byId("task-project").setValue("bob/api");
		await flushPromises();
		expect(byId("task-duplicate-repo-warning").exists()).toBe(true);
		// The same project in another case is not another project.
		await byId("task-project").setValue("ACME/WEB");
		await flushPromises();
		expect(byId("task-duplicate-repo-warning").exists()).toBe(false);
	});
});

describe("task delete", () => {
	it("deletes a shown task with its data", async () => {
		await router.push({
			path: "/agents/alice/tasks",
			query: { task: "build" },
		});
		const replace = vi.spyOn(router, "replace");
		await mountDialogs();
		await open({ kind: "task", agent: "alice", mode: "delete", name: "build" });
		const confirm = byId("delete-item-dialog");
		expect(confirm.find("h2").text()).toBe("Delete task?");
		expect(byId("task-delete-data").attributes("data-name")).toBe(
			"alice/build",
		);
		expect((byId("task-delete-data").element as HTMLInputElement).checked).toBe(
			true,
		);
		await byId("confirm").trigger("click");
		await flushPromises();
		expect(saveAgentTasks).toHaveBeenCalledWith("alice", expect.any(Function), [
			{ kind: "task", agent: "alice", name: "build" },
		]);
		expect(tasksOf("alice").map((t) => t.name)).not.toContain("build");
		expect(replace).toHaveBeenCalledWith({ path: "/agents/alice/tasks" });
		expect(dialogs.dialog.value).toBeNull();
	});

	it("keeps the task's data when unchecked", async () => {
		const replace = vi.spyOn(router, "replace");
		await mountDialogs();
		await open({ kind: "task", agent: "alice", mode: "delete", name: "build" });
		await byId("task-delete-data").setValue(false);
		await byId("confirm").trigger("click");
		await flushPromises();
		expect(saveAgentTasks.mock.calls[0]?.[2]).toEqual([]);
		expect(replace).not.toHaveBeenCalled();

		// The next dialog deletes data again by default.
		await open({ kind: "task", agent: "alice", mode: "delete", name: "daily" });
		expect((byId("task-delete-data").element as HTMLInputElement).checked).toBe(
			true,
		);
	});

	it("shows a failed delete", async () => {
		saveAgentTasks.mockRejectedValueOnce(new Error("save failed"));
		await mountDialogs();
		await open({ kind: "task", agent: "alice", mode: "delete", name: "build" });
		await byId("confirm").trigger("click");
		await flushPromises();
		expect(byId("delete-item-dialog").find("[role=alert]").text()).toBe(
			"save failed",
		);
		expect(dialogs.dialog.value).not.toBeNull();
	});
});
