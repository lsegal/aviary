import { expect, type Page, test } from "@playwright/test";
import { clickCardAt } from "./helpers/cards";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";
import { chooseOption, optionLabels, searchOption } from "./helpers/select";
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

// The schedule's one board entry, as project_job_list reports it for
// kinds: ["schedule"]: its latest run, with its last and next run and recent
// runs, in place of a job per fire (#977).
const PREVIOUS_FIRE = FIRE - 86_400;
const SCHEDULED_ENTRY = {
	...SCHEDULED_JOB,
	state: "running",
	last_run: {
		key: `lsegal/aviary#${PREVIOUS_FIRE}`,
		number: PREVIOUS_FIRE,
		status: "complete",
		state: "done",
		result: "succeeded",
		summary: "Labeled 4 issues and opened #362 for a duplicate report",
		started: minutesFromNow(-1440),
		finished: minutesFromNow(-1435),
		duration_ms: 312000,
	},
	next_run: { at: minutesFromNow(42), paused: false, queued: false },
	recent_runs: [
		{
			key: `lsegal/aviary#${FIRE}`,
			number: FIRE,
			status: "active",
			state: "running",
			started: minutesFromNow(-2),
		},
		{
			key: `lsegal/aviary#${PREVIOUS_FIRE}`,
			number: PREVIOUS_FIRE,
			status: "complete",
			state: "done",
			result: "succeeded",
			summary: "Labeled 4 issues and opened #362 for a duplicate report",
			started: minutesFromNow(-1440),
			finished: minutesFromNow(-1435),
			duration_ms: 312000,
		},
	],
};

