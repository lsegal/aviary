import { describe, expect, test } from "vitest";
import { page } from "vitest/browser";
import { currentURL, reload, renderApp } from "../component-test/app";
import { declarePages } from "../component-test/routes";
import { mockMCP } from "../component-test/server";

// The pages this file renders (see component-test/routes.ts).
declarePages(
	() => import("../views/OverviewView.vue"),
	() => import("../views/SettingsView.vue"),
);

function agent(name: string) {
	return { name, model: "anthropic/claude-sonnet-4-5", fallbacks: [] };
}

// config_get answers from a mutable agent list so a test can add or remove
// agents and reload the configuration.
function mockAgents(names: string[]) {
	const state = { names };
	mockMCP({
		agent_list: [],
		job_list: [],
		config_validate: [],
		config_get: () => ({ agents: state.names.map(agent) }),
	});
	return state;
}

const count = (loc: { elements(): Element[] }) => loc.elements().length;

test("a single agent's pages are the sidebar's top level", async () => {
	mockAgents(["assistant"]);
	await renderApp("/system/overview");

	const sidebar = page.getByTestId("sidebar-nav");
	const sole = sidebar.getByTestId("sidebar-sole-agent");
	await expect.element(sole).toBeVisible();
	expect(count(sidebar.getByTestId("sidebar-agents"))).toBe(0);
	expect(count(sidebar.getByTestId("sidebar-agent-list"))).toBe(0);
	expect(count(sole.getByRole("link", { name: "assistant" }))).toBe(0);
	for (const label of [
		"Chats",
		"Agent Settings",
		"Scheduled Tasks",
		"Code Loop",
	]) {
		await expect
			.element(sole.getByRole("link", { name: label, exact: true }))
			.toBeVisible();
	}
	await expect
		.element(sole.getByRole("link", { name: "Chats", exact: true }))
		.toHaveAttribute("href", "/agents/assistant/chat");

	// System follows the agent's pages.
	const order = sidebar
		.locator("[data-testid=sidebar-sole-agent], [data-testid=sidebar-system]")
		.elements()
		.map((el) => el.getAttribute("data-testid"));
	expect(order).toEqual(["sidebar-sole-agent", "sidebar-system"]);
	// Agents are added from System → Settings, not the sidebar (#759).
	expect(count(page.getByTestId("sidebar-add-agent"))).toBe(0);
});

test("a second agent restores the agent tree and removing it collapses again", async () => {
	const state = mockAgents(["assistant"]);
	await renderApp("/system/overview");
	const sidebar = page.getByTestId("sidebar-nav");
	await expect.element(sidebar.getByTestId("sidebar-sole-agent")).toBeVisible();

	state.names = ["assistant", "helper"];
	await reload();
	await expect.element(sidebar.getByTestId("sidebar-agents")).toBeVisible();
	expect(count(sidebar.getByTestId("sidebar-sole-agent"))).toBe(0);
	const list = sidebar.getByTestId("sidebar-agent-list");
	await expect.element(list.locator("[data-agent=assistant]")).toBeVisible();
	await expect.element(list.locator("[data-agent=helper]")).toBeVisible();
	expect(count(page.getByTestId("sidebar-add-agent"))).toBe(0);

	state.names = ["assistant"];
	await reload();
	await expect.element(sidebar.getByTestId("sidebar-sole-agent")).toBeVisible();
	expect(count(sidebar.getByTestId("sidebar-agents"))).toBe(0);
});

