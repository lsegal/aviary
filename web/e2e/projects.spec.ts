import { expect, type Locator, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken, type ToolFixtures } from "./helpers/mockMCP";

// Fixtures follow the payloads internal/projects' Manager returns for
// project_status, project_job_list and project_job_get.

const minutesAgo = (m: number) =>
	new Date(Date.now() - m * 60_000).toISOString();

const STATUS = {
	identity: "7F3CA91B",
	running: true,
	targets: ["lsegal/aviary", "lsegal/zvidlib"],
	issue_counts: { "lsegal/aviary": 6, "lsegal/zvidlib": 0 },
	jobs: {
		running: 2,
		queued: 0,
		paused: 1,
		completed: 7,
		merged_24h: 3,
		failed: 1,
	},
	concurrency: 4,
	delivery: "poll",
	poll_interval: "30s",
	issue_filter: "assignee:@me",
	allowed_commenters: ["lsegal", "octocat"],
	quotas: {
		claude: "38% of 5h quota left",
		codex: "quota ok",
		gemini: "signed out",
	},
	agents: ["claude/opus", "codex", "gemini"],
	clis: [
		{ name: "claude", models: ["opus"], quota: "38% of 5h quota left" },
		{ name: "codex", models: ["gpt-5.6-terra"], quota: "quota ok" },
		{ name: "gemini", models: [], quota: "signed out" },
	],
	recent_logs:
		"12:03:58 checked lsegal/aviary: 6 ready issues\n12:04:11 claimed #221, cloning into ~/.aviary/clones/aviary-221",
	log_offset: 2,
	last_check: minutesAgo(0.1),
};

const actions = (status: string) => ({
	retry: status !== "queued",
	pause: status === "active" || status === "waiting",
	resume: status === "paused",
	stop: ["active", "waiting", "paused"].includes(status),
});

// The board state core.JobSnapshot.BoardState reports for each status of a
// job whose clone exists.
const STATE_OF: Record<string, string> = {
	queued: "starting",
	active: "running",
	stopping: "running",
	waiting: "paused",
	paused: "paused",
	complete: "done",
	failed: "failed",
};

const job = (fields: {
	number: number;
	title: string;
	status: string;
	agent: string;
	model: string;
	started: number;
	pr?: number;
	prRepo?: string;
	progress?: [number, number];
	waitReason?: string;
	target?: string;
	/** Overrides the board state, such as a running job with no clone yet. */
	state?: string;
	prState?: "draft" | "open" | "merged";
}) => ({
	key: `${fields.target ?? "lsegal/aviary"}#${fields.number}`,
	target: fields.target ?? "lsegal/aviary",
	number: fields.number,
	title: fields.title,
	status: fields.status,
	state: fields.state ?? STATE_OF[fields.status],
	kind: "issue",
	agent: fields.agent,
	model: fields.model,
	actions: actions(fields.status),
	repository: "lsegal/aviary",
	issue_url: `https://github.com/lsegal/aviary/issues/${fields.number}`,
	...(fields.pr
		? {
				pull_request: fields.pr,
				pull_request_url: `https://github.com/${fields.prRepo ?? "lsegal/aviary"}/pull/${fields.pr}`,
				pull_request_state: fields.prState ?? "open",
			}
		: {}),
	...(fields.progress
		? {
				progress: {
					done: fields.progress[0],
					total: fields.progress[1],
					percent: Math.round((fields.progress[0] / fields.progress[1]) * 100),
				},
			}
		: {}),
	started: minutesAgo(fields.started),
	...(fields.waitReason ? { wait_reason: fields.waitReason } : {}),
});

/** The clone each job's detail reports. */
const CLONES: Record<number, Record<string, unknown>> = {
	218: {
		checkout_directory: "/tmp/aviary-gh-fix-200",
		checkout_state: "present",
	},
	221: { checkout_directory: "", checkout_state: "none" },
	209: {
		checkout_directory: "/tmp/aviary-gh-fix-209",
		checkout_state: "removed",
	},
	200: {
		checkout_directory: "/tmp/aviary-gh-fix-200",
		checkout_state: "reused",
		checkout_reused_by: 218,
	},
};

const JOBS = [
	job({
		number: 218,
		title: "Cap scheduler retry backoff at 10 minutes",
		status: "active",
		agent: "claude",
		model: "opus",
		started: 14,
		pr: 233,
		prState: "draft",
		progress: [2, 5],
	}),
	job({
		number: 221,
		title: "Export a session as Markdown from the chat menu",
		status: "active",
		agent: "codex",
		model: "gpt-5.5",
		started: 3,
		state: "starting",
	}),
	job({
		number: 209,
		title: "Scoop install fails when ~/.local/bin is missing",
		status: "failed",
		agent: "claude",
		model: "opus",
		started: 60,
		pr: 227,
	}),
	job({
		number: 214,
		title: "Show token usage per scheduled task",
		status: "paused",
		agent: "claude",
		model: "opus",
		started: 41,
		pr: 229,
		prRepo: "someone/aviary",
		progress: [5, 6],
		waitReason: "Stacked on PR #226, waiting for it to merge",
	}),
	job({
		number: 200,
		title: "Add a dark theme toggle",
		status: "complete",
		agent: "claude",
		model: "opus",
		started: 300,
		pr: 205,
		prState: "merged",
	}),
];

const at = (m: number) => minutesAgo(m);

const EVENTS_218 = [
	{ kind: "system", time: at(14), text: "Claimed #218" },
	{ kind: "system", time: at(13), text: "Opened draft PR #233" },
	{
		kind: "message",
		time: at(12),
		text: "I can reproduce it: retries back off **without a cap**. I'll clamp the delay at 10 minutes.",
	},
	{
		kind: "reasoning",
		time: at(12),
		text: "The backoff doubles each attempt.",
	},
	{
		kind: "plan",
		time: at(11),
		items: [
			{ text: "Reproduce the unbounded backoff", status: "completed" },
			{ text: "Clamp the delay", status: "in_progress" },
			{ text: "Add a test", status: "pending" },
		],
	},
	{
		kind: "tool_call",
		time: at(11),
		call_id: "c1",
		name: "Read",
		input: { file_path: "internal/scheduler/queue.go" },
	},
	{
		kind: "tool_result",
		time: at(11),
		call_id: "c1",
		output: "package scheduler\n…",
	},
	{
		kind: "file_change",
		time: at(10),
		path: "internal/scheduler/queue.go",
		diff: "@@ -40,3 +40,4 @@\n delay := base << attempt\n-return delay\n+return min(delay, 10*time.Minute)\n+// capped",
	},
	{
		kind: "command",
		time: at(9),
		call_id: "b1",
		command: "go test ./internal/scheduler/...",
		status: "running",
	},
	{
		kind: "command",
		time: at(9),
		call_id: "b1",
		output: "--- FAIL: TestBackoffCap (0.01s)",
		exit_code: 1,
		is_error: true,
		status: "done",
	},
	{
		kind: "comment_relay",
		time: at(8),
		author: "lsegal",
		text: "Keep the jitter, please.",
		url: "https://github.com/lsegal/aviary/issues/218#issuecomment-1",
	},
	{
		kind: "command",
		time: at(7),
		call_id: "b2",
		command: "go test ./internal/scheduler/...",
		status: "running",
	},
	{
		kind: "command",
		time: at(7),
		call_id: "b2",
		output: "ok  internal/scheduler 2.41s",
		exit_code: 0,
		status: "done",
	},
	{
		kind: "usage",
		time: at(6),
		usage: { input_tokens: 182000, output_tokens: 9100, cost_usd: 1.42 },
	},
];

// The typed events and raw stdout project_job_get returns per job; tests may
// grow them to stand in for a running job.
const EVENTS: Record<number, object[]> = { 218: EVENTS_218 };
const RAW_OUTPUT: Record<number, string> = {
	218: '{"type":"assistant","message":{"content":[{"type":"text","text":"I can reproduce it"}]}}\n',
};

// project_job_list's project filter: the board passes targets, one or more.
// The board's toggle chip for one job state.
const stateChip = (page: Page, label: string) =>
	page
		.getByRole("group", { name: "Job filter" })
		.getByRole("button", { name: new RegExp(`^${label}`) });

const inTargets = (args: Record<string, unknown> | undefined, target: string) =>
	!Array.isArray(args?.targets) || args.targets.includes(target);

interface Calls {
	[tool: string]: Record<string, unknown>[];
}

async function setup(
	page: Page,
	status: () => object = () => STATUS,
	extra: ToolFixtures = {},
) {
	await setAuthToken(page);
	const calls: Calls = {};
	const record = (tool: string, args?: Record<string, unknown>) => {
		calls[tool] ??= [];
		calls[tool].push(args ?? {});
	};
	let tick = 0;
	const scrollback = (number: number) => {
		tick++;
		return [
			`▸ read internal/scheduler/queue.go (#${number})`,
			"$ go test ./internal/scheduler/...",
			`ok  internal/scheduler tick ${tick}`,
		].join("\n");
	};
	const ok = (tool: string) => (args?: Record<string, unknown>) => {
		record(tool, args);
		return { ok: true };
	};
	await mockMCP(page, {
		agent_list: [],
		// coder owns both watched projects, so their boards sit under it.
		config_get: {
			agents: [
				{
					name: "coder",
					model: "cli/claude/opus",
					projects: [{ target: "lsegal/aviary" }, { target: "lsegal/zvidlib" }],
				},
			],
		},
		// Like the Manager, project_status counts the jobs of the projects
		// it is passed in each board state.
		project_status: (args?: Record<string, unknown>) => {
			record("project_status", args);
			const states: Record<string, number> = {
				starting: 0,
				paused: 0,
				running: 0,
				done: 0,
				failed: 0,
			};
			for (const j of JOBS) if (inTargets(args, j.target)) states[j.state]++;
			return { states, ...status() };
		},
		project_job_list: (args?: Record<string, unknown>) => {
			record("project_job_list", args);
			const jobs = JOBS.filter(
				(j) =>
					inTargets(args, j.target) &&
					(!args?.status || j.status === args.status) &&
					(!args?.states || (args.states as string[]).includes(j.state)),
			);
			return { jobs, total: jobs.length, offset: 0, limit: args?.limit ?? 50 };
		},
		project_job_get: (args?: Record<string, unknown>) => {
			record("project_job_get", args);
			const found = JOBS.find(
				(j) => j.target === args?.target && j.number === args?.number,
			);
			if (!found) throw new Error("no such job");
			return {
				...found,
				...CLONES[found.number],
				session_id: "7f3c…a9",
				scrollback: scrollback(found.number),
				events: EVENTS[found.number] ?? [],
				raw_output: RAW_OUTPUT[found.number] ?? "",
			};
		},
		project_job_retry: ok("project_job_retry"),
		project_job_stop: ok("project_job_stop"),
		project_refresh: ok("project_refresh"),
		project_github_signin: ok("project_github_signin"),
		project_github_use_polling: ok("project_github_use_polling"),
		project_github_import: ok("project_github_import"),
		...extra,
	});
	return calls;
}

test("an agent's Code Loop in the sidebar list its projects with live counts", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/system/overview");
	// coder is the only agent, so its pages, Code Loop among them, are
	// the sidebar's top level (#538).
	const row = page.getByTestId("sidebar-row-projects");
	await expect(row.getByTestId("project-running-badge")).toHaveText("2");
	await expect(
		page.getByTestId("sidebar-nav").getByRole("link", { name: /^Code Loop/ }),
	).toHaveAttribute("href", "/agents/coder/projects");

	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	const projects = page.getByTestId("sidebar-agent-projects");
	// A project with running jobs shows only their count in a gray badge
	// (#636).
	const aviary = projects.getByRole("link", { name: /lsegal\/aviary/ });
	const badge = aviary.getByTestId("project-running-badge");
	await expect(badge).toHaveText("2");
	await expect(badge).toHaveAttribute("aria-label", "2 running jobs");
	await expect(badge).toHaveAttribute("title", "2 running jobs");
	await expect(badge).toHaveClass(/\bbg-gray-200\b/);
	await expect(aviary).not.toContainText("running");
	// An idle project shows nothing beside its name.
	const zvidlib = projects.getByRole("link", { name: /lsegal\/zvidlib/ });
	await expect(zvidlib).toHaveText("lsegal/zvidlib");
	await expect(zvidlib.getByTestId("project-running-badge")).toHaveCount(0);
	await expect(zvidlib).not.toContainText("idle");
	await expect(page.getByTestId("sidebar-add-project")).toHaveAttribute(
		"href",
		"/agents/coder/projects?add=1",
	);
});

test("the sidebar always shows Code Loop expanded with its projects (#867)", async ({
	page,
}) => {
	await setup(page);
	// Off the Code Loop pages, Code Loop still lists every project.
	await page.goto("/system/overview");
	const projects = page.getByTestId("sidebar-agent-projects");
	await expect(projects).toBeVisible();
	await expect(
		projects.getByRole("link", { name: /lsegal\/aviary/ }),
	).toHaveAttribute("href", "/agents/coder/projects/lsegal%2Faviary");
	await expect(
		projects.getByRole("link", { name: /lsegal\/zvidlib/ }),
	).toBeVisible();

	// Reloading, and leaving the board and coming back, keep it expanded.
	const nav = page.getByTestId("sidebar-nav");
	await nav.getByRole("link", { name: /^Code Loop/ }).click();
	await expect(page).toHaveURL(/\/agents\/coder\/projects$/);
	await expect(projects).toBeVisible();
	await nav.getByRole("link", { name: "Agent Settings" }).click();
	await expect(page).toHaveURL(/\/agents\/coder\/settings/);
	await expect(projects).toBeVisible();
	await page.reload();
	await expect(
		projects.getByRole("link", { name: /lsegal\/aviary/ }),
	).toBeVisible({ timeout: 15_000 });
});

test('the Code Loop running badge is gray and the Add project "+" sits beside it on hover', async ({
	page,
}) => {
	await setup(page);
	await page.goto("/system/overview");
	const row = page.getByTestId("sidebar-row-projects");
	const badge = row.getByTestId("project-running-badge");
	await expect(badge).toHaveText("2");
	await expect(badge).toHaveAttribute("aria-label", "2 running jobs");
	await expect(badge).toHaveClass(/\bbg-gray-200\b/);
	await expect(row.getByTestId("agent-running-badge")).toHaveCount(0);

	const plus = page.getByTestId("sidebar-add-project");
	await expect(plus).toHaveAttribute("aria-label", "Add project");
	const opacity = () =>
		plus.evaluate((el) => Number(getComputedStyle(el).opacity));
	await expect.poll(opacity).toBe(0);
	await page
		.getByTestId("sidebar-nav")
		.getByRole("link", { name: /^Code Loop/ })
		.hover();
	await expect.poll(opacity).toBe(1);

	const badgeBox = await badge.boundingBox();
	const plusBox = await plus.boundingBox();
	expect(badgeBox && plusBox).toBeTruthy();
	if (!badgeBox || !plusBox) return;
	expect(badgeBox.x + badgeBox.width).toBeLessThanOrEqual(plusBox.x);
});

test('Code Loop badges sit at the row\'s right edge until the "+" shows', async ({
	page,
}) => {
	await setup(page);
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	await page.mouse.move(800, 600);
	// Gap between a badge's right edge and its row's right edge.
	const gap = async (row: Locator, badge: Locator) => {
		const r = await row.boundingBox();
		const b = await badge.boundingBox();
		if (!r || !b) throw new Error("missing box");
		return r.x + r.width - (b.x + b.width);
	};
	const header = page
		.getByTestId("sidebar-nav")
		.getByRole("link", { name: /^Code Loop/ });
	const headerBadge = header.getByTestId("project-running-badge");
	const project = page
		.getByTestId("sidebar-agent-projects")
		.getByRole("link", { name: /lsegal\/aviary/ });
	const projectBadge = project.getByTestId("project-running-badge");
	// With nothing hovered both badges hug the right edge, inside the row's
	// own padding.
	await expect.poll(() => gap(header, headerBadge)).toBeLessThanOrEqual(12);
	await expect.poll(() => gap(project, projectBadge)).toBeLessThanOrEqual(12);

	// Hovering shows the "+", and the badge moves left of it.
	const plus = page.getByTestId("sidebar-add-project");
	await header.hover();
	await expect
		.poll(() => plus.evaluate((el) => Number(getComputedStyle(el).opacity)))
		.toBe(1);
	await expect.poll(() => gap(header, headerBadge)).toBeGreaterThan(24);
	const badgeBox = await headerBadge.boundingBox();
	const plusBox = await plus.boundingBox();
	if (!badgeBox || !plusBox) throw new Error("missing box");
	expect(badgeBox.x + badgeBox.width).toBeLessThanOrEqual(plusBox.x);
});

