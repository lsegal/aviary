import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";
import { chooseOption, expectValue, optionLabels } from "./helpers/select";
import {
	expectSavedTrigger,
	expectTrigger,
	fillTrigger,
	TRIGGER_CASES,
} from "./helpers/triggerEditor";

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

/**
 * Mocks one agent with `tasks` and root `files`, keeping both as the UI
 * saves, renames and deletes them.
 */
async function mockAgent(
	page: Page,
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
	const converted: Record<string, unknown>[] = [];
	const previews: Record<string, unknown>[] = [];
	const taskSessions: Record<string, unknown>[] = [];
	const store = new Map(Object.entries(files));
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: [
			{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
		],
		config_get: () => config,
		config_save: (args) => {
			config = JSON.parse(String(args?.config ?? "{}")) as Config;
			saved.push(config);
			return "ok";
		},
		config_task_rename: (args?: Record<string, unknown>) => {
			for (const t of config.agents[0].tasks)
				if (t.name === args?.task) t.name = String(args?.new_name);
			return "renamed";
		},
		config_task_convert_to_script: (args?: Record<string, unknown>) => {
			converted.push(args ?? {});
			return "task_compile_1";
		},
		config_validate: [],
		task_preview: (args?: Record<string, unknown>) => {
			previews.push(args ?? {});
			const at = (h: number) =>
				new Date(Date.now() + h * 3_600_000).toISOString();
			return { description: "preview", next: [at(1), at(2), at(3)] };
		},
		task_session: (args?: Record<string, unknown>) => {
			taskSessions.push(args ?? {});
			const name = String(args?.name);
			return { id: name, agent_id: "assistant", name, title: name };
		},
		session_messages: (args?: Record<string, unknown>) =>
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
		agent_file_read: (args) => store.get(String(args?.file ?? "")) ?? "",
		agent_file_write: (args) => {
			store.set(String(args?.file ?? ""), String(args?.content ?? ""));
			return "ok";
		},
		agent_file_delete: (args) => {
			store.delete(String(args?.file ?? ""));
			return "ok";
		},
		agent_file_rename: (args) => {
			const from = String(args?.file ?? "");
			store.set(String(args?.to ?? ""), store.get(from) ?? "");
			store.delete(from);
			return "ok";
		},
	});
	return { saved, store, converted, previews, taskSessions };
}

const FILES = {
	"AGENTS.md": "# Agents",
	"IDENTITY.md": "# Identity",
	"MEMORY.md": "Remembered",
};

function sidebarRow(page: Page, key: "tasks" | "files") {
	return page
		.getByTestId("sidebar-sole-agent")
		.getByTestId(`sidebar-row-${key}`);
}

function taskRows(page: Page) {
	return page.getByTestId("sidebar-tasks").getByTestId("task-row");
}

/** The open task's name in its settings editor. */
function taskNameField(page: Page) {
	return page.locator("main").locator('input[placeholder="daily-briefing"]');
}

function fileRows(page: Page) {
	return page.getByTestId("sidebar-files").getByTestId("file-row");
}

