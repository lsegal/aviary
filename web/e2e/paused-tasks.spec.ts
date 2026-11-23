import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

// Paused scheduled tasks: one amber Paused chip, paused sidebar rows, and
// Pause/Resume in the sidebar's "…" menu (#975).

interface Task {
	name: string;
	enabled?: boolean;
	[key: string]: unknown;
}

/**
 * Mocks one agent with an active config task and a paused file task.
 * task_update keeps the config current, or fails while `fail` is set.
 */
async function mockPausable(page: Page) {
	const tasks: Task[] = [
		{
			name: "nightly",
			enabled: false,
			from_file: true,
			file: "tasks/nightly.md",
			every: "5m",
			prompt: "tidy up",
		},
		{ name: "daily-report", every: "1h", prompt: "write the report" },
	];
	const state = { fail: false, updates: [] as Record<string, unknown>[] };
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: [
			{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
		],
		config_get: () => ({
			agents: [{ name: "assistant", model: "x/y", tasks }],
		}),
		config_validate: [],
		auth_list: [],
		tool_list: [],
		session_list: [],
		job_query: [],
		job_list: [],
		task_compile_query: [],
		task_preview: () => ({ description: "every 5m0s (UTC)", next: [] }),
		task_update: (args?: Record<string, unknown>) => {
			state.updates.push(args ?? {});
			if (state.fail) throw new Error("disk is read-only");
			const task = tasks.find((t) => `assistant/${t.name}` === args?.name);
			if (task && typeof args?.enabled === "boolean")
				task.enabled = args.enabled;
			return "updated";
		},
	});
	return state;
}

const sidebarRow = (page: Page, name: string) =>
	page
		.getByTestId("sidebar")
		.locator(`[data-testid=task-row][data-task="${name}"]`);

async function openRowMenu(page: Page, name: string) {
	const row = sidebarRow(page, name);
	await row.hover();
	await row.getByTestId("task-menu-button").click();
}

test("a paused task shows the amber Paused chip on its card, page and editor", async ({
	page,
}) => {
	await mockPausable(page);
	await page.goto("/agents/assistant/tasks");

	const card = page.locator('[data-testid=task-card][data-task="nightly"]');
	const chip = card.getByTestId("paused-chip");
	await expect(chip).toHaveText("Paused");
	await expect(chip.locator("svg")).toBeVisible();
	await expect(chip).toHaveClass(/bg-amber-50/);
	await expect(
		page
			.locator('[data-testid=task-card][data-task="daily-report"]')
			.getByTestId("paused-chip"),
	).toHaveCount(0);

	await page.goto("/agents/assistant/tasks?task=nightly");
	await expect(page.getByText("Defined in: tasks/nightly.md")).toBeVisible();
	await expect(page.getByTestId("paused-chip")).toHaveText("Paused");
	await expect(page.getByTestId("paused-chip")).not.toHaveClass(/red/);
	await expect(page.getByText("disabled", { exact: true })).toHaveCount(0);

	await page.goto("/agents/assistant/tasks/nightly");
	const state = page.getByTestId("task-state");
	await expect(state).toHaveText("Paused");
	await expect(state).toHaveClass(/bg-amber-50/);
	await expect(state.locator("svg")).toBeVisible();
});

test("a paused task's sidebar row is muted, marked and sorted last", async ({
	page,
}) => {
	await mockPausable(page);
	await page.goto("/agents/assistant/tasks");

	const rows = page.getByTestId("sidebar").getByTestId("task-row");
	await expect(rows).toHaveCount(2);
	await expect(rows.nth(0)).toHaveAttribute("data-task", "daily-report");
	await expect(rows.nth(1)).toHaveAttribute("data-task", "nightly");

	const paused = sidebarRow(page, "nightly");
	await expect(paused).toHaveAttribute("data-paused", "true");
	await expect(paused.getByTestId("task-row-paused")).toBeVisible();
	const link = paused.getByTestId("sidebar-item-link");
	await expect(link).toHaveAttribute("title", "Paused");
	await expect(link).toHaveClass(/italic/);

	const active = sidebarRow(page, "daily-report");
	await expect(active.getByTestId("task-row-paused")).toHaveCount(0);
	await expect(active.getByTestId("sidebar-item-link")).toHaveAttribute(
		"title",
		"daily-report",
	);
	await expect(active.getByTestId("sidebar-item-link")).not.toHaveClass(
		/italic/,
	);
});

