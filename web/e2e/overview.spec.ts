import { expect, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

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

test.beforeEach(async ({ page }) => {
	await setAuthToken(page);
});

test("shows agent and job counts", async ({ page }) => {
	await mockMCP(page, {
		agent_list: AGENTS,
		job_list: JOBS,
		config_validate: [],
	});
	await page.goto("/system/overview");

	await expect(page.getByText("2").first()).toBeVisible(); // 2 agents
	// scope to the Jobs card to avoid matching the agent state badge
	const jobsCard = page.locator("a", { hasText: "Jobs" });
	await expect(jobsCard.getByText("1 running")).toBeVisible();
});

test("health card is green when no config issues", async ({ page }) => {
	await mockMCP(page, {
		agent_list: AGENTS,
		job_list: [],
		config_validate: [],
	});
	await page.goto("/system/overview");

	await expect(page.getByText("Healthy")).toBeVisible();
	await expect(page.getByText("No issues found")).toBeVisible();
});

test("health card is red and shows errors", async ({ page }) => {
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
	await mockMCP(page, {
		agent_list: AGENTS,
		job_list: [],
		config_validate: issues,
	});
	await page.goto("/system/overview");

	await expect(page.getByText("Errors")).toBeVisible();
	await expect(page.getByText("1 error, 1 warning")).toBeVisible();
	await expect(page.getByText("agents[0].model")).toBeVisible();
	await expect(page.getByText("agents[0].channels[0].allowFrom")).toBeVisible();
});

test("health card is yellow when warnings only", async ({ page }) => {
	const issues = [
		{
			level: "WARN",
			field: "agents[0].model",
			message: "no model configured; agent will not respond to prompts",
		},
	];
	await mockMCP(page, {
		agent_list: [{ name: "bot", state: "idle" }],
		job_list: [],
		config_validate: issues,
	});
	await page.goto("/system/overview");

	await expect(page.getByText("Warnings")).toBeVisible();
	await expect(page.getByText(/^1 warning$/)).toBeVisible();
});

test("re-check button refreshes doctor status", async ({ page }) => {
	const issues = [
		{
			level: "ERROR",
			field: "agents[0].model",
			message: "credential not found",
		},
	];
	await mockMCP(page, {
		agent_list: [{ name: "bot", state: "idle" }],
		job_list: [],
		config_validate: issues,
	});
	await page.goto("/system/overview");
	await expect(page.getByText("Errors")).toBeVisible();

	// Fix the issue, re-intercept with clean result.
	await page.unroute("/mcp");
	await mockMCP(page, {
		agent_list: [{ name: "bot", state: "idle" }],
		job_list: [],
		config_validate: [],
	});
	await page.getByRole("button", { name: "Re-check" }).click();

	await expect(page.getByText("No issues found")).toBeVisible();
});

// A failed first tool call is retried: a 404 means the session expired, so
// the client reinitializes first; a 502 is transient and retried as is.
for (const { name, status, body, initializeCount } of [
	{
		name: "recovers when the MCP session expires",
		status: 404,
		body: "session not found",
		initializeCount: 2,
	},
	{
		name: "retries a transient MCP 502 during overview load",
		status: 502,
		body: "bad gateway",
		initializeCount: 1,
	},
]) {
	test(name, async ({ page }) => {
		await mockMCP(page, {
			agent_list: AGENTS,
			job_list: JOBS,
			config_validate: [],
		});
		let initializes = 0;
		let firstToolCallFails = true;
		await page.route("/mcp", (route) => {
			const method = route.request().postDataJSON()?.method;
			if (method === "initialize") initializes += 1;
			if (method === "tools/call" && firstToolCallFails) {
				firstToolCallFails = false;
				return route.fulfill({ status, body });
			}
			return route.fallback();
		});

		await page.goto("/system/overview");

		await expect(page.getByText("2").first()).toBeVisible();
		await expect(page.getByText("No issues found")).toBeVisible();
		expect(initializes).toBe(initializeCount);
	});
}

test("stat cards link to their detail views", async ({ page }) => {
	await mockMCP(page, {
		agent_list: [{ name: "bot", state: "idle" }],
		job_list: [],
		config_validate: [],
	});
	await page.goto("/system/overview");

	await page.getByText("Agents").first().click();
	await expect(page).toHaveURL(/\/agents/);
});

test("cards below the overview list every System page (#822)", async ({
	page,
}) => {
	await mockMCP(page, {
		agent_list: [{ name: "bot", state: "idle" }],
		job_list: [],
		config_validate: [],
	});
	await page.goto("/system/overview");

	const pages = page.getByTestId("overview-system-pages");
	await expect(pages.getByTestId("overview-system-card-title")).toHaveText([
		"Usage",
		"Tools",
		"Skills",
		"Sessions",
		"Models",
		"Logs",
		"Daemons",
	]);
	await pages.getByTestId("overview-system-card-logs").click();
	await expect(page).toHaveURL("/system/logs");
});

test("each System card shows its own decorative icon (#896)", async ({
	page,
}) => {
	await mockMCP(page, {
		agent_list: [{ name: "bot", state: "idle" }],
		job_list: [],
		config_validate: [],
	});
	await page.goto("/system/overview");

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
		await expect(tile).toHaveAttribute("aria-hidden", "true");
		await expect(tile.locator("svg")).toBeVisible();
		icons.push(await tile.locator("svg").innerHTML());
		// The icon is decorative, so the card's name is its visible title.
		const title = card.getByTestId("overview-system-card-title");
		await expect(card).toHaveAccessibleName((await title.textContent()) ?? "");
	}
	expect(new Set(icons).size).toBe(keys.length);
});