test("a project's running badge follows its jobs live in the sidebar", async ({
	page,
}) => {
	await page.clock.install();
	let running = 1;
	await setup(page, () => STATUS, {
		project_job_list: () => {
			const jobs = Array.from({ length: running }, (_, i) => ({
				...JOBS[0],
				target: "lsegal/zvidlib",
				number: 900 + i,
				status: "active",
			}));
			return { jobs, total: jobs.length, offset: 0, limit: 50 };
		},
	});
	await page.goto("/agents/coder/projects/lsegal%2Fzvidlib");
	const row = page
		.getByTestId("sidebar-agent-projects")
		.getByRole("link", { name: /lsegal\/zvidlib/ });
	const badge = row.getByTestId("project-running-badge");
	await expect(badge).toHaveText("1");
	await expect(badge).toHaveAttribute("aria-label", "1 running job");

	running = 3;
	await page.clock.runFor(10_000);
	await expect(badge).toHaveText("3");
	await expect(badge).toHaveAttribute("aria-label", "3 running jobs");

	running = 0;
	await page.clock.runFor(10_000);
	await expect(badge).toHaveCount(0);
	await expect(row).toHaveText("lsegal/zvidlib");
});

// One watched project with no running jobs shows no badge on the GitHub
// Projects row, not the number of watched projects (#701).
async function setupOneProject(page: Page, running: () => number) {
	await setup(page, () => ({ ...STATUS, targets: ["lsegal/aviary"] }), {
		config_get: {
			agents: [
				{
					name: "coder",
					model: "cli/claude/opus",
					projects: [{ target: "lsegal/aviary" }],
				},
			],
		},
		project_job_list: () => {
			const jobs = Array.from({ length: running() }, (_, i) => ({
				...JOBS[0],
				number: 900 + i,
				status: "active",
			}));
			return { jobs, total: jobs.length, offset: 0, limit: 50 };
		},
	});
}

test("the Code Loop row counts running jobs, not watched projects", async ({
	page,
}) => {
	await page.clock.install();
	let running = 0;
	await setupOneProject(page, () => running);
	await page.goto("/system/overview");
	const row = page
		.getByTestId("sidebar-row-projects")
		.getByRole("link", { name: /^Code Loop/ });
	await expect(row).toHaveText("Code Loop");
	await expect(row.getByTestId("project-running-badge")).toHaveCount(0);

	running = 1;
	await page.clock.runFor(10_000);
	await expect(row.getByTestId("project-running-badge")).toHaveText("1");

	running = 0;
	await page.clock.runFor(10_000);
	await expect(row.getByTestId("project-running-badge")).toHaveCount(0);
	await expect(row).toHaveText("Code Loop");
});

// The collapsed sidebar rail keeps the running count on its Code Loop
// icon (#724).
test("the collapsed rail's Code Loop icon counts running jobs", async ({
	page,
}) => {
	await page.clock.install();
	let running = 2;
	await setupOneProject(page, () => running);
	await page.goto("/system/overview");
	await page.getByTestId("sidebar-collapse").click();
	const icon = page.getByTestId("sidebar-rail-projects");
	// Code Loop's own loop-arrow-around-</> icon, not a generic grid (#1033).
	await expect(icon.getByTestId("code-loop-icon")).toBeVisible();
	await expect(icon.getByTestId("sidebar-rail-badge")).toHaveText("2");
	await icon.hover();
	await expect(page.getByRole("tooltip")).toHaveText(
		"Code Loop · 2 running jobs",
	);

	running = 0;
	await page.clock.runFor(10_000);
	await expect(icon.getByTestId("sidebar-rail-badge")).toHaveCount(0);
});

test.describe("on a phone", () => {
	test.use({ viewport: { width: 390, height: 844 } });

	test("the Code Loop tab counts running jobs, not watched projects", async ({
		page,
	}) => {
		await page.clock.install();
		let running = 0;
		await setupOneProject(page, () => running);
		await page.goto("/system/overview");
		const tab = page
			.getByRole("navigation", { name: "Main" })
			.getByRole("link", { name: /^Code Loop/ });
		await expect(tab).toHaveText("Code Loop");
		await expect(tab.getByTestId("project-running-badge")).toHaveCount(0);

		running = 1;
		await page.clock.runFor(10_000);
		await expect(tab.getByTestId("project-running-badge")).toHaveText("1");

		running = 0;
		await page.clock.runFor(10_000);
		await expect(tab.getByTestId("project-running-badge")).toHaveCount(0);
		await expect(tab).toHaveText("Code Loop");
	});
});

test("a board watching a project URL links its header to that URL", async ({
	page,
}) => {
	await setup(page);
	const board = "https://github.com/users/lsegal/projects/3";
	await page.goto(`/agents/coder/projects/${encodeURIComponent(board)}`);

	const link = page
		.getByRole("heading", { name: board })
		.getByRole("link", { name: board });
	await expect(link).toHaveAttribute("href", board);
	await expect(link).toHaveAttribute("target", "_blank");
});

test("board shows status, ordered job cards and live output", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto("/agents/coder/projects/lsegal%2Faviary");

	await expect(
		page.getByRole("heading", { name: "lsegal/aviary" }),
	).toBeVisible();
	const repoLink = page
		.getByRole("heading", { name: "lsegal/aviary" })
		.getByRole("link", { name: "lsegal/aviary" });
	await expect(repoLink).toHaveAttribute(
		"href",
		"https://github.com/lsegal/aviary",
	);
	await expect(repoLink).toHaveAttribute("target", "_blank");
	await expect(page.getByTestId("delivery-status")).toHaveText("live");
	await expect(page.getByTestId("instance-id")).toHaveText("Instance 7F3CA91B");
	await expect(page.getByText("poll · checked")).toContainText("every 30s");
	// The header names the issue search pickup runs, never a ready label.
	await expect(page.getByTestId("issue-filter")).toHaveText("assignee:@me");
	await expect(page.getByText("poll · checked")).toContainText(
		"Issues matching assignee:@me",
	);
	await expect(page.getByText("poll · checked")).not.toContainText("labeled");
	const stats = page.getByRole("region", { name: "Job counts" });
	await expect(stats).toContainText("2of 4 busy");
	await expect(stats).toContainText("Merged (24h)3");
	await expect(stats).not.toContainText("Completed");
	await expect(stats).not.toContainText("Waiting");
	await expect
		.poll(() => calls.project_status.at(-1)?.targets)
		.toEqual(["lsegal/aviary"]);
	// Each board state's card counts the jobs its filter chip shows.
	for (const label of ["Starting", "Paused", "Running", "Done", "Failed"]) {
		const card = stats.locator("div").filter({ hasText: label });
		await expect(card).toHaveText(new RegExp(`^${label}\\d+$`));
		const count = Number((await card.textContent())?.replace(/\D/g, ""));
		const chip = stateChip(page, label);
		await chip.click();
		await expect(page.getByTestId("job-card")).toHaveCount(count);
		await chip.click();
	}
	const clis = page.getByRole("region", { name: "CLI providers" });
	await expect(clis).toContainText("38% of 5h quota left");
	await expect(clis).toContainText("signed out");
	// A provider/model agent is its provider running a model, not a second
	// provider without a quota reading.
	const claude = clis.locator("> span", { hasText: "claude" });
	await expect(claude).toHaveCount(1);
	await expect(claude).toHaveText(/claudes*opuss*38% of 5h quota left/);
	await expect(clis).not.toContainText("claude/opus");
	await expect(clis).not.toContainText("no quota reported");

	const cards = page.getByTestId("job-card");
	await expect(cards).toHaveCount(5);
	// Running and errored jobs come first, finished jobs last.
	// #221's agent has not reported its clone yet, so it is still starting.
	await expect(cards.nth(0)).toContainText("Starting");
	await expect(cards.nth(0).getByTestId("job-state")).toHaveClass(/orange/);
	await expect(cards.nth(1)).toContainText("Running");
	await expect(cards.nth(2)).toContainText("#209");
	await expect(cards.nth(4)).toContainText("Done");
	await expect(cards.nth(0)).toContainText("gh-fix · codex/gpt-5.5");

	// Fork-aware PR link, waiting reason and checklist progress.
	const paused = cards.filter({ hasText: "#214" });
	await expect(
		paused.getByRole("link", { name: /PR someone\/aviary#229/ }),
	).toHaveAttribute("href", "https://github.com/someone/aviary/pull/229");
	await expect(paused.getByTestId("job-waiting")).toHaveText(
		"Stacked on PR #226, waiting for it to merge",
	);
	await expect(paused.getByTestId("job-progress")).toHaveAttribute(
		"style",
		/width: 83%/,
	);

	// Cards stream output and refresh within a few seconds; #221 emits no
	// events, so it shows its scrollback.
	const running = cards.filter({ hasText: "#221" }).getByTestId("job-output");
	await expect(running).toContainText(
		"read internal/scheduler/queue.go (#221)",
	);
	const first = await running.textContent();
	await expect(running).not.toHaveText(first ?? "", { timeout: 5_000 });
});

test("a board shown from the restart cache says it is refreshing until a live poll (#921)", async ({
	page,
}) => {
	let stale = true;
	await setup(page, () =>
		stale
			? {
					...STATUS,
					stale: true,
					stale_targets: ["lsegal/aviary"],
					cached_at: minutesAgo(3),
				}
			: STATUS,
	);
	await page.goto("/agents/coder/projects/lsegal%2Faviary");

	const indicator = page.getByTestId("stale-indicator");
	await expect(indicator).toContainText("refreshing… last updated 3m ago");
	// The cached board still renders its stats and cards meanwhile.
	await expect(page.getByRole("region", { name: "Job counts" })).toContainText(
		"Merged (24h)3",
	);
	await expect(
		page.getByText("Cap scheduler retry backoff at 10 minutes").first(),
	).toBeVisible();

	stale = false;
	await expect(indicator).toHaveCount(0);
});

test("long wrapped output never grows a card past half the viewport (#718)", async ({
	page,
}) => {
	await page.setViewportSize({ width: 1280, height: 700 });
	const line = `$ ${"go test ./internal/scheduler/... -run TestBackoffCap -count=1 ".repeat(6)}`;
	await setup(page, () => STATUS, {
		project_job_get: (args?: Record<string, unknown>) => {
			const found = JOBS.find(
				(j) => j.target === args?.target && j.number === args?.number,
			);
			if (!found) throw new Error("no such job");
			return {
				...found,
				...CLONES[found.number],
				scrollback: Array(12).fill(line).join("\n"),
				events: [],
				raw_output: "",
			};
		},
	});
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	const card = page.getByTestId("job-card").filter({ hasText: "#218" });
	const output = card.getByTestId("job-output");
	await expect(output).toContainText("TestBackoffCap");
	const cardBox = await card.boundingBox();
	const outputBox = await output.boundingBox();
	expect(cardBox?.height).toBeLessThanOrEqual(350);
	// The log area clips its content instead of stretching the card.
	expect(outputBox?.height).toBeLessThan(
		await output.evaluate((el) => el.scrollHeight),
	);
	// The header and actions keep their natural size.
	await expect(
		card.getByRole("link", { name: /#218 Cap scheduler/ }),
	).toBeInViewport();
	await expect(card.getByRole("button", { name: "Retry" })).toBeInViewport();
});

/** project_job_get for the board's fixtures, with #218's events from events(). */
const jobGetWithEvents =
	(events: () => unknown[]) => (args?: Record<string, unknown>) => {
		const found = JOBS.find(
			(j) => j.target === args?.target && j.number === args?.number,
		);
		if (!found) throw new Error("no such job");
		return {
			...found,
			...CLONES[found.number],
			scrollback: `$ go test ./... (#${found.number})`,
			events: found.number === 218 ? events() : [],
			raw_output: "",
		};
	};

test("a job card's wrapped meta line never starts with a separator (#1013)", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	const card = page.getByTestId("job-card").filter({ hasText: "#218" });
	const separator = card.getByTestId("job-meta-separator");
	const pr = card.getByTestId("job-pr");
	await expect(pr).toContainText("PR #233");
	// Squeeze the card until the PR wraps onto its own line.
	await card.evaluate((el) => {
		(el as HTMLElement).style.width = "90px";
	});
	const top = async (l: typeof pr) => (await l.boundingBox())?.y ?? 0;
	const number = card.getByRole("link", { name: "#218", exact: true });
	await expect.poll(() => top(pr)).toBeGreaterThan(await top(number));
	// The separator stays at the end of the line above it.
	expect(Math.abs((await top(separator)) - (await top(number)))).toBeLessThan(
		4,
	);
});

test("job cards show the transcript in a rich view by default, falling back to raw (#769)", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	const card = page.getByTestId("job-card").filter({ hasText: "#218" });
	const rich = card.getByTestId("job-output-rich");
	await expect(rich).toBeVisible();
	await expect(card.getByTestId("job-output")).toHaveCount(0);
	// Cards read enough events to fill their output area.
	expect(calls.project_job_get?.[0]?.events).toBe(40);

	// Agent messages render as markdown.
	await expect(rich.locator('[data-row="message"] strong')).toHaveText(
		"without a cap",
	);
	// Each tool call is one row with its summary and a status badge, its
	// full text in a tooltip.
	const read = rich.locator('[data-call-id="c1"]');
	await expect(read).toHaveCount(1);
	await expect(read).toContainText("Read internal/scheduler/queue.go");
	await expect(read.getByTestId("tool-status")).toHaveText("✓ ok");
	await expect(read.locator("span.font-mono")).toHaveAttribute(
		"title",
		"Read internal/scheduler/queue.go",
	);
	const commands = rich.locator('[data-row="tool"]', {
		hasText: "go test ./internal/scheduler/...",
	});
	await expect(commands.first().getByTestId("tool-status")).toHaveText(
		"✕ exit 1",
	);
	await expect(commands.last().getByTestId("tool-status")).toHaveText("✓ ok");
	await expect(
		rich.locator('[data-row="tool"]', { hasText: "edit internal/scheduler" }),
	).toContainText("+2 −1");

	// The toggle sits in the action row, rich selected.
	await expect(
		card.getByRole("button", { name: "Rich output" }),
	).toHaveAttribute("aria-pressed", "true");
	await expect(
		card.getByRole("button", { name: "Raw output" }),
	).toHaveAttribute("title", /raw terminal scrollback/);

	// A job with no events shows its scrollback, with no error.
	const fallback = page.getByTestId("job-card").filter({ hasText: "#221" });
	await expect(fallback.getByTestId("job-output")).toContainText(
		"read internal/scheduler/queue.go (#221)",
	);
	await expect(fallback.getByTestId("job-output-rich")).toHaveCount(0);
});

test("the rich/raw toggle applies to every card and survives a reload (#769)", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	const cards = page.getByTestId("job-card");
	await expect(cards).toHaveCount(5);
	await expect(page.getByTestId("job-output-rich")).toHaveCount(1);

	await cards
		.filter({ hasText: "#209" })
		.getByRole("button", { name: "Raw output" })
		.click();
	await expect(page.getByTestId("job-output-rich")).toHaveCount(0);
	await expect(page.getByTestId("job-output")).toHaveCount(5);
	const card = cards.filter({ hasText: "#218" });
	await expect(card.getByTestId("job-output")).toContainText(
		"read internal/scheduler/queue.go (#218)",
	);
	await expect(
		card.getByRole("button", { name: "Raw output" }),
	).toHaveAttribute("aria-pressed", "true");

	await page.reload();
	await expect(cards).toHaveCount(5);
	await expect(page.getByTestId("job-output")).toHaveCount(5);
	await expect(page.getByTestId("job-output-rich")).toHaveCount(0);
	// Raw output keeps streaming.
	const output = card.getByTestId("job-output");
	const first = await output.textContent();
	await expect(output).not.toHaveText(first ?? "", { timeout: 5_000 });

	// Switching back shows the rich view again.
	await card.getByRole("button", { name: "Rich output" }).click();
	await expect(page.getByTestId("job-output-rich")).toHaveCount(1);
});

