import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

const HOUR = 3_600_000;
const iso = (offset: number) => new Date(Date.now() + offset).toISOString();

function job(
	n: number,
	task: string,
	status: string,
	ageMs: number,
	durationMs = 2_000,
) {
	return {
		id: `job_${String(n).padStart(8, "0")}`,
		task_id: `assistant/${task}`,
		agent_id: "assistant",
		prompt: "",
		status,
		attempts: 1,
		max_retries: 3,
		created_at: iso(-ageMs),
		updated_at: iso(-ageMs + durationMs),
	};
}

/**
 * Mocks an agent with three tasks: a daily one that succeeded, a paused
 * weekly one that failed, and a project task running now.
 */
async function mockTasks(page: Page) {
	const config = {
		agents: [
			{
				name: "assistant",
				model: "x/y",
				tasks: [
					{
						name: "daily-report",
						trigger: "daily",
						at: "02:00",
						prompt: "write the report",
					},
					{
						name: "weekly-cleanup",
						trigger: "weekly",
						days: "mon",
						at: "09:00",
						enabled: false,
						prompt: "clean up",
					},
					{
						name: "ci-watch",
						type: "project",
						project: "o/r",
						every: "1h",
						prompt: "watch ci",
					},
				],
			},
		],
	};
	const calls = {
		runs: [] as unknown[],
		updates: [] as Record<string, unknown>[],
	};
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: [
			{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
		],
		config_get: () => config,
		config_validate: [],
		auth_list: [],
		tool_list: [],
		session_list: [],
		job_query: [
			job(4, "ci-watch", "in_progress", 60_000),
			job(3, "weekly-cleanup", "failed", 2 * HOUR, 4_000),
			job(2, "daily-report", "completed", 3 * HOUR, 1_000),
			job(1, "daily-report", "completed", 27 * HOUR, 3_000),
		],
		task_compile_query: [
			{
				id: "task_compile_1",
				agent_id: "assistant",
				task_name: "daily-report",
				status: "succeeded",
				created_at: iso(-HOUR),
				updated_at: iso(-HOUR),
			},
		],
		task_preview: (args?: Record<string, unknown>) => {
			const name = String(args?.name);
			if (name.endsWith("/daily-report"))
				return {
					description: "daily at 02:00 (America/Toronto)",
					next: [iso(41 * 60_000), iso(25 * HOUR)],
				};
			if (name.endsWith("/weekly-cleanup"))
				return { description: "weekly on Mon at 09:00 (UTC)", next: [] };
			return {
				description: "every 1h0m0s (UTC)",
				next: [iso(HOUR / 2), iso(1.5 * HOUR)],
			};
		},
		task_run: (args) => {
			calls.runs.push(args);
			return job(5, "daily-report", "in_progress", 0);
		},
		task_update: (args?: Record<string, unknown>) => {
			calls.updates.push(args ?? {});
			const task = config.agents[0].tasks.find(
				(t) => `assistant/${t.name}` === args?.name,
			);
			if (task && typeof args?.enabled === "boolean")
				task.enabled = args.enabled;
			return "updated";
		},
	});
	return calls;
}

test("the Scheduled Tasks root page has no Reload or Save Changes", async ({
	page,
}) => {
	await mockTasks(page);
	await page.goto("/agents/assistant/tasks");

	await expect(page.getByTestId("settings-title")).toHaveText(
		"Scheduled Tasks",
	);
	await expect(page.getByTestId("tasks-overview")).toBeVisible();
	await expect(page.getByRole("button", { name: "Reload" })).toHaveCount(0);
	await expect(page.getByRole("button", { name: "Save Changes" })).toHaveCount(
		0,
	);
	await expect(page.getByTestId("new-task-button")).toBeVisible();
});

test("a task's editor keeps its own Save Changes", async ({ page }) => {
	await mockTasks(page);
	await page.goto("/agents/assistant/tasks?task=daily-report");

	await expect(
		page.getByRole("button", { name: "Save Changes" }),
	).toBeVisible();
	await expect(page.getByTestId("tasks-overview")).toHaveCount(0);
});