test("a System page keeps its icon in the rail and header, but not the sidebar list (#896, #902)", async ({
	page,
}) => {
	await mockMCP(page, {
		agent_list: [{ name: "bot", state: "idle" }],
		job_list: [],
		config_validate: [],
	});
	await page.goto("/system/overview");
	const cardIcon = await page
		.getByTestId("overview-system-card-usage")
		.locator("svg")
		.innerHTML();

	await page.getByTestId("sidebar-system-toggle").click();
	const sidebarList = page.getByTestId("sidebar-system-list");
	await expect(sidebarList.getByTestId("sidebar-system-usage")).toBeVisible();
	await expect(sidebarList.getByTestId("system-page-icon")).toHaveCount(0);
	await expect(sidebarList.locator("svg")).toHaveCount(0);

	await page.getByTestId("sidebar-system-usage").click();
	await expect(page).toHaveURL("/system/usage");
	const header = page.getByTestId("settings-header-icon");
	expect(await header.locator("svg").innerHTML()).toBe(cardIcon);

	await page.keyboard.press("ControlOrMeta+b");
	const railIcon = page
		.getByTestId("sidebar-rail-system-usage")
		.getByTestId("system-page-icon");
	expect(await railIcon.locator("svg").innerHTML()).toBe(cardIcon);
});

const box = async (locator: import("@playwright/test").Locator) => {
	const b = await locator.boundingBox();
	if (!b) throw new Error("element has no bounding box");
	return b;
};

const padding = (locator: import("@playwright/test").Locator) =>
	locator.evaluate((el) => getComputedStyle(el).paddingTop);

const overflows = (locator: import("@playwright/test").Locator) =>
	locator.evaluateAll((els) =>
		els
			.flatMap((el) => [el, ...el.querySelectorAll("*")])
			.filter((el) => el.scrollWidth > el.clientWidth + 1)
			.map((el) => el.textContent?.trim()),
	);

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
	test(`status cards are compact and unclipped on ${name} (#925)`, async ({
		page,
	}) => {
		await page.setViewportSize({ width, height: 900 });
		await mockMCP(page, {
			agent_list: MANY_AGENT_STATES,
			job_list: JOBS,
			config_validate: [],
		});
		await page.goto("/system/overview");

		const cards = page.getByTestId("overview-stat-cards").locator(":scope > *");
		await expect(cards).toHaveCount(4);
		await expect(cards.first().getByText("1 starting")).toBeVisible();
		for (const card of await cards.all()) {
			expect(await padding(card)).toBe("16px");
			expect((await box(card)).height).toBeGreaterThanOrEqual(44);
		}
		const value = cards.first().locator("span").nth(1);
		expect(await value.evaluate((el) => getComputedStyle(el).fontSize)).toBe(
			"24px",
		);
		expect(await overflows(cards)).toEqual([]);
	});
}

test("System cards are compact on mobile only, leaving page headers alone (#925)", async ({
	page,
}) => {
	await page.setViewportSize({ width: 375, height: 900 });
	await mockMCP(page, {
		agent_list: [{ name: "bot", state: "idle" }],
		job_list: [],
		config_validate: [],
	});
	await page.goto("/system/overview");

	const cards = page
		.getByTestId("overview-system-pages")
		.locator(
			"[data-testid^='overview-system-card-']:not([data-testid$='-title'])",
		);
	await expect(cards).toHaveCount(7);
	const card = cards.first();
	const tile = card.getByTestId("system-page-icon");
	expect(await padding(card)).toBe("16px");
	expect((await box(tile)).width).toBe(28);
	for (const c of await cards.all()) {
		expect((await box(c)).height).toBeGreaterThanOrEqual(44);
	}
	expect(await overflows(cards)).toEqual([]);

	await page.setViewportSize({ width: 1280, height: 900 });
	expect(await padding(card)).toBe("20px");
	expect((await box(tile)).width).toBe(36);

	await page.setViewportSize({ width: 375, height: 900 });
	await card.click();
	await expect(page).toHaveURL("/system/usage");
	// The header keeps the shared header's tile (#1147).
	const header = page.getByTestId("settings-header-icon");
	expect((await box(header)).width).toBe(28);
});

test("loading skeleton matches the compact status cards (#925)", async ({
	page,
}) => {
	await page.setViewportSize({ width: 1280, height: 900 });
	let release = () => {};
	const held = new Promise<void>((resolve) => {
		release = resolve;
	});
	await mockMCP(page, {
		agent_list: AGENTS,
		job_list: JOBS,
		config_validate: [],
	});
	await page.route("/mcp", async (route) => {
		await held;
		await route.fallback();
	});
	await page.goto("/system/overview");

	const skeleton = page.getByTestId("overview-stat-skeleton").first();
	await expect(skeleton).toBeVisible();
	expect(await padding(skeleton)).toBe("16px");
	const skeletonHeight = (await box(skeleton)).height;

	release();
	const card = page
		.getByTestId("overview-stat-cards")
		.locator(":scope > *")
		.first();
	await expect(card).toBeVisible();
	expect(
		Math.abs((await box(card)).height - skeletonHeight),
	).toBeLessThanOrEqual(4);
});
