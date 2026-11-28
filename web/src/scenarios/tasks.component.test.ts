import { afterEach, describe, expect, test, vi } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { currentURL, goto, reload, renderApp } from "../component-test/app";
import { mockMCP } from "../component-test/server";
import { clickCardAt } from "./helpers/cards";
import {
	dragPanelEdge,
	job,
	mockTasks,
	openRunDetails,
	panelWidth,
} from "./tasks-fixtures";
// The pages this file renders (see component-test/routes.ts).
import "../views/SettingsView.vue";
import "../views/TaskView.vue";

const count = (loc: Locator) => () => loc.elements().length;

const ariaLabels = (loc: Locator) =>
	loc.elements().map((e) => e.getAttribute("aria-label"));

test("the Scheduled Tasks root page has no Reload or Save Changes", async () => {
	mockTasks();
	await renderApp("/agents/assistant/tasks");

	await expect
		.element(page.getByTestId("settings-title"))
		.toHaveText("Scheduled Tasks");
	await expect.element(page.getByTestId("tasks-overview")).toBeVisible();
	await expect
		.poll(count(page.getByRole("button", { name: "Reload" })))
		.toBe(0);
	await expect
		.poll(count(page.getByRole("button", { name: "Save Changes" })))
		.toBe(0);
	await expect.element(page.getByTestId("new-task-button")).toBeVisible();
});

test("a task's editor keeps its own Save Changes", async () => {
	mockTasks();
	await renderApp("/agents/assistant/tasks?task=daily-report");

	await expect
		.element(page.getByRole("button", { name: "Save Changes" }))
		.toBeVisible();
	await expect.poll(count(page.getByTestId("tasks-overview"))).toBe(0);
});

test("the summary stat cards and graphs render with data", async () => {
	mockTasks();
	await renderApp("/agents/assistant/tasks");

	const runs = page.getByTestId("stat-runs");
	await expect.element(runs).toMatchTextContent("4");
	await expect.element(runs).toMatchTextContent("2 ok");
	await expect.element(runs).toMatchTextContent("1 failed");
	await expect
		.poll(count(page.getByTestId("runs-sparkline").locator(".bg-green-500")))
		.not.toBe(0);
	await expect
		.element(page.getByTestId("stat-success-rate"))
		.toMatchTextContent("67%");
	await expect
		.element(page.getByTestId("stat-duration"))
		.toMatchTextContent("2.7s");
	await expect
		.element(page.getByTestId("stat-duration"))
		.toMatchTextContent("median 3.0s");
	// daily-report once and ci-watch twice; the paused task doesn't count.
	await expect
		.element(page.getByTestId("stat-upcoming"))
		.toMatchTextContent("3");
	await expect
		.element(page.getByTestId("stat-running"))
		.toMatchTextContent("1");
	await expect
		.element(page.getByTestId("stat-failures"))
		.toMatchTextContent("1");
	await expect
		.element(page.getByTestId("stat-failures"))
		.toMatchTextContent("last weekly-cleanup");

	await page.getByTestId("more-analytics-toggle").click();
	await expect.element(page.getByTestId("jobs-by-day")).toBeVisible();
	await expect.element(page.getByText("task_compile_1")).toBeVisible();
	await expect.element(page.getByTestId("job-count")).toHaveText("4 jobs");
});

test("more analytics shows the job queue above compile attempts", async () => {
	mockTasks();
	await renderApp("/agents/assistant/tasks");
	await page.getByTestId("more-analytics-toggle").click();
	await expect.element(page.getByText("task_compile_1")).toBeVisible();

	const queue = page.getByTestId("job-queue").element().getBoundingClientRect();
	const compiles = page
		.getByTestId("compile-attempts")
		.element()
		.getBoundingClientRect();
	expect(queue.y).toBeLessThan(compiles.y);
});

