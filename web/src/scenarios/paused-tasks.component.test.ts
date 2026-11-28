import { expect, test } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { currentURL, goto, renderApp } from "../component-test/app";
import { mockMCP } from "../component-test/server";

// Paused scheduled tasks: one amber Paused chip, paused sidebar rows, and
// Pause/Resume in the sidebar's "…" menu (#975). The touch screen case stays
// in web/e2e/paused-tasks.spec.ts: it needs a touch browser context, where
// (hover: none) matches.

interface Task {
	name: string;
	enabled?: boolean;
	[key: string]: unknown;
}

/**
 * Mocks one agent with an active config task and a paused file task.
 * task_update keeps the config current, or fails while `fail` is set.
 */
function mockPausable() {
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
	mockMCP({
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

const count = (loc: Locator) => loc.elements().length;
const box = (loc: Locator) => loc.element().getBoundingClientRect();
const texts = (loc: Locator) =>
	loc
		.elements()
		.map((el) => (el.textContent ?? "").replace(/\s+/g, " ").trim());

const sidebarRow = (name: string) =>
	page
		.getByTestId("sidebar")
		.locator(`[data-testid=task-row][data-task="${name}"]`);

const card = (name: string) =>
	page.locator(`[data-testid=task-card][data-task="${name}"]`);

async function openRowMenu(name: string) {
	const row = sidebarRow(name);
	await row.hover();
	await row.getByTestId("task-menu-button").click();
}

test("a paused task shows the amber Paused chip on its card, page and editor", async () => {
	mockPausable();
	await renderApp("/agents/assistant/tasks");

	const chip = card("nightly").getByTestId("paused-chip");
	await expect.element(chip).toHaveText("Paused");
	await expect.element(chip.locator("svg")).toBeVisible();
	await expect.element(chip).toHaveClass(/bg-amber-50/);
	expect(count(card("daily-report").getByTestId("paused-chip"))).toBe(0);

	await goto("/agents/assistant/tasks?task=nightly");
	await expect
		.element(page.getByText("Defined in: tasks/nightly.md"))
		.toBeVisible();
	await expect.element(page.getByTestId("paused-chip")).toHaveText("Paused");
	await expect.element(page.getByTestId("paused-chip")).not.toHaveClass(/red/);
	expect(count(page.getByText("disabled", { exact: true }))).toBe(0);

	await goto("/agents/assistant/tasks/nightly");
	const state = page.getByTestId("task-state");
	await expect.element(state).toHaveText("Paused");
	await expect.element(state).toHaveClass(/bg-amber-50/);
	await expect.element(state.locator("svg")).toBeVisible();
});

test("a paused task's sidebar row is muted, marked and sorted last", async () => {
	mockPausable();
	await renderApp("/agents/assistant/tasks");

	const rows = page.getByTestId("sidebar").getByTestId("task-row");
	await expect.poll(() => count(rows)).toBe(2);
	await expect
		.element(rows.nth(0))
		.toHaveAttribute("data-task", "daily-report");
	await expect.element(rows.nth(1)).toHaveAttribute("data-task", "nightly");

	const paused = sidebarRow("nightly");
	await expect.element(paused).toHaveAttribute("data-paused", "true");
	await expect.element(paused.getByTestId("task-row-paused")).toBeVisible();
	const link = paused.getByTestId("sidebar-item-link");
	await expect.element(link).toHaveAttribute("title", "Paused");
	await expect.element(link).toHaveClass(/italic/);

	const active = sidebarRow("daily-report");
	expect(count(active.getByTestId("task-row-paused"))).toBe(0);
	await expect
		.element(active.getByTestId("sidebar-item-link"))
		.toHaveAttribute("title", "daily-report");
	await expect
		.element(active.getByTestId("sidebar-item-link"))
		.not.toHaveClass(/italic/);
});

test("a paused row's icon sits flush right until the menu button shows", async () => {
	mockPausable();
	await page.viewport(1280, 800);
	await renderApp("/agents/assistant/tasks");

	const row = sidebarRow("nightly");
	const icon = row.getByTestId("task-row-paused");
	const button = row.getByTestId("task-menu-button");
	const link = row.getByTestId("sidebar-item-link");
	await expect.element(icon).toBeVisible();
	await commands.mouseMove(1000, 700);

	// Hidden "…" takes no room, so the icon ends at the row's right padding (#985).
	await expect.element(button).not.toBeVisible();
	const rowBox = box(row);
	const restIcon = box(icon);
	const restLink = box(link);
	const gap = rowBox.x + rowBox.width - (restIcon.x + restIcon.width);
	expect(gap).toBeGreaterThanOrEqual(2);
	expect(gap).toBeLessThanOrEqual(6);

	// On hover, "…" shows right of the icon without moving the label.
	await row.hover();
	await expect.element(button).toBeVisible();
	const hoverIcon = box(icon);
	const hoverButton = box(button);
	const hoverLink = box(link);
	expect(hoverButton.x).toBeGreaterThanOrEqual(hoverIcon.x + hoverIcon.width);
	expect(hoverButton.x + hoverButton.width).toBeLessThanOrEqual(
		rowBox.x + rowBox.width,
	);
	expect(hoverLink.x).toBe(restLink.x);

	// Keyboard focus shows it too.
	await commands.mouseMove(1000, 700);
	(button.element() as HTMLElement).focus();
	await expect.element(button).toBeVisible();
	await expect.element(button).toHaveFocus();
});

test("a long paused task name truncates before its icon", async () => {
	mockPausable();
	await page.viewport(1280, 800);
	await renderApp("/agents/assistant/tasks");

	const row = sidebarRow("nightly");
	const link = row.getByTestId("sidebar-item-link");
	await expect.element(link).toBeVisible();
	link.element().textContent = "nightly-".repeat(20);
	await commands.mouseMove(1000, 700);
	const icon = box(row.getByTestId("task-row-paused"));
	const linkBox = box(link);
	expect(linkBox.x + linkBox.width).toBeLessThanOrEqual(icon.x);
	const el = link.element();
	expect(el.scrollWidth > el.clientWidth).toBe(true);
});

test("the mobile nav marks paused tasks too", async () => {
	mockPausable();
	await page.viewport(390, 844);
	await renderApp("/agents/assistant/tasks");

	const pill = page.locator('a[data-paused="true"]');
	await expect.poll(() => count(pill)).toBe(1);
	await expect.element(pill).toHaveText("nightly");
	await expect.element(pill).toHaveAttribute("title", "Paused");
	await expect.element(pill.getByTestId("mobile-task-paused")).toBeVisible();
});

test("the sidebar menu pauses and resumes a task without navigating", async () => {
	const state = mockPausable();
	await renderApp("/agents/assistant/tasks?task=daily-report");
	await expect
		.element(sidebarRow("daily-report").getByTestId("sidebar-item-link"))
		.toBeVisible();
	const url = currentURL();
	expect(count(page.getByTestId("paused-chip"))).toBe(0);

	await openRowMenu("nightly");
	await expect
		.poll(() => texts(page.getByRole("menuitem")))
		.toEqual(["Resume", "Settings", "Duplicate…", "Delete"]);
	await commands.keyPress("Escape");

	await openRowMenu("daily-report");
	await expect
		.poll(() => texts(page.getByRole("menuitem")))
		.toEqual(["Pause", "Settings", "Duplicate…", "Delete"]);
	await page.getByRole("menuitem", { name: "Pause" }).click();

	// The sidebar, and the open editor, show it paused; the page stays put.
	await expect
		.element(sidebarRow("daily-report"))
		.toHaveAttribute("data-paused", "true");
	await expect.element(page.getByTestId("paused-chip")).toBeVisible();
	await expect
		.element(page.getByRole("switch", { name: "Toggle task enabled" }))
		.toHaveAttribute("aria-checked", "false");
	expect(currentURL()).toBe(url);
	expect(state.updates).toEqual([
		{ name: "assistant/daily-report", enabled: false },
	]);

	// The file-defined task resumes the same way.
	await openRowMenu("nightly");
	await page.getByRole("menuitem", { name: "Resume" }).click();
	await expect
		.element(sidebarRow("nightly"))
		.not.toHaveAttribute("data-paused", "true");
	expect(state.updates.at(-1)).toEqual({
		name: "assistant/nightly",
		enabled: true,
	});

	// The overview's cards follow too.
	await goto("/agents/assistant/tasks");
	await expect
		.element(card("daily-report").getByTestId("paused-chip"))
		.toBeVisible();
	await openRowMenu("daily-report");
	await page.getByRole("menuitem", { name: "Resume" }).click();
	await expect
		.poll(() => count(card("daily-report").getByTestId("paused-chip")))
		.toBe(0);
	await expect.poll(currentURL).toMatch(/\/agents\/assistant\/tasks$/);
});

test("pausing from the task's page updates the sidebar", async () => {
	mockPausable();
	await renderApp("/agents/assistant/tasks/daily-report");

	await page.getByTestId("task-toggle-enabled").click();
	await expect.element(page.getByTestId("task-state")).toHaveText("Paused");
	await expect
		.element(sidebarRow("daily-report"))
		.toHaveAttribute("data-paused", "true");
});

test("a failed toggle rolls back and shows an error toast", async () => {
	const state = mockPausable();
	await renderApp("/agents/assistant/tasks");
	state.fail = true;

	await openRowMenu("daily-report");
	await page.getByRole("menuitem", { name: "Pause" }).click();

	const toast = page.getByTestId("toast");
	await expect.element(toast).toMatchTextContent("Couldn't pause daily-report");
	await expect.element(toast).toMatchTextContent("disk is read-only");
	await expect.element(toast).toHaveAttribute("data-tone", "error");
	await expect
		.element(sidebarRow("daily-report"))
		.not.toHaveAttribute("data-paused", "true");
	expect(count(card("daily-report").getByTestId("paused-chip"))).toBe(0);
	await expect
		.element(card("daily-report").getByTestId("task-enabled"))
		.toHaveAttribute("aria-checked", "true");

	// The overview's switch rolls back the same way.
	await card("nightly").getByTestId("task-enabled").click();
	await expect
		.element(toast.last())
		.toMatchTextContent("Couldn't resume nightly");
	await expect
		.element(card("nightly").getByTestId("paused-chip"))
		.toBeVisible();
});
