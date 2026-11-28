import { beforeEach, expect, test } from "vitest";
import { type Locator, page } from "vitest/browser";
import { currentURL, renderApp } from "../component-test/app";
import { mockMCP } from "../component-test/server";
import { useAgentItemDialogs } from "../composables/useAgentItemDialogs";
import { chooseOption, expectValue, optionLabels } from "./helpers/select";

// The open task dialog is module state; a spec's fresh page starts with
// none open, so close whatever the previous test left open.
beforeEach(() => {
	useAgentItemDialogs().close();
});

/** As Playwright's toBeHidden: absent, or present but not visible. */
async function expectHidden(loc: Locator) {
	await expect
		.poll(() => loc.elements().every((el) => !el.checkVisibility()))
		.toBe(true);
}

type Task = Record<string, unknown> & { name: string };

interface Config {
	agents: {
		name: string;
		model: string;
		projects?: unknown[];
		channels?: unknown[];
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
function setup() {
	let config: Config = {
		agents: [
			{
				name: "assistant",
				model: "x/y",
				projects: [{ target: "lsegal/aviary" }],
				channels: [{ type: "slack", id: "standup", name: "Team standup" }],
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
	mockMCP({
		agent_list: [
			{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
			{ id: "reviewer", name: "reviewer", model: "x/y", state: "idle" },
		],
		config_get: () => config,
		config_save: (args?: Record<string, unknown>) => {
			config = JSON.parse(String(args?.config ?? "{}")) as Config;
			saved.push(config);
			return "ok";
		},
		config_validate: [],
		config_task_convert_to_script: "task_compile_1",
		auth_list: [],
		tool_list: [],
		session_list: [],
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

async function openNewTask(agent = "assistant") {
	await renderApp(`/agents/${agent}/tasks`);
	await page.getByTestId("new-task-button").click();
	const dialog = page.getByRole("dialog");
	await expect.element(dialog).toBeVisible();
	return dialog;
}

async function openEdit(agent: string, task: string) {
	await renderApp(`/agents/${agent}/tasks`);
	await page
		.getByTestId("task-card")
		.filter({ hasText: task })
		.getByRole("button", { name: "Task settings" })
		.click();
	const dialog = page.getByRole("dialog", { name: "Edit task" });
	await expect.element(dialog).toBeVisible();
	return dialog;
}

test("Scheduled Tasks creates a script task whose type, script and trigger round-trip", async () => {
	const { tasks } = setup();
	const dialog = await openNewTask();
	await expect
		.element(radio(dialog, "Type", "Prompt"))
		.toHaveAttribute("aria-checked", "true");
	await expect
		.element(radio(dialog, "Target", "Agent"))
		.toHaveAttribute("aria-checked", "true");
	await radio(dialog, "Type", "Script").click();
	// A script is already a script, so it is never precomputed.
	await expect
		.element(dialog.getByLabelText("Try to precompile"))
		.toBeDisabled();
	await dialog.getByLabelText("Task name").fill("lua-hello");
	await dialog.getByRole("radio", { name: "Cron" }).click();
	await dialog.getByLabelText("Cron expression").fill("0 8 * * 1");
	await dialog.getByLabelText("Script", { exact: true }).fill("print('hi')");
	await dialog.getByRole("button", { name: "Create" }).click();
	await expectHidden(dialog);
	expect(tasks("assistant").at(-1)).toMatchObject({
		name: "lua-hello",
		type: "script",
		schedule: "0 8 * * 1",
		prompt: "print('hi')",
	});

	const edit = await openEdit("assistant", "lua-hello");
	await expect
		.element(radio(edit, "Type", "Script"))
		.toHaveAttribute("aria-checked", "true");
	await expect
		.element(edit.getByLabelText("Script", { exact: true }))
		.toHaveValue("print('hi')");
	await expect
		.element(edit.getByLabelText("Cron expression"))
		.toHaveValue("0 8 * * 1");
});

test("Scheduled Tasks creates a project task under the project's agent, with no Agent field", async () => {
	const { saved, tasks } = setup();
	const dialog = await openNewTask();
	await expectValue(
		dialog.getByLabelText("Agent", { exact: true }),
		"assistant",
	);
	await radio(dialog, "Target", "Project").click();
	await expect
		.element(dialog)
		.toHaveAccessibleName("Add project scheduled task");
	await expect
		.poll(
			() => dialog.getByLabelText("Agent", { exact: true }).elements().length,
		)
		.toBe(0);
	await expect.element(radio(dialog, "Type", "Script")).toBeDisabled();
	// Every agent's watched projects are offered.
	const project = dialog.getByLabelText("Project", { exact: true });
	expect(await optionLabels(project)).toEqual(["lsegal/aviary", "o/other"]);
	await chooseOption(project, "o/other");
	// A project task needs a time trigger: daily by default.
	await expect
		.poll(() => dialog.getByRole("radio", { name: "None" }).elements().length)
		.toBe(0);
	await expect
		.element(dialog.getByRole("radio", { name: "Daily" }))
		.toHaveAttribute("aria-checked", "true");
	await dialog.getByLabelText("Task name").fill("other-digest");
	await dialog.getByRole("radio", { name: "Interval" }).click();
	await dialog.locator('input[name="every"]').fill("2h");
	await dialog.getByLabelText("Prompt", { exact: true }).fill("Summarize PRs.");
	await dialog.getByRole("button", { name: "Create" }).click();
	await expectHidden(dialog);

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

	const edit = await openEdit("reviewer", "other-digest");
	await expect
		.element(radio(edit, "Target", "Project"))
		.toHaveAttribute("aria-checked", "true");
	await expect
		.element(edit.getByLabelText("Project", { exact: true }))
		.toHaveValue("o/other");
	await expect
		.poll(() => edit.getByLabelText("Agent", { exact: true }).elements().length)
		.toBe(0);
	await expect
		.element(edit.getByRole("radio", { name: "Interval" }))
		.toHaveAttribute("aria-checked", "true");
	await expect.element(edit.locator('input[name="every"]')).toHaveValue("2h");
});

test("an Agent target can create the task under another agent", async () => {
	const { tasks } = setup();
	const dialog = await openNewTask();
	await chooseOption(
		dialog.getByLabelText("Agent", { exact: true }),
		"reviewer",
	);
	await expect
		.element(dialog)
		.toMatchTextContent("A scheduled task for reviewer.");
	await dialog.getByLabelText("Task name").fill("review-queue");
	await dialog
		.getByLabelText("Prompt", { exact: true })
		.fill("Review the queue.");
	await dialog.getByRole("button", { name: "Create" }).click();
	await expectHidden(dialog);
	expect(tasks("reviewer")).toEqual([
		expect.objectContaining({
			name: "review-queue",
			type: "prompt",
			prompt: "Review the queue.",
		}),
	]);
	await expect
		.poll(currentURL)
		.toMatch(/\/agents\/reviewer\/tasks\?task=review-queue$/);
});

test("editing an existing script task keeps it a script with every field", async () => {
	const { tasks } = setup();
	const dialog = await openEdit("assistant", "lua-ping");
	await expect
		.element(radio(dialog, "Type", "Script"))
		.toHaveAttribute("aria-checked", "true");
	await expect
		.element(radio(dialog, "Target", "Agent"))
		.toHaveAttribute("aria-checked", "true");
	await dialog.getByLabelText("Script", { exact: true }).fill("print('pong')");
	await dialog.getByRole("button", { name: "Save" }).click();
	await expectHidden(dialog);
	expect(tasks("assistant")[0]).toEqual({
		...SCRIPT_TASK,
		prompt: "print('pong')",
	});
});

test("editing an existing project task round-trips its project and model", async () => {
	const { saved, tasks } = setup();
	const dialog = await openEdit("assistant", "ci-watch");
	await expect
		.element(radio(dialog, "Target", "Project"))
		.toHaveAttribute("aria-checked", "true");
	await expect
		.element(dialog.getByLabelText("Project", { exact: true }))
		.toHaveValue("lsegal/aviary");
	await expect
		.poll(
			() => dialog.getByLabelText("Agent", { exact: true }).elements().length,
		)
		.toBe(0);
	// Saving unchanged writes nothing.
	await dialog.getByRole("button", { name: "Save" }).click();
	await expectHidden(dialog);
	expect(saved).toHaveLength(0);

	const again = await openEdit("assistant", "ci-watch");
	await again.getByLabelText("Prompt", { exact: true }).fill("Rerun all CI.");
	await again.getByRole("button", { name: "Save" }).click();
	await expectHidden(again);
	expect(tasks("assistant")[1]).toEqual({
		...PROJECT_TASK,
		prompt: "Rerun all CI.",
	});
});

test("the trigger editor and Send To show for every type and target", async () => {
	setup();
	const dialog = await openNewTask();
	for (const [type, target] of [
		["Prompt", "Agent"],
		["Script", "Agent"],
		["Prompt", "Project"],
	]) {
		await radio(dialog, "Target", target).click();
		await radio(dialog, "Type", type).click();
		await expect
			.element(radio(dialog, "Type", type))
			.toHaveAttribute("aria-checked", "true");
		await expect.element(dialog.getByTestId("trigger-editor")).toBeVisible();
		await expect.element(dialog.getByLabelText("Send To")).toBeVisible();
		expect(await optionLabels(dialog.getByLabelText("Send To"))).toEqual([
			"Task session only",
			"Team standup · slack",
		]);
	}
	// Choosing Project turns a script back into a prompt.
	await radio(dialog, "Target", "Agent").click();
	await radio(dialog, "Type", "Script").click();
	await radio(dialog, "Target", "Project").click();
	await expect
		.element(radio(dialog, "Type", "Prompt"))
		.toHaveAttribute("aria-checked", "true");
});