// An issue's job, to share the board with the scheduled cards.
const ISSUE_JOB = {
	...SCHEDULED_JOB,
	key: "lsegal/aviary#7",
	number: 7,
	title: "Fix the parser",
	kind: "issue",
	schedule_id: undefined,
	state: "running",
	checkout_directory: "/tmp/aviary-7",
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
	opts: {
		upcoming?: unknown[];
		owner?: string;
		tasks?: unknown[];
		/** The board's jobs; its schedules' entries are entries. */
		jobs?: unknown[];
		entries?: unknown[];
		job?: unknown;
	} = {},
) {
	await setAuthToken(page);
	const calls: Record<string, Args[]> = {};
	const tasks: unknown[] = [...(opts.tasks ?? TASKS)];
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
				{
					name: "assistant",
					model: "cli/codex/gpt-5.5",
					channels: [{ type: "discord", id: "ops", name: "ops" }],
				},
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
		// A project's board reads its jobs and its schedules' entries apart.
		project_job_list: (args?: Args) => {
			record("project_job_list")(args);
			const kinds = args?.kinds as string[] | undefined;
			const states = args?.states as string[] | undefined;
			const list = kinds?.includes("schedule")
				? (opts.entries ?? [SCHEDULED_ENTRY])
				: (opts.jobs ?? []).filter(
						(j) =>
							!states ||
							states.includes(String((j as Args).state ?? "running")),
					);
			return { jobs: list, total: list.length };
		},
		project_job_dismiss: record("project_job_dismiss"),
		project_job_get: opts.job ?? {
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
		// Project tasks are analyzed as their compiled script would run.
		task_analyze: (args?: Args) => {
			record("task_analyze")(args);
			return {
				verdict: "deterministic",
				will_precompile: args?.precompile === true,
				reason: "fixed gh commands",
				estimate:
					args?.precompile === true
						? { available: true, min_tokens: 0, max_tokens: 0 }
						: { available: true, min_tokens: 4600, max_tokens: 12400 },
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

test("an agent board shows a scheduled run as a job card while it runs", async ({
	page,
}) => {
	const calls = await setup(page, {
		owner: "coder",
		jobs: [{ ...SCHEDULED_JOB, state: "running" }],
	});
	await page.goto("/agents/coder/projects");

	const card = page.getByTestId("job-card");
	await expect(card).toHaveCount(1);
	await expect(card.getByTestId("job-scheduled")).toHaveText("Scheduled");
	await expect(card.getByRole("heading")).toHaveText("nightly-triage");
	// The label names the task as its title does, unbroken (#958).
	const scheduleTask = card.getByTestId("job-schedule-task");
	await expect(scheduleTask).toHaveText("nightly-triage");
	await expect(scheduleTask).toHaveAttribute(
		"title",
		"assistant/nightly-triage",
	);
	await expect(scheduleTask).toHaveCSS("white-space", "nowrap");
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
	// An agent board lists jobs in their job states, so a schedule between
	// runs, which is in none, never shows as a job card (#977).
	expect(calls.project_job_list?.at(-1)?.states).toEqual([
		"starting",
		"paused",
		"running",
		"done",
		"failed",
	]);
});

test("a project board shows the up next strip", async ({ page }) => {
	const calls = await setup(page);
	await page.goto("/system/watches/lsegal%2Faviary");

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
	page.on("console", (m) => console.log("CONSOLE", m.type(), m.text()));
	page.on("pageerror", (e) => console.log("PAGEERR", e.message));
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
	// The output area shows the run's output, not the last finished result.
	await expect(nightly.getByTestId("scheduled-last")).toHaveCount(0);
	await expect(nightly.getByTestId("scheduled-agent")).toHaveText(
		"assistant · cli/claude/opus",
	);
	await expect(boxes.nth(1).getByTestId("scheduled-next")).toHaveText("Paused");
	await expect(boxes.nth(1).getByTestId("scheduled-last")).toHaveText(
		"Hasn't run yet",
	);

	// A recurring schedule's run in flight shows on its own card, as its
	// state and the control that opens the run, beside its next fire time
	// and its last finished result; no job card copies it (#977).
	const running = nightly.getByTestId("scheduled-running");
	await expect(running).toHaveText(/^\s*Running now · [12]m\s*$/);
	await expect(nightly.getByTestId("scheduled-state")).toHaveText("Running");
	await expect(page.getByTestId("job-card")).toHaveCount(0);
	// The pencil to its editor stays beside it.
	await expect(
		nightly.getByRole("link", { name: "Edit schedule" }),
	).toBeVisible();
	// It shows the run's output like an issue card does.
	await expect(nightly.getByTestId("scheduled-output")).toHaveText(
		"Reading the issues opened since 02:00 yesterday…",
	);
	await expect(nightly.getByTestId("output-mode")).toBeVisible();
	await running.click();
	await expect(page).toHaveURL(new RegExp(`/jobs/${FIRE}`));
	await page.goto("/system/watches/lsegal%2Faviary");
	await expect(boxes).toHaveCount(2);
	// A schedule with no run has an empty output area, no running control
	// and the expand link to its editor.
	const weekly = boxes.nth(1);
	await expect(weekly.getByTestId("scheduled-running")).toHaveCount(0);
	await expect(weekly.getByTestId("scheduled-output")).toHaveCount(0);
	await expect(weekly.getByTestId("scheduled-empty")).toBeVisible();
	await expect(weekly.getByTestId("output-mode")).toHaveCount(0);
	await expect(
		weekly.getByRole("link", { name: "Edit schedule" }),
	).toBeVisible();

	// The search narrows scheduled boxes with the jobs. A state filter shows
	// only the schedules in that state: the running one under Running, and
	// none under Done (#977).
	await page.getByLabel("Search jobs").fill("dependencies");
	await expect(boxes).toHaveCount(1);
	await expect(boxes.first()).toContainText("weekly-deps");
	await page.getByLabel("Search jobs").fill("");
	const filter = page.getByRole("group", { name: "Job filter" });
	await filter.getByRole("button", { name: /^Running/ }).click();
	await expect(boxes).toHaveCount(1);
	await expect(boxes.first()).toContainText("nightly-triage");
	await filter.getByRole("button", { name: /^Running/ }).click();
	await filter.getByRole("button", { name: /^Done/ }).click();
	await expect(boxes).toHaveCount(0);
});

test("scheduled cards are compact: trigger and next run share one line (#875)", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/system/watches/lsegal%2Faviary");
	const weekly = page.getByTestId("scheduled-card").nth(1);
	await expect(weekly.getByTestId("scheduled-trigger")).toBeVisible();
	const details = await weekly
		.getByTestId("scheduled-run-details")
		.boundingBox();
	const trigger = await weekly.getByTestId("scheduled-trigger").boundingBox();
	const next = await weekly.getByTestId("scheduled-next").boundingBox();
	expect(details!.height).toBeLessThan(24);
	expect(Math.abs(trigger!.y - next!.y)).toBeLessThan(4);
});

for (const viewport of [
	{ name: "desktop", width: 1280, height: 900 },
	{ name: "mobile", width: 390, height: 844 },
]) {
	test(`scheduled and job cards share a row's height on ${viewport.name} (#948)`, async ({
		page,
	}) => {
		await page.setViewportSize(viewport);
		await setup(page, {
			tasks: [TASKS[1]],
			jobs: [ISSUE_JOB],
		});
		await page.goto("/system/watches/lsegal%2Faviary");
		const scheduled = page.getByTestId("scheduled-card");
		const job = page.getByTestId("job-card");
		await expect(scheduled).toHaveCount(1);
		await expect(job).toHaveCount(1);
		// A never-run schedule still has the output area, with its empty state.
		await expect(scheduled.getByTestId("scheduled-empty")).toBeVisible();
		const s = (await scheduled.boundingBox())!;
		const j = (await job.boundingBox())!;
		if (viewport.name === "desktop") {
			expect(Math.abs(s.y - j.y)).toBeLessThan(1);
			expect(Math.abs(s.height - j.height)).toBeLessThan(1);
		} else {
			// Stacked on a phone, each card is as tall as a job card's layout.
			expect(Math.abs(s.x - j.x)).toBeLessThan(1);
			expect(s.height).toBeGreaterThan(200);
		}
		// The footer action rows line up at each card's bottom.
		const footerBottom = async (card: typeof job, name: string) => {
			const b = (await card.getByRole("link", { name }).boundingBox())!;
			return b.y + b.height;
		};
		const sBottom = await footerBottom(scheduled, "Edit schedule");
		const jBottom = await footerBottom(job, "Details");
		expect(
			Math.abs(s.y + s.height - sBottom - (j.y + j.height - jBottom)),
		).toBeLessThan(2);
	});
}

test("a scheduled card's footer runs, pauses and opens settings like a job card's (#948)", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto("/system/watches/lsegal%2Faviary");
	const weekly = page.getByTestId("scheduled-card").nth(1);
	await expect(weekly.getByTestId("scheduled-agent")).toHaveText("assistant");

	await weekly.getByRole("button", { name: "Run now" }).click();
	await expect.poll(() => calls.task_run?.length ?? 0).toBe(1);
	expect(calls.task_run[0]).toMatchObject({ name: "assistant/weekly-deps" });

	// Paused, it offers Resume; resuming saves it enabled.
	await expect(weekly.getByRole("button", { name: "Pause" })).toHaveCount(0);
	await weekly.getByRole("button", { name: "Resume" }).click();
	await expect.poll(() => calls.task_update?.length ?? 0).toBe(1);
	expect(calls.task_update[0]).toMatchObject({
		name: "assistant/weekly-deps",
		enabled: true,
	});
	const nightly = page.getByTestId("scheduled-card").first();
	await nightly.getByRole("button", { name: "Pause" }).click();
	await expect.poll(() => calls.task_update?.length ?? 0).toBe(2);
	expect(calls.task_update[1]).toMatchObject({
		name: "assistant/nightly-triage",
		enabled: false,
	});

	// Settings opens the shared task dialog, as the sidebar's gear does.
	await weekly.getByRole("button", { name: "Settings" }).click();
	await expect(page.getByRole("dialog")).toBeVisible();
});

test("a scheduled card opens its task's page, like its sidebar row (#950)", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/system/watches/lsegal%2Faviary");
	const weekly = page.getByTestId("scheduled-card").nth(1);
	await expect(weekly).toContainText("weekly-deps");

	// The name is a real link to the task's page, stretched over the card.
	const link = weekly.getByTestId("scheduled-card-link");
	await expect(link).toHaveAttribute(
		"href",
		"/agents/assistant/tasks/weekly-deps",
	);
	await clickCardAt(weekly, weekly.getByTestId("scheduled-trigger"));
	await expect(page).toHaveURL(/\/agents\/assistant\/tasks\/weekly-deps$/);

	// Kanban cards follow the same rules: card → task page, pencil → editor.
	await page.goto("/system/watches/lsegal%2Faviary");
	await page.getByTestId("kanban-toggle").click();
	const kanban = page
		.getByTestId("kanban-scheduled-card")
		.filter({ hasText: "weekly-deps" });
	await kanban.getByRole("link", { name: "Edit schedule" }).click();
	await expect(page).toHaveURL(/scheduled=assistant(%2F|\/)weekly-deps/);
	await page.goto("/system/watches/lsegal%2Faviary");
	await clickCardAt(kanban, kanban.getByTestId("kanban-scheduled-prompt"));
	await expect(page).toHaveURL(/\/agents\/assistant\/tasks\/weekly-deps$/);
});

// A one-time schedule moves through these states: armed, fired and running
// (the scheduler lists no next run once it fires), then finished.
const ONCE_AT = minutesFromNow(-1);
const onceTask = (fields: Record<string, unknown>) =>
	schedule({
		id: "assistant/one-off",
		name: "one-off",
		prompt: "Summarize this week's merged pull requests.",
		enabled: true,
		trigger_type: "once",
		start_at: ONCE_AT,
		description: "once at 2026-11-21 19:11 (Local)",
		...fields,
	});
const ONCE_JOB = {
	...SCHEDULED_JOB,
	key: "lsegal/aviary#1795424500",
	number: 1795424500,
	title: "one-off",
	schedule_id: "assistant/one-off",
	scrollback: "Collecting merged pull requests…",
	events: [{ kind: "message", text: "Found 12 merged pull requests." }],
};

const ONCE_RUN = {
	key: ONCE_JOB.key,
	number: ONCE_JOB.number,
	started: ONCE_AT,
};

for (const tc of [
	{
		state: "before",
		task: onceTask({ next_run: minutesFromNow(30), history: [] }),
		entries: [],
		next: /^Next run in (29|30)m \(.+\)$/,
		running: false,
	},
	{
		state: "during",
		task: onceTask({
			history: [{ fire_time: ONCE_AT, reason: "schedule", status: "running" }],
		}),
		entries: [
			{
				...ONCE_JOB,
				state: "running",
				next_run: { paused: false, queued: false },
				recent_runs: [{ ...ONCE_RUN, status: "active", state: "running" }],
			},
		],
		next: null,
		running: true,
	},
	{
		state: "after",
		task: onceTask({
			history: [
				{
					fire_time: ONCE_AT,
					reason: "schedule",
					status: "completed",
					summary: "Posted the weekly summary",
				},
			],
		}),
		entries: [
			{
				...ONCE_JOB,
				status: "complete",
				state: "scheduled",
				actions: {},
				next_run: { paused: false, queued: false },
				last_run: {
					...ONCE_RUN,
					status: "complete",
					state: "done",
					result: "succeeded",
					summary: "Posted the weekly summary",
					finished: ONCE_AT,
				},
				recent_runs: [
					{
						...ONCE_RUN,
						status: "complete",
						state: "done",
						result: "succeeded",
						summary: "Posted the weekly summary",
					},
				],
			},
		],
		next: /^No next run$/,
		running: false,
	},
]) {
	test(`a one-time schedule's next run ${tc.state} its run (#875)`, async ({
		page,
	}) => {
		await setup(page, {
			tasks: [tc.task],
			entries: tc.entries,
			job: tc.entries[0],
		});
		await page.goto("/system/watches/lsegal%2Faviary");
		const card = page.getByTestId("scheduled-card");
		await expect(card).toHaveCount(1);
		await expect(card.getByTestId("scheduled-trigger")).toHaveText(
			"once at 2026-11-21 19:11 (Local)",
		);
		await expect(card).not.toContainText("none");
		if (tc.next)
			await expect(card.getByTestId("scheduled-next")).toHaveText(tc.next);
		else await expect(card.getByTestId("scheduled-next")).toHaveCount(0);
		await expect(card.getByTestId("scheduled-running")).toHaveCount(
			tc.running ? 1 : 0,
		);

		const kanbanNext = async () => {
			await page.getByTestId("kanban-toggle").click();
			const text = await page
				.getByTestId("kanban-scheduled-next")
				.textContent();
			await page.getByTestId("kanban-toggle").click();
			return text?.trim();
		};
		if (tc.state === "during") {
			await expect(card.getByTestId("scheduled-running")).toHaveText(
				/^\s*Running now · \d+[sm]\s*$/,
			);
			await expect(card.getByTestId("scheduled-last")).toHaveCount(0);
			// The run's output shows in rich and raw mode, like an issue card.
			await expect(card.getByTestId("scheduled-output-rich")).toContainText(
				"Found 12 merged pull requests.",
			);
			await card.getByRole("button", { name: "Raw output" }).click();
			await expect(card.getByTestId("scheduled-output")).toHaveText(
				"Collecting merged pull requests…",
			);
			await card.getByRole("button", { name: "Rich output" }).click();
			expect(await kanbanNext()).toBe("Running now");
		}
		if (tc.state === "after") {
			// The card is back to Scheduled with the finished run's result and
			// summary in place of its output (#977).
			await expect(card.getByTestId("scheduled-state")).toHaveText("Scheduled");
			await expect(card.getByTestId("scheduled-output-rich")).toHaveCount(0);
			await expect(card.getByTestId("scheduled-last-summary")).toHaveText(
				"Posted the weekly summary",
			);
			await expect(card.getByTestId("scheduled-last-result")).toHaveText("✓");
			expect(await kanbanNext()).toBe("No next run");
		}
		if (tc.state === "before") {
			await expect(card.getByTestId("scheduled-last")).toHaveText(
				/^\s*Hasn't run yet · next run in (29|30)m\s*$/,
			);
			await expect(card.getByTestId("scheduled-output")).toHaveCount(0);
		}
	});
}

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

test("a scheduled card's pencil opens its editor within the project page", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto("/system/watches/lsegal%2Faviary");
	await page
		.getByTestId("scheduled-card")
		.first()
		.getByRole("link", { name: "Edit schedule" })
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
	// The analyzer previews the task as its script would run in the clone.
	const feedback = editor.getByTestId("task-cost-feedback");
	await expect(feedback.getByTestId("task-cost-verdict")).toHaveText(
		"This task can run deterministically",
	);
	await expect(feedback.getByTestId("task-cost-estimate")).not.toHaveText(
		"≈ 0 tokens/run",
	);
	expect(calls.task_analyze?.at(-1)).toMatchObject({
		agent: "assistant",
		type: "project",
		project: "lsegal/aviary",
		precompile: false,
	});
	// Turning precompile on shows the cost of the compiled script's runs.
	const precompile = editor.getByLabel("Try to precompute task");
	await expect(precompile).not.toBeChecked();
	await precompile.check();
	await expect(feedback.getByTestId("task-cost-estimate")).toHaveText(
		"≈ 0 tokens/run",
	);
	await expect(feedback.getByTestId("task-cost-note")).toContainText(
		"runs in a clone of the project with no model calls",
	);
	await precompile.uncheck();
	const runs = detail.getByTestId("schedule-run");
	await expect(runs).toHaveCount(2);
	await expect(runs.first()).toContainText("Running");
	await expect(page.getByTestId("job-card")).toHaveCount(0);

	await editor.getByRole("button", { name: "Run now" }).click();
	await expect
		.poll(() => calls.task_run?.[0])
		.toEqual({ name: "assistant/nightly-triage" });

	// The other card's pencil opens its own editor.
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
	await expect(page.getByTestId("scheduled-card")).toHaveCount(2);
	await expect(page.getByTestId("up-next")).toHaveCount(0);
});

