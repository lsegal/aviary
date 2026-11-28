import { afterEach, beforeEach, describe, expect, test } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { goto, reload, renderApp } from "../component-test/app";
import { declarePages } from "../component-test/routes";
import { requestLog } from "../component-test/server";
import { useNavPrefs } from "../composables/useNavPrefs";
import {
	DEFAULT_PAGES,
	editRow,
	mobileRowKeys,
	mockAgents,
	openEditor,
	rowKeys,
	sidebarPages,
	systemPages,
} from "./helpers/navCustomize";

// The pages this file renders (see component-test/routes.ts).
declarePages(
	() => import("../views/ChatView.vue"),
	() => import("../views/OverviewView.vue"),
	() => import("../views/SettingsView.vue"),
	() => import("../views/UsageView.vue"),
);

// Customizing the navigation (#1003): the sidebar editor, in Settings →
// Display (#1093), reorders and hides pages, the result persists on the
// server with a localStorage fallback, and Reset restores the built-in
// order.
//
// The preferences live in module state, read from this browser's copy when
// the module loads and from the server's on first use; both outlast a test
// and reload(). So the scenarios that need that first load have their own
// files (nav-customize-*.component.test.ts), and here each test starts from
// the built-in order. Touch dragging stays in web/e2e/nav-customize.spec.ts:
// it needs real touch input.

function box(loc: Locator): DOMRect {
	return loc.element().getBoundingClientRect();
}

/** Moves the pointer to the middle of an element, as locator.hover does. */
async function pointAt(loc: Locator) {
	loc.element().scrollIntoView({ block: "nearest" });
	const b = box(loc);
	await commands.mouseMove(b.x + b.width / 2, b.y + b.height / 2);
}

function focus(loc: Locator) {
	(loc.element() as HTMLElement).focus();
}

beforeEach(async () => {
	await page.viewport(1280, 900);
});

afterEach(() => {
	const nav = useNavPrefs();
	nav.reset();
	nav.showHidden.value = false;
});

test("pages reorder by keyboard and hide, and persist on the server", async () => {
	mockAgents(["assistant"]);
	await renderApp("/system/overview");
	expect(await sidebarPages()).toEqual(DEFAULT_PAGES);

	await openEditor();
	const up = editRow("agentPages", "tasks").getByTestId("nav-edit-up");
	focus(up);
	await commands.keyPress("Enter");
	// Focus follows the moved row, so the key repeats.
	await expect.element(up).toHaveFocus();
	await commands.keyPress("Enter");
	await editRow("agentPages", "files")
		.getByTestId("nav-edit-visibility")
		.click();
	await expect
		.element(editRow("agentPages", "files"))
		.toHaveAttribute("data-hidden", "true");

	const arranged = ["chat", "tasks", "projects", "channels", "settings"];
	await expect.poll(sidebarPages).toEqual(arranged);

	// A fresh browser on another device reads the server's copy. reload()
	// keeps the module's copy, so check the server holds the same prefs;
	// nav-customize-server-copy shows a first load applies them.
	const server = await (await fetch("/api/preferences/navigation")).json();
	expect(server).toEqual(JSON.parse(JSON.stringify(useNavPrefs().prefs.value)));
	expect(server.hidden.agentPages).toEqual(["files"]);
	localStorage.removeItem("aviary.nav.prefs");
	await reload();
	await expect.poll(sidebarPages).toEqual(arranged);

	// The hidden page stays reachable through Show hidden.
	const toggle = page.getByTestId("sidebar-show-hidden");
	await expect.element(toggle).toHaveText("Show 1 hidden page");
	await toggle.click();
	await expect
		.poll(sidebarPages)
		.toEqual([...arranged.slice(0, 4), "files", "settings"]);
	await toggle.click();
	await expect.poll(sidebarPages).toEqual(arranged);
});

