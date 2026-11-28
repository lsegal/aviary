import { beforeEach, describe, expect, test } from "vitest";
import { commands, page } from "vitest/browser";
import { nextTick } from "vue";
import { currentURL, reload, renderApp } from "../component-test/app";
import { declarePages } from "../component-test/routes";
import { mockMCP } from "../component-test/server";
import {
	SIDEBAR_DEFAULT_WIDTH,
	useSidebarLayout,
} from "../composables/useSidebarLayout";

// The pages this file renders (see component-test/routes.ts).
declarePages(
	() => import("../views/ChatView.vue"),
	() => import("../views/OverviewView.vue"),
	() => import("../views/ProjectsView.vue"),
	() => import("../views/SettingsView.vue"),
);

// The desktop sidebar collapses to an icon rail and resizes from its right
// edge, and remembers both per browser (#724).

function agent(name: string) {
	return { name, model: "anthropic/claude-sonnet-4-5", fallbacks: [] };
}

function mockAgents(names: string[]) {
	mockMCP({
		agent_list: [],
		job_list: [],
		config_validate: [],
		session_list: [],
		agent_file_list: [],
		config_get: { agents: names.map(agent) },
	});
}

function sidebarWidth(): number {
	return Math.round(
		page.getByTestId("sidebar").element().getBoundingClientRect().width,
	);
}

function mainWidth(): number {
	return page.locator("main").element().getBoundingClientRect().width;
}

/** Drags the sidebar's resize handle by dx pixels. */
async function dragHandle(dx: number) {
	const box = page
		.getByTestId("sidebar-resize")
		.element()
		.getBoundingClientRect();
	const x = box.x + box.width / 2;
	const y = box.y + box.height / 2;
	await commands.mouseMove(x, y);
	await commands.mouseDown();
	await commands.mouseMove(x + dx / 2, y);
	await commands.mouseMove(x + dx, y);
	await commands.mouseUp();
}

function focusHandle() {
	(page.getByTestId("sidebar-resize").element() as HTMLElement).focus();
}

beforeEach(async () => {
	// The layout lives in module state, which outlasts a test (and reload()),
	// so start each test from a fresh browser's layout.
	const layout = useSidebarLayout();
	layout.collapsed.value = false;
	layout.width.value = SIDEBAR_DEFAULT_WIDTH;
	await nextTick();
	localStorage.clear();
	mockAgents(["assistant"]);
});

test("the header toggle collapses the sidebar to a rail and the rail expands it", async () => {
	await renderApp("/system/overview");
	await expect.element(page.locator("main")).toBeVisible();
	const before = mainWidth();

	await page.getByTestId("sidebar-collapse").click();
	await expect.element(page.getByTestId("sidebar")).not.toBeInTheDocument();
	await expect.element(page.getByTestId("sidebar-rail")).toBeVisible();
	await expect.poll(mainWidth).toBeGreaterThan(before + 150);

	await page.getByTestId("sidebar-expand").click();
	await expect.element(page.getByTestId("sidebar")).toBeVisible();
	await expect
		.element(page.getByTestId("sidebar-rail"))
		.not.toBeInTheDocument();
	expect(sidebarWidth()).toBe(240);
});

test("Cmd/Ctrl+B toggles the sidebar", async () => {
	await renderApp("/system/overview");
	await expect.element(page.getByTestId("sidebar")).toBeVisible();

	await commands.keyPress("ControlOrMeta+b");
	await expect.element(page.getByTestId("sidebar-rail")).toBeVisible();
	await expect.element(page.getByTestId("sidebar")).not.toBeInTheDocument();

	await commands.keyPress("ControlOrMeta+b");
	await expect.element(page.getByTestId("sidebar")).toBeVisible();
	await expect
		.element(page.getByTestId("sidebar-rail"))
		.not.toBeInTheDocument();
});

test("rail icons navigate and name their page in a tooltip", async () => {
	await renderApp("/system/overview");
	await page.getByTestId("sidebar-collapse").click();
	const rail = page.getByTestId("sidebar-rail");

	const files = rail.getByTestId("sidebar-rail-files");
	await files.hover();
	await expect.element(page.getByRole("tooltip")).toHaveText("Files");
	await files.click();
	await expect.poll(currentURL).toBe("/agents/assistant/files");
	await expect.element(files).toHaveAttribute("aria-label", "Files");

	for (const [key, label] of [
		["chat", "Chats"],
		["projects", "Code Loop"],
		["tasks", "Scheduled Tasks"],
		["settings", "Agent Settings"],
		["system", "System"],
	]) {
		await rail.getByTestId(`sidebar-rail-${key}`).hover();
		await expect.element(page.getByRole("tooltip")).toHaveText(label);
	}
	await rail.getByTestId("sidebar-rail-system").click();
	await expect.poll(currentURL).toBe("/system/overview");

	// The footer's connection and gear menu stay reachable.
	await expect
		.element(rail.getByTestId("connection-status"))
		.toHaveAttribute(
			"aria-label",
			expect.stringMatching(/Connected|Connecting|Disconnected/),
		);
	await rail.getByTestId("gear-menu-button").click();
	const items = page.getByTestId("gear-menu").getByRole("menuitem");
	await expect
		.poll(() => items.elements().map((el) => el.textContent?.trim()))
		.toEqual(["Settings", "Edit sidebar", "Restart", "Logout"]);
});

