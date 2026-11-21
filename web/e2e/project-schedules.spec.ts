import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

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
	ready_label: "ready",
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
		project_status: STATUS,
		project_job_list: { jobs: [SCHEDULED_JOB], total: 1 },
		project_job_get: {
			...SCHEDULED_JOB,
			scrollback: "Reading the issues opened since 02:00 yesterday…",
			events: [],
		},
		project_job_stop: record("project_job_stop"),
		task_list: TASKS,
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

test("up next strip is hidden when no schedule is enabled", async ({
	page,
}) => {
	await setup(page, { upcoming: [] });
	await page.goto("/system/watches/lsegal%2Faviary");
	await expect(page.getByTestId("job-card")).toHaveCount(1);
	await expect(page.getByTestId("up-next")).toHaveCount(0);
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
	await expect(
		editor.getByTestId("schedule-preview").locator("li"),
	).toHaveCount(3);
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

test("new schedule creates a project task", async ({ page }) => {
	const calls = await setup(page);
	await page.goto("/system/watches/lsegal%2Faviary?tab=schedules");
	await page.getByTestId("schedule-new").click();
	const editor = page.getByTestId("schedule-editor");
	await editor.getByLabel("Name").fill("hourly-ci-watch");
	await editor.getByLabel("Owner agent").selectOption("reviewer");
	await editor.getByLabel("Prompt").fill("Rerun flaky CI jobs on open PRs.");
	await editor.getByRole("radio", { name: "Interval" }).click();
	await editor.getByLabel("Every").fill("1h");
	await editor.getByLabel(/still going/).selectOption("skip");
	await editor.getByRole("button", { name: "Save" }).click();
	await expect
		.poll(() => calls.task_schedule?.[0])
		.toEqual({
			trigger: "interval",
			schedule: "",
			every: "1h",
			at: "",
			days: "",
			start_at: "",
			timezone: "",
			overlap: "skip",
			missed: "skip",
			content: "Rerun flaky CI jobs on open PRs.",
			model: "",
			agent: "reviewer",
			name: "hourly-ci-watch",
			type: "project",
			project: "lsegal/aviary",
		});
	await expect(page).toHaveURL(/schedule=reviewer\/hourly-ci-watch/);
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
