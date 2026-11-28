import { afterEach, describe, expect, test } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { currentURL, goto, renderApp } from "../component-test/app";
import { mockMCP } from "../component-test/server";
import { useAgentItemDialogs } from "../composables/useAgentItemDialogs";
import { chooseOption, expectValue, optionLabels } from "./helpers/select";
import {
	expectSavedTrigger,
	expectTrigger,
	fillTrigger,
	TRIGGER_CASES,
} from "./helpers/triggerEditor";

// The "on a touch screen" scenario stays in web/e2e/tasks-files-sidebar.spec.ts:
// it needs a touch browser context, where (hover: none) matches, which a
// component test cannot emulate.

// The sidebar's add/edit dialog state is module state, which outlives a
// test's app; close it after each test, as a new page starts with it closed.
afterEach(() => {
	useAgentItemDialogs().close();
});

interface Task {
	name: string;
	schedule?: string;
	prompt?: string;
	[key: string]: unknown;
}

interface Config {
	agents: Array<{
		name: string;
		model: string;
		tasks: Task[];
		channels?: Array<{ type: string; id: string; name?: string }>;
	}>;
	[key: string]: unknown;
}

type Args = Record<string, unknown>;

/**
 * Mocks one agent with `tasks` and root `files`, keeping both as the UI
 * saves, renames and deletes them.
 */
