// Ported from web/e2e/project-schedules.spec.ts (#1636): creating a
// project's scheduled task from its board, through the shared Add task
// dialog and through the prompt chat.
import { afterEach, expect, test, vi } from "vitest";
import { commands, page } from "vitest/browser";
import { currentURL, goto, renderApp } from "../component-test/app";
import { chooseOption, optionLabels } from "./helpers/select";
import {
	expectSavedTrigger,
	expectTrigger,
	fillTrigger,
	TRIGGER_CASES,
} from "./helpers/triggerEditor";
import {
	type Args,
	BOARD,
	closeTaskDialog,
	expectCount,
	expectHidden,
	minutesFromNow,
	press,
	schedule,
	setup,
} from "./project-schedules-fixtures";

afterEach(() => {
	vi.useRealTimers();
});

test("the header Schedule button creates a project scheduled task through the shared Add task dialog", async () => {
	const calls = setup();
	await renderApp(BOARD);
	await expectCount(page.getByTestId("scheduled-card"), 2);
	await page.getByRole("button", { name: "Schedule task" }).click();

	const dialog = page.getByRole("dialog", {
		name: "Add project scheduled task",
	});
	await expect.element(dialog).toBeVisible();
	await expect.element(dialog).toMatchTextContent("lsegal/aviary");
	// The board opens the shared dialog targeting this project; the project's
	// agent is implied, so there is no Agent field, and project tasks run a
	// prompt, so Script is unavailable.
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
	await expectCount(dialog.getByLabelText("Agent", { exact: true }), 0);
	await expect
		.element(dialog.getByRole("radio", { name: "Script" }))
		.toBeDisabled();
	await dialog.getByLabelText("Task name").fill("hourly-ci-watch");
	// A project task needs a time trigger: no None tab, daily by default.
	await expectCount(dialog.getByRole("radio", { name: "None" }), 0);
	await expect
		.element(dialog.getByRole("radio", { name: "Daily" }))
		.toHaveAttribute("aria-checked", "true");
	await dialog.getByRole("radio", { name: "Cron" }).click();
	await dialog.getByLabelText("Cron expression").fill("0 * * * *");
	await dialog
		.getByLabelText("Prompt")
		.fill("Rerun flaky CI jobs on open PRs.");
	// Project tasks precompile like agent tasks: on by default, with the
	// same live feedback.
	const precompute = dialog.getByLabelText("Try to precompile");
	await expect.element(precompute).toBeEnabled();
	await expect.element(precompute).toBeChecked();
	const feedback = dialog.getByTestId("task-cost-feedback");
	await expect
		.element(feedback.getByTestId("task-cost-verdict"))
		.toHaveText("This task can run deterministically");
	await expect
		.element(feedback.getByTestId("task-cost-estimate"))
		.toHaveText("≈ 0 tokens/run");
	expect(calls.task_analyze?.at(-1)).toMatchObject({
		type: "project",
		project: "lsegal/aviary",
		precompile: true,
	});
	// Send To offers the agent's channels by name.
	const sendTo = dialog.getByLabelText("Send To");
	expect(await optionLabels(sendTo)).toEqual([
		"Task session only",
		"ops · discord",
	]);
	await chooseOption(sendTo, "ops · discord");
	await dialog.getByRole("button", { name: "Create" }).click();
	await expectHidden(dialog);
	// Saving compiles the project task's script in the background.
	expect(calls.config_task_convert_to_script).toEqual([
		{ agent: "assistant", task: "hourly-ci-watch" },
	]);

	const config = JSON.parse(String(calls.config_save?.[0]?.config)) as {
		agents: { name: string; tasks: unknown[] }[];
	};
	expect(config.agents[0].tasks).toEqual([
		expect.objectContaining({
			name: "hourly-ci-watch",
			type: "project",
			project: "lsegal/aviary",
			schedule: "0 * * * *",
			prompt: "Rerun flaky CI jobs on open PRs.",
			target: "channel:ops",
			enabled: true,
			precompile: true,
		}),
	]);
	expect(calls.task_schedule).toBeUndefined();

	// It shows on the board with the other scheduled items, and its card's
	// gear opens the same task's full settings.
	const card = page
		.getByTestId("scheduled-card")
		.filter({ hasText: "hourly-ci-watch" });
	await expectCount(card, 1);
	await expectCount(page.getByTestId("scheduled-card"), 3);
	await page.getByTestId("kanban-toggle").click();
	await expectCount(
		page
			.getByTestId("kanban-column")
			.first()
			.getByTestId("kanban-scheduled-card")
			.filter({ hasText: "hourly-ci-watch" }),
		1,
	);
	await page.getByTestId("kanban-toggle").click();
	await card.getByRole("button", { name: "Task settings" }).click();
	const settings = page.getByRole("dialog", { name: "Edit task" });
	await expect
		.element(settings.getByLabelText("Task name"))
		.toHaveValue("hourly-ci-watch");
	await closeTaskDialog();
});

