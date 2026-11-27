import { expect, type Locator, type Page, test } from "@playwright/test";
import { clickCardAt } from "./helpers/cards";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";
import { chooseOption, optionLabels } from "./helpers/select";
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
		/** The owner's projects; lsegal/aviary alone by default. */
		projects?: string[];
		/** Each project's board state counts, which project_status sums. */
		states?: Record<string, Record<string, number>>;
		tasks?: unknown[];
		/** The board's jobs; its schedules' entries are entries. */
		jobs?: unknown[];
		entries?: unknown[];
		job?: unknown;
		/** More tool fixtures, given the task list the board reads. */
		extra?: (tasks: unknown[]) => Record<string, unknown>;
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
								projects: (opts.projects ?? ["lsegal/aviary"]).map(
									(target) => ({ target }),
								),
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
		// The counts are the named projects' sums, as boardStateCounts does.
		project_status: (args?: Args) => {
			if (!opts.states) return STATUS;
			const states: Record<string, number> = {};
			for (const [target, counts] of Object.entries(opts.states)) {
				const targets = args?.targets as string[] | undefined;
				if (targets?.length && !targets.includes(target)) continue;
				for (const [state, n] of Object.entries(counts))
					states[state] = (states[state] ?? 0) + n;
			}
			return { ...STATUS, targets: opts.projects ?? STATUS.targets, states };
		},
		// A project's board reads its jobs and its schedules' entries apart,
		// each limited to its projects and the kinds it names.
		project_job_list: (args?: Args) => {
			record("project_job_list")(args);
			const kinds = args?.kinds as string[] | undefined;
			const states = args?.states as string[] | undefined;
			const targets = args?.targets as string[] | undefined;
			const list = (
				kinds?.includes("schedule")
					? (opts.entries ?? [SCHEDULED_ENTRY])
					: (opts.jobs ?? []).filter(
							(j) =>
								(!states ||
									states.includes(String((j as Args).state ?? "running"))) &&
								(!kinds || kinds.includes(String((j as Args).kind))),
						)
			).filter((j) => !targets || targets.includes(String((j as Args).target)));
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
		...opts.extra?.(tasks),
	});
	return calls;
}

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

	// A run in the strip opens its task's page, as its card does.
	await strip.getByTestId("up-next-run").first().click();
	await expect(page).toHaveURL(/\/agents\/assistant\/tasks\/nightly-triage$/);
});