test("a watch no agent owns opens no add-project dialog", async ({ page }) => {
	await setup(page);
	await page.goto("/system/watches/lsegal%2Faviary?add=1");
	await expect(page.getByTestId("scheduled-card")).toHaveCount(2);
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
	const runs = page.getByTestId("schedule-detail").getByTestId("schedule-run");
	await expect(runs).toHaveCount(2);
	// Most recent first.
	await expect(runs.first()).toContainText("Running");

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
	await expect(model.getByRole("combobox")).toHaveValue("cli/claude/opus");
	await searchOption(model.getByRole("combobox"), "cli/codex/gpt-5.5");
	await expect(model.getByRole("combobox")).toHaveValue("cli/codex/gpt-5.5");
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
			precompile: false,
		});

	// Precompile is saved with the rest of the schedule.
	await editor.getByLabel("Try to precompute task").check();
	await editor.getByRole("button", { name: "Save" }).click();
	await expect
		.poll(() => calls.task_update?.at(-1))
		.toMatchObject({ name: "assistant/nightly-triage", precompile: true });

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
	// The board opens the shared dialog targeting this project; the project's
	// agent is implied, so there is no Agent field, and project tasks run a
	// prompt, so Script is unavailable.
	await expect(
		dialog.getByRole("radiogroup", { name: "Target" }).getByRole("radio", {
			name: "Project",
		}),
	).toHaveAttribute("aria-checked", "true");
	await expect(dialog.getByLabel("Project", { exact: true })).toHaveValue(
		"lsegal/aviary",
	);
	await expect(dialog.getByLabel("Agent", { exact: true })).toHaveCount(0);
	await expect(dialog.getByRole("radio", { name: "Script" })).toBeDisabled();
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
	// Project tasks precompile like agent tasks: on by default, with the
	// same live feedback.
	const precompute = dialog.getByLabel("Try to precompute task");
	await expect(precompute).toBeEnabled();
	await expect(precompute).toBeChecked();
	const feedback = dialog.getByTestId("task-cost-feedback");
	await expect(feedback.getByTestId("task-cost-verdict")).toHaveText(
		"This task can run deterministically",
	);
	await expect(feedback.getByTestId("task-cost-estimate")).toHaveText(
		"≈ 0 tokens/run",
	);
	expect(calls.task_analyze?.at(-1)).toMatchObject({
		type: "project",
		project: "lsegal/aviary",
		precompile: true,
	});
	// Send To offers the agent's channels by name.
	const sendTo = dialog.getByLabel("Send To");
	expect(await optionLabels(sendTo)).toEqual([
		"Task session only",
		"ops · discord",
	]);
	await chooseOption(sendTo, "ops · discord");
	await dialog.getByRole("button", { name: "Create" }).click();
	await expect(dialog).toBeHidden();
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
	await card.getByRole("link", { name: "Edit schedule" }).click();
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
		await card.getByRole("link", { name: "Edit schedule" }).click();
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
		.getByRole("link", { name: "Edit schedule" })
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
	await setup(page, { jobs: [ISSUE_JOB] });
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

	// Deselecting Scheduled while Running stays selected leaves the running
	// schedule on its own card, never a job card of its run (#977).
	await chip(page, "Scheduled").click();
	await expect(page).toHaveURL(/[?&]state=running$/);
	await expect(scheduled).toHaveCount(1);
	await expect(scheduled.getByRole("heading")).toHaveText("nightly-triage");
	await expect(jobs).toHaveCount(1);
	await expect(jobs.getByRole("heading")).toContainText("Fix the parser");
});

