import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";
import {
	expectSavedTrigger,
	expectTrigger,
	fillTrigger,
	TRIGGER_CASES,
} from "./helpers/triggerEditor";

// Fixtures follow the payloads of task_list, task_upcoming and task_preview
// (internal/mcp) and of project_* for a scheduled run's job.

const minutesFromNow = (m: number) =>
	new Date(Date.now() + m * 60_000).toISOString();

const STATUS = {
	identity: "7F3CA91B",
	running: true,
	targets: ["lsegal/aviary"],
	issue_counts: { "lsegal/aviary": 3 },
	jobs: { running: 1, queued: 0, paused: 0, completed: 4, failed: 0 },
	concurrency: 4,
	delivery: "poll",
	poll_interval: "30s",
	issue_filter: "assignee:@me",
	quotas: {},
	clis: [],
	agents: ["claude"],
	recent_logs: "",
	log_offset: 0,
	last_check: minutesFromNow(0),
};

const FIRE = 1795424400;
const SCHEDULED_JOB = {
	key: `lsegal/aviary#${FIRE}`,
	target: "lsegal/aviary",
	number: FIRE,
	title: "nightly-triage",
	status: "active",
	kind: "schedule",
	schedule_id: "assistant/nightly-triage",
	agent: "claude",
	model: "opus",
	repository: "lsegal/aviary",
	actions: { retry: false, pause: false, resume: false, stop: true },
	retry_unavailable: "The scheduled run has not finished",
	started: minutesFromNow(-2),
};

const schedule = (fields: Record<string, unknown>) => ({
	agent_name: "assistant",
	agent_id: "assistant",
	type: "project",
	project: "lsegal/aviary",
	created_at: "0001-01-01T00:00:00Z",
	...fields,
});

const TASKS = [
	schedule({
		id: "assistant/nightly-triage",
		name: "nightly-triage",
		prompt: "Triage issues opened since the last run and label them.",
		model: "cli/claude/opus",
		enabled: true,
		trigger_type: "daily",
		at: "02:00",
		timezone: "America/Toronto",
		overlap: "skip",
		missed: "run_once",
		description: "daily at 02:00 (America/Toronto)",
		next_run: minutesFromNow(42),
		history: [
			{
				fire_time: minutesFromNow(-1440),
				reason: "schedule",
				status: "completed",
				summary: "Labeled 4 issues and opened #362 for a duplicate report",
				duration_ms: 312000,
			},
			{
				fire_time: minutesFromNow(-2),
				reason: "schedule",
				status: "running",
			},
		],
	}),
	schedule({
		id: "assistant/weekly-deps",
		name: "weekly-deps",
		prompt: "Update Go and pnpm dependencies and open a PR.",
		enabled: false,
		trigger_type: "weekly",
		days: "mon",
		at: "09:00",
		overlap: "queue",
		missed: "skip",
		description: "weekly on Mon at 09:00 (Local)",
	}),
	// Another project's schedule and a non-project task stay off this tab.
	schedule({
		id: "assistant/other",
		name: "other",
		project: "lsegal/zvidlib",
		enabled: true,
		every: "1h",
	}),
	{
		id: "assistant/digest",
		name: "digest",
		agent_name: "assistant",
		type: "prompt",
		enabled: true,
	},
];

const UPCOMING = [
	{
		task_id: "assistant/nightly-triage",
		name: "nightly-triage",
		project: "lsegal/aviary",
		at: minutesFromNow(42),
	},
	{
		task_id: "assistant/nightly-triage",
		name: "nightly-triage",
		project: "lsegal/aviary",
		at: minutesFromNow(42 + 1440),
	},
];

type Args = Record<string, unknown>;

