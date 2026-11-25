import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

// Queueing pull requests for gh-safe-merge and Discussion threads for
// gh-discuss from a Code Loop board (#1095). Fixtures follow the payloads
// internal/projects' Manager returns.

const minutesAgo = (m: number) =>
	new Date(Date.now() - m * 60_000).toISOString();

const TARGET = "lsegal/aviary";

const queuedActions = (status: string) => ({
	retry: ["paused", "complete", "failed"].includes(status),
	pause: ["queued", "active", "waiting"].includes(status),
	resume: status === "paused",
	stop: ["queued", "active", "waiting", "paused"].includes(status),
});

const JOBS = [
	{
		key: `${TARGET}#218`,
		target: TARGET,
		number: 218,
		title: "Fix the scheduler's missed fires",
		status: "complete",
		state: "done",
		kind: "issue",
		agent: "claude",
		model: "opus",
		repository: TARGET,
		issue_url: `https://github.com/${TARGET}/issues/218`,
		pull_request: 230,
		pull_request_url: `https://github.com/${TARGET}/pull/230`,
		pull_request_state: "open",
		started: minutesAgo(40),
		actions: { retry: true, pause: false, resume: false, stop: false },
	},
	{
		key: `${TARGET}#1201`,
		target: TARGET,
		number: 1201,
		title: "Add a dark theme toggle",
		status: "active",
		state: "running",
		kind: "merge",
		agent: "claude",
		model: "opus",
		repository: TARGET,
		issue_url: `https://github.com/${TARGET}/pull/1201`,
		pull_request: 1201,
		queue_position: 1,
		started: minutesAgo(3),
		actions: queuedActions("active"),
	},
	{
		key: `${TARGET}#1207`,
		target: TARGET,
		number: 1207,
		title: "Bump vite",
		status: "queued",
		state: "starting",
		kind: "merge",
		repository: TARGET,
		issue_url: `https://github.com/${TARGET}/pull/1207`,
		pull_request: 1207,
		queue_position: 2,
		started: minutesAgo(1),
		actions: queuedActions("queued"),
		retry_unavailable: "Already queued",
	},
	{
		key: `${TARGET}#1190`,
		target: TARGET,
		number: 1190,
		title: "Rewrite the poller",
		status: "failed",
		state: "failed",
		kind: "merge",
		repository: TARGET,
		issue_url: `https://github.com/${TARGET}/pull/1190`,
		pull_request: 1190,
		summary: "Skipped: the pull request conflicts with main",
		started: minutesAgo(20),
		actions: queuedActions("failed"),
	},
	{
		key: `${TARGET}#44`,
		target: TARGET,
		number: 44,
		title: "How do I watch a Project board?",
		status: "queued",
		state: "starting",
		kind: "discuss",
		repository: TARGET,
		issue_url: `https://github.com/${TARGET}/discussions/44`,
		started: minutesAgo(1),
		actions: queuedActions("queued"),
		retry_unavailable: "Already queued",
	},
];

const PULLS = [
	{
		number: 1210,
		title: "Fix typo in README",
		author: "octocat",
		url: `https://github.com/${TARGET}/pull/1210`,
		ci: "passing",
		review: "approved",
		mergeable: "mergeable",
	},
	{
		number: 1212,
		title: "Speed up the job list",
		author: "hubot",
		url: `https://github.com/${TARGET}/pull/1212`,
		ci: "pending",
		review: "review_required",
		mergeable: "conflicting",
	},
	{
		number: 1207,
		title: "Bump vite",
		author: "dependabot",
		url: `https://github.com/${TARGET}/pull/1207`,
		ci: "failing",
		review: "none",
		mergeable: "mergeable",
		owned_by: `${TARGET}#1207`,
	},
	{
		number: 230,
		title: "Fix the scheduler's missed fires",
		author: "lsegal",
		url: `https://github.com/${TARGET}/pull/230`,
		ci: "passing",
		review: "none",
		mergeable: "mergeable",
	},
];

// A scheduled task of the project, which shows as a scheduled card.
const SCHEDULE = {
	id: "coder/nightly-triage",
	name: "nightly-triage",
	agent_name: "coder",
	agent_id: "coder",
	type: "project",
	project: TARGET,
	prompt: "Triage issues opened since the last run and label them.",
	enabled: true,
	trigger_type: "daily",
	at: "02:00",
	description: "daily at 02:00",
	next_run: new Date(Date.now() + 42 * 60_000).toISOString(),
	created_at: "0001-01-01T00:00:00Z",
};