test("with several agents the rail lists each agent and the active one's pages", async () => {
	mockAgents(["assistant", "coder"]);
	await renderApp("/agents/coder/chat");
	await page.getByTestId("sidebar-collapse").click();
	const rail = page.getByTestId("sidebar-rail");

	await expect
		.poll(() =>
			rail
				.getByTestId("sidebar-rail-agent")
				.elements()
				.map((el) => el.textContent?.trim()),
		)
		.toEqual(["a", "c"]);
	await rail.locator("[data-agent=assistant]").hover();
	await expect.element(page.getByRole("tooltip")).toHaveText("assistant");
	await expect
		.element(rail.getByTestId("sidebar-rail-chat"))
		.toHaveAttribute("href", "/agents/coder/chat");
	// Agents opens /agents, which moves on to the first agent's chat.
	await rail.getByTestId("sidebar-rail-agents").click();
	await expect.poll(currentURL).toMatch(/\/agents(\/assistant\/chat)?$/);
});

test("dragging the edge resizes the sidebar within limits and snaps to the rail", async () => {
	await renderApp("/system/overview");
	const handle = page.getByTestId("sidebar-resize");
	await expect.element(handle).toHaveAttribute("aria-label", "Resize sidebar");
	await expect.element(handle).toHaveCSS("cursor", "col-resize");

	await dragHandle(100);
	expect(sidebarWidth()).toBe(340);

	await dragHandle(400);
	expect(sidebarWidth()).toBe(480);

	await dragHandle(-270);
	expect(sidebarWidth()).toBe(210);
	await dragHandle(-30);
	expect(sidebarWidth()).toBe(200);

	// Double-clicking resets the default width.
	await handle.dblClick();
	await expect.poll(sidebarWidth).toBe(240);

	// The handle is focusable and arrow keys move it.
	focusHandle();
	await commands.keyPress("ArrowRight");
	await expect.poll(sidebarWidth).toBe(256);
	await commands.keyPress("ArrowLeft");
	await commands.keyPress("ArrowLeft");
	await expect.poll(sidebarWidth).toBe(224);
	await expect.element(handle).toHaveAttribute("aria-valuenow", "224");

	// Dragging well below the minimum collapses it; expanding restores the
	// width the drag started from.
	await dragHandle(-150);
	await expect.element(page.getByTestId("sidebar-rail")).toBeVisible();
	await page.getByTestId("sidebar-expand").click();
	await expect.element(page.getByTestId("sidebar")).toBeVisible();
	expect(sidebarWidth()).toBe(224);
});

test("the width and collapsed state survive a reload without a flash", async () => {
	await renderApp("/system/overview");
	await expect.element(page.getByTestId("sidebar-resize")).toBeVisible();
	focusHandle();
	await commands.keyPress("End");
	await expect.poll(sidebarWidth).toBe(480);
	// reload() keeps module state, so check what a real reload reads, too.
	expect(storedLayout().width).toBe("480");
	await reload();
	await expect.element(page.getByTestId("sidebar")).toBeVisible();
	expect(sidebarWidth()).toBe(480);

	await commands.keyPress("ControlOrMeta+b");
	await expect.element(page.getByTestId("sidebar-rail")).toBeVisible();
	expect(storedLayout()).toEqual({ collapsed: "1", width: "480" });

	// Record whether the expanded sidebar ever renders during the reload.
	let sawSidebar = false;
	const observer = new MutationObserver(() => {
		if (document.querySelector("[data-testid=sidebar]")) sawSidebar = true;
	});
	observer.observe(document, { childList: true, subtree: true });
	await reload();
	await expect.element(page.getByTestId("sidebar-rail")).toBeVisible();
	observer.disconnect();
	expect(sawSidebar).toBe(false);

	await page.getByTestId("sidebar-expand").click();
	await expect.element(page.getByTestId("sidebar")).toBeVisible();
	expect(sidebarWidth()).toBe(480);
});

/** Rests the pointer on an empty part of the collapsed rail. */
async function restOnRail() {
	await expect.element(page.getByTestId("sidebar-rail")).toBeVisible();
	const box = page
		.getByTestId("sidebar-rail")
		.element()
		.getBoundingClientRect();
	await commands.mouseMove(box.x + box.width / 2, box.y + box.height - 220);
}

function storedLayout() {
	return {
		collapsed: localStorage.getItem("aviary.sidebar.collapsed"),
		width: localStorage.getItem("aviary.sidebar.width"),
	};
}