async function setup(page: Page, opts: { upcoming?: unknown[] } = {}) {
	await setAuthToken(page);
	const calls: Record<string, Args[]> = {};
	const tasks: unknown[] = [...TASKS];
	const record = (tool: string) => (args?: Args) => {
		calls[tool] ??= [];
		calls[tool].push(args ?? {});
		return "ok";
	};
	await mockMCP(page, {
		agent_list: [{ name: "assistant" }, { name: "reviewer" }],
		auth_list: [],
		// No agent owns the watch, so its board sits under System → Watches.
		config_get: { agents: [{ name: "assistant", model: "cli/codex/gpt-5.5" }] },
		// The shared Add task dialog saves the config; the saved project task
		// then lists like any other.
		config_save: (args?: Args) => {
			record("config_save")(args);
			const config = JSON.parse(String(args?.config ?? "{}")) as {
				agents: { name: string; tasks?: Record<string, unknown>[] }[];
			};
			for (const agent of config.agents)
				for (const t of agent.tasks ?? [])
					if (!tasks.some((x) => (x as Args).id === `${agent.name}/${t.name}`))
						tasks.push(
							schedule({
								...t,
								id: `${agent.name}/${t.name}`,
								agent_name: agent.name,
								trigger_type: t.trigger ?? (t.schedule ? "cron" : ""),
							}),
						);
			return "ok";
		},
		project_status: STATUS,
		project_job_list: { jobs: [SCHEDULED_JOB], total: 1 },
		project_job_get: {
			...SCHEDULED_JOB,
			scrollback: "Reading the issues opened since 02:00 yesterday…",
			events: [],
		},
		project_job_stop: record("project_job_stop"),
		task_list: () => tasks,
		task_upcoming: (args?: Args) => {
			record("task_upcoming")(args);
			return opts.upcoming ?? UPCOMING;
		},
		task_preview: (args?: Args) => {
			record("task_preview")(args);
			return {
				description: "preview",
				next: [
					minutesFromNow(60),
					minutesFromNow(60 + 1440),
					minutesFromNow(60 + 2880),
				],
			};
		},
		task_update: record("task_update"),
		task_schedule: record("task_schedule"),
		config_task_convert_to_script: record("config_task_convert_to_script"),
		task_run: record("task_run"),
		task_delete: record("task_delete"),
	});
	return calls;
}

test("board shows scheduled job cards and the up next strip", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto("/system/watches/lsegal%2Faviary");

	const card = page.getByTestId("job-card");
	await expect(card).toHaveCount(1);
	await expect(card.getByTestId("job-scheduled")).toHaveText("Scheduled");
	await expect(card.getByRole("heading")).toHaveText("nightly-triage");
	await expect(card).toContainText("assistant/nightly-triage");
	// A scheduled run has no issue: no issue link and no "no PR yet".
	await expect(card.getByRole("link", { name: `#${FIRE}` })).toHaveCount(0);
	await expect(card).not.toContainText("no PR yet");
	await expect(card.getByRole("button", { name: "Retry" })).toBeDisabled();
	await card.getByRole("button", { name: "Stop" }).click();
	await expect
		.poll(() => calls.project_job_stop?.[0])
		.toEqual({ target: "lsegal/aviary", number: FIRE });

	const strip = page.getByTestId("up-next");
	await expect(strip.getByTestId("up-next-run")).toHaveCount(2);
	await expect(strip.getByTestId("up-next-run").first()).toContainText(
		"nightly-triage",
	);
	expect(calls.task_upcoming?.[0]).toEqual({
		project: "lsegal/aviary",
		limit: 5,
		per_task: 3,
	});

	// A run in the strip opens its schedule in the Schedules tab.
	await strip.getByTestId("up-next-run").first().click();
	await expect(page).toHaveURL(
		/tab=schedules&schedule=assistant\/nightly-triage/,
	);
	await expect(page.getByTestId("schedule-editor")).toBeVisible();
});

test("board lists scheduled tasks as their own boxes with run details", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/system/watches/lsegal%2Faviary");

	const boxes = page.getByTestId("scheduled-card");
	// Only this project's scheduled tasks, sorted by name.
	await expect(boxes).toHaveCount(2);
	const nightly = boxes.nth(0);
	await expect(nightly.getByRole("heading")).toHaveText("nightly-triage");
	await expect(nightly.getByTestId("scheduled-trigger")).toHaveText(
		"daily at 02:00 (America/Toronto)",
	);
	await expect(nightly.getByTestId("scheduled-next")).toContainText(
		/in 4[12]m/,
	);
	await expect(nightly.getByTestId("scheduled-last")).toContainText(
		"Labeled 4 issues",
	);
	await expect(boxes.nth(1).getByTestId("scheduled-next")).toHaveText("paused");
	await expect(boxes.nth(1).getByTestId("scheduled-last")).toHaveText("never");

	// The search narrows scheduled boxes with the jobs; a state filter hides them.
	await page.getByLabel("Search jobs").fill("dependencies");
	await expect(boxes).toHaveCount(1);
	await expect(boxes.first()).toContainText("weekly-deps");
	await page.getByLabel("Search jobs").fill("");
	await page.getByRole("tab", { name: "Running" }).click();
	await expect(boxes).toHaveCount(0);
});