test("the Tasks + opens the add-task modal and the new task joins the sidebar", async ({
	page,
}) => {
	const { saved, converted } = await mockAgent(
		page,
		[{ name: "daily-briefing", schedule: "0 9 * * *", prompt: "Brief" }],
		FILES,
	);
	await page.goto("/agents/assistant/tasks");
	await expect(taskRows(page)).toHaveText(["daily-briefing"]);
	// No inline add-task input remains on the page.
	await expect(page.getByPlaceholder("task-name")).toHaveCount(0);
	// Precompute is chosen per task in the dialog, not on the page.
	await expect(page.getByText(/Try to precompute/)).toHaveCount(0);

	const add = sidebarRow(page, "tasks").getByRole("button", {
		name: "Add task",
	});
	await expect(add).toHaveAttribute("title", "Add task");
	await sidebarRow(page, "tasks").hover();
	await add.click();

	const dialog = page.getByRole("dialog", { name: "Add task" });
	await expect(dialog).toBeVisible();
	await dialog.getByLabel("Task name").fill("weekly-report");
	await dialog.getByRole("radio", { name: "Cron" }).click();
	await dialog.getByLabel("Cron expression").fill("0 8 * * 1");
	await dialog.getByLabel("Prompt").fill("Write the weekly report");
	// scheduler.precompute_tasks defaults on, so the new task is precomputed.
	await expect(dialog.getByLabel("Try to precompute task")).toBeChecked();
	await dialog.getByRole("button", { name: "Create" }).click();

	await expect(dialog).toBeHidden();
	await expect(taskRows(page)).toHaveText(["daily-briefing", "weekly-report"]);
	expect(converted).toEqual([{ agent: "assistant", task: "weekly-report" }]);
	await expect(page).toHaveURL(
		/\/agents\/assistant\/tasks\?task=weekly-report$/,
	);
	await expect(page.locator('input[placeholder="daily-briefing"]')).toHaveValue(
		"weekly-report",
	);
	expect(saved.at(-1)?.agents[0].tasks).toMatchObject([
		{ name: "daily-briefing" },
		{
			name: "weekly-report",
			schedule: "0 8 * * 1",
			prompt: "Write the weekly report",
			from_file: true,
		},
	]);
});

test("the add-task modal sets every trigger type, timezone and policy, and they round-trip", async ({
	page,
}) => {
	const { saved, previews } = await mockAgent(page, [], FILES);
	await page.goto("/agents/assistant/tasks");

	for (const c of TRIGGER_CASES) {
		const name = `task-${c.label.toLowerCase()}`;
		await sidebarRow(page, "tasks").hover();
		await sidebarRow(page, "tasks")
			.getByRole("button", { name: "Add task" })
			.click();
		const dialog = page.getByRole("dialog", { name: "Add task" });
		await dialog.getByLabel("Task name").fill(name);
		// The add modal is the shared trigger editor, not a raw cron field.
		await expect(dialog.getByLabel("Schedule")).toHaveCount(0);
		await fillTrigger(dialog, c);
		// The preview follows the edited trigger before it is saved.
		await expect
			.poll(() => previews.at(-1))
			.toMatchObject({ timezone: "America/Los_Angeles", ...c.saved });
		await dialog.getByLabel("Prompt").fill(`Run ${name}`);
		await dialog.getByRole("button", { name: "Create" }).click();
		await expect(dialog).toBeHidden();

		const task = saved.at(-1)?.agents[0].tasks.find((t) => t.name === name) as
			| Record<string, unknown>
			| undefined;
		expectSavedTrigger(task, c);

		// The task's own page edits it with the same trigger editor.
		await expect(page).toHaveURL(new RegExp(`\\?task=${name}$`));
		await expectTrigger(page.locator("main"), c);

		// Reopening it in the Tasks overview's edit modal keeps every field.
		await page.goto("/agents/assistant/tasks");
		await page
			.getByRole("group", { name: `${name} actions` })
			.getByRole("button", { name: "Edit" })
			.click();
		const edit = page.getByRole("dialog", { name: "Edit task" });
		await expectTrigger(edit, c);
		await edit.getByRole("button", { name: "Cancel" }).click();
		await expect(edit).toBeHidden();
	}
});

