import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

function agent(name: string) {
	return { name, model: "anthropic/claude-sonnet-4-5", fallbacks: [] };
}

// config_get answers from a mutable agent list so a test can add or remove
// agents and reload the configuration.
async function mockAgents(page: Page, names: string[]) {
	const state = { names };
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: [],
		job_list: [],
		config_validate: [],
		config_get: () => ({ agents: state.names.map(agent) }),
	});
	return state;
}

test("a single agent's pages are the sidebar's top level", async ({ page }) => {
	await mockAgents(page, ["assistant"]);
	await page.goto("/system/overview");

	const sidebar = page.getByTestId("sidebar-nav");
	const sole = sidebar.getByTestId("sidebar-sole-agent");
	await expect(sole).toBeVisible();
	await expect(sidebar.getByTestId("sidebar-agents")).toHaveCount(0);
	await expect(sidebar.getByTestId("sidebar-agent-list")).toHaveCount(0);
	await expect(sole.getByRole("link", { name: "assistant" })).toHaveCount(0);
	for (const label of [
		"Chats",
		"Agent Settings",
		"Scheduled Tasks",
		"Code Loop",
	]) {
		await expect(
			sole.getByRole("link", { name: label, exact: true }),
		).toBeVisible();
	}
	await expect(
		sole.getByRole("link", { name: "Chats", exact: true }),
	).toHaveAttribute("href", "/agents/assistant/chat");

	// System follows the agent's pages.
	const order = await sidebar
		.locator("[data-testid=sidebar-sole-agent], [data-testid=sidebar-system]")
		.evaluateAll((els) => els.map((el) => el.getAttribute("data-testid")));
	expect(order).toEqual(["sidebar-sole-agent", "sidebar-system"]);
	// Agents are added from System → Settings, not the sidebar (#759).
	await expect(page.getByTestId("sidebar-add-agent")).toHaveCount(0);
});

test("a second agent restores the agent tree and removing it collapses again", async ({
	page,
}) => {
	const state = await mockAgents(page, ["assistant"]);
	await page.goto("/system/overview");
	const sidebar = page.getByTestId("sidebar-nav");
	await expect(sidebar.getByTestId("sidebar-sole-agent")).toBeVisible();

	state.names = ["assistant", "helper"];
	await page.reload();
	await expect(sidebar.getByTestId("sidebar-sole-agent")).toHaveCount(0);
	await expect(sidebar.getByTestId("sidebar-agents")).toBeVisible();
	const list = sidebar.getByTestId("sidebar-agent-list");
	await expect(list.locator("[data-agent=assistant]")).toBeVisible();
	await expect(list.locator("[data-agent=helper]")).toBeVisible();
	await expect(page.getByTestId("sidebar-add-agent")).toHaveCount(0);

	state.names = ["assistant"];
	await page.reload();
	await expect(sidebar.getByTestId("sidebar-sole-agent")).toBeVisible();
	await expect(sidebar.getByTestId("sidebar-agents")).toHaveCount(0);
});

