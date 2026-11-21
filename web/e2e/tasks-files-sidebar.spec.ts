import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

interface Task {
	name: string;
	schedule?: string;
	prompt?: string;
	[key: string]: unknown;
}

interface Config {
	agents: Array<{ name: string; model: string; tasks: Task[] }>;
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
		agents: [{ name: "assistant", model: "x/y", tasks }],
		...extra,
	};
	const saved: Config[] = [];
	const converted: Record<string, unknown>[] = [];
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
	});
	return { saved, store, converted };
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
	await dialog.getByLabel("Schedule").fill("0 8 * * 1");
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
	await dialog.getByLabel("Schedule").fill("0 8 * * 1");
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
	await dialog.getByLabel("Schedule").fill("*/5 * * * *");
	await dialog.getByLabel("Prompt").fill("Print the time");
	await precompute.check();
	await dialog.getByRole("button", { name: "Create" }).click();
	await expect(dialog).toBeHidden();
	expect(converted).toEqual([{ agent: "assistant", task: "compiled" }]);
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

test("the … menu edits and deletes tasks", async ({ page }) => {
	const { saved } = await mockAgent(
		page,
		[
			{ name: "daily-briefing", schedule: "0 9 * * *", prompt: "Brief" },
			{ name: "cleanup", schedule: "0 0 * * *", prompt: "Clean" },
		],
		FILES,
	);
	await page.goto("/agents/assistant/tasks?task=cleanup");

	const cleanup = taskRows(page).filter({ hasText: "cleanup" });
	await cleanup.hover();
	await cleanup.getByTestId("task-menu-button").click();
	await page.getByRole("menuitem", { name: "Edit" }).click();

	const dialog = page.getByRole("dialog", { name: "Edit task" });
	await expect(dialog.getByLabel("Task name")).toHaveValue("cleanup");
	await expect(dialog.getByLabel("Schedule")).toHaveValue("0 0 * * *");
	await dialog.getByLabel("Task name").fill("nightly-cleanup");
	await dialog.getByLabel("Schedule").fill("30 1 * * *");
	await dialog.getByRole("button", { name: "Save" }).click();

	await expect(dialog).toBeHidden();
	await expect(taskRows(page)).toHaveText([
		"daily-briefing",
		"nightly-cleanup",
	]);
	// The open task follows its rename.
	await expect(page).toHaveURL(/\?task=nightly-cleanup$/);
	expect(saved.at(-1)?.agents[0].tasks[1]).toMatchObject({
		schedule: "30 1 * * *",
	});

	const renamed = taskRows(page).filter({ hasText: "nightly-cleanup" });
	await renamed.hover();
	await renamed.getByTestId("task-menu-button").click();
	await page.getByRole("menuitem", { name: "Delete" }).click();
	const confirm = page.getByRole("alertdialog", { name: "Delete task?" });
	await expect(confirm).toContainText("nightly-cleanup");
	await confirm.getByRole("button", { name: "Delete" }).click();

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
	const dialog = page.getByRole("dialog", { name: "Edit file" });
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