test("System pages reorder by dragging their handle", async () => {
	mockAgents(["assistant"]);
	await renderApp("/system/usage");
	expect((await systemPages()).slice(0, 3)).toEqual([
		"usage",
		"tools",
		"skills",
	]);

	await openEditor();
	const handle = editRow("systemPages", "skills").getByTestId(
		"nav-edit-handle",
	);
	await pointAt(handle);
	const target = box(editRow("systemPages", "usage"));
	await commands.mouseDown();
	await commands.mouseMove(target.x + 20, target.y + 4, 8);
	await commands.mouseUp();
	await expect
		.poll(() => rowKeys("systemPages").slice(0, 3))
		.toEqual(["skills", "usage", "tools"]);

	// The sidebar lists System pages on System pages.
	await goto("/system/usage");
	await expect
		.poll(async () => (await systemPages()).slice(0, 3))
		.toEqual(["skills", "usage", "tools"]);
	await reload();
	await expect
		.poll(async () => (await systemPages()).slice(0, 3))
		.toEqual(["skills", "usage", "tools"]);
});

test("Reset to default restores the order on the server and locally", async () => {
	mockAgents(["assistant"]);
	const deletes = () =>
		requestLog().filter(
			(r) =>
				r.url.endsWith("/api/preferences/navigation") && r.method === "DELETE",
		);
	await renderApp("/system/overview");
	await openEditor();
	const reset = page.getByTestId("nav-edit-reset");
	await expect.element(reset).toBeDisabled();
	await editRow("agentPages", "settings").getByTestId("nav-edit-up").click();
	await editRow("agentPages", "chat")
		.getByTestId("nav-edit-visibility")
		.click();
	await expect.element(reset).toBeEnabled();

	await reset.click();
	await expect.element(reset).toBeDisabled();
	await expect.poll(sidebarPages).toEqual(DEFAULT_PAGES);
	await expect
		.element(page.getByTestId("sidebar-show-hidden"))
		.not.toBeInTheDocument();
	expect(deletes()).toHaveLength(1);
	expect(localStorage.getItem("aviary.nav.prefs")).toBeNull();

	await reload();
	await expect.poll(sidebarPages).toEqual(DEFAULT_PAGES);
});

describe("the top row", () => {
	beforeEach(async () => {
		await page.viewport(390, 844);
	});

	test("follows the Pages order and visibility, with no order of its own", async () => {
		mockAgents(["assistant"]);
		await renderApp("/agents/assistant/chat");
		await expect.poll(mobileRowKeys).toEqual([...DEFAULT_PAGES, "system"]);

		await page.locator("header").getByTestId("gear-menu-button").click();
		await page.getByTestId("gear-menu-edit-nav").click();
		await expect.element(page.getByTestId("nav-edit-agentPages")).toBeVisible();
		await expect
			.element(page.getByTestId("nav-edit-top"))
			.not.toBeInTheDocument();
		await expect
			.element(page.getByTestId("nav-editor"))
			.not.toMatchTextContent(/top buttons/i);
		await editRow("agentPages", "files").getByTestId("nav-edit-up").click();
		await editRow("agentPages", "channels")
			.getByTestId("nav-edit-visibility")
			.click();

		await expect
			.poll(mobileRowKeys)
			.toEqual(["chat", "projects", "files", "tasks", "settings", "system"]);
		await page.viewport(1280, 900);
		await expect
			.poll(sidebarPages)
			.toEqual(["chat", "projects", "files", "tasks", "settings"]);
	});
});

