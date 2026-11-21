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

async function setup(
	page: Page,
	opts: { upcoming?: unknown[]; owner?: string } = {},
) {
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
		// Without an owner no agent owns the watch, so its board sits under
		// System → Watches.
		config_get: {
			agents: [
				{ name: "assistant", model: "cli/codex/gpt-5.5" },
				...(opts.owner
					? [
							{
								name: opts.owner,
								model: "cli/codex/gpt-5.5",
								projects: [{ target: "lsegal/aviary" }],
							},
						]
					: []),
			],
		},
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
		session_list: (args?: Args) =>
			args?.agent === "assistant"
				? [
						{
							id: "sess_discord",
							agent_id: "assistant",
							name: "ops",
							created_at: minutesFromNow(-60),
							channels: ["discord"],
						},
						{
							id: "sess_plain",
							agent_id: "assistant",
							name: "plain",
							created_at: minutesFromNow(-60),
						},
					]
				: [],
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
	// The time sits in the header and the agent label in the footer (#756).
	const cardBox = await card.boundingBox();
	const headingBox = await card.getByRole("heading").boundingBox();
	const timeBox = await card.getByTestId("job-time").boundingBox();
	const agentBox = await card.getByTestId("job-agent").boundingBox();
	expect(timeBox!.y + timeBox!.height).toBeLessThanOrEqual(headingBox!.y);
	expect(agentBox!.y).toBeGreaterThan(headingBox!.y + headingBox!.height);
	for (const box of [timeBox!, agentBox!]) {
		expect(cardBox!.x + cardBox!.width - (box.x + box.width)).toBeLessThan(20);
	}
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

	// A run in the strip opens its schedule's editor on the board.
	await strip.getByTestId("up-next-run").first().click();
	await expect(page).toHaveURL(/scheduled=assistant(%2F|\/)nightly-triage/);
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
	await page
		.getByRole("group", { name: "Job filter" })
		.getByRole("button", { name: /^Running/ })
		.click();
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
	await page
		.getByRole("group", { name: "Job filter" })
		.getByRole("button", { name: /^Running/ })
		.click();
	await expect(columns).toHaveCount(1);
	await expect(columns.first()).toHaveAttribute("data-state", "running");
});

test("a scheduled card opens its editor within the project page", async ({
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

	// The project header and its actions stay put around the editor.
	await expect(
		page.getByRole("link", { name: "lsegal/aviary", exact: true }),
	).toBeVisible();
	await expect(page.getByTestId("schedule-new")).toBeVisible();
	const detail = page.getByTestId("schedule-detail");
	const editor = detail.getByTestId("schedule-editor");
	await expect(editor.getByLabel("Prompt")).toHaveValue(
		"Triage issues opened since the last run and label them.",
	);
	await expect(editor.getByTestId("trigger-preview").locator("li")).toHaveCount(
		3,
	);
	const runs = editor.getByTestId("schedule-runs").locator("li");
	await expect(runs).toHaveCount(2);
	await expect(runs.first()).toContainText("running");
	await expect(page.getByTestId("job-card")).toHaveCount(0);

	await editor.getByRole("button", { name: "Run now" }).click();
	await expect
		.poll(() => calls.task_run?.[0])
		.toEqual({ name: "assistant/nightly-triage" });

	// The card's expand link opens the same editor.
	await detail.getByRole("link", { name: "Back to board" }).click();
	await expect(page).not.toHaveURL(/scheduled=/);
	await expect(page.getByTestId("scheduled-card")).toHaveCount(2);
	await page
		.getByTestId("scheduled-card")
		.nth(1)
		.getByRole("link", { name: "Edit schedule" })
		.click();
	await expect(
		page.getByTestId("schedule-editor").getByLabel("Name"),
	).toHaveValue("weekly-deps");
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

test("a scheduled card's editor edits, runs and deletes its schedule", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto(
		"/system/watches/lsegal%2Faviary?scheduled=assistant/nightly-triage",
	);

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
	// Deleting it goes back to the board.
	await expect(page).not.toHaveURL(/scheduled=/);
	await expect(editor).toHaveCount(0);
});

test("the header Schedule button creates a project scheduled task through the shared Add task dialog", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto("/system/watches/lsegal%2Faviary");
	await expect(page.getByTestId("scheduled-card")).toHaveCount(2);
	await page.getByRole("button", { name: "Schedule", exact: true }).click();

	const dialog = page.getByRole("dialog", {
		name: "Add project scheduled task",
	});
	await expect(dialog).toBeVisible();
	await expect(dialog).toContainText("lsegal/aviary");
	// The task's agent comes from the project, so the dialog never asks.
	await expect(dialog.getByLabel("Agent")).toHaveCount(0);
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
	// Send To offers the agent's channel sessions by name.
	const sendTo = dialog.getByLabel("Send To");
	await expect(sendTo.locator("option")).toHaveText([
		"Task session only",
		"ops · discord",
	]);
	await sendTo.selectOption({ label: "ops · discord" });
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
			target: "session:sess_discord",
			enabled: true,
		}),
	]);
	expect(calls.task_schedule).toBeUndefined();

	// It shows on the board with the other scheduled items, and its card
	// opens the schedule editor, which links to the same task in Scheduled
	// Tasks.
	const card = page
		.getByTestId("scheduled-card")
		.filter({ hasText: "hourly-ci-watch" });
	await expect(card).toHaveCount(1);
	await expect(page.getByTestId("scheduled-card")).toHaveCount(3);
	await page.getByTestId("kanban-toggle").click();
	await expect(
		page
			.getByTestId("kanban-column")
			.first()
			.getByTestId("kanban-scheduled-card")
			.filter({ hasText: "hourly-ci-watch" }),
	).toHaveCount(1);
	await page.getByTestId("kanban-toggle").click();
	await card.getByRole("link", { name: "hourly-ci-watch" }).click();
	await expect(page).toHaveURL(/scheduled=assistant(%2F|\/)hourly-ci-watch/);
	const editor = page.getByTestId("schedule-editor");
	await expect(editor.getByLabel("Name")).toHaveValue("hourly-ci-watch");
	await expect(editor.getByTestId("schedule-task-link")).toHaveAttribute(
		"href",
		"/agents/assistant/tasks?task=hourly-ci-watch",
	);
});