for (const colorScheme of ["light", "dark"] as const) {
	test(`the rich view uses the raw view's colors and stays readable in the ${colorScheme} theme (#873)`, async ({
		page,
	}) => {
		await page.emulateMedia({ colorScheme });
		await setup(page);
		await page.goto("/agents/coder/projects/lsegal%2Faviary");
		const card = page.getByTestId("job-card").filter({ hasText: "#218" });
		const rich = card.getByTestId("job-output-rich");
		await expect(rich.locator('[data-row="tool"]').first()).toBeVisible();
		const colors = (el: Element) => {
			const s = getComputedStyle(el);
			return { background: s.backgroundColor, color: s.color };
		};
		const richColors = await rich.evaluate(colors);

		// Every text element in the rich view contrasts with the surface.
		const contrasts = await rich.evaluate((root) => {
			// Computed colors may be oklch(); a canvas pixel normalizes them.
			const ctx = document
				.createElement("canvas")
				.getContext("2d", { willReadFrequently: true });
			if (!ctx) throw new Error("no canvas");
			const rgb = (c: string) => {
				ctx.clearRect(0, 0, 1, 1);
				ctx.fillStyle = c;
				ctx.fillRect(0, 0, 1, 1);
				const [r, g, b, a] = ctx.getImageData(0, 0, 1, 1).data;
				return [r, g, b, a / 255];
			};
			// Composite a translucent color over the opaque one under it.
			const over = (top: number[], under: number[]) => {
				const a = top[3] ?? 1;
				return [0, 1, 2].map((i) => top[i] * a + under[i] * (1 - a));
			};
			const lum = (c: number[]) => {
				const [r, g, b] = c.map((v) => {
					const s = v / 255;
					return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
				});
				return 0.2126 * r + 0.7152 * g + 0.0722 * b;
			};
			const surface = (el: Element): number[] => {
				const chain: Element[] = [];
				for (let e: Element | null = el; e; e = e.parentElement) chain.push(e);
				let bg = [255, 255, 255];
				for (const e of chain.reverse()) {
					const c = rgb(getComputedStyle(e).backgroundColor);
					if ((c[3] ?? 1) > 0) bg = over(c, bg);
				}
				return bg;
			};
			const out: { text: string; ratio: number }[] = [];
			for (const el of root.querySelectorAll("li, li *")) {
				if (el.closest("pre")) continue;
				const own = [...el.childNodes].some(
					(n) => n.nodeType === Node.TEXT_NODE && n.textContent?.trim(),
				);
				if (!own) continue;
				const bg = surface(el);
				const fg = over(rgb(getComputedStyle(el).color), bg);
				const [hi, lo] = [lum(fg), lum(bg)].sort((a, b) => b - a);
				out.push({
					text: (el.textContent ?? "").trim().slice(0, 40),
					ratio: (hi + 0.05) / (lo + 0.05),
				});
			}
			return out;
		});
		expect(contrasts.length).toBeGreaterThan(5);
		expect(contrasts.filter((c) => c.ratio < 4.5)).toEqual([]);

		await card.getByRole("button", { name: "Raw output" }).click();
		const raw = card.getByTestId("job-output");
		await expect(raw).toBeVisible();
		expect(await raw.evaluate(colors)).toEqual(richColors);
	});
}

test("a tool call's status updates in place without duplicate rows (#769)", async ({
	page,
}) => {
	let finished = false;
	let failed = false;
	const events = () => [
		{ kind: "message", time: at(2), text: "Running the tests." },
		{
			kind: "tool_call",
			time: at(2),
			call_id: "t1",
			name: "Bash",
			input: { command: "go test ./internal/scheduler/... -run TestBackoff" },
		},
		...(finished
			? [
					{
						kind: "tool_result",
						time: at(1),
						call_id: "t1",
						output: "ok",
					},
				]
			: []),
		{
			kind: "tool_call",
			time: at(1),
			call_id: "t2",
			name: "Read",
			input: { file_path: "missing.go" },
		},
		...(failed
			? [
					{
						kind: "tool_result",
						time: at(1),
						call_id: "t2",
						output: "no such file",
						is_error: true,
					},
				]
			: []),
	];
	await setup(page, () => STATUS, {
		project_job_get: jobGetWithEvents(events),
	});
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	const rich = page
		.getByTestId("job-card")
		.filter({ hasText: "#218" })
		.getByTestId("job-output-rich");
	const bash = rich.locator('[data-call-id="t1"]');
	const read = rich.locator('[data-call-id="t2"]');
	await expect(bash.getByTestId("tool-status")).toHaveText("running");
	await expect(read.getByTestId("tool-status")).toHaveText("running");

	finished = true;
	failed = true;
	await expect(bash.getByTestId("tool-status")).toHaveText("✓ ok");
	await expect(read.getByTestId("tool-status")).toHaveText("✕ error");
	await expect(read.getByTestId("tool-status")).toHaveAttribute(
		"title",
		"no such file",
	);
	await expect(rich.locator('[data-row="tool"]')).toHaveCount(2);
	await expect(bash).toHaveCount(1);
	await expect(read).toHaveCount(1);
});

test("rich card output stays pinned to the bottom unless scrolled up (#769)", async ({
	page,
}) => {
	let count = 30;
	const events = () =>
		Array.from({ length: count }, (_, i) => ({
			kind: "message",
			time: at(1),
			text: `Step ${i + 1} of the plan is done.`,
		}));
	await setup(page, () => STATUS, {
		project_job_get: jobGetWithEvents(events),
	});
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	const rich = page
		.getByTestId("job-card")
		.filter({ hasText: "#218" })
		.getByTestId("job-output-rich");
	const gap = () =>
		rich.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight);
	await expect(rich).toContainText("Step 30 of the plan");
	await expect.poll(gap).toBeLessThanOrEqual(1);

	// New output keeps the view at the bottom.
	count = 34;
	await expect(rich).toContainText("Step 34 of the plan");
	await expect.poll(gap).toBeLessThanOrEqual(1);

	// Scrolled up, it stays put as output arrives.
	await rich.evaluate((el) => {
		el.scrollTop = 0;
		el.dispatchEvent(new Event("scroll"));
	});
	count = 38;
	await expect(rich).toContainText("Step 38 of the plan");
	expect(await rich.evaluate((el) => el.scrollTop)).toBe(0);

	// The shared "More below" pill counts the new rows and follows again
	// (#1072).
	const card = page.getByTestId("job-card").filter({ hasText: "#218" });
	const pill = card.getByTestId("job-output-more-below");
	await expect(pill).toHaveText("4 new · More below ↓");
	await pill.click();
	await expect.poll(gap).toBeLessThanOrEqual(1);
	await expect(pill).toBeHidden();
	count = 40;
	await expect(rich).toContainText("Step 40 of the plan");
	await expect.poll(gap).toBeLessThanOrEqual(1);
});

test("job detail's transcript opens at the bottom and pauses with the pill (#1072)", async ({
	page,
}) => {
	await page.setViewportSize({ width: 1600, height: 900 });
	let count = 40;
	const events = () =>
		Array.from({ length: count }, (_, i) => ({
			kind: "message",
			time: at(1),
			text: `Step ${i + 1} of the plan is done.`,
		}));
	await setup(page, () => STATUS, {
		project_job_get: jobGetWithEvents(events),
	});
	await page.goto("/agents/coder/projects/lsegal%2Faviary/jobs/218");
	const transcript = page.getByTestId("job-transcript");
	const pill = page.getByTestId("job-transcript-more-below");
	const gap = () =>
		transcript.evaluate(
			(el) => el.scrollHeight - el.scrollTop - el.clientHeight,
		);
	await expect(transcript).toContainText("Step 40 of the plan");
	await expect.poll(gap).toBeLessThanOrEqual(8);

	await transcript.evaluate((el) => {
		el.scrollTop = 0;
	});
	await expect(pill).toBeVisible();
	count = 42;
	await expect(transcript).toContainText("Step 42 of the plan");
	expect(await transcript.evaluate((el) => el.scrollTop)).toBe(0);
	await expect(pill).toHaveText("2 new · More below ↓");

	await pill.click();
	await expect.poll(gap).toBeLessThanOrEqual(8);
	await expect(pill).toBeHidden();
	count = 44;
	await expect(transcript).toContainText("Step 44 of the plan");
	await expect.poll(gap).toBeLessThanOrEqual(8);
});

/** Asserts el's right edge sits at the card's right padding. */
async function expectRightAligned(card: Locator, el: Locator) {
	const c = await card.boundingBox();
	const e = await el.boundingBox();
	expect(c && e).toBeTruthy();
	if (!c || !e) return;
	expect(e.x).toBeGreaterThanOrEqual(c.x);
	expect(c.x + c.width - (e.x + e.width)).toBeLessThanOrEqual(20);
}

for (const [label, width] of [
	["desktop", 1280],
	["mobile", 390],
] as const) {
	test(`job cards put the time top right and the agent label bottom right on ${label} (#756)`, async ({
		page,
	}) => {
		await page.setViewportSize({ width, height: 900 });
		await setup(page);
		await page.goto("/agents/coder/projects/lsegal%2Faviary");
		const cards = page.getByTestId("job-card");
		await expect(cards).toHaveCount(5);
		for (const state of ["Starting", "Running", "Failed", "Paused", "Done"]) {
			const card = cards.filter({
				has: page.getByTestId("job-state").getByText(state),
			});
			const time = card.getByTestId("job-time");
			const agent = card.getByTestId("job-agent");
			const title = card.getByRole("heading");
			await expect(time).toHaveText(/\d+(m|h)/);
			await expect(time).toHaveAttribute("title", /^Started /);
			await expect(agent).toHaveText(/^gh-fix · /);
			await expect(agent).toHaveAttribute("title", /^gh-fix · /);
			await expectRightAligned(card, time);
			await expectRightAligned(card, agent);
			const timeBox = await time.boundingBox();
			const titleBox = await title.boundingBox();
			const agentBox = await agent.boundingBox();
			const retryBox = await card
				.getByRole("button", { name: "Retry" })
				.boundingBox();
			// The time stays in the header row above the title; the label sits
			// in the footer beside the actions.
			expect(timeBox!.y + timeBox!.height).toBeLessThanOrEqual(titleBox!.y);
			expect(agentBox!.y).toBeGreaterThan(titleBox!.y + titleBox!.height);
			expect(Math.abs(agentBox!.y - retryBox!.y)).toBeLessThan(
				retryBox!.height,
			);
		}
	});
}

test("a long agent label truncates inside the card with a tooltip (#756)", async ({
	page,
}) => {
	await page.setViewportSize({ width: 390, height: 900 });
	const model = `claude/${"very-long-model-name-".repeat(6)}opus`;
	const jobs = JOBS.filter((j) => j.number === 218).map((j) => ({
		...j,
		model,
	}));
	await setup(page, () => STATUS, {
		project_job_list: () => ({ jobs, total: 1, offset: 0, limit: 50 }),
	});
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	const card = page.getByTestId("job-card");
	await expect(card).toHaveCount(1);
	const agent = card.getByTestId("job-agent");
	await expect(agent).toHaveAttribute("title", new RegExp(`${model}$`));
	await expect(agent).toHaveCSS("text-overflow", "ellipsis");
	await expect(agent).toHaveCSS("white-space", "nowrap");
	const truncated = await agent.evaluate(
		(el) => el.scrollWidth > el.clientWidth,
	);
	expect(truncated).toBe(true);
	await expectRightAligned(card, agent);
	const cardBox = await card.boundingBox();
	const agentBox = await agent.boundingBox();
	expect(agentBox!.x + agentBox!.width).toBeLessThanOrEqual(
		cardBox!.x + cardBox!.width,
	);
	// One line tall, and the title keeps the full header width.
	expect(agentBox!.height).toBeLessThan(24);
	const title = card.getByRole("heading");
	const titleBox = await title.boundingBox();
	expect(titleBox!.width).toBeGreaterThan(cardBox!.width - 40);
});

test("retry, stop, filters and check now call project tools", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	const card = page.getByTestId("job-card").filter({ hasText: "#218" });
	await card.getByRole("button", { name: "Retry" }).click();
	await expect
		.poll(() => calls.project_job_retry?.[0])
		.toEqual({ target: "lsegal/aviary", number: 218 });
	await card.getByRole("button", { name: "Stop" }).click();
	await expect
		.poll(() => calls.project_job_stop?.[0])
		.toEqual({ target: "lsegal/aviary", number: 218 });
	// The engine's actions decide what each card offers.
	const done = page.getByTestId("job-card").filter({ hasText: "#200" });
	await expect(done.getByRole("button", { name: "Stop" })).toHaveCount(0);

	await stateChip(page, "Paused").click();
	await expect(page.getByTestId("job-card")).toHaveCount(1);
	await expect(page.getByTestId("job-card")).toContainText("#214");
	await expect
		.poll(() =>
			calls.project_job_list.some(
				(c) => JSON.stringify(c.states) === '["paused"]',
			),
		)
		.toBe(true);

	const checkNow = page.getByRole("button", { name: "Check now" });
	await expect(checkNow).toHaveAttribute("title", "Check now");
	await expect(checkNow).toHaveText("");
	await checkNow.click();
	await expect
		.poll(() => calls.project_refresh?.[0])
		.toEqual({ target: "lsegal/aviary" });
});

const ZVID_JOB = job({
	number: 7,
	title: "Decode B-frames",
	status: "active",
	agent: "claude",
	model: "opus",
	started: 5,
	target: "lsegal/zvidlib",
});

test("an agent's Code Loop is one board of all its projects, with no settings", async ({
	page,
}) => {
	const calls = await setup(page, undefined, {
		project_job_list: (args?: Record<string, unknown>) => {
			calls.project_job_list ??= [];
			calls.project_job_list.push(args ?? {});
			const query = typeof args?.query === "string" ? args.query : "";
			const jobs = [...JOBS, ZVID_JOB].filter(
				(j) =>
					inTargets(args, j.target) &&
					(!args?.status || j.status === args.status) &&
					(!args?.states || (args.states as string[]).includes(j.state)) &&
					(!query || j.title.toLowerCase().includes(query.toLowerCase())),
			);
			return { jobs, total: jobs.length, offset: 0, limit: args?.limit ?? 50 };
		},
	});
	await page.goto("/agents/coder/projects");

	await expect(page.getByRole("heading", { name: "Code Loop" })).toBeVisible();
	await expect(page.getByTestId("board-projects")).toHaveText("2 projects");
	// Every project's jobs share the board, each naming its project.
	const cards = page.getByTestId("job-card");
	await expect(cards).toHaveCount(6);
	// The board's own call names its targets; the project pills' poll reads
	// every project's jobs (#1044).
	expect(
		calls.project_job_list.filter((c) => c.targets).at(-1)?.targets,
	).toEqual(["lsegal/aviary", "lsegal/zvidlib"]);
	const zvid = cards.filter({ hasText: "Decode B-frames" });
	await expect(zvid.getByTestId("job-project")).toHaveText("lsegal/zvidlib");
	await expect(zvid.getByTestId("job-project")).toHaveAttribute(
		"href",
		"/agents/coder/projects/lsegal%2Fzvidlib",
	);

	// No settings live on the route: no form fields, and Settings opens the
	// agent's projects in Code Loop settings.
	await expect(page.locator("[data-field]")).toHaveCount(0);
	await expect(page.getByTestId("agent-projects")).toHaveCount(0);
	await expect(page.getByTestId("project-settings-link")).toHaveAttribute(
		"href",
		"/projects/settings?agent=coder",
	);

	// Filters and search narrow the whole board, as on one project's.
	await stateChip(page, "Running").click();
	await expect(cards).toHaveCount(2);
	await page.getByLabel("Search jobs").fill("B-frames");
	await expect(cards).toHaveCount(1);
	await expect(cards).toContainText("lsegal/zvidlib");

	// One check reads every project.
	await page.getByLabel("Search jobs").fill("");
	await page.getByRole("button", { name: "Check now" }).click();
	await expect.poll(() => calls.project_refresh?.[0]).toEqual({});
});