test("the add-task modal's precompute choice follows the setting and applies to that task", async ({
	page,
}) => {
	const { saved, converted } = await mockAgent(page, [], FILES, {
		scheduler: { precompute_tasks: false },
	});
	await page.goto("/agents/assistant/tasks");
	await sidebarRow(page, "tasks").hover();
	await sidebarRow(page, "tasks")
		.getByRole("button", { name: "Add task" })
		.click();

	const dialog = page.getByRole("dialog", { name: "Add task" });
	const precompute = dialog.getByLabel("Try to precompute task");
	await expect(precompute).not.toBeChecked();
	await dialog.getByLabel("Task name").fill("stays-prompt");
	await dialog.getByRole("radio", { name: "Cron" }).click();
	await dialog.getByLabel("Cron expression").fill("0 8 * * 1");
	await dialog.getByLabel("Prompt").fill("Summarize the week");
	await dialog.getByRole("button", { name: "Create" }).click();
	await expect(dialog).toBeHidden();
	expect(saved.at(-1)?.agents[0].tasks).toMatchObject([
		{ name: "stays-prompt", type: "prompt" },
	]);
	expect(converted).toEqual([]);

	// Checking it precomputes just the task being created.
	await sidebarRow(page, "tasks").hover();
	await sidebarRow(page, "tasks")
		.getByRole("button", { name: "Add task" })
		.click();
	await expect(precompute).not.toBeChecked();
	await dialog.getByLabel("Task name").fill("compiled");
	await dialog.getByRole("radio", { name: "Cron" }).click();
	await dialog.getByLabel("Cron expression").fill("*/5 * * * *");
	await dialog.getByLabel("Prompt").fill("Print the time");
	await precompute.check();
	await dialog.getByRole("button", { name: "Create" }).click();
	await expect(dialog).toBeHidden();
	expect(converted).toEqual([{ agent: "assistant", task: "compiled" }]);
});

test("the add-task modal's Send To forwards runs to a channel by name, shown as Send Via in the editor", async ({
	page,
}) => {
	const { saved } = await mockAgent(page, [], FILES);
	await page.goto("/agents/assistant/tasks");
	await sidebarRow(page, "tasks").hover();
	await sidebarRow(page, "tasks")
		.getByRole("button", { name: "Add task" })
		.click();

	const dialog = page.getByRole("dialog", { name: "Add task" });
	const sendTo = dialog.getByLabel("Send To");
	// The agent's channels are offered by name after the default.
	await expectValue(sendTo, "");
	expect(await optionLabels(sendTo)).toEqual([
		"Task session only",
		"Team standup · slack",
	]);
	await dialog.getByLabel("Task name").fill("standup-digest");
	await dialog.getByLabel("Prompt").fill("Summarize yesterday");
	await chooseOption(sendTo, "Team standup · slack");
	await dialog.getByRole("button", { name: "Create" }).click();
	await expect(dialog).toBeHidden();
	expect(saved.at(-1)?.agents[0].tasks).toMatchObject([
		{ name: "standup-digest", target: "channel:Team standup" },
	]);

	// The task's editor shows the same delivery in Send Via.
	await expect(page).toHaveURL(/\?task=standup-digest$/);
	const main = page.locator("main");
	await expectValue(
		main.getByRole("combobox", { name: "Send Via" }),
		"channel:Team standup",
	);

	// Left at the default, a new task forwards nowhere, as before.
	await sidebarRow(page, "tasks").hover();
	await sidebarRow(page, "tasks")
		.getByRole("button", { name: "Add task" })
		.click();
	await expectValue(sendTo, "");
	await dialog.getByLabel("Task name").fill("quiet");
	await dialog.getByRole("button", { name: "Create" }).click();
	await expect(dialog).toBeHidden();
	expect(
		saved.at(-1)?.agents[0].tasks.find((t) => t.name === "quiet"),
	).toMatchObject({ target: "" });
});

test("the Files + creates a file that joins the sidebar, and the page has no file tabs", async ({
	page,
}) => {
	const { store } = await mockAgent(page, [], FILES);
	await page.goto("/agents/assistant/files");
	await expect(fileRows(page)).toHaveText([
		"AGENTS.md",
		"IDENTITY.md",
		"MEMORY.md",
	]);

	// The page only edits the open file: no in-page file list or add input.
	const main = page.locator("main");
	await expect(main.getByRole("button", { name: "IDENTITY.md" })).toHaveCount(
		0,
	);
	await expect(main.getByPlaceholder("IDENTITY.md")).toHaveCount(0);
	await expect(
		main.getByRole("button", { name: "+", exact: true }),
	).toHaveCount(0);

	await sidebarRow(page, "files").hover();
	await sidebarRow(page, "files")
		.getByRole("button", { name: "Add file" })
		.click();
	const dialog = page.getByRole("dialog", { name: "Add file" });
	await dialog.getByLabel("File name").fill("PROFILE");
	await dialog.getByRole("button", { name: "Create" }).click();

	await expect(dialog).toBeHidden();
	await expect(fileRows(page)).toHaveText([
		"AGENTS.md",
		"IDENTITY.md",
		"MEMORY.md",
		"PROFILE.md",
	]);
	await expect(page).toHaveURL(/\/agents\/assistant\/files\?file=PROFILE\.md$/);
	await expect(page.getByTestId("agent-file-name")).toHaveText("PROFILE.md");
	expect(store.has("PROFILE.md")).toBe(true);
});

