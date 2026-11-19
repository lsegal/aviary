import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

const minutesAgo = (m: number) =>
	new Date(Date.now() - m * 60_000).toISOString();

const STATUS = {
	instance_id: "7F3CA91B",
	version: "0.7.0",
	delivery: { mode: "poll", status: "online" },
	last_checked: minutesAgo(0.1),
	interval_seconds: 30,
	ready_label: "ready",
	slots: { used: 3, total: 4 },
	counts: { running: 2, waiting: 1, merged_24h: 7, failed: 1 },
	targets: [{ name: "lsegal/aviary", issues: 6 }],
	clis: [
		{
			name: "claude",
			model: "opus",
			signed_in: true,
			quota: "38% of 5h quota left",
		},
		{ name: "codex", model: "gpt-5.5", signed_in: true, quota: "quota ok" },
		{ name: "gemini", model: "", signed_in: false, quota: "" },
	],
	log: [
		"12:03:58 checked lsegal/aviary: 6 ready issues",
		"12:04:11 claimed #221, cloning into ~/.aviary/clones/aviary-221",
	],
	projects: [
		{ name: "lsegal/aviary", running: 2, waiting: 1, jobs: 5 },
		{ name: "lsegal/zvidlib", running: 0, waiting: 0, jobs: 0 },
	],
};

const JOBS = [
	{
		id: "j-209",
		repo: "lsegal/aviary",
		issue: 209,
		title: "Scoop install fails when ~/.local/bin is missing",
		status: "failed",
		skill: "gh-fix",
		cli: "claude/opus",
		pr: { number: 227, repo: "lsegal/aviary", draft: false },
		checks: "CI failed after 3 repairs",
		started_at: minutesAgo(60),
		updated_at: minutesAgo(5),
		clone_dir: "~/.aviary/clones/aviary-209",
		output: ["$ gh run view 8812 --log-failed", "X giving up"],
	},
	{
		id: "j-214",
		repo: "lsegal/aviary",
		issue: 214,
		title: "Show token usage per scheduled task",
		status: "waiting",
		skill: "gh-fix",
		cli: "claude/opus",
		pr: { number: 229, repo: "someone/aviary", draft: false },
		waiting_reason: "CI running · 3 of 5 checks passed",
		progress: { done: 5, total: 6 },
		started_at: minutesAgo(41),
		updated_at: minutesAgo(2),
		output: ["▸ marked PR #229 ready", "▸ watching checks"],
	},
	{
		id: "j-218",
		repo: "lsegal/aviary",
		issue: 218,
		title: "Cap scheduler retry backoff at 10 minutes",
		status: "running",
		skill: "gh-fix",
		cli: "claude/opus",
		branch: "fix/issue-218",
		pr: { number: 233, repo: "lsegal/aviary", draft: true },
		progress: { done: 2, total: 5 },
		started_at: minutesAgo(14),
		updated_at: minutesAgo(1),
		clone_dir: "~/.aviary/clones/aviary-218",
		output: [],
	},
	{
		id: "j-221",
		repo: "lsegal/aviary",
		issue: 221,
		title: "Export a session as Markdown from the chat menu",
		status: "running",
		skill: "gh-fix",
		cli: "codex/gpt-5.5",
		branch: "fix/issue-221",
		started_at: minutesAgo(3),
		updated_at: minutesAgo(0.5),
		output: [],
	},
	{
		id: "j-200",
		repo: "lsegal/aviary",
		issue: 200,
		title: "Add a dark theme toggle",
		status: "merged",
		skill: "gh-fix",
		cli: "claude/opus",
		pr: { number: 205, repo: "lsegal/aviary", draft: false },
		started_at: minutesAgo(300),
		updated_at: minutesAgo(200),
		output: ["✓ merged PR #205"],
	},
];

