import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

// Paused scheduled tasks: one amber Paused chip, paused sidebar rows, and
// Pause/Resume in the sidebar's "…" menu (#975). The other scenarios moved
// to web/src/scenarios/paused-tasks.component.test.ts (#1636); this one stays
// because it needs a touch browser context (hasTouch, isMobile), where
// (hover: none) matches, which a component test's page cannot switch to.

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

test("on a touch screen a paused row shows its menu beside the icon", async ({
	browser,
}) => {
	const context = await browser.newContext({
		viewport: { width: 1280, height: 800 },
		hasTouch: true,
		isMobile: true,
	});
	const page = await context.newPage();
	await mockPausable(page);
	await page.goto("/agents/assistant/tasks");
	expect(await page.evaluate(() => matchMedia("(hover: none)").matches)).toBe(
		true,
	);

	const row = sidebarRow(page, "nightly");
	const button = row.getByTestId("task-menu-button");
	await expect(button).toBeVisible();
	const icon = await row.getByTestId("task-row-paused").boundingBox();
	const box = await button.boundingBox();
	if (!icon || !box) throw new Error("row not laid out");
	expect(box.x).toBeGreaterThanOrEqual(icon.x + icon.width);
	await context.close();
});
