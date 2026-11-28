import { afterEach, beforeEach, describe, expect, test } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { currentURL, goto, reload, renderApp } from "../component-test/app";
import { mockMCP, requestLog, route } from "../component-test/server";

// Harness gap (#1636): setup.ts's locators.extend({ locator }) replaces the
// Locator prototype's own `locator`, which chained getBy* calls use, so any
// chained locator recurses forever. Restore Playwright chaining here.
{
	const proto = Object.getPrototypeOf(page.getByTestId("probe")) as {
		locator(selector: string): Locator;
	};
	proto.locator = function (
		this: { selector: string; _container?: Element },
		selector: string,
	) {
		const Ctor = (this as object).constructor as new (
			s: string,
			c?: Element,
		) => Locator;
		return new Ctor(`${this.selector} >> ${selector}`, this._container);
	};
}

// Customizing the navigation (#1003): the sidebar editor, in Settings →
// Display (#1093), reorders and hides pages, the result persists on the
// server with a localStorage fallback, and Reset restores the built-in
// order. Touch dragging stays in web/e2e/nav-customize.spec.ts: it needs
// real touch input.

const DEFAULT_PAGES = [
	"chat",
	"projects",
	"channels",
	"tasks",
	"files",
	"settings",
];

function mockAgents(names: string[]) {
	mockMCP({
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

function testIds(selector: string, prefix: string): string[] {
	return Array.from(document.querySelectorAll(selector)).map(
		(el) => el.getAttribute("data-testid")?.replace(prefix, "") ?? "",
	);
}

/** The sole agent's sidebar page rows, by key, in order. */
async function sidebarPages(): Promise<string[]> {
	const rows = page
		.getByTestId("sidebar-sole-agent")
		.locator('[data-testid^="sidebar-row-"]');
	await expect.element(rows.first()).toBeVisible();
	return rows
		.elements()
		.map(
			(el) => el.getAttribute("data-testid")?.replace("sidebar-row-", "") ?? "",
		);
}

async function systemPages(): Promise<string[]> {
	const links = page
		.getByTestId("sidebar-system-list")
		.locator('[data-testid^="sidebar-system-"]');
	await expect.element(links.first()).toBeVisible();
	return testIds(
		'[data-testid="sidebar-system-list"] [data-testid^="sidebar-system-"]',
		"sidebar-system-",
	);
}

async function openEditor() {
	await page.getByTestId("sidebar").getByTestId("gear-menu-button").click();
	await page.getByTestId("gear-menu-edit-nav").click();
	await expect.poll(currentURL).toBe("/system/display#sidebar");
	await expect.element(page.getByTestId("nav-edit-agentPages")).toBeVisible();
}

function editRow(list: string, key: string) {
	return page.getByTestId(`nav-edit-${list}`).locator(`[data-key="${key}"]`);
}

/** A section's rows in the sidebar editor, by key, in order. */
function rowKeys(list: string): string[] {
	return page
		.getByTestId(`nav-edit-${list}`)
		.getByTestId("nav-edit-row")
		.elements()
		.map((el) => el.getAttribute("data-key") ?? "");
}

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
	await editRow("agentPages", "files").getByTestId("nav-edit-visibility").click();
	await expect
		.element(editRow("agentPages", "files"))
		.toHaveAttribute("data-hidden", "true");

	const arranged = ["chat", "tasks", "projects", "channels", "settings"];
	await expect.poll(sidebarPages).toEqual(arranged);

	// A fresh browser on another device reads the server's copy.
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
	await editRow("agentPages", "chat").getByTestId("nav-edit-visibility").click();
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

test("without the server's preferences, localStorage keeps them", async () => {
	mockAgents(["assistant"]);
	route("/api/preferences/navigation", (r) =>
		r.fulfill({ status: 500, body: "unavailable" }),
	);
	await renderApp("/system/overview");
	await openEditor();
	await editRow("agentPages", "channels")
		.getByTestId("nav-edit-visibility")
		.click();
	await expect
		.element(editRow("agentPages", "channels"))
		.toHaveAttribute("data-hidden", "true");

	await reload();
	await expect
		.poll(sidebarPages)
		.toEqual(DEFAULT_PAGES.filter((k) => k !== "channels"));
});

test("a page missing from a saved order takes its default place", async () => {
	mockAgents(["assistant"]);
	// Saved by an older version: no Files page, plus one since removed.
	const saved = {
		order: {
			agentPages: ["settings", "tasks", "gone", "chat", "projects", "channels"],
		},
		hidden: {},
	};
	route("/api/preferences/navigation", (r) =>
		r.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify(saved),
		}),
	);
	await renderApp("/system/overview");
	await expect
		.poll(sidebarPages)
		.toEqual(["settings", "tasks", "files", "chat", "projects", "channels"]);
});

/** The narrow-screen main row's keys, in order. */
function mobileRowKeys() {
	return page
		.getByTestId("mobile-main-nav")
		.locator("a")
		.elements()
		.map((el) => el.getAttribute("data-key"));
}

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
		await expect
			.element(page.getByTestId("nav-edit-agentPages"))
			.toBeVisible();
		await expect
			.element(page.getByTestId("nav-edit-top"))
			.not.toBeInTheDocument();
		await expect
			.element(page.getByTestId("nav-editor"))
			.not.toHaveTextContent(/top buttons/i);
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

	test("drops a saved top-button order in favor of the Pages order", async () => {
		mockAgents(["assistant", "coder"]);
		// Saved before the top row followed the Pages order (#1015).
		let saved: unknown = {
			order: {
				agentPages: ["tasks", "chat"],
				top: ["system", "agent:coder", "agent:assistant"],
			},
			hidden: { agentPages: ["files"], top: ["agent:coder"] },
		};
		const puts: unknown[] = [];
		route("/api/preferences/navigation", async (r) => {
			if (r.request().method() === "PUT") {
				saved = r.request().postDataJSON();
				puts.push(saved);
			}
			await r.fulfill({
				status: 200,
				contentType: "application/json",
				body: JSON.stringify(saved),
			});
		});
		localStorage.setItem("aviary.nav.prefs", JSON.stringify(saved));
		const errors: string[] = [];
		const onError = (e: ErrorEvent) => errors.push(e.message);
		const onRejection = (e: PromiseRejectionEvent) =>
			errors.push(String(e.reason));
		window.addEventListener("error", onError);
		window.addEventListener("unhandledrejection", onRejection);

		try {
			await renderApp("/agents/assistant/chat");
			// Agents keep their configured order, and System stays last.
			await expect
				.poll(mobileRowKeys)
				.toEqual(["agent:assistant", "agent:coder", "system"]);
			// The agent's own pages still follow its saved Pages order.
			await expect
				.poll(() =>
					page
						.getByTestId("mobile-sub-nav")
						.locator("a")
						.elements()
						.map((el) => el.getAttribute("href")?.split("/").pop()),
				)
				// Settings and the hidden Files, missing from the saved order,
				// follow Scheduled Tasks as they do by default.
				.toEqual(["tasks", "settings", "chat", "projects", "channels"]);
			await expect.poll(() => puts.length).toBe(1);
			expect(puts[0]).toEqual({
				order: { agentPages: ["tasks", "chat"] },
				hidden: { agentPages: ["files"] },
			});
			expect(
				JSON.parse(localStorage.getItem("aviary.nav.prefs") ?? "{}"),
			).toEqual(puts[0]);
			expect(errors).toEqual([]);
		} finally {
			window.removeEventListener("error", onError);
			window.removeEventListener("unhandledrejection", onRejection);
		}
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
		const handle = editRow("agentPages", "chat").getByTestId(
			"nav-edit-handle",
		);
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
