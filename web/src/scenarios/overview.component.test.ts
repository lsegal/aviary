import { expect, test } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { currentURL, renderApp } from "../component-test/app";
import { mockMCP, route, unroute } from "../component-test/server";
import { boundingBoxOf } from "./helpers/box";

// The MCP retry scenarios ("recovers when the MCP session expires", "retries
// a transient MCP 502 during overview load") are in overview-mcp-retry-*:
// they count MCP initializes, and the MCP session is module state, so each
// needs a fresh page.

const AGENTS = [
	{
		id: "a1",
		name: "assistant",
		model: "anthropic/claude-sonnet-4-5",
		fallbacks: [],
		state: "idle",
	},
	{
		id: "a2",
		name: "coder",
		model: "openai/gpt-4o",
		fallbacks: [],
		state: "running",
	},
];

const JOBS = [
	{
		id: "j1",
		task_id: "nightly",
		agent_id: "assistant",
		status: "completed",
		attempts: 1,
		created_at: "",
		updated_at: "",
	},
	{
		id: "j2",
		task_id: "build",
		agent_id: "coder",
		status: "in_progress",
		attempts: 1,
		created_at: "",
		updated_at: "",
	},
];

const count = (loc: Locator) => loc.elements().length;
const texts = (loc: Locator) =>
	loc
		.elements()
		.map((el) => (el.textContent ?? "").replace(/\s+/g, " ").trim());

test("shows agent and job counts", async () => {
	mockMCP({
		agent_list: AGENTS,
		job_list: JOBS,
		config_validate: [],
	});
	await renderApp("/system/overview");

	await expect.element(page.getByText("2").first()).toBeVisible(); // 2 agents
	// scope to the Jobs card to avoid matching the agent state badge
	const jobsCard = page.locator("a").filter({ hasText: "Jobs" });
	await expect.element(jobsCard.getByText("1 running")).toBeVisible();
});

test("health card is green when no config issues", async () => {
	mockMCP({
		agent_list: AGENTS,
		job_list: [],
		config_validate: [],
	});
	await renderApp("/system/overview");

	await expect.element(page.getByText("Healthy")).toBeVisible();
	await expect.element(page.getByText("No issues found")).toBeVisible();
});

test("health card is red and shows errors", async () => {
	const issues = [
		{
			level: "ERROR",
			field: "agents[0].model",
			message:
				"credential \"anthropic:default\" not found in auth store — run 'aviary auth set anthropic:default <your-api-key>'",
		},
		{
			level: "WARN",
			field: "agents[0].channels[0].allowFrom",
			message:
				"empty allowFrom list will silently reject all incoming messages",
		},
	];
	mockMCP({
		agent_list: AGENTS,
		job_list: [],
		config_validate: issues,
	});
	await renderApp("/system/overview");

	await expect.element(page.getByText("Errors")).toBeVisible();
	await expect.element(page.getByText("1 error, 1 warning")).toBeVisible();
	await expect.element(page.getByText("agents[0].model")).toBeVisible();
	await expect
		.element(page.getByText("agents[0].channels[0].allowFrom"))
		.toBeVisible();
});

test("health card is yellow when warnings only", async () => {
	const issues = [
		{
			level: "WARN",
			field: "agents[0].model",
			message: "no model configured; agent will not respond to prompts",
		},
	];
	mockMCP({
		agent_list: [{ name: "bot", state: "idle" }],
		job_list: [],
		config_validate: issues,
	});
	await renderApp("/system/overview");

	await expect.element(page.getByText("Warnings")).toBeVisible();
	await expect.element(page.getByText(/^1 warning$/)).toBeVisible();
});