test("the Code Loop board's header starts with a blue + that adds a project (#739, #1147, #1197)", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/agents/coder/projects");
	const icons = page.getByTestId("board-add-project").locator("..");
	const add = icons.locator(":scope > *").nth(1);
	await expect(add).toHaveAttribute("aria-label", "Add project");
	await expect(add).toHaveAttribute("title", "Add project");
	// The sidebar's blue + (#1197): a white + on the same blue.
	const blue = (el: Element) => getComputedStyle(el).backgroundColor;
	await expect
		.poll(() => add.evaluate(blue))
		.toBe(await page.getByTestId("sidebar-add-project").evaluate(blue));
	await expect(add).toHaveCSS("color", "rgb(255, 255, 255)");
	await expect(icons.locator(":scope > *").nth(2)).toHaveAttribute(
		"aria-label",
		"Check now",
	);
	await add.click();
	await expect(page).toHaveURL(/\/agents\/coder\/projects\?add=1$/);
	await expect(
		page.getByRole("dialog", { name: "Watch a repository" }),
	).toBeVisible();

	// One project's board has no + in its header.
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	await expect(page.getByRole("button", { name: "Check now" })).toBeVisible();
	await expect(page.getByTestId("board-add-project")).toHaveCount(0);
});

test("one project's board leaves its cards unlabelled", async ({ page }) => {
	await setup(page);
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	await expect(page.getByTestId("job-card")).toHaveCount(5);
	await expect(page.getByTestId("job-project")).toHaveCount(0);
});

test("job actions are icon buttons with tooltips", async ({ page }) => {
	await setup(page);
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	const card = page.getByTestId("job-card").filter({ hasText: "#218" });
	const actions = [
		card.getByRole("link", { name: "Details" }),
		card.getByRole("button", { name: "Retry" }),
		card.getByRole("button", { name: "Summarize" }),
		card.getByRole("button", { name: "Stop" }),
	];
	for (const action of actions) {
		await expect(action.locator("svg")).toHaveCount(1);
		await expect(action).toHaveText("");
		await expect(action).toHaveAttribute("title", /\S/);
	}

	await page.goto("/agents/coder/projects/lsegal%2Faviary/jobs/218");
	for (const name of ["Retry", "Stop"]) {
		const action = page.getByRole("button", { name });
		await expect(action.locator("svg")).toHaveCount(1);
		await expect(action).toHaveText("");
		await expect(action).toHaveAttribute("title", /\S/);
	}
});

test("job detail links #NNN and PR #YYY in the title, with icon-only actions (#835)", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/agents/coder/projects/lsegal%2Faviary/jobs/218");
	const title = page.getByTestId("job-detail-title");
	await expect(title.getByRole("link", { name: "#218" })).toHaveAttribute(
		"href",
		"https://github.com/lsegal/aviary/issues/218",
	);
	await expect(title.getByRole("link", { name: "PR #233" })).toHaveAttribute(
		"href",
		"https://github.com/lsegal/aviary/pull/233",
	);
	// The header row has no Issue/PR button links left, only icon buttons.
	const actions = page.getByTestId("job-detail-actions");
	await expect(actions.getByRole("link", { name: /#\d+/ })).toHaveCount(0);
	const toggle = page.getByTestId("raw-output-toggle");
	for (const width of [390, 820, 1600]) {
		await page.setViewportSize({ width, height: 900 });
		await expect(toggle).toHaveText("");
		await expect(toggle.locator("svg")).toHaveCount(1);
		await expect(toggle).toHaveAttribute("aria-label", "Raw output");
		await expect(toggle).toHaveAttribute("title", /\S/);
	}
	// Closing the details is a plain dismiss icon, not a bordered action.
	expect(
		await page
			.getByTestId("job-detail-close")
			.evaluate((el) => getComputedStyle(el).borderTopWidth),
	).toBe("0px");

	await page.goto("/agents/coder/projects/lsegal%2Faviary/jobs/221");
	await expect(title).toHaveText(
		"#221 Export a session as Markdown from the chat menu",
	);
	await expect(title.getByRole("link")).toHaveCount(1);
});

for (const width of [390, 1600]) {
	test(`job detail's rich transcript wraps instead of scrolling sideways at ${width}px (#835)`, async ({
		page,
	}) => {
		const long = `${"x".repeat(400)} ${"/very/long/path".repeat(30)}`;
		EVENTS[221] = [
			{
				kind: "message",
				time: at(5),
				text: `Long \`${long}\`\n\n\`\`\`\n${long}\n\`\`\``,
			},
			{
				kind: "tool_call",
				time: at(4),
				call_id: "t1",
				name: "Read",
				input: { file_path: long },
			},
			{ kind: "tool_result", time: at(4), call_id: "t1", output: long },
			{
				kind: "file_change",
				time: at(3),
				path: "a.go",
				diff: `@@ -1 +1 @@\n-${long}\n+${long}`,
			},
			{
				kind: "command",
				time: at(2),
				call_id: "b1",
				command: `echo ${long}`,
				status: "running",
			},
			{
				kind: "command",
				time: at(2),
				call_id: "b1",
				output: long,
				exit_code: 0,
			},
		];
		try {
			await page.setViewportSize({ width, height: 900 });
			await setup(page);
			await page.goto("/agents/coder/projects/lsegal%2Faviary/jobs/221");
			const timeline = page.getByRole("list", { name: "Activity" });
			await expect(timeline.locator('[data-event="command"]')).toHaveCount(1);
			for (const summary of await timeline.locator("summary").all()) {
				await summary.click();
			}
			await expect(timeline.locator("details[open]")).toHaveCount(3);
			// Nothing scrolls sideways: not the pane, the list, or any scroller in it.
			const overflowing = await timeline.evaluate((list) =>
				[list.parentElement as Element, list, ...list.querySelectorAll("*")]
					.filter(
						(el) =>
							(el === list ||
								el === list.parentElement ||
								/auto|scroll/.test(getComputedStyle(el).overflowX)) &&
							el.scrollWidth - el.clientWidth > 1,
					)
					.map((el) => el.outerHTML.slice(0, 100)),
			);
			expect(overflowing).toEqual([]);
		} finally {
			delete EVENTS[221];
		}
	});
}

test("job detail shows the job's clone path, copyable, or why it has none", async ({
	page,
	context,
}) => {
	await context.grantPermissions(["clipboard-read", "clipboard-write"]);
	await setup(page);
	const clone = page.getByTestId("job-clone");

	await page.goto("/agents/coder/projects/lsegal%2Faviary/jobs/218");
	await expect(page.getByTestId("job-clone-path")).toHaveText(
		"/tmp/aviary-gh-fix-200",
	);
	await clone.getByRole("button", { name: "Copy clone path" }).click();
	await expect(
		clone.getByRole("button", { name: "Copied clone path" }),
	).toBeVisible();
	expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(
		"/tmp/aviary-gh-fix-200",
	);

	await page.goto("/agents/coder/projects/lsegal%2Faviary/jobs/221");
	await expect(clone).toContainText("No clone yet.");
	await expect(clone.getByRole("button")).toHaveCount(0);

	await page.goto("/agents/coder/projects/lsegal%2Faviary/jobs/209");
	await expect(clone).toContainText(
		"This job's clone was removed: /tmp/aviary-gh-fix-209",
	);
	await expect(clone.getByRole("button")).toHaveCount(0);

	await page.goto("/agents/coder/projects/lsegal%2Faviary/jobs/200");
	await expect(clone).toContainText(
		"This job's clone was handed to #218: /tmp/aviary-gh-fix-200",
	);
});

test("run view renders the typed event timeline read-only", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	await page
		.getByTestId("job-card")
		.filter({ hasText: "#218" })
		.getByRole("link", { name: "Details" })
		.click();
	await expect(page).toHaveURL(
		"/agents/coder/projects/lsegal%2Faviary/jobs/218",
	);
	await expect(page.getByTestId("job-detail-title")).toHaveText(
		/^#218 • (Draft pull request)?PR #233 Cap scheduler retry backoff at 10 minutes$/,
	);

	const steps = page.getByTestId("lifecycle");
	await expect(steps.locator('[aria-current="step"]')).toContainText(
		"Implementing",
	);

	const timeline = page.getByRole("list", { name: "Activity" });
	await expect(timeline.locator("strong")).toHaveText("without a cap");
	// Each command is one row whose badge carries its result.
	const commands = timeline.locator('[data-event="command"]');
	await expect(commands).toHaveCount(2);
	await expect(commands.first().getByTestId("command-status")).toHaveText(
		"failed · exit 1",
	);
	await expect(commands.last().getByTestId("command-status")).toHaveText("ok");
	await expect(timeline).not.toContainText("running");
	await commands.first().locator("summary").click();
	await expect(commands.first()).toContainText("--- FAIL: TestBackoffCap");
	const edit = timeline.locator('[data-event="file_change"]');
	await expect(edit).toContainText("edit internal/scheduler/queue.go");
	await expect(edit).toContainText("+2");
	await expect(edit).toContainText("-1");
	await expect(timeline.locator('[data-event="plan"]')).toContainText(
		"Clamp the delay",
	);
	await timeline.getByText("Read", { exact: true }).click();
	await expect(timeline.locator('[data-event="tool_call"]')).toContainText(
		"package scheduler",
	);
	await expect(timeline.locator('[data-event="comment_relay"]')).toContainText(
		"lsegal commented",
	);
	await expect(timeline.locator('[data-event="comment_relay"]')).toContainText(
		"Keep the jitter, please.",
	);
	await expect(timeline.locator('[data-event="usage"]')).toContainText("$1.42");

	// Raw CLI stdout sits beside the timeline once opened.
	await page.getByTestId("raw-output-toggle").click();
	await expect(page.getByTestId("run-output")).toContainText(
		'"type":"assistant"',
	);
	await expect(
		page.getByRole("link", { name: "Comment on #218 ↗" }),
	).toHaveAttribute(
		"href",
		"https://github.com/lsegal/aviary/issues/218#new_comment_field",
	);

	// The footer names who may steer the job by commenting.
	const commenters = page.getByTestId("allowed-commenters");
	await expect(commenters).toHaveText("@lsegal, @octocat");
	await expect(
		commenters.getByRole("link", { name: "@octocat" }),
	).toHaveAttribute("href", "https://github.com/octocat");

	// No steering: nothing on the page can send a message to the job.
	await expect(page.getByRole("textbox")).toHaveCount(0);
	await expect(page.getByRole("button", { name: /send/i })).toHaveCount(0);
});

test("the run view stepper follows the PR out of draft, like the board icon", async ({
	page,
}) => {
	// #218's events never mention review or CI, so only its PR state can move
	// the stepper past Implementing.
	const job218 = JOBS.find((j) => j.number === 218) as Record<string, unknown>;
	try {
		await setup(page);
		const steps = page.getByTestId("lifecycle");
		await page.goto("/agents/coder/projects/lsegal%2Faviary/jobs/218");
		await expect(steps.locator('[aria-current="step"]')).toContainText(
			"Implementing",
		);

		job218.pull_request_state = "open";
		await page.goto("/agents/coder/projects/lsegal%2Faviary");
		await expect(
			page
				.getByTestId("job-card")
				.filter({ hasText: "#218" })
				.getByTestId("pr-state-icon"),
		).toHaveAttribute("aria-label", "Open pull request");

		await page.goto("/agents/coder/projects/lsegal%2Faviary/jobs/218");
		await expect(steps.locator('[aria-current="step"]')).toContainText("CI");
		await expect(steps.locator("li").nth(3)).toContainText("✓");
		await expect(steps.locator("li").nth(5)).not.toContainText("✓");
	} finally {
		job218.pull_request_state = "draft";
	}
});

test("the run view stepper shows a merged PR as done", async ({ page }) => {
	await setup(page);
	await page.goto("/agents/coder/projects/lsegal%2Faviary/jobs/200");
	const steps = page.getByTestId("lifecycle");
	await expect(steps.locator('[aria-current="step"]')).toHaveCount(0);
	await expect(steps.locator("li").nth(5)).toContainText("✓");
});

test("job detail commands are single rows whose badge resolves in place (#774)", async ({
	page,
}) => {
	const events: object[] = [
		{
			kind: "command",
			time: at(3),
			call_id: "r1",
			command: "pnpm lint",
			status: "running",
		},
	];
	EVENTS[221] = events;
	try {
		await setup(page);
		await page.goto("/agents/coder/projects/lsegal%2Faviary/jobs/221");
		const rows = page
			.getByRole("list", { name: "Activity" })
			.locator('[data-event="command"]');
		await expect(rows).toHaveCount(1);
		const badge = rows.first().getByTestId("command-status");
		await expect(badge).toHaveText("running");

		events.push({
			kind: "command",
			time: at(2),
			call_id: "r1",
			output: "Checked 107 files",
			exit_code: 0,
			status: "done",
		});
		await expect(badge).toHaveText("ok");
		await expect(badge).toHaveAttribute("title", "Succeeded");
		await expect(rows).toHaveCount(1);

		events.push(
			{
				kind: "command",
				time: at(1),
				call_id: "r2",
				command: "go test ./...",
				status: "running",
			},
			{
				kind: "command",
				time: at(1),
				call_id: "r2",
				output: "FAIL",
				exit_code: 2,
				is_error: true,
				status: "done",
			},
		);
		await expect(rows).toHaveCount(2);
		const failed = rows.last().getByTestId("command-status");
		await expect(failed).toHaveText("failed · exit 2");
		await expect(failed).toHaveAttribute("title", "Failed with exit code 2");
		await expect(page.getByText(/^exit \d/)).toHaveCount(0);
	} finally {
		delete EVENTS[221];
	}
});

test("job detail's raw Output starts collapsed, toggles, remembers, and hides heartbeats (#774)", async ({
	page,
}) => {
	RAW_OUTPUT[221] = [
		'{"type":"assistant","message":{"content":[{"type":"text","text":"Working"}]}}',
		'{"type":"tool_progress","tool_use_id":"t1","heartbeat":true,"elapsed_time_seconds":30}',
	].join("\n");
	try {
		await setup(page);
		await page.goto("/agents/coder/projects/lsegal%2Faviary/jobs/221");
		const toggle = page.getByTestId("raw-output-toggle");
		const panel = page.getByTestId("raw-output");
		await expect(page.getByRole("list", { name: "Activity" })).toBeVisible();
		await expect(panel).toHaveCount(0);
		await expect(toggle).toHaveAttribute("aria-pressed", "false");

		await toggle.click();
		await expect(panel).toBeVisible();
		const output = page.getByTestId("run-output");
		await expect(output).toContainText('"text":"Working"');
		await expect(output).not.toContainText("tool_progress");
		await page.getByTestId("raw-output-heartbeats").check();
		await expect(output).toContainText('"heartbeat":true');

		await page.reload();
		await expect(panel).toBeVisible();
		await expect(toggle).toHaveAttribute("aria-pressed", "true");

		await toggle.click();
		await expect(panel).toHaveCount(0);
		await page.reload();
		await expect(page.getByRole("list", { name: "Activity" })).toBeVisible();
		await expect(panel).toHaveCount(0);
	} finally {
		delete RAW_OUTPUT[221];
	}
});

test("job detail shows the clone with session and agent above the stepper (#774)", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/agents/coder/projects/lsegal%2Faviary/jobs/218");
	const meta = page.getByTestId("job-meta");
	await expect(meta.getByTestId("job-session")).toHaveText("7f3c…a9");
	await expect(meta.getByTestId("job-agent")).toContainText("claude");
	await expect(meta.getByTestId("job-clone-path")).toBeVisible();
	const clone = await meta.getByTestId("job-clone").boundingBox();
	const steps = await page.getByTestId("lifecycle").boundingBox();
	expect(clone && steps && clone.y + clone.height <= steps.y).toBe(true);
});

