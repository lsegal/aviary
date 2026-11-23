import { expect, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";
import { systemList } from "./helpers/sidebar";

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
	// Agents are added from Settings, not the sidebar (#759).
	await expect(page.getByTestId("sidebar-add-agent")).toHaveCount(0);

	// System starts collapsed and lists every page, with no Overview child,
	// Settings or Providers & Auth, and no More expander (#822).
	const toggle = page.getByTestId("sidebar-system-toggle");
	await expect(toggle).toHaveAttribute("aria-expanded", "false");
	await expect(page.getByTestId("sidebar-system-list")).toHaveCount(0);
	const system = await systemList(page);
	await expect(system.getByRole("link")).toHaveText([
		"Usage",
		"Tools",
		"Skills",
		"Sessions",
		"Models",
		"Logs",
		"Daemons",
	]);
	await expect(page.getByTestId("sidebar-system-more")).toHaveCount(0);
});

test("the System section collapses to its header and stays collapsed across pages", async ({
	page,
}) => {
	await page.goto("/agents");

	const header = page.getByTestId("sidebar-system");
	const toggle = page.getByTestId("sidebar-system-toggle");
	const list = page.getByTestId("sidebar-system-list");
	await expect(toggle).toHaveAttribute("aria-expanded", "false");
	await expect(header).not.toHaveClass(/bg-white/);

	await toggle.click();
	await expect(toggle).toHaveAttribute("aria-expanded", "true");
	await expect(list.getByRole("link", { name: "Daemons" })).toBeVisible();

	await toggle.click();
	await expect(toggle).toHaveAttribute("aria-expanded", "false");
	await expect(list).toHaveCount(0);

	// The overview is the header's own page, so it doesn't open the section,
	// and the state outlives route changes.
	await header.click();
	await expect(page).toHaveURL("/system/overview");
	await expect(list).toHaveCount(0);
	await expect(header).toHaveClass(/bg-white/);

	// Expanded, the header is still the overview's own item.
	await toggle.click();
	await expect(toggle).toHaveAttribute("aria-expanded", "true");
	await expect(list.getByRole("link", { name: "Daemons" })).toBeVisible();
	await expect(header).toHaveClass(/bg-white/);
});

// The current System page shows in the sidebar however it was reached (#928).
test("a deep link to a System page opens the System section on that page", async ({
	page,
}) => {
	await page.goto("/system/logs");

	const toggle = page.getByTestId("sidebar-system-toggle");
	await expect(toggle).toHaveAttribute("aria-expanded", "true");
	await expect(page.getByTestId("sidebar-system-logs")).toHaveClass(
		/font-semibold/,
	);
	await expect(page.getByTestId("sidebar-system")).not.toHaveClass(/bg-white/);

	await page.reload();
	await expect(toggle).toHaveAttribute("aria-expanded", "true");
	await expect(page.getByTestId("sidebar-system-logs")).toHaveClass(
		/font-semibold/,
	);
});

test("navigating to a System page opens the System section, including back and forward", async ({
	page,
}) => {
	await page.goto("/system/overview");
	const toggle = page.getByTestId("sidebar-system-toggle");
	await expect(toggle).toHaveAttribute("aria-expanded", "false");

	// An overview card is an in-app link to the page.
	await page.getByTestId("overview-system-card-usage").click();
	await expect(page).toHaveURL("/system/usage");
	await expect(toggle).toHaveAttribute("aria-expanded", "true");
	await expect(page.getByTestId("sidebar-system-usage")).toHaveClass(
		/font-semibold/,
	);

	// Leaving System leaves the section as it was; collapse it from there.
	await page.getByTestId("sidebar-agents").click();
	await expect(page).toHaveURL("/agents");
	await expect(toggle).toHaveAttribute("aria-expanded", "true");
	await toggle.click();
	await expect(toggle).toHaveAttribute("aria-expanded", "false");

	await page.goBack();
	await expect(page).toHaveURL("/system/usage");
	await expect(toggle).toHaveAttribute("aria-expanded", "true");
	await expect(page.getByTestId("sidebar-system-usage")).toHaveClass(
		/font-semibold/,
	);

	await page.goForward();
	await expect(page).toHaveURL("/agents");
	await toggle.click();
	await expect(toggle).toHaveAttribute("aria-expanded", "false");
	await page.goBack();
	await expect(toggle).toHaveAttribute("aria-expanded", "true");
});