test("re-check button refreshes doctor status", async () => {
	const issues = [
		{
			level: "ERROR",
			field: "agents[0].model",
			message: "credential not found",
		},
	];
	mockMCP({
		agent_list: [{ name: "bot", state: "idle" }],
		job_list: [],
		config_validate: issues,
	});
	await renderApp("/system/overview");
	await expect.element(page.getByText("Errors")).toBeVisible();

	// Fix the issue, re-intercept with clean result.
	unroute("/mcp");
	mockMCP({
		agent_list: [{ name: "bot", state: "idle" }],
		job_list: [],
		config_validate: [],
	});
	await page.getByRole("button", { name: "Re-check" }).click();

	await expect.element(page.getByText("No issues found")).toBeVisible();
});

test("stat cards link to their detail views", async () => {
	mockMCP({
		agent_list: [{ name: "bot", state: "idle" }],
		job_list: [],
		config_validate: [],
	});
	await renderApp("/system/overview");

	await page.getByText("Agents").first().click();
	await expect.poll(currentURL).toMatch(/\/agents/);
});

test("cards below the overview list every System page (#822)", async () => {
	mockMCP({
		agent_list: [{ name: "bot", state: "idle" }],
		job_list: [],
		config_validate: [],
	});
	await renderApp("/system/overview");

	const pages = page.getByTestId("overview-system-pages");
	await expect
		.poll(() => texts(pages.getByTestId("overview-system-card-title")))
		.toEqual([
			"Usage",
			"Tools",
			"Skills",
			"Sessions",
			"Models",
			"Logs",
			"Daemons",
		]);
	await pages.getByTestId("overview-system-card-logs").click();
	await expect.poll(currentURL).toBe("/system/logs");
});

test("each System card shows its own decorative icon (#896)", async () => {
	mockMCP({
		agent_list: [{ name: "bot", state: "idle" }],
		job_list: [],
		config_validate: [],
	});
	await renderApp("/system/overview");

	const keys = [
		"usage",
		"tools",
		"skills",
		"sessions",
		"models",
		"logs",
		"daemons",
	];
	const icons: string[] = [];
	for (const key of keys) {
		const card = page.getByTestId(`overview-system-card-${key}`);
		const tile = card.getByTestId("system-page-icon");
		await expect.element(tile).toHaveAttribute("aria-hidden", "true");
		await expect.element(tile.locator("svg")).toBeVisible();
		icons.push(tile.locator("svg").element().innerHTML);
		// The icon is decorative, so the card's name is its visible title.
		const title = card.getByTestId("overview-system-card-title");
		await expect
			.element(card)
			.toHaveAccessibleName(title.element().textContent ?? "");
	}
	expect(new Set(icons).size).toBe(keys.length);
});

test("a System page keeps its icon in the rail and header, but not the sidebar list (#896, #902)", async () => {
	mockMCP({
		agent_list: [{ name: "bot", state: "idle" }],
		job_list: [],
		config_validate: [],
	});
	await renderApp("/system/overview");
	const usageCard = page
		.getByTestId("overview-system-card-usage")
		.locator("svg");
	await expect.element(usageCard).toBeInTheDocument();
	const cardIcon = usageCard.element().innerHTML;

	await page.getByTestId("sidebar-system-toggle").click();
	const sidebarList = page.getByTestId("sidebar-system-list");
	await expect
		.element(sidebarList.getByTestId("sidebar-system-usage"))
		.toBeVisible();
	await expect
		.poll(() => count(sidebarList.getByTestId("system-page-icon")))
		.toBe(0);
	await expect.poll(() => count(sidebarList.locator("svg"))).toBe(0);

	await page.getByTestId("sidebar-system-usage").click();
	await expect.poll(currentURL).toBe("/system/usage");
	const header = page.getByTestId("settings-header-icon").locator("svg");
	await expect.element(header).toBeInTheDocument();
	expect(header.element().innerHTML).toBe(cardIcon);

	await commands.keyPress("ControlOrMeta+b");
	const railIcon = page
		.getByTestId("sidebar-rail-system-usage")
		.getByTestId("system-page-icon")
		.locator("svg");
	await expect.element(railIcon).toBeInTheDocument();
	expect(railIcon.element().innerHTML).toBe(cardIcon);
	// The collapsed sidebar is module state; leave it expanded for the next test.
	await commands.keyPress("ControlOrMeta+b");
	await expect.element(page.getByTestId("sidebar")).toBeVisible();
});

