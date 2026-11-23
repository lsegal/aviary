import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

interface MockJob {
	id: string;
	task_id: string;
	agent_id: string;
	prompt: string;
	status: "pending" | "in_progress" | "completed" | "failed" | "canceled";
	attempts: number;
	max_retries: number;
	trigger?: string;
	error?: string;
	output?: string;
	created_at: string;
	updated_at: string;
}

const TASK_ID = "assistant/report";

function minutesAgo(m: number): string {
	return new Date(Date.now() - m * 60_000).toISOString();
}

function job(
	id: string,
	status: MockJob["status"],
	startedMinutesAgo: number,
	extra: Partial<MockJob> = {},
): MockJob {
	return {
		id,
		task_id: TASK_ID,
		agent_id: "assistant",
		prompt: "Report",
		status,
		attempts: 1,
		max_retries: 0,
		trigger: "schedule",
		created_at: minutesAgo(startedMinutesAgo),
		updated_at: minutesAgo(startedMinutesAgo - 1),
		...extra,
	};
}

/**
 * Mocks one agent with a "report" task whose runs are `jobs`, which the
 * tests may change, and records the actions the page takes.
 */
async function mockTask(
	page: Page,
	jobs: MockJob[],
	opts: { enabled?: boolean; history?: object[] } = {},
) {
	const state = { jobs, enabled: opts.enabled ?? true };
	const calls: { tool: string; args: Record<string, unknown> }[] = [];
	const logs = new Map<string, string>();
	const record = (tool: string, args?: Record<string, unknown>) =>
		calls.push({ tool, args: args ?? {} });
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: [
			{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
		],
		config_get: () => ({
			agents: [
				{
					name: "assistant",
					model: "x/y",
					tasks: [
						{
							name: "report",
							every: "15m",
							prompt: "Report",
							enabled: state.enabled,
						},
					],
				},
			],
		}),
		config_validate: [],
		task_list: () => [
			{
				id: TASK_ID,
				agent_name: "assistant",
				name: "report",
				enabled: state.enabled,
				description: "every 15m0s (Local)",
				next_run: new Date(Date.now() + 14 * 60_000 + 5_000).toISOString(),
				history: opts.history ?? [],
			},
		],
		task_session: (args?: Record<string, unknown>) => ({
			id: "task-report",
			agent_id: "assistant",
			name: String(args?.name),
		}),
		session_messages: [],
		session_list: [],
		auth_list: [],
		tool_list: [],
		job_list: (args?: Record<string, unknown>) => {
			record("job_list", args);
			return state.jobs;
		},
		job_logs: (args?: Record<string, unknown>) =>
			logs.get(String(args?.id)) ?? `logs of ${String(args?.id)}`,
		job_stop: (args?: Record<string, unknown>) => {
			record("job_stop", args);
			for (const j of state.jobs) if (j.id === args?.id) j.status = "canceled";
			return "stopped";
		},
		task_run: (args?: Record<string, unknown>) => {
			record("task_run", args);
			const started = job(`run-${state.jobs.length}`, "in_progress", 0, {
				trigger: "manual",
			});
			state.jobs = [started, ...state.jobs];
			return started;
		},
		task_update: (args?: Record<string, unknown>) => {
			record("task_update", args);
			state.enabled = Boolean(args?.enabled);
			return "updated";
		},
	});
	return { state, calls, logs };
}

test.beforeEach(async ({ page }) => {
	// Each test starts with the default layout: chat on, default height.
	await page.addInitScript(() => {
		if (!sessionStorage.getItem("taskDashboardInit")) {
			localStorage.removeItem("aviary.taskChat.open");
			localStorage.removeItem("aviary.taskChat.height");
			sessionStorage.setItem("taskDashboardInit", "1");
		}
	});
});

test("a task with no runs shows empty states and its schedule", async ({
	page,
}) => {
	const { calls } = await mockTask(page, [], { enabled: false });
	await page.goto("/agents/assistant/tasks/report");

	await expect(page.getByTestId("task-title")).toHaveText("report");
	await expect(page.getByTestId("task-state")).toHaveText("Paused");
	await expect(page.getByTestId("task-schedule")).toContainText(
		"every 15m0s (Local) · paused",
	);
	await expect(page.getByTestId("task-run-log-empty")).toBeVisible();
	await expect(page.getByTestId("task-run-history-empty")).toContainText(
		"No runs yet",
	);
	expect(calls.find((c) => c.tool === "job_list")?.args).toEqual({
		task: TASK_ID,
	});

	await page.getByTestId("task-toggle-enabled").click();
	await expect(page.getByTestId("task-state")).toHaveText("Enabled");
	await expect(page.getByTestId("task-schedule")).toContainText(
		"every 15m0s (Local) · next in 14m",
	);
	expect(calls.find((c) => c.tool === "task_update")?.args).toEqual({
		name: TASK_ID,
		enabled: true,
	});
});