const EVENTS_218 = [
	{ type: "system", text: "Claimed #218", time: minutesAgo(14) },
	{
		type: "system",
		text: "Opened draft PR #233 on fix/issue-218",
		time: minutesAgo(13),
	},
	{
		type: "message",
		text: "I can reproduce it: retries back off **without a cap**. I'll clamp the delay at 10 minutes.",
	},
	{ type: "reasoning", text: "The backoff doubles each attempt." },
	{
		type: "plan",
		items: [
			{ text: "Reproduce the unbounded backoff", status: "completed" },
			{ text: "Clamp the delay", status: "in_progress" },
			{ text: "Add a test", status: "pending" },
		],
	},
	{
		type: "tool_call",
		call_id: "c1",
		name: "Read",
		input: { file_path: "internal/scheduler/queue.go" },
	},
	{
		type: "tool_result",
		call_id: "c1",
		output: "package scheduler\n…",
		error: false,
	},
	{
		type: "file_change",
		path: "internal/scheduler/queue.go",
		diff: "@@ -40,3 +40,4 @@\n delay := base << attempt\n-return delay\n+return min(delay, 10*time.Minute)\n+// capped",
	},
	{
		type: "command",
		command: "go test ./internal/scheduler/...",
		output: "--- FAIL: TestBackoffCap (0.01s)",
		exit_code: 1,
		status: "done",
	},
	{
		type: "comment_relay",
		author: "lsegal",
		target: "#218",
		body: "Keep the jitter, please.",
		url: "https://github.com/lsegal/aviary/issues/218#issuecomment-1",
	},
	{
		type: "command",
		command: "go test ./internal/scheduler/...",
		output: "ok  internal/scheduler 2.41s",
		exit_code: 0,
		status: "done",
	},
	{ type: "usage", input_tokens: 182000, output_tokens: 9100, cost: 1.42 },
];

interface Calls {
	[tool: string]: Record<string, unknown>[];
}

async function setup(page: Page) {
	await setAuthToken(page);
	const calls: Calls = {};
	const record = (tool: string, args?: Record<string, unknown>) => {
		calls[tool] ??= [];
		calls[tool].push(args ?? {});
	};
	let tick = 0;
	const outputFor = (id: string) => {
		tick++;
		const base = [
			`▸ read internal/scheduler/queue.go (${id})`,
			"$ go test ./internal/scheduler/...",
		];
		return [...base, `ok  internal/scheduler tick ${tick}`];
	};
	await mockMCP(page, {
		agent_list: [],
		project_status: (args?: Record<string, unknown>) => {
			record("project_status", args);
			return STATUS;
		},
		project_job_list: (args?: Record<string, unknown>) => {
			record("project_job_list", args);
			const statuses = args?.status as string[] | undefined;
			const jobs = statuses
				? JOBS.filter((j) => statuses.includes(j.status))
				: JOBS;
			return { jobs, total: jobs.length };
		},
		project_job_get: (args?: Record<string, unknown>) => {
			record("project_job_get", args);
			const id = String(args?.id);
			const job = JOBS.find((j) => j.id === id);
			return {
				job: {
					...job,
					allowed_commenters: ["lsegal", "teammate"],
					session: "7f3c…a9",
					events: id === "j-218" ? EVENTS_218 : [],
				},
				output: outputFor(id),
			};
		},
		project_job_retry: (args?: Record<string, unknown>) => {
			record("project_job_retry", args);
			return { ok: true };
		},
		project_job_stop: (args?: Record<string, unknown>) => {
			record("project_job_stop", args);
			return { ok: true };
		},
		project_refresh: (args?: Record<string, unknown>) => {
			record("project_refresh", args);
			return { ok: true };
		},
	});
	return calls;
}

test("sidebar lists watched projects with live counts", async ({ page }) => {
	await setup(page);
	await page.goto("/overview");
	const nav = page.locator("nav").last();
	await expect(nav.getByRole("link", { name: /^Projects/ })).toBeVisible();
	await expect(nav.getByTestId("projects-running-badge")).toHaveText("2");

	await page.goto("/projects");
	await expect(page).toHaveURL("/projects/lsegal%2Faviary");
	await expect(nav.getByRole("link", { name: /lsegal\/aviary/ })).toContainText(
		"2 running",
	);
	await expect(
		nav.getByRole("link", { name: /lsegal\/zvidlib/ }),
	).toContainText("idle");
});

