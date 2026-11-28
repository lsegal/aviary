import { expect, test } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { renderApp } from "../component-test/app";
import { mockMCP } from "../component-test/server";

// The Code Loop board's agent models and quota show in exactly one place at
// every width: inline under the stats from sm up, or in the ⓘ Board details
// popover, or in "…" when the details moved there, below it (#1580, #1582).
// Fixtures follow the payloads of project_status and project_job_list.
// The browser font size cases stay in web/e2e (they set it through CDP).

const QUOTA = "week 65% left";
const TARGETS = ["lsegal/aviary", "lsegal/forgo"];
const WIDTHS = [639, 640, 641, 700, 768, 900, 1024];

function setup() {
	mockMCP({
		agent_list: [{ name: "coder" }],
		job_list: [],
		task_upcoming: [],
		config_validate: [],
		config_get: {
			agents: [
				{
					name: "coder",
					model: "cli/claude/opus",
					projects: TARGETS.map((target) => ({ target })),
				},
			],
		},
		project_status: {
			running: true,
			targets: TARGETS,
			issue_counts: { "lsegal/aviary": 3, "lsegal/forgo": 0 },
			delivery: "poll",
			instanceID: "AB12CD34",
			clis: [
				{ name: "claude", models: ["opus"], quota: QUOTA, signedIn: true },
			],
			states: { starting: 0, paused: 0, running: 0, done: 0, failed: 0 },
			recent_logs: "",
		},
		project_job_list: { jobs: [], total: 0, offset: 0, limit: 50 },
		task_list: [],
	});
}

/** Whether any match is visible, as Playwright's isVisible. */
function isVisible(loc: Locator): boolean {
	return loc.elements().some((el) => {
		const box = el.getBoundingClientRect();
		return (
			box.width > 0 &&
			box.height > 0 &&
			el.checkVisibility({ visibilityProperty: true })
		);
	});
}

// Where the models and quota are actually visible, opening the details
// popover and "…" to look inside them.
async function placesShowingQuota() {
	const region = (scope: Locator) =>
		scope.getByRole("region", { name: "Models and quota" }).getByText(QUOTA);
	const places: string[] = [];
	if (isVisible(region(page.getByTestId("board-nav-row"))))
		places.push("inline");

	const info = page.getByTestId("board-info");
	if (isVisible(info)) {
		await info.click();
		const details = page.getByRole("dialog", { name: "Board details" });
		await expect.element(details).toBeVisible();
		if (isVisible(details.getByText(QUOTA))) places.push("popover");
		await commands.keyPress("Escape");
		await expect.poll(() => isVisible(details)).toBe(false);
	}

	await page.getByTestId("board-more-menu").click();
	const menu = page.getByTestId("board-more-items");
	await expect.element(menu).toBeVisible();
	if (isVisible(menu.getByText(QUOTA))) places.push("menu");
	await commands.keyPress("Escape");
	await expect.poll(() => isVisible(menu)).toBe(false);
	return places;
}

async function sweep(path: string, collapsed: boolean) {
	for (const width of WIDTHS) {
		await page.viewport(width, 900);
		// A fresh page load per width, with the sidebar state stored first.
		localStorage.setItem("aviary.sidebar.collapsed", collapsed ? "1" : "0");
		await renderApp(path);
		await expect.element(page.getByTestId("stats-toggle")).toBeVisible();
		if (width >= 1024)
			await expect
				.element(page.getByTestId(collapsed ? "sidebar-rail" : "sidebar"))
				.toBeVisible();
		const places = await placesShowingQuota();
		expect(places, `${width}px`).toHaveLength(1);
		expect(places[0], `${width}px`).toBe(width >= 640 ? "inline" : "popover");
	}
}

for (const collapsed of [false, true]) {
	const sidebar = collapsed ? "collapsed" : "open";
	test(`a project board shows the models and quota in exactly one place at every width, sidebar ${sidebar} (#1582)`, async () => {
		setup();
		await sweep("/agents/coder/projects/lsegal%2Faviary", collapsed);
	});

	test(`the all-projects board shows the models and quota in exactly one place at every width, sidebar ${sidebar} (#1582)`, async () => {
		setup();
		await sweep("/agents/coder/projects", collapsed);
	});
}