test("a mobile board folds the filter chips into a Filter button", async ({
	page,
}) => {
	await page.setViewportSize({ width: 375, height: 812 });
	await setup(page, { jobs: [ISSUE_JOB] });
	await page.goto("/system/watches/lsegal%2Faviary");
	const toolbar = page.getByTestId("job-toolbar");
	const toggle = page.getByTestId("filter-toggle");
	const group = page.getByRole("group", { name: "Job filter" });

	// Collapsed: search, one Filter button, the layout toggle and the count.
	await expect(
		toolbar.getByRole("searchbox", { name: "Search jobs" }),
	).toBeVisible();
	await expect(toggle).toBeVisible();
	await expect(toggle).toHaveAttribute("aria-expanded", "false");
	await expect(toggle).toHaveText("Filter: All");
	await expect(group).toBeHidden();
	await expect(page.getByTestId("kanban-toggle")).toBeVisible();
	await expect(page.getByTestId("job-total")).toHaveText("3 items");
	// The search keeps a usable width; Filter, layout toggle and count share a row.
	const searchBox = await toolbar
		.getByRole("searchbox", { name: "Search jobs" })
		.boundingBox();
	expect(searchBox?.width ?? 0).toBeGreaterThan(200);
	const rowOf = async (testId: string) => {
		const box = await page.getByTestId(testId).boundingBox();
		return Math.round((box?.y ?? 0) + (box?.height ?? 0) / 2);
	};
	const filterRow = await rowOf("filter-toggle");
	expect(Math.abs((await rowOf("kanban-toggle")) - filterRow)).toBeLessThan(6);
	expect(Math.abs((await rowOf("job-total")) - filterRow)).toBeLessThan(6);

	// Opening lists the same chips and counts; toggling works as inline.
	await toggle.click();
	await expect(toggle).toHaveAttribute("aria-expanded", "true");
	await expect(group).toBeVisible();
	await expect(chip(page, "Scheduled")).toHaveText(/^Scheduled\s*2$/);
	await chip(page, "Running").click();
	await expect(page).toHaveURL(/[?&]state=running$/);
	await expect(chip(page, "Running")).toHaveAttribute("aria-pressed", "true");
	await chip(page, "Scheduled").click();
	await expect(toggle).toHaveText(/^Filter: Scheduled 2, Running \d+$/);

	// Esc closes the panel and returns focus to the button.
	await page.keyboard.press("Escape");
	await expect(group).toBeHidden();
	await expect(toggle).toBeFocused();
	await expect(toggle).toHaveAttribute("aria-expanded", "false");

	// The keyboard opens it again; Clear empties the selection.
	await page.keyboard.press("Enter");
	await expect(group).toBeVisible();
	await group.getByRole("button", { name: "Clear" }).click();
	await expect(page).not.toHaveURL(/state=/);
	await expect(toggle).toHaveText("Filter: All");

	// A tap outside closes it.
	await page.getByTestId("job-total").click();
	await expect(group).toBeHidden();

	// The selection persists across a reload.
	await toggle.click();
	await chip(page, "Running").click();
	await page.reload();
	await expect(toggle).toHaveText(/^Filter: Running \d+$/);

	// Desktop widths show the inline chips and no Filter button.
	await page.setViewportSize({ width: 1280, height: 720 });
	await expect(toggle).toBeHidden();
	await expect(group).toBeVisible();
	await expect(chip(page, "Running")).toHaveAttribute("aria-pressed", "true");
});