async function setup(
	page: Page,
	{
		noMerge = false,
		delivery = "poll",
		listing = "ok",
	}: {
		noMerge?: boolean;
		delivery?: string;
		/** ok lists; unavailable returns the server's notice; error fails the call. */
		listing?: "ok" | "unavailable" | "error";
	} = {},
) {
	const calls: Record<string, Record<string, unknown>[]> = {};
	const record = (tool: string, args?: Record<string, unknown>) => {
		calls[tool] ??= [];
		calls[tool].push(args ?? {});
	};
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: [],
		config_get: {
			agents: [
				{
					name: "coder",
					model: "cli/claude/opus",
					projects: [{ target: TARGET }],
				},
			],
		},
		project_status: {
			identity: "7F3CA91B",
			running: true,
			targets: [TARGET],
			issue_counts: { [TARGET]: 3 },
			states: { starting: 2, paused: 0, running: 1, done: 1, failed: 1 },
			jobs: {
				running: 1,
				queued: 2,
				paused: 0,
				completed: 1,
				merged_24h: 0,
				failed: 1,
			},
			concurrency: 2,
			delivery,
			poll_interval: "30s",
			no_merge: noMerge,
			last_check: minutesAgo(0.1),
		},
		// The board reads its schedules' entries apart from its jobs.
		project_job_list: (args?: Record<string, unknown>) => {
			const kinds = args?.kinds as string[] | undefined;
			const jobs = kinds?.includes("schedule") ? [] : JOBS;
			return { jobs, total: jobs.length, offset: 0, limit: 500 };
		},
		task_list: [SCHEDULE],
		project_job_get: (args?: Record<string, unknown>) => ({
			...JOBS.find((j) => j.number === args?.number),
			scrollback: "",
			events: [],
		}),
		project_queue_candidates: (args?: Record<string, unknown>) => {
			record("project_queue_candidates", args);
			if (listing === "error") throw new Error("gh: connection refused");
			if (listing === "unavailable")
				return {
					target: TARGET,
					repository: TARGET,
					skill: args?.skill,
					no_merge: noMerge,
					unavailable:
						args?.skill === "gh-discuss"
							? "Can't list discussions: sign in to GitHub (gh auth login) to enable this. You can still queue by number."
							: "Can't list pull requests: sign in to GitHub (gh auth login) to enable this. You can still queue by number.",
				};
			if (args?.skill === "gh-discuss")
				return {
					target: TARGET,
					repository: TARGET,
					skill: "gh-discuss",
					no_merge: noMerge,
					discussions: [
						{
							number: 51,
							title: "Is Windows supported?",
							author: "ada",
							url: "",
							category: "Q&A",
							comments: 0,
							answered: false,
						},
						{
							number: 44,
							title: "How do I watch a Project board?",
							author: "bob",
							url: "",
							category: "Q&A",
							comments: 0,
							answered: false,
							owned_by: `${TARGET}#44`,
						},
					],
				};
			return {
				target: TARGET,
				repository: TARGET,
				skill: "gh-safe-merge",
				no_merge: noMerge,
				pull_requests: PULLS,
			};
		},
		project_job_queue: (args?: Record<string, unknown>) => {
			record("project_job_queue", args);
			const numbers = (args?.numbers as number[]) ?? [];
			return {
				target: TARGET,
				skill: args?.skill,
				results: numbers.map((number, i) => ({
					number,
					queued: true,
					key: `${TARGET}#${number}`,
					...(args?.skill === "gh-safe-merge" ? { position: i + 3 } : {}),
				})),
			};
		},
	});
	return calls;
}

const BOARD = `/agents/coder/projects/${encodeURIComponent(TARGET)}`;

test("the header's Queue menu offers Fix issue, Safe merge PR and Discuss", async ({
	page,
}) => {
	await setup(page);
	await page.goto(BOARD);
	await page.getByTestId("queue-menu").click();
	const items = page.getByTestId("queue-menu-items");
	await expect(items.getByRole("menuitem")).toHaveText([
		/Fix issue/,
		/Safe merge PR/,
		/Discuss/,
	]);
});