function mockAgent(
	tasks: Task[],
	files: Record<string, string>,
	extra: Record<string, unknown> = {},
) {
	let config: Config = {
		agents: [
			{
				name: "assistant",
				model: "x/y",
				tasks,
				channels: [{ type: "slack", id: "standup", name: "Team standup" }],
			},
		],
		...extra,
	};
	const saved: Config[] = [];
	const deletedData: unknown[] = [];
	const converted: Args[] = [];
	const previews: Args[] = [];
	const taskSessions: Args[] = [];
	const store = new Map(Object.entries(files));
	mockMCP({
		agent_list: [
			{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
		],
		config_get: () => config,
		config_save: (args?: Args) => {
			config = JSON.parse(String(args?.config ?? "{}")) as Config;
			saved.push(config);
			deletedData.push(args?.delete_data);
			return "ok";
		},
		config_task_rename: (args?: Args) => {
			for (const t of config.agents[0].tasks)
				if (t.name === args?.task) t.name = String(args?.new_name);
			return "renamed";
		},
		config_task_convert_to_script: (args?: Args) => {
			converted.push(args ?? {});
			return "task_compile_1";
		},
		config_validate: [],
		task_preview: (args?: Args) => {
			previews.push(args ?? {});
			const at = (h: number) =>
				new Date(Date.now() + h * 3_600_000).toISOString();
			return { description: "preview", next: [at(1), at(2), at(3)] };
		},
		task_session: (args?: Args) => {
			taskSessions.push(args ?? {});
			const name = String(args?.name);
			return { id: name, agent_id: "assistant", name, title: name };
		},
		session_messages: (args?: Args) =>
			args?.session_id === "main"
				? []
				: [
						{
							id: "m1",
							role: "assistant",
							content: `${String(args?.session_id)}: all green`,
							timestamp: new Date().toISOString(),
						},
					],
		delete_preview: { sessions: 1, messages: 34, runs: 12, files: 3 },
		job_list: [],
		session_list: [
			{
				id: "main",
				agent_id: "assistant",
				name: "main",
				created_at: new Date().toISOString(),
			},
		],
		auth_list: [],
		tool_list: [],
		agent_file_list: () => Array.from(store.keys()).sort(),
		agent_file_read: (args?: Args) => store.get(String(args?.file ?? "")) ?? "",
		agent_file_write: (args?: Args) => {
			store.set(String(args?.file ?? ""), String(args?.content ?? ""));
			return "ok";
		},
		agent_file_delete: (args?: Args) => {
			store.delete(String(args?.file ?? ""));
			return "ok";
		},
		agent_file_rename: (args?: Args) => {
			const from = String(args?.file ?? "");
			store.set(String(args?.to ?? ""), store.get(from) ?? "");
			store.delete(from);
			return "ok";
		},
	});
	return { saved, deletedData, store, converted, previews, taskSessions };
}

const FILES = {
	"AGENTS.md": "# Agents",
	"IDENTITY.md": "# Identity",
	"MEMORY.md": "Remembered",
};

function sidebarRow(key: "tasks" | "files") {
	return page
		.getByTestId("sidebar-sole-agent")
		.getByTestId(`sidebar-row-${key}`);
}

function taskRows() {
	return page.getByTestId("sidebar-tasks").getByTestId("task-row");
}

/** The open task's name in its settings editor. */
function taskNameField() {
	return page.locator("main").locator('input[placeholder="daily-briefing"]');
}

function fileRows() {
	return page.getByTestId("sidebar-files").getByTestId("file-row");
}

const count = (loc: Locator) => () => loc.elements().length;

/** Each matched element's text, whitespace collapsed, equals texts, as
 * Playwright's toHaveText with an array. */
async function expectTexts(loc: Locator, texts: string[]) {
	await expect
		.poll(() =>
			loc
				.elements()
				.map((e) => (e.textContent ?? "").replace(/\s+/g, " ").trim()),
		)
		.toEqual(texts);
}

/** Checks a checkbox, as Playwright's check. */
async function check(loc: Locator) {
	await expect.element(loc).not.toBeChecked();
	await loc.click();
	await expect.element(loc).toBeChecked();
}

/** Unchecks a checkbox, as Playwright's uncheck. */
async function uncheck(loc: Locator) {
	await expect.element(loc).toBeChecked();
	await loc.click();
	await expect.element(loc).not.toBeChecked();
}

/** A touch tap, as Playwright's locator.tap with hasTouch. */
async function tap(loc: Locator) {
	await expect.element(loc).toBeVisible();
	const el = loc.element() as HTMLElement;
	const r = el.getBoundingClientRect();
	const init: PointerEventInit = {
		bubbles: true,
		cancelable: true,
		composed: true,
		pointerType: "touch",
		isPrimary: true,
		pointerId: 1,
		clientX: r.left + r.width / 2,
		clientY: r.top + r.height / 2,
	};
	el.dispatchEvent(new PointerEvent("pointerdown", init));
	el.dispatchEvent(new PointerEvent("pointerup", init));
	el.click();
}

async function openAdd(kind: "tasks" | "files") {
	await sidebarRow(kind).hover();
	await sidebarRow(kind)
		.getByRole("button", { name: kind === "tasks" ? "Add task" : "Add file" })
		.click();
}

test("the Tasks + opens the add-task modal and the new task joins the sidebar", async () => {
	const { saved, converted } = mockAgent(
		[{ name: "daily-briefing", schedule: "0 9 * * *", prompt: "Brief" }],
		FILES,
	);
	await renderApp("/agents/assistant/tasks");
	await expectTexts(taskRows(), ["daily-briefing"]);
	// No inline add-task input remains on the page.
	await expect.poll(count(page.getByPlaceholder("task-name"))).toBe(0);
	// Precompute is chosen per task in the dialog, not on the page.
	await expect.poll(count(page.getByText(/Try to precompile/))).toBe(0);

	const add = sidebarRow("tasks").getByRole("button", {
		name: "Add task",
	});
	await expect.element(add).toHaveAttribute("title", "Add task");
	await sidebarRow("tasks").hover();
	await add.click();

	const dialog = page.getByRole("dialog", { name: "Add task" });
	await expect.element(dialog).toBeVisible();
	await dialog.getByLabelText("Task name").fill("weekly-report");
	await dialog.getByRole("radio", { name: "Cron" }).click();
	await dialog.getByLabelText("Cron expression").fill("0 8 * * 1");
	await dialog.getByLabelText("Prompt").fill("Write the weekly report");
	// scheduler.precompute_tasks defaults on, so the new task is precomputed.
	await expect
		.element(dialog.getByLabelText("Try to precompile"))
		.toBeChecked();
	await dialog.getByRole("button", { name: "Create" }).click();

	await expect.element(dialog).not.toBeInTheDocument();
	await expectTexts(taskRows(), ["daily-briefing", "weekly-report"]);
	expect(converted).toEqual([{ agent: "assistant", task: "weekly-report" }]);
	await expect
		.poll(currentURL)
		.toMatch(/\/agents\/assistant\/tasks\?task=weekly-report$/);
	await expect
		.element(page.locator('input[placeholder="daily-briefing"]'))
		.toHaveValue("weekly-report");
	expect(saved.at(-1)?.agents[0].tasks).toMatchObject([
		{ name: "daily-briefing" },
		{
			name: "weekly-report",
			schedule: "0 8 * * 1",
			prompt: "Write the weekly report",
			from_file: true,
			// It keeps its prompt and compiles a script beside it.
			type: "prompt",
			precompile: true,
		},
	]);
});

test("the add-task modal sets every trigger type, timezone and policy, and they round-trip", async () => {
	const { saved, previews } = mockAgent([], FILES);
	await renderApp("/agents/assistant/tasks");

	for (const c of TRIGGER_CASES) {
		const name = `task-${c.label.toLowerCase()}`;
		await openAdd("tasks");
		const dialog = page.getByRole("dialog", { name: "Add task" });
		await dialog.getByLabelText("Task name").fill(name);
		// The add modal is the shared trigger editor, not a raw cron field.
		expect(dialog.getByLabelText("Schedule").elements()).toHaveLength(0);
		await fillTrigger(dialog, c);
		// The preview follows the edited trigger before it is saved.
		await expect
			.poll(() => previews.at(-1))
			.toMatchObject({ timezone: "America/Los_Angeles", ...c.saved });
		await dialog.getByLabelText("Prompt").fill(`Run ${name}`);
		await dialog.getByRole("button", { name: "Create" }).click();
		await expect.element(dialog).not.toBeInTheDocument();

		const task = saved.at(-1)?.agents[0].tasks.find((t) => t.name === name) as
			| Record<string, unknown>
			| undefined;
		expectSavedTrigger(task, c);

		// The task's own page edits it with the same trigger editor.
		await expect.poll(currentURL).toMatch(new RegExp(`\\?task=${name}$`));
		await expectTrigger(page.locator("main"), c);

		// Reopening it in the Tasks overview's settings modal keeps every field.
		await goto("/agents/assistant/tasks");
		await page
			.getByRole("group", { name: `${name} actions` })
			.getByRole("button", { name: "Task settings" })
			.click();
		const edit = page.getByRole("dialog", { name: "Edit task" });
		await expectTrigger(edit, c);
		await edit.getByRole("button", { name: "Cancel" }).click();
		await expect.element(edit).not.toBeInTheDocument();
	}
});

test("the add-task modal's precompute choice follows the setting and applies to that task", async () => {
	const { saved, converted } = mockAgent([], FILES, {
		scheduler: { precompute_tasks: false },
	});
	await renderApp("/agents/assistant/tasks");
	await openAdd("tasks");

	const dialog = page.getByRole("dialog", { name: "Add task" });
	const precompute = dialog.getByLabelText("Try to precompile");
	await expect.element(precompute).not.toBeChecked();
	await dialog.getByLabelText("Task name").fill("stays-prompt");
	await dialog.getByRole("radio", { name: "Cron" }).click();
	await dialog.getByLabelText("Cron expression").fill("0 8 * * 1");
	await dialog.getByLabelText("Prompt").fill("Summarize the week");
	await dialog.getByRole("button", { name: "Create" }).click();
	await expect.element(dialog).not.toBeInTheDocument();
	expect(saved.at(-1)?.agents[0].tasks).toMatchObject([
		{ name: "stays-prompt", type: "prompt" },
	]);
	expect(saved.at(-1)?.agents[0].tasks[0]).not.toHaveProperty("precompile");
	expect(converted).toEqual([]);

	// Checking it precomputes just the task being created.
	await openAdd("tasks");
	await expect.element(precompute).not.toBeChecked();
	await dialog.getByLabelText("Task name").fill("compiled");
	await dialog.getByRole("radio", { name: "Cron" }).click();
	await dialog.getByLabelText("Cron expression").fill("*/5 * * * *");
	await dialog.getByLabelText("Prompt").fill("Print the time");
	await check(precompute);
	await dialog.getByRole("button", { name: "Create" }).click();
	await expect.element(dialog).not.toBeInTheDocument();
	expect(saved.at(-1)?.agents[0].tasks).toMatchObject([
		{ name: "stays-prompt" },
		{
			name: "compiled",
			type: "prompt",
			prompt: "Print the time",
			precompile: true,
		},
	]);
	expect(converted).toEqual([{ agent: "assistant", task: "compiled" }]);

	// Editing a scheduled prompt task offers the same choice, from its own
	// setting, and turning it on precompiles that task beside its prompt.
	await goto("/agents/assistant/tasks");
	await page
		.getByRole("group", { name: "stays-prompt actions" })
		.getByRole("button", { name: "Task settings" })
		.click();
	const edit = page.getByRole("dialog", { name: "Edit task" });
	const editPrecompute = edit.getByLabelText("Try to precompile");
	await expect.element(editPrecompute).not.toBeChecked();
	await check(editPrecompute);
	await edit.getByRole("button", { name: "Save" }).click();
	await expect.element(edit).not.toBeInTheDocument();
	expect(
		saved.at(-1)?.agents[0].tasks.find((t) => t.name === "stays-prompt"),
	).toMatchObject({
		type: "prompt",
		prompt: "Summarize the week",
		precompile: true,
	});
	expect(converted.at(-1)).toEqual({
		agent: "assistant",
		task: "stays-prompt",
	});
});

test("the add-task modal's Send To forwards runs to a channel by name, shown as Send Via in the editor", async () => {
	const { saved } = mockAgent([], FILES);
	await renderApp("/agents/assistant/tasks");
	await openAdd("tasks");

	const dialog = page.getByRole("dialog", { name: "Add task" });
	const sendTo = dialog.getByLabelText("Send To");
	// The agent's channels are offered by name after the default.
	await expectValue(sendTo, "");
	expect(await optionLabels(sendTo)).toEqual([
		"Task session only",
		"Team standup · slack",
	]);
	await dialog.getByLabelText("Task name").fill("standup-digest");
	await dialog.getByLabelText("Prompt").fill("Summarize yesterday");
	await chooseOption(sendTo, "Team standup · slack");
	await dialog.getByRole("button", { name: "Create" }).click();
	await expect.element(dialog).not.toBeInTheDocument();
	expect(saved.at(-1)?.agents[0].tasks).toMatchObject([
		{ name: "standup-digest", target: "channel:Team standup" },
	]);

	// The task's editor shows the same delivery in Send Via.
	await expect.poll(currentURL).toMatch(/\?task=standup-digest$/);
	const main = page.locator("main");
	await expectValue(
		main.getByRole("combobox", { name: "Send Via" }),
		"channel:Team standup",
	);

	// Left at the default, a new task forwards nowhere, as before.
	await openAdd("tasks");
	await expectValue(sendTo, "");
	await dialog.getByLabelText("Task name").fill("quiet");
	await dialog.getByRole("button", { name: "Create" }).click();
	await expect.element(dialog).not.toBeInTheDocument();
	expect(
		saved.at(-1)?.agents[0].tasks.find((t) => t.name === "quiet"),
	).toMatchObject({ target: "" });
});

test("the Files + creates a file that joins the sidebar, and the page has no file tabs", async () => {
	const { store } = mockAgent([], FILES);
	await renderApp("/agents/assistant/files");
	await expectTexts(fileRows(), ["AGENTS.md", "IDENTITY.md", "MEMORY.md"]);

	// The page only edits the open file: no in-page file list or add input.
	const main = page.locator("main");
	expect(
		main.getByRole("button", { name: "IDENTITY.md" }).elements(),
	).toHaveLength(0);
	expect(main.getByPlaceholder("IDENTITY.md").elements()).toHaveLength(0);
	expect(
		main.getByRole("button", { name: "+", exact: true }).elements(),
	).toHaveLength(0);

	await openAdd("files");
	const dialog = page.getByRole("dialog", { name: "Add file" });
	await dialog.getByLabelText("File name").fill("PROFILE");
	await dialog.getByRole("button", { name: "Create" }).click();

	await expect.element(dialog).not.toBeInTheDocument();
	await expectTexts(fileRows(), [
		"AGENTS.md",
		"IDENTITY.md",
		"MEMORY.md",
		"PROFILE.md",
	]);
	await expect
		.poll(currentURL)
		.toMatch(/\/agents\/assistant\/files\?file=PROFILE\.md$/);
	await expect
		.element(page.getByTestId("settings-title"))
		.toHaveText("PROFILE.md");
	expect(store.has("PROFILE.md")).toBe(true);
});

test("Scheduled Tasks lists its tasks on every page and a row opens the task's session", async () => {
	const { taskSessions } = mockAgent(
		[
			{ name: "daily-briefing", schedule: "0 9 * * *", prompt: "Brief" },
			{ name: "cleanup", schedule: "0 0 * * *", prompt: "Clean" },
		],
		FILES,
	);
	await renderApp("/agents/assistant/chat");
	await expectTexts(taskRows(), ["daily-briefing", "cleanup"]);

	const cleanup = taskRows().filter({ hasText: "cleanup" });
	await cleanup.getByTestId("sidebar-item-link").click();
	await expect.poll(currentURL).toMatch(/\/agents\/assistant\/tasks\/cleanup$/);
	await expect
		.element(page.getByTestId("settings-title"))
		.toHaveText("cleanup");
	await expect.element(page.getByText("cleanup: all green")).toBeVisible();
	expect(taskSessions.at(-1)).toEqual({ agent: "assistant", name: "cleanup" });
	await expect.element(cleanup).toHaveClass(/(^|\s)bg-gray-100/);
	await expect
		.element(taskRows().filter({ hasText: "daily-briefing" }))
		.not.toHaveClass(/(^|\s)bg-gray-100/);

	// The gear opens the task's settings, where the row stays active.
	await page.getByTestId("task-settings-link").click();
	await expect
		.poll(currentURL)
		.toMatch(/\/agents\/assistant\/tasks\?task=cleanup$/);
	await expect.element(taskNameField().first()).toHaveValue("cleanup");
	await expectTexts(taskRows(), ["daily-briefing", "cleanup"]);
	await expect.element(cleanup).toHaveClass(/(^|\s)bg-gray-100/);
});

test("a renamed task's session takes the new name", async () => {
	mockAgent(
		[{ name: "cleanup", schedule: "0 0 * * *", prompt: "Clean" }],
		FILES,
	);
	await renderApp("/agents/assistant/tasks?task=cleanup");
	const name = taskNameField().first();
	await name.fill("nightly-cleanup");
	(name.element() as HTMLElement).blur();
	await expectTexts(taskRows(), ["nightly-cleanup"]);

	await taskRows().getByTestId("sidebar-item-link").click();
	await expect
		.poll(currentURL)
		.toMatch(/\/agents\/assistant\/tasks\/nightly-cleanup$/);
	await expect
		.element(page.getByTestId("settings-title"))
		.toHaveText("nightly-cleanup");
	await expect
		.element(page.getByText("nightly-cleanup: all green"))
		.toBeVisible();
});

test("the … menu opens a task's settings and deletes tasks", async () => {
	const { saved, deletedData } = mockAgent(
		[
			{ name: "daily-briefing", schedule: "0 9 * * *", prompt: "Brief" },
			{ name: "cleanup", schedule: "0 0 * * *", prompt: "Clean" },
		],
		FILES,
	);
	await renderApp("/agents/assistant/tasks/daily-briefing");

	const cleanup = taskRows().filter({ hasText: "cleanup" });
	await cleanup.hover();
	await cleanup.getByTestId("task-menu-button").click();
	await expectTexts(page.getByRole("menuitem"), [
		"Pause",
		"Settings",
		"Duplicate…",
		"Delete",
	]);
	await page.getByRole("menuitem", { name: "Settings" }).click();

	await expect
		.poll(currentURL)
		.toMatch(/\/agents\/assistant\/tasks\?task=cleanup$/);
	await expect.poll(count(page.getByRole("dialog"))).toBe(0);
	await expect.element(taskNameField().first()).toHaveValue("cleanup");

	await cleanup.hover();
	await cleanup.getByTestId("task-menu-button").click();
	await page.getByRole("menuitem", { name: "Delete" }).click();
	const confirm = page.getByRole("alertdialog", { name: "Delete task?" });
	await expect.element(confirm).toMatchTextContent("cleanup");
	// Deleting the task's data is offered, checked, with what it removes.
	await expect
		.element(page.getByTestId("task-delete-data-checkbox"))
		.toBeChecked();
	await expect
		.element(page.getByTestId("task-delete-data-summary"))
		.toHaveText(
			"Deletes 1 session, 34 messages, 12 runs, 3 files. This can't be undone.",
		);
	await confirm.getByRole("button", { name: "Delete", exact: true }).click();

	await expectTexts(taskRows(), ["daily-briefing"]);
	await expect.poll(currentURL).toMatch(/\/agents\/assistant\/tasks$/);
	expect(saved.at(-1)?.agents[0].tasks.map((t) => t.name)).toEqual([
		"daily-briefing",
	]);
	expect(deletedData.at(-1)).toEqual([
		{ kind: "task", agent: "assistant", name: "cleanup" },
	]);
});

test("the task editor's Danger Zone removes the open task", async () => {
	const { saved, deletedData } = mockAgent(
		[
			{ name: "daily-briefing", schedule: "0 9 * * *", prompt: "Brief" },
			{
				name: "cleanup",
				schedule: "0 0 * * *",
				prompt: "Clean",
				from_file: true,
				file: "tasks/cleanup.md",
			},
		],
		FILES,
	);
	await renderApp("/agents/assistant/tasks?task=cleanup");

	const dangerZone = page.getByTestId("task-danger-zone");
	await expect
		.element(dangerZone.getByRole("heading", { name: "Danger Zone" }))
		.toBeVisible();
	await expect
		.element(dangerZone)
		.toMatchTextContent(
			"Remove this scheduled task from the agent's configuration, including its definition file tasks/cleanup.md.",
		);
	await dangerZone.getByRole("button", { name: "Remove Task" }).click();

	const confirm = page.getByRole("alertdialog", { name: "Delete task?" });
	await expect.element(confirm).toMatchTextContent("cleanup");
	await confirm.getByRole("button", { name: "Cancel" }).click();
	await expect.element(confirm).not.toBeInTheDocument();
	await expectTexts(taskRows(), ["daily-briefing", "cleanup"]);

	await dangerZone.getByRole("button", { name: "Remove Task" }).click();
	// Unchecked keeps the task's session and runs; the session becomes a chat.
	await uncheck(page.getByTestId("task-delete-data-checkbox"));
	await expect
		.element(page.getByTestId("task-delete-data-summary"))
		.toMatchTextContent("Its sessions become regular chats");
	await confirm.getByRole("button", { name: "Delete", exact: true }).click();

	await expect.element(confirm).not.toBeInTheDocument();
	await expectTexts(taskRows(), ["daily-briefing"]);
	await expect.poll(currentURL).toMatch(/\/agents\/assistant\/tasks$/);
	expect(saved.at(-1)?.agents[0].tasks.map((t) => t.name)).toEqual([
		"daily-briefing",
	]);
	expect(deletedData.at(-1)).toBeUndefined();
});

test("the … menu renames and deletes files, and locks built-ins", async () => {
	const { store } = mockAgent([], FILES);
	await renderApp("/agents/assistant/files?file=IDENTITY.md");

	const identity = fileRows().filter({ hasText: "IDENTITY.md" });
	await identity.hover();
	await identity.getByTestId("file-menu-button").click();
	await page.getByRole("menuitem", { name: "Edit" }).click();
	const dialog = page.getByRole("dialog", { name: "Rename file" });
	await expect
		.element(dialog.getByLabelText("File name"))
		.toHaveValue("IDENTITY.md");
	await dialog.getByLabelText("File name").fill("PERSONA.md");
	await dialog.getByRole("button", { name: "Save" }).click();

	await expect.element(dialog).not.toBeInTheDocument();
	await expectTexts(fileRows(), ["AGENTS.md", "MEMORY.md", "PERSONA.md"]);
	await expect.poll(currentURL).toMatch(/\?file=PERSONA\.md$/);
	expect(store.get("PERSONA.md")).toBe("# Identity");

	const persona = fileRows().filter({ hasText: "PERSONA.md" });
	await persona.hover();
	await persona.getByTestId("file-menu-button").click();
	await page.getByRole("menuitem", { name: "Delete" }).click();
	await page
		.getByRole("alertdialog", { name: "Delete file?" })
		.getByRole("button", { name: "Delete" })
		.click();
	await expectTexts(fileRows(), ["AGENTS.md", "MEMORY.md"]);
	expect(store.has("PERSONA.md")).toBe(false);

	// Built-in files cannot be renamed or deleted.
	const agents = fileRows().filter({ hasText: "AGENTS.md" });
	await agents.hover();
	await agents.getByTestId("file-menu-button").click();
	await expect
		.element(page.getByRole("menuitem", { name: "Edit" }))
		.toHaveAttribute("data-disabled", "");
	await expect
		.element(page.getByRole("menuitem", { name: "Delete" }))
		.toHaveAttribute("data-disabled", "");
});

test("long Tasks and Files lists show five items plus More", async () => {
	const tasks = Array.from({ length: 7 }, (_, i) => ({
		name: `task-${i + 1}`,
	}));
	const files = Object.fromEntries(
		Array.from({ length: 7 }, (_, i) => [`DOC${i + 1}.md`, ""]),
	);
	mockAgent(tasks, files);

	await renderApp("/agents/assistant/tasks");
	await expect.poll(count(taskRows())).toBe(5);
	await page.getByTestId("sidebar-tasks-more").click();
	await expect.poll(count(taskRows())).toBe(7);

	await goto("/agents/assistant/files");
	await expect.poll(count(fileRows())).toBe(5);
	await page.getByTestId("sidebar-files-more").click();
	await expect.poll(count(fileRows())).toBe(7);
});

test("Chats, Tasks and Files share one sidebar row and … menu", async () => {
	mockAgent([{ name: "daily-briefing" }], FILES);
	await renderApp("/agents/assistant/chat");
	for (const [path, list, row] of [
		["chat", "sidebar-chats", "chat-row"],
		["tasks", "sidebar-tasks", "task-row"],
		["files", "sidebar-files", "file-row"],
	] as const) {
		await goto(`/agents/assistant/${path}`);
		const item = page.getByTestId(list).getByTestId(row).first();
		await expect.element(item.getByTestId("sidebar-item-link")).toBeVisible();
		await expect
			.poll(count(item.getByRole("button", { name: /^More options for / })))
			.toBe(1);
	}
});

test("+ and … show on hover and focus on desktop", async () => {
	mockAgent([{ name: "daily-briefing" }], FILES);
	await renderApp("/agents/assistant/tasks");
	await commands.mouseMove(800, 400);

	const add = sidebarRow("tasks").getByRole("button", {
		name: "Add task",
	});
	const menu = taskRows().first().getByTestId("task-menu-button");
	await expect.element(add).toHaveCSS("opacity", "0");
	await expect.element(menu).toHaveCSS("opacity", "0");

	await sidebarRow("tasks").hover();
	await expect.element(add).toHaveCSS("opacity", "1");
	await taskRows().first().hover();
	await expect.element(menu).toHaveCSS("opacity", "1");
	await expect.element(add).toHaveCSS("opacity", "0");

	// Keyboard focus reveals them too.
	await commands.mouseMove(800, 400);
	(add.element() as HTMLElement).focus();
	await expect.element(add).toHaveCSS("opacity", "1");
	(menu.element() as HTMLElement).focus();
	await expect.element(menu).toHaveCSS("opacity", "1");
});

// The spec ran these in a touch phone context and tapped; a component test
// can't turn on touch, so they run at phone width and tap with touch
// pointer events.
describe("on a phone", () => {
	test("the second row lists files and adds one", async () => {
		await page.viewport(390, 844);
		mockAgent([], FILES);
		await renderApp("/agents/assistant/files");
		await tap(page.getByRole("link", { name: "IDENTITY.md" }));
		await expect.poll(currentURL).toMatch(/\?file=IDENTITY\.md$/);
		await expect
			.element(page.getByTestId("settings-title"))
			.toHaveText("IDENTITY.md");

		await tap(page.getByTestId("mobile-row-action"));
		await expect
			.element(page.getByRole("dialog", { name: "Add file" }))
			.toBeVisible();
	});

	for (const width of [360, 390, 430]) {
		test(`the second row pins + right of its scrolling tabs at ${width}px (#1403)`, async () => {
			await page.viewport(width, 844);
			const names = [
				"aviary-find-dupe-tests",
				"aviary-release",
				"aviary-update-docs",
				"improve-coverage",
				"weekly-report",
			];
			mockAgent(
				names.map((name) => ({ name, schedule: "0 0 * * *", prompt: "x" })),
				FILES,
			);
			await renderApp("/agents/assistant/tasks?task=weekly-report");
			const strip = page.getByTestId("mobile-sub-nav");
			const add = page.getByTestId("mobile-row-action");
			await expect.element(add).toBeVisible();
			await expect.element(add).toHaveCSS("opacity", "1");

			// The + sits outside the strip, flush with the row's right edge.
			const row = page.getByTestId("mobile-sub-nav-row");
			const rowBox = row.element().getBoundingClientRect();
			const addBox = add.element().getBoundingClientRect();
			const stripBox = strip.element().getBoundingClientRect();
			expect(
				Math.abs(addBox.x + addBox.width - (rowBox.x + rowBox.width)),
			).toBeLessThan(1);
			expect(stripBox.x + stripBox.width).toBeLessThanOrEqual(addBox.x);
			expect(strip.getByRole("button").elements()).toHaveLength(0);

			// The strip overflows, shows no scrollbar, and the active (last)
			// tab is scrolled into view, so the left edge fades.
			const metrics = () => {
				const el = strip.element();
				const style = getComputedStyle(el);
				return {
					overflow: el.scrollWidth > el.clientWidth,
					scrollLeft: el.scrollLeft,
					scrollbar: style.scrollbarWidth,
					mask: style.maskImage || style.getPropertyValue("-webkit-mask-image"),
				};
			};
			await expect.poll(() => metrics().scrollLeft).toBeGreaterThan(0);
			let m = metrics();
			expect(m.overflow).toBe(true);
			expect(m.scrollbar).toBe("none");
			expect(m.mask).toMatch(
				/^linear-gradient\(to right, (rgba\(0, 0, 0, 0\)|transparent)/,
			);
			const active = strip.locator("[data-active]");
			await expect.element(active).toHaveText("weekly-report");
			const activeBox = active.element().getBoundingClientRect();
			expect(activeBox.x + activeBox.width).toBeLessThanOrEqual(
				stripBox.x + stripBox.width + 1,
			);

			// Scrolled back to the start, the right edge fades before the +
			// and the + stays put.
			strip.element().scrollLeft = 0;
			await expect
				.poll(() => metrics().mask)
				.toMatch(/transparent\)$|rgba\(0, 0, 0, 0\)\)$/);
			m = metrics();
			expect(m.mask).toMatch(
				/^linear-gradient\(to right, (rgb\(0, 0, 0\)|#000)/,
			);
			expect(add.element().getBoundingClientRect().toJSON()).toEqual(
				addBox.toJSON(),
			);

			// The page itself never scrolls sideways.
			expect(document.documentElement.scrollWidth <= window.innerWidth).toBe(
				true,
			);

			await tap(add);
			await expect
				.element(page.getByRole("dialog", { name: "Add task" }))
				.toBeVisible();
		});
	}
});

test("a scheduled prompt task can turn on precompile in its editor (#1032)", async () => {
	const { saved } = mockAgent(
		[
			{ name: "cleanup", schedule: "0 0 * * *", prompt: "Clean" },
			{
				name: "lua",
				type: "script",
				schedule: "0 0 * * *",
				prompt: "print(1)",
			},
		],
		FILES,
	);
	await renderApp("/agents/assistant/tasks?task=cleanup");
	const precompile = page.getByLabelText("Try to precompile");
	await expect.element(precompile).not.toBeChecked();
	await expect
		.element(page.getByTestId("settings-task-precompile-help"))
		.toMatchTextContent("a run whose script fails falls back to the model");
	await check(precompile);
	await page.getByRole("button", { name: "Save Changes" }).click();
	await expect
		.poll(() => saved.at(-1)?.agents[0].tasks.find((t) => t.name === "cleanup"))
		.toMatchObject({ precompile: true, prompt: "Clean" });

	// A script task has no prompt to compile.
	await goto("/agents/assistant/tasks?task=lua");
	await expect.poll(count(page.getByLabelText("Try to precompile"))).toBe(0);
});