test("a mobile agent board's Filter panel has no Scheduled option", async ({
	page,
}) => {
	await page.setViewportSize({ width: 375, height: 812 });
	await setup(page, { owner: "coder" });
	await page.goto("/agents/coder/projects");
	await page.getByTestId("filter-toggle").click();
	const group = page.getByRole("group", { name: "Job filter" });
	await expect(group.getByRole("button", { name: /^Running/ })).toBeVisible();
	await expect(group.getByRole("button", { name: /^Scheduled/ })).toHaveCount(
		0,
	);
});

test("the Board | Schedules toggle and Scheduled Tasks link are gone", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/system/watches/lsegal%2Faviary");
	await expect(page.getByTestId("scheduled-card")).toHaveCount(2);
	await expect(
		page.getByRole("tablist", { name: "Project views" }),
	).toHaveCount(0);
	await expect(page.getByRole("tab", { name: /^Schedules/ })).toHaveCount(0);
	// The sidebar's Scheduled Tasks page lists every agent task (#906).
	await expect(
		page.getByRole("link", { name: "Manage in Scheduled Tasks" }),
	).toHaveCount(0);
	await expect(page.getByTestId("manage-scheduled-tasks")).toHaveCount(0);
	await expect(
		page
			.getByTestId("sidebar-nav")
			.locator('a[href="/agents/assistant/tasks"]'),
	).toHaveCount(1);
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