test("new schedule on an owned project runs under the project's agent without asking", async ({
	page,
}) => {
	const calls = await setup(page, { owner: "reviewer" });
	await page.goto("/agents/reviewer/projects/lsegal%2Faviary");
	await page.getByRole("button", { name: "Schedule", exact: true }).click();

	const dialog = page.getByRole("dialog", {
		name: "Add project scheduled task",
	});
	await expect(dialog).toBeVisible();
	await expect(dialog.getByLabel("Agent")).toHaveCount(0);
	await dialog.getByLabel("Task name").fill("owned-watch");
	await dialog.getByLabel("Prompt").fill("Summarize open PRs.");
	await dialog.getByRole("button", { name: "Create" }).click();
	await expect(dialog).toBeHidden();

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

test("new schedule sets every trigger type, timezone and policy, and they round-trip", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto("/system/watches/lsegal%2Faviary");

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

		// Its card summarizes the trigger, cron expression included.
		const card = page.getByTestId("scheduled-card").filter({ hasText: name });
		if (c.label === "Cron")
			await expect(card.getByTestId("scheduled-trigger")).toHaveText(
				"cron 0 * * * * (America/Los_Angeles)",
			);
		// The saved task reopens in the schedule editor with every field kept.
		await card.getByRole("link", { name, exact: true }).click();
		const editor = page.getByTestId("schedule-editor");
		await expect(editor.getByLabel("Name")).toHaveValue(name);
		await expectTrigger(editor, c);
		await page.getByRole("link", { name: "Back to board" }).click();
	}
});

