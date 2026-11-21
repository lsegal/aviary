import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

// The desktop sidebar collapses to an icon rail and resizes from its right
// edge, and remembers both per browser (#724).

function agent(name: string) {
	return { name, model: "anthropic/claude-sonnet-4-5", fallbacks: [] };
}

async function mockAgents(page: Page, names: string[]) {
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: [],
		job_list: [],
		config_validate: [],
		session_list: [],
		agent_file_list: [],
		config_get: { agents: names.map(agent) },
	});
}

async function sidebarWidth(page: Page): Promise<number> {
	const box = await page.getByTestId("sidebar").boundingBox();
	return Math.round(box?.width ?? 0);
}

/** Drags the sidebar's resize handle by dx pixels. */
async function dragHandle(page: Page, dx: number) {
	const box = await page.getByTestId("sidebar-resize").boundingBox();
	if (!box) throw new Error("no resize handle");
	const x = box.x + box.width / 2;
	const y = box.y + box.height / 2;
	await page.mouse.move(x, y);
	await page.mouse.down();
	await page.mouse.move(x + dx / 2, y);
	await page.mouse.move(x + dx, y);
	await page.mouse.up();
}

test.beforeEach(async ({ page }) => {
	await mockAgents(page, ["assistant"]);
});

test("the header toggle collapses the sidebar to a rail and the rail expands it", async ({
	page,
}) => {
	await page.goto("/system/overview");
	const main = page.locator("main");
	const before = (await main.boundingBox())?.width ?? 0;

	await page.getByTestId("sidebar-collapse").click();
	await expect(page.getByTestId("sidebar")).toHaveCount(0);
	await expect(page.getByTestId("sidebar-rail")).toBeVisible();
	await expect
		.poll(async () => (await main.boundingBox())?.width ?? 0)
		.toBeGreaterThan(before + 150);

	await page.getByTestId("sidebar-expand").click();
	await expect(page.getByTestId("sidebar")).toBeVisible();
	await expect(page.getByTestId("sidebar-rail")).toHaveCount(0);
	expect(await sidebarWidth(page)).toBe(240);
});

test("Cmd/Ctrl+B toggles the sidebar", async ({ page }) => {
	await page.goto("/system/overview");
	await expect(page.getByTestId("sidebar")).toBeVisible();

	await page.keyboard.press("ControlOrMeta+b");
	await expect(page.getByTestId("sidebar-rail")).toBeVisible();
	await expect(page.getByTestId("sidebar")).toHaveCount(0);

	await page.keyboard.press("ControlOrMeta+b");
	await expect(page.getByTestId("sidebar")).toBeVisible();
	await expect(page.getByTestId("sidebar-rail")).toHaveCount(0);
});

test("rail icons navigate and name their page in a tooltip", async ({
	page,
}) => {
	await page.goto("/system/overview");
	await page.getByTestId("sidebar-collapse").click();
	const rail = page.getByTestId("sidebar-rail");

	const files = rail.getByTestId("sidebar-rail-files");
	await files.hover();
	await expect(page.getByRole("tooltip")).toHaveText("Files");
	await files.click();
	await expect(page).toHaveURL("/agents/assistant/files");
	await expect(files).toHaveAttribute("aria-label", "Files");

	for (const [key, label] of [
		["chat", "Chats"],
		["projects", "GitHub Projects"],
		["tasks", "Scheduled Tasks"],
		["settings", "Agent Settings"],
		["system", "System"],
	]) {
		await rail.getByTestId(`sidebar-rail-${key}`).hover();
		await expect(page.getByRole("tooltip")).toHaveText(label);
	}
	await rail.getByTestId("sidebar-rail-system").click();
	await expect(page).toHaveURL("/system/overview");

	// The footer's connection, restart and logout stay reachable.
	await rail.getByTestId("sidebar-rail-health").hover();
	await expect(page.getByRole("tooltip")).toHaveText(
		/Connected|Connecting|Disconnected/,
	);
	await expect(rail.getByTestId("restart-button")).toBeVisible();
	await expect(rail.getByRole("button", { name: "Log out" })).toBeVisible();
});

