import { beforeEach, expect, test } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { currentURL, renderApp } from "../component-test/app";
import { declarePages } from "../component-test/routes";
import { mockMCP, route } from "../component-test/server";

// The pages this file renders (see component-test/routes.ts).
declarePages(
	() => import("../components/projects/ProjectJobDetail.vue"),
	() => import("../components/tasks/dashboard/TaskRunPage.vue"),
	() => import("../views/ChannelView.vue"),
	() => import("../views/ProjectsView.vue"),
	() => import("../views/SettingsView.vue"),
	() => import("../views/TaskView.vue"),
);

// The breadcrumb leaf on a project, task, channel or file page switches
// between its siblings, like GitHub's repository switcher (#1063).

const LONG_PROJECT = "lsegal/a-repository-with-a-very-long-name-indeed";
const BOARD = "https://github.com/users/lsegal/projects/3";

const states = { starting: 0, paused: 0, running: 0, done: 0, failed: 0 };

function mockAviary() {
	mockMCP({
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

/** Opens url once its breadcrumb shows. */
async function open(url: string, options?: { token?: string | null }) {
	await renderApp(url, options);
	await expect.element(page.getByTestId("settings-title")).toBeVisible();
}

function trigger() {
	return page
		.getByTestId("settings-breadcrumb")
		.getByTestId("crumb-switcher-trigger");
}

function options() {
	return page.getByTestId("crumb-switcher").getByRole("option");
}

/** Each matched element's text, whitespace collapsed, as Playwright's
 * toHaveText with an array compares them. */
function texts(loc: Locator): string[] {
	return loc
		.elements()
		.map((el) => (el.textContent ?? "").replace(/\s+/g, " ").trim());
}

function count(loc: Locator): number {
	return loc.elements().length;
}

beforeEach(() => {
	mockAviary();
});

test("a project leaf opens a focused, filterable switcher of the agent's projects", async () => {
	await open("/agents/Aviary/projects/o%2Fr?state=running");
	const button = trigger();
	await expect.element(button).toHaveAttribute("aria-haspopup", "listbox");
	await expect.element(button).toHaveAttribute("aria-expanded", "false");
	await expect
		.element(button)
		.toHaveAttribute(
			"title",
			expect.stringMatching(/^Switch project \((Alt\+|⌥)S\)$/),
		);
	await button.click();
	await expect.element(button).toHaveAttribute("aria-expanded", "true");

	const popover = page.getByTestId("crumb-switcher");
	await expect
		.element(popover.getByTestId("crumb-switcher-title"))
		.toHaveText("Switch project");
	const search = popover.getByTestId("crumb-switcher-search");
	await expect.element(search).toHaveFocus();
	await expect.element(popover.getByRole("listbox")).toBeVisible();
	await expect
		.poll(() => texts(options()))
		.toEqual(["o/r", LONG_PROJECT, BOARD]);
	// The current project is checkmarked and starts selected.
	await expect
		.element(options().nth(0))
		.toHaveAttribute("data-current", "true");
	await expect
		.element(options().nth(0))
		.toHaveAttribute("aria-selected", "true");
	await expect
		.element(popover.locator('[data-current="true"]').getByLabelText("Current"))
		.toBeVisible();
	const icons = popover.getByTestId("crumb-switcher-icon");
	await expect.element(icons.nth(0)).toHaveAttribute("data-icon", "repo");
	await expect.element(icons.nth(2)).toHaveAttribute("data-icon", "board");

	await search.fill("LONG name");
	await expect.poll(() => texts(options())).toEqual([LONG_PROJECT]);
	await search.fill("nothing-like-it");
	await expect.poll(() => count(options())).toBe(0);
	await expect
		.element(popover.getByTestId("crumb-switcher-empty"))
		.toBeVisible();
	await search.fill("long");
	// Enter opens the sibling's board with the same filters.
	await commands.keyPress("Enter");
	await expect
		.poll(currentURL)
		.toBe(
			`/agents/Aviary/projects/${encodeURIComponent(LONG_PROJECT)}?state=running`,
		);
	await expect.poll(() => count(popover)).toBe(0);
	await expect
		.element(page.getByTestId("settings-title"))
		.toHaveText(LONG_PROJECT);
});

test("a job page's leaf is the job, so it has no switcher", async () => {
	await open("/agents/Aviary/projects/o%2Fr/jobs/5");
	await expect.poll(() => count(trigger())).toBe(0);
});

test("the shortcut opens the task switcher; arrows and Enter go to a sibling", async () => {
	await open("/agents/Aviary/tasks/release-aviary");
	await expect.element(trigger()).toHaveAttribute("aria-keyshortcuts", "Alt+S");
	await commands.keyPress("Alt+KeyS");
	const popover = page.getByTestId("crumb-switcher");
	await expect
		.element(popover.getByTestId("crumb-switcher-title"))
		.toHaveText("Switch task");
	await expect
		.element(popover.getByTestId("crumb-switcher-search"))
		.toHaveFocus();
	await expect
		.poll(() => texts(options()))
		.toEqual(["release-aviary", "nightlyPaused", "triage"]);
	const icons = popover.getByTestId("crumb-switcher-icon");
	await expect.element(icons.nth(0)).toHaveAttribute("data-icon", "task");
	await expect.element(icons.nth(2)).toHaveAttribute("data-icon", "workflow");

	await commands.keyPress("ArrowDown");
	await commands.keyPress("ArrowDown");
	await expect
		.element(options().nth(2))
		.toHaveAttribute("aria-selected", "true");
	// ArrowDown wraps from the last row to the first, ArrowUp back.
	await commands.keyPress("ArrowDown");
	await expect
		.element(options().nth(0))
		.toHaveAttribute("aria-selected", "true");
	await commands.keyPress("ArrowUp");
	await commands.keyPress("ArrowUp");
	await expect
		.element(options().nth(1))
		.toHaveAttribute("aria-selected", "true");
	await commands.keyPress("Enter");
	await expect.poll(currentURL).toBe("/agents/Aviary/tasks/nightly");
});

test("a run page's leaf is the run, so it has no switcher", async () => {
	await open("/agents/Aviary/tasks/release-aviary/runs/job_1");
	await expect.poll(() => count(trigger())).toBe(0);
});

test("Escape closes the channel switcher and returns focus to the leaf", async () => {
	await open("/agents/Aviary/channels/signal");
	await trigger().click();
	await expect.poll(() => texts(options())).toEqual(["signal", "teamDisabled"]);
	const icons = page.getByTestId("crumb-switcher-icon");
	await expect.element(icons.nth(0)).toHaveAttribute("data-icon", "signal");
	await expect.element(icons.nth(1)).toHaveAttribute("data-icon", "slack");
	await commands.keyPress("Escape");
	await expect.poll(() => count(page.getByTestId("crumb-switcher"))).toBe(0);
	await expect.element(trigger()).toHaveFocus();
	// Clicking a sibling opens its conversation.
	await trigger().click();
	await options().filter({ hasText: "team" }).click();
	await expect.poll(currentURL).toBe("/agents/Aviary/channels/team");
});

test("the Files page's leaf switches between files, marking built-in ones", async () => {
	await open("/agents/Aviary/files?file=AGENTS.md");
	await expect
		.poll(() =>
			texts(page.getByTestId("settings-breadcrumb").getByRole("listitem")),
		)
		.toEqual(["Aviary", "Files", "AGENTS.md"]);
	await trigger().click();
	await expect
		.element(page.getByTestId("crumb-switcher-title"))
		.toHaveText("Switch file");
	await expect
		.poll(() => texts(options()))
		.toEqual(["AGENTS.mdBuilt-in", "MEMORY.mdBuilt-in", "notes.md"]);
	const icons = page.getByTestId("crumb-switcher-icon");
	await expect.element(icons.nth(0)).toHaveAttribute("data-icon", "builtin");
	await expect.element(icons.nth(2)).toHaveAttribute("data-icon", "file");
	await page.getByTestId("crumb-switcher-search").fill("notes");
	await commands.keyPress("Enter");
	await expect.poll(currentURL).toBe("/agents/Aviary/files?file=notes.md");
	await expect
		.element(page.getByTestId("settings-title"))
		.toHaveText("notes.md");
});

test("the switcher fits a phone screen and truncates long names", async () => {
	await page.viewport(360, 700);
	await open(`/agents/Aviary/projects/${encodeURIComponent(LONG_PROJECT)}`);
	await trigger().click();
	const popover = page.getByTestId("crumb-switcher");
	await expect.element(popover).toBeVisible();
	const box = popover.element().getBoundingClientRect();
	expect(box.x).toBeGreaterThanOrEqual(0);
	expect(box.x + box.width).toBeLessThanOrEqual(360);
	const label = options().nth(1).locator(".truncate");
	await expect.element(label).toBeVisible();
	const el = label.element();
	const overflow = {
		scroll: el.scrollWidth,
		client: el.clientWidth,
		ellipsis: getComputedStyle(el).textOverflow,
	};
	expect(overflow.scroll).toBeGreaterThan(overflow.client);
	expect(overflow.ellipsis).toBe("ellipsis");
});

test("the switcher works with the sidebar collapsed", async () => {
	localStorage.setItem("aviary.sidebar.collapsed", "1");
	await open("/agents/Aviary/tasks/release-aviary");
	await trigger().click();
	await options().filter({ hasText: "triage" }).click();
	await expect.poll(currentURL).toBe("/agents/Aviary/tasks/triage");
});

test("an aviary watch session switches between its projects", async () => {
	mockMCP({
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
	// After mockMCP, so this health answer wins over its default.
	route("/api/health", (r) =>
		r.fulfill({
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
	await open("/agents/watch/projects/owner%2Frepo", { token: null });
	await trigger().click();
	await expect
		.poll(() => texts(options()))
		.toEqual(["owner/repo", "owner/other"]);
	await options().filter({ hasText: "owner/other" }).click();
	await expect.poll(currentURL).toBe("/agents/watch/projects/owner%2Fother");
});