test.describe("phone width", () => {
	test.use({ viewport: { width: 390, height: 844 } });

	test("the board's schedule editor has no horizontal overflow", async ({
		page,
	}) => {
		await setup(page);
		await page.goto(
			"/system/watches/lsegal%2Faviary?scheduled=assistant/nightly-triage",
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
		"/system/watches/lsegal%2Faviary?scheduled=assistant/nightly-triage",
	);
	const editor = page.getByTestId("schedule-editor");
	const prompt = editor.getByLabel("Prompt");
	await expect(prompt).toHaveValue(
		"Triage issues opened since the last run and label them.",
	);
	const back = page.getByRole("link", { name: "Back to board" });
	const dialog = page.getByTestId("unsaved-changes-dialog");

	// Unchanged: going back to the board does not ask.
	await back.click();
	await expect(page).not.toHaveURL(/scheduled=/);
	await expect(dialog).toHaveCount(0);

	await page
		.getByTestId("scheduled-card")
		.first()
		.getByRole("link", { name: "nightly-triage" })
		.click();
	await prompt.fill("Edited prompt");
	await back.click();
	await expect(dialog).toBeVisible();
	await dialog.getByRole("button", { name: "Stay" }).click();
	await expect(page).toHaveURL(/scheduled=assistant(%2F|\/)nightly-triage/);
	await expect(prompt).toHaveValue("Edited prompt");

	// Saving makes it clean, so leaving no longer asks.
	await editor.getByRole("button", { name: "Save", exact: true }).click();
	await expect(editor.getByRole("button", { name: "Save" })).toBeEnabled();
	await back.click();
	await expect(dialog).toHaveCount(0);
	await expect(page).not.toHaveURL(/scheduled=/);
});

// The board's filter chip for one state, Scheduled included.
const chip = (page: Page, label: string) =>
	page
		.getByRole("group", { name: "Job filter" })
		.getByRole("button", { name: new RegExp(`^${label}`) });

test("the Scheduled chip counts, filters and persists with the job state chips", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/system/watches/lsegal%2Faviary");
	const scheduled = page.getByTestId("scheduled-card");
	const jobs = page.getByTestId("job-card");
	const total = page.getByTestId("job-total");

	// No chip selected shows the scheduled items and every job, and the
	// total counts both: one job and two scheduled items.
	await expect(chip(page, "Scheduled")).toHaveText(/^Scheduled\s*2$/);
	await expect(chip(page, "Scheduled")).toHaveAttribute(
		"aria-pressed",
		"false",
	);
	await expect(scheduled).toHaveCount(2);
	await expect(jobs).toHaveCount(1);
	await expect(total).toHaveText("3 items");

	// Scheduled alone shows only the scheduled items.
	await chip(page, "Scheduled").click();
	await expect(page).toHaveURL(/[?&]state=scheduled$/);
	await expect(chip(page, "Scheduled")).toHaveClass(/bg-violet-600/);
	await expect(scheduled).toHaveCount(2);
	await expect(jobs).toHaveCount(0);
	await expect(total).toHaveText(/^2 of \d+ items$/);

	// With Running it shows both.
	await chip(page, "Running").click();
	await expect(page).toHaveURL(/[?&]state=scheduled(,|%2C)running$/);
	await expect(scheduled).toHaveCount(2);
	await expect(jobs).toHaveCount(1);
	await expect(total).toHaveText(/^3 of \d+ items$/);

	// A reload keeps the selection.
	await page.reload();
	await expect(chip(page, "Scheduled")).toHaveAttribute("aria-pressed", "true");
	await expect(chip(page, "Running")).toHaveAttribute("aria-pressed", "true");
	await expect(scheduled).toHaveCount(2);
	await expect(jobs).toHaveCount(1);

	// The kanban follows the chips: a Scheduled column and a Running one.
	await page.getByTestId("kanban-toggle").click();
	const columns = page.getByTestId("kanban-column");
	await expect(columns).toHaveCount(2);
	await expect(columns.nth(0)).toHaveAttribute("data-state", "scheduled");
	await expect(columns.nth(1)).toHaveAttribute("data-state", "running");
	await page.getByTestId("kanban-toggle").click();

	// Deselecting Scheduled while Running stays selected hides them; the
	// scheduled task's running run is a job, so it shows under Running.
	await chip(page, "Scheduled").click();
	await expect(page).toHaveURL(/[?&]state=running$/);
	await expect(scheduled).toHaveCount(0);
	await expect(jobs).toHaveCount(1);
	await expect(jobs.getByTestId("job-scheduled")).toHaveText("Scheduled");
	await expect(jobs.getByRole("heading")).toHaveText("nightly-triage");
});

test("the Board | Schedules toggle is gone and the board links to Scheduled Tasks", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/system/watches/lsegal%2Faviary");
	await expect(page.getByTestId("scheduled-card")).toHaveCount(2);
	await expect(
		page.getByRole("tablist", { name: "Project views" }),
	).toHaveCount(0);
	await expect(page.getByRole("tab", { name: /^Schedules/ })).toHaveCount(0);
	await expect(
		page.getByRole("link", { name: "Manage in Scheduled Tasks" }),
	).toHaveAttribute("href", "/agents/assistant/tasks");
});

for (const query of ["tab=schedules", "view=schedules"])
	test(`the old Schedules URL ?${query} lands on the board with only Scheduled selected`, async ({
		page,
	}) => {
		await setup(page);
		await page.goto(`/system/watches/lsegal%2Faviary?${query}`);
		await expect(page).toHaveURL(/\?state=scheduled$/);
		for (const label of [
			"Scheduled",
			"Starting",
			"Paused",
			"Running",
			"Done",
			"Failed",
		])
			await expect(chip(page, label)).toHaveAttribute(
				"aria-pressed",
				label === "Scheduled" ? "true" : "false",
			);
		await expect(page.getByTestId("scheduled-card")).toHaveCount(2);
		await expect(page.getByTestId("job-card")).toHaveCount(0);
	});

test("an old Schedules URL for one schedule opens its editor on the board", async ({
	page,
}) => {
	await setup(page);
	await page.goto(
		"/system/watches/lsegal%2Faviary?tab=schedules&schedule=assistant/nightly-triage",
	);
	await expect(page).toHaveURL(/state=scheduled/);
	await expect(page).toHaveURL(/scheduled=assistant(%2F|\/)nightly-triage/);
	await expect(page).not.toHaveURL(/tab=/);
	await expect(
		page.getByTestId("schedule-editor").getByLabel("Name"),
	).toHaveValue("nightly-triage");
});