test("with several agents the rail lists each agent and the active one's pages", async ({
	page,
}) => {
	await mockAgents(page, ["assistant", "coder"]);
	await page.goto("/agents/coder/chat");
	await page.getByTestId("sidebar-collapse").click();
	const rail = page.getByTestId("sidebar-rail");

	await expect(rail.getByTestId("sidebar-rail-agent")).toHaveText(["a", "c"]);
	await rail.locator("[data-agent=assistant]").hover();
	await expect(page.getByRole("tooltip")).toHaveText("assistant");
	await expect(rail.getByTestId("sidebar-rail-chat")).toHaveAttribute(
		"href",
		"/agents/coder/chat",
	);
	// Agents opens /agents, which moves on to the first agent's chat.
	await rail.getByTestId("sidebar-rail-agents").click();
	await expect(page).toHaveURL(/\/agents(\/assistant\/chat)?$/);
});

test("dragging the edge resizes the sidebar within limits and snaps to the rail", async ({
	page,
}) => {
	await page.goto("/system/overview");
	const handle = page.getByTestId("sidebar-resize");
	await expect(handle).toHaveAttribute("aria-label", "Resize sidebar");
	await expect(handle).toHaveCSS("cursor", "col-resize");

	await dragHandle(page, 100);
	expect(await sidebarWidth(page)).toBe(340);

	await dragHandle(page, 400);
	expect(await sidebarWidth(page)).toBe(480);

	await dragHandle(page, -270);
	expect(await sidebarWidth(page)).toBe(210);
	await dragHandle(page, -30);
	expect(await sidebarWidth(page)).toBe(200);

	// Double-clicking resets the default width.
	await handle.dblclick();
	expect(await sidebarWidth(page)).toBe(240);

	// The handle is focusable and arrow keys move it.
	await handle.focus();
	await page.keyboard.press("ArrowRight");
	expect(await sidebarWidth(page)).toBe(256);
	await page.keyboard.press("ArrowLeft");
	await page.keyboard.press("ArrowLeft");
	expect(await sidebarWidth(page)).toBe(224);
	await expect(handle).toHaveAttribute("aria-valuenow", "224");

	// Dragging well below the minimum collapses it; expanding restores the
	// width the drag started from.
	await dragHandle(page, -150);
	await expect(page.getByTestId("sidebar-rail")).toBeVisible();
	await page.getByTestId("sidebar-expand").click();
	expect(await sidebarWidth(page)).toBe(224);
});

test("the width and collapsed state survive a reload without a flash", async ({
	page,
}) => {
	await page.goto("/system/overview");
	await page.getByTestId("sidebar-resize").focus();
	await page.keyboard.press("End");
	expect(await sidebarWidth(page)).toBe(480);
	await page.reload();
	await expect(page.getByTestId("sidebar")).toBeVisible();
	expect(await sidebarWidth(page)).toBe(480);

	await page.keyboard.press("ControlOrMeta+b");
	await expect(page.getByTestId("sidebar-rail")).toBeVisible();

	// Record whether the expanded sidebar ever renders during the reload.
	await page.addInitScript(() => {
		const w = window as unknown as { sawSidebar: boolean };
		w.sawSidebar = false;
		new MutationObserver(() => {
			if (document.querySelector("[data-testid=sidebar]")) w.sawSidebar = true;
		}).observe(document, { childList: true, subtree: true });
	});
	await page.reload();
	await expect(page.getByTestId("sidebar-rail")).toBeVisible();
	expect(
		await page.evaluate(
			() => (window as unknown as { sawSidebar: boolean }).sawSidebar,
		),
	).toBe(false);

	await page.getByTestId("sidebar-expand").click();
	expect(await sidebarWidth(page)).toBe(480);
});

test.describe("at the narrowest desktop width", () => {
	test.use({ viewport: { width: 1024, height: 768 } });

	for (const path of [
		"/agents/assistant/chat",
		"/agents/assistant/files",
		"/agents/assistant/projects",
	]) {
		test(`${path} has no horizontal scrollbar at any sidebar width`, async ({
			page,
		}) => {
			const overflow = () =>
				page.evaluate(
					() =>
						document.documentElement.scrollWidth -
						document.documentElement.clientWidth,
				);
			await page.goto(path);
			await page.getByTestId("sidebar-resize").focus();
			await page.keyboard.press("End");
			expect(await sidebarWidth(page)).toBe(480);
			expect(await overflow()).toBeLessThanOrEqual(0);
			await page.keyboard.press("Home");
			expect(await overflow()).toBeLessThanOrEqual(0);
			await page.keyboard.press("ControlOrMeta+b");
			await expect(page.getByTestId("sidebar-rail")).toBeVisible();
			expect(await overflow()).toBeLessThanOrEqual(0);
		});
	}
});
