import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

// The breadcrumb leaf on a project, task, channel or file page switches
// between its siblings, like GitHub's repository switcher (#1063).

const LONG_PROJECT = "lsegal/a-repository-with-a-very-long-name-indeed";
const BOARD = "https://github.com/users/lsegal/projects/3";

const states = { starting: 0, paused: 0, running: 0, done: 0, failed: 0 };

async function mockAviary(page: Page) {
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: [{ id: "Aviary", name: "Aviary", model: "x/y", state: "idle" }],
		config_get: {
			agents: [
				{
					name: "Aviary",
					model: "x/y",
					channels: [
						{ type: "signal", id: "signal", deliver_to: "+1" },
						{ type: "slack", id: "team", name: "team", enabled: false },
					],
					tasks: [
						{ name: "release-aviary", every: "1h", prompt: "release" },
						{ name: "nightly", every: "24h", prompt: "n", enabled: false },
						{
							name: "triage",
							trigger: "message",
							channel: "signal",
							prompt: "t",
						},
					],
					projects: [
						{ target: "o/r" },
						{ target: LONG_PROJECT },
						{ target: BOARD },
					],
				},
			],
		},
		config_validate: [],
		auth_list: [],
		tool_list: [],
		job_list: [],
		job_query: [],
		task_list: [],
		session_list: [],
		session_messages: [],
		channel_sessions: [],
		agent_file_list: ["AGENTS.md", "MEMORY.md", "notes.md"],
		agent_file_read: "# hello",
		project_status: { states, targets: ["o/r", LONG_PROJECT, BOARD] },
		project_job_list: { jobs: [], total: 0, offset: 0, limit: 50 },
	});
}

function trigger(page: Page) {
	return page
		.getByTestId("page-breadcrumb")
		.getByTestId("crumb-switcher-trigger");
}

function options(page: Page) {
	return page.getByTestId("crumb-switcher").getByRole("option");
}

test.beforeEach(async ({ page }) => {
	await mockAviary(page);
});

test("a project leaf opens a focused, filterable switcher of the agent's projects", async ({
	page,
}) => {
	await page.goto("/agents/Aviary/projects/o%2Fr?state=running");
	const button = trigger(page);
	await expect(button).toHaveAttribute("aria-haspopup", "listbox");
	await expect(button).toHaveAttribute("aria-expanded", "false");
	await expect(button).toHaveAttribute(
		"title",
		/^Switch project \((Alt\+|⌥)S\)$/,
	);
	await button.click();
	await expect(button).toHaveAttribute("aria-expanded", "true");

	const popover = page.getByTestId("crumb-switcher");
	await expect(popover.getByTestId("crumb-switcher-title")).toHaveText(
		"Switch project",
	);
	const search = popover.getByTestId("crumb-switcher-search");
	await expect(search).toBeFocused();
	await expect(popover.getByRole("listbox")).toBeVisible();
	await expect(options(page)).toHaveText(["o/r", LONG_PROJECT, BOARD]);
	// The current project is checkmarked and starts selected.
	await expect(options(page).nth(0)).toHaveAttribute("data-current", "true");
	await expect(options(page).nth(0)).toHaveAttribute("aria-selected", "true");
	await expect(
		popover.locator('[data-current="true"]').getByLabel("Current"),
	).toBeVisible();
	const icons = popover.getByTestId("crumb-switcher-icon");
	await expect(icons.nth(0)).toHaveAttribute("data-icon", "repo");
	await expect(icons.nth(2)).toHaveAttribute("data-icon", "board");

	await search.fill("LONG name");
	await expect(options(page)).toHaveText([LONG_PROJECT]);
	await search.fill("nothing-like-it");
	await expect(options(page)).toHaveCount(0);
	await expect(popover.getByTestId("crumb-switcher-empty")).toBeVisible();
	await search.fill("long");
	// Enter opens the sibling's board with the same filters.
	await page.keyboard.press("Enter");
	await expect(page).toHaveURL(
		`/agents/Aviary/projects/${encodeURIComponent(LONG_PROJECT)}?state=running`,
	);
	await expect(popover).toHaveCount(0);
	await expect(page.getByTestId("page-title")).toHaveText(LONG_PROJECT);
});

test("a job page's leaf is the job, so it has no switcher", async ({
	page,
}) => {
	await page.goto("/agents/Aviary/projects/o%2Fr/jobs/5");
	await expect(trigger(page)).toHaveCount(0);
});