for (const { name, width, height, compact } of [
	{ name: "mobile", width: 390, height: 844, compact: true },
	{ name: "narrow desktop", width: 820, height: 800, compact: true },
	{ name: "wide desktop", width: 1600, height: 900, compact: false },
]) {
	test(`the job detail stepper never scrolls sideways at ${name} width (#774)`, async ({
		page,
	}) => {
		await page.setViewportSize({ width, height });
		await setup(page);
		await page.goto("/agents/coder/projects/lsegal%2Faviary/jobs/218");
		const steps = page.getByTestId("lifecycle");
		await expect(steps.locator('[aria-current="step"]')).toContainText(
			"Implementing",
		);
		const overflow = await steps.evaluate(
			(el) => el.scrollWidth - el.clientWidth,
		);
		expect(overflow).toBeLessThanOrEqual(0);
		const summary = page.getByTestId("lifecycle-compact");
		if (compact) {
			await expect(summary).toBeVisible();
			await expect(summary).toHaveText("3/6 · Implementing");
		} else {
			await expect(summary).toBeHidden();
		}
		await expect(steps.locator("li").nth(2)).toHaveAttribute(
			"title",
			"Step 3 of 6: Implementing (current)",
		);
		await expect(steps.locator("li").nth(0)).toHaveAttribute(
			"data-step",
			"done",
		);
	});
}

for (const { name, width, height } of [
	{ name: "mobile", width: 390, height: 844 },
	{ name: "wide desktop", width: 1600, height: 900 },
]) {
	test(`the job detail page never scrolls past its content at ${name} width (#854)`, async ({
		page,
	}) => {
		// Each tool call carries a visually hidden, absolutely positioned
		// status label, which must stay inside the transcript's scroller.
		EVENTS[221] = Array.from({ length: 80 }, (_, i) => [
			{
				kind: "tool_call",
				time: at(3),
				call_id: `t${i}`,
				name: "Read",
				input: { file_path: `file${i}.go` },
			},
			{ kind: "tool_result", time: at(3), call_id: `t${i}`, output: "ok" },
		]).flat();
		try {
			await page.setViewportSize({ width, height });
			await setup(page);
			await page.goto("/agents/coder/projects/lsegal%2Faviary/jobs/221");
			const calls = page
				.getByRole("list", { name: "Activity" })
				.locator('[data-event="tool_call"]');
			await expect(calls).toHaveCount(80);
			const overscroll = await page.evaluate(() => {
				const doc = document.scrollingElement ?? document.documentElement;
				return doc.scrollHeight - doc.clientHeight;
			});
			expect(overscroll).toBeLessThanOrEqual(0);

			// The transcript itself still scrolls to its last entry and footer.
			await calls.last().scrollIntoViewIfNeeded();
			await expect(calls.last()).toBeInViewport();
			const footer = page.getByText("Jobs only take instructions");
			await footer.scrollIntoViewIfNeeded();
			await expect(footer).toBeInViewport();
		} finally {
			delete EVENTS[221];
		}
	});
}

const SIGNED_OUT = {
	signed_out: true,
	signing_in: false,
	url: "https://github.com/lsegal/aviary/issues?q=is%3Aissue+state%3Aopen+label%3Aready",
	status: 404,
	since: minutesAgo(2),
};
const browserStatus = (github: object) => ({
	...STATUS,
	issue_filter: "",
	delivery: "browser",
	poll_interval: "20s",
	github,
});

test.describe("GitHub sign-in banner", () => {
	test("a signed-out read shows the banner, never a window", async ({
		page,
	}) => {
		const calls = await setup(page, () => browserStatus(SIGNED_OUT));
		await page.goto("/agents/coder/projects/lsegal%2Faviary");
		const banner = page.getByTestId("github-signin-banner");
		await expect(banner).toContainText(
			"Signed out of GitHub in Aviary's browser",
		);
		await expect(banner).toContainText("GitHub answered 404");
		await expect(banner).toContainText('This is not "no issues"');
		await expect(page.getByTestId("delivery-status")).toHaveText("offline");
		await expect(page.getByText("browser · checked")).toContainText(
			"every 20s",
		);
		// Without a filter every open issue is picked up, with no label.
		await expect(page.getByText("browser · checked")).toContainText(
			"All open issues",
		);
		await expect(page.getByTestId("issue-filter")).toHaveCount(0);
		// The page only reads status until the user acts.
		expect(calls.project_github_signin).toBeUndefined();

		await banner.getByRole("button", { name: "Sign in to GitHub" }).click();
		await expect.poll(() => calls.project_github_signin?.length).toBe(1);
		await banner.getByRole("button", { name: "Switch to gh polling" }).click();
		await expect.poll(() => calls.project_github_use_polling?.length).toBe(1);
		await expect(banner.getByRole("textbox")).toHaveCount(0);
	});

	test("an open sign-in window replaces the buttons", async ({ page }) => {
		await setup(page, () => browserStatus({ ...SIGNED_OUT, signing_in: true }));
		await page.goto("/agents/coder/projects/lsegal%2Faviary");
		const banner = page.getByTestId("github-signin-banner");
		await expect(banner).toContainText("Waiting for you to sign in to GitHub");
		await expect(banner.getByRole("button")).toHaveCount(0);
	});

	test("without a display it offers gh polling or a profile import", async ({
		page,
	}) => {
		const calls = await setup(page, () =>
			browserStatus({
				...SIGNED_OUT,
				window_unavailable:
					"no display server (DISPLAY and WAYLAND_DISPLAY are both unset)",
				last_error: "",
			}),
		);
		await page.goto("/agents/coder/projects/lsegal%2Faviary");
		const banner = page.getByTestId("github-signin-banner");
		await expect(banner.getByTestId("github-signin-no-window")).toContainText(
			"no display server",
		);
		await expect(
			banner.getByRole("button", { name: "Sign in to GitHub" }),
		).toHaveCount(0);
		const importButton = banner.getByRole("button", { name: "Import profile" });
		await expect(importButton).toBeDisabled();
		await banner
			.getByLabel("Aviary browser profile to import")
			.fill(" /mnt/desktop/aviary/browser ");
		await importButton.click();
		await expect
			.poll(() => calls.project_github_import?.[0])
			.toEqual({ profile: "/mnt/desktop/aviary/browser" });
		await expect(
			banner.getByRole("button", { name: "Switch to gh polling" }),
		).toBeVisible();
	});

	test("a signed-in session and gh polling show no banner", async ({
		page,
	}) => {
		let status: object = browserStatus({
			signed_out: false,
			signing_in: false,
		});
		await setup(page, () => status);
		await page.goto("/agents/coder/projects/lsegal%2Faviary");
		await expect(page.getByTestId("job-card").first()).toBeVisible();
		await expect(page.getByTestId("github-signin-banner")).toHaveCount(0);
		await expect(page.getByTestId("delivery-status")).toHaveText("live");
		// Signing out mid-run raises the banner on the next poll, and signing in
		// again clears it.
		status = browserStatus(SIGNED_OUT);
		await expect(page.getByTestId("github-signin-banner")).toBeVisible();
		status = browserStatus({ signed_out: false, signing_in: false });
		await expect(page.getByTestId("github-signin-banner")).toHaveCount(0);
		status = STATUS;
		await expect(page.getByText("poll · checked")).toBeVisible();
		await expect(page.getByTestId("github-signin-banner")).toHaveCount(0);
	});
});

const PNG =
	"data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==";

/** Fixtures for the built-in Code Loop agent behind the prompt chat. */
function chatFixtures(calls: Calls): ToolFixtures {
	const record = (tool: string, args?: Record<string, unknown>) => {
		calls[tool] ??= [];
		calls[tool].push(args ?? {});
	};
	return {
		agent_list: [
			{
				id: "projects",
				name: "projects",
				model: "anthropic/claude-sonnet-5-5",
				state: "idle",
			},
		],
		session_list: (args?: Record<string, unknown>) => {
			record("session_list", args);
			return [
				{ id: "main", agent_id: "projects", name: "main", created_at: at(60) },
			];
		},
		session_messages: (args?: Record<string, unknown>) => {
			record("session_messages", args);
			return chatLog(calls);
		},
		agent_run: (args?: Record<string, unknown>) => {
			record("agent_run", args);
			return { stream: ["#218 is clamping the retry delay."], result: "" };
		},
	};
}

/** The prompt chat's persisted log: two seeded messages, then every prompt. */
function chatLog(calls: Calls) {
	return [
		{
			id: "m1",
			session_id: "projects-chat",
			role: "user",
			content: "What does the board look like?",
			media_urls: [PNG],
			timestamp: at(5),
		},
		{
			id: "m2",
			session_id: "projects-chat",
			role: "assistant",
			content: "Here is the failing run.",
			media_urls: [PNG],
			timestamp: at(4),
		},
		// What the server persists once a prompt has run.
		...(calls.agent_run ?? []).flatMap((run, i) => [
			{
				id: `r${i}`,
				session_id: "projects-chat",
				role: "user",
				content: run.message,
				timestamp: at(0),
			},
			{
				id: `a${i}`,
				session_id: "projects-chat",
				role: "assistant",
				content: "#218 is clamping the retry delay.",
				timestamp: at(0),
			},
		]),
	];
}

test("prompt chat opens beside the board and remembers it is open", async ({
	page,
}) => {
	const calls: Calls = {};
	Object.assign(calls, await setup(page, undefined, chatFixtures(calls)));
	await page.goto("/agents/coder/projects/lsegal%2Faviary");

	await expect(page.getByTestId("project-chat")).toHaveCount(0);
	await page.getByTestId("prompt-chat-toggle").click();
	const chat = page.getByTestId("project-chat");
	await expect(chat).toBeVisible();
	await expect(chat).toContainText(
		"Ask the projects agent about lsegal/aviary",
	);
	// It talks to the built-in Code Loop agent's one hidden session, never to
	// a job, and offers no other chats (#624).
	await expect
		.poll(() => calls.session_messages?.at(-1))
		.toMatchObject({ agent: "projects", session_id: "projects-chat" });
	await expect(chat.getByTestId("chat-picker")).toHaveCount(0);
	await expect(chat.getByRole("button", { name: "New chat" })).toHaveCount(0);
	// The sidebar lists the sole agent's own chats; none are the Projects
	// agent's.
	expect(
		(calls.session_list ?? []).filter((c) => c.agent === "projects"),
	).toHaveLength(0);
	// Images show in both directions.
	await expect(chat.locator("img")).toHaveCount(2);
	await expect(chat).toContainText("Here is the failing run.");
	// The board's cards stay visible beside the panel.
	await expect(page.getByTestId("job-card").first()).toBeVisible();

	// Dragging the handle widens the panel, and both survive a reload.
	const before = (await chat.boundingBox())?.width ?? 0;
	const handle = page.getByTestId("project-chat-resize");
	const box = await handle.boundingBox();
	if (!box) throw new Error("no resize handle");
	await page.mouse.move(box.x + box.width / 2, box.y + 100);
	await page.mouse.down();
	await page.mouse.move(box.x - 80, box.y + 100);
	await page.mouse.up();
	const after = (await chat.boundingBox())?.width ?? 0;
	expect(after).toBeGreaterThan(before + 60);

	await page.reload();
	await expect(page.getByTestId("project-chat")).toBeVisible();
	expect(
		(await page.getByTestId("project-chat").boundingBox())?.width,
	).toBeCloseTo(after, 0);

	await page.getByRole("button", { name: "Close prompt chat" }).click();
	await expect(page.getByTestId("project-chat")).toHaveCount(0);
	await page.reload();
	await expect(
		page.getByRole("heading", { name: "lsegal/aviary" }),
	).toBeVisible();
	await expect(page.getByTestId("project-chat")).toHaveCount(0);
});

test("prompt chat previews a pending image when it is clicked", async ({
	page,
}) => {
	const calls: Calls = {};
	Object.assign(calls, await setup(page, undefined, chatFixtures(calls)));
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	await page.getByTestId("prompt-chat-toggle").click();
	const chat = page.getByTestId("project-chat");
	const png = Buffer.from(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGP4z8AAAAMBAQDJ/pLvAAAAAElFTkSuQmCC",
		"base64",
	);
	const chooser = page.waitForEvent("filechooser");
	await chat.getByRole("button", { name: "Attach files" }).click();
	await (await chooser).setFiles({
		name: "board.png",
		mimeType: "image/png",
		buffer: png,
	});

	await chat.getByRole("button", { name: "Preview board.png" }).click();
	const preview = page.getByTestId("image-preview");
	await expect(preview.getByTestId("image-preview-img")).toHaveAttribute(
		"src",
		`data:image/png;base64,${png.toString("base64")}`,
	);
	// Esc closes only the preview, leaving the chat and its attachment.
	await page.keyboard.press("Escape");
	await expect(preview).toHaveCount(0);
	await expect(chat).toBeVisible();
	await expect(chat.getByTestId("attachment-image")).toHaveCount(1);
});

test("logs panel docks below the board, streams lines, and resizes", async ({
	page,
}) => {
	// Each status poll after the first adds a log line.
	let polls = 0;
	await setup(page, () => {
		polls++;
		const extra = Array.from(
			{ length: Math.max(0, polls - 1) },
			(_, i) => `12:05:${String(i).padStart(2, "0")} live line ${i + 1}`,
		);
		return {
			...STATUS,
			recent_logs: [STATUS.recent_logs, ...extra].join("\n"),
		};
	});
	await page.goto("/agents/coder/projects/lsegal%2Faviary");

	// The log lines no longer sit under the job cards.
	await expect(page.getByTestId("job-card").first()).toBeVisible();
	await expect(page.getByText("Project log")).toHaveCount(0);
	await expect(page.getByTestId("project-logs")).toHaveCount(0);

	// The Logs button sits just left of the Prompt chat button.
	const toggle = page.getByTestId("logs-toggle");
	await expect(toggle).toHaveAttribute("title", "Logs");
	await expect(toggle).toHaveAttribute("aria-pressed", "false");
	const toggleBox = await toggle.boundingBox();
	const chatBox = await page.getByTestId("prompt-chat-toggle").boundingBox();
	if (!toggleBox || !chatBox) throw new Error("no header buttons");
	expect(toggleBox.x).toBeLessThan(chatBox.x);

	await toggle.click();
	const panel = page.getByTestId("project-logs");
	await expect(panel).toBeVisible();
	await expect(toggle).toHaveAttribute("aria-pressed", "true");
	await expect(panel).toContainText("claimed #221");
	// It docks at the bottom of the board.
	const viewport = page.viewportSize();
	const panelBox = await panel.boundingBox();
	if (!viewport || !panelBox) throw new Error("no panel");
	expect(panelBox.y + panelBox.height).toBeCloseTo(viewport.height, 0);

	// New lines stream in, and the view stays pinned to the newest one.
	await expect(panel).toContainText("live line 2", { timeout: 10_000 });
	const lines = page.getByTestId("project-logs-lines");
	await expect
		.poll(() =>
			lines.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight),
		)
		.toBeLessThan(2);

	// Dragging the top edge up makes the panel taller, and the height and
	// open state survive a reload.
	const before = panelBox.height;
	const handle = page.getByTestId("project-logs-resize");
	const box = await handle.boundingBox();
	if (!box) throw new Error("no resize handle");
	await page.mouse.move(box.x + 200, box.y + box.height / 2);
	await page.mouse.down();
	await page.mouse.move(box.x + 200, box.y - 100);
	await page.mouse.up();
	const after = (await panel.boundingBox())?.height ?? 0;
	expect(after).toBeGreaterThan(before + 80);

	await page.reload();
	await expect(page.getByTestId("project-logs")).toBeVisible();
	expect(
		(await page.getByTestId("project-logs").boundingBox())?.height,
	).toBeCloseTo(after, 0);

	// Clicking Logs again closes it, and it stays closed after a reload.
	await page.getByTestId("logs-toggle").click();
	await expect(page.getByTestId("project-logs")).toHaveCount(0);
	await expect(page.getByTestId("logs-toggle")).toHaveAttribute(
		"aria-pressed",
		"false",
	);
	await page.reload();
	await expect(
		page.getByRole("heading", { name: "lsegal/aviary" }),
	).toBeVisible();
	await expect(page.getByTestId("project-logs")).toHaveCount(0);
});

