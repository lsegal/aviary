import { expect, test } from "vitest";
import { type Locator, page } from "vitest/browser";
import { renderApp } from "../component-test/app";
import { declarePages } from "../component-test/routes";
import { mockMCP } from "../component-test/server";
import { chooseOption } from "./helpers/select";

// The pages this file renders (see component-test/routes.ts).
declarePages(
	() => import("../views/SettingsView.vue"),
	() => import("../views/TaskView.vue"),
);

// Duplicate opens the create form prefilled from a scheduled task, mainly to
// copy it to another project (#1465). Fixtures follow config_get's agents
// and the tools the shared Add task dialog calls.

type Args = Record<string, unknown>;

/** As Playwright's toBeHidden: absent, or present but not visible. */
async function expectHidden(loc: Locator) {
	await expect
		.poll(() => loc.elements().every((el) => !el.checkVisibility()))
		.toBe(true);
}

const SOURCE = {
	name: "aviary-release",
	type: "project",
	project: "lsegal/aviary",
	prompt:
		"Cut a release of lsegal/aviary: tag https://github.com/lsegal/aviary and update ~/src/aviary/CHANGELOG.md.",
	model: "cli/claude/opus",
	trigger: "weekly",
	days: "fri",
	at: "16:00",
	timezone: "America/Toronto",
	overlap: "skip",
	missed: "run_once",
	target: "channel:ops",
	precompile: true,
	compiled: "return 'stale'",
	compiled_key: "abc",
	script_verified: true,
	enabled: false,
	from_file: true,
};

function setup() {
	const calls: Record<string, Args[]> = {};
	const record = (tool: string) => (args?: Args) => {
		calls[tool] ??= [];
		calls[tool].push(args ?? {});
		return "ok";
	};
	// lsegal/aviary is assistant's, lsegal/yard reviewer's, so the copy's
	// name is checked against another agent's tasks there.
	const agents: { name: string; tasks: Args[]; [key: string]: unknown }[] = [
		{
			name: "assistant",
			model: "cli/codex/gpt-5.5",
			channels: [{ type: "discord", id: "ops", name: "ops" }],
			projects: [{ target: "lsegal/aviary" }],
			tasks: [SOURCE, { ...SOURCE, name: "aviary-release-2", enabled: true }],
		},
		{
			name: "reviewer",
			model: "cli/codex/gpt-5.5",
			projects: [{ target: "lsegal/yard" }],
			tasks: [],
		},
	];
	mockMCP({
		agent_list: agents.map((a) => ({ id: a.name, name: a.name })),
		auth_list: [],
		config_get: () => ({ agents }),
		config_validate: [],
		config_save: (args?: Args) => {
			record("config_save")(args);
			const saved = JSON.parse(String(args?.config ?? "{}")) as {
				agents: { name: string; tasks?: Args[] }[];
			};
			for (const a of saved.agents) {
				const agent = agents.find((x) => x.name === a.name);
				if (agent) agent.tasks = a.tasks ?? [];
			}
			return "ok";
		},
		config_task_convert_to_script: record("config_task_convert_to_script"),
		session_list: [],
		job_query: [],
		job_list: [],
		task_list: [],
		task_compile_query: [],
		task_session: { id: "sess_task" },
		task_preview: (args?: Args) => {
			record("task_preview")(args);
			return { description: "weekly on Fri at 16:00", next: [] };
		},
		task_analyze: (args?: Args) => {
			record("task_analyze")(args);
			return {
				verdict: "deterministic",
				will_precompile: true,
				reason: "fixed gh commands",
				estimate: { available: true, min_tokens: 0, max_tokens: 0 },
			};
		},
	});
	return calls;
}

function savedTasks(calls: Record<string, Args[]>, agent: string): Args[] {
	const config = JSON.parse(String(calls.config_save?.at(-1)?.config)) as {
		agents: { name: string; tasks?: Args[] }[];
	};
	return config.agents.find((a) => a.name === agent)?.tasks ?? [];
}