test("the header's Queue button looks like its neutral icon neighbors (#1305)", async ({
	page,
}) => {
	await setup(page);
	const queue = page.getByTestId("queue-menu");
	const settings = page.getByTestId("project-settings-link");
	const look = (el: Element) => {
		const s = getComputedStyle(el);
		const r = el.getBoundingClientRect();
		return {
			background: s.backgroundColor,
			border: s.borderColor,
			borderWidth: s.borderWidth,
			radius: s.borderRadius,
			color: s.color,
			padding: s.padding,
			width: r.width,
			height: r.height,
		};
	};
	for (const colorScheme of ["light", "dark"] as const) {
		await page.emulateMedia({ colorScheme });
		await page.goto(BOARD);
		await expect(queue).toHaveAttribute("aria-label", "Queue…");
		await expect(queue).toHaveAttribute("title", "Queue…");
		await expect(queue).toHaveText("");
		await expect
			.poll(() => queue.evaluate(look))
			.toEqual(await settings.evaluate(look));
		await settings.hover();
		const hovered = await settings.evaluate(look);
		await queue.hover();
		await expect.poll(() => queue.evaluate(look)).toEqual(hovered);
	}
});

test("the safe-merge picker lists open PRs and queues the picked ones in order", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto(BOARD);
	await page.getByTestId("queue-menu").click();
	await page.getByTestId("queue-gh-safe-merge").click();
	const dialog = page.getByTestId("queue-dialog");
	await expect(dialog).toHaveAttribute("data-skill", "gh-safe-merge");
	const rows = dialog.getByTestId("queue-candidate");
	await expect(rows).toHaveCount(4);
	const typo = rows.filter({ hasText: "#1210" });
	await expect(typo).toContainText("octocat");
	await expect(typo).toContainText("passing");
	await expect(typo).toContainText("approved");
	await expect(typo).toContainText("clean");
	await expect(rows.filter({ hasText: "#1212" })).toContainText("conflicts");
	// A pull request a job already owns names the job and cannot be picked.
	const owned = rows.filter({ hasText: "#1207" });
	await expect(owned.getByTestId("queue-owned")).toHaveText(`${TARGET}#1207`);
	await expect(owned).toHaveAttribute("aria-disabled", "true");
	await owned.click({ force: true });
	await expect(owned.getByTestId("queue-order")).toHaveCount(0);

	// Picking order is merge order.
	await rows.filter({ hasText: "#1212" }).click();
	await rows.filter({ hasText: "#1210" }).click();
	await expect(
		rows.filter({ hasText: "#1212" }).getByTestId("queue-order"),
	).toHaveText("1");
	await expect(
		rows.filter({ hasText: "#1210" }).getByTestId("queue-order"),
	).toHaveText("2");

	// Search narrows the list.
	await dialog.getByTestId("queue-search").fill("typo");
	await expect(rows).toHaveCount(1);
	await dialog.getByTestId("queue-search").fill("");

	await dialog.getByTestId("queue-submit").click();
	await expect
		.poll(() => calls.project_job_queue?.[0])
		.toEqual({
			target: TARGET,
			skill: "gh-safe-merge",
			numbers: [1212, 1210],
			titles: { "1210": "Fix typo in README", "1212": "Speed up the job list" },
		});
	await expect(dialog.getByTestId("queue-results")).toContainText(
		"#1212: queued, #3 in the merge lane",
	);
});

test("merge and discuss jobs show as board cards with their place and reason", async ({
	page,
}) => {
	await setup(page);
	await page.goto(BOARD);
	await expect(page.getByTestId("job-card")).toHaveCount(JOBS.length, {
		timeout: 15_000,
	});
	const running = page
		.getByTestId("job-card")
		.filter({ hasText: "Add a dark theme toggle" });
	await expect(running.getByTestId("job-kind")).toHaveText("Safe merge");
	await expect(running.getByTestId("job-queue-position")).toHaveText(
		"merging now",
	);
	await expect(running.getByRole("link", { name: "PR #1201" })).toHaveAttribute(
		"href",
		`https://github.com/${TARGET}/pull/1201`,
	);
	await expect(running.getByRole("button", { name: "Stop" })).toBeVisible();

	const queued = page.getByTestId("job-card").filter({ hasText: "Bump vite" });
	await expect(queued.getByTestId("job-queue-position")).toHaveText(
		"#2 in the merge queue",
	);
	await expect(queued.getByRole("button", { name: "Retry" })).toBeDisabled();

	const failed = page
		.getByTestId("job-card")
		.filter({ hasText: "Rewrite the poller" });
	await expect(failed.getByTestId("job-reason")).toHaveText(
		"Skipped: the pull request conflicts with main",
	);
	await expect(failed.getByRole("button", { name: "Retry" })).toBeEnabled();

	const discuss = page
		.getByTestId("job-card")
		.filter({ hasText: "watch a Project board" });
	await expect(discuss.getByTestId("job-kind")).toHaveText("Discuss");
	await expect(
		discuss.getByRole("link", { name: "Discussion #44" }),
	).toHaveAttribute("href", `https://github.com/${TARGET}/discussions/44`);
	await expect(discuss.getByTestId("job-agent")).toContainText("gh-discuss");
});