test("the mobile nav marks paused tasks too", async ({ page }) => {
	await mockPausable(page);
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto("/agents/assistant/tasks");

	const pill = page.locator('a[data-paused="true"]');
	await expect(pill).toHaveCount(1);
	await expect(pill).toHaveText("nightly");
	await expect(pill).toHaveAttribute("title", "Paused");
	await expect(pill.getByTestId("mobile-task-paused")).toBeVisible();
});

test("the sidebar menu pauses and resumes a task without navigating", async ({
	page,
}) => {
	const state = await mockPausable(page);
	await page.goto("/agents/assistant/tasks?task=daily-report");
	const url = page.url();
	await expect(page.getByTestId("paused-chip")).toHaveCount(0);

	await openRowMenu(page, "nightly");
	await expect(page.getByRole("menuitem")).toHaveText([
		"Resume",
		"Settings",
		"Delete",
	]);
	await page.keyboard.press("Escape");

	await openRowMenu(page, "daily-report");
	await expect(page.getByRole("menuitem")).toHaveText([
		"Pause",
		"Settings",
		"Delete",
	]);
	await page.getByRole("menuitem", { name: "Pause" }).click();

	// The sidebar, and the open editor, show it paused; the page stays put.
	await expect(sidebarRow(page, "daily-report")).toHaveAttribute(
		"data-paused",
		"true",
	);
	await expect(page.getByTestId("paused-chip")).toBeVisible();
	await expect(
		page.getByRole("switch", { name: "Toggle task enabled" }),
	).toHaveAttribute("aria-checked", "false");
	expect(page.url()).toBe(url);
	expect(state.updates).toEqual([
		{ name: "assistant/daily-report", enabled: false },
	]);

	// The file-defined task resumes the same way.
	await openRowMenu(page, "nightly");
	await page.getByRole("menuitem", { name: "Resume" }).click();
	await expect(sidebarRow(page, "nightly")).not.toHaveAttribute(
		"data-paused",
		"true",
	);
	expect(state.updates.at(-1)).toEqual({
		name: "assistant/nightly",
		enabled: true,
	});

	// The overview's cards follow too.
	await page.goto("/agents/assistant/tasks");
	await expect(
		page
			.locator('[data-testid=task-card][data-task="daily-report"]')
			.getByTestId("paused-chip"),
	).toBeVisible();
	await openRowMenu(page, "daily-report");
	await page.getByRole("menuitem", { name: "Resume" }).click();
	await expect(
		page
			.locator('[data-testid=task-card][data-task="daily-report"]')
			.getByTestId("paused-chip"),
	).toHaveCount(0);
	await expect(page).toHaveURL(/\/agents\/assistant\/tasks$/);
});

test("pausing from the task's page updates the sidebar", async ({ page }) => {
	await mockPausable(page);
	await page.goto("/agents/assistant/tasks/daily-report");

	await page.getByTestId("task-toggle-enabled").click();
	await expect(page.getByTestId("task-state")).toHaveText("Paused");
	await expect(sidebarRow(page, "daily-report")).toHaveAttribute(
		"data-paused",
		"true",
	);
});

test("a failed toggle rolls back and shows an error toast", async ({
	page,
}) => {
	const state = await mockPausable(page);
	await page.goto("/agents/assistant/tasks");
	state.fail = true;

	await openRowMenu(page, "daily-report");
	await page.getByRole("menuitem", { name: "Pause" }).click();

	const toast = page.getByTestId("toast");
	await expect(toast).toContainText("Couldn't pause daily-report");
	await expect(toast).toContainText("disk is read-only");
	await expect(toast).toHaveAttribute("data-tone", "error");
	await expect(sidebarRow(page, "daily-report")).not.toHaveAttribute(
		"data-paused",
		"true",
	);
	await expect(
		page
			.locator('[data-testid=task-card][data-task="daily-report"]')
			.getByTestId("paused-chip"),
	).toHaveCount(0);
	await expect(
		page
			.locator('[data-testid=task-card][data-task="daily-report"]')
			.getByTestId("task-enabled"),
	).toHaveAttribute("aria-checked", "true");

	// The overview's switch rolls back the same way.
	await page
		.locator('[data-testid=task-card][data-task="nightly"]')
		.getByTestId("task-enabled")
		.click();
	await expect(toast.last()).toContainText("Couldn't resume nightly");
	await expect(
		page
			.locator('[data-testid=task-card][data-task="nightly"]')
			.getByTestId("paused-chip"),
	).toBeVisible();
});