test("tasks show as cards by default with every field", async () => {
	mockTasks();
	await renderApp("/agents/assistant/tasks");

	const cards = page.getByTestId("task-card");
	await expect.poll(count(cards)).toBe(3);
	await expect.poll(count(page.getByTestId("task-list"))).toBe(0);

	const daily = cards.filter({ hasText: "daily-report" });
	await expect
		.element(daily.getByTestId("task-enabled"))
		.toHaveAttribute("aria-checked", "true");
	await expect
		.element(daily.getByTestId("task-owner"))
		.toHaveText("agent assistant");
	await expect
		.element(daily.getByTestId("task-schedule"))
		.toHaveText(/^daily at 02:00 \(America\/Toronto\) · next in 4\dm$/);
	await expect
		.element(daily.getByTestId("task-last-run"))
		.toHaveText("Succeeded 3h ago");
	// Run now and Pause first, the gear to the task's settings last; no
	// pencil (#1165).
	const actions = daily
		.getByRole("group", { name: "daily-report actions" })
		.getByRole("button");
	await expect.element(actions.first()).toBeVisible();
	expect(ariaLabels(actions)).toEqual([
		"Run now",
		"Pause",
		"View runs",
		"Duplicate…",
		"Task settings",
	]);
	await expect
		.element(actions.last())
		.toHaveAttribute("title", "Task settings");

	const weekly = cards.filter({ hasText: "weekly-cleanup" });
	await expect
		.element(weekly.getByTestId("task-schedule"))
		.toHaveText("weekly on Mon at 09:00 (UTC) · paused");
	await expect
		.element(weekly.getByTestId("task-last-run"))
		.toHaveText("Failed 2h ago");
	await expect
		.element(weekly.getByRole("button", { name: "Resume" }))
		.toBeVisible();

	const ci = cards.filter({ hasText: "ci-watch" });
	await expect.element(ci.getByTestId("task-owner")).toHaveText("project o/r");
	await expect.element(ci.getByTestId("task-running")).toBeVisible();
	await expect
		.element(ci.getByTestId("task-last-run"))
		.toHaveText("Running now");

	// The filter chips narrow the cards by state.
	await page
		.getByRole("group", { name: "Task filter" })
		.getByRole("button", { name: /Paused/ })
		.click();
	await expect.poll(count(cards)).toBe(1);
	await expect.element(cards).toMatchTextContent("weekly-cleanup");
});

test("the list toggle switches the layout and survives a reload", async () => {
	mockTasks();
	await renderApp("/agents/assistant/tasks");
	await expect.poll(count(page.getByTestId("task-card"))).toBe(3);

	await page.getByTestId("layout-list").click();
	await expect.element(page.getByTestId("task-list")).toBeVisible();
	await expect.poll(count(page.getByTestId("task-row-item"))).toBe(3);
	await expect.poll(count(page.getByTestId("task-card"))).toBe(0);
	await expect
		.element(
			page.getByTestId("task-row-item").filter({ hasText: "daily-report" }),
		)
		.toMatchTextContent("daily at 02:00 (America/Toronto)");
	// The list rows have the same actions: no pencil, the gear last (#1165).
	const rowActions = page
		.getByRole("group", { name: "daily-report actions" })
		.getByRole("button");
	expect(ariaLabels(rowActions)).toEqual([
		"Run now",
		"Pause",
		"View runs",
		"Duplicate…",
		"Task settings",
	]);
	await rowActions.last().click();
	await expect
		.element(page.getByRole("dialog", { name: "Edit task" }))
		.toBeVisible();
	await commands.keyPress("Escape");

	await reload();
	await expect.poll(count(page.getByTestId("task-row-item"))).toBe(3);
	await expect.poll(count(page.getByTestId("task-card"))).toBe(0);

	await page.getByTestId("layout-cards").click();
	await expect.poll(count(page.getByTestId("task-card"))).toBe(3);
});