test("board shows status, ordered job cards and live output", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/projects/lsegal%2Faviary");

	await expect(
		page.getByRole("heading", { name: "lsegal/aviary" }),
	).toBeVisible();
	await expect(page.getByTestId("delivery-status")).toHaveText("live");
	const stats = page.getByRole("region", { name: "Job counts" });
	await expect(stats).toContainText("3of 4 busy");
	await expect(stats).toContainText("Merged · 24h7");
	const clis = page.getByRole("region", { name: "Coding CLIs" });
	await expect(clis).toContainText("38% of 5h quota left");
	await expect(clis).toContainText("signed out");

	const cards = page.getByTestId("job-card");
	await expect(cards).toHaveCount(5);
	// Running and errored jobs come first, merged jobs last.
	await expect(cards.nth(0)).toContainText("Running");
	await expect(cards.nth(1)).toContainText("Running");
	await expect(cards.nth(2)).toContainText("#209");
	await expect(cards.nth(4)).toContainText("Merged");

	// Fork-aware PR link and waiting reason.
	const waiting = cards.filter({ hasText: "#214" });
	await expect(
		waiting.getByRole("link", { name: "PR someone/aviary#229" }),
	).toHaveAttribute("href", "https://github.com/someone/aviary/pull/229");
	await expect(waiting.getByTestId("job-waiting")).toHaveText(
		"CI running · 3 of 5 checks passed",
	);
	await expect(waiting.getByTestId("job-progress")).toHaveAttribute(
		"style",
		/width: 83%/,
	);

	// Running cards stream output and refresh within a few seconds.
	const running = cards.filter({ hasText: "#218" }).getByTestId("job-output");
	await expect(running).toContainText(
		"read internal/scheduler/queue.go (j-218)",
	);
	const first = await running.textContent();
	await expect(running).not.toHaveText(first ?? "", { timeout: 5_000 });
});

test("retry, stop, filters and check now call project tools", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto("/projects/lsegal%2Faviary");
	const card = page.getByTestId("job-card").filter({ hasText: "#218" });
	await card.getByRole("button", { name: "Retry" }).click();
	await expect
		.poll(() => calls.project_job_retry?.[0])
		.toEqual({ id: "j-218" });
	await card.getByRole("button", { name: "Stop" }).click();
	await expect.poll(() => calls.project_job_stop?.[0]).toEqual({ id: "j-218" });
	// Merged jobs can be retried but not stopped.
	const merged = page.getByTestId("job-card").filter({ hasText: "#200" });
	await expect(merged.getByRole("button", { name: "Stop" })).toHaveCount(0);

	await page.getByRole("tab", { name: "Failed" }).click();
	await expect(page.getByTestId("job-card")).toHaveCount(1);
	expect(calls.project_job_list.at(-1)).toMatchObject({
		project: "lsegal/aviary",
		status: ["failed"],
	});

	await page.getByRole("button", { name: "Check now" }).click();
	await expect
		.poll(() => calls.project_refresh?.[0])
		.toEqual({ project: "lsegal/aviary" });
});

test("run view renders the typed event timeline read-only", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/projects/lsegal%2Faviary");
	await page
		.getByTestId("job-card")
		.filter({ hasText: "#218" })
		.getByRole("link", { name: "Details" })
		.click();
	await expect(page).toHaveURL("/projects/lsegal%2Faviary/jobs/j-218");

	const steps = page.getByTestId("lifecycle");
	await expect(steps.locator('[aria-current="step"]')).toContainText(
		"Implementing",
	);

	const timeline = page.getByRole("list", { name: "Activity" });
	await expect(timeline.locator("strong")).toHaveText("without a cap");
	await expect(
		timeline.locator('[data-event="command"]').first(),
	).toContainText("exit 1");
	await expect(timeline.locator('[data-event="command"]').last()).toContainText(
		"exit 0",
	);
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
		"lsegal commented on #218",
	);
	await expect(timeline.locator('[data-event="usage"]')).toContainText("$1.42");

	await expect(page.getByTestId("run-output")).toContainText("$ go test");
	await expect(
		page.getByRole("link", { name: "Comment on #218 ↗" }),
	).toHaveAttribute(
		"href",
		"https://github.com/lsegal/aviary/issues/218#new_comment_field",
	);
	await expect(
		page.getByText("allowed commenters (lsegal, teammate)"),
	).toBeVisible();

	// No steering: nothing on the page can send a message to the job.
	await expect(page.getByRole("textbox")).toHaveCount(0);
	await expect(page.getByRole("button", { name: /send/i })).toHaveCount(0);
});

test.describe("phone width", () => {
	test.use({ viewport: { width: 390, height: 844 } });

	for (const path of [
		"/projects/lsegal%2Faviary",
		"/projects/lsegal%2Faviary/jobs/j-218",
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
});