test("Scheduled Tasks lists its tasks on every page and a row opens the task's session", async ({
	page,
}) => {
	const { taskSessions } = await mockAgent(
		page,
		[
			{ name: "daily-briefing", schedule: "0 9 * * *", prompt: "Brief" },
			{ name: "cleanup", schedule: "0 0 * * *", prompt: "Clean" },
		],
		FILES,
	);
	await page.goto("/agents/assistant/chat");
	await expect(taskRows(page)).toHaveText(["daily-briefing", "cleanup"]);

	const cleanup = taskRows(page).filter({ hasText: "cleanup" });
	await cleanup.getByTestId("sidebar-item-link").click();
	await expect(page).toHaveURL(/\/agents\/assistant\/tasks\/cleanup$/);
	await expect(page.getByTestId("task-title")).toHaveText("cleanup");
	await expect(page.getByText("cleanup: all green")).toBeVisible();
	expect(taskSessions.at(-1)).toEqual({ agent: "assistant", name: "cleanup" });
	await expect(cleanup).toHaveClass(/(^|\s)bg-gray-100/);
	await expect(
		taskRows(page).filter({ hasText: "daily-briefing" }),
	).not.toHaveClass(/(^|\s)bg-gray-100/);

	// The gear opens the task's settings, where the row stays active.
	await page.getByTestId("task-settings-link").click();
	await expect(page).toHaveURL(/\/agents\/assistant\/tasks\?task=cleanup$/);
	await expect(taskNameField(page).first()).toHaveValue("cleanup");
	await expect(taskRows(page)).toHaveText(["daily-briefing", "cleanup"]);
	await expect(cleanup).toHaveClass(/(^|\s)bg-gray-100/);
});

test("a renamed task's session takes the new name", async ({ page }) => {
	await mockAgent(
		page,
		[{ name: "cleanup", schedule: "0 0 * * *", prompt: "Clean" }],
		FILES,
	);
	await page.goto("/agents/assistant/tasks?task=cleanup");
	const name = taskNameField(page).first();
	await name.fill("nightly-cleanup");
	await name.blur();
	await expect(taskRows(page)).toHaveText(["nightly-cleanup"]);

	await taskRows(page).getByTestId("sidebar-item-link").click();
	await expect(page).toHaveURL(/\/agents\/assistant\/tasks\/nightly-cleanup$/);
	await expect(page.getByTestId("task-title")).toHaveText("nightly-cleanup");
	await expect(page.getByText("nightly-cleanup: all green")).toBeVisible();
});

test("the … menu opens a task's settings and deletes tasks", async ({
	page,
}) => {
	const { saved } = await mockAgent(
		page,
		[
			{ name: "daily-briefing", schedule: "0 9 * * *", prompt: "Brief" },
			{ name: "cleanup", schedule: "0 0 * * *", prompt: "Clean" },
		],
		FILES,
	);
	await page.goto("/agents/assistant/tasks/daily-briefing");

	const cleanup = taskRows(page).filter({ hasText: "cleanup" });
	await cleanup.hover();
	await cleanup.getByTestId("task-menu-button").click();
	await expect(page.getByRole("menuitem")).toHaveText([
		"Pause",
		"Settings",
		"Delete",
	]);
	await page.getByRole("menuitem", { name: "Settings" }).click();

	await expect(page).toHaveURL(/\/agents\/assistant\/tasks\?task=cleanup$/);
	await expect(page.getByRole("dialog")).toHaveCount(0);
	await expect(taskNameField(page).first()).toHaveValue("cleanup");

	await cleanup.hover();
	await cleanup.getByTestId("task-menu-button").click();
	await page.getByRole("menuitem", { name: "Delete" }).click();
	const confirm = page.getByRole("alertdialog", { name: "Delete task?" });
	await expect(confirm).toContainText("cleanup");
	await confirm.getByRole("button", { name: "Delete" }).click();

	await expect(taskRows(page)).toHaveText(["daily-briefing"]);
	await expect(page).toHaveURL(/\/agents\/assistant\/tasks$/);
	expect(saved.at(-1)?.agents[0].tasks.map((t) => t.name)).toEqual([
		"daily-briefing",
	]);
});