// Safe merge is queued only from the header's Queue menu, never from a card
// (#1163): a fix job already drives its own pull request to a merge.
for (const [board, path] of [
	["a project board", BOARD],
	["the root board", "/agents/coder/projects"],
]) {
	test(`job and scheduled cards on ${board} offer no safe-merge queue button`, async ({
		page,
	}) => {
		await setup(page);
		await page.goto(path);
		const cards = page.getByTestId("job-card");
		await expect(cards).toHaveCount(JOBS.length, { timeout: 15_000 });
		await expect(page.getByTestId("scheduled-card")).toHaveCount(1);
		// The fix job with an open linked pull request, which used to offer it.
		const fix = cards.filter({ hasText: "#218" });
		await expect(fix.getByTestId("job-pr")).toContainText("PR #230");
		await expect(page.getByTestId("job-safe-merge")).toHaveCount(0);
		await expect(page.getByRole("button", { name: "Safe merge" })).toHaveCount(
			0,
		);

		// The header's Queue menu still opens the safe-merge picker.
		await page.getByTestId("queue-menu").click();
		await page.getByTestId("queue-gh-safe-merge").click();
		const dialog = page.getByTestId("queue-dialog");
		await expect(dialog).toHaveAttribute("data-skill", "gh-safe-merge");
		await expect(dialog.getByTestId("queue-candidate")).toHaveCount(4);
		await expect(dialog.getByTestId("queue-order")).toHaveCount(0);
	});
}

test("with no_merge, safe merge is disabled with an explanation", async ({
	page,
}) => {
	await setup(page, { noMerge: true });
	await page.goto(BOARD);
	await page.getByTestId("queue-menu").click();
	const item = page.getByTestId("queue-gh-safe-merge");
	await expect(item).toHaveAttribute("data-disabled", "");
	await expect(item).toContainText("no_merge");
	await expect(item).toHaveAttribute(
		"title",
		/no_merge setting leaves pull requests for a human/,
	);
});

test("Discuss queues picked threads and Fix issue queues typed issue numbers", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto(BOARD);
	await page.getByTestId("queue-menu").click();
	await page.getByTestId("queue-gh-discuss").click();
	const dialog = page.getByTestId("queue-dialog");
	const rows = dialog.getByTestId("queue-candidate");
	await expect(
		rows.filter({ hasText: "#44" }).getByTestId("queue-owned"),
	).toBeVisible();
	await rows.filter({ hasText: "#51" }).click();
	await dialog.getByTestId("queue-submit").click();
	await expect
		.poll(() => calls.project_job_queue?.[0])
		.toMatchObject({ skill: "gh-discuss", numbers: [51] });
	await dialog.getByRole("button", { name: "Close" }).click();

	await page.getByTestId("queue-menu").click();
	await page.getByTestId("queue-gh-fix").click();
	await dialog.getByTestId("queue-issues").fill("#12, 15");
	await expect(dialog.getByTestId("queue-submit")).toHaveText("Queue 2 issues");
	await dialog.getByTestId("queue-submit").click();
	await expect
		.poll(() => calls.project_job_queue?.[1])
		.toMatchObject({ skill: "gh-fix", numbers: [12, 15] });
});

test("in browser delivery mode the pickers list open PRs and Discussions", async ({
	page,
}) => {
	const calls = await setup(page, { delivery: "browser" });
	await page.goto(BOARD);
	await page.getByTestId("queue-menu").click();
	await page.getByTestId("queue-gh-safe-merge").click();
	const dialog = page.getByTestId("queue-dialog");
	const rows = dialog.getByTestId("queue-candidate");
	await expect(rows).toHaveCount(4);
	const conflicting = rows.filter({ hasText: "#1212" });
	await expect(conflicting).toContainText("hubot");
	await expect(conflicting).toContainText("pending");
	await expect(conflicting).toContainText("review required");
	await expect(conflicting).toContainText("conflicts");
	await expect(
		rows.filter({ hasText: "#1207" }).getByTestId("queue-owned"),
	).toHaveText(`${TARGET}#1207`);
	await expect(dialog.getByTestId("queue-unavailable")).toHaveCount(0);
	await page.keyboard.press("Escape");

	await page.getByTestId("queue-menu").click();
	await page.getByTestId("queue-gh-discuss").click();
	await expect(rows).toHaveCount(2);
	await expect(rows.filter({ hasText: "#51" })).toContainText("Q&A");
	// Each open asks once; the server caches the listing briefly.
	expect(calls.project_queue_candidates).toHaveLength(2);
});