test("the summary stat cards and graphs render with data", async ({ page }) => {
	await mockTasks(page);
	await page.goto("/agents/assistant/tasks");

	const runs = page.getByTestId("stat-runs");
	await expect(runs).toContainText("4");
	await expect(runs).toContainText("2 ok");
	await expect(runs).toContainText("1 failed");
	await expect(
		page.getByTestId("runs-sparkline").locator(".bg-green-500"),
	).not.toHaveCount(0);
	await expect(page.getByTestId("stat-success-rate")).toContainText("67%");
	await expect(page.getByTestId("stat-duration")).toContainText("2.7s");
	await expect(page.getByTestId("stat-duration")).toContainText("median 3.0s");
	// daily-report once and ci-watch twice; the paused task doesn't count.
	await expect(page.getByTestId("stat-upcoming")).toContainText("3");
	await expect(page.getByTestId("stat-running")).toContainText("1");
	await expect(page.getByTestId("stat-failures")).toContainText("1");
	await expect(page.getByTestId("stat-failures")).toContainText(
		"last weekly-cleanup",
	);

	await page.getByTestId("more-analytics-toggle").click();
	await expect(page.getByTestId("jobs-by-day")).toBeVisible();
	await expect(page.getByText("task_compile_1")).toBeVisible();
	await expect(page.getByTestId("job-count")).toHaveText("4 jobs");
});

test("tasks show as cards by default with every field", async ({ page }) => {
	await mockTasks(page);
	await page.goto("/agents/assistant/tasks");

	const cards = page.getByTestId("task-card");
	await expect(cards).toHaveCount(3);
	await expect(page.getByTestId("task-list")).toHaveCount(0);

	const daily = cards.filter({ hasText: "daily-report" });
	await expect(daily.getByTestId("task-enabled")).toHaveAttribute(
		"aria-checked",
		"true",
	);
	await expect(daily.getByTestId("task-owner")).toHaveText("agent assistant");
	await expect(daily.getByTestId("task-schedule")).toHaveText(
		/^daily at 02:00 \(America\/Toronto\) · next in 4\dm$/,
	);
	await expect(daily.getByTestId("task-last-run")).toHaveText(
		"Succeeded 3h ago",
	);
	for (const action of ["Run now", "Pause", "Edit", "View runs"])
		await expect(daily.getByRole("button", { name: action })).toBeVisible();

	const weekly = cards.filter({ hasText: "weekly-cleanup" });
	await expect(weekly.getByTestId("task-schedule")).toHaveText(
		"weekly on Mon at 09:00 (UTC) · paused",
	);
	await expect(weekly.getByTestId("task-last-run")).toHaveText("Failed 2h ago");
	await expect(weekly.getByRole("button", { name: "Resume" })).toBeVisible();

	const ci = cards.filter({ hasText: "ci-watch" });
	await expect(ci.getByTestId("task-owner")).toHaveText("project o/r");
	await expect(ci.getByTestId("task-running")).toBeVisible();
	await expect(ci.getByTestId("task-last-run")).toHaveText("Running now");

	// The filter chips narrow the cards by state.
	await page
		.getByRole("group", { name: "Task filter" })
		.getByRole("button", { name: /Paused/ })
		.click();
	await expect(cards).toHaveCount(1);
	await expect(cards).toContainText("weekly-cleanup");
});

test("the list toggle switches the layout and survives a reload", async ({
	page,
}) => {
	await mockTasks(page);
	await page.goto("/agents/assistant/tasks");
	await expect(page.getByTestId("task-card")).toHaveCount(3);

	await page.getByTestId("layout-list").click();
	await expect(page.getByTestId("task-list")).toBeVisible();
	await expect(page.getByTestId("task-row-item")).toHaveCount(3);
	await expect(page.getByTestId("task-card")).toHaveCount(0);
	await expect(
		page.getByTestId("task-row-item").filter({ hasText: "daily-report" }),
	).toContainText("daily at 02:00 (America/Toronto)");

	await page.reload();
	await expect(page.getByTestId("task-row-item")).toHaveCount(3);
	await expect(page.getByTestId("task-card")).toHaveCount(0);

	await page.getByTestId("layout-cards").click();
	await expect(page.getByTestId("task-card")).toHaveCount(3);
});