test("logs panel pauses following with the pill when scrolled up (#1072)", async ({
	page,
}) => {
	let polls = 0;
	const old = Array.from(
		{ length: 40 },
		(_, i) => `12:00:${String(i).padStart(2, "0")} old line ${i + 1}`,
	);
	await setup(page, () => {
		polls++;
		const extra = Array.from(
			{ length: polls },
			(_, i) => `12:05:${String(i).padStart(2, "0")} live line ${i + 1}`,
		);
		return { ...STATUS, recent_logs: [...old, ...extra].join("\n") };
	});
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	await page.getByTestId("logs-toggle").click();
	const lines = page.getByTestId("project-logs-lines");
	const pill = page.getByTestId("project-logs-more-below");
	const gap = () =>
		lines.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight);
	await expect(lines).toContainText("old line 40");
	await expect.poll(gap).toBeLessThanOrEqual(8);

	await lines.evaluate((el) => {
		el.scrollTop = 0;
	});
	await expect(pill).toBeVisible();
	const seen = polls;
	await expect(lines).toContainText(`live line ${seen + 1}`, {
		timeout: 10_000,
	});
	expect(await lines.evaluate((el) => el.scrollTop)).toBe(0);
	await expect(pill).toContainText("new · More below ↓");

	await pill.click();
	await expect.poll(gap).toBeLessThanOrEqual(8);
	await expect(pill).toBeHidden();
});

test("prompt chat inserts a newline on Shift+Enter and sends on Enter", async ({
	page,
}) => {
	const calls: Calls = {};
	Object.assign(calls, await setup(page, undefined, chatFixtures(calls)));
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	await page.getByTestId("prompt-chat-toggle").click();
	const chat = page.getByTestId("project-chat");
	const input = chat.getByPlaceholder("Describe a task or ask a question");
	await expect(input).toBeEnabled();

	await input.fill("line one");
	const before = (await input.boundingBox())?.height ?? 0;
	await input.press("Shift+Enter");
	await input.pressSequentially("line two");
	await expect(input).toHaveValue("line one\nline two");
	await expect
		.poll(async () => (await input.boundingBox())?.height ?? 0)
		.toBeGreaterThan(before);
	expect(calls.agent_run ?? []).toHaveLength(0);

	await input.press("Enter");
	await expect.poll(() => calls.agent_run?.length ?? 0).toBe(1);
	expect(calls.agent_run[0].message).toBe("line one\nline two");
	await expect(input).toHaveValue("");
	const sent = chat
		.getByTestId("chat-message-content")
		.filter({ hasText: "line two" });
	await expect(sent).toBeVisible();
	expect(await sent.innerText()).toBe("line one\nline two");
});

test("job details open under the project header, keeping the chat and logs panels", async ({
	page,
}) => {
	const calls: Calls = {};
	Object.assign(calls, await setup(page, undefined, chatFixtures(calls)));
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	await page.getByTestId("prompt-chat-toggle").click();
	await page.getByTestId("logs-toggle").click();
	const chat = page.getByTestId("project-chat");
	const input = chat.getByPlaceholder("Describe a task or ask a question");
	await expect(chat).toContainText("Here is the failing run.");
	await input.fill("what about #218?");
	await expect(page.getByTestId("project-logs")).toBeVisible();

	await page
		.getByTestId("job-card")
		.filter({ hasText: "#218" })
		.getByRole("link", { name: "Details" })
		.click();
	await expect(page).toHaveURL(
		"/agents/coder/projects/lsegal%2Faviary/jobs/218",
	);
	await expect(page.getByTestId("job-detail-title")).toContainText(
		/#218 • (Draft pull request)?PR #233 Cap scheduler retry backoff at 10 minutes/,
	);
	// The shared header names the job under its project, and the project's
	// buttons stay.
	await expect(
		page
			.getByTestId("settings-breadcrumb")
			.getByRole("link", { name: "lsegal/aviary", exact: true }),
	).toBeVisible();
	await expect(page.getByTestId("settings-title")).toHaveText("#218");
	await expect(page.getByTestId("delivery-status")).toHaveCount(0);
	await expect(page.getByRole("button", { name: "Check now" })).toBeVisible();
	await expect(page.getByTestId("project-settings-link")).toBeVisible();
	// The open panels keep their content and the unsent draft.
	await expect(chat).toContainText("Here is the failing run.");
	await expect(input).toHaveValue("what about #218?");
	await expect(page.getByTestId("project-logs")).toBeVisible();

	// Switching jobs and closing details keeps them too.
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	await page
		.getByTestId("job-card")
		.filter({ hasText: "#218" })
		.getByRole("link", { name: "Details" })
		.click();
	await page.getByTestId("job-detail-close").click();
	await expect(page).toHaveURL("/agents/coder/projects/lsegal%2Faviary");
	await expect(page.getByTestId("job-card").first()).toBeVisible();
	await expect(chat).toBeVisible();
	await expect(page.getByTestId("project-logs")).toBeVisible();
	expect(calls.agent_run ?? []).toHaveLength(0);
});

test("closing job details returns to the board with its filters and search", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	const search = page.getByRole("searchbox", { name: "Search jobs" });
	await search.fill("scheduler");
	await stateChip(page, "Running").click();
	const card = page.getByTestId("job-card").filter({ hasText: "#218" });
	await expect(card).toBeVisible();

	await card.getByRole("link", { name: "Details" }).click();
	await expect(page.getByTestId("job-detail")).toBeVisible();
	await expect(search).toHaveCount(0);
	await page.getByTestId("job-detail-close").click();
	await expect(search).toHaveValue("scheduler");
	await expect(stateChip(page, "Running")).toHaveAttribute(
		"aria-pressed",
		"true",
	);

	// Browser back and forward move between the board and the details
	// without reloading the page.
	await page.evaluate(() => {
		(window as unknown as { marker: boolean }).marker = true;
	});
	await page.goBack();
	await expect(page.getByTestId("job-detail")).toBeVisible();
	await page.goForward();
	await expect(page.getByTestId("job-detail")).toHaveCount(0);
	await expect(search).toHaveValue("scheduler");
	expect(
		await page.evaluate(
			() => (window as unknown as { marker?: boolean }).marker,
		),
	).toBe(true);
});

test("loading a job details URL shows the project header with that job open", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/agents/coder/projects/lsegal%2Faviary/jobs/218");
	await expect(
		page
			.getByTestId("settings-breadcrumb")
			.getByRole("link", { name: "lsegal/aviary", exact: true }),
	).toBeVisible();
	await expect(page.getByTestId("prompt-chat-toggle")).toBeVisible();
	await expect(page.getByTestId("logs-toggle")).toBeVisible();
	await expect(page.getByTestId("job-detail-title")).toHaveText(
		/^#218 • (Draft pull request)?PR #233 Cap scheduler retry backoff at 10 minutes$/,
	);
	await expect(page.getByTestId("job-card")).toHaveCount(0);
});

test("logs panel shows project_status recent_logs and updates live", async ({
	page,
}) => {
	// project_status returns recent_logs as an array of dated lines. None have
	// been logged yet; the test logs them one at a time.
	const logged: string[] = [];
	await setup(page, () => ({
		...STATUS,
		recent_logs: [...logged],
		log_offset: 650 + logged.length,
	}));
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	await page.getByTestId("logs-toggle").click();
	const panel = page.getByTestId("project-logs");
	await expect(panel).toContainText("No log lines yet.");

	// Lines appear without a reload, and the placeholder goes away.
	logged.push("2026-11-21 11:02:00 issue #737 picked up");
	await expect(panel).toContainText("issue #737 picked up", {
		timeout: 10_000,
	});
	await expect(panel).not.toContainText("No log lines yet.");
	logged.push("2026-11-21 11:02:01 issue #738 picked up");
	await expect(panel).toContainText("issue #738 picked up", {
		timeout: 10_000,
	});
	await expect(page.getByTestId("project-logs-entry")).toHaveCount(2);
	await expect(
		page.getByTestId("project-logs-entry").first().locator("span").first(),
	).toHaveText("2026-11-21 11:02:00");
});

test("logs panel wraps long lines and indents multi-line entries", async ({
	page,
}) => {
	const url = `https://github.com/lsegal/aviary/issues?q=${"x".repeat(300)}`;
	await setup(page, () => ({
		...STATUS,
		recent_logs: [
			`2026-11-20 12:03:58 handoff poll #2 error: read issue list at ${url} the issue list did not render within 15s`,
			"2026-11-20 12:04:02 prompt agent (claude/opus) filing an issue on lsegal/aviary",
			"## Summary",
			"Body line two",
			"12:04:11 claimed #221",
		].join("\n"),
	}));
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	await page.getByTestId("logs-toggle").click();
	const lines = page.getByTestId("project-logs-lines");
	await expect(lines).toContainText("claimed #221");

	// Nothing overflows sideways, not even the unbroken URL.
	expect(
		await lines.evaluate((el) => el.scrollWidth - el.clientWidth),
	).toBeLessThanOrEqual(0);
	const entries = page.getByTestId("project-logs-entry");
	await expect(entries).toHaveCount(3);
	const first = entries.nth(0);
	expect(await first.evaluate((el) => el.scrollWidth - el.clientWidth)).toBe(0);
	await expect(first).toContainText(url);

	// Continuation lines stay in their entry, keep their line breaks, and line
	// up after the timestamp rather than under it.
	const second = entries.nth(1);
	const text = second.locator("span").nth(1);
	expect(await text.innerText()).toBe(
		"prompt agent (claude/opus) filing an issue on lsegal/aviary\n## Summary\nBody line two",
	);
	const timeBox = await second.locator("span").first().boundingBox();
	const textBox = await text.boundingBox();
	if (!timeBox || !textBox) throw new Error("no entry boxes");
	expect(textBox.x).toBeGreaterThan(timeBox.x + timeBox.width);
	const oneLine = (await entries.nth(2).boundingBox())?.height ?? 0;
	expect(textBox.height).toBeGreaterThan(oneLine * 2.5);
	await expect(second.locator("span").first()).toHaveText(
		"2026-11-20 12:04:02",
	);
	await expect(entries.nth(2).locator("span").first()).toHaveText("12:04:11");
});

test("Summarize sends the job to the prompt chat", async ({ page }) => {
	const calls: Calls = {};
	Object.assign(calls, await setup(page, undefined, chatFixtures(calls)));
	await page.goto("/agents/coder/projects/lsegal%2Faviary");

	const card = page.getByTestId("job-card").filter({ hasText: "#218" });
	await card.getByTestId("job-summarize").click();
	const chat = page.getByTestId("project-chat");
	await expect(chat).toBeVisible();
	await expect.poll(() => calls.agent_run?.length ?? 0).toBe(1);
	expect(calls.agent_run[0]).toMatchObject({
		name: "projects",
		message: "Summarize the current session log of work on lsegal/aviary#218",
		// The board's project goes with every prompt (#516).
		project: "lsegal/aviary",
		session: "projects-chat",
	});
	expect(calls.agent_run[0].session_id).toBeUndefined();
	await expect(chat).toContainText(
		"Summarize the current session log of work on lsegal/aviary#218",
	);
	await expect(chat).toContainText("#218 is clamping the retry delay.");
});

test("every board shares one prompt chat log with its own project hint", async ({
	page,
}) => {
	const calls: Calls = {};
	Object.assign(calls, await setup(page, undefined, chatFixtures(calls)));
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	await page.getByTestId("prompt-chat-toggle").click();
	let chat = page.getByTestId("project-chat");
	await expect(chat).toContainText("about lsegal/aviary");
	const input = chat.getByPlaceholder("Describe a task or ask a question");
	await input.fill("What is stuck here?");
	await input.press("Enter");
	await expect.poll(() => calls.agent_run?.length ?? 0).toBe(1);
	await expect(chat).toContainText("What is stuck here?");

	// The all-projects board opens the same log, now about every project.
	await page.goto("/agents/coder/projects");
	chat = page.getByTestId("project-chat");
	await expect(chat).toContainText("about every project");
	await expect(chat).toContainText("Here is the failing run.");
	await expect(chat).toContainText("What is stuck here?");
	await chat
		.getByPlaceholder("Describe a task or ask a question")
		.fill("And everywhere?");
	await chat
		.getByPlaceholder("Describe a task or ask a question")
		.press("Enter");
	await expect.poll(() => calls.agent_run?.length ?? 0).toBe(2);

	// Only the project hint changed; both prompts went to the one session.
	expect(calls.agent_run[0]).toMatchObject({
		session: "projects-chat",
		project: "lsegal/aviary",
	});
	expect(calls.agent_run[1].session).toBe("projects-chat");
	expect(calls.agent_run[1].project).toBeUndefined();
	expect(
		new Set((calls.session_messages ?? []).map((a) => a.session_id)),
	).toEqual(new Set(["projects-chat"]));
});

test.describe("phone width", () => {
	test.use({ viewport: { width: 390, height: 844 } });

	for (const path of [
		"/agents/coder/projects/lsegal%2Faviary",
		"/agents/coder/projects/lsegal%2Faviary/jobs/218",
	]) {
		test(`${path} has no horizontal overflow`, async ({ page }) => {
			await setup(page);
			await page.goto(path);
			await expect(page.getByTestId("settings-title")).toBeVisible();
			await page.waitForTimeout(500);
			const overflow = await page.evaluate(() =>
				[...document.querySelectorAll("main *")].some(
					(el) =>
						el.getBoundingClientRect().right > window.innerWidth + 1 &&
						getComputedStyle(el).position !== "fixed" &&
						!el.closest("[class*='overflow-x-auto']") &&
						!el.closest("pre"),
				),
			);
			expect(overflow).toBe(false);
		});
	}

	test("prompt chat opens as a bottom drawer", async ({ page }) => {
		await setup(page, undefined, chatFixtures({}));
		await page.goto("/agents/coder/projects/lsegal%2Faviary");
		await page.getByTestId("prompt-chat-toggle").click();
		const chat = page.getByTestId("project-chat");
		await expect(chat).toBeVisible();
		const box = await chat.boundingBox();
		if (!box) throw new Error("no prompt chat");
		expect(box.width).toBeCloseTo(390, 0);
		expect(box.y + box.height).toBeCloseTo(844, 0);
		expect(box.y).toBeGreaterThan(100);
	});
});

