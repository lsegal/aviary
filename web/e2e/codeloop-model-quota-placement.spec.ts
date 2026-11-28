import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

// The Code Loop board's agent models and quota show in exactly one place at
// every width: inline under the stats from sm up, or in the ⓘ Board details
// popover, or in "…" when the details moved there, below it (#1580, #1582).
// Fixtures follow the payloads of project_status and project_job_list.

const QUOTA = "week 65% left";
const TARGETS = ["lsegal/aviary", "lsegal/forgo"];
const WIDTHS = [639, 640, 641, 700, 768, 900, 1024];

async function setup(page: Page, collapsed: boolean) {
	await setAuthToken(page);
	await page.addInitScript(
		(value) => localStorage.setItem("aviary.sidebar.collapsed", value),
		collapsed ? "1" : "0",
	);
	await mockMCP(page, {
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

// Where the models and quota are actually visible, opening the details
// popover and "…" to look inside them.
async function placesShowingQuota(page: Page) {
	const region = (scope: ReturnType<Page["getByTestId"]>) =>
		scope.getByRole("region", { name: "Models and quota" }).getByText(QUOTA);
	const places: string[] = [];
	if (await region(page.getByTestId("board-nav-row")).isVisible())
		places.push("inline");

	const info = page.getByTestId("board-info");
	if (await info.isVisible()) {
		await info.click();
		const details = page.getByRole("dialog", { name: "Board details" });
		await expect(details).toBeVisible();
		if (await details.getByText(QUOTA).isVisible()) places.push("popover");
		await page.keyboard.press("Escape");
		await expect(details).toBeHidden();
	}

	await page.getByTestId("board-more-menu").click();
	const menu = page.getByTestId("board-more-items");
	await expect(menu).toBeVisible();
	if (await menu.getByText(QUOTA).isVisible()) places.push("menu");
	await page.keyboard.press("Escape");
	await expect(menu).toBeHidden();
	return places;
}

async function sweep(page: Page, path: string, collapsed: boolean) {
	for (const width of WIDTHS) {
		await page.setViewportSize({ width, height: 900 });
		await page.goto(path);
		await expect(page.getByTestId("stats-toggle")).toBeVisible();
		if (width >= 1024)
			await expect(
				page.getByTestId(collapsed ? "sidebar-rail" : "sidebar"),
			).toBeVisible();
		const places = await placesShowingQuota(page);
		expect(places, `${width}px`).toHaveLength(1);
		expect(places[0], `${width}px`).toBe(width >= 640 ? "inline" : "popover");
	}
}

for (const collapsed of [false, true]) {
	const sidebar = collapsed ? "collapsed" : "open";
	test(`a project board shows the models and quota in exactly one place at every width, sidebar ${sidebar} (#1582)`, async ({
		page,
	}) => {
		await setup(page, collapsed);
		await sweep(page, "/agents/coder/projects/lsegal%2Faviary", collapsed);
	});

	test(`the all-projects board shows the models and quota in exactly one place at every width, sidebar ${sidebar} (#1582)`, async ({
		page,
	}) => {
		await setup(page, collapsed);
		await sweep(page, "/agents/coder/projects", collapsed);
	});
}

// Tailwind's sm is 40rem, so with a larger or smaller browser font size the
// board's layout switches elsewhere than 640px, and the models and quota
// switch with it, still in exactly one place.
for (const fontSize of [12, 20]) {
	test(`with a ${fontSize}px browser font the models and quota still show in exactly one place (#1582)`, async ({
		page,
		browserName,
	}) => {
		test.skip(browserName !== "chromium", "font sizes are set through CDP");
		const cdp = await page.context().newCDPSession(page);
		await cdp.send("Page.setFontSizes", {
			fontSizes: { standard: fontSize },
		});
		await setup(page, false);
		const breakpoint = 40 * fontSize;
		for (const width of [breakpoint - 1, breakpoint, 639, 640, 1024]) {
			await page.setViewportSize({ width, height: 900 });
			await page.goto("/agents/coder/projects/lsegal%2Faviary");
			await expect(page.getByTestId("stats-toggle")).toBeVisible();
			const places = await placesShowingQuota(page);
			expect(places, `${width}px`).toHaveLength(1);
			// Below sm a narrow header may move the details into "…".
			if (width >= breakpoint) expect(places[0], `${width}px`).toBe("inline");
			else expect(places[0], `${width}px`).not.toBe("inline");
		}
	});
}