test("adding an agent from a single-agent sidebar shows the agent tree", async ({
	page,
}) => {
	await mockAgents(page, ["assistant"]);
	await page.goto("/system/settings");
	const sidebar = page.getByTestId("sidebar-nav");
	await expect(sidebar.getByTestId("sidebar-sole-agent")).toBeVisible();
	await page.getByTestId("settings-add-agent").click();
	await expect(page).toHaveURL(/\/agents\/_1\//);
	await expect(sidebar.getByTestId("sidebar-sole-agent")).toHaveCount(0);
	const list = sidebar.getByTestId("sidebar-agent-list");
	await expect(list.locator("[data-agent=assistant]")).toBeVisible();
	await expect(list.locator("[data-agent=_1]")).toHaveText(/New agent/);
});

const ALL_PAGES = [
	"Chats",
	"Code Loop",
	"Channels",
	"Scheduled Tasks",
	"Files",
	"Agent Settings",
	"System",
];

// The narrow-screen main row: its pills' labels in order, and whether they
// sit on one line that scrolls sideways rather than wrapping (#1003).
async function mobileNav(page: Page) {
	const nav = page.getByTestId("mobile-main-nav");
	await expect(nav).toBeVisible();
	await expect(nav.getByTestId("mobile-nav-more")).toHaveCount(0);
	const row = await nav.evaluate((el) => {
		const items = [...el.children] as HTMLElement[];
		const tops = items.map((c) => Math.round(c.getBoundingClientRect().top));
		return {
			labels: items.map((c) => (c.firstChild?.textContent ?? "").trim()),
			oneLine: new Set(tops).size === 1,
			scrolls: el.scrollWidth > el.clientWidth,
			overflowX: getComputedStyle(el).overflowX,
		};
	});
	expect(row.oneLine).toBe(true);
	expect(row.overflowX).toBe("auto");
	return row;
}

test.describe("on a phone", () => {
	test.use({ viewport: { width: 390, height: 844 } });

	test("the main nav is one swipeable row of every page, with no … menu", async ({
		page,
	}) => {
		await mockAgents(page, ["assistant"]);
		await page.goto("/system/overview");
		const row = await mobileNav(page);
		expect(row.labels).toEqual(ALL_PAGES);
		expect(row.scrolls).toBe(true);
		const nav = page.getByTestId("mobile-main-nav");
		// The active System pill, last, is scrolled into view, and only the
		// edge with pills past it fades.
		const startFade = /right, rgba\(0, 0, 0, 0\)/;
		const endFade = /rgba\(0, 0, 0, 0\)\)$/;
		await expect(nav).toHaveCSS("mask-image", startFade);
		await expect(nav).not.toHaveCSS("mask-image", endFade);
		await nav.evaluate((el) => {
			el.scrollLeft = 0;
		});
		await expect(nav).toHaveCSS("mask-image", endFade);
		await expect(nav).not.toHaveCSS("mask-image", startFade);
		// The page itself never scrolls sideways (#997).
		expect(
			await page.evaluate(
				() =>
					document.documentElement.scrollWidth <=
					document.documentElement.clientWidth,
			),
		).toBe(true);

		await nav
			.getByRole("link", { name: "Scheduled Tasks", exact: true })
			.click();
		await expect(page).toHaveURL("/agents/assistant/tasks");
	});

	test("the active page is scrolled into view on load", async ({ page }) => {
		await mockAgents(page, ["assistant"]);
		await page.goto("/agents/assistant/settings");
		const nav = page.getByTestId("mobile-main-nav");
		const pill = nav.getByRole("link", { name: "Agent Settings", exact: true });
		await expect(pill).toHaveClass(/shadow-sm/);
		await expect
			.poll(async () => {
				const [n, p] = await Promise.all([
					nav.boundingBox(),
					pill.boundingBox(),
				]);
				return (
					!!n && !!p && p.x >= n.x - 1 && p.x + p.width <= n.x + n.width + 1
				);
			})
			.toBe(true);
		expect(await nav.evaluate((el) => el.scrollLeft)).toBeGreaterThan(0);
	});

	test("the header is about two rows: logo and status, then the nav", async ({
		page,
	}) => {
		await mockAgents(page, ["assistant"]);
		await page.goto("/system/overview");
		await expect(page.getByTestId("mobile-main-nav")).toBeVisible();
		// Logo row plus nav row: about two rows.
		const header = await page.locator("header").boundingBox();
		expect(header?.height ?? 0).toBeLessThan(110);
	});
});

test("a wider narrow screen fits the main nav without scrolling or fades", async ({
	page,
}) => {
	await page.setViewportSize({ width: 390, height: 844 });
	await mockAgents(page, ["assistant"]);
	await page.goto("/system/overview");
	expect((await mobileNav(page)).scrolls).toBe(true);

	await page.setViewportSize({ width: 820, height: 1000 });
	await expect.poll(async () => (await mobileNav(page)).scrolls).toBe(false);
	await expect(page.getByTestId("mobile-main-nav")).toHaveCSS(
		"mask-image",
		"none",
	);
});
