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

test("shots", async ({ page }) => {
	await page.setViewportSize({ width: 1400, height: 1000 });
	await setup(page);
	await page.goto("/system/watches/lsegal%2Faviary");
	await page.getByTestId("scheduled-card").first().waitFor();
	await page.waitForTimeout(500);
	await page.getByLabel("Jobs").screenshot({ path: "/tmp/shot-board.png" });
	await page.getByTestId("kanban-toggle").click();
	await page.waitForTimeout(500);
	await page.getByTestId("kanban").screenshot({ path: "/tmp/shot-kanban.png" });
	await page.getByTestId("kanban-toggle").click();
	await page.getByTestId("scheduled-card").first().getByRole("link", { name: "nightly-triage" }).click();
	await page.getByTestId("schedule-predicted").waitFor();
	await page.waitForTimeout(300);
	await page.getByTestId("schedule-detail").screenshot({ path: "/tmp/shot-detail.png" });
});