for (const c of [
	{ skill: "gh-safe-merge", noun: "pull requests", number: 1156 },
	{ skill: "gh-discuss", noun: "discussions", number: 44 },
] as const) {
	test(`when ${c.noun} can't be listed, a calm notice shows and numbers still queue`, async ({
		page,
	}) => {
		const calls = await setup(page, {
			delivery: "browser",
			listing: "unavailable",
		});
		await page.goto(BOARD);
		await page.getByTestId("queue-menu").click();
		await page.getByTestId(`queue-${c.skill}`).click();
		const dialog = page.getByTestId("queue-dialog");
		const notice = dialog.getByTestId("queue-unavailable");
		await expect(notice).toHaveText(
			`Can't list ${c.noun}: sign in to GitHub (gh auth login) to enable this. You can still queue by number.`,
		);
		await expect(notice).not.toHaveClass(/red/);
		await expect(dialog).not.toContainText("No open");
		await expect(dialog).not.toContainText("SignInGuard");
		await expect(dialog.getByRole("alert")).toHaveCount(0);
		await expect(dialog.getByTestId("queue-candidates")).toHaveCount(0);

		await dialog.getByTestId("queue-number").fill(`#${c.number}`);
		await dialog.getByTestId("queue-number").press("Enter");
		await expect
			.poll(() => calls.project_job_queue?.[0])
			.toEqual({
				target: TARGET,
				skill: c.skill,
				numbers: [c.number],
				titles: {},
			});
		await expect(dialog.getByTestId("queue-results")).toContainText(
			`#${c.number}: queued`,
		);
	});
}

test("a failed listing call shows the notice, not an empty list or a red error", async ({
	page,
}) => {
	await setup(page, { listing: "error" });
	await page.goto(BOARD);
	await page.getByTestId("queue-menu").click();
	await page.getByTestId("queue-gh-safe-merge").click();
	const dialog = page.getByTestId("queue-dialog");
	await expect(dialog.getByTestId("queue-unavailable")).toHaveText(
		"Can't list pull requests right now. You can still queue by number.",
	);
	await expect(dialog).not.toContainText("No open");
	await expect(dialog).not.toContainText("connection refused");
	await expect(dialog.getByRole("alert")).toHaveCount(0);
	await expect(dialog.getByTestId("queue-number")).toBeEnabled();
});

test("typed PR numbers queue after the picked rows", async ({ page }) => {
	const calls = await setup(page);
	await page.goto(BOARD);
	await page.getByTestId("queue-menu").click();
	await page.getByTestId("queue-gh-safe-merge").click();
	const dialog = page.getByTestId("queue-dialog");
	await dialog
		.getByTestId("queue-candidate")
		.filter({ hasText: "#1210" })
		.click();
	await dialog.getByTestId("queue-number").fill("#1300, 1210");
	await expect(dialog.getByTestId("queue-submit")).toHaveText("Queue 2 PRs");
	await dialog.getByTestId("queue-submit").click();
	await expect
		.poll(() => calls.project_job_queue?.[0]?.numbers)
		.toEqual([1210, 1300]);
});

test("a watch session's one-project board offers the Queue menu", async ({
	page,
}) => {
	await page.addInitScript(() => localStorage.removeItem("aviary_token"));
	await page.route("/api/health", (route) =>
		route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify({
				ok: true,
				version: "dev",
				auth_disabled: true,
				minimal: true,
			}),
		}),
	);
	await mockMCP(page, {
		agent_list: [{ name: "watch" }],
		job_list: [],
		task_list: [],
		task_upcoming: [],
		config_validate: [],
		config_get: {
			agents: [{ name: "watch", projects: [{ target: "owner/repo" }] }],
		},
		project_status: {
			states: { starting: 0, paused: 0, running: 0, done: 0, failed: 0 },
			targets: ["owner/repo"],
		},
		project_job_list: { jobs: [], total: 0, offset: 0, limit: 50 },
		project_queue_candidates: {
			target: "owner/repo",
			repository: "owner/repo",
			pull_requests: [],
		},
	});
	await page.goto("/agents/watch/projects");
	await page.getByTestId("queue-menu").click();
	await page.getByTestId("queue-gh-safe-merge").click();
	await expect(page.getByTestId("queue-dialog")).toContainText(
		"No open pull requests",
	);
});
