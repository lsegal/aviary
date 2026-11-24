import { expect, type Page, test } from "@playwright/test";
import { mockMCP } from "./helpers/mockMCP";

// An `aviary watch` session: no token, and only Code Loop and Scheduled
// Tasks (#991).
async function mockWatchSession(
	page: Page,
	fixtures: Parameters<typeof mockMCP>[1] = {},
) {
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
		...fixtures,
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
	// Code Loop keeps its blue + to add a project, as outside a watch (#1197).
	const plus = page.getByTestId("sidebar-add-project");
	await expect(plus).toHaveAttribute("href", "/agents/watch/projects?add=1");
	await sidebar.getByRole("link", { name: "Code Loop", exact: true }).hover();
	await expect
		.poll(() => plus.evaluate((el) => Number(getComputedStyle(el).opacity)))
		.toBe(1);
});

test("a watch session's Code Loop has the prompt chat (#1195)", async ({
	page,
}) => {
	const runs: Record<string, unknown>[] = [];
	await mockWatchSession(page, {
		session_messages: () =>
			runs.flatMap((run, i) => [
				{
					id: `u${i}`,
					session_id: "projects-chat",
					role: "user",
					content: run.message,
					timestamp: new Date().toISOString(),
				},
				{
					id: `a${i}`,
					session_id: "projects-chat",
					role: "assistant",
					content: "Nothing is running yet.",
					timestamp: new Date().toISOString(),
				},
			]),
		agent_run: (args?: Record<string, unknown>) => {
			runs.push(args ?? {});
			return { stream: ["Nothing is running yet."], result: "" };
		},
	});
	await page.goto("/agents/watch/projects/owner%2Frepo");
	await expect(page.getByTestId("project-chat")).toHaveCount(0);
	await page.getByTestId("prompt-chat-toggle").click();
	const chat = page.getByTestId("project-chat");
	await expect(chat).toContainText("Ask the projects agent about owner/repo");
	// A watch session serves no speech-to-text.
	await expect(chat.getByTestId("voice-button")).toHaveCount(0);

	const input = chat.getByPlaceholder("Describe a task or ask a question");
	await expect(input).toBeEnabled();
	await input.fill("What is running?");
	await input.press("Enter");
	await expect.poll(() => runs.length).toBe(1);
	// The same Code Loop agent session as the watch TUI's chat.
	expect(runs[0]).toMatchObject({
		name: "projects",
		session: "projects-chat",
		message: "What is running?",
	});
	await expect(chat).toContainText("Nothing is running yet.");

	await chat.getByRole("button", { name: "Close prompt chat" }).click();
	await expect(page.getByTestId("project-chat")).toHaveCount(0);
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

test("a watch session's Code Loop shows every project's schedules with a Scheduled filter (#1108)", async ({
	page,
}) => {
	const task = (project: string, name: string) => ({
		id: `watch/${name}`,
		name,
		agent_name: "watch",
		type: "project",
		project,
		enabled: true,
		trigger_type: "every",
		every: "1h",
	});
	const issue = {
		key: "owner/repo#7",
		target: "owner/repo",
		number: 7,
		title: "Fix the parser",
		status: "active",
		state: "running",
		kind: "issue",
		actions: {},
	};
	await mockWatchSession(page, {
		config_get: {
			agents: [
				{
					name: "watch",
					projects: [{ target: "owner/repo" }, { target: "owner/other" }],
				},
			],
		},
		project_status: {
			states: { starting: 0, paused: 0, running: 1, done: 0, failed: 0 },
			targets: ["owner/repo", "owner/other"],
		},
		task_list: [task("owner/repo", "release"), task("owner/other", "nightly")],
		project_job_list: (args?: Record<string, unknown>) => {
			const jobs = (args?.kinds as string[] | undefined)?.includes("schedule")
				? []
				: [issue];
			return { jobs, total: jobs.length, offset: 0, limit: 50 };
		},
	});
	await page.goto("/agents/watch/projects");
	const scheduled = page.getByTestId("scheduled-card");
	const jobs = page.getByTestId("job-card");
	await expect(scheduled).toHaveCount(2);
	await expect(scheduled.getByTestId("scheduled-project")).toHaveText([
		"owner/repo",
		"owner/other",
	]);
	await expect(jobs).toHaveCount(1);
	await expect(page.getByTestId("job-total")).toHaveText("3 items");

	const chip = page
		.getByRole("group", { name: "Job filter" })
		.getByRole("button", { name: /^Scheduled/ });
	await expect(chip).toHaveText(/^Scheduled\s*2$/);
	await chip.click();
	await expect(page).toHaveURL(/[?&]state=scheduled$/);
	await expect(scheduled).toHaveCount(2);
	await expect(jobs).toHaveCount(0);
	await chip.click();
	await expect(scheduled).toHaveCount(2);
	await expect(jobs).toHaveCount(1);
});

test("a watch session keeps Settings → Display, behind a Display-only gear (#1093)", async ({
	page,
}) => {
	await mockWatchSession(page);
	await page.goto("/agents/watch/projects");
	await page.getByTestId("sidebar").getByTestId("gear-menu-button").click();
	const menu = page.getByTestId("gear-menu");
	await expect(menu.getByRole("menuitem")).toHaveText(["Display"]);
	await menu.getByTestId("gear-menu-display").click();
	await expect(page).toHaveURL("/system/display");
	await expect(page.getByTestId("settings-display")).toBeVisible();
	await expect(page.getByTestId("settings-tabs").getByRole("tab")).toHaveText([
		"Display",
	]);
	await page
		.getByTestId("display-theme")
		.locator('[data-value="dark"]')
		.click();
	await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
});