test("card quick actions work", async ({ page }) => {
	const calls = await mockTasks(page);
	await page.goto("/agents/assistant/tasks");
	const daily = page
		.getByTestId("task-card")
		.filter({ hasText: "daily-report" });

	await daily.getByRole("button", { name: "Run now" }).click();
	await expect
		.poll(() => calls.runs)
		.toEqual([{ name: "assistant/daily-report" }]);

	await daily.getByRole("button", { name: "Pause" }).click();
	await expect
		.poll(() => calls.updates)
		.toEqual([{ name: "assistant/daily-report", enabled: false }]);
	await expect(daily.getByTestId("task-schedule")).toContainText("· paused");
	await expect(daily.getByRole("button", { name: "Resume" })).toBeVisible();

	await daily.getByTestId("task-enabled").click();
	await expect
		.poll(() => calls.updates.at(-1))
		.toEqual({ name: "assistant/daily-report", enabled: true });

	await daily.getByRole("button", { name: "View runs" }).click();
	await expect(page.getByTestId("job-task-filter")).toHaveText(
		/assistant\/daily-report/,
	);
	await expect(page.getByTestId("job-count")).toHaveText("2 jobs");

	await daily.getByRole("button", { name: "Edit" }).click();
	await expect(page.getByRole("dialog")).toBeVisible();
	await page.keyboard.press("Escape");

	// Clicking the card opens the task's editor.
	await daily.getByRole("heading", { name: "daily-report" }).click();
	await expect(page).toHaveURL(
		/\/agents\/assistant\/tasks\?task=daily-report$/,
	);
	await expect(
		page.getByRole("button", { name: "Save Changes" }),
	).toBeVisible();
});

test("clicking a list row opens the task's editor", async ({ page }) => {
	await mockTasks(page);
	await page.goto("/agents/assistant/tasks");
	await page.getByTestId("layout-list").click();
	await page
		.getByTestId("task-row-item")
		.filter({ hasText: "weekly-cleanup" })
		.locator("td")
		.first()
		.click();
	await expect(page).toHaveURL(
		/\/agents\/assistant\/tasks\?task=weekly-cleanup$/,
	);
});

test("System → Jobs redirects to Scheduled Tasks and has no nav entry", async ({
	page,
}) => {
	await mockTasks(page);
	await page.goto("/system/jobs");

	await expect(page).toHaveURL(/\/agents\/assistant\/tasks$/);
	await expect(page.getByTestId("settings-title")).toHaveText(
		"Scheduled Tasks",
	);
	await expect(page.locator('a[href="/system/jobs"]')).toHaveCount(0);
});

test("an open running job leaves Running and shows why it failed", async ({
	page,
}) => {
	await page.clock.install();
	const running = job(7, "daily-report", "in_progress", 60_000);
	let current: Record<string, unknown> = running;
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: [
			{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
		],
		config_get: {
			agents: [
				{
					name: "assistant",
					model: "x/y",
					tasks: [
						{
							name: "daily-report",
							trigger: "daily",
							at: "02:00",
							prompt: "write the report",
						},
					],
				},
			],
		},
		config_validate: [],
		auth_list: [],
		tool_list: [],
		session_list: [],
		job_query: () => [current],
		job_logs: "error: run timed out: no output received for 15m0s",
		task_compile_query: [],
		task_preview: { description: "daily at 02:00 (UTC)", next: [] },
	});
	await page.goto("/agents/assistant/tasks");
	await page.getByTestId("more-analytics-toggle").click();
	await page
		.getByTestId("job-queue")
		.getByText("assistant/daily-report")
		.click();

	await expect(page.getByText("Waiting for output…")).toBeVisible();
	await expect(
		page.getByText(
			"A run with no output for 15m is stopped and marked failed.",
		),
	).toBeVisible();

	current = {
		...running,
		status: "failed",
		error: "run timed out: no output received for 15m0s",
	};
	await page.clock.fastForward(16_000);

	await expect(page.getByTestId("job-error")).toContainText(
		"run timed out: no output received for 15m0s",
	);
	await expect(page.getByText("Waiting for output…")).toHaveCount(0);
});

// The run details drawer resizes from its left edge and remembers its width
// per browser; narrow viewports keep the fixed layout (#878).

