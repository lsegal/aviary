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
	ready_label: "ready",
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
		command: "go test ./internal/scheduler/...",
		output: "--- FAIL: TestBackoffCap (0.01s)",
		exit_code: 1,
		status: "completed",
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
		command: "go test ./internal/scheduler/...",
		output: "ok  internal/scheduler 2.41s",
		exit_code: 0,
		status: "completed",
	},
	{
		kind: "usage",
		time: at(6),
		usage: { input_tokens: 182000, output_tokens: 9100, cost_usd: 1.42 },
	},
];

// project_job_list's project filter: the board passes targets, one or more.
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
					(!args?.state || j.state === args.state),
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
				events: found.number === 218 ? EVENTS_218 : [],
				raw_output:
					found.number === 218
						? '{"type":"assistant","message":{"content":[{"type":"text","text":"I can reproduce it"}]}}\n'
						: "",
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

test("an agent's GitHub Projects in the sidebar list its projects with live counts", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/system/overview");
	// coder is the only agent, so its pages, GitHub Projects among them, are
	// the sidebar's top level (#538).
	const row = page.getByTestId("sidebar-row-projects");
	await expect(row.getByTestId("project-running-badge")).toHaveText("2");
	await expect(
		page.getByTestId("sidebar-nav").getByRole("link", { name: /^GitHub/ }),
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

test('the GitHub Projects running badge is gray and the Add project "+" sits beside it on hover', async ({
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
		.getByRole("link", { name: /^GitHub Projects/ })
		.hover();
	await expect.poll(opacity).toBe(1);

	const badgeBox = await badge.boundingBox();
	const plusBox = await plus.boundingBox();
	expect(badgeBox && plusBox).toBeTruthy();
	if (!badgeBox || !plusBox) return;
	expect(badgeBox.x + badgeBox.width).toBeLessThanOrEqual(plusBox.x);
});

test('GitHub Projects badges sit at the row\'s right edge until the "+" shows', async ({
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
		.getByRole("link", { name: /^GitHub Projects/ });
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

test("the GitHub Projects row counts running jobs, not watched projects", async ({
	page,
}) => {
	await page.clock.install();
	let running = 0;
	await setupOneProject(page, () => running);
	await page.goto("/system/overview");
	const row = page
		.getByTestId("sidebar-row-projects")
		.getByRole("link", { name: /^GitHub Projects/ });
	await expect(row).toHaveText("GitHub Projects");
	await expect(row.getByTestId("project-running-badge")).toHaveCount(0);

	running = 1;
	await page.clock.runFor(10_000);
	await expect(row.getByTestId("project-running-badge")).toHaveText("1");

	running = 0;
	await page.clock.runFor(10_000);
	await expect(row.getByTestId("project-running-badge")).toHaveCount(0);
	await expect(row).toHaveText("GitHub Projects");
});

test.describe("on a phone", () => {
	test.use({ viewport: { width: 390, height: 844 } });

	test("the GitHub Projects tab counts running jobs, not watched projects", async ({
		page,
	}) => {
		await page.clock.install();
		let running = 0;
		await setupOneProject(page, () => running);
		await page.goto("/system/overview");
		const tab = page
			.getByRole("navigation", { name: "Main" })
			.getByRole("link", { name: /^GitHub Projects/ });
		await expect(tab).toHaveText("GitHub Projects");
		await expect(tab.getByTestId("project-running-badge")).toHaveCount(0);

		running = 1;
		await page.clock.runFor(10_000);
		await expect(tab.getByTestId("project-running-badge")).toHaveText("1");

		running = 0;
		await page.clock.runFor(10_000);
		await expect(tab.getByTestId("project-running-badge")).toHaveCount(0);
		await expect(tab).toHaveText("GitHub Projects");
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
	const stats = page.getByRole("region", { name: "Job counts" });
	await expect(stats).toContainText("2of 4 busy");
	await expect(stats).toContainText("Merged (24h)3");
	await expect(stats).not.toContainText("Completed");
	await expect(stats).not.toContainText("Waiting");
	await expect
		.poll(() => calls.project_status.at(-1)?.targets)
		.toEqual(["lsegal/aviary"]);
	// Each board state's card counts the jobs its filter tab shows.
	for (const label of ["Starting", "Paused", "Running", "Done", "Failed"]) {
		const card = stats.locator("div").filter({ hasText: label });
		await expect(card).toHaveText(new RegExp(`^${label}\\d+$`));
		const count = Number((await card.textContent())?.replace(/\D/g, ""));
		await page.getByRole("tab", { name: label }).click();
		await expect(page.getByTestId("job-card")).toHaveCount(count);
	}
	await page.getByRole("tab", { name: "All" }).click();
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

	// Running cards stream output and refresh within a few seconds.
	const running = cards.filter({ hasText: "#218" }).getByTestId("job-output");
	await expect(running).toContainText(
		"read internal/scheduler/queue.go (#218)",
	);
	const first = await running.textContent();
	await expect(running).not.toHaveText(first ?? "", { timeout: 5_000 });
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

	await page.getByRole("tab", { name: "Paused" }).click();
	await expect(page.getByTestId("job-card")).toHaveCount(1);
	await expect(page.getByTestId("job-card")).toContainText("#214");
	await expect
		.poll(() => calls.project_job_list.some((c) => c.state === "paused"))
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

test("an agent's GitHub Projects is one board of all its projects, with no settings", async ({
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
					(!args?.state || j.state === args.state) &&
					(!query || j.title.toLowerCase().includes(query.toLowerCase())),
			);
			return { jobs, total: jobs.length, offset: 0, limit: args?.limit ?? 50 };
		},
	});
	await page.goto("/agents/coder/projects");

	await expect(
		page.getByRole("heading", { name: "GitHub Projects" }),
	).toBeVisible();
	await expect(page.getByTestId("board-projects")).toHaveText("2 projects");
	// Every project's jobs share the board, each naming its project.
	const cards = page.getByTestId("job-card");
	await expect(cards).toHaveCount(6);
	expect(calls.project_job_list.at(-1)?.targets).toEqual([
		"lsegal/aviary",
		"lsegal/zvidlib",
	]);
	const zvid = cards.filter({ hasText: "Decode B-frames" });
	await expect(zvid.getByTestId("job-project")).toHaveText("lsegal/zvidlib");
	await expect(zvid.getByTestId("job-project")).toHaveAttribute(
		"href",
		"/agents/coder/projects/lsegal%2Fzvidlib",
	);

	// No settings live on the route: no form fields, and Settings opens the
	// agent's projects in System → Projects.
	await expect(page.locator("[data-field]")).toHaveCount(0);
	await expect(page.getByTestId("agent-projects")).toHaveCount(0);
	await expect(page.getByTestId("project-settings-link")).toHaveAttribute(
		"href",
		"/system/projects?agent=coder",
	);

	// Filters and search narrow the whole board, as on one project's.
	await page.getByRole("tab", { name: "Running" }).click();
	await expect(cards).toHaveCount(2);
	await page.getByLabel("Search jobs").fill("B-frames");
	await expect(cards).toHaveCount(1);
	await expect(cards).toContainText("lsegal/zvidlib");

	// One check reads every project.
	await page.getByLabel("Search jobs").fill("");
	await page.getByRole("button", { name: "Check now" }).click();
	await expect.poll(() => calls.project_refresh?.[0]).toEqual({});
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
	await expect(page.getByRole("heading", { level: 1 })).toHaveText(
		"#218 Cap scheduler retry backoff at 10 minutes",
	);

	const steps = page.getByTestId("lifecycle");
	await expect(steps.locator('[aria-current="step"]')).toContainText(
		"Implementing",
	);

	const timeline = page.getByRole("list", { name: "Activity" });
	await expect(timeline.locator("strong")).toHaveText("without a cap");
	const commands = timeline.locator('[data-event="command"]');
	await expect(commands.first()).toContainText("exit 1");
	await expect(commands.last()).toContainText("exit 0");
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

	// Raw CLI stdout sits beside the timeline.
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

const SIGNED_OUT = {
	signed_out: true,
	signing_in: false,
	url: "https://github.com/lsegal/aviary/issues?q=is%3Aissue+state%3Aopen+label%3Aready",
	status: 404,
	since: minutesAgo(2),
};
const browserStatus = (github: object) => ({
	...STATUS,
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

/** Fixtures for the built-in Projects agent behind the prompt chat. */
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
	// It talks to the built-in Projects agent's one hidden session, never to
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
	const sent = chat.locator(".whitespace-pre-wrap", { hasText: "line two" });
	await expect(sent).toBeVisible();
	expect(await sent.innerText()).toBe("line one\nline two");
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
			await expect(page.locator("h1").last()).toBeVisible();
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
					(!args?.state || j.state === args.state),
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
	await page.getByRole("tab", { name: "Failed" }).click();
	await expect(cards).toHaveCount(1);
	await expect(cards).toContainText("#306");
	await expect(page.getByTestId("search-count")).toHaveText(
		"1 of 1 job matches",
	);
	await page.getByRole("tab", { name: /^All/ }).click();

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

test("the view tabs, five state filters and kanban toggle sit beside the search", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	const toolbar = page.getByTestId("job-toolbar");
	await expect(
		toolbar.getByRole("searchbox", { name: "Search jobs" }),
	).toBeVisible();
	await expect(
		toolbar.getByRole("tablist", { name: "Project views" }),
	).toBeVisible();
	await expect(
		toolbar.getByRole("tablist", { name: "Job filter" }).getByRole("tab"),
	).toHaveText([/^All/, "Starting", "Paused", "Running", "Done", "Failed"]);
	await expect(toolbar.getByTestId("kanban-toggle")).toBeVisible();
	// The header no longer carries its own copy of the tabs.
	await expect(
		page.getByRole("tablist", { name: "Project views" }),
	).toHaveCount(1);
	await expect(page.getByRole("tablist", { name: "Job filter" })).toHaveCount(
		1,
	);

	// Each state filter shows exactly that state's jobs.
	for (const [tab, numbers] of [
		["Starting", ["#221"]],
		["Running", ["#218"]],
		["Paused", ["#214"]],
		["Done", ["#200"]],
		["Failed", ["#209"]],
	] as const) {
		await toolbar.getByRole("tab", { name: tab }).click();
		const cards = page.getByTestId("job-card");
		await expect(cards).toHaveCount(numbers.length);
		for (const n of numbers) await expect(cards).toContainText(n);
	}
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
	await page.getByRole("tab", { name: "Paused" }).click();
	await expect(columns).toHaveCount(1);
	await expect(columns).toHaveAttribute("data-state", "paused");

	// The layout survives a reload.
	await page.reload();
	await expect(page.getByTestId("kanban-toggle")).toHaveAttribute(
		"aria-pressed",
		"true",
	);
});