test("kanban shows a Scheduled column of compact scheduled boxes", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/system/watches/lsegal%2Faviary");
	await page.getByTestId("kanban-toggle").click();

	const columns = page.getByTestId("kanban-column");
	await expect(columns.first()).toHaveAttribute("data-state", "scheduled");
	const cards = columns.first().getByTestId("kanban-scheduled-card");
	await expect(cards).toHaveCount(2);
	await expect(cards.first().getByTestId("kanban-scheduled-next")).toHaveText(
		/^Next in 4[12]m$/,
	);
	await expect(
		cards.first().getByTestId("kanban-scheduled-prompt"),
	).toContainText("Triage issues opened since the last run");
	await expect(cards.nth(1).getByTestId("kanban-scheduled-next")).toHaveText(
		"Paused",
	);
	// The compact card has no output.
	await expect(cards.first().locator("pre")).toHaveCount(0);

	// A state filter narrows the board to that state's column.
	await page.getByRole("tab", { name: "Running" }).click();
	await expect(columns).toHaveCount(1);
	await expect(columns.first()).toHaveAttribute("data-state", "running");
});

test("a scheduled box opens its timings, last runs and predicted runs", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto("/system/watches/lsegal%2Faviary");
	await page
		.getByTestId("scheduled-card")
		.first()
		.getByRole("link", { name: "nightly-triage" })
		.click();
	await expect(page).toHaveURL(/scheduled=assistant(%2F|\/)nightly-triage/);

	const detail = page.getByTestId("schedule-detail");
	await expect(detail.getByTestId("schedule-timings")).toContainText(
		"daily at 02:00 (America/Toronto)",
	);
	await expect(detail.getByTestId("schedule-timings")).toContainText(
		"America/Toronto",
	);
	// Predictions come from the scheduler for the saved task.
	await expect(
		detail.getByTestId("schedule-predicted").locator("li"),
	).toHaveCount(3);
	expect(calls.task_preview?.[0]).toEqual({
		name: "assistant/nightly-triage",
		count: 5,
	});
	const runs = detail.getByTestId("schedule-last-runs").locator("li");
	await expect(runs).toHaveCount(2);
	await expect(runs.first()).toContainText("running");
	await expect(detail.getByTestId("schedule-prompt")).toHaveText(
		"Triage issues opened since the last run and label them.",
	);
	await expect(detail.getByTestId("schedule-settings")).toContainText(
		"cli/claude/opus",
	);
	await expect(page.getByTestId("job-card")).toHaveCount(0);

	await detail.getByRole("button", { name: "Run now" }).click();
	await expect
		.poll(() => calls.task_run?.[0])
		.toEqual({ name: "assistant/nightly-triage" });

	await detail.getByRole("link", { name: "Edit" }).click();
	await expect(page.getByTestId("schedule-editor")).toBeVisible();

	await page.goBack();
	await detail.getByRole("link", { name: "Back to board" }).click();
	await expect(page.getByTestId("scheduled-card")).toHaveCount(2);
});

test("up next strip is hidden when no schedule is enabled", async ({
	page,
}) => {
	await setup(page, { upcoming: [] });
	await page.goto("/system/watches/lsegal%2Faviary");
	await expect(page.getByTestId("job-card")).toHaveCount(1);
	await expect(page.getByTestId("up-next")).toHaveCount(0);
});

test("a watch no agent owns opens no add-project dialog", async ({ page }) => {
	await setup(page);
	await page.goto("/system/watches/lsegal%2Faviary?add=1");
	await expect(page.getByTestId("job-card")).toHaveCount(1);
	await expect(
		page.getByRole("dialog", { name: "Watch a repository" }),
	).toHaveCount(0);
});

