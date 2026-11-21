import { expect, type Page, test } from "@playwright/test";
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
	agents: ["claude", "codex", "gemini"],
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
}) => ({
	key: `${fields.target ?? "lsegal/aviary"}#${fields.number}`,
	target: fields.target ?? "lsegal/aviary",
	number: fields.number,
	title: fields.title,
	status: fields.status,
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

const JOBS = [
	job({
		number: 218,
		title: "Cap scheduler retry backoff at 10 minutes",
		status: "active",
		agent: "claude",
		model: "opus",
		started: 14,
		pr: 233,
		progress: [2, 5],
	}),
	job({
		number: 221,
		title: "Export a session as Markdown from the chat menu",
		status: "active",
		agent: "codex",
		model: "gpt-5.5",
		started: 3,
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
					projects: {
						list: [{ target: "lsegal/aviary" }, { target: "lsegal/zvidlib" }],
					},
				},
			],
		},
		project_status: (args?: Record<string, unknown>) => {
			record("project_status", args);
			return status();
		},
		project_job_list: (args?: Record<string, unknown>) => {
			record("project_job_list", args);
			const jobs = JOBS.filter(
				(j) =>
					(!args?.target || j.target === args.target) &&
					(!args?.status || j.status === args.status),
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
				checkout_directory: `~/.aviary/clones/aviary-${found.number}`,
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
	const agent = page.getByTestId("sidebar-sole-agent");
	await expect(agent.getByTestId("agent-running-badge")).toHaveText("2");
	await expect(
		page.getByTestId("sidebar-nav").getByRole("link", { name: /^GitHub/ }),
	).toHaveAttribute("href", "/agents/coder/projects");

	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	const projects = page.getByTestId("sidebar-agent-projects");
	await expect(
		projects.getByRole("link", { name: /lsegal\/aviary/ }),
	).toContainText("2 running");
	await expect(
		projects.getByRole("link", { name: /lsegal\/zvidlib/ }),
	).toContainText("idle");
	await expect(page.getByTestId("sidebar-add-project")).toHaveAttribute(
		"href",
		"/agents/coder/projects?new=1",
	);
});

test("board shows status, ordered job cards and live output", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/agents/coder/projects/lsegal%2Faviary");

	await expect(
		page.getByRole("heading", { name: "lsegal/aviary" }),
	).toBeVisible();
	await expect(page.getByTestId("delivery-status")).toHaveText("live");
	await expect(page.getByText("poll · checked")).toContainText("every 30s");
	const stats = page.getByRole("region", { name: "Job counts" });
	await expect(stats).toContainText("2of 4 busy");
	await expect(stats).toContainText("Waiting1");
	await expect(stats).toContainText("Merged (24h)3");
	await expect(stats).not.toContainText("Completed");
	const clis = page.getByRole("region", { name: "CLI providers" });
	await expect(clis).toContainText("38% of 5h quota left");
	await expect(clis).toContainText("signed out");

	const cards = page.getByTestId("job-card");
	await expect(cards).toHaveCount(5);
	// Running and errored jobs come first, finished jobs last.
	await expect(cards.nth(0)).toContainText("Running");
	await expect(cards.nth(1)).toContainText("Running");
	await expect(cards.nth(2)).toContainText("#209");
	await expect(cards.nth(4)).toContainText("Done");
	await expect(cards.nth(0)).toContainText("gh-fix · codex/gpt-5.5");

	// Fork-aware PR link, waiting reason and checklist progress.
	const paused = cards.filter({ hasText: "#214" });
	await expect(
		paused.getByRole("link", { name: "PR someone/aviary#229" }),
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

	await page.getByRole("tab", { name: "Waiting" }).click();
	await expect(page.getByTestId("job-card")).toHaveCount(1);
	await expect(page.getByTestId("job-card")).toContainText("#214");
	const statuses = calls.project_job_list
		.filter((c) => c.status)
		.map((c) => c.status);
	expect(statuses).toEqual(
		expect.arrayContaining(["waiting", "paused", "queued"]),
	);

	await page.getByRole("button", { name: "Check now" }).click();
	await expect
		.poll(() => calls.project_refresh?.[0])
		.toEqual({ target: "lsegal/aviary" });
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
				{ id: "p1", agent_id: "projects", name: "main", created_at: at(60) },
			];
		},
		session_messages: () => [
			{
				id: "m1",
				session_id: "p1",
				role: "user",
				content: "What does the board look like?",
				media_url: PNG,
				timestamp: at(5),
			},
			{
				id: "m2",
				session_id: "p1",
				role: "assistant",
				content: "Here is the failing run.",
				media_url: PNG,
				timestamp: at(4),
			},
			// What the server persists once a prompt has run.
			...(calls.agent_run ?? []).flatMap((run, i) => [
				{
					id: `r${i}`,
					session_id: "p1",
					role: "user",
					content: run.message,
					timestamp: at(0),
				},
				{
					id: `a${i}`,
					session_id: "p1",
					role: "assistant",
					content: "#218 is clamping the retry delay.",
					timestamp: at(0),
				},
			]),
		],
		agent_run: (args?: Record<string, unknown>) => {
			record("agent_run", args);
			return { stream: ["#218 is clamping the retry delay."], result: "" };
		},
	};
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
	// It talks to the built-in Projects agent, never to a job.
	await expect.poll(() => calls.session_list?.at(-1)?.agent).toBe("projects");
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
	});
	await expect(chat).toContainText(
		"Summarize the current session log of work on lsegal/aviary#218",
	);
	await expect(chat).toContainText("#218 is clamping the retry delay.");
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
					(!args?.target || j.target === args.target) &&
					(!args?.status || j.status === args.status),
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
	await expect(search).toHaveClass(/field-input/);
	await expect(pages).toContainText("Page 1 of 3");
	await expect(cards).toHaveCount(12);
	await expect(page.getByTestId("search-count")).toHaveCount(0);

	// Typing filters at once and lands on the first page of the matches.
	await pages.getByRole("button", { name: "Next" }).click();
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