describe("dragging", () => {
	afterEach(async () => {
		await commands.emulateMedia({ reducedMotion: null });
	});

	test("lifts the row, opens a placeholder and settles it on drop", async () => {
		mockAgents(["assistant"]);
		await renderApp("/system/overview");
		await openEditor();
		const files = editRow("agentPages", "files");
		const projects = editRow("agentPages", "projects");
		await pointAt(files.getByTestId("nav-edit-handle"));
		const before = box(projects);
		const target = box(editRow("agentPages", "chat"));
		await commands.mouseDown();
		await commands.mouseMove(target.x + 20, target.y + 30, 10);

		const list = page.getByTestId("nav-edit-agentPages");
		const placeholder = list.getByTestId("sortable-placeholder");
		await expect.element(placeholder).toBeVisible();
		// Files lifts under the pointer while the rows it passed slide down.
		await expect.element(files).toHaveClass(/shadow-xl/);
		await expect.poll(() => box(projects).y).toBeGreaterThan(before.y + 10);
		// The placeholder opens where Code Loop was.
		await expect
			.poll(() => Math.abs(box(placeholder).y - before.y))
			.toBeLessThan(2);
		// Nothing saves until the drop.
		expect(rowKeys("agentPages")).toEqual(DEFAULT_PAGES);

		await commands.mouseUp();
		await expect.element(placeholder).not.toBeInTheDocument();
		await expect
			.poll(() => rowKeys("agentPages"))
			.toEqual(["chat", "files", "projects", "channels", "tasks", "settings"]);
		await expect.element(files).not.toHaveClass(/shadow-xl/);
	});

	test("Escape cancels a drag", async () => {
		mockAgents(["assistant"]);
		await renderApp("/system/overview");
		await openEditor();
		await pointAt(
			editRow("agentPages", "settings").getByTestId("nav-edit-handle"),
		);
		const target = box(editRow("agentPages", "chat"));
		await commands.mouseDown();
		await commands.mouseMove(target.x + 20, target.y, 8);
		await commands.keyPress("Escape");
		await commands.mouseUp();
		// The editor stays, and the order is unchanged.
		await expect.element(page.getByTestId("nav-editor")).toBeVisible();
		await expect.poll(() => rowKeys("agentPages")).toEqual(DEFAULT_PAGES);
	});

	test("the handle's arrow keys move its row", async () => {
		mockAgents(["assistant"]);
		await renderApp("/system/overview");
		await openEditor();
		const handle = editRow("agentPages", "chat").getByTestId("nav-edit-handle");
		focus(handle);
		await commands.keyPress("ArrowDown");
		await commands.keyPress("ArrowDown");
		await expect.element(handle).toHaveFocus();
		await commands.keyPress("ArrowUp");
		await expect
			.poll(() => rowKeys("agentPages"))
			.toEqual(["projects", "chat", "channels", "tasks", "files", "settings"]);
	});

	test("reorders without motion under prefers-reduced-motion", async () => {
		await commands.emulateMedia({ reducedMotion: "reduce" });
		mockAgents(["assistant"]);
		await renderApp("/system/overview");
		await openEditor();
		const files = editRow("agentPages", "files");
		await pointAt(files.getByTestId("nav-edit-handle"));
		const target = box(editRow("agentPages", "chat"));
		await commands.mouseDown();
		await commands.mouseMove(target.x + 20, target.y + 4, 8);
		const s = getComputedStyle(files.element());
		expect({ scale: s.scale, rotate: s.rotate }).toEqual({
			scale: "none",
			rotate: "none",
		});
		expect(
			getComputedStyle(editRow("agentPages", "chat").element())
				.transitionDuration,
		).toBe("0s");
		await commands.mouseUp();
		// The drop saves at once, with no settle.
		expect(rowKeys("agentPages")).toEqual([
			"files",
			"chat",
			"projects",
			"channels",
			"tasks",
			"settings",
		]);
	});

	test("autoscrolls the panel near its edges", async () => {
		await page.viewport(1280, 520);
		mockAgents(["assistant"]);
		await renderApp("/system/overview");
		await openEditor();
		const scroller = page.getByTestId("settings-scroll");
		const el = scroller.element();
		expect(el.scrollHeight > el.clientHeight).toBe(true);
		await pointAt(editRow("agentPages", "chat").getByTestId("nav-edit-handle"));
		const b = box(scroller);
		await commands.mouseDown();
		await commands.mouseMove(b.x + 40, b.y + b.height - 6, 10);
		await expect.poll(() => el.scrollTop).toBeGreaterThan(20);
		await commands.mouseUp();
		// Chat drops at the end of its own section.
		await expect.poll(() => rowKeys("agentPages").at(-1)).toBe("chat");
	});
});
