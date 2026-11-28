// Fixtures and helpers shared by project-schedules-*.component.test.ts,
// ported from web/e2e/project-schedules.spec.ts (#1636). They follow the
// payloads of task_list, task_upcoming and task_preview (internal/mcp) and of
// project_* for a scheduled run's job.
import { expect } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { mockMCP } from "../component-test/server";

export const minutesFromNow = (m: number) =>
	new Date(Date.now() + m * 60_000).toISOString();

export const BOARD = "/system/watches/lsegal%2Faviary";

export const STATUS = {
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

export const FIRE = 1795424400;
export const SCHEDULED_JOB = {
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
export const SCHEDULED_ENTRY = {
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
export const ISSUE_JOB = {
	...SCHEDULED_JOB,
	key: "lsegal/aviary#7",
	number: 7,
	title: "Fix the parser",
	kind: "issue",
	schedule_id: undefined,
	state: "running",
	checkout_directory: "/tmp/aviary-7",
};

export const schedule = (fields: Record<string, unknown>) => ({
	agent_name: "assistant",
	agent_id: "assistant",
	type: "project",
	project: "lsegal/aviary",
	created_at: "0001-01-01T00:00:00Z",
	...fields,
});

export const TASKS = [
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

export type Args = Record<string, unknown>;

export interface SetupOptions {
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
}

/** Mocks the board's server and returns the calls of the tools it records. */
export function setup(opts: SetupOptions = {}) {
	const calls: Record<string, Args[]> = {};
	const tasks: unknown[] = [...(opts.tasks ?? TASKS)];
	const record = (tool: string) => (args?: Args) => {
		calls[tool] ??= [];
		calls[tool].push(args ?? {});
		return "ok";
	};
	mockMCP({
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

// A task's next fires, each task listed as task_upcoming lists it.
export const upcomingRun = (
	name: string,
	minutes: number,
	project = "lsegal/aviary",
) => ({
	task_id: `assistant/${name}`,
	name,
	project,
	at: minutesFromNow(minutes),
});

// The board's filter chip for one state, Scheduled included.
export const chip = (label: string) =>
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
export const AGENT_BOARD: SetupOptions = {
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

// One card per scheduled task (#977): its runs update it in place instead of
// each leaving a Done card of its own.
const RUN_MINUTE = 60;
export const finishedRun = (
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
export const RELEASE_TASK = schedule({
	id: "assistant/release-aviary",
	name: "release-aviary",
	prompt: "Release aviary when main is ahead of the latest release.",
	enabled: true,
	trigger_type: "interval",
	every: "5m",
	description: "every 5m (Local)",
	next_run: minutesFromNow(3),
});
export const RELEASE_RUNS = [
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
export const releaseEntry = (fields: Record<string, unknown> = {}) => ({
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

// Playwright-style assertions for component tests.

/** Polls the number of elements loc matches, as toHaveCount. */
export async function expectCount(loc: Locator, n: number) {
	await expect.poll(() => loc.elements().length).toBe(n);
}

const normalize = (s: string) => s.replace(/\s+/g, " ").trim();

/** Each element's whitespace-normalized text. */
export const textsOf = (loc: Locator) =>
	loc.elements().map((el) => normalize(el.textContent ?? ""));

/** Polls every match's text, as toHaveText with an array: strings match the
 * whole text, regular expressions test it. */
export async function expectTexts(loc: Locator, expected: (string | RegExp)[]) {
	await expect
		.poll(() => textsOf(loc))
		.toEqual(
			expected.map((e) =>
				typeof e === "string" ? normalize(e) : expect.stringMatching(e),
			),
		);
}

/** Focuses the element and presses key, as Playwright's locator.press. */
export async function press(loc: Locator, key: string) {
	(loc.element() as HTMLElement).focus();
	await commands.keyPress(key);
}

/** Whether an element shows, as Playwright's :visible. */
export const shown = (el: Element) => el.getClientRects().length > 0;

/** Polls until loc is absent or not shown, as Playwright's toBeHidden. */
export async function expectHidden(loc: Locator) {
	await expect
		.poll(() => {
			const el = loc.query();
			return !el || !shown(el) || getComputedStyle(el).visibility === "hidden";
		})
		.toBe(true);
}

/**
 * Closes the task dialog a test leaves open. The shared task dialog's state
 * is module state, which outlives the test's app in a component test file
 * (each Playwright test had a fresh page), so a test that ends with it open
 * closes it for the next one.
 */
export async function closeTaskDialog() {
	await commands.keyPress("Escape");
	await expectHidden(page.getByRole("dialog"));
}