test("board lists scheduled tasks as their own boxes with run details", async ({
	page,
}) => {
	page.on("console", (m) => console.log("CONSOLE", m.type(), m.text()));
	page.on("pageerror", (e) => console.log("PAGEERR", e.message));
	await setup(page);
	await page.goto("/system/watches/lsegal%2Faviary");

	const boxes = page.getByTestId("scheduled-card");
	// Only this project's scheduled tasks, by their next run.
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
	await expect(boxes.nth(1).getByTestId("scheduled-next")).toHaveText(
		"Next run: paused",
	);
	await expect(boxes.nth(1).getByTestId("paused-chip")).toBeVisible();
	await expect(boxes.nth(1).getByTestId("scheduled-last")).toHaveText(
		"Hasn't run yet",
	);

	// A recurring schedule's run in flight shows on its own card, as its
	// state and the control that opens the run, beside its next fire time
	// and its last finished result; no job card copies it (#977).
	const running = nightly.getByTestId("scheduled-running");
	await expect(running).toHaveText(/^\s*Running · [12]m\s*$/);
	await expect(nightly.getByTestId("scheduled-state")).toHaveText("Running");
	await expect(page.getByTestId("job-card")).toHaveCount(0);
	// The gear to its task settings stays beside it.
	await expect(
		nightly.getByRole("button", { name: "Task settings" }),
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
	// and the gear to its task settings.
	const weekly = boxes.nth(1);
	await expect(weekly.getByTestId("scheduled-running")).toHaveCount(0);
	await expect(weekly.getByTestId("scheduled-output")).toHaveCount(0);
	await expect(weekly.getByTestId("scheduled-empty")).toBeVisible();
	await expect(weekly.getByTestId("output-mode")).toHaveCount(0);
	await expect(
		weekly.getByRole("button", { name: "Task settings" }),
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

test("scheduled cards sit behind running jobs unless their next run is within five minutes (#1207)", async ({
	page,
}) => {
	const at = (name: string, minutes: number, fields = {}) =>
		schedule({
			id: `assistant/${name}`,
			name,
			prompt: `Run ${name}.`,
			enabled: true,
			every: "1d",
			next_run: minutesFromNow(minutes),
			...fields,
		});
	const issue = (number: number) => ({
		...ISSUE_JOB,
		key: `lsegal/aviary#${number}`,
		number,
		title: `Issue ${number}`,
	});
	await page.clock.install();
	await setup(page, {
		// Listed by name; a running task sorts by its next run, never by when
		// its run started.
		tasks: [
			at("a-two-hours", 120),
			at("b-running", 50, {
				history: [
					{
						fire_time: minutesFromNow(-1),
						reason: "schedule",
						status: "running",
					},
				],
			}),
			at("c-paused", 1, { enabled: false }),
			at("d-twenty", 20),
			at("e-three", 3),
		],
		jobs: [issue(11), issue(12)],
		entries: [],
	});
	await page.goto("/system/watches/lsegal%2Faviary");

	const order = page
		.getByRole("region", { name: "Jobs" })
		.locator("article h3");
	await expect(order).toHaveText([
		"e-three",
		"#11 Issue 11",
		"#12 Issue 12",
		"d-twenty",
		"b-running",
		"a-two-hours",
		"c-paused",
	]);

	// Sixteen minutes on, the twenty-minute card is due within five minutes
	// and moves up behind the overdue one, with no refresh of the board.
	await page.clock.fastForward("16:00");
	await expect(order).toHaveText([
		"e-three",
		"d-twenty",
		"#11 Issue 11",
		"#12 Issue 12",
		"b-running",
		"a-two-hours",
		"c-paused",
	]);
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
		const footerBottom = async (
			card: typeof job,
			role: "button" | "link",
			name: string,
		) => {
			const b = (await card.getByRole(role, { name }).boundingBox())!;
			return b.y + b.height;
		};
		const sBottom = await footerBottom(scheduled, "button", "Task settings");
		const jBottom = await footerBottom(job, "link", "Details");
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

	// The gear opens the shared task dialog, as the sidebar's gear does.
	await weekly.getByRole("button", { name: "Task settings" }).click();
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

	// Kanban cards follow the same rules: card → task page, gear → settings.
	await page.goto("/system/watches/lsegal%2Faviary");
	await page.getByTestId("kanban-toggle").click();
	const kanban = page
		.getByTestId("kanban-scheduled-card")
		.filter({ hasText: "weekly-deps" });
	await kanban.getByRole("button", { name: "Task settings" }).click();
	await expect(page.getByRole("dialog", { name: "Edit task" })).toBeVisible();
	await page.keyboard.press("Escape");
	await expect(page.getByRole("dialog")).toHaveCount(0);
	await expect(page).toHaveURL(/\/system\/watches\/lsegal%2Faviary/);
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
				/^\s*Running · \d+[sm]\s*$/,
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

test("scheduled cards have no pencil; the gear is the last action and opens the full task settings (#1165)", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/system/watches/lsegal%2Faviary");
	const cards = page.getByTestId("scheduled-card");
	await expect(cards).toHaveCount(2);
	for (const card of await cards.all()) {
		// No pencil: nothing on the card edits the task but the gear.
		await expect(card.getByRole("link", { name: /edit/i })).toHaveCount(0);
		await expect(card.getByRole("button", { name: /edit/i })).toHaveCount(0);
		// Run now and Pause or Resume come first; the gear is the last action.
		const actions = card
			.getByTestId("scheduled-footer")
			.locator("a:visible, button:visible");
		const names = await actions.evaluateAll((els) =>
			els.map((e) => e.getAttribute("aria-label") ?? ""),
		);
		expect(names.at(-1)).toBe("Task settings");
		expect(names.indexOf("Run now")).toBeLessThan(names.length - 1);
		expect(
			Math.max(names.indexOf("Pause"), names.indexOf("Resume")),
		).toBeLessThan(names.length - 1);
		const gear = actions.last();
		await expect(gear).toHaveAttribute("title", "Task settings");
	}

	// The gear is reachable by keyboard and opens the full task settings,
	// with the newer task options such as its type, target and Send To.
	const gear = cards.first().getByRole("button", { name: "Task settings" });
	await gear.focus();
	await expect(gear).toBeFocused();
	await page.keyboard.press("Enter");
	const dialog = page.getByRole("dialog", { name: "Edit task" });
	await expect(dialog).toBeVisible();
	await expect(dialog.getByTestId("task-send-to")).toBeVisible();
	await expect(dialog.getByLabel("Task name")).toHaveValue("nightly-triage");
	await expect(dialog.getByRole("radiogroup", { name: "Type" })).toBeVisible();
	await expect(
		dialog.getByRole("radiogroup", { name: "Target" }),
	).toBeVisible();
	// The board stays where it was underneath.
	await expect(page).toHaveURL(/\/system\/watches\/lsegal%2Faviary$/);
});

test("a scheduled card's menu duplicates its task through the shared create form (#1465)", async ({
	page,
}) => {
	const calls = await setup(page, {
		extra: () => ({
			config_get: {
				agents: [
					{
						name: "assistant",
						model: "cli/codex/gpt-5.5",
						channels: [{ type: "discord", id: "ops", name: "ops" }],
						tasks: [
							{
								name: "nightly-triage",
								type: "project",
								project: "lsegal/aviary",
								prompt:
									"Triage issues opened since the last run and label them.",
								model: "cli/claude/opus",
								trigger: "daily",
								at: "02:00",
								timezone: "America/Toronto",
								overlap: "skip",
								missed: "run_once",
								precompile: true,
								compiled: "return 'stale'",
							},
						],
					},
				],
			},
		}),
	});
	await page.goto("/system/watches/lsegal%2Faviary");
	const nightly = page.getByTestId("scheduled-card").first();
	await nightly.getByTestId("scheduled-more").click();
	await page.getByTestId("scheduled-duplicate").click();

	const dialog = page.getByRole("dialog", { name: "Duplicate nightly-triage" });
	await expect(dialog.getByLabel("Task name")).toHaveValue("nightly-triage-2");
	await expect(dialog.getByLabel("Project", { exact: true })).toHaveValue(
		"lsegal/aviary",
	);
	await expect(dialog.getByLabel("Prompt")).toHaveValue(
		"Triage issues opened since the last run and label them.",
	);
	await expect(dialog.getByRole("radio", { name: "Daily" })).toHaveAttribute(
		"aria-checked",
		"true",
	);
	await dialog.getByRole("button", { name: "Create" }).click();
	await expect(dialog).toBeHidden();
	const config = JSON.parse(String(calls.config_save?.[0]?.config)) as {
		agents: { name: string; tasks: Record<string, unknown>[] }[];
	};
	const copy = config.agents[0].tasks.find(
		(t) => t.name === "nightly-triage-2",
	);
	expect(copy).toMatchObject({
		type: "project",
		project: "lsegal/aviary",
		model: "cli/claude/opus",
		trigger: "daily",
		at: "02:00",
		timezone: "America/Toronto",
		overlap: "skip",
		missed: "run_once",
		precompile: true,
	});
	expect(copy).not.toHaveProperty("compiled");
	// The board stays put and lists the copy.
	await expect(page).toHaveURL(/\/system\/watches\/lsegal%2Faviary$/);
	await expect(
		page.getByTestId("scheduled-card").filter({ hasText: "nightly-triage-2" }),
	).toHaveCount(1);
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

test("the header Schedule button creates a project scheduled task through the shared Add task dialog", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto("/system/watches/lsegal%2Faviary");
	await expect(page.getByTestId("scheduled-card")).toHaveCount(2);
	await page.getByRole("button", { name: "Schedule task" }).click();

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
	const precompute = dialog.getByLabel("Try to precompile");
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

	// It shows on the board with the other scheduled items, and its card's
	// gear opens the same task's full settings.
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
	await card.getByRole("button", { name: "Task settings" }).click();
	const settings = page.getByRole("dialog", { name: "Edit task" });
	await expect(settings.getByLabel("Task name")).toHaveValue("hourly-ci-watch");
});

test("the prompt chat creates a weekly scheduled task that shows on the board and in Scheduled Tasks (#1114)", async ({
	page,
}) => {
	const reply =
		"Created weekly-seo on lsegal/aviary: weekly on Mon at 09:00 (UTC). Next runs: Mon 09:00, then each Monday. Not deterministic, so each run uses the model: about 4,600–12,400 tokens per run.";
	const SEO_PROMPT =
		"Review the aviary docs site's SEO and file issues for what to fix.";
	const runs: Args[] = [];
	await setup(page, {
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
	await page.clock.install();
	await page.goto("/agents/assistant/projects/lsegal%2Faviary");
	await expect(page.getByTestId("scheduled-card")).toHaveCount(2);
	// With the board's poll stopped, the new task can only show because the
	// finished chat turn refreshed the schedules (#1151).
	await page.clock.pauseAt(Date.now() + 1_000);

	await page.getByTestId("prompt-chat-toggle").click();
	const chat = page.getByTestId("project-chat");
	const input = chat.getByPlaceholder("Describe a task or ask a question");
	await expect(input).toBeEnabled();
	await input.fill("create a scheduled task to do SEO for aviary weekly");
	await input.press("Enter");
	await expect.poll(() => runs.length).toBe(1);
	expect(runs[0]).toMatchObject({ name: "projects" });
	// The reply previews the schedule, its next runs and its cost.
	await expect(chat).toContainText("weekly on Mon at 09:00 (UTC)");
	await expect(chat).toContainText("tokens per run");

	// The task shows on the board with the others, as a UI-made one does.
	const card = page
		.getByTestId("scheduled-card")
		.filter({ hasText: "weekly-seo" });
	await expect(card).toHaveCount(1);
	await expect(card.getByTestId("scheduled-trigger")).toHaveText(
		"weekly on Mon at 09:00 (UTC)",
	);
	// And in Scheduled Tasks.
	await page.goto("/agents/assistant/tasks?task=weekly-seo");
	await expect(page).toHaveURL(/\/agents\/assistant\/tasks\?task=weekly-seo/);
	const main = page.getByRole("main");
	await expect(main).toContainText("Defined in: tasks/weekly-seo.md");
	await expect(main.getByRole("textbox").first()).toHaveValue("weekly-seo");
});

test("new schedule on an owned project runs under the project's agent without asking", async ({
	page,
}) => {
	const calls = await setup(page, { owner: "reviewer" });
	await page.goto("/agents/reviewer/projects/lsegal%2Faviary");
	await page.getByRole("button", { name: "Schedule task" }).click();

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
		// The saved task reopens in its full settings with every field kept.
		await card.getByRole("button", { name: "Task settings" }).click();
		const settings = page.getByRole("dialog", { name: "Edit task" });
		await expect(settings.getByLabel("Task name")).toHaveValue(name);
		await expectTrigger(settings, c);
		await page.keyboard.press("Escape");
		await expect(settings).toBeHidden();
	}
});

// The board's filter chip for one state, Scheduled included.
const chip = (page: Page, label: string) =>
	page
		.getByRole("group", { name: "Job filter" })
		.getByRole("button", { name: new RegExp(`^${label}`) });

// A second project's schedule, between runs, and its failed issue's job,
// for the agent board of both projects (#1107).
const ZVID_TASK = schedule({
	id: "assistant/zvid-bench",
	name: "zvid-bench",
	project: "lsegal/zvidlib",
	prompt: "Run the decoder benchmarks and report regressions.",
	enabled: true,
	trigger_type: "interval",
	every: "1h",
	description: "every 1h (Local)",
	next_run: minutesFromNow(30),
});
const ZVID_RUN = {
	key: "lsegal/zvidlib#1795456000",
	number: 1795456000,
	status: "complete",
	state: "done",
	result: "succeeded",
	summary: "Benchmarks within noise",
	started: minutesFromNow(-31),
	finished: minutesFromNow(-30),
	duration_ms: 60_000,
};
const ZVID_ENTRY = {
	key: ZVID_RUN.key,
	target: "lsegal/zvidlib",
	number: ZVID_RUN.number,
	title: "zvid-bench",
	status: "complete",
	state: "scheduled",
	kind: "schedule",
	schedule_id: "assistant/zvid-bench",
	agent: "claude",
	repository: "lsegal/zvidlib",
	actions: { retry: true, pause: false, resume: false, stop: false },
	started: ZVID_RUN.started,
	last_run: ZVID_RUN,
	next_run: { at: minutesFromNow(30), paused: false, queued: false },
	recent_runs: [ZVID_RUN],
};
const ZVID_ISSUE_JOB = {
	...ISSUE_JOB,
	key: "lsegal/zvidlib#12",
	target: "lsegal/zvidlib",
	repository: "lsegal/zvidlib",
	number: 12,
	title: "Decode B-frames",
	status: "failed",
	state: "failed",
};
// The board counts as boardStateCounts reports them: the running nightly
// schedule counts once as Running, and its finished runs never as Done.
const AGENT_BOARD = {
	owner: "coder",
	projects: ["lsegal/aviary", "lsegal/zvidlib"],
	tasks: [TASKS[0], TASKS[1], ZVID_TASK, TASKS[3]],
	entries: [SCHEDULED_ENTRY, ZVID_ENTRY],
	jobs: [ISSUE_JOB, ZVID_ISSUE_JOB],
	states: {
		"lsegal/aviary": { starting: 0, paused: 0, running: 2, done: 3, failed: 0 },
		"lsegal/zvidlib": {
			starting: 1,
			paused: 0,
			running: 0,
			done: 1,
			failed: 1,
		},
	},
};

test.describe("the agent board's scheduled items (#1107)", () => {
	test("shows every project's schedules as one card each, with its project", async ({
		page,
	}) => {
		const calls = await setup(page, AGENT_BOARD);
		await page.goto("/agents/coder/projects");

		const cards = page.getByTestId("scheduled-card");
		await expect(cards).toHaveCount(3);
		// By next run: zvid-bench in 30m, nightly-triage in 42m, then paused.
		await expect(cards.getByRole("heading")).toHaveText([
			"zvid-bench",
			"nightly-triage",
			"weekly-deps",
		]);
		await expect(cards.getByTestId("scheduled-project")).toHaveText([
			"lsegal/zvidlib",
			"lsegal/aviary",
			"lsegal/aviary",
		]);
		const zvid = cards.nth(0);
		const nightly = cards.nth(1);
		const weekly = cards.nth(2);
		await expect(zvid.getByTestId("scheduled-project")).toHaveAttribute(
			"href",
			"/agents/coder/projects/lsegal%2Fzvidlib",
		);
		// A run shows in place on its card; no job card copies it.
		await expect(nightly.getByTestId("scheduled-state")).toHaveText("Running");
		await expect(nightly.getByTestId("scheduled-running")).toBeVisible();
		await expect(nightly.getByTestId("scheduled-next")).toContainText(
			/in 4[12]m/,
		);
		await expect(weekly.getByTestId("scheduled-next")).toHaveText(
			"Next run: paused",
		);
		await expect(weekly.getByTestId("paused-chip")).toBeVisible();
		await expect(zvid.getByTestId("scheduled-state")).toHaveText("Scheduled");
		await expect(zvid.getByTestId("scheduled-next")).toContainText(
			/in (29|30)m/,
		);
		await expect(zvid.getByTestId("scheduled-last-result")).toHaveText("✓");
		await expect(zvid.getByTestId("scheduled-last-summary")).toHaveText(
			"Benchmarks within noise",
		);
		const jobs = page.getByTestId("job-card");
		await expect(jobs).toHaveCount(2);
		await expect(jobs.getByRole("heading")).toHaveText([
			/Fix the parser/,
			/Decode B-frames/,
		]);
		// The board reads its projects' jobs apart from their schedules'
		// entries, as a project board does.
		expect(calls.project_job_list).toContainEqual(
			expect.objectContaining({
				targets: ["lsegal/aviary", "lsegal/zvidlib"],
				kinds: ["issue", "discussion", "merge", "discuss"],
			}),
		);
		expect(calls.project_job_list).toContainEqual(
			expect.objectContaining({
				targets: ["lsegal/aviary", "lsegal/zvidlib"],
				kinds: ["schedule"],
			}),
		);
		// No Schedule button: a new schedule needs a project, so it is made
		// from a project's board.
		await expect(page.getByTestId("schedule-new")).toHaveCount(0);

		// The kanban's Scheduled column names each card's project too.
		await page.getByTestId("kanban-toggle").click();
		const boxes = page.getByTestId("kanban-scheduled-card");
		await expect(boxes).toHaveCount(3);
		await expect(boxes.getByTestId("kanban-scheduled-project")).toHaveText([
			"lsegal/zvidlib",
			"lsegal/aviary",
			"lsegal/aviary",
		]);
		await page.getByTestId("kanban-toggle").click();

		// No pencil (#1165): the gear, the last action, opens the task's full
		// settings in place.
		await expect(zvid.getByRole("link", { name: /edit/i })).toHaveCount(0);
		const gear = zvid
			.getByTestId("scheduled-footer")
			.locator("a:visible, button:visible")
			.last();
		await expect(gear).toHaveAccessibleName("Task settings");
		await gear.click();
		const settings = page.getByRole("dialog", { name: "Edit task" });
		await expect(settings.getByLabel("Task name")).toHaveValue("zvid-bench");
		await expect(page).toHaveURL(/^[^?]*\/agents\/coder\/projects(\?|$)/);
	});

	test("the Scheduled chip filters schedules with the other chips and persists", async ({
		page,
	}) => {
		await setup(page, AGENT_BOARD);
		await page.goto("/agents/coder/projects");
		const scheduled = page.getByTestId("scheduled-card");
		const jobs = page.getByTestId("job-card");
		const total = page.getByTestId("job-total");

		// No chip shows everything.
		await expect(chip(page, "Scheduled")).toHaveText(/^Scheduled\s*3$/);
		await expect(scheduled).toHaveCount(3);
		await expect(jobs).toHaveCount(2);
		await expect(total).toHaveText("5 items");

		// Scheduled alone shows only the schedules.
		await chip(page, "Scheduled").click();
		await expect(page).toHaveURL(/[?&]state=scheduled$/);
		await expect(scheduled).toHaveCount(3);
		await expect(jobs).toHaveCount(0);

		// With Failed it shows the union.
		await chip(page, "Failed").click();
		await expect(page).toHaveURL(/[?&]state=scheduled(,|%2C)failed$/);
		await expect(scheduled).toHaveCount(3);
		await expect(jobs).toHaveCount(1);
		await expect(jobs.getByRole("heading")).toContainText("Decode B-frames");
		expect(
			await page.evaluate(() =>
				localStorage.getItem("aviary.projects.states.agent:coder"),
			),
		).toBe("scheduled,failed");

		// A reload, and leaving the board and coming back, keep it.
		await page.reload();
		await expect(chip(page, "Scheduled")).toHaveAttribute(
			"aria-pressed",
			"true",
		);
		await expect(chip(page, "Failed")).toHaveAttribute("aria-pressed", "true");
		await expect(scheduled).toHaveCount(3);
		await page.goto("/agents/coder/projects/lsegal%2Fzvidlib");
		await expect(page.getByTestId("scheduled-card")).toHaveCount(1);
		await page.goto("/agents/coder/projects");
		await expect(page).toHaveURL(/[?&]state=scheduled(,|%2C)failed$/);
		await expect(chip(page, "Scheduled")).toHaveAttribute(
			"aria-pressed",
			"true",
		);
		await expect(scheduled).toHaveCount(3);
		await expect(jobs).toHaveCount(1);

		// Deselecting Scheduled while Failed stays selected hides them: no
		// schedule's last run failed.
		await chip(page, "Scheduled").click();
		await expect(page).toHaveURL(/[?&]state=failed$/);
		await expect(scheduled).toHaveCount(0);
		await expect(jobs).toHaveCount(1);
	});

	test("its counts are the sums of the project boards' counts", async ({
		page,
	}) => {
		await setup(page, AGENT_BOARD);
		const labels = [
			"Scheduled",
			"Starting",
			"Paused",
			"Running",
			"Done",
			"Failed",
		];
		async function counts(path: string) {
			await page.goto(path);
			await expect(page.getByTestId("stat-card").first()).toBeVisible();
			await expect(page.getByTestId("scheduled-card").first()).toBeVisible();
			const out: Record<string, number> = {};
			for (const label of labels) {
				const text = (await chip(page, label).innerText()).trim();
				out[`chip ${label}`] = Number(text.replace(/^\D+/, ""));
			}
			for (const card of await page.getByTestId("stat-card").all()) {
				const label = await card.getByTestId("stat-label").innerText();
				if (!labels.some((l) => l.toUpperCase() === label.toUpperCase()))
					continue;
				out[`stat ${label.toLowerCase()}`] = Number(
					await card.getByTestId("stat-value").innerText(),
				);
			}
			out.total = Number(
				(await page.getByTestId("job-total").innerText()).replace(/\D+$/, ""),
			);
			return out;
		}
		const aviary = await counts("/agents/coder/projects/lsegal%2Faviary");
		const zvid = await counts("/agents/coder/projects/lsegal%2Fzvidlib");
		const root = await counts("/agents/coder/projects");
		const sums = Object.fromEntries(
			Object.keys(aviary).map((k) => [k, aviary[k] + zvid[k]]),
		);
		expect(root).toEqual(sums);
		// Each schedule counts once under Scheduled, the running one counts
		// as Running, and finished runs leave Done alone.
		expect(root["chip Scheduled"]).toBe(3);
		expect(root["chip Running"]).toBe(2);
		expect(root["chip Done"]).toBe(4);
	});
});

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
	await expect(chip(page, "Scheduled")).toHaveClass(/bg-violet-100/);
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
	await expect(toggle).toHaveText("Filters");
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
	await expect(toggle).toHaveText("Filters · 2");

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
	await expect(toggle).toHaveText("Filters");

	// A tap outside closes it.
	await page.getByTestId("job-total").click();
	await expect(group).toBeHidden();

	// The selection persists across a reload.
	await toggle.click();
	await chip(page, "Running").click();
	await page.reload();
	await expect(toggle).toHaveText("Filters · 1");

	// Desktop widths show the inline chips and no Filter button.
	await page.setViewportSize({ width: 1280, height: 720 });
	await expect(toggle).toBeHidden();
	await expect(group).toBeVisible();
	await expect(chip(page, "Running")).toHaveAttribute("aria-pressed", "true");
});

test("a mobile agent board's Filter panel has a Scheduled option (#1107)", async ({
	page,
}) => {
	await page.setViewportSize({ width: 375, height: 812 });
	await setup(page, { owner: "coder" });
	await page.goto("/agents/coder/projects");
	await page.getByTestId("filter-toggle").click();
	const group = page.getByRole("group", { name: "Job filter" });
	await expect(group.getByRole("button", { name: /^Running/ })).toBeVisible();
	await expect(group.getByRole("button", { name: /^Scheduled/ })).toHaveText(
		/^Scheduled\s*2$/,
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

test("an old Schedules URL for one schedule lands on the board's scheduled items", async ({
	page,
}) => {
	await setup(page);
	await page.goto(
		"/system/watches/lsegal%2Faviary?tab=schedules&schedule=assistant/nightly-triage",
	);
	await expect(page).toHaveURL(/\?state=scheduled$/);
	await expect(page.getByTestId("scheduled-card")).toHaveCount(2);
});

// One card per scheduled task (#977): its runs update it in place instead of
// each leaving a Done card of its own.
const RUN_MINUTE = 60;
const finishedRun = (
	number: number,
	minutesAgo: number,
	result: string,
	summary: string,
) => ({
	key: `lsegal/aviary#${number}`,
	number,
	status: result === "failed" ? "failed" : "complete",
	state: result === "failed" ? "failed" : "done",
	result,
	summary,
	started: minutesFromNow(-minutesAgo - 1),
	finished: minutesFromNow(-minutesAgo),
	duration_ms: RUN_MINUTE * 1000,
});
const RELEASE_TASK = schedule({
	id: "assistant/release-aviary",
	name: "release-aviary",
	prompt: "Release aviary when main is ahead of the latest release.",
	enabled: true,
	trigger_type: "interval",
	every: "5m",
	description: "every 5m (Local)",
	next_run: minutesFromNow(3),
});
const RELEASE_RUNS = [
	finishedRun(
		1795456753,
		2,
		"succeeded",
		"main matches the latest release, so no release was started",
	),
	finishedRun(1795456453, 7, "fallback", "Released v0.7.8"),
	finishedRun(1795456153, 12, "failed", "gh: HTTP 502"),
	finishedRun(1795455853, 17, "succeeded", "Nothing to release"),
];
const releaseEntry = (fields: Record<string, unknown> = {}) => ({
	key: `lsegal/aviary#${RELEASE_RUNS[0].number}`,
	target: "lsegal/aviary",
	number: RELEASE_RUNS[0].number,
	title: "release-aviary",
	status: "complete",
	state: "scheduled",
	kind: "schedule",
	schedule_id: "assistant/release-aviary",
	agent: "claude",
	repository: "lsegal/aviary",
	actions: { retry: true, pause: false, resume: false, stop: false },
	started: RELEASE_RUNS[0].started,
	last_run: RELEASE_RUNS[0],
	next_run: { at: minutesFromNow(3), paused: false, queued: false },
	recent_runs: RELEASE_RUNS,
	...fields,
});

test.describe("one card per scheduled task (#977)", () => {
	test("several completed runs leave one card with the last and next run, and no Done copies", async ({
		page,
	}) => {
		const calls = await setup(page, {
			tasks: [RELEASE_TASK],
			entries: [releaseEntry()],
			jobs: [ISSUE_JOB],
		});
		await page.goto("/system/watches/lsegal%2Faviary");
		const card = page.getByTestId("scheduled-card");
		await expect(card).toHaveCount(1);
		// The runs are not job cards: only the issue's is.
		await expect(page.getByTestId("job-card")).toHaveCount(1);
		await expect(page.getByTestId("job-card")).toContainText("Fix the parser");
		await expect(card.getByTestId("scheduled-state")).toHaveText("Scheduled");
		await expect(card.getByTestId("scheduled-next")).toHaveText(
			/^Next run in [23]m \(.+\)$/,
		);
		await expect(card.getByTestId("scheduled-last-result")).toHaveText("✓");
		await expect(card.getByTestId("scheduled-last-time")).toHaveText(
			/^Last run 2m ago · succeeded$/,
		);
		await expect(card.getByTestId("scheduled-last-summary")).toHaveText(
			"main matches the latest release, so no release was started",
		);
		// The strip shows recent results, oldest first.
		await expect(card.getByTestId("scheduled-strip")).toHaveText("✓✖⚠✓");
		// The project's jobs are read apart from its schedules' entries.
		expect(calls.project_job_list).toContainEqual(
			expect.objectContaining({
				kinds: ["issue", "discussion", "merge", "discuss"],
			}),
		);

		// Done shows neither the schedule nor its runs.
		const filter = page.getByRole("group", { name: "Job filter" });
		await filter.getByRole("button", { name: /^Done/ }).click();
		await expect(card).toHaveCount(0);
		await expect(page.getByTestId("job-card")).toHaveCount(0);
		await filter.getByRole("button", { name: /^Done/ }).click();

		// So does the kanban: one Scheduled box and no Done column cards.
		await page.getByTestId("kanban-toggle").click();
		await expect(page.getByTestId("kanban-scheduled-card")).toHaveCount(1);
		await expect(
			page
				.getByTestId("kanban-scheduled-card")
				.getByTestId("kanban-scheduled-last"),
		).toHaveText(/✓\s*Last run 2m ago/);
		await expect(
			page
				.locator('[data-testid="kanban-column"][data-state="done"]')
				.getByTestId("kanban-card"),
		).toHaveCount(0);
	});

	test("a run shows Running on the same card, which then returns to Scheduled", async ({
		page,
	}) => {
		const running = releaseEntry({
			number: 1795457053,
			key: "lsegal/aviary#1795457053",
			status: "active",
			state: "running",
			started: minutesFromNow(-1),
			actions: { retry: false, pause: false, resume: false, stop: true },
			recent_runs: [
				{
					key: "lsegal/aviary#1795457053",
					number: 1795457053,
					status: "active",
					state: "running",
					started: minutesFromNow(-1),
				},
				...RELEASE_RUNS,
			],
		});
		const entries: unknown[] = [running];
		await setup(page, {
			tasks: [RELEASE_TASK],
			entries,
			job: {
				...running,
				scrollback: "Comparing main with v0.7.8…",
				events: [{ kind: "message", text: "Comparing main with v0.7.8…" }],
			},
		});
		await page.goto("/system/watches/lsegal%2Faviary");
		const card = page.getByTestId("scheduled-card");
		await expect(card).toHaveCount(1);
		await expect(card.getByTestId("scheduled-state")).toHaveText("Running");
		await expect(card.getByTestId("scheduled-spinner")).toBeVisible();
		await expect(card.getByTestId("scheduled-elapsed")).toHaveText(/^[12]m$/);
		await expect(card.getByTestId("scheduled-output-rich")).toContainText(
			"Comparing main with v0.7.8…",
		);
		// The last run stays on the card while the next one runs.
		await expect(card.getByTestId("scheduled-last-time")).toHaveText(
			/^Last run 2m ago/,
		);
		await expect(page.getByTestId("job-card")).toHaveCount(0);
		// Running counts it; the card stays where it is.
		const filter = page.getByRole("group", { name: "Job filter" });
		await filter.getByRole("button", { name: /^Running/ }).click();
		await expect(card).toHaveCount(1);
		await filter.getByRole("button", { name: /^Running/ }).click();

		// The run finishes: the same card is back to Scheduled with it as its
		// last run, and nothing is left behind.
		entries.splice(
			0,
			1,
			releaseEntry({
				number: 1795457053,
				key: "lsegal/aviary#1795457053",
				last_run: finishedRun(1795457053, 0, "succeeded", "Released v0.7.9"),
				recent_runs: [
					finishedRun(1795457053, 0, "succeeded", "Released v0.7.9"),
					...RELEASE_RUNS,
				],
			}),
		);
		await expect(card.getByTestId("scheduled-state")).toHaveText("Scheduled");
		await expect(card).toHaveCount(1);
		await expect(card.getByTestId("scheduled-output-rich")).toHaveCount(0);
		await expect(card.getByTestId("scheduled-last-summary")).toHaveText(
			"Released v0.7.9",
		);
		await expect(page.getByTestId("job-card")).toHaveCount(0);
	});

	test("a failed last run shows on the card and under Failed until dismissed", async ({
		page,
	}) => {
		const failed = finishedRun(1795457053, 1, "failed", "gh: HTTP 502");
		const calls = await setup(page, {
			tasks: [RELEASE_TASK],
			entries: [
				releaseEntry({
					number: 1795457053,
					key: "lsegal/aviary#1795457053",
					status: "failed",
					state: "failed",
					last_run: failed,
					recent_runs: [failed, ...RELEASE_RUNS],
				}),
			],
		});
		await page.goto("/system/watches/lsegal%2Faviary");
		const card = page.getByTestId("scheduled-card");
		await expect(card.getByTestId("scheduled-failed")).toHaveText("Failed");
		await expect(card.getByTestId("scheduled-last-result")).toHaveText("✖");
		await expect(card.getByTestId("scheduled-last-summary")).toHaveText(
			"gh: HTTP 502",
		);
		const filter = page.getByRole("group", { name: "Job filter" });
		await filter.getByRole("button", { name: /^Failed/ }).click();
		await expect(card).toHaveCount(1);
		await card.getByTestId("scheduled-dismiss").click();
		await expect
			.poll(() => calls.project_job_dismiss?.[0])
			.toEqual({ target: "lsegal/aviary", number: 1795457053 });
	});

	test("Last run counts manual runs, skips missed fires and opens the run (#1013)", async ({
		page,
	}) => {
		// The scheduler's history, oldest first: a scheduled run 6h ago, a
		// missed fire after it, then a manual run a minute ago that the
		// board's entry has not caught up with.
		const fire = (minutesAgo: number, fields: Record<string, unknown>) => ({
			fire_time: minutesFromNow(-minutesAgo),
			...fields,
		});
		const history = [
			fire(365, {
				reason: "schedule",
				status: "completed",
				summary: "Nothing to release",
			}),
			fire(360, { reason: "missed", status: "missed" }),
		];
		const sixHoursAgo = finishedRun(
			1795456753,
			364,
			"succeeded",
			"Nothing to release",
		);
		const entries: unknown[] = [
			releaseEntry({ last_run: sixHoursAgo, recent_runs: [sixHoursAgo] }),
		];
		await setup(page, { tasks: [{ ...RELEASE_TASK, history }], entries });
		await page.setViewportSize({ width: 420, height: 900 });
		await page.goto("/system/watches/lsegal%2Faviary");
		const card = page.getByTestId("scheduled-card");
		const line = card.getByTestId("scheduled-last-run");

		// The missed fire is not a run: the last run is the scheduled one,
		// with its result, and it opens that run.
		await expect(card.getByTestId("scheduled-last-result")).toHaveText("✓");
		await expect(card.getByTestId("scheduled-last-time")).toHaveText(
			/^Last run 6h ago · succeeded$/,
		);
		await card.getByTestId("scheduled-last-time").click();
		await expect(page).toHaveURL(/\/jobs\/1795456753/);
		await page.goBack();

		// A manual run finishes: the card shows it, newest of any trigger.
		history.push(
			fire(1, {
				reason: "manual",
				status: "completed",
				summary: "Triggered release.yml on main",
			}),
		);
		await page.reload();
		await expect(card.getByTestId("scheduled-last-time")).toHaveText(
			/^Last run 1m ago · succeeded$/,
		);
		await expect(line).not.toHaveText(/^\s*[·•]/);
	});

	test("a missed fire never shows as the last run while a manual run is in flight (#1013)", async ({
		page,
	}) => {
		// The issue's history, oldest first: the last scheduled run 6h ago, a
		// missed fire, and a manual run still running, which the board has
		// no finished run for yet.
		const history = [
			{
				fire_time: minutesFromNow(-365),
				reason: "schedule",
				status: "completed",
				summary: "Nothing to release",
			},
			{ fire_time: minutesFromNow(-360), reason: "missed", status: "missed" },
			{ fire_time: minutesFromNow(-1), reason: "manual", status: "running" },
		];
		await setup(page, { tasks: [{ ...RELEASE_TASK, history }], entries: [] });
		await page.goto("/system/watches/lsegal%2Faviary");
		const line = page
			.getByTestId("scheduled-card")
			.getByTestId("scheduled-last-run");
		await expect(line).toHaveText(/^\s*✓\s*Last run 6h ago · succeeded$/);
		await expect(line).not.toHaveText(/^\s*[·•]/);
	});

	test("Paused and Queued take the next run's place", async ({ page }) => {
		await setup(page, {
			tasks: [
				{ ...RELEASE_TASK, enabled: false },
				schedule({
					id: "assistant/sync",
					name: "sync",
					prompt: "Sync labels.",
					enabled: true,
					every: "1m",
					description: "every 1m (Local)",
					next_run: minutesFromNow(1),
				}),
			],
			entries: [
				releaseEntry({ next_run: { paused: true, queued: false } }),
				releaseEntry({
					key: "lsegal/aviary#1795457000",
					number: 1795457000,
					title: "sync",
					schedule_id: "assistant/sync",
					status: "active",
					state: "running",
					next_run: { at: minutesFromNow(1), paused: false, queued: true },
					recent_runs: [
						{
							key: "lsegal/aviary#1795457000",
							number: 1795457000,
							status: "active",
							state: "running",
							started: minutesFromNow(-3),
						},
					],
					last_run: undefined,
				}),
			],
		});
		await page.goto("/system/watches/lsegal%2Faviary");
		const cards = page.getByTestId("scheduled-card");
		await expect(cards).toHaveCount(2);
		const release = cards.filter({ hasText: "release-aviary" });
		await expect(release.getByTestId("scheduled-next")).toHaveText(
			"Next run: paused",
		);
		await expect(release.getByTestId("paused-chip")).toBeVisible();
		await expect(
			cards.filter({ hasText: "sync" }).getByTestId("scheduled-next"),
		).toHaveText("Queued");
	});
});

// A run changes a scheduled card's contents, never its size: the running
// state sits in the status chip, the output toggle in the output's corner,
// and the footer stays one row (#1157).
test.describe("a running scheduled card keeps its size (#1157)", () => {
	// The neighbors run either side of the release's next run, in 3m, so it
	// stays the middle card.
	const neighbor = (name: string, minutes: number) =>
		schedule({
			id: `assistant/${name}`,
			name,
			prompt: `Run ${name}.`,
			enabled: true,
			trigger_type: "interval",
			every: "1h",
			description: "every 1h (Local)",
			next_run: minutesFromNow(minutes),
		});
	const TASKS_1157 = [
		neighbor("aviary-find-dupe-tests", 2),
		{ ...RELEASE_TASK, name: "aviary-release", model: "cli/claude/opus" },
		neighbor("aviary-update-docs", 30),
	];
	const RUN = 1795457053;
	const runningEntry = releaseEntry({
		number: RUN,
		key: `lsegal/aviary#${RUN}`,
		status: "active",
		state: "running",
		started: minutesFromNow(-1),
		actions: { retry: false, pause: false, resume: false, stop: true },
		recent_runs: [
			{
				key: `lsegal/aviary#${RUN}`,
				number: RUN,
				status: "active",
				state: "running",
				started: minutesFromNow(-1),
			},
			...RELEASE_RUNS,
		],
	});
	const doneEntry = releaseEntry({
		number: RUN,
		key: `lsegal/aviary#${RUN}`,
		last_run: finishedRun(RUN, 0, "succeeded", "Released v0.7.9"),
		recent_runs: [
			finishedRun(RUN, 0, "succeeded", "Released v0.7.9"),
			...RELEASE_RUNS,
		],
	});

	// The top edge of each visible button in a card's footer.
	const footerButtonTops = (footer: Locator) =>
		footer.evaluate((el) =>
			[...el.querySelectorAll<HTMLElement>(":scope > a, :scope > button")]
				.filter((b) => b.offsetParent)
				.map((b) => b.offsetTop),
		);
	type Layout = {
		cards: { x: number; y: number; width: number; height: number }[];
		footerTops: number[];
		gridHeight: number;
	};
	async function layout(page: Page): Promise<Layout> {
		const cards = page.getByTestId("scheduled-card");
		const boxes = [];
		for (let i = 0; i < (await cards.count()); i++)
			boxes.push((await cards.nth(i).boundingBox())!);
		const footerTops = await footerButtonTops(
			cards.nth(1).getByTestId("scheduled-footer"),
		);
		const grid = (await page
			.getByRole("region", { name: "Jobs" })
			.boundingBox())!;
		return { cards: boxes, footerTops, gridHeight: grid.height };
	}
	function expectSameLayout(actual: Layout, expected: Layout) {
		expect(actual.cards).toHaveLength(expected.cards.length);
		actual.cards.forEach((box, i) => {
			for (const key of ["x", "y", "width", "height"] as const)
				expect(Math.abs(box[key] - expected.cards[i][key])).toBeLessThan(1);
		});
		expect(Math.abs(actual.gridHeight - expected.gridHeight)).toBeLessThan(1);
		// One footer row: every button shares the same offsetTop.
		expect(actual.footerTops.length).toBeGreaterThan(2);
		expect(new Set(actual.footerTops).size).toBe(1);
		expect(actual.footerTops).toEqual(expected.footerTops);
	}

	for (const viewport of [
		{ name: "desktop", width: 1280, height: 900 },
		{ name: "narrow", width: 390, height: 844 },
	]) {
		test(`idle, running and finished cards share one layout on ${viewport.name}`, async ({
			page,
		}) => {
			await page.setViewportSize(viewport);
			const entries: unknown[] = [releaseEntry()];
			await setup(page, {
				tasks: TASKS_1157,
				entries,
				job: {
					...runningEntry,
					scrollback: "scheduled run started on claude/opus",
					events: [
						{ kind: "message", text: "scheduled run started on claude/opus" },
					],
				},
			});
			await page.goto("/system/watches/lsegal%2Faviary");
			const cards = page.getByTestId("scheduled-card");
			await expect(cards).toHaveCount(3);
			const release = cards.nth(1);
			await expect(release.getByRole("heading")).toHaveText("aviary-release");
			await expect(release.getByTestId("scheduled-state")).toHaveText(
				"Scheduled",
			);
			const idle = await layout(page);
			expectSameLayout(idle, idle);

			// The run starts: the chip says so, the output shows with its
			// toggle, and nothing moves.
			entries.splice(0, 1, runningEntry);
			await expect(release.getByTestId("scheduled-running")).toHaveText(
				/^\s*Running · [12]m\s*$/,
			);
			await expect(release.getByTestId("scheduled-output-rich")).toContainText(
				"scheduled run started on claude/opus",
			);
			await expect(release.getByTestId("output-mode")).toBeVisible();
			expectSameLayout(await layout(page), idle);

			// It finishes: the card is back to Scheduled, the same size.
			entries.splice(0, 1, doneEntry);
			await expect(release.getByTestId("scheduled-state")).toHaveText(
				"Scheduled",
			);
			await expect(release.getByTestId("scheduled-last-summary")).toHaveText(
				"Released v0.7.9",
			);
			expectSameLayout(await layout(page), idle);
		});
	}

	test("a narrow card folds Settings and Pause into a menu instead of wrapping", async ({
		page,
	}) => {
		await page.setViewportSize({ width: 390, height: 844 });
		const calls = await setup(page, {
			tasks: TASKS_1157,
			entries: [runningEntry],
		});
		await page.goto("/system/watches/lsegal%2Faviary");
		const release = page.getByTestId("scheduled-card").nth(1);
		await expect(release.getByTestId("scheduled-running")).toBeVisible();
		const footer = release.getByTestId("scheduled-footer");
		await expect(
			footer.getByRole("button", { name: "Task settings" }),
		).toBeVisible();
		await expect(release.getByTestId("scheduled-more")).toBeVisible();

		// Squeeze the card below the footer's width.
		await release.evaluate((el) => {
			(el as HTMLElement).style.width = "240px";
		});
		await expect(release.getByTestId("scheduled-more")).toBeVisible();
		await expect(
			footer.getByRole("button", { name: "Task settings" }),
		).toBeHidden();
		await expect(footer.getByRole("button", { name: "Pause" })).toBeHidden();
		const tops = await footerButtonTops(footer);
		// Run now and More actions; the gear folds into the menu.
		expect(tops.length).toBe(2);
		expect(new Set(tops).size).toBe(1);
		const footerBox = (await footer.boundingBox())!;
		const cardBox = (await release.boundingBox())!;
		expect(footerBox.x + footerBox.width).toBeLessThanOrEqual(
			cardBox.x + cardBox.width + 1,
		);

		await release.getByTestId("scheduled-more").click();
		// Task settings stays the last action in the folded menu (#1165).
		await expect(
			page.getByTestId("scheduled-more-items").getByRole("menuitem"),
		).toHaveText(["Duplicate…", "Pause", "Task settings"]);
		await page
			.getByTestId("scheduled-more-items")
			.getByRole("menuitem", { name: "Pause" })
			.click();
		await expect.poll(() => calls.task_update?.length ?? 0).toBe(1);
		expect(calls.task_update[0]).toMatchObject({
			name: "assistant/aviary-release",
			enabled: false,
		});
	});
});