test("schedules tab lists, toggles, edits, runs and deletes schedules", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto("/system/watches/lsegal%2Faviary?tab=schedules");

	const rows = page.getByTestId("schedule-row");
	await expect(rows).toHaveCount(2);
	await expect(rows.nth(0)).toContainText("nightly-triage");
	await expect(rows.nth(0)).toContainText("daily at 02:00 (America/Toronto)");
	await expect(rows.nth(0)).toContainText("Labeled 4 issues");
	await expect(rows.nth(1)).toContainText("paused");

	await rows.nth(1).getByRole("switch").click();
	await expect
		.poll(() => calls.task_update?.[0])
		.toEqual({ name: "assistant/weekly-deps", enabled: true });

	await rows
		.nth(0)
		.getByRole("button", { name: /nightly-triage/ })
		.click();
	const editor = page.getByTestId("schedule-editor");
	await expect(editor.getByLabel("Prompt")).toHaveValue(
		"Triage issues opened since the last run and label them.",
	);
	await expect(editor.getByLabel("Name")).toBeDisabled();
	await expect(editor.getByRole("radio", { name: "Daily" })).toHaveAttribute(
		"aria-checked",
		"true",
	);
	await expect(editor.getByTestId("trigger-preview").locator("li")).toHaveCount(
		3,
	);
	await expect(editor.getByTestId("schedule-runs").locator("li")).toHaveCount(
		2,
	);
	// Most recent first.
	await expect(
		editor.getByTestId("schedule-runs").locator("li").first(),
	).toContainText("running");

	// Switching to weekly previews the edited trigger before it is saved.
	await editor.getByRole("radio", { name: "Weekly" }).click();
	await editor.getByRole("button", { name: "mon" }).click();
	await editor.getByRole("button", { name: "fri" }).click();
	await expect
		.poll(() => calls.task_preview?.at(-1))
		.toMatchObject({
			trigger: "weekly",
			days: "mon,fri",
			at: "02:00",
			timezone: "America/Toronto",
		});

	// The model override is the shared model picker, holding the task's model.
	const model = editor.getByTestId("schedule-model");
	await expect(model).toContainText("cli/claude/opus");
	await model.getByRole("button").click();
	await model.getByRole("textbox").fill("cli/codex/gpt-5.5");
	await model.getByRole("textbox").press("Enter");
	await expect(model).toContainText("cli/codex/gpt-5.5");
	await editor.getByRole("button", { name: "Save" }).click();
	await expect
		.poll(() => calls.task_update?.at(-1))
		.toEqual({
			name: "assistant/nightly-triage",
			trigger: "weekly",
			schedule: "",
			every: "",
			at: "02:00",
			days: "mon,fri",
			start_at: "",
			timezone: "America/Toronto",
			overlap: "skip",
			missed: "run_once",
			content: "Triage issues opened since the last run and label them.",
			model: "cli/codex/gpt-5.5",
			project: "lsegal/aviary",
			enabled: true,
		});

	await editor.getByRole("button", { name: "Run now" }).click();
	await expect
		.poll(() => calls.task_run?.[0])
		.toEqual({ name: "assistant/nightly-triage" });

	page.once("dialog", (dialog) => dialog.accept());
	await editor.getByRole("button", { name: "Delete" }).click();
	await expect
		.poll(() => calls.task_delete?.[0])
		.toEqual({ name: "assistant/nightly-triage" });
});

test("new schedule creates a project scheduled task through the shared Add task dialog", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto("/system/watches/lsegal%2Faviary?tab=schedules");
	await page.getByTestId("schedule-new").click();

	const dialog = page.getByRole("dialog", {
		name: "Add project scheduled task",
	});
	await expect(dialog).toBeVisible();
	await expect(dialog).toContainText("lsegal/aviary");
	await expect(dialog.getByLabel("Agent")).toHaveValue("assistant");
	await dialog.getByLabel("Task name").fill("hourly-ci-watch");
	// A project task needs a time trigger: no None tab, daily by default.
	await expect(dialog.getByRole("radio", { name: "None" })).toHaveCount(0);
	await expect(dialog.getByRole("radio", { name: "Daily" })).toHaveAttribute(
		"aria-checked",
		"true",
	);
	await dialog.getByRole("radio", { name: "Cron" }).click();
	await dialog.getByLabel("Cron expression").fill("0 * * * *");
	await dialog.getByLabel("Prompt").fill("Rerun flaky CI jobs on open PRs.");
	// Project tasks run as Projects jobs, so the dialog offers precompute but
	// it cannot apply to them.
	const precompute = dialog.getByLabel("Try to precompute task");
	await expect(precompute).toBeDisabled();
	await expect(precompute).not.toBeChecked();
	await dialog.getByRole("button", { name: "Create" }).click();
	await expect(dialog).toBeHidden();
	expect(calls.config_task_convert_to_script).toBeUndefined();

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
			enabled: true,
		}),
	]);
	expect(calls.task_schedule).toBeUndefined();

	// It stays in the project, opened in the schedule editor, which links to
	// the same task in Scheduled Tasks.
	await expect(page).toHaveURL(/schedule=assistant\/hourly-ci-watch/);
	const editor = page.getByTestId("schedule-editor");
	await expect(editor.getByLabel("Name")).toHaveValue("hourly-ci-watch");
	await expect(editor.getByTestId("schedule-task-link")).toHaveAttribute(
		"href",
		"/agents/assistant/tasks?task=hourly-ci-watch",
	);
});