test("resting on the collapsed rail peeks the full sidebar as an overlay", async () => {
	await renderApp("/system/overview");
	await expect.element(page.getByTestId("sidebar-resize")).toBeVisible();
	focusHandle();
	await commands.keyPress("ArrowRight");
	await commands.keyPress("ControlOrMeta+b");
	await expect.element(page.getByTestId("sidebar-rail")).toBeVisible();
	const width = mainWidth();
	const stored = storedLayout();

	await restOnRail();
	const sidebar = page.getByTestId("sidebar");
	await expect.element(sidebar).toBeVisible();
	await expect.element(sidebar).toHaveAttribute("data-peek", "true");
	expect(sidebarWidth()).toBe(256);
	await expect
		.element(page.getByTestId("sidebar-resize"))
		.not.toBeInTheDocument();
	expect(mainWidth()).toBe(width);
	expect(storedLayout()).toEqual(stored);
	await expect.element(sidebar.getByTestId("sidebar-pin")).toBeVisible();

	// Leaving the overlay closes it after a short delay.
	await commands.mouseMove(800, 400);
	await expect.element(sidebar).toBeVisible();
	await expect.element(sidebar).not.toBeInTheDocument();
	await expect.element(page.getByTestId("sidebar-rail")).toBeVisible();

	// Escape closes it too.
	await restOnRail();
	await expect.element(sidebar).toBeVisible();
	await commands.keyPress("Escape");
	await expect.element(sidebar).not.toBeInTheDocument();
	// The pointer still rests on the rail, which would peek again after its
	// delay; in Playwright the next hover beats that, so move off first.
	await commands.mouseMove(800, 400);

	// Resting on an icon shows its tooltip instead of the overlay.
	await page.getByTestId("sidebar-rail-files").hover();
	await expect.element(page.getByRole("tooltip")).toHaveText("Files");
	await new Promise((r) => setTimeout(r, 500));
	await expect.element(sidebar).not.toBeInTheDocument();

	// The peek is never saved: a reload restores the rail.
	await restOnRail();
	await expect.element(sidebar).toBeVisible();
	await reload();
	await expect.element(page.getByTestId("sidebar-rail")).toBeVisible();
	await expect.element(sidebar).not.toBeInTheDocument();
	expect(storedLayout()).toEqual(stored);
});

test("navigating from the peek closes it and its toggle expands the sidebar", async () => {
	await renderApp("/system/overview");
	await expect.element(page.getByTestId("sidebar")).toBeVisible();
	await commands.keyPress("ControlOrMeta+b");
	await restOnRail();
	const sidebar = page.getByTestId("sidebar");
	await sidebar.getByTestId("sidebar-row-files").getByRole("link").click();
	await expect.poll(currentURL).toBe("/agents/assistant/files");
	await expect.element(sidebar).not.toBeInTheDocument();
	await expect.element(page.getByTestId("sidebar-rail")).toBeVisible();

	// The links in the overlay are reachable by keyboard, and tabbing out
	// closes it.
	await restOnRail();
	await expect.element(sidebar).toBeVisible();
	(
		sidebar
			.getByTestId("sidebar-row-chat")
			.getByRole("link")
			.element() as HTMLElement
	).focus();
	(page.getByTestId("sidebar-rail-system").element() as HTMLElement).focus();
	await expect.element(sidebar).not.toBeInTheDocument();

	await restOnRail();
	await sidebar.getByTestId("sidebar-pin").click();
	await expect
		.element(page.getByTestId("sidebar-rail"))
		.not.toBeInTheDocument();
	await expect.element(sidebar).not.toHaveAttribute("data-peek");
	await expect.element(page.getByTestId("sidebar-resize")).toBeVisible();
	expect(storedLayout().collapsed).toBe("0");
});

describe("at the narrowest desktop width", () => {
	beforeEach(async () => {
		await page.viewport(1024, 768);
	});

	for (const path of [
		"/agents/assistant/chat",
		"/agents/assistant/files",
		"/agents/assistant/projects",
	]) {
		test(`${path} has no horizontal scrollbar at any sidebar width`, async () => {
			const overflow = () =>
				document.documentElement.scrollWidth -
				document.documentElement.clientWidth;
			await renderApp(path);
			await expect.element(page.getByTestId("sidebar-resize")).toBeVisible();
			focusHandle();
			await commands.keyPress("End");
			await expect.poll(sidebarWidth).toBe(480);
			expect(overflow()).toBeLessThanOrEqual(0);
			await commands.keyPress("Home");
			await expect.poll(sidebarWidth).toBe(200);
			expect(overflow()).toBeLessThanOrEqual(0);
			await commands.keyPress("ControlOrMeta+b");
			await expect.element(page.getByTestId("sidebar-rail")).toBeVisible();
			expect(overflow()).toBeLessThanOrEqual(0);
		});
	}
});