test("the shortcut opens the task switcher; arrows and Enter go to a sibling", async ({
	page,
}) => {
	await page.goto("/agents/Aviary/tasks/release-aviary");
	await expect(trigger(page)).toHaveAttribute("aria-keyshortcuts", "Alt+S");
	await page.keyboard.press("Alt+KeyS");
	const popover = page.getByTestId("crumb-switcher");
	await expect(popover.getByTestId("crumb-switcher-title")).toHaveText(
		"Switch task",
	);
	await expect(popover.getByTestId("crumb-switcher-search")).toBeFocused();
	await expect(options(page)).toHaveText([
		"release-aviary",
		"nightlyPaused",
		"triage",
	]);
	const icons = popover.getByTestId("crumb-switcher-icon");
	await expect(icons.nth(0)).toHaveAttribute("data-icon", "task");
	await expect(icons.nth(2)).toHaveAttribute("data-icon", "workflow");

	await page.keyboard.press("ArrowDown");
	await page.keyboard.press("ArrowDown");
	await expect(options(page).nth(2)).toHaveAttribute("aria-selected", "true");
	// ArrowDown wraps from the last row to the first, ArrowUp back.
	await page.keyboard.press("ArrowDown");
	await expect(options(page).nth(0)).toHaveAttribute("aria-selected", "true");
	await page.keyboard.press("ArrowUp");
	await page.keyboard.press("ArrowUp");
	await expect(options(page).nth(1)).toHaveAttribute("aria-selected", "true");
	await page.keyboard.press("Enter");
	await expect(page).toHaveURL("/agents/Aviary/tasks/nightly");
});

test("a run page's leaf is the run, so it has no switcher", async ({
	page,
}) => {
	await page.goto("/agents/Aviary/tasks/release-aviary/runs/job_1");
	await expect(trigger(page)).toHaveCount(0);
});

test("Escape closes the channel switcher and returns focus to the leaf", async ({
	page,
}) => {
	await page.goto("/agents/Aviary/channels/signal");
	await trigger(page).click();
	await expect(options(page)).toHaveText(["signal", "teamDisabled"]);
	const icons = page.getByTestId("crumb-switcher-icon");
	await expect(icons.nth(0)).toHaveAttribute("data-icon", "signal");
	await expect(icons.nth(1)).toHaveAttribute("data-icon", "slack");
	await page.keyboard.press("Escape");
	await expect(page.getByTestId("crumb-switcher")).toHaveCount(0);
	await expect(trigger(page)).toBeFocused();
	// Clicking a sibling opens its conversation.
	await trigger(page).click();
	await options(page).filter({ hasText: "team" }).click();
	await expect(page).toHaveURL("/agents/Aviary/channels/team");
});

test("the Files page's leaf switches between files, marking built-in ones", async ({
	page,
}) => {
	await page.goto("/agents/Aviary/files?file=AGENTS.md");
	await expect(
		page.getByTestId("page-breadcrumb").getByRole("listitem"),
	).toHaveText(["Aviary", "Files", "AGENTS.md"]);
	await trigger(page).click();
	await expect(page.getByTestId("crumb-switcher-title")).toHaveText(
		"Switch file",
	);
	await expect(options(page)).toHaveText([
		"AGENTS.mdBuilt-in",
		"MEMORY.mdBuilt-in",
		"notes.md",
	]);
	const icons = page.getByTestId("crumb-switcher-icon");
	await expect(icons.nth(0)).toHaveAttribute("data-icon", "builtin");
	await expect(icons.nth(2)).toHaveAttribute("data-icon", "file");
	await page.getByTestId("crumb-switcher-search").fill("notes");
	await page.keyboard.press("Enter");
	await expect(page).toHaveURL("/agents/Aviary/files?file=notes.md");
	await expect(page.getByTestId("page-title")).toHaveText("notes.md");
});

test("the switcher fits a phone screen and truncates long names", async ({
	page,
}) => {
	await page.setViewportSize({ width: 360, height: 700 });
	await page.goto(
		`/agents/Aviary/projects/${encodeURIComponent(LONG_PROJECT)}`,
	);
	await trigger(page).click();
	const popover = page.getByTestId("crumb-switcher");
	const box = await popover.boundingBox();
	expect(box?.x).toBeGreaterThanOrEqual(0);
	expect((box?.x ?? 0) + (box?.width ?? 0)).toBeLessThanOrEqual(360);
	const label = options(page).nth(1).locator(".truncate");
	const overflow = await label.evaluate((el) => ({
		scroll: el.scrollWidth,
		client: el.clientWidth,
		ellipsis: getComputedStyle(el).textOverflow,
	}));
	expect(overflow.scroll).toBeGreaterThan(overflow.client);
	expect(overflow.ellipsis).toBe("ellipsis");
});

test("the switcher works with the sidebar collapsed", async ({ page }) => {
	await page.addInitScript(() =>
		localStorage.setItem("aviary.sidebar.collapsed", "1"),
	);
	await page.goto("/agents/Aviary/tasks/release-aviary");
	await trigger(page).click();
	await options(page).filter({ hasText: "triage" }).click();
	await expect(page).toHaveURL("/agents/Aviary/tasks/triage");
});

test("an aviary watch session switches between its projects", async ({
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
			agents: [
				{
					name: "watch",
					projects: [{ target: "owner/repo" }, { target: "owner/other" }],
				},
			],
		},
		project_status: { states, targets: ["owner/repo", "owner/other"] },
		project_job_list: { jobs: [], total: 0, offset: 0, limit: 50 },
	});
	await page.goto("/agents/watch/projects/owner%2Frepo");
	await trigger(page).click();
	await expect(options(page)).toHaveText(["owner/repo", "owner/other"]);
	await options(page).filter({ hasText: "owner/other" }).click();
	await expect(page).toHaveURL("/agents/watch/projects/owner%2Fother");
});
