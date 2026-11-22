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

test("recovers when the MCP session expires", async ({ page }) => {
	let initializeCount = 0;
	let firstToolCallFails = true;

	await page.route("/mcp", async (route) => {
		const body = route.request().postDataJSON() as {
			jsonrpc: string;
			id?: number;
			method: string;
			params?: { name?: string };
		};

		if (body.method === "initialize") {
			initializeCount += 1;
			return route.fulfill({
				status: 200,
				headers: {
					"Content-Type": "application/json",
					"Mcp-Session-Id": `mock-session-${initializeCount}`,
				},
				body: JSON.stringify({
					jsonrpc: "2.0",
					id: body.id,
					result: {
						protocolVersion: "2024-11-05",
						capabilities: {},
						serverInfo: { name: "aviary-mock", version: "0.0.0" },
					},
				}),
			});
		}

		if (body.method === "notifications/initialized") {
			return route.fulfill({ status: 200, body: "{}" });
		}

		if (body.method === "tools/call") {
			if (firstToolCallFails) {
				firstToolCallFails = false;
				return route.fulfill({ status: 404, body: "session not found" });
			}

			const fixtures = {
				agent_list: AGENTS,
				job_list: JOBS,
				config_validate: [],
			};
			const toolName = body.params?.name as keyof typeof fixtures;
			return route.fulfill({
				status: 200,
				contentType: "application/json",
				body: JSON.stringify({
					jsonrpc: "2.0",
					id: body.id,
					result: {
						content: [
							{ type: "text", text: JSON.stringify(fixtures[toolName] ?? []) },
						],
					},
				}),
			});
		}

		return route.fulfill({ status: 200, body: "{}" });
	});

	await page.goto("/system/overview");

	await expect(page.getByText("2").first()).toBeVisible();
	await expect(page.getByText("No issues found")).toBeVisible();
	expect(initializeCount).toBe(2);
});

test("retries a transient MCP 502 during overview load", async ({ page }) => {
	let firstToolCallFails = true;

	await page.route("/mcp", async (route) => {
		const body = route.request().postDataJSON() as {
			jsonrpc: string;
			id?: number;
			method: string;
			params?: { name?: string };
		};

		if (body.method === "initialize") {
			return route.fulfill({
				status: 200,
				headers: {
					"Content-Type": "application/json",
					"Mcp-Session-Id": "mock-session",
				},
				body: JSON.stringify({
					jsonrpc: "2.0",
					id: body.id,
					result: {
						protocolVersion: "2024-11-05",
						capabilities: {},
						serverInfo: { name: "aviary-mock", version: "0.0.0" },
					},
				}),
			});
		}

		if (body.method === "notifications/initialized") {
			return route.fulfill({ status: 200, body: "{}" });
		}

		if (body.method === "tools/call") {
			if (firstToolCallFails) {
				firstToolCallFails = false;
				return route.fulfill({ status: 502, body: "bad gateway" });
			}

			const fixtures = {
				agent_list: AGENTS,
				job_list: JOBS,
				config_validate: [],
			};
			const toolName = body.params?.name as keyof typeof fixtures;
			return route.fulfill({
				status: 200,
				contentType: "application/json",
				body: JSON.stringify({
					jsonrpc: "2.0",
					id: body.id,
					result: {
						content: [
							{ type: "text", text: JSON.stringify(fixtures[toolName] ?? []) },
						],
					},
				}),
			});
		}

		return route.fulfill({ status: 200, body: "{}" });
	});

	await page.goto("/system/overview");

	await expect(page.getByText("2").first()).toBeVisible();
	await expect(page.getByText("No issues found")).toBeVisible();
});

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
	const header = page.locator("main").getByTestId("system-page-icon").first();
	expect(await header.locator("svg").innerHTML()).toBe(cardIcon);

	await page.keyboard.press("ControlOrMeta+b");
	const railIcon = page
		.getByTestId("sidebar-rail-system-usage")
		.getByTestId("system-page-icon");
	expect(await railIcon.locator("svg").innerHTML()).toBe(cardIcon);
});

test("stat cards are compact on phones and unchanged on desktop (#923)", async ({
	page,
}) => {
	await mockMCP(page, {
		agent_list: AGENTS,
		job_list: JOBS,
		config_validate: [],
	});
	const cardSizes = () =>
		page
			.locator("main a[href='/agents']", { hasText: "Agents" })
			.evaluate((card) => {
				const css = getComputedStyle(card);
				const value = card.children[1] as HTMLElement;
				return {
					padding: `${css.paddingTop} ${css.paddingLeft}`,
					value: getComputedStyle(value).fontSize,
				};
			});
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto("/system/overview");
	await expect(page.getByText("Config Validation")).toBeVisible();
	expect(await cardSizes()).toEqual({ padding: "12px 16px", value: "24px" });
	await page.setViewportSize({ width: 1280, height: 900 });
	expect(await cardSizes()).toEqual({ padding: "20px 20px", value: "30px" });
});