const padding = (locator: Locator) =>
	getComputedStyle(locator.element()).paddingTop;

const overflows = (locator: Locator) =>
	locator
		.elements()
		.flatMap((el) => [el, ...el.querySelectorAll("*")])
		.filter((el) => el.scrollWidth > el.clientWidth + 1)
		.map((el) => el.textContent?.trim());

const MANY_AGENT_STATES = [
	"idle",
	"running",
	"stopped",
	"error",
	"starting",
	"paused",
].map((state, i) => ({ name: `agent-${i}`, state }));

for (const { name, width } of [
	{ name: "desktop", width: 1280 },
	{ name: "tablet", width: 800 },
	{ name: "mobile", width: 375 },
]) {
	test(`status cards are compact and unclipped on ${name} (#925)`, async () => {
		await page.viewport(width, 900);
		mockMCP({
			agent_list: MANY_AGENT_STATES,
			job_list: JOBS,
			config_validate: [],
		});
		await renderApp("/system/overview");

		const cards = page.getByTestId("overview-stat-cards").locator(":scope > *");
		await expect.poll(() => count(cards)).toBe(4);
		await expect.element(cards.first().getByText("1 starting")).toBeVisible();
		for (const card of cards.all()) {
			expect(padding(card)).toBe("16px");
			expect((await boundingBoxOf(card)).height).toBeGreaterThanOrEqual(44);
		}
		const value = cards.first().locator("span").nth(1);
		expect(getComputedStyle(value.element()).fontSize).toBe("24px");
		expect(overflows(cards)).toEqual([]);
	});
}

test("System cards are compact on mobile only, leaving page headers alone (#925)", async () => {
	await page.viewport(375, 900);
	mockMCP({
		agent_list: [{ name: "bot", state: "idle" }],
		job_list: [],
		config_validate: [],
	});
	await renderApp("/system/overview");

	const cards = page
		.getByTestId("overview-system-pages")
		.locator(
			"[data-testid^='overview-system-card-']:not([data-testid$='-title'])",
		);
	await expect.poll(() => count(cards)).toBe(7);
	const card = cards.first();
	const tile = card.getByTestId("system-page-icon");
	expect(padding(card)).toBe("16px");
	expect((await boundingBoxOf(tile)).width).toBe(28);
	for (const c of cards.all()) {
		expect((await boundingBoxOf(c)).height).toBeGreaterThanOrEqual(44);
	}
	expect(overflows(cards)).toEqual([]);

	await page.viewport(1280, 900);
	await expect.poll(() => padding(card)).toBe("20px");
	await expect.poll(async () => (await boundingBoxOf(tile)).width).toBe(36);

	await page.viewport(375, 900);
	await card.click();
	await expect.poll(currentURL).toBe("/system/usage");
	// The header keeps the shared header's tile (#1147).
	const header = page.getByTestId("settings-header-icon");
	expect((await boundingBoxOf(header)).width).toBe(28);
});

test("loading skeleton matches the compact status cards (#925)", async () => {
	await page.viewport(1280, 900);
	let release = () => {};
	const held = new Promise<void>((resolve) => {
		release = resolve;
	});
	mockMCP({
		agent_list: AGENTS,
		job_list: JOBS,
		config_validate: [],
	});
	route("/mcp", async (r) => {
		await held;
		await r.fallback();
	});
	await renderApp("/system/overview");

	const skeleton = page.getByTestId("overview-stat-skeleton").first();
	await expect.element(skeleton).toBeVisible();
	expect(padding(skeleton)).toBe("16px");
	const skeletonHeight = (await boundingBoxOf(skeleton)).height;

	release();
	const card = page
		.getByTestId("overview-stat-cards")
		.locator(":scope > *")
		.first();
	await expect.element(card).toBeVisible();
	expect(
		Math.abs((await boundingBoxOf(card)).height - skeletonHeight),
	).toBeLessThanOrEqual(4);
});
