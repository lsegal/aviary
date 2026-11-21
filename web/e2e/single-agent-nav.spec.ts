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
	for (const label of ["Chats", "Settings", "Tasks", "GitHub Projects"]) {
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
