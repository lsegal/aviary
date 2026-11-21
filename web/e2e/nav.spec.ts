import { expect, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

test.beforeEach(async ({ page }) => {
	await setAuthToken(page);
	await mockMCP(page, { agent_list: [], job_list: [], config_validate: [] });
});

test("root redirects to overview", async ({ page }) => {
	await page.goto("/");
	await expect(page).toHaveURL("/system/overview");
});

test("sidebar lists Agents then System, with no Settings top level", async ({
	page,
}) => {
	await mockMCP(page, {
		agent_list: [],
		job_list: [],
		config_validate: [],
		config_get: {
			agents: [
				{ name: "assistant", model: "anthropic/claude-sonnet-4-5" },
				{ name: "coder", model: "anthropic/claude-sonnet-4-5" },
			],
		},
	});
	await page.goto("/system/overview");

	const nav = page.getByTestId("sidebar-nav");
	await expect(
		nav.locator("[data-testid=sidebar-agents], [data-testid=sidebar-system]"),
	).toHaveText(["Agents", "System"]);

	const agents = page.getByTestId("sidebar-agent-list");
	await expect(agents.locator("a[data-agent]")).toHaveText([
		"assistant",
		"coder",
	]);
	// Add agent is the "+" on the Agents row (#638).
	const addAgent = page.getByTestId("sidebar-add-agent");
	await expect(addAgent).toHaveAttribute("aria-label", "Add agent");
	await expect(addAgent).toHaveAttribute("title", "Add agent");
	await expect(addAgent).toHaveText("");

	const system = page.getByTestId("sidebar-system-list");
	await expect(system.getByRole("link")).toHaveText([
		"Overview",
		"Settings",
		"Projects",
		"Providers & Auth",
		"Usage",
		"Jobs",
	]);
	await page.getByTestId("sidebar-system-more").click();
	await expect(system.getByRole("link")).toHaveText([
		"Overview",
		"Settings",
		"Projects",
		"Providers & Auth",
		"Usage",
		"Jobs",
		"Tools",
		"Skills",
		"Sessions",
		"Models",
		"Logs",
		"Daemons",
	]);
});

test("an agent's pages are Chats, GitHub Projects, Tasks, Files and Agent Settings", async ({
	page,
}) => {
	await mockMCP(page, {
		agent_list: [{ id: "a1", name: "assistant", state: "idle" }],
		job_list: [],
		config_validate: [],
		session_list: [],
		config_get: {
			agents: [{ name: "assistant", model: "anthropic/claude-sonnet-4-5" }],
		},
	});
	await page.goto("/agents/assistant/chat");

	// With one agent its pages are the sidebar's top level (#538).
	const agent = page.getByTestId("sidebar-sole-agent");
	await expect(
		agent.getByRole("link").and(page.locator(":not([title])")),
	).toHaveText([
		"Chats",
		"GitHub Projects",
		"Tasks",
		"Files",
		"Agent Settings",
	]);
	// The agent comes from the sidebar: the chat has no agent switch.
	await expect(page.getByRole("button", { name: "assistant" })).toHaveCount(0);

	await agent
		.getByRole("link", { name: "Agent Settings", exact: true })
		.click();
	await expect(page).toHaveURL("/agents/assistant/settings");
	await expect(page.getByTestId("settings-title")).toHaveText(
		"assistant · Agent Settings",
	);
	// One page of stacked sections, with no tab bar (#684).
	await expect(
		page.getByRole("navigation", { name: "Agent settings" }),
	).toHaveCount(0);
	for (const section of ["General", "Permissions", "Channels", "Danger Zone"]) {
		await expect(
			page.getByRole("heading", { name: section, exact: true }),
		).toBeVisible();
	}
});

test("each agent's sidebar item reads Agent Settings", async ({ page }) => {
	await mockMCP(page, {
		agent_list: [],
		job_list: [],
		config_validate: [],
		session_list: [],
		config_get: {
			agents: [
				{ name: "assistant", model: "anthropic/claude-sonnet-4-5" },
				{ name: "coder", model: "anthropic/claude-sonnet-4-5" },
			],
		},
	});
	await page.goto("/agents/coder/chat");

	const agents = page.getByTestId("sidebar-agent-list");
	const settings = agents.getByRole("link", {
		name: "Agent Settings",
		exact: true,
	});
	await expect(settings).toHaveAttribute("href", "/agents/coder/settings");
	await expect(
		agents.getByRole("link", { name: "Settings", exact: true }),
	).toHaveCount(0);
	await settings.click();
	await expect(page.getByTestId("settings-title")).toHaveText(
		"coder · Agent Settings",
	);
});

test('the Add agent "+" opens a new agent\'s settings', async ({ page }) => {
	await mockMCP(page, {
		agent_list: [],
		job_list: [],
		config_validate: [],
		config_get: {
			agents: [{ name: "assistant", model: "anthropic/claude-sonnet-4-5" }],
		},
	});
	await page.goto("/system/overview");
	await page.getByTestId("sidebar-add-agent").click();

	await expect(page).toHaveURL("/agents/_1/settings");
	await expect(
		page
			.getByTestId("sidebar-agent-list")
			.getByRole("link", { name: "New agent" }),
	).toBeVisible();
	await expect(page.locator('input[placeholder="assistant"]')).toHaveValue("");
});

test("unauthenticated user is redirected to login", async ({ page }) => {
	// Clear token so auth guard fires.
	await page.addInitScript(() => localStorage.removeItem("aviary_token"));
	await page.goto("/system/overview");
	await expect(page).toHaveURL("/login");
});
