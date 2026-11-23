import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

// Customizing the navigation (#1003): the Edit sidebar dialog reorders and
// hides pages, the result persists on the server with a localStorage
// fallback, and Reset restores the built-in order.

const DEFAULT_PAGES = [
	"chat",
	"projects",
	"channels",
	"tasks",
	"files",
	"settings",
];

async function mockAgents(page: Page, names: string[]) {
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: [],
		job_list: [],
		config_validate: [],
		session_list: [],
		config_get: {
			agents: names.map((name) => ({
				name,
				model: "anthropic/claude-sonnet-4-5",
				fallbacks: [],
			})),
		},
	});
}

/** The sole agent's sidebar page rows, by key, in order. */
async function sidebarPages(page: Page): Promise<string[]> {
	const rows = page
		.getByTestId("sidebar-sole-agent")
		.locator('[data-testid^="sidebar-row-"]');
	await expect(rows.first()).toBeVisible();
	return (await rows.evaluateAll((els) =>
		els.map((el) =>
			el.getAttribute("data-testid")?.replace("sidebar-row-", ""),
		),
	)) as string[];
}

async function systemPages(page: Page): Promise<string[]> {
	const links = page
		.getByTestId("sidebar-system-list")
		.locator('[data-testid^="sidebar-system-"]');
	await expect(links.first()).toBeVisible();
	return (await links.evaluateAll((els) =>
		els.map((el) =>
			el.getAttribute("data-testid")?.replace("sidebar-system-", ""),
		),
	)) as string[];
}

async function openEditor(page: Page) {
	await page.getByTestId("sidebar").getByTestId("gear-menu-button").click();
	await page.getByTestId("gear-menu-edit-nav").click();
	await expect(page.getByTestId("nav-edit-dialog")).toBeVisible();
}

function editRow(page: Page, list: string, key: string) {
	return page.getByTestId(`nav-edit-${list}`).locator(`[data-key="${key}"]`);
}

test.use({ viewport: { width: 1280, height: 900 } });

test("pages reorder by keyboard and hide, and persist on the server", async ({
	page,
}) => {
	await mockAgents(page, ["assistant"]);
	await page.goto("/system/overview");
	expect(await sidebarPages(page)).toEqual(DEFAULT_PAGES);

	await openEditor(page);
	const up = editRow(page, "agentPages", "tasks").getByTestId("nav-edit-up");
	await up.focus();
	await page.keyboard.press("Enter");
	// Focus follows the moved row, so the key repeats.
	await expect(up).toBeFocused();
	await page.keyboard.press("Enter");
	await editRow(page, "agentPages", "files")
		.getByTestId("nav-edit-visibility")
		.click();
	await expect(editRow(page, "agentPages", "files")).toHaveAttribute(
		"data-hidden",
		"true",
	);
	await page.getByTestId("nav-edit-done").click();
	await expect(page.getByTestId("nav-edit-dialog")).toHaveCount(0);

	const arranged = ["chat", "tasks", "projects", "channels", "settings"];
	expect(await sidebarPages(page)).toEqual(arranged);

	// A fresh browser on another device reads the server's copy.
	await page.evaluate(() => localStorage.removeItem("aviary.nav.prefs"));
	await page.reload();
	expect(await sidebarPages(page)).toEqual(arranged);

	// The hidden page stays reachable through Show hidden.
	const toggle = page.getByTestId("sidebar-show-hidden");
	await expect(toggle).toHaveText("Show 1 hidden page");
	await toggle.click();
	expect(await sidebarPages(page)).toEqual([
		...arranged.slice(0, 4),
		"files",
		"settings",
	]);
	await toggle.click();
	expect(await sidebarPages(page)).toEqual(arranged);
});

test("System pages reorder by dragging their handle", async ({ page }) => {
	await mockAgents(page, ["assistant"]);
	await page.goto("/system/usage");
	expect((await systemPages(page)).slice(0, 3)).toEqual([
		"usage",
		"tools",
		"skills",
	]);

	await openEditor(page);
	const handle = editRow(page, "systemPages", "skills").getByTestId(
		"nav-edit-handle",
	);
	const target = await editRow(page, "systemPages", "usage").boundingBox();
	await handle.hover();
	await page.mouse.down();
	await page.mouse.move((target?.x ?? 0) + 20, (target?.y ?? 0) + 4, {
		steps: 8,
	});
	await page.mouse.up();
	await page.getByTestId("nav-edit-done").click();

	expect((await systemPages(page)).slice(0, 3)).toEqual([
		"skills",
		"usage",
		"tools",
	]);
	await page.reload();
	expect((await systemPages(page)).slice(0, 3)).toEqual([
		"skills",
		"usage",
		"tools",
	]);
});