test("a running task streams its logs, and copy and stop work", async ({
	page,
	context,
}) => {
	await context.grantPermissions(["clipboard-read", "clipboard-write"]);
	const running = job("live", "in_progress", 2);
	const { state, calls, logs } = await mockTask(page, [
		running,
		job("done", "completed", 30),
	]);
	const lines = Array.from({ length: 80 }, (_, i) => `line ${i + 1}`);
	logs.set("live", lines.join("\n"));
	await page.goto("/agents/assistant/tasks/report");

	const log = page.getByTestId("task-run-log");
	await expect(log.getByTestId("task-run-log-title")).toHaveText("Current run");
	await expect(log.getByTestId("task-run-result")).toHaveText("Running");
	await expect(log.getByTestId("task-run-log-elapsed")).toContainText(
		"elapsed",
	);
	const output = log.getByTestId("task-run-log-output");
	await expect(output).toContainText("line 80");
	// It follows the newest line.
	await expect
		.poll(() =>
			output.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight),
		)
		.toBeLessThan(30);

	// Scrolling up pauses following while new lines stream in.
	await output.evaluate((el) => {
		el.scrollTop = 0;
		el.dispatchEvent(new Event("scroll"));
	});
	await expect(log.getByTestId("task-run-log-follow")).toBeVisible();
	logs.set("live", [...lines, "line 81"].join("\n"));
	await expect(output).toContainText("line 81");
	expect(await output.evaluate((el) => el.scrollTop)).toBe(0);
	await log.getByTestId("task-run-log-follow").click();
	await expect(log.getByTestId("task-run-log-follow")).toBeHidden();

	await log.getByTestId("task-run-log-copy").click();
	expect(await page.evaluate(() => navigator.clipboard.readText())).toContain(
		"line 81",
	);

	await log.getByTestId("task-run-log-stop").click();
	await expect
		.poll(() => calls.find((c) => c.tool === "job_stop")?.args)
		.toEqual({ id: "live" });
	expect(state.jobs[0].status).toBe("canceled");
	// With nothing running, the stopped run is the last run.
	await expect(log.getByTestId("task-run-log-title")).toHaveText("Last run");
	await expect(log.getByTestId("task-run-result")).toHaveText("Canceled");
});

test("an idle task shows its last run, and Run now starts one", async ({
	page,
}) => {
	const { calls } = await mockTask(page, [
		job("older", "completed", 60),
		job("newest", "failed", 20, { error: "exit status 1: boom" }),
	]);
	await page.goto("/agents/assistant/tasks/report");

	const log = page.getByTestId("task-run-log");
	await expect(log.getByTestId("task-run-log-title")).toHaveText("Last run");
	await expect(log).toHaveAttribute("data-run", "newest");
	await expect(log.getByTestId("task-run-result")).toHaveText("Failed");
	await expect(log.getByTestId("task-run-log-error")).toHaveText(
		"exit status 1: boom",
	);
	await expect(log.getByTestId("task-run-log-output")).toHaveText(
		"logs of newest",
	);

	await page.getByTestId("task-run-now").click();
	expect(calls.find((c) => c.tool === "task_run")?.args).toEqual({
		name: TASK_ID,
	});
	await expect(log.getByTestId("task-run-log-title")).toHaveText("Current run");
	await expect(page.getByTestId("task-stop")).toBeVisible();
	await expect(
		page.getByTestId("task-run-row").first().getByTestId("task-run-trigger"),
	).toHaveText("manual");
});

test("many runs paginate, and results filter from the chips and pills", async ({
	page,
}) => {
	const jobs: MockJob[] = [];
	for (let i = 0; i < 45; i++) {
		const failed = i % 3 === 0;
		jobs.push(
			job(`j${i}`, failed ? "failed" : "completed", 15 * (i + 1), {
				error: failed ? `failure ${i}` : undefined,
				trigger: i === 1 ? "missed" : "schedule",
			}),
		);
	}
	await mockTask(page, jobs, {
		history: [
			{
				fire_time: minutesAgo(3),
				reason: "schedule",
				status: "skipped",
				summary: "previous run still active",
			},
		],
	});
	await page.goto("/agents/assistant/tasks/report");

	const rows = page.getByTestId("task-run-row");
	await expect(rows).toHaveCount(20);
	// Newest first: the skipped fire, then j0.
	await expect(rows.first()).toHaveAttribute("data-result", "skipped");
	await expect(rows.first().getByTestId("task-run-error")).toHaveText(
		"previous run still active",
	);
	await expect(rows.nth(1).getByTestId("task-run-error")).toHaveText(
		"failure 0",
	);
	await expect(rows.nth(2).getByTestId("task-run-trigger")).toHaveText(
		"catch-up",
	);
	await page.getByTestId("task-run-history-more").click();
	await expect(rows).toHaveCount(40);
	await page.getByTestId("task-run-history-more").click();
	await expect(rows).toHaveCount(46);
	await expect(page.getByTestId("task-run-history-more")).toBeHidden();

	await page.getByTestId("task-run-filter-failed").click();
	await expect(rows).toHaveCount(15);
	for (const result of await rows.evaluateAll((els) =>
		els.map((el) => el.getAttribute("data-result")),
	))
		expect(result).toBe("failed");

	const chips = page.getByTestId("stat-chip");
	await chips.filter({ hasText: "succeeded" }).click();
	await expect(page.getByTestId("task-run-filter-succeeded")).toHaveAttribute(
		"aria-pressed",
		"true",
	);
	await expect(rows).toHaveCount(20);
	const rate = chips.filter({ hasText: /%\s*success$/ });
	await expect(rate).toContainText("67%");
	await rate.click();
	await expect(page.getByTestId("task-run-filter-all")).toHaveAttribute(
		"aria-pressed",
		"true",
	);
	await expect(chips.filter({ hasText: "next run" })).toContainText("in 14m");
});