test("the prompt chat creates a weekly scheduled task that shows on the board and in Scheduled Tasks (#1114)", async () => {
	const reply =
		"Created weekly-seo on lsegal/aviary: weekly on Mon at 09:00 (UTC). Next runs: Mon 09:00, then each Monday. Not deterministic, so each run uses the model: about 4,600–12,400 tokens per run.";
	const SEO_PROMPT =
		"Review the aviary docs site's SEO and file issues for what to fix.";
	const runs: Args[] = [];
	setup({
		owner: "assistant",
		extra: (tasks) => ({
			agent_list: [{ name: "assistant" }, { name: "projects" }],
			// The config lists the task once it is saved, as it lists a task
			// saved from the Add task dialog.
			config_get: () => ({
				agents: [
					{
						name: "assistant",
						model: "cli/codex/gpt-5.5",
						projects: [{ target: "lsegal/aviary" }],
						tasks: runs.length
							? [
									{
										name: "weekly-seo",
										type: "project",
										project: "lsegal/aviary",
										prompt: SEO_PROMPT,
										trigger: "weekly",
										days: "mon",
										at: "09:00",
										timezone: "UTC",
										enabled: true,
										from_file: true,
									},
								]
							: [],
					},
				],
			}),
			session_messages: () =>
				runs.flatMap((run, i) => [
					{
						id: `u${i}`,
						session_id: "projects-chat",
						role: "user",
						content: run.message,
						timestamp: minutesFromNow(0),
					},
					{
						id: `a${i}`,
						session_id: "projects-chat",
						role: "assistant",
						content: reply,
						timestamp: minutesFromNow(0),
					},
				]),
			// The mocked model calls project_task_create, which saves the task
			// through task_schedule; the scheduler then lists it like a task
			// made in the Add task dialog.
			agent_run: (args?: Args) => {
				runs.push(args ?? {});
				tasks.push(
					schedule({
						id: "assistant/weekly-seo",
						name: "weekly-seo",
						prompt: SEO_PROMPT,
						enabled: true,
						trigger_type: "weekly",
						days: "mon",
						at: "09:00",
						timezone: "UTC",
						description: "weekly on Mon at 09:00 (UTC)",
						next_run: minutesFromNow(60 * 24 * 3),
					}),
				);
				return { stream: [reply], result: "" };
			},
		}),
	});
	await renderApp("/agents/assistant/projects/lsegal%2Faviary");
	await expectCount(page.getByTestId("scheduled-card"), 2);
	// With the board's poll stopped, the new task can only show because the
	// finished chat turn refreshed the schedules (#1151). As page.clock's
	// pauseAt: the board polls on setTimeout, which never fires from here on.
	vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });

	await page.getByTestId("prompt-chat-toggle").click();
	const chat = page.getByTestId("project-chat");
	const input = chat.getByPlaceholder("Describe a task or ask a question");
	await expect.element(input).toBeEnabled();
	await input.fill("create a scheduled task to do SEO for aviary weekly");
	await press(input, "Enter");
	await expect.poll(() => runs.length).toBe(1);
	expect(runs[0]).toMatchObject({ name: "projects" });
	// The reply previews the schedule, its next runs and its cost.
	await expect.element(chat).toMatchTextContent("weekly on Mon at 09:00 (UTC)");
	await expect.element(chat).toMatchTextContent("tokens per run");

	// The task shows on the board with the others, as a UI-made one does.
	const card = page
		.getByTestId("scheduled-card")
		.filter({ hasText: "weekly-seo" });
	await expectCount(card, 1);
	await expect
		.element(card.getByTestId("scheduled-trigger"))
		.toHaveText("weekly on Mon at 09:00 (UTC)");
	// And in Scheduled Tasks.
	vi.useRealTimers();
	await goto("/agents/assistant/tasks?task=weekly-seo");
	await expect
		.poll(currentURL)
		.toMatch(/\/agents\/assistant\/tasks\?task=weekly-seo/);
	const main = page.getByRole("main");
	await expect
		.element(main)
		.toMatchTextContent("Defined in: tasks/weekly-seo.md");
	await expect
		.element(main.getByRole("textbox").first())
		.toHaveValue("weekly-seo");
});