test("card quick actions work", async () => {
	const calls = mockTasks();
	await renderApp("/agents/assistant/tasks");
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
	await expect
		.element(daily.getByTestId("task-schedule"))
		.toMatchTextContent("· paused");
	await expect
		.element(daily.getByRole("button", { name: "Resume" }))
		.toBeVisible();

	await daily.getByTestId("task-enabled").click();
	await expect
		.poll(() => calls.updates.at(-1))
		.toEqual({ name: "assistant/daily-report", enabled: true });

	await daily.getByRole("button", { name: "View runs" }).click();
	await expect
		.element(page.getByTestId("job-task-filter"))
		.toHaveText(/assistant\/daily-report/);
	await expect.element(page.getByTestId("job-count")).toHaveText("2 jobs");

	// None of the controls opens the card's task page.
	await expect.poll(currentURL).toMatch(/\/agents\/assistant\/tasks$/);

	// The gear opens the task's settings editor, without leaving the page.
	await daily.getByRole("button", { name: "Task settings" }).click();
	await expect
		.element(page.getByRole("dialog", { name: "Edit task" }))
		.toBeVisible();
	await commands.keyPress("Escape");
	await expect.poll(currentURL).toMatch(/\/agents\/assistant\/tasks$/);
});

test("a task card opens the task's page, like its sidebar row (#950)", async () => {
	mockTasks();
	await renderApp("/agents/assistant/tasks");
	const daily = page
		.getByTestId("task-card")
		.filter({ hasText: "daily-report" });
	// A real link, to the route the sidebar's task row uses.
	const link = daily.getByTestId("task-card-link");
	await expect
		.element(link)
		.toHaveAttribute("href", "/agents/assistant/tasks/daily-report");
	await expect.element(daily).toHaveCSS("cursor", "pointer");

	// Cmd/Ctrl-click opens it in a new tab and leaves this one alone. A new
	// tab can't be observed from inside the page, so this checks what makes
	// the browser open one: a modified click on the link that nothing
	// cancels. The listener runs last, then cancels the click itself so no
	// tab opens.
	const newTabClicks: string[] = [];
	const onClick = (e: MouseEvent) => {
		const a = (e.target as Element).closest("a");
		if (a && (e.metaKey || e.ctrlKey) && !e.defaultPrevented)
			newTabClicks.push(a.getAttribute("href") ?? "");
		e.preventDefault();
	};
	window.addEventListener("click", onClick);
	try {
		await clickCardAt(daily, daily.getByTestId("task-schedule"), [
			"ControlOrMeta",
		]);
		await expect
			.poll(() => newTabClicks)
			.toEqual(["/agents/assistant/tasks/daily-report"]);
	} finally {
		window.removeEventListener("click", onClick);
	}
	await expect.poll(currentURL).toMatch(/\/agents\/assistant\/tasks$/);

	// Anywhere on the card body opens the task's page.
	await clickCardAt(daily, daily.getByTestId("task-owner"));
	await expect
		.poll(currentURL)
		.toMatch(/\/agents\/assistant\/tasks\/daily-report$/);
	await expect
		.element(page.getByTestId("settings-title"))
		.toHaveText("daily-report");

	// The card is focusable and Enter opens it.
	await goto("/agents/assistant/tasks");
	await expect.element(link).toBeVisible();
	(link.element() as HTMLElement).focus();
	await expect.element(link).toHaveFocus();
	await commands.keyPress("Enter");
	await expect
		.poll(currentURL)
		.toMatch(/\/agents\/assistant\/tasks\/daily-report$/);

	// Running now opens the running task's page.
	await goto("/agents/assistant/tasks");
	await page
		.getByTestId("task-card")
		.filter({ hasText: "ci-watch" })
		.getByTestId("task-running-link")
		.click();
	await expect
		.poll(currentURL)
		.toMatch(/\/agents\/assistant\/tasks\/ci-watch$/);
});

test("clicking a list row opens the task's page", async () => {
	mockTasks();
	await renderApp("/agents/assistant/tasks");
	await page.getByTestId("layout-list").click();
	await page
		.getByTestId("task-row-item")
		.filter({ hasText: "weekly-cleanup" })
		.locator("td")
		.first()
		.click();
	await expect
		.poll(currentURL)
		.toMatch(/\/agents\/assistant\/tasks\/weekly-cleanup$/);
});