test("Duplicate in the sidebar menu prefills the create form, and Cancel saves nothing", async () => {
	const calls = setup();
	await renderApp("/agents/assistant/tasks");
	const row = page
		.getByTestId("sidebar")
		.locator('[data-testid=task-row][data-task="aviary-release"]');
	await row.hover();
	await row.getByTestId("task-menu-button").click();
	await page.getByRole("menuitem", { name: "Duplicate…" }).click();

	const dialog = page.getByRole("dialog", { name: "Duplicate aviary-release" });
	await expect.element(dialog).toBeVisible();
	// x and x-2 are taken, so the copy is x-3.
	await expect
		.element(dialog.getByLabelText("Task name"))
		.toHaveValue("aviary-release-3");
	await expect
		.element(
			dialog.getByRole("radiogroup", { name: "Target" }).getByRole("radio", {
				name: "Project",
			}),
		)
		.toHaveAttribute("aria-checked", "true");
	await expect
		.element(dialog.getByLabelText("Project", { exact: true }))
		.toHaveValue("lsegal/aviary");
	await expect
		.element(dialog.getByLabelText("Prompt"))
		.toHaveValue(SOURCE.prompt);
	await expect
		.element(dialog.getByRole("radio", { name: "Weekly" }))
		.toHaveAttribute("aria-checked", "true");
	await expect
		.element(dialog.getByLabelText("Try to precompile"))
		.toBeChecked();
	await expect
		.element(dialog.getByTestId("task-model-override"))
		.toMatchTextContent("cli/claude/opus");
	await expect
		.element(dialog.getByLabelText("Send To"))
		.toHaveValue("ops · discord");
	// The same project raises no warning about its own references.
	await expect
		.poll(
			() => dialog.getByTestId("task-duplicate-repo-warning").elements().length,
		)
		.toBe(0);

	await dialog.getByRole("button", { name: "Cancel" }).click();
	await expectHidden(dialog);
	expect(calls.config_save).toBeUndefined();
});

test("Duplicate on the task page copies the task to another project, replacing its references only when accepted", async () => {
	const calls = setup();
	await renderApp("/agents/assistant/tasks/aviary-release");
	await page.getByTestId("task-duplicate").click();

	const dialog = page.getByRole("dialog", { name: "Duplicate aviary-release" });
	await expect
		.element(dialog.getByLabelText("Task name"))
		.toHaveValue("aviary-release-3");
	await chooseOption(
		dialog.getByLabelText("Project", { exact: true }),
		"lsegal/yard",
	);
	// reviewer, which owns lsegal/yard, has no aviary-release.
	await expect
		.element(dialog.getByLabelText("Task name"))
		.toHaveValue("aviary-release");
	// The preview and estimate follow the destination.
	await expect
		.poll(() => calls.task_analyze?.at(-1))
		.toMatchObject({ project: "lsegal/yard", agent: "reviewer" });

	const warning = dialog.getByTestId("task-duplicate-repo-warning");
	await expect.element(warning).toMatchTextContent("lsegal/aviary");
	await expect.element(warning).toMatchTextContent("3 references");
	// Keep as is leaves the prompt and hides the warning for this project.
	await warning.getByRole("button", { name: "Keep as is" }).click();
	await expect.poll(() => warning.elements().length).toBe(0);
	await expect
		.element(dialog.getByLabelText("Prompt"))
		.toHaveValue(SOURCE.prompt);

	// Another project asks again; Replace rewrites the references.
	await chooseOption(
		dialog.getByLabelText("Project", { exact: true }),
		"lsegal/aviary",
	);
	await chooseOption(
		dialog.getByLabelText("Project", { exact: true }),
		"lsegal/yard",
	);
	await expect.element(warning).toBeVisible();
	await warning
		.getByRole("button", { name: "Replace with lsegal/yard" })
		.click();
	await expect.poll(() => warning.elements().length).toBe(0);
	await expect
		.element(dialog.getByLabelText("Prompt"))
		.toHaveValue(
			"Cut a release of lsegal/yard: tag https://github.com/lsegal/yard and update ~/src/yard/CHANGELOG.md.",
		);

	await dialog.getByRole("button", { name: "Create" }).click();
	await expectHidden(dialog);
	const [copy] = savedTasks(calls, "reviewer");
	expect(copy).toMatchObject({
		name: "aviary-release",
		type: "project",
		project: "lsegal/yard",
		model: "cli/claude/opus",
		trigger: "weekly",
		days: "fri",
		at: "16:00",
		timezone: "America/Toronto",
		overlap: "skip",
		missed: "run_once",
		precompile: true,
		enabled: false,
		target: "",
	});
	// It starts fresh: no compiled script, compiled for its own project.
	expect(copy).not.toHaveProperty("compiled");
	expect(copy).not.toHaveProperty("compiled_key");
	expect(copy).not.toHaveProperty("script_verified");
	expect(calls.config_task_convert_to_script).toEqual([
		{ agent: "reviewer", task: "aviary-release" },
	]);
	// The source is untouched.
	expect(savedTasks(calls, "assistant")).toHaveLength(2);
});

test("Duplicate on the Scheduled Tasks page opens the same form", async () => {
	const calls = setup();
	await renderApp("/agents/assistant/tasks");
	await page
		.locator('[data-testid=task-card][data-task="aviary-release"]')
		.getByTestId("task-duplicate")
		.click();
	const dialog = page.getByRole("dialog", { name: "Duplicate aviary-release" });
	await expect
		.element(dialog.getByLabelText("Task name"))
		.toHaveValue("aviary-release-3");
	await dialog.getByRole("button", { name: "Create" }).click();
	await expectHidden(dialog);
	expect(savedTasks(calls, "assistant").map((t) => t.name)).toEqual([
		"aviary-release",
		"aviary-release-2",
		"aviary-release-3",
	]);
});