test("Reset to default restores the order on the server and locally", async ({
	page,
}) => {
	await mockAgents(page, ["assistant"]);
	const deletes: string[] = [];
	page.on("request", (req) => {
		if (
			req.url().endsWith("/api/preferences/navigation") &&
			req.method() === "DELETE"
		)
			deletes.push(req.method());
	});
	await page.goto("/system/overview");
	await openEditor(page);
	const reset = page.getByTestId("nav-edit-reset");
	await expect(reset).toBeDisabled();
	await editRow(page, "agentPages", "settings")
		.getByTestId("nav-edit-up")
		.click();
	await editRow(page, "agentPages", "chat")
		.getByTestId("nav-edit-visibility")
		.click();
	await expect(reset).toBeEnabled();

	await reset.click();
	await expect(reset).toBeDisabled();
	await page.getByTestId("nav-edit-done").click();
	expect(await sidebarPages(page)).toEqual(DEFAULT_PAGES);
	await expect(page.getByTestId("sidebar-show-hidden")).toHaveCount(0);
	expect(deletes).toHaveLength(1);
	expect(
		await page.evaluate(() => localStorage.getItem("aviary.nav.prefs")),
	).toBeNull();

	await page.reload();
	expect(await sidebarPages(page)).toEqual(DEFAULT_PAGES);
});

test("without the server's preferences, localStorage keeps them", async ({
	page,
}) => {
	await mockAgents(page, ["assistant"]);
	await page.route("/api/preferences/navigation", (route) =>
		route.fulfill({ status: 500, body: "unavailable" }),
	);
	await page.goto("/system/overview");
	await openEditor(page);
	await editRow(page, "agentPages", "channels")
		.getByTestId("nav-edit-visibility")
		.click();
	await page.getByTestId("nav-edit-done").click();

	await page.reload();
	expect(await sidebarPages(page)).toEqual(
		DEFAULT_PAGES.filter((k) => k !== "channels"),
	);
});

test("a page missing from a saved order takes its default place", async ({
	page,
}) => {
	await mockAgents(page, ["assistant"]);
	// Saved by an older version: no Files page, plus one since removed.
	const saved = {
		order: {
			agentPages: ["settings", "tasks", "gone", "chat", "projects", "channels"],
		},
		hidden: {},
	};
	await page.route("/api/preferences/navigation", (route) =>
		route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify(saved),
		}),
	);
	await page.goto("/system/overview");
	await expect
		.poll(() => sidebarPages(page))
		.toEqual(["settings", "tasks", "files", "chat", "projects", "channels"]);
});

test.describe("top buttons", () => {
	test("reorder on the phone row and the sidebar's agent list", async ({
		page,
	}) => {
		await mockAgents(page, ["assistant", "coder"]);
		await page.setViewportSize({ width: 390, height: 844 });
		await page.goto("/agents/assistant/chat");
		const row = page.getByTestId("mobile-main-nav");
		const keys = () =>
			row
				.locator("a")
				.evaluateAll((els) => els.map((el) => el.getAttribute("data-key")));
		await expect
			.poll(keys)
			.toEqual(["agent:assistant", "agent:coder", "system"]);

		await page.locator("header").getByTestId("gear-menu-button").click();
		await page.getByTestId("gear-menu-edit-nav").click();
		const system = editRow(page, "top", "system");
		await system.getByTestId("nav-edit-up").click();
		await system.getByTestId("nav-edit-up").click();
		// The top row keeps every item: there is nothing to hide.
		await expect(system.getByTestId("nav-edit-visibility")).toHaveCount(0);
		await page.getByTestId("nav-edit-done").click();
		await expect
			.poll(keys)
			.toEqual(["system", "agent:assistant", "agent:coder"]);

		await editRowAgain(page);
		await expect
			.poll(keys)
			.toEqual(["system", "agent:coder", "agent:assistant"]);

		await page.setViewportSize({ width: 1280, height: 900 });
		const agents = page
			.getByTestId("sidebar-agent-list")
			.locator("a[data-agent]");
		await expect
			.poll(() =>
				agents.evaluateAll((els) =>
					els.map((el) => el.getAttribute("data-agent")),
				),
			)
			.toEqual(["coder", "assistant"]);
	});
});

async function editRowAgain(page: Page) {
	await page.locator("header").getByTestId("gear-menu-button").click();
	await page.getByTestId("gear-menu-edit-nav").click();
	await editRow(page, "top", "agent:coder").getByTestId("nav-edit-up").click();
	await page.keyboard.press("Escape");
	await expect(page.getByTestId("nav-edit-dialog")).toHaveCount(0);
}
