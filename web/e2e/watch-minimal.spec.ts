import { expect, type Page, test } from "@playwright/test";
import { mockMCP } from "./helpers/mockMCP";

// An `aviary watch` session: no token, and only Code Loop and Scheduled
// Tasks (#991).
async function mockWatchSession(page: Page) {
	await page.addInitScript(() => localStorage.removeItem("aviary_token"));
	await page.route("/api/health", (route) =>
		route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify({
				ok: true,
				version: "dev",
				auth_disabled: true,
				minimal: true,
			}),
		}),
	);
	await mockMCP(page, {
		agent_list: [{ name: "watch" }],
		job_list: [],
		task_list: [],
		task_upcoming: [],
		config_validate: [],
		config_get: {
			agents: [{ name: "watch", projects: [{ target: "owner/repo" }] }],
		},
		project_status: {
			states: { starting: 0, paused: 0, running: 0, done: 0, failed: 0 },
			targets: ["owner/repo"],
		},
		project_job_list: { jobs: [], total: 0, offset: 0, limit: 50 },
	});
}

test("a watch session lands on Code Loop with no login prompt", async ({
	page,
}) => {
	await mockWatchSession(page);
	await page.goto("/");
	await expect(page).toHaveURL("/agents/watch/projects");
	await expect(page.getByLabel("Token")).toHaveCount(0);
});

test("a watch session's nav shows only Code Loop and Scheduled Tasks", async ({
	page,
}) => {
	await mockWatchSession(page);
	await page.goto("/agents/watch/projects");
	const sidebar = page.getByTestId("sidebar-nav");
	await expect(
		sidebar.getByRole("link", { name: "Code Loop", exact: true }),
	).toBeVisible();
	await expect(
		sidebar.getByRole("link", { name: "Scheduled Tasks", exact: true }),
	).toBeVisible();
	for (const label of [
		"Chats",
		"Channels",
		"Files",
		"Agent Settings",
		"System",
	]) {
		await expect(
			sidebar.getByRole("link", { name: label, exact: true }),
		).toHaveCount(0);
	}
	await expect(page.getByTestId("sidebar-add-project")).toHaveCount(0);
	await expect(page.getByTestId("prompt-chat-toggle")).toHaveCount(0);
});

test("a watch session redirects pages outside Code Loop and tasks", async ({
	page,
}) => {
	await mockWatchSession(page);
	for (const path of [
		"/agents/watch/chat",
		"/agents/watch/settings",
		"/agents/watch/channels",
		"/system/overview",
		"/system/providers",
		"/onboarding",
	]) {
		await page.goto(path);
		await expect(page).toHaveURL("/agents/watch/projects");
	}
	await page.goto("/agents/watch/tasks");
	await expect(page).toHaveURL("/agents/watch/tasks");
	await page.goto("/projects/settings");
	await expect(page).toHaveURL("/projects/settings");
});

test("saved navigation preferences can't bring back pages minimal mode hides (#1003)", async ({
	page,
}) => {
	await mockWatchSession(page);
	// Chats first and Code Loop hidden, as saved by the full UI.
	await page.route("/api/preferences/navigation", (route) =>
		route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify({
				order: { agentPages: ["chat", "tasks", "projects"] },
				hidden: { agentPages: ["projects"] },
			}),
		}),
	);
	await page.goto("/agents/watch/tasks");
	const sole = page.getByTestId("sidebar-sole-agent");
	await expect(
		sole.getByRole("link", { name: "Scheduled Tasks", exact: true }),
	).toBeVisible();
	await expect(
		sole.getByRole("link", { name: "Chats", exact: true }),
	).toHaveCount(0);
	await expect(
		sole.getByRole("link", { name: "Code Loop", exact: true }),
	).toHaveCount(0);

	// Showing hidden pages brings back only what minimal mode serves.
	await page.getByTestId("sidebar-show-hidden").click();
	await expect(
		sole.getByRole("link", { name: "Code Loop", exact: true }),
	).toBeVisible();
	await expect(
		sole.getByRole("link", { name: "Chats", exact: true }),
	).toHaveCount(0);
	const keys = await sole
		.locator('[data-testid^="sidebar-row-"]')
		.evaluateAll((els) => els.map((el) => el.getAttribute("data-testid")));
	expect(keys).toEqual(["sidebar-row-tasks", "sidebar-row-projects"]);
});