test("adding an agent from a single-agent sidebar shows the agent tree", async () => {
	mockAgents(["assistant"]);
	await renderApp("/system/settings");
	const sidebar = page.getByTestId("sidebar-nav");
	await expect.element(sidebar.getByTestId("sidebar-sole-agent")).toBeVisible();
	await page.getByTestId("settings-add-agent").click();
	await expect.poll(currentURL).toMatch(/\/agents\/_1\//);
	await expect
		.element(sidebar.getByTestId("sidebar-sole-agent"))
		.not.toBeInTheDocument();
	const list = sidebar.getByTestId("sidebar-agent-list");
	await expect.element(list.locator("[data-agent=assistant]")).toBeVisible();
	await expect.element(list.locator("[data-agent=_1]")).toHaveText(/New agent/);
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
async function mobileNav() {
	const nav = page.getByTestId("mobile-main-nav");
	await expect.element(nav).toBeVisible();
	expect(count(nav.getByTestId("mobile-nav-more"))).toBe(0);
	const el = nav.element();
	const items = [...el.children] as HTMLElement[];
	const tops = items.map((c) => Math.round(c.getBoundingClientRect().top));
	const row = {
		labels: items.map((c) => (c.firstChild?.textContent ?? "").trim()),
		oneLine: new Set(tops).size === 1,
		scrolls: el.scrollWidth > el.clientWidth,
		overflowX: getComputedStyle(el).overflowX,
	};
	expect(row.oneLine).toBe(true);
	expect(row.overflowX).toBe("auto");
	return row;
}

describe("on a phone", () => {
	test("the main nav is one swipeable row of every page, with no … menu", async () => {
		await page.viewport(390, 844);
		mockAgents(["assistant"]);
		await renderApp("/system/overview");
		// The agent's pills join System once the configuration loads.
		await expect
			.poll(async () => (await mobileNav()).labels)
			.toEqual(ALL_PAGES);
		const row = await mobileNav();
		expect(row.scrolls).toBe(true);
		const nav = page.getByTestId("mobile-main-nav");
		// The active System pill, last, is scrolled into view, and only the
		// edge with pills past it fades.
		const startFade = /right, rgba\(0, 0, 0, 0\)/;
		const endFade = /rgba\(0, 0, 0, 0\)\)$/;
		await expect.element(nav).toHaveCSS("mask-image", startFade);
		await expect.element(nav).not.toHaveCSS("mask-image", endFade);
		nav.element().scrollLeft = 0;
		await expect.element(nav).toHaveCSS("mask-image", endFade);
		await expect.element(nav).not.toHaveCSS("mask-image", startFade);
		// The page itself never scrolls sideways (#997).
		expect(
			document.documentElement.scrollWidth <=
				document.documentElement.clientWidth,
		).toBe(true);

		await nav
			.getByRole("link", { name: "Scheduled Tasks", exact: true })
			.click();
		await expect.poll(currentURL).toBe("/agents/assistant/tasks");
	});

	test("the active page is scrolled into view on load", async () => {
		await page.viewport(390, 844);
		mockAgents(["assistant"]);
		await renderApp("/agents/assistant/settings");
		const nav = page.getByTestId("mobile-main-nav");
		const pill = nav.getByRole("link", { name: "Agent Settings", exact: true });
		await expect.element(pill).toHaveClass(/shadow-sm/);
		await expect
			.poll(() => {
				const n = nav.element().getBoundingClientRect();
				const p = pill.element().getBoundingClientRect();
				return p.x >= n.x - 1 && p.x + p.width <= n.x + n.width + 1;
			})
			.toBe(true);
		expect(nav.element().scrollLeft).toBeGreaterThan(0);
	});

	test("the header is about two rows: logo and status, then the nav", async () => {
		await page.viewport(390, 844);
		mockAgents(["assistant"]);
		await renderApp("/system/overview");
		await expect.element(page.getByTestId("mobile-main-nav")).toBeVisible();
		// Logo row plus nav row: about two rows.
		const header = page.locator("header").element().getBoundingClientRect();
		expect(header.height).toBeLessThan(110);
	});
});

test("a wider narrow screen fits the main nav without scrolling or fades", async () => {
	await page.viewport(390, 844);
	mockAgents(["assistant"]);
	await renderApp("/system/overview");
	// The agent's pills join System once the configuration loads.
	await expect.poll(async () => (await mobileNav()).labels).toEqual(ALL_PAGES);
	expect((await mobileNav()).scrolls).toBe(true);

	await page.viewport(820, 1000);
	await expect.poll(async () => (await mobileNav()).scrolls).toBe(false);
	await expect
		.element(page.getByTestId("mobile-main-nav"))
		.toHaveCSS("mask-image", "none");
});