test("System → Jobs redirects to Scheduled Tasks and has no nav entry", async () => {
	mockTasks();
	await renderApp("/system/jobs");

	await expect.poll(currentURL).toMatch(/\/agents\/assistant\/tasks$/);
	await expect
		.element(page.getByTestId("settings-title"))
		.toHaveText("Scheduled Tasks");
	await expect.poll(count(page.locator('a[href="/system/jobs"]'))).toBe(0);
});

describe("with a fake clock", () => {
	afterEach(() => {
		vi.useRealTimers();
	});

	test("an open running job leaves Running and shows why it failed", async () => {
		// The overview refreshes jobs on a 15s interval; fake only that and
		// the clock, so the app's timeouts and the matchers' polling run.
		vi.useFakeTimers({ toFake: ["setInterval", "clearInterval", "Date"] });
		const running = job(7, "daily-report", "in_progress", 60_000);
		let current: Record<string, unknown> = running;
		mockMCP({
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
		await renderApp("/agents/assistant/tasks");
		await page.getByTestId("more-analytics-toggle").click();
		await page
			.getByTestId("job-queue")
			.getByText("assistant/daily-report")
			.click();

		await expect.element(page.getByText("Waiting for output…")).toBeVisible();
		await expect
			.element(
				page.getByText(
					"A run with no output for 15m is stopped and marked failed.",
				),
			)
			.toBeVisible();

		current = {
			...running,
			status: "failed",
			error: "run timed out: no output received for 15m0s",
		};
		await vi.advanceTimersByTimeAsync(16_000);

		await expect
			.element(page.getByTestId("job-error"))
			.toMatchTextContent("run timed out: no output received for 15m0s");
		await expect
			.element(page.getByText("Waiting for output…"))
			.not.toBeInTheDocument();
	});
});

describe("the run details drawer on desktop", () => {
	test("drags wider and narrower within its bounds", async () => {
		await page.viewport(1280, 800);
		mockTasks();
		await renderApp("/agents/assistant/tasks");
		await openRunDetails();

		const handle = page.getByTestId("details-panel-resize");
		await expect.element(handle).toHaveAttribute("role", "separator");
		await expect
			.element(handle)
			.toHaveAttribute("aria-orientation", "vertical");
		await expect
			.element(handle)
			.toHaveAttribute("aria-label", "Resize details panel");
		await expect.element(handle).toHaveCSS("cursor", "col-resize");
		expect(panelWidth()).toBe(384);

		await dragPanelEdge(-200);
		await expect.poll(panelWidth).toBe(584);

		// No wider than 75% of the viewport, no narrower than the default.
		await dragPanelEdge(-1000);
		await expect.poll(panelWidth).toBe(960);
		await dragPanelEdge(1000);
		await expect.poll(panelWidth).toBe(384);

		// The arrow keys move the edge too; Home and End jump to the bounds.
		(handle.element() as HTMLElement).focus();
		await commands.keyPress("ArrowLeft");
		await expect.poll(panelWidth).toBe(400);
		await commands.keyPress("End");
		await expect.poll(panelWidth).toBe(960);
		await commands.keyPress("Home");
		await expect.poll(panelWidth).toBe(384);
	});

	test("remembers its width when reopened and after a reload", async () => {
		await page.viewport(1280, 800);
		mockTasks();
		await renderApp("/agents/assistant/tasks");
		await openRunDetails();
		await dragPanelEdge(-300);
		await expect.poll(panelWidth).toBe(684);

		await page.getByTestId("details-panel").getByText("✕").click();
		await expect.poll(count(page.getByTestId("details-panel"))).toBe(0);
		await page
			.locator("tbody tr")
			.filter({ hasText: "weekly-cleanup" })
			.first()
			.click();
		await expect.poll(panelWidth).toBe(684);

		// reload() keeps the drawer's module, which read the width when it
		// loaded; tasks-details-panel-clamp covers that read, so check here
		// that the width is stored for it.
		expect(localStorage.getItem("aviary.detailsPanel.width")).toBe("684");
		await reload();
		await openRunDetails();
		await expect.poll(panelWidth).toBe(684);
	});
});
