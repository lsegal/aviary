import { expect, type Locator, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

type Task = Record<string, unknown> & { name: string };

interface Config {
	agents: {
		name: string;
		model: string;
		projects?: unknown[];
		tasks: Task[];
	}[];
	[key: string]: unknown;
}

const SCRIPT_TASK: Task = {
	name: "lua-ping",
	enabled: true,
	type: "script",
	trigger: "interval",
	every: "15m",
	prompt: "print('ping')",
	target: "",
	from_file: true,
};

const PROJECT_TASK: Task = {
	name: "ci-watch",
	enabled: true,
	type: "project",
	project: "lsegal/aviary",
	model: "x/z",
	trigger: "daily",
	at: "09:00",
	prompt: "Rerun flaky CI.",
	target: "",
	from_file: true,
};

/**
 * Mocks two agents that each watch a project: assistant has a script task
 * and a project task; reviewer has none. Saves replace the config.
 */
async function setup(page: Page) {
	let config: Config = {
		agents: [
			{
				name: "assistant",
				model: "x/y",
				projects: [{ target: "lsegal/aviary" }],
				tasks: [{ ...SCRIPT_TASK }, { ...PROJECT_TASK }],
			},
			{
				name: "reviewer",
				model: "x/y",
				projects: [{ target: "o/other" }],
				tasks: [],
			},
		],
	};
	const saved: Config[] = [];
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: [
			{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
			{ id: "reviewer", name: "reviewer", model: "x/y", state: "idle" },
		],
		config_get: () => config,
		config_save: (args) => {
			config = JSON.parse(String(args?.config ?? "{}")) as Config;
			saved.push(config);
			return "ok";
		},
		config_validate: [],
		config_task_convert_to_script: "task_compile_1",
		auth_list: [],
		tool_list: [],
		session_list: [
			{
				id: "sess_slack",
				agent_id: "assistant",
				name: "sess_slack",
				title: "Team standup",
				created_at: new Date().toISOString(),
				channels: ["slack"],
			},
		],
		job_query: [],
		job_list: [],
		task_compile_query: [],
		task_preview: () => ({
			description: "preview",
			next: [new Date(Date.now() + 3_600_000).toISOString()],
		}),
		agent_file_list: [],
	});
	return {
		saved,
		tasks: (agent: string) =>
			saved.at(-1)?.agents.find((a) => a.name === agent)?.tasks ?? [],
	};
}

function radio(dialog: Locator, group: "Type" | "Target", name: string) {
	return dialog
		.getByRole("radiogroup", { name: group })
		.getByRole("radio", { name });
}

async function openNewTask(page: Page, agent = "assistant") {
	await page.goto(`/agents/${agent}/tasks`);
	await page.getByTestId("new-task-button").click();
	const dialog = page.getByRole("dialog");
	await expect(dialog).toBeVisible();
	return dialog;
}

async function openEdit(page: Page, agent: string, task: string) {
	await page.goto(`/agents/${agent}/tasks`);
	await page
		.getByTestId("task-card")
		.filter({ hasText: task })
		.getByRole("button", { name: "Edit" })
		.click();
	const dialog = page.getByRole("dialog", { name: "Edit task" });
	await expect(dialog).toBeVisible();
	return dialog;
}

test("Scheduled Tasks creates a script task whose type, script and trigger round-trip", async ({
	page,
}) => {
	const { tasks } = await setup(page);
	const dialog = await openNewTask(page);
	await expect(radio(dialog, "Type", "Prompt")).toHaveAttribute(
		"aria-checked",
		"true",
	);
	await expect(radio(dialog, "Target", "Agent")).toHaveAttribute(
		"aria-checked",
		"true",
	);
	await radio(dialog, "Type", "Script").click();
	// A script is already a script, so it is never precomputed.
	await expect(dialog.getByLabel("Try to precompute task")).toBeDisabled();
	await dialog.getByLabel("Task name").fill("lua-hello");
	await dialog.getByRole("radio", { name: "Cron" }).click();
	await dialog.getByLabel("Cron expression").fill("0 8 * * 1");
	await dialog.getByLabel("Script", { exact: true }).fill("print('hi')");
	await dialog.getByRole("button", { name: "Create" }).click();
	await expect(dialog).toBeHidden();
	expect(tasks("assistant").at(-1)).toMatchObject({
		name: "lua-hello",
		type: "script",
		schedule: "0 8 * * 1",
		prompt: "print('hi')",
	});

	const edit = await openEdit(page, "assistant", "lua-hello");
	await expect(radio(edit, "Type", "Script")).toHaveAttribute(
		"aria-checked",
		"true",
	);
	await expect(edit.getByLabel("Script", { exact: true })).toHaveValue(
		"print('hi')",
	);
	await expect(edit.getByLabel("Cron expression")).toHaveValue("0 8 * * 1");
});