test("a run opens at its own URL that survives a reload", async ({ page }) => {
	await mockTask(page, [
		job("good", "completed", 10),
		job("bad", "failed", 40, { error: "connection refused" }),
	]);
	await page.goto("/agents/assistant/tasks/report");

	await page
		.getByTestId("task-run-row")
		.filter({ hasText: "Failed" })
		.getByTestId("task-run-open")
		.click();
	await expect(page).toHaveURL(
		/\/agents\/assistant\/tasks\/report\/runs\/bad$/,
	);
	const runPage = page.getByTestId("task-run-page");
	await expect(runPage.getByTestId("task-run-log-error")).toHaveText(
		"connection refused",
	);
	await expect(runPage.getByTestId("task-run-log-output")).toHaveText(
		"logs of bad",
	);
	// The sidebar still marks the task open.
	await expect(page.getByTestId("task-chat-panel")).toBeVisible();

	await page.reload();
	await expect(runPage.getByTestId("task-run-log-title")).toHaveText("Run bad");
	await expect(runPage.getByTestId("task-run-log-error")).toHaveText(
		"connection refused",
	);

	await runPage.getByTestId("task-run-back").click();
	await expect(page).toHaveURL(/\/agents\/assistant\/tasks\/report$/);
	await expect(page.getByTestId("task-run-history")).toBeVisible();

	await page.goto("/agents/assistant/tasks/report/runs/nope");
	await expect(page.getByTestId("task-run-missing")).toBeVisible();
});

test("the chat panel toggles and resizes, and both persist", async ({
	page,
}) => {
	await mockTask(page, []);
	await page.goto("/agents/assistant/tasks/report");

	const toggle = page.getByTestId("task-chat-toggle");
	const panel = page.getByTestId("task-chat-panel");
	await expect(toggle).toHaveAttribute("aria-pressed", "true");
	await expect(panel).toHaveAttribute("data-layout", "panel");
	await expect(panel.getByTestId("task-chat")).toBeVisible();
	const before = (await panel.boundingBox())?.height ?? 0;
	expect(before).toBe(320);

	// Dragging the top edge up makes the panel taller.
	const handle = page.getByTestId("task-chat-resize");
	const box = await handle.boundingBox();
	if (!box) throw new Error("no resize handle");
	const x = box.x + box.width / 2;
	const y = box.y + box.height / 2;
	await page.mouse.move(x, y);
	await page.mouse.down();
	await page.mouse.move(x, y - 80, { steps: 4 });
	await page.mouse.up();
	await expect.poll(async () => (await panel.boundingBox())?.height).toBe(400);

	// It stops at its bounds.
	await handle.focus();
	await page.keyboard.press("Home");
	await expect.poll(async () => (await panel.boundingBox())?.height).toBe(160);
	await page.keyboard.press("ArrowUp");
	await expect.poll(async () => (await panel.boundingBox())?.height).toBe(176);

	await page.reload();
	await expect.poll(async () => (await panel.boundingBox())?.height).toBe(176);

	const dashboard = page.getByTestId("task-dashboard");
	const withChat = (await dashboard.boundingBox())?.height ?? 0;
	await toggle.click();
	await expect(panel).toHaveCount(0);
	await expect(toggle).toHaveAttribute("aria-pressed", "false");
	await expect
		.poll(async () => (await dashboard.boundingBox())?.height ?? 0)
		.toBeGreaterThan(withChat + 150);

	await page.reload();
	await expect(page.getByTestId("task-title")).toHaveText("report");
	await expect(panel).toHaveCount(0);
	await expect(toggle).toHaveAttribute("aria-pressed", "false");
});

test("on a phone the chat opens as a bottom sheet", async ({ page }) => {
	await page.setViewportSize({ width: 390, height: 844 });
	await mockTask(page, [job("done", "completed", 30)]);
	await page.goto("/agents/assistant/tasks/report");

	const panel = page.getByTestId("task-chat-panel");
	await expect(panel).toHaveAttribute("data-layout", "sheet");
	await page.getByTestId("task-chat-close").click();
	await expect(panel).toHaveCount(0);
	await expect(page.getByTestId("task-run-history")).toBeVisible();

	await page.getByTestId("task-chat-toggle").click();
	await expect(panel).toBeVisible();
	const box = await panel.boundingBox();
	expect(Math.round((box?.y ?? 0) + (box?.height ?? 0))).toBe(844);
	await page
		.getByTestId("task-chat-backdrop")
		.click({ position: { x: 10, y: 10 } });
	await expect(panel).toHaveCount(0);
});