test("search filters the whole job list across every field", async ({
	page,
}) => {
	// 30 jobs span three pages. The search must find matches on any page, in
	// the title, issue and PR numbers, session id, and output.
	const many = Array.from({ length: 30 }, (_, i) => {
		const number = 300 + i * 2;
		return job({
			number: number === 350 ? 351 : number,
			title:
				number === 350 ? "Search target on the last page" : `Job ${number}`,
			status: number === 306 ? "failed" : "complete",
			agent: "claude",
			model: "opus",
			started: 10 + i,
			...(number === 310 ? { pr: 351 } : {}),
		});
	});
	const sessions: Record<number, string> = { 320: "sess-351-abc" };
	const logs: Record<number, string> = {
		330: "panic: kaboom",
		306: "retry 351 failed",
	};
	const matches = (j: (typeof many)[number], q: string) =>
		[
			j.key,
			`#${j.number}`,
			j.title,
			j.status,
			j.pull_request ? `#${j.pull_request}` : "",
			sessions[j.number] ?? "",
			logs[j.number] ?? "",
		].some((f) => f.toLowerCase().includes(q.toLowerCase()));
	await setup(page, undefined, {
		project_job_list: (args?: Record<string, unknown>) => {
			const query = typeof args?.query === "string" ? args.query : "";
			const scoped = many.filter(
				(j) =>
					inTargets(args, j.target) &&
					(!args?.status || j.status === args.status) &&
					(!args?.states || (args.states as string[]).includes(j.state)),
			);
			const found = query ? scoped.filter((j) => matches(j, query)) : scoped;
			const offset = Number(args?.offset ?? 0);
			const limit = Number(args?.limit ?? 50);
			return {
				jobs: found.slice(offset, offset + limit),
				total: found.length,
				offset,
				limit,
				...(query ? { unfiltered_total: scoped.length } : {}),
			};
		},
		project_job_get: (args?: Record<string, unknown>) => {
			const found = many.find((j) => j.number === args?.number);
			if (!found) throw new Error("no such job");
			return {
				...found,
				session_id: sessions[found.number] ?? "",
				scrollback: logs[found.number] ?? "",
			};
		},
	});
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	const cards = page.getByTestId("job-card");
	const pages = page.getByRole("navigation", { name: "Job pages" });
	const search = page.getByRole("searchbox", { name: "Search jobs" });
	// The search reads as a search box: an icon and the standard field styling.
	await expect(
		page.getByTestId("job-toolbar").getByTestId("search-icon"),
	).toBeVisible();
	await expect(search).toHaveClass(/\bfield-input\b/);
	await expect(pages).toContainText("Page 1 of 3");
	await expect(cards).toHaveCount(12);
	await expect(page.getByTestId("search-count")).toHaveCount(0);

	// Paging uses chevron icon buttons that still name themselves and disable
	// at either end.
	const prev = pages.getByRole("button", { name: "Previous page" });
	const next = pages.getByRole("button", { name: "Next page" });
	for (const [button, title] of [
		[prev, "Previous page"],
		[next, "Next page"],
	] as const) {
		await expect(button).toHaveAttribute("title", title);
		await expect(button).toHaveText("");
		await expect(button.locator("svg")).toHaveCount(1);
	}
	await expect(prev).toBeDisabled();
	await expect(next).toBeEnabled();
	await next.click();
	await next.click();
	await expect(pages).toContainText("Page 3 of 3");
	await expect(next).toBeDisabled();
	await expect(prev).toBeEnabled();
	await prev.click();

	// Typing filters at once and lands on the first page of the matches.
	await expect(pages).toContainText("Page 2 of 3");
	await search.pressSequentially("351");
	await expect(pages).toContainText("Page 1 of 1");
	await expect(page.getByTestId("search-count")).toHaveText(
		"4 of 30 jobs match",
	);
	await expect(cards).toHaveCount(4);
	for (const n of ["Search target", "Job 310", "Job 320", "Job 306"]) {
		await expect(cards.filter({ hasText: n })).toHaveCount(1);
	}

	// The search narrows within the active status filter.
	await stateChip(page, "Failed").click();
	await expect(cards).toHaveCount(1);
	await expect(cards).toContainText("#306");
	await expect(page.getByTestId("search-count")).toHaveText(
		"1 of 1 job matches",
	);
	await stateChip(page, "Failed").click();

	await search.fill("kaboom");
	await expect(cards).toHaveCount(1);
	await expect(cards).toContainText("#330");
	await search.fill("search target");
	await expect(cards).toHaveCount(1);
	await expect(cards).toContainText("#351");

	await search.fill("zzzzz");
	await expect(cards).toHaveCount(0);
	await expect(page.getByTestId("search-empty")).toContainText(
		"No jobs match “zzzzz”",
	);
	await expect(page.getByTestId("search-count")).toHaveText(
		"0 of 30 jobs match",
	);
	await expect(pages).toContainText("Page 1 of 1");

	await page.getByRole("button", { name: "Clear search" }).click();
	await expect(search).toHaveValue("");
	await expect(pages).toContainText("Page 1 of 3");
	await expect(cards).toHaveCount(12);
});

test("the Scheduled and five state filters and kanban toggle sit beside the search", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	const toolbar = page.getByTestId("job-toolbar");
	await expect(
		toolbar.getByRole("searchbox", { name: "Search jobs" }),
	).toBeVisible();
	await expect(
		toolbar.getByRole("group", { name: "Job filter" }).getByRole("button"),
	).toHaveText([
		/^Scheduled/,
		/^Starting/,
		/^Paused/,
		/^Running/,
		/^Done/,
		/^Failed/,
	]);
	await expect(toolbar.getByTestId("kanban-toggle")).toBeVisible();
	// The board has no separate Schedules view to switch to (#767).
	await expect(
		page.getByRole("tablist", { name: "Project views" }),
	).toHaveCount(0);
	await expect(page.getByRole("group", { name: "Job filter" })).toHaveCount(1);

	// Each state filter shows exactly that state's jobs.
	for (const [tab, numbers] of [
		["Starting", ["#221"]],
		["Running", ["#218"]],
		["Paused", ["#214"]],
		["Done", ["#200"]],
		["Failed", ["#209"]],
	] as const) {
		await stateChip(page, tab).click();
		const cards = page.getByTestId("job-card");
		await expect(cards).toHaveCount(numbers.length);
		for (const n of numbers) await expect(cards).toContainText(n);
		await stateChip(page, tab).click();
	}
});

test("the state chips multi-select and each board remembers its own", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	const cards = page.getByTestId("job-card");
	await expect(cards).toHaveCount(5);
	const total = page.getByTestId("job-total");
	await expect(total).toHaveText("5 jobs");
	const pressed = async (on: string[]) => {
		for (const label of ["Starting", "Paused", "Running", "Done", "Failed"])
			await expect(stateChip(page, label)).toHaveAttribute(
				"aria-pressed",
				on.includes(label) ? "true" : "false",
			);
	};
	await pressed([]);
	await expect(page.getByRole("button", { name: "Clear" })).toHaveCount(0);

	// Running and Failed together show only their jobs, in list and kanban.
	await stateChip(page, "Running").click();
	await stateChip(page, "Failed").click();
	await pressed(["Running", "Failed"]);
	await expect(stateChip(page, "Running")).toHaveClass(/bg-blue-600/);
	await expect(stateChip(page, "Done")).not.toHaveClass(/bg-green-600/);
	await expect(cards).toHaveCount(2);
	await expect(cards.filter({ hasText: "#218" })).toHaveCount(1);
	await expect(cards.filter({ hasText: "#209" })).toHaveCount(1);
	await expect(total).toHaveText("2 of 5 jobs");
	await expect(page).toHaveURL(/[?&]state=running(,|%2C)failed$/);
	await page.getByTestId("kanban-toggle").click();
	const columns = page.getByTestId("kanban-column");
	await expect(columns).toHaveCount(2);
	await expect(columns.nth(0)).toHaveAttribute("data-state", "running");
	await expect(columns.nth(1)).toHaveAttribute("data-state", "failed");
	await expect(page.getByTestId("kanban-card")).toHaveCount(2);
	await page.getByTestId("kanban-toggle").click();

	// Opening a job's details and going back to the board keeps them.
	await cards
		.filter({ hasText: "#218" })
		.getByRole("link", { name: "Details" })
		.click();
	await expect(page.getByTestId("job-detail")).toBeVisible();
	await page.getByTestId("job-detail-close").click();
	await expect(page).toHaveURL(/[?&]state=running(,|%2C)failed$/);
	await pressed(["Running", "Failed"]);
	await expect(cards).toHaveCount(2);

	// Another project keeps its own selection, and coming back restores this one.
	await page.goto("/agents/coder/projects/lsegal%2Fzvidlib");
	await pressed([]);
	await stateChip(page, "Done").click();
	await pressed(["Done"]);
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	await pressed(["Running", "Failed"]);
	await expect(cards).toHaveCount(2);

	// A reload keeps them.
	await page.reload();
	await pressed(["Running", "Failed"]);
	await expect(cards).toHaveCount(2);

	// Deselecting every chip shows every job again, and stays that way.
	await page.getByRole("button", { name: "Clear" }).click();
	await pressed([]);
	await expect(cards).toHaveCount(5);
	await expect(total).toHaveText("5 jobs");
	await expect(page).not.toHaveURL(/state=/);
	await page.reload();
	await pressed([]);
	await expect(cards).toHaveCount(5);
	await stateChip(page, "Done").click();
	await stateChip(page, "Done").click();
	await pressed([]);
	await expect(cards).toHaveCount(5);
	await page.goto("/agents/coder/projects/lsegal%2Fzvidlib");
	await pressed(["Done"]);
});

test("a URL's state query preselects its chips over the remembered ones", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	await stateChip(page, "Done").click();
	await expect(page.getByTestId("job-card")).toHaveCount(1);

	await page.goto(
		"/agents/coder/projects/lsegal%2Faviary?state=running,failed",
	);
	for (const [label, on] of [
		["Starting", "false"],
		["Paused", "false"],
		["Running", "true"],
		["Done", "false"],
		["Failed", "true"],
	])
		await expect(stateChip(page, label)).toHaveAttribute("aria-pressed", on);
	const cards = page.getByTestId("job-card");
	await expect(cards).toHaveCount(2);

	// The URL's selection is now the remembered one.
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	await expect(page).toHaveURL(/[?&]state=running(,|%2C)failed$/);
	await expect(cards).toHaveCount(2);
});

test("a job card's PR icon is colored by its pull request state", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	const icon = (n: string) =>
		page
			.getByTestId("job-card")
			.filter({ hasText: n })
			.getByTestId("pr-state-icon");
	for (const [n, label, color] of [
		["#218", "Draft pull request", /text-gray-400/],
		["#209", "Open pull request", /text-green-600/],
		["#200", "Merged pull request", /text-purple-600/],
	] as const) {
		await expect(icon(n)).toHaveAttribute("aria-label", label);
		await expect(icon(n).locator("title")).toHaveText(label);
		await expect(icon(n)).toHaveClass(color);
	}
});

test("the kanban view lays jobs out in state columns of compact cards", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	await expect(page.getByTestId("job-card")).toHaveCount(5);
	const toggle = page.getByTestId("kanban-toggle");
	await toggle.click();
	await expect(toggle).toHaveAttribute("aria-pressed", "true");

	const columns = page.getByTestId("kanban-column");
	// A project board leads with its Scheduled column, empty here.
	await expect(columns).toHaveCount(6);
	await expect(columns.locator("h3")).toHaveText([
		/Scheduled\s*0/,
		/Starting\s*1/,
		/Paused\s*1/,
		/Running\s*1/,
		/Done\s*1/,
		/Failed\s*1/,
	]);
	await expect(page.getByTestId("job-card")).toHaveCount(0);
	const cards = page.getByTestId("kanban-card");
	await expect(cards).toHaveCount(5);
	// Compact cards: truncated title, PR, timing, and no output.
	const running = columns
		.filter({ hasText: "Running" })
		.getByTestId("kanban-card");
	await expect(running.locator("a").first()).toHaveClass(/\btruncate\b/);
	await expect(running.getByTestId("job-pr")).toContainText("PR #233");
	await expect(running.getByTestId("kanban-timing")).toHaveText("14m");
	await expect(page.getByTestId("job-output")).toHaveCount(0);
	// A paused card says what it waits on, from the job's recorded reason.
	await expect(
		columns.filter({ hasText: "Paused" }).getByTestId("kanban-paused-reason"),
	).toHaveText("Stacked on PR #226, waiting for it to merge");
	// The kanban reads every job at once rather than a page.
	await expect.poll(() => calls.project_job_list.at(-1)?.limit).toBe(1000);

	// A state filter narrows the board to that state's column.
	await stateChip(page, "Paused").click();
	await expect(columns).toHaveCount(1);
	await expect(columns).toHaveAttribute("data-state", "paused");

	// The layout survives a reload.
	await page.reload();
	await expect(page.getByTestId("kanban-toggle")).toHaveAttribute(
		"aria-pressed",
		"true",
	);
});

test("prompt chat opened mid-turn shows the running turn (#764)", async ({
	page,
}) => {
	const calls: Calls = {};
	Object.assign(
		calls,
		await setup(page, undefined, {
			...chatFixtures(calls),
			session_turn: (args?: Record<string, unknown>) => ({
				agent_id: args?.agent,
				session_id: args?.session_id,
				processing: true,
				items: [
					{ tool: { id: "call_1", name: "project_status", done: true } },
					{ text: "Reading the board" },
					{ tool: { id: "call_2", name: "project_job_get" } },
				],
			}),
		}),
	);
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	await page.getByTestId("prompt-chat-toggle").click();
	const chat = page.getByTestId("project-chat");
	await expect(chat.getByTestId("chat-tool-call")).toHaveCount(2);
	await expect(chat.getByTestId("chat-tool-call").nth(1)).toHaveAttribute(
		"data-pending",
		"true",
	);
	// A running call shows on its chip, not in a typing bubble below it.
	await expect(chat.getByTestId("chat-typing")).toHaveCount(0);
	await expect(chat).toContainText("Reading the board");
});

test("job count stat boxes reflow without wrapping mid-value", async ({
	page,
}) => {
	await setup(page, () => ({
		...STATUS,
		jobs: { ...STATUS.jobs, running: 10, merged_24h: 61 },
		concurrency: 20,
	}));
	// 1280 keeps the sidebar beside a narrow board while the viewport is xl.
	for (const width of [375, 768, 1024, 1280, 1440]) {
		await page.setViewportSize({ width, height: 900 });
		await page.goto("/agents/coder/projects/lsegal%2Faviary");
		// Phones open on the compact row; expanding it keeps the cards from
		// then on.
		if (width < 768) {
			await expect(page.getByTestId("stat-row")).toBeVisible();
			await page.getByTestId("stats-toggle").click();
		}
		const stats = page.getByRole("region", { name: "Job counts" });
		await expect(stats).toContainText("10of 20 busy");
		await expect(stats).toContainText("Merged (24h)61");
		const layout = await stats.evaluate((section) => {
			const lines = (el: Element) => {
				// Text of mixed sizes on one line overlaps vertically; a wrap
				// starts a rect below every earlier one.
				const range = document.createRange();
				range.selectNodeContents(el);
				let count = 0;
				let bottom = Number.NEGATIVE_INFINITY;
				for (const r of [...range.getClientRects()].sort(
					(a, b) => a.top - b.top,
				)) {
					if (r.top >= bottom - 1) count++;
					bottom = Math.max(bottom, r.bottom);
				}
				return count;
			};
			const overflows = (el: Element) => el.scrollWidth > el.clientWidth;
			const cards = [
				...section.querySelectorAll("[data-testid=stat-card]"),
			].map((card) => {
				const label = card.querySelector("[data-testid=stat-label]");
				const value = card.querySelector("[data-testid=stat-value]");
				if (!label || !value) throw new Error("stat card is missing parts");
				const box = card.getBoundingClientRect();
				return {
					text: card.textContent,
					top: Math.round(box.top),
					height: Math.round(box.height),
					labelTop: Math.round(label.getBoundingClientRect().top),
					valueTop: Math.round(value.getBoundingClientRect().top),
					labelLines: lines(label),
					valueLines: lines(value),
					overflow: overflows(card) || overflows(label) || overflows(value),
				};
			});
			const clis = document.querySelector("[aria-label='CLI providers']");
			return {
				cards,
				pageOverflow:
					document.documentElement.scrollWidth > window.innerWidth ||
					overflows(section) ||
					(clis ? overflows(clis) : false),
				chipOverflow: clis
					? [...clis.children].some(
							(chip) =>
								chip.getBoundingClientRect().right >
								clis.getBoundingClientRect().right + 1,
						)
					: false,
			};
		});
		expect(layout.cards).toHaveLength(7);
		expect(layout.pageOverflow, `page overflow at ${width}px`).toBe(false);
		expect(layout.chipOverflow, `CLI chip overflow at ${width}px`).toBe(false);
		for (const card of layout.cards) {
			expect(card.labelLines, `${card.text} label at ${width}px`).toBe(1);
			expect(card.valueLines, `${card.text} value at ${width}px`).toBe(1);
			expect(card.overflow, `${card.text} overflow at ${width}px`).toBe(false);
		}
		// Cards in one row share a height and line up labels and numbers.
		const rows = new Map<number, typeof layout.cards>();
		for (const card of layout.cards) {
			rows.set(card.top, [...(rows.get(card.top) ?? []), card]);
		}
		for (const row of rows.values()) {
			for (const key of ["height", "labelTop", "valueTop"] as const) {
				expect(
					new Set(row.map((card) => card[key])).size,
					`${key} in a row at ${width}px`,
				).toBe(1);
			}
		}
	}
});

test("expanded job count cards fit one row at desktop widths (#923)", async ({
	page,
}) => {
	await setup(page, () => ({
		...STATUS,
		jobs: { ...STATUS.jobs, running: 10, merged_24h: 61 },
		concurrency: 20,
	}));
	for (const width of [1280, 1440]) {
		await page.setViewportSize({ width, height: 900 });
		await page.goto("/agents/coder/projects/lsegal%2Faviary");
		const cards = page.getByTestId("stat-card");
		await expect(cards).toHaveCount(7);
		const boxes = await cards.evaluateAll((els) =>
			els.map((el) => {
				const box = el.getBoundingClientRect();
				return { top: Math.round(box.top), height: box.height };
			}),
		);
		expect(new Set(boxes.map((b) => b.top)).size, `rows at ${width}px`).toBe(1);
		for (const box of boxes) {
			expect(box.height, `card height at ${width}px`).toBeLessThanOrEqual(60);
		}
	}
});