test("a manually collapsed System section stays collapsed until another System page", async ({
	page,
}) => {
	await page.goto("/system/logs");
	const header = page.getByTestId("sidebar-system");
	const toggle = page.getByTestId("sidebar-system-toggle");
	const list = page.getByTestId("sidebar-system-list");
	await expect(toggle).toHaveAttribute("aria-expanded", "true");

	await toggle.click();
	await expect(toggle).toHaveAttribute("aria-expanded", "false");
	await expect(list).toHaveCount(0);
	// The header stands in for the hidden current page.
	await expect(header).toHaveClass(/bg-white/);

	await header.click();
	await expect(page).toHaveURL("/system/overview");
	await page.getByTestId("overview-system-card-daemons").click();
	await expect(page).toHaveURL("/system/daemons");
	await expect(toggle).toHaveAttribute("aria-expanded", "true");
	await expect(page.getByTestId("sidebar-system-daemons")).toHaveClass(
		/font-semibold/,
	);
});

test("an agent's pages are Chats, Code Loop, Channels, Scheduled Tasks, Files and Agent Settings", async ({
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
		"Code Loop",
		"Channels",
		"Scheduled Tasks",
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
	for (const section of ["General", "Permissions", "Danger Zone"]) {
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

test("System → Settings' + Add agent opens a new agent's settings", async ({
	page,
}) => {
	await mockMCP(page, {
		agent_list: [],
		job_list: [],
		config_validate: [],
		config_get: {
			agents: [{ name: "assistant", model: "anthropic/claude-sonnet-4-5" }],
		},
	});
	await page.goto("/system/settings");
	await page.getByTestId("settings-add-agent").click();

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
	// A reachable server that requires a token.
	await page.route("/api/health", (route) =>
		route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify({ ok: true, version: "dev" }),
		}),
	);
	await page.goto("/system/overview");
	await expect(page).toHaveURL("/login");
	await expect(page.getByTestId("server-unavailable")).toHaveCount(0);
});

test("with several agents the phone main nav stays on one row", async ({
	page,
}) => {
	await page.setViewportSize({ width: 390, height: 844 });
	const names = ["assistant", "coder", "researcher", "reviewer", "writer"];
	await mockMCP(page, {
		agent_list: [],
		job_list: [],
		config_validate: [],
		config_get: {
			agents: names.map((name) => ({
				name,
				model: "anthropic/claude-sonnet-4-5",
			})),
		},
	});
	await page.goto("/agents/writer/chat");

	const nav = page.getByTestId("mobile-main-nav");
	const writer = nav.getByRole("link", { name: "writer", exact: true });
	await expect(writer).toBeVisible();
	const tops = await nav.evaluate((el) =>
		[...el.children]
			.filter(
				(c) => c instanceof HTMLElement && c.offsetParent && !c.ariaHidden,
			)
			.map((c) => Math.round(c.getBoundingClientRect().top)),
	);
	expect(new Set(tops).size).toBe(1);

	await nav.getByTestId("mobile-nav-more").click();
	const menu = page.getByTestId("mobile-nav-menu");
	await expect(menu.getByRole("menuitem", { name: "writer" })).toHaveCount(0);
	await menu.getByRole("menuitem", { name: "System" }).click();
	await expect(page).toHaveURL("/system/overview");
});

test("a server without token authentication skips the login prompt (#877)", async ({
	page,
}) => {
	await page.addInitScript(() => localStorage.removeItem("aviary_token"));
	await page.route("/api/health", (route) =>
		route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify({ ok: true, version: "dev", auth_disabled: true }),
		}),
	);
	await page.goto("/system/overview");
	await expect(page).toHaveURL("/system/overview");
	await page.goto("/login");
	await expect(page).not.toHaveURL(/\/login$/);
});

test("an unreachable server shows a reconnecting state instead of login (#968)", async ({
	page,
}) => {
	await page.addInitScript(() => localStorage.removeItem("aviary_token"));
	let serverUp = false;
	let failures = 0;
	await page.route("/api/health", (route) => {
		if (!serverUp) {
			failures++;
			// Alternate a network error and a proxy 5xx, as vite's proxy does.
			return failures % 2
				? route.abort("connectionrefused")
				: route.fulfill({ status: 502, body: "ECONNREFUSED" });
		}
		return route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify({ ok: true, version: "dev", auth_disabled: true }),
		});
	});
	await page.goto("/system/overview");
	const reconnecting = page.getByTestId("server-unavailable");
	await expect(reconnecting).toHaveText(/Server unavailable, reconnecting/);
	await expect.poll(() => failures).toBeGreaterThanOrEqual(2);
	await expect(page).not.toHaveURL(/\/login$/);
	await expect(page.getByLabel("Token")).toHaveCount(0);

	serverUp = true;
	await expect(reconnecting).toHaveCount(0);
	await expect(page).toHaveURL("/system/overview");
});