test("Scheduled Tasks creates a project task under the project's agent, with no Agent field", async ({
	page,
}) => {
	const { saved, tasks } = await setup(page);
	const dialog = await openNewTask(page);
	await expect(dialog.getByLabel("Agent", { exact: true })).toHaveValue(
		"assistant",
	);
	await radio(dialog, "Target", "Project").click();
	await expect(dialog).toHaveAccessibleName("Add project scheduled task");
	await expect(dialog.getByLabel("Agent", { exact: true })).toHaveCount(0);
	await expect(radio(dialog, "Type", "Script")).toBeDisabled();
	// Every agent's watched projects are offered.
	const project = dialog.getByLabel("Project", { exact: true });
	await expect(project.locator("option:not([disabled])")).toHaveText([
		"lsegal/aviary",
		"o/other",
	]);
	await project.selectOption("o/other");
	// A project task needs a time trigger: daily by default.
	await expect(dialog.getByRole("radio", { name: "None" })).toHaveCount(0);
	await expect(dialog.getByRole("radio", { name: "Daily" })).toHaveAttribute(
		"aria-checked",
		"true",
	);
	await dialog.getByLabel("Task name").fill("other-digest");
	await dialog.getByRole("radio", { name: "Interval" }).click();
	await dialog.locator('input[name="every"]').fill("2h");
	await dialog.getByLabel("Prompt", { exact: true }).fill("Summarize PRs.");
	await dialog.getByRole("button", { name: "Create" }).click();
	await expect(dialog).toBeHidden();

	// It belongs to reviewer, which watches o/other.
	expect(tasks("reviewer")).toEqual([
		expect.objectContaining({
			name: "other-digest",
			type: "project",
			project: "o/other",
			trigger: "interval",
			every: "2h",
			prompt: "Summarize PRs.",
		}),
	]);
	expect(
		saved.at(-1)?.agents.find((a) => a.name === "assistant")?.tasks,
	).toHaveLength(2);

	const edit = await openEdit(page, "reviewer", "other-digest");
	await expect(radio(edit, "Target", "Project")).toHaveAttribute(
		"aria-checked",
		"true",
	);
	await expect(edit.getByLabel("Project", { exact: true })).toHaveValue(
		"o/other",
	);
	await expect(edit.getByLabel("Agent", { exact: true })).toHaveCount(0);
	await expect(edit.getByRole("radio", { name: "Interval" })).toHaveAttribute(
		"aria-checked",
		"true",
	);
	await expect(edit.locator('input[name="every"]')).toHaveValue("2h");
});

test("an Agent target can create the task under another agent", async ({
	page,
}) => {
	const { tasks } = await setup(page);
	const dialog = await openNewTask(page);
	await dialog.getByLabel("Agent", { exact: true }).selectOption("reviewer");
	await expect(dialog).toContainText("A scheduled task for reviewer.");
	await dialog.getByLabel("Task name").fill("review-queue");
	await dialog.getByLabel("Prompt", { exact: true }).fill("Review the queue.");
	await dialog.getByRole("button", { name: "Create" }).click();
	await expect(dialog).toBeHidden();
	expect(tasks("reviewer")).toEqual([
		expect.objectContaining({
			name: "review-queue",
			type: "prompt",
			prompt: "Review the queue.",
		}),
	]);
	await expect(page).toHaveURL(/\/agents\/reviewer\/tasks\?task=review-queue$/);
});

test("editing an existing script task keeps it a script with every field", async ({
	page,
}) => {
	const { tasks } = await setup(page);
	const dialog = await openEdit(page, "assistant", "lua-ping");
	await expect(radio(dialog, "Type", "Script")).toHaveAttribute(
		"aria-checked",
		"true",
	);
	await expect(radio(dialog, "Target", "Agent")).toHaveAttribute(
		"aria-checked",
		"true",
	);
	await dialog.getByLabel("Script", { exact: true }).fill("print('pong')");
	await dialog.getByRole("button", { name: "Save" }).click();
	await expect(dialog).toBeHidden();
	expect(tasks("assistant")[0]).toEqual({
		...SCRIPT_TASK,
		prompt: "print('pong')",
	});
});

test("editing an existing project task round-trips its project and model", async ({
	page,
}) => {
	const { saved, tasks } = await setup(page);
	const dialog = await openEdit(page, "assistant", "ci-watch");
	await expect(radio(dialog, "Target", "Project")).toHaveAttribute(
		"aria-checked",
		"true",
	);
	await expect(dialog.getByLabel("Project", { exact: true })).toHaveValue(
		"lsegal/aviary",
	);
	await expect(dialog.getByLabel("Agent", { exact: true })).toHaveCount(0);
	// Saving unchanged writes nothing.
	await dialog.getByRole("button", { name: "Save" }).click();
	await expect(dialog).toBeHidden();
	expect(saved).toHaveLength(0);

	const again = await openEdit(page, "assistant", "ci-watch");
	await again.getByLabel("Prompt", { exact: true }).fill("Rerun all CI.");
	await again.getByRole("button", { name: "Save" }).click();
	await expect(again).toBeHidden();
	expect(tasks("assistant")[1]).toEqual({
		...PROJECT_TASK,
		prompt: "Rerun all CI.",
	});
});

test("the trigger editor and Send To show for every type and target", async ({
	page,
}) => {
	await setup(page);
	const dialog = await openNewTask(page);
	for (const [type, target] of [
		["Prompt", "Agent"],
		["Script", "Agent"],
		["Prompt", "Project"],
	]) {
		await radio(dialog, "Target", target).click();
		await radio(dialog, "Type", type).click();
		await expect(radio(dialog, "Type", type)).toHaveAttribute(
			"aria-checked",
			"true",
		);
		await expect(dialog.getByTestId("trigger-editor")).toBeVisible();
		await expect(dialog.getByLabel("Send To")).toBeVisible();
		await expect(dialog.getByLabel("Send To").locator("option")).toHaveText([
			"Task session only",
			"Team standup · slack",
		]);
	}
	// Choosing Project turns a script back into a prompt.
	await radio(dialog, "Target", "Agent").click();
	await radio(dialog, "Type", "Script").click();
	await radio(dialog, "Target", "Project").click();
	await expect(radio(dialog, "Type", "Prompt")).toHaveAttribute(
		"aria-checked",
		"true",
	);
});