test("the task editor's Danger Zone removes the open task", async ({
	page,
}) => {
	const { saved } = await mockAgent(
		page,
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
	await page.goto("/agents/assistant/tasks?task=cleanup");

	const dangerZone = page.getByTestId("task-danger-zone");
	await expect(
		dangerZone.getByRole("heading", { name: "Danger Zone" }),
	).toBeVisible();
	await expect(dangerZone).toContainText(
		"Remove this scheduled task from the agent's configuration, including its definition file tasks/cleanup.md.",
	);
	await dangerZone.getByRole("button", { name: "Remove Task" }).click();

	const confirm = page.getByRole("alertdialog", { name: "Delete task?" });
	await expect(confirm).toContainText("cleanup");
	await confirm.getByRole("button", { name: "Cancel" }).click();
	await expect(confirm).toBeHidden();
	await expect(taskRows(page)).toHaveText(["daily-briefing", "cleanup"]);

	await dangerZone.getByRole("button", { name: "Remove Task" }).click();
	await confirm.getByRole("button", { name: "Delete" }).click();

	await expect(confirm).toBeHidden();
	await expect(taskRows(page)).toHaveText(["daily-briefing"]);
	await expect(page).toHaveURL(/\/agents\/assistant\/tasks$/);
	expect(saved.at(-1)?.agents[0].tasks.map((t) => t.name)).toEqual([
		"daily-briefing",
	]);
});

test("the … menu renames and deletes files, and locks built-ins", async ({
	page,
}) => {
	const { store } = await mockAgent(page, [], FILES);
	await page.goto("/agents/assistant/files?file=IDENTITY.md");

	const identity = fileRows(page).filter({ hasText: "IDENTITY.md" });
	await identity.hover();
	await identity.getByTestId("file-menu-button").click();
	await page.getByRole("menuitem", { name: "Edit" }).click();
	const dialog = page.getByRole("dialog", { name: "Rename file" });
	await expect(dialog.getByLabel("File name")).toHaveValue("IDENTITY.md");
	await dialog.getByLabel("File name").fill("PERSONA.md");
	await dialog.getByRole("button", { name: "Save" }).click();

	await expect(dialog).toBeHidden();
	await expect(fileRows(page)).toHaveText([
		"AGENTS.md",
		"MEMORY.md",
		"PERSONA.md",
	]);
	await expect(page).toHaveURL(/\?file=PERSONA\.md$/);
	expect(store.get("PERSONA.md")).toBe("# Identity");

	const persona = fileRows(page).filter({ hasText: "PERSONA.md" });
	await persona.hover();
	await persona.getByTestId("file-menu-button").click();
	await page.getByRole("menuitem", { name: "Delete" }).click();
	await page
		.getByRole("alertdialog", { name: "Delete file?" })
		.getByRole("button", { name: "Delete" })
		.click();
	await expect(fileRows(page)).toHaveText(["AGENTS.md", "MEMORY.md"]);
	expect(store.has("PERSONA.md")).toBe(false);

	// Built-in files cannot be renamed or deleted.
	const agents = fileRows(page).filter({ hasText: "AGENTS.md" });
	await agents.hover();
	await agents.getByTestId("file-menu-button").click();
	await expect(page.getByRole("menuitem", { name: "Edit" })).toHaveAttribute(
		"data-disabled",
		"",
	);
	await expect(page.getByRole("menuitem", { name: "Delete" })).toHaveAttribute(
		"data-disabled",
		"",
	);
});

test("long Tasks and Files lists show five items plus More", async ({
	page,
}) => {
	const tasks = Array.from({ length: 7 }, (_, i) => ({
		name: `task-${i + 1}`,
	}));
	const files = Object.fromEntries(
		Array.from({ length: 7 }, (_, i) => [`NOTE${i + 1}.md`, ""]),
	);
	await mockAgent(page, tasks, files);

	await page.goto("/agents/assistant/tasks");
	await expect(taskRows(page)).toHaveCount(5);
	await page.getByTestId("sidebar-tasks-more").click();
	await expect(taskRows(page)).toHaveCount(7);

	await page.goto("/agents/assistant/files");
	await expect(fileRows(page)).toHaveCount(5);
	await page.getByTestId("sidebar-files-more").click();
	await expect(fileRows(page)).toHaveCount(7);
});

test("Chats, Tasks and Files share one sidebar row and … menu", async ({
	page,
}) => {
	await mockAgent(page, [{ name: "daily-briefing" }], FILES);
	for (const [path, list, row] of [
		["chat", "sidebar-chats", "chat-row"],
		["tasks", "sidebar-tasks", "task-row"],
		["files", "sidebar-files", "file-row"],
	] as const) {
		await page.goto(`/agents/assistant/${path}`);
		const item = page.getByTestId(list).getByTestId(row).first();
		await expect(item.getByTestId("sidebar-item-link")).toBeVisible();
		await expect(
			item.getByRole("button", { name: /^More options for / }),
		).toHaveCount(1);
	}
});

test("+ and … show on hover and focus on desktop", async ({ page }) => {
	await mockAgent(page, [{ name: "daily-briefing" }], FILES);
	await page.goto("/agents/assistant/tasks");
	await page.mouse.move(800, 400);

	const add = sidebarRow(page, "tasks").getByRole("button", {
		name: "Add task",
	});
	const menu = taskRows(page).first().getByTestId("task-menu-button");
	await expect(add).toHaveCSS("opacity", "0");
	await expect(menu).toHaveCSS("opacity", "0");

	await sidebarRow(page, "tasks").hover();
	await expect(add).toHaveCSS("opacity", "1");
	await taskRows(page).first().hover();
	await expect(menu).toHaveCSS("opacity", "1");
	await expect(add).toHaveCSS("opacity", "0");

	// Keyboard focus reveals them too.
	await page.mouse.move(800, 400);
	await add.focus();
	await expect(add).toHaveCSS("opacity", "1");
	await menu.focus();
	await expect(menu).toHaveCSS("opacity", "1");
});

test.describe("on a touch screen", () => {
	test.use({
		hasTouch: true,
		isMobile: true,
		viewport: { width: 1180, height: 820 },
	});

	test("+ and … are always visible", async ({ page }) => {
		await mockAgent(page, [{ name: "daily-briefing" }], FILES);
		await page.goto("/agents/assistant/files");
		await expect(
			sidebarRow(page, "files").getByRole("button", { name: "Add file" }),
		).toHaveCSS("opacity", "1");
		await expect(
			fileRows(page).first().getByTestId("file-menu-button"),
		).toHaveCSS("opacity", "1");

		await page.goto("/agents/assistant/tasks");
		await expect(
			sidebarRow(page, "tasks").getByRole("button", { name: "Add task" }),
		).toHaveCSS("opacity", "1");
		await expect(
			taskRows(page).first().getByTestId("task-menu-button"),
		).toHaveCSS("opacity", "1");
	});
});

test.describe("on a phone", () => {
	test.use({
		hasTouch: true,
		isMobile: true,
		viewport: { width: 390, height: 844 },
	});

	test("the second row lists files and adds one", async ({ page }) => {
		await mockAgent(page, [], FILES);
		await page.goto("/agents/assistant/files");
		await page.getByRole("link", { name: "IDENTITY.md" }).tap();
		await expect(page).toHaveURL(/\?file=IDENTITY\.md$/);
		await expect(page.getByTestId("agent-file-name")).toHaveText("IDENTITY.md");

		await page.getByTestId("mobile-row-action").tap();
		await expect(page.getByRole("dialog", { name: "Add file" })).toBeVisible();
	});
});