test("new schedule on an owned project runs under the project's agent without asking", async () => {
	const calls = setup({ owner: "reviewer" });
	await renderApp("/agents/reviewer/projects/lsegal%2Faviary");
	await page.getByRole("button", { name: "Schedule task" }).click();

	const dialog = page.getByRole("dialog", {
		name: "Add project scheduled task",
	});
	await expect.element(dialog).toBeVisible();
	await expectCount(dialog.getByLabelText("Agent"), 0);
	await dialog.getByLabelText("Task name").fill("owned-watch");
	await dialog.getByLabelText("Prompt").fill("Summarize open PRs.");
	await dialog.getByRole("button", { name: "Create" }).click();
	await expectHidden(dialog);

	const config = JSON.parse(String(calls.config_save?.[0]?.config)) as {
		agents: { name: string; tasks?: { name: string }[] }[];
	};
	const owner = config.agents.find((a) => a.name === "reviewer");
	expect(owner?.tasks).toEqual([
		expect.objectContaining({
			name: "owned-watch",
			type: "project",
			project: "lsegal/aviary",
		}),
	]);
	expect(
		config.agents.find((a) => a.name === "assistant")?.tasks ?? [],
	).toEqual([]);
});

test("new schedule sets every trigger type, timezone and policy, and they round-trip", async () => {
	const calls = setup();
	await renderApp(BOARD);

	for (const c of TRIGGER_CASES) {
		const name = `schedule-${c.label.toLowerCase()}`;
		await page.getByTestId("schedule-new").click();
		const dialog = page.getByRole("dialog", {
			name: "Add project scheduled task",
		});
		await dialog.getByLabelText("Task name").fill(name);
		await expectCount(dialog.getByLabelText("Schedule"), 0);
		await fillTrigger(dialog, c);
		// The preview follows the edited trigger before it is saved.
		await expect
			.poll(() => calls.task_preview?.at(-1))
			.toMatchObject({ timezone: "America/Los_Angeles", ...c.saved });
		await dialog.getByLabelText("Prompt").fill(`Run ${name}`);
		await dialog.getByRole("button", { name: "Create" }).click();
		await expectHidden(dialog);

		const config = JSON.parse(String(calls.config_save?.at(-1)?.config)) as {
			agents: { tasks: Record<string, unknown>[] }[];
		};
		const task = config.agents[0].tasks.find((t) => t.name === name);
		expect(task).toMatchObject({ type: "project", project: "lsegal/aviary" });
		expectSavedTrigger(task, c);

		// Its card summarizes the trigger, cron expression included.
		const card = page.getByTestId("scheduled-card").filter({ hasText: name });
		if (c.label === "Cron")
			await expect
				.element(card.getByTestId("scheduled-trigger"))
				.toHaveText("cron 0 * * * * (America/Los_Angeles)");
		// The saved task reopens in its full settings with every field kept.
		await card.getByRole("button", { name: "Task settings" }).click();
		const settings = page.getByRole("dialog", { name: "Edit task" });
		await expect
			.element(settings.getByLabelText("Task name"))
			.toHaveValue(name);
		await expectTrigger(settings, c);
		await commands.keyPress("Escape");
		await expectHidden(settings);
	}
});
