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
		"GitHub Projects",
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
	// With no Agents row, Add agent is the "+" on the Aviary row (#638).
	await expect(sidebar.getByTestId("sidebar-add-agent")).toHaveCount(0);
	await expect(page.getByTestId("sidebar-add-agent")).toHaveAttribute(
		"aria-label",
		"Add agent",
	);
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
	await expect(sidebar.getByTestId("sidebar-add-agent")).toHaveAttribute(
		"aria-label",
		"Add agent",
	);

	state.names = ["assistant"];
	await page.reload();
	await expect(sidebar.getByTestId("sidebar-sole-agent")).toBeVisible();
	await expect(sidebar.getByTestId("sidebar-agents")).toHaveCount(0);
});

test("adding an agent from a single-agent sidebar shows the agent tree", async ({
	page,
}) => {
	await mockAgents(page, ["assistant"]);
	await page.goto("/agents/assistant/chat");
	const sidebar = page.getByTestId("sidebar-nav");
	await expect(sidebar.getByTestId("sidebar-sole-agent")).toBeVisible();
	await page.getByTestId("sidebar-add-agent").click();
	await expect(page).toHaveURL(/\/agents\/_1\//);
	await expect(sidebar.getByTestId("sidebar-sole-agent")).toHaveCount(0);
	const list = sidebar.getByTestId("sidebar-agent-list");
	await expect(list.locator("[data-agent=assistant]")).toBeVisible();
	await expect(list.locator("[data-agent=_1]")).toHaveText(/New agent/);
});

const ALL_PAGES = [
	"Chats",
	"GitHub Projects",
	"Scheduled Tasks",
	"Files",
	"Agent Settings",
	"System",
	"+ Add Agent",
];

// The narrow-screen main row: its pills' labels, the "…" menu's labels, and
// whether every row item sits on one line, one pill tall.
async function mobileNav(page: Page) {
	const nav = page.getByTestId("mobile-main-nav");
	await expect(nav).toBeVisible();
	// Let a resize's ResizeObserver pass settle the split.
	await page.evaluate(
		() =>
			new Promise((done) =>
				requestAnimationFrame(() => requestAnimationFrame(done)),
			),
	);
	const more = nav.getByTestId("mobile-nav-more");
	const hidden: string[] = [];
	if (await more.isVisible()) {
		await more.click();
		const menu = page.getByTestId("mobile-nav-menu");
		hidden.push(...(await menu.getByRole("menuitem").allInnerTexts()));
		await more.click();
		await expect(menu).toHaveCount(0);
	}
	const row = await nav.evaluate((el) => {
		const items = [...el.children].filter(
			(c) => c instanceof HTMLElement && c.offsetParent && !c.ariaHidden,
		) as HTMLElement[];
		const tops = items.map((c) => Math.round(c.getBoundingClientRect().top));
		const tallest = Math.max(
			...items.map((c) => c.getBoundingClientRect().height),
		);
		return {
			labels: items
				.filter((c) => c.tagName === "A")
				.map((c) => (c.firstChild?.textContent ?? "").trim()),
			oneLine: new Set(tops).size === 1,
			height: el.getBoundingClientRect().height,
			tallest,
		};
	});
	expect(row.oneLine).toBe(true);
	expect(row.height).toBeLessThanOrEqual(row.tallest + 1);
	return { visible: row.labels, hidden: hidden.map((h) => h.trim()) };
}

test.describe("on a phone", () => {
	test.use({ viewport: { width: 390, height: 844 } });

	test("the main nav is one row and the rest sit behind …", async ({
		page,
	}) => {
		await mockAgents(page, ["assistant"]);
		await page.goto("/system/overview");
		const { visible, hidden } = await mobileNav(page);
		expect(visible.slice(0, 2)).toEqual(["Chats", "GitHub Projects"]);
		expect(visible).toContain("System");
		expect(hidden.length).toBeGreaterThan(0);
		expect([...visible, ...hidden].sort()).toEqual([...ALL_PAGES].sort());

		// Choosing a hidden item navigates and closes the menu.
		const nav = page.getByTestId("mobile-main-nav");
		await nav.getByTestId("mobile-nav-more").click();
		await page
			.getByTestId("mobile-nav-menu")
			.getByRole("menuitem", { name: "Scheduled Tasks" })
			.click();
		await expect(page).toHaveURL("/agents/assistant/tasks");
		await expect(page.getByTestId("mobile-nav-menu")).toHaveCount(0);
	});

	test("the … menu closes on an outside tap and works by keyboard", async ({
		page,
	}) => {
		await mockAgents(page, ["assistant"]);
		await page.goto("/system/overview");
		const more = page.getByTestId("mobile-nav-more");
		const menu = page.getByTestId("mobile-nav-menu");
		await more.click();
		await expect(menu).toBeVisible();
		await expect(more).toHaveAttribute("aria-expanded", "true");
		await page.mouse.click(200, 600);
		await expect(menu).toHaveCount(0);

		await more.focus();
		await page.keyboard.press("ArrowDown");
		const items = menu.getByRole("menuitem");
		await expect(items.first()).toBeFocused();
		await page.keyboard.press("ArrowDown");
		await expect(items.nth(1)).toBeFocused();
		await page.keyboard.press("Escape");
		await expect(menu).toHaveCount(0);
		await expect(more).toBeFocused();

		await page.keyboard.press("ArrowDown");
		await page.keyboard.press("End");
		const last = (await items.last().innerText()).trim();
		await page.keyboard.press("Enter");
		await expect(menu).toHaveCount(0);
		await expect(page).toHaveURL(
			last === "+ Add Agent" ? /\/agents/ : /\/agents\/assistant\//,
		);
	});

	test("the active page stays in the row", async ({ page }) => {
		await mockAgents(page, ["assistant"]);
		await page.goto("/agents/assistant/settings");
		const { visible, hidden } = await mobileNav(page);
		expect(visible).toContain("Agent Settings");
		expect(hidden).not.toContain("Agent Settings");
		expect(hidden.length).toBeGreaterThan(0);
		const pill = page
			.getByTestId("mobile-main-nav")
			.getByRole("link", { name: "Agent Settings", exact: true });
		await expect(pill).toHaveClass(/shadow-sm/);
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

test("a wider narrow screen shows more of the main nav and recomputes on resize", async ({
	page,
}) => {
	await page.setViewportSize({ width: 390, height: 844 });
	await mockAgents(page, ["assistant"]);
	await page.goto("/system/overview");
	const phone = await mobileNav(page);

	await page.setViewportSize({ width: 820, height: 1000 });
	const tablet = await mobileNav(page);
	expect(tablet.visible.length).toBeGreaterThan(phone.visible.length);
	expect(tablet.hidden.length).toBeLessThan(phone.hidden.length);

	await page.setViewportSize({ width: 390, height: 844 });
	expect(await mobileNav(page)).toEqual(phone);
});
