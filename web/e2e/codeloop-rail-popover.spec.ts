import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";
import { targetJobs } from "./helpers/targetJobs";

// The collapsed rail's Code Loop flyout names where the work is: each
// project's running count, running projects first, with a header total that
// counts them the way the board's states do (#1359). The count stands in for
// the jobs, which the flyout does not list (#1494).
// Fixtures follow the payloads of project_status and project_job_list.

const TARGETS = ["lsegal/forgo", "lsegal/aviary", "lsegal/zvid"];

const started = new Date().toISOString();

const issueJob = (
	target: string,
	number: number,
	status = "active",
	state = "running",
) => ({
	key: `${target}#${number}`,
	target,
	number,
	title: `Issue ${number}`,
	status,
	state,
	kind: "issue",
	agent: "claude",
	model: "opus",
	repository: target,
	checkout_directory: state === "running" ? "/tmp/clone" : "",
	actions: { retry: false, stop: true },
	started,
});

// A schedule's one board entry, its latest run, with runs in flight.
const scheduleEntry = (
	target: string,
	task: string,
	number: number,
	inFlight: number,
) => ({
	key: `${target}#${number}`,
	target,
	number,
	title: task,
	status: "active",
	state: "running",
	kind: "schedule",
	schedule_id: `coder/${task}`,
	agent: "claude",
	repository: target,
	actions: { retry: false, stop: true },
	started,
	recent_runs: [
		...Array.from({ length: inFlight }, (_, i) => ({
			number: number - i,
			status: "active",
			state: "running",
			started,
		})),
		{
			number: number - inFlight,
			status: "complete",
			state: "done",
			started,
			finished: started,
			result: "succeeded",
		},
	],
});

const JOBS = [
	issueJob("lsegal/aviary", 1347),
	scheduleEntry("lsegal/aviary", "aviary-find-dupe-tests", 1764140000, 2),
	scheduleEntry("lsegal/aviary", "aviary-update-docs", 1764141000, 2),
	// Starting and waiting jobs are not running ones.
	issueJob("lsegal/aviary", 1350, "active", "starting"),
	issueJob("lsegal/aviary", 1351, "waiting", "paused"),
	issueJob("lsegal/zvid", 11),
	issueJob("lsegal/zvid", 12),
	issueJob("lsegal/zvid", 13),
	issueJob("lsegal/zvid", 14),
	issueJob("lsegal/zvid", 15),
];

async function setup(page: Page) {
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: [{ name: "coder" }],
		job_list: [],
		task_upcoming: [],
		config_validate: [],
		config_get: {
			agents: [
				{
					name: "coder",
					model: "cli/claude/opus",
					projects: TARGETS.map((target) => ({ target })),
				},
			],
		},
		project_status: () => {
			const states = { starting: 0, paused: 0, running: 0, done: 0, failed: 0 };
			for (const j of JOBS) states[j.state as keyof typeof states]++;
			return {
				running: true,
				targets: TARGETS,
				issue_counts: Object.fromEntries(TARGETS.map((t) => [t, 3])),
				delivery: "poll",
				states,
				target_jobs: targetJobs(JOBS),
				// The engine counts every run: two of each schedule's.
				jobs: { running: 10 },
				clis: [],
				recent_logs: "",
			};
		},
		project_job_list: (args?: Record<string, unknown>) => {
			const list = (args?.kinds as string[] | undefined)?.includes("schedule")
				? []
				: JOBS;
			return { jobs: list, total: list.length, offset: 0, limit: 50 };
		},
		task_list: [],
	});
}

async function openFlyout(page: Page) {
	await setup(page);
	await page.goto("/agents/coder/chat");
	await page.getByTestId("sidebar-collapse").click();
	const icon = page.getByTestId("sidebar-rail-projects");
	await expect(icon.getByTestId("sidebar-rail-badge")).toHaveText("8");
	await icon.focus();
	const flyout = page.getByTestId("sidebar-rail-flyout");
	await expect(flyout).toBeVisible();
	return flyout;
}

test("the Code Loop flyout shows each project's running count, totalling the header", async ({
	page,
}) => {
	const flyout = await openFlyout(page);

	// Running projects first, then idle ones, dimmed and without a count.
	const projects = flyout.getByTestId("sidebar-rail-flyout-item");
	await expect(projects).toHaveText([
		/^lsegal\/aviary\s*3$/,
		/^lsegal\/zvid\s*5$/,
		/^lsegal\/forgo$/,
	]);
	await expect(projects.nth(2)).toHaveClass(/text-gray-400/);
	await expect(projects.nth(0)).not.toHaveClass(/text-gray-400/);

	// The header total is the sum of the project counts, as the board's
	// Running state counts them: a schedule once, however many runs overlap.
	await expect(flyout.getByTestId("sidebar-rail-tooltip")).toHaveText(
		"Code Loop · 8 running jobs",
	);

	// The count is enough: no running job is listed under a project.
	await expect(flyout.getByRole("link")).toHaveCount(3);
	await expect(flyout).not.toContainText("Issue 1347");
	await expect(flyout).not.toContainText("aviary-find-dupe-tests");
});

test("clicking a project in the Code Loop flyout opens its board", async ({
	page,
}) => {
	const flyout = await openFlyout(page);
	await flyout.getByTestId("sidebar-rail-flyout-item").first().click();
	await expect(page).toHaveURL(/\/agents\/coder\/projects\/lsegal%2Faviary$/);
});