async function openRunDetails(page: Page) {
	await page.getByTestId("more-analytics-toggle").click();
	await page
		.locator("tbody tr")
		.filter({ hasText: "weekly-cleanup" })
		.first()
		.click();
	await expect(page.getByTestId("details-panel")).toBeVisible();
}

async function panelWidth(page: Page): Promise<number> {
	const box = await page.getByTestId("details-panel").boundingBox();
	return Math.round(box?.width ?? 0);
}

async function dragPanelEdge(page: Page, dx: number) {
	const box = await page.getByTestId("details-panel-resize").boundingBox();
	if (!box) throw new Error("no resize handle");
	const x = box.x + box.width / 2;
	const y = box.y + box.height / 2;
	await page.mouse.move(x, y);
	await page.mouse.down();
	await page.mouse.move(x + dx / 2, y);
	await page.mouse.move(x + dx, y);
	await page.mouse.up();
}

test.describe("the run details drawer on desktop", () => {
	test.use({ viewport: { width: 1280, height: 800 } });

	test("drags wider and narrower within its bounds", async ({ page }) => {
		await mockTasks(page);
		await page.goto("/agents/assistant/tasks");
		await openRunDetails(page);

		const handle = page.getByTestId("details-panel-resize");
		await expect(handle).toHaveAttribute("role", "separator");
		await expect(handle).toHaveAttribute("aria-orientation", "vertical");
		await expect(handle).toHaveAttribute("aria-label", "Resize details panel");
		await expect(handle).toHaveCSS("cursor", "col-resize");
		expect(await panelWidth(page)).toBe(384);

		await dragPanelEdge(page, -200);
		await expect.poll(() => panelWidth(page)).toBe(584);

		// No wider than 75% of the viewport, no narrower than the default.
		await dragPanelEdge(page, -1000);
		await expect.poll(() => panelWidth(page)).toBe(960);
		await dragPanelEdge(page, 1000);
		await expect.poll(() => panelWidth(page)).toBe(384);

		// The arrow keys move the edge too; Home and End jump to the bounds.
		await handle.focus();
		await page.keyboard.press("ArrowLeft");
		await expect.poll(() => panelWidth(page)).toBe(400);
		await page.keyboard.press("End");
		await expect.poll(() => panelWidth(page)).toBe(960);
		await page.keyboard.press("Home");
		await expect.poll(() => panelWidth(page)).toBe(384);
	});

	test("remembers its width when reopened and after a reload", async ({
		page,
	}) => {
		await mockTasks(page);
		await page.goto("/agents/assistant/tasks");
		await openRunDetails(page);
		await dragPanelEdge(page, -300);
		await expect.poll(() => panelWidth(page)).toBe(684);

		await page.getByTestId("details-panel").getByText("✕").click();
		await expect(page.getByTestId("details-panel")).toHaveCount(0);
		await page
			.locator("tbody tr")
			.filter({ hasText: "weekly-cleanup" })
			.first()
			.click();
		await expect.poll(() => panelWidth(page)).toBe(684);

		await page.reload();
		await openRunDetails(page);
		await expect.poll(() => panelWidth(page)).toBe(684);
	});

	test("clamps a stored width wider than the viewport allows", async ({
		page,
	}) => {
		await page.addInitScript(() =>
			localStorage.setItem("aviary.detailsPanel.width", "5000"),
		);
		await mockTasks(page);
		await page.goto("/agents/assistant/tasks");
		await openRunDetails(page);
		await expect.poll(() => panelWidth(page)).toBe(960);
		const overflow = await page.evaluate(
			() =>
				document.documentElement.scrollWidth -
				document.documentElement.clientWidth,
		);
		expect(overflow).toBeLessThanOrEqual(0);
	});
});

test.describe("the run details drawer on a narrow viewport", () => {
	test.use({ viewport: { width: 375, height: 740 } });

	test("keeps its full-width layout with no resize handle", async ({
		page,
	}) => {
		await page.addInitScript(() =>
			localStorage.setItem("aviary.detailsPanel.width", "700"),
		);
		await mockTasks(page);
		await page.goto("/agents/assistant/tasks");
		await openRunDetails(page);
		await expect(page.getByTestId("details-panel-resize")).toHaveCount(0);
		expect(await panelWidth(page)).toBe(375);
	});
});