test("job counts fold into one compact row on phones (#910)", async ({
	page,
}) => {
	await setup(page, () => ({
		...STATUS,
		jobs: { ...STATUS.jobs, merged_24h: 65 },
		concurrency: 20,
	}));
	const colors = (testid: string) =>
		page.getByTestId(testid).evaluateAll((els) =>
			els.map((el) => {
				const css = getComputedStyle(el);
				return [css.color, css.backgroundColor, css.borderColor].join(" ");
			}),
		);
	for (const scheme of ["light", "dark"] as const) {
		await page.emulateMedia({ colorScheme: scheme });
		await page.setViewportSize({ width: 390, height: 844 });
		await page.goto("/agents/coder/projects/lsegal%2Faviary");
		await page.evaluate(() =>
			localStorage.removeItem("aviary.projects.statsCompact"),
		);
		await page.reload();
		const row = page.getByTestId("stat-row");
		await expect(row).toBeVisible();
		await expect(page.getByTestId("stat-card")).toHaveCount(0);
		const chips = page.getByTestId("stat-chip");
		await expect(chips).toHaveText([
			/^\d+\/20slots$/,
			/^\d+starting$/,
			/^\d+paused$/,
			/^\d+running$/,
			/^\d+done$/,
			/^\d+failed$/,
			"65merged 24h",
		]);
		const layout = await row.evaluate((section) => {
			const tops = [...section.children].map((chip) =>
				Math.round(chip.getBoundingClientRect().top),
			);
			const chip = section.children[0].getBoundingClientRect();
			return {
				tops: new Set(tops).size,
				height: section.getBoundingClientRect().height,
				chipHeight: chip.height,
				pageOverflow: document.documentElement.scrollWidth > window.innerWidth,
				scrolls: getComputedStyle(section).overflowX,
			};
		});
		expect(layout.tops, "chips on one line").toBe(1);
		expect(layout.height).toBeLessThanOrEqual(layout.chipHeight + 20);
		expect(layout.pageOverflow, "page overflow").toBe(false);
		expect(layout.scrolls).toBe("auto");
		// Each chip keeps its card's colors.
		const chipColors = await colors("stat-chip");
		const toggle = page.getByTestId("stats-toggle");
		await expect(toggle).toHaveAttribute("aria-expanded", "false");
		await toggle.click();
		await expect(row).toHaveCount(0);
		await expect(page.getByTestId("stat-card")).toHaveCount(7);
		await expect(toggle).toHaveAttribute("aria-expanded", "true");
		expect(await colors("stat-card"), `${scheme} colors`).toEqual(chipColors);
		// The choice survives a reload, both ways.
		await page.reload();
		await expect(page.getByTestId("stat-card")).toHaveCount(7);
		await toggle.click();
		await expect(row).toBeVisible();
		await page.reload();
		await expect(row).toBeVisible();
	}
	// Wide boards keep the cards until the user picks the row.
	await page.evaluate(() =>
		localStorage.removeItem("aviary.projects.statsCompact"),
	);
	await page.setViewportSize({ width: 1280, height: 900 });
	await page.reload();
	await expect(page.getByTestId("stat-card")).toHaveCount(7);
	await expect(page.getByTestId("stat-row")).toHaveCount(0);
});

test("an overflowing compact row keeps its scrollbar off the chips and fades its cut edge (#917)", async ({
	page,
}) => {
	await setup(page, () => ({
		...STATUS,
		jobs: { ...STATUS.jobs, merged_24h: 65 },
		concurrency: 20,
	}));
	const row = page.getByTestId("stat-row");
	const mask = () => row.evaluate((el) => getComputedStyle(el).maskImage);
	for (const width of [320, 1280]) {
		await page.setViewportSize({ width, height: 844 });
		await page.goto("/agents/coder/projects/lsegal%2Faviary");
		await page.evaluate(() =>
			localStorage.setItem("aviary.projects.statsCompact", "1"),
		);
		await page.reload();
		await expect(row).toBeVisible();
		const layout = await row.evaluate((section: HTMLElement) => {
			const box = section.getBoundingClientRect();
			const chips = [...section.children].map((chip) =>
				chip.getBoundingClientRect(),
			);
			const scrollbar = section.offsetHeight - section.clientHeight;
			return {
				overflows: section.scrollWidth > section.clientWidth,
				tops: new Set(chips.map((chip) => Math.round(chip.top))).size,
				// Nothing is clipped vertically.
				clipped: chips.some(
					(chip) => chip.top < box.top || chip.bottom > box.bottom,
				),
				// Space between the chips' bottom borders and where the
				// scrollbar starts, overlay or not.
				gap:
					box.bottom -
					scrollbar -
					Math.max(...chips.map((chip) => chip.bottom)),
			};
		});
		expect(layout.tops, `one line at ${width}px`).toBe(1);
		expect(layout.clipped, `clipped at ${width}px`).toBe(false);
		expect(layout.gap, `scrollbar gap at ${width}px`).toBeGreaterThanOrEqual(4);
		if (!layout.overflows) {
			expect(await mask(), `no fade at ${width}px`).toBe("none");
			continue;
		}
		// Only the right edge fades at the start, both edges mid-way, and only
		// the left once scrolled to the end.
		const fades = async () => {
			const m = await mask();
			return {
				left: /^linear-gradient\(to right, rgba\(0, 0, 0, 0\)/.test(m),
				right: /rgba\(0, 0, 0, 0\)\)$/.test(m),
			};
		};
		expect(await fades()).toEqual({ left: false, right: true });
		await row.evaluate((el) => {
			el.scrollLeft = 20;
		});
		await expect.poll(fades).toEqual({ left: true, right: true });
		await row.evaluate((el) => {
			el.scrollLeft = el.scrollWidth;
		});
		await expect.poll(fades).toEqual({ left: true, right: false });
	}
	// The narrow phone has to overflow for this test to mean anything.
	await page.setViewportSize({ width: 320, height: 844 });
	await expect
		.poll(() => row.evaluate((el) => el.scrollWidth > el.clientWidth))
		.toBe(true);
});

for (const { name, width } of [
	{ name: "desktop", width: 1440 },
	{ name: "narrow desktop", width: 1024 },
	{ name: "mobile", width: 390 },
]) {
	test(`job detail header keeps its actions on one row at ${name} width (#797)`, async ({
		page,
	}) => {
		await page.setViewportSize({ width, height: 900 });
		const title =
			"Job detail view: rich single-row transcript, collapsible raw Output, prominent Clone, and a stepper that never scrolls sideways";
		await setup(page, () => STATUS, {
			project_job_get: (args?: Record<string, unknown>) => {
				const found = JOBS.find(
					(j) => j.target === args?.target && j.number === args?.number,
				);
				if (!found) throw new Error("no such job");
				return {
					...found,
					title,
					...CLONES[found.number],
					scrollback: "",
					events: [],
					raw_output: "",
				};
			},
		});
		await page.goto("/agents/coder/projects/lsegal%2Faviary/jobs/218");
		// The job's actions sit in the shared header's title row (#1147).
		const header = page.getByTestId("settings-title-row");
		const actions = page.getByTestId("job-detail-actions");
		await expect(page.getByTestId("job-detail-title")).toContainText(title);
		await expect(actions.getByRole("button", { name: "Stop" })).toBeVisible();

		const buttons = actions.locator(":scope > a, :scope > button");
		// Raw output, Retry, Stop and close; the Issue/PR links live in the title (#835).
		await expect(buttons).toHaveCount(4);
		const boxes = await buttons.evaluateAll((els) =>
			els.map((el) => {
				const r = el.getBoundingClientRect();
				return { top: r.top, height: r.height };
			}),
		);
		// Every action, the close button included, sits on one row at one height.
		expect(new Set(boxes.map((b) => b.top)).size).toBe(1);
		expect(new Set(boxes.map((b) => b.height)).size).toBe(1);
		await expect(page.getByTestId("job-detail-close")).toHaveAttribute(
			"aria-label",
			"Close details",
		);

		const layout = await header.evaluate((el) => {
			const t = el
				.querySelector("[data-testid=job-detail-title]")
				?.getBoundingClientRect();
			const a = el
				.querySelector("[data-testid=settings-actions]")
				?.getBoundingClientRect();
			return {
				overflow: el.scrollWidth - el.clientWidth,
				titleHeight: t?.height ?? 0,
				titleRight: t?.right ?? 0,
				titleBottom: t?.bottom ?? 0,
				actionsLeft: a?.left ?? 0,
				actionsTop: a?.top ?? 0,
			};
		});
		expect(layout.overflow).toBeLessThanOrEqual(0);
		// The long title wraps instead of squeezing or overlapping the actions.
		expect(layout.titleHeight).toBeGreaterThan(15);
		// The actions sit beside the title, or wrap below it when they don't fit.
		expect(
			layout.titleRight <= layout.actionsLeft ||
				layout.titleBottom <= layout.actionsTop,
		).toBe(true);
		if (width < 1024)
			expect(layout.titleBottom).toBeLessThanOrEqual(layout.actionsTop);
	});
}

test("job detail's title PR link shows the PR state (#834)", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/agents/coder/projects/lsegal%2Faviary/jobs/218");
	const pr = page.getByTestId("job-detail-pr-link");
	await expect(pr).toHaveAttribute("target", "_blank");
	await expect(pr).toHaveAttribute("data-pr-state", "draft");
	await expect(pr.getByTestId("pr-state-icon")).toHaveAttribute(
		"aria-label",
		"Draft pull request",
	);
	await page.goto("/agents/coder/projects/lsegal%2Faviary/jobs/200");
	await expect(pr).toHaveAttribute("data-pr-state", "merged");
	await expect(page.getByTestId("job-detail-issue-link")).toHaveAttribute(
		"target",
		"_blank",
	);
});

test("job detail's Raw output is an icon button that toggles its panel (#834)", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/agents/coder/projects/lsegal%2Faviary/jobs/218");
	const actions = page.getByTestId("job-detail-actions");
	const toggle = actions.getByRole("button", {
		name: "Raw output",
		exact: true,
	});
	await expect(toggle).toHaveAttribute("aria-pressed", "false");
	await expect(toggle).toHaveText("");
	await expect(toggle).toHaveAttribute("title", /raw CLI output/);
	const size = async (name: string) => {
		const box = await actions
			.getByRole("button", { name, exact: true })
			.boundingBox();
		return box && { width: box.width, height: box.height };
	};
	expect(await size("Raw output")).toEqual(await size("Retry"));

	await toggle.click();
	await expect(page.getByTestId("raw-output")).toBeVisible();
	await expect(toggle).toHaveAttribute("aria-pressed", "true");
	await toggle.click();
	await expect(page.getByTestId("raw-output")).toHaveCount(0);
	await expect(toggle).toHaveAttribute("aria-pressed", "false");
});

test("job detail's close button is a borderless ghost, and it and Escape close the pane (#834)", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/agents/coder/projects/lsegal%2Faviary/jobs/218");
	const close = page.getByRole("link", { name: "Close details" });
	await expect(close).toHaveAttribute("title", "Close details");
	const border = (el: Element) =>
		Number.parseFloat(getComputedStyle(el).borderLeftWidth);
	expect(await close.evaluate(border)).toBe(0);
	expect(
		await page.getByRole("button", { name: "Retry" }).evaluate(border),
	).toBeGreaterThan(0);
	await close.click();
	await expect(page).toHaveURL("/agents/coder/projects/lsegal%2Faviary");
	await expect(page.getByTestId("job-detail")).toHaveCount(0);

	await page.goto("/agents/coder/projects/lsegal%2Faviary/jobs/218");
	await expect(page.getByTestId("job-detail")).toBeVisible();
	await page.keyboard.press("Escape");
	await expect(page).toHaveURL("/agents/coder/projects/lsegal%2Faviary");
	await expect(page.getByTestId("job-detail")).toHaveCount(0);
	await expect(page.getByTestId("job-card").first()).toBeVisible();
});

// Long unbroken tokens: a clone path, a URL and minified JSON.
const LONG_PATH =
	"/var/folders/_3/6nm7yxhj0v3716vn7dyl134h0000gn/T/aviary-gh-fix-761-b/internal/projects/core/very_long_directory_name_without_breaks/file_with_a_really_long_name.go";
const LONG_URL =
	"https://github.com/lsegal/aviary/actions/runs/43097434009/job/123456789012?pr=834&check_suite_focus=true&very_long_query_parameter=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
const LONG_JSON = JSON.stringify({
	type: "assistant",
	message: { content: [{ type: "text", text: "x".repeat(160) }] },
});
const LONG_EVENTS = [
	{ kind: "system", time: at(9), text: `Cloned into ${LONG_PATH}` },
	{
		kind: "message",
		time: at(8),
		text: `See ${LONG_URL} and \`${LONG_PATH}\`.\n\n\`\`\`json\n${LONG_JSON}\n\`\`\``,
	},
	{ kind: "reasoning", time: at(8), text: LONG_JSON },
	{
		kind: "plan",
		time: at(7),
		items: [{ text: `Edit ${LONG_PATH}`, status: "in_progress" }],
	},
	{
		kind: "tool_call",
		time: at(7),
		call_id: "w1",
		name: "Read",
		input: { file_path: LONG_PATH },
	},
	{ kind: "tool_result", time: at(7), call_id: "w1", output: LONG_JSON },
	{
		kind: "file_change",
		time: at(6),
		path: LONG_PATH,
		diff: `@@ -1,1 +1,1 @@\n-${LONG_JSON}\n+${LONG_URL}`,
	},
	{
		kind: "command",
		time: at(5),
		call_id: "w2",
		command: `cat ${LONG_PATH} | curl -d @- ${LONG_URL}`,
		status: "running",
	},
	{
		kind: "command",
		time: at(5),
		call_id: "w2",
		output: `${LONG_JSON}\n${LONG_URL}`,
		exit_code: 0,
		status: "done",
	},
	{
		kind: "comment_relay",
		time: at(4),
		author: "lsegal",
		text: `Look at ${LONG_URL}`,
		url: "https://github.com/lsegal/aviary/issues/221#issuecomment-1",
	},
];

// Elements wider than their box, other than one-line rows that truncate
// with an ellipsis on purpose.
const sidewaysOverflow = (root: Element) =>
	[root, ...root.querySelectorAll("*")]
		.filter(
			(el): el is HTMLElement =>
				el instanceof HTMLElement &&
				getComputedStyle(el).textOverflow !== "ellipsis" &&
				!el.classList.contains("sr-only") &&
				el.scrollWidth > el.clientWidth + 1,
		)
		.map((el) => `${el.tagName}.${el.className}`);

for (const { name, width, height } of [
	{ name: "mobile", width: 390, height: 844 },
	{ name: "narrow desktop", width: 820, height: 800 },
]) {
	test(`the rich transcript never scrolls sideways at ${name} width (#834)`, async ({
		page,
	}) => {
		EVENTS[221] = LONG_EVENTS;
		const clone = CLONES[221];
		CLONES[221] = { checkout_directory: LONG_PATH, checkout_state: "present" };
		try {
			await page.setViewportSize({ width, height });
			await setup(page);
			await page.goto("/agents/coder/projects/lsegal%2Faviary/jobs/221");
			const transcript = page.getByTestId("job-transcript");
			await expect(transcript.locator('[data-event="command"]')).toHaveCount(1);
			// Expanded content wraps too.
			await transcript.evaluate((el) => {
				for (const d of el.querySelectorAll("details")) d.open = true;
			});
			expect(await transcript.evaluate(sidewaysOverflow)).toEqual([]);
			expect(
				await page.getByTestId("job-meta").evaluate(sidewaysOverflow),
			).toEqual([]);
			await expect(page.getByTestId("job-clone-path")).toHaveText(LONG_PATH);

			// The board card's transcript shares the rule.
			await page.goto("/agents/coder/projects/lsegal%2Faviary");
			const rich = page
				.getByTestId("job-card")
				.filter({ hasText: "#221" })
				.getByTestId("job-output-rich");
			await expect(rich.locator('[data-row="message"]')).toBeVisible();
			expect(await rich.evaluate(sidewaysOverflow)).toEqual([]);
		} finally {
			delete EVENTS[221];
			CLONES[221] = clone;
		}
	});
}