test("new schedule sets every trigger type, timezone and policy, and they round-trip", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto("/system/watches/lsegal%2Faviary?tab=schedules");

	for (const c of TRIGGER_CASES) {
		const name = `schedule-${c.label.toLowerCase()}`;
		await page.getByTestId("schedule-new").click();
		const dialog = page.getByRole("dialog", {
			name: "Add project scheduled task",
		});
		await dialog.getByLabel("Task name").fill(name);
		await expect(dialog.getByLabel("Schedule")).toHaveCount(0);
		await fillTrigger(dialog, c);
		// The preview follows the edited trigger before it is saved.
		await expect
			.poll(() => calls.task_preview?.at(-1))
			.toMatchObject({ timezone: "America/Los_Angeles", ...c.saved });
		await dialog.getByLabel("Prompt").fill(`Run ${name}`);
		await dialog.getByRole("button", { name: "Create" }).click();
		await expect(dialog).toBeHidden();

		const config = JSON.parse(String(calls.config_save?.at(-1)?.config)) as {
			agents: { tasks: Record<string, unknown>[] }[];
		};
		const task = config.agents[0].tasks.find((t) => t.name === name);
		expect(task).toMatchObject({ type: "project", project: "lsegal/aviary" });
		expectSavedTrigger(task, c);

		// The saved task reopens in the schedule editor with every field kept.
		await expect(page).toHaveURL(new RegExp(`schedule=assistant/${name}`));
		const editor = page.getByTestId("schedule-editor");
		await expect(editor.getByLabel("Name")).toHaveValue(name);
		await expectTrigger(editor, c);
		// Its list entry summarizes the trigger, cron expression included.
		if (c.label === "Cron")
			await expect(
				page.getByTestId("schedule-row").filter({ hasText: name }),
			).toContainText("cron 0 * * * * (America/Los_Angeles)");
	}
});

test.describe("phone width", () => {
	test.use({ viewport: { width: 390, height: 844 } });

	test("the schedules tab and editor have no horizontal overflow", async ({
		page,
	}) => {
		await setup(page);
		await page.goto(
			"/system/watches/lsegal%2Faviary?tab=schedules&schedule=assistant/nightly-triage",
		);
		await expect(page.getByTestId("schedule-editor")).toBeVisible();
		await page.waitForTimeout(500);
		// Elements wider than the viewport, named so a failure says which.
		const overflowing = await page.evaluate(() =>
			[...document.querySelectorAll("main *")]
				.filter(
					(el) =>
						el.getBoundingClientRect().right > window.innerWidth + 1 &&
						getComputedStyle(el).position !== "fixed" &&
						!el.closest("[class*='overflow-x-auto']") &&
						!el.closest("pre"),
				)
				.map((el) => `${el.tagName.toLowerCase()}.${el.className}`),
		);
		expect(overflowing).toEqual([]);
	});
});

test("leaving a schedule with unsaved edits asks first", async ({ page }) => {
	await setup(page);
	await page.goto(
		"/system/watches/lsegal%2Faviary?tab=schedules&schedule=assistant/nightly-triage",
	);
	const editor = page.getByTestId("schedule-editor");
	const prompt = editor.getByLabel("Prompt");
	await expect(prompt).toHaveValue(
		"Triage issues opened since the last run and label them.",
	);
	const rows = page.getByTestId("schedule-row");
	const dialog = page.getByTestId("unsaved-changes-dialog");

	// Unchanged: picking another schedule does not ask.
	await rows
		.nth(1)
		.getByRole("button", { name: /weekly-deps/ })
		.click();
	await expect(page).toHaveURL(/schedule=assistant\/weekly-deps/);
	await expect(dialog).toHaveCount(0);

	await prompt.fill("Edited prompt");
	await rows
		.nth(0)
		.getByRole("button", { name: /nightly-triage/ })
		.click();
	await expect(dialog).toBeVisible();
	await dialog.getByRole("button", { name: "Stay" }).click();
	await expect(page).toHaveURL(/schedule=assistant\/weekly-deps/);
	await expect(prompt).toHaveValue("Edited prompt");

	// Saving makes it clean, so leaving no longer asks.
	await editor.getByRole("button", { name: "Save", exact: true }).click();
	await expect(editor.getByRole("button", { name: "Save" })).toBeEnabled();
	await page.getByRole("tab", { name: "Board" }).click();
	await expect(dialog).toHaveCount(0);
	await expect(page).not.toHaveURL(/tab=schedules/);
});
