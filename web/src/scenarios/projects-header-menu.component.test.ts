// Ported from web/e2e/projects.spec.ts (#1636): the board header's … menu and phone header.
import { afterEach, beforeEach, expect, test } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { renderApp } from "../component-test/app";
import {
	COLLAPSIBLE_HEADER_ITEMS,
	expectHeaderOneLine,
	headerItemPlaces,
	headerSettled,
	openBoardInfo,
} from "./helpers/boardHeader";
import { boundingBoxOf } from "./helpers/box";
import { setup } from "./projects-fixtures";

/** What the stubbed clipboard was given, as the e2e init script kept. */
let copied: string[] = [];

/** Replaces navigator.clipboard with one that records what it writes. */
function stubClipboard() {
	copied = [];
	Object.defineProperty(navigator, "clipboard", {
		configurable: true,
		value: {
			writeText: async (t: string) => {
				copied.push(t);
			},
		},
	});
}

// A fresh page's pointer hovers nothing; the last test's may rest over a
// header icon whose hover opens a popover.
beforeEach(async () => {
	await commands.mouseMove(0, 0);
});

afterEach(() => {
	// Undo the clipboard stub and the text size the tests set.
	Reflect.deleteProperty(navigator, "clipboard");
	document.documentElement.style.fontSize = "";
});

/** Playwright's toHaveText with a list: one entry per element, in order. */
async function expectTexts(locator: Locator, expected: (string | RegExp)[]) {
	const texts = () =>
		locator
			.elements()
			.map((el) => (el.textContent ?? "").replace(/\s+/g, " ").trim());
	await expect
		.poll(texts)
		.toEqual(
			expected.map((e) =>
				typeof e === "string" ? e : expect.stringMatching(e),
			),
		);
}

/**
 * Waits for the header's overflow to stop moving items: under load its
 * resize observer can run a frame or more after headerSettled returns.
 */
async function headerStable() {
	const row = page.getByTestId("settings-title-row");
	const shown = () =>
		COLLAPSIBLE_HEADER_ITEMS.filter(({ header }) =>
			isVisible(row.getByTestId(header)),
		)
			.map(({ header }) => header)
			.join();
	let last = "";
	await expect
		.poll(async () => {
			const before = shown();
			await new Promise((r) => setTimeout(r, 150));
			const now = shown();
			const stable = now === before && now === last;
			last = now;
			return stable;
		})
		.toBe(true);
}

/** Playwright's isVisible: attached, with a box, and not hidden. */
function isVisible(locator: Locator): boolean {
	const el = locator.query();
	if (!el) return false;
	return (
		el.getClientRects().length > 0 &&
		getComputedStyle(el).visibility !== "hidden"
	);
}

/** Playwright's toBeHidden: absent, or present but not visible. */
async function expectHidden(locator: Locator) {
	await expect.poll(() => isVisible(locator)).toBe(false);
}

test("the board header's … menu holds Refresh, Clear and Settings (#1488)", async () => {
	await page.viewport(1440, 900);
	setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	const actions = page.getByTestId("settings-actions");
	// Wide, Logs and Watch stay buttons, and "…" ends the row.
	await expect.element(actions.getByTestId("logs-toggle")).toBeVisible();
	await expect.element(actions.getByTestId("project-mode-menu")).toBeVisible();
	await expect
		.element(actions.locator("a, button").last())
		.toHaveAttribute("data-testid", "board-more-menu");
	for (const name of ["Check now", "Clear finished jobs…", "Settings"])
		await expect
			.poll(() => actions.getByRole("button", { name }).elements().length)
			.toBe(0);
	await expect
		.poll(
			() => actions.getByRole("link", { name: "Settings" }).elements().length,
		)
		.toBe(0);

	// Keyboard: the menu button opens with Enter and its items take focus.
	const more = page.getByTestId("board-more-menu");
	await expect.element(more).toHaveAttribute("aria-haspopup", "menu");
	await expect.element(more).toHaveAttribute("aria-expanded", "false");
	await expect.element(more).toHaveAttribute("title", "More board actions");
	(more.element() as HTMLElement).focus();
	await commands.keyPress("Enter");
	await expect.element(more).toHaveAttribute("aria-expanded", "true");
	const menu = page.getByTestId("board-more-items");
	await expectTexts(menu.getByRole("menuitem"), [
		"Refresh",
		/Clear done/,
		/Clear failed/,
		/Clear all finished/,
		"Settings",
	]);
	// Logs and Watch are buttons, so they are not in the menu too.
	await expect
		.poll(() => menu.getByTestId("board-logs").elements().length)
		.toBe(0);
	await expect
		.poll(() => menu.getByRole("menuitemradio").elements().length)
		.toBe(0);
	await expect
		.element(menu.getByRole("menuitem", { name: "Refresh" }))
		.toHaveFocus();
	await commands.keyPress("ArrowDown");
	await commands.keyPress("ArrowDown");
	await expect
		.element(menu.getByRole("menuitem", { name: /Clear failed/ }))
		.toHaveFocus();
	await commands.keyPress("Enter");
	await expect
		.element(page.getByTestId("clear-jobs-confirm"))
		.toMatchTextContent("failed jobs of lsegal/aviary");
	await page.getByTestId("clear-jobs-confirm-cancel").click();
});

test("the board header moves Logs, then Watch, into … as it narrows (#1488)", async () => {
	setup();
	const actions = page.getByTestId("settings-actions");
	const more = page.getByTestId("board-more-menu");
	let logsGone = false;
	let watchGone = false;
	for (const width of [1440, 1024, 900, 800, 700, 640]) {
		await page.viewport(width, 800);
		await renderApp("/agents/coder/projects/lsegal%2Faviary");
		await expect.element(more).toBeVisible();
		// Wider than a phone, +, Schedule, Chat and the icons stay put.
		for (const id of [
			"queue-menu",
			"schedule-new",
			"prompt-chat-toggle",
			"board-info",
			"instance-tag",
		])
			await expect.element(page.getByTestId(id)).toBeVisible();
		// The row never wraps or scrolls sideways.
		await expect
			.poll(() =>
				((el) => el.scrollWidth - el.clientWidth)(
					actions.element() as HTMLElement,
				),
			)
			.toBeLessThanOrEqual(0);
		const boxes = actions
			.locator(":scope > button, :scope > a")
			.elements()
			.map((e) => e.getBoundingClientRect().top);
		expect(new Set(boxes.map(Math.round)).size).toBe(1);

		// Each of Logs and Watch is a button or a menu item, never both.
		const { header, menu } = await headerItemPlaces();
		for (const id of ["logs-toggle", "project-mode-menu"])
			expect(header.includes(id) !== menu.includes(id), id).toBe(true);
		// Logs goes first, then Watch; neither comes back as it narrows.
		const logsItem = menu.includes("logs-toggle");
		const watchItem = menu.includes("project-mode-menu");
		if (logsGone) expect(logsItem).toBe(true);
		if (watchGone) expect(watchItem).toBe(true);
		if (watchItem) expect(logsItem).toBe(true);
		logsGone ||= logsItem;
		watchGone ||= watchItem;
	}
	expect(logsGone).toBe(true);
});

// On a phone the compact header is a single line (#1576): its items move
// into "…" one at a time as it narrows, in priority order, until only the
// chips the board can't do without and "…" are left.
for (const [board, path, last] of [
	["a project's", "/agents/coder/projects/lsegal%2Faviary", "board-issues"],
	["the root", "/agents/coder/projects", "board-issues"],
] as const) {
	test(`${board} header collapses into … one item at a time on one line (#1576)`, async () => {
		setup();
		const order = COLLAPSIBLE_HEADER_ITEMS.map(({ header }) => header);
		let moved = 0;
		let reachedIcons = false;
		for (const width of [
			639, 560, 500, 470, 430, 400, 390, 375, 360, 340, 320, 300, 280, 260,
		]) {
			await page.viewport(width, 800);
			await renderApp(path);
			await headerSettled();
			await expect.element(page.getByTestId(last)).toBeVisible();
			await expectHeaderOneLine();
			await headerStable();
			const { header, menu } = await headerItemPlaces();
			// Each item shows in the header or in "…", never both or neither.
			for (const id of order) {
				if (header.includes(id) || menu.includes(id))
					expect(header.includes(id) !== menu.includes(id), id).toBe(true);
			}
			// The items in "…" are always the first ones in the order, and
			// more move in as the header narrows; none come back.
			const present = order.filter(
				(id) => header.includes(id) || menu.includes(id),
			);
			expect(menu).toEqual(present.slice(0, menu.length));
			expect(menu.length).toBeGreaterThanOrEqual(moved);
			moved = menu.length;
			// The icons go only once every toolbar action has.
			if (menu.includes("board-info") || menu.includes("instance-tag")) {
				reachedIcons = true;
				for (const id of present.slice(0, present.indexOf("board-info")))
					expect(menu).toContain(id);
			}
		}
		// The narrowest header has every action and the details in "…".
		expect(reachedIcons).toBe(true);
		const row = page.getByTestId("settings-title-row");
		for (const { header } of COLLAPSIBLE_HEADER_ITEMS.slice(0, -1))
			await expect
				.poll(() => row.getByTestId(header).elements().length)
				.toBe(0);
	});
}

test("a larger text size collapses more of the phone header instead of wrapping it (#1576)", async () => {
	await page.viewport(430, 800);
	setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	await headerSettled();
	await expectHeaderOneLine();
	const normal = await headerItemPlaces();
	// Raising the root font size, as a browser's text size setting does,
	// re-measures the line without a reload.
	await (() => {
		document.documentElement.style.fontSize = "20px";
	})();
	await expect
		.poll(async () => (await headerItemPlaces()).menu.length)
		.toBeGreaterThan(normal.menu.length);
	await expectHeaderOneLine();
});

test("the phone header's … menu runs the actions and shows the details it holds (#1576)", async () => {
	stubClipboard();
	await page.viewport(260, 800);
	setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	const more = page.getByTestId("board-more-menu");
	const menu = page.getByTestId("board-more-items");
	// The narrowest header: the chips and "…". A slightly larger text size
	// leaves no room for the instance icon either.
	await headerSettled();
	await expectHeaderOneLine();
	await (() => {
		document.documentElement.style.fontSize = "18px";
	})();
	await expect.element(page.getByTestId("board-issues")).toBeVisible();
	for (const id of ["board-info", "instance-tag", "queue-menu"])
		await expect.poll(() => page.getByTestId(id).elements().length).toBe(0);

	// The details are a section of the menu, with a working filter editor.
	await more.click();
	const details = menu.getByTestId("board-more-info");
	await expect
		.element(details)
		.toMatchTextContent("Issues matching assignee:@me");
	await expect.element(details.getByTestId("board-mode")).toHaveText("Watch");
	await expect
		.element(details.getByTestId("board-interval"))
		.toHaveText("every 30s");
	await details
		.getByRole("button", { name: "Edit issue filter for this project" })
		.click();
	const input = details.getByTestId("issue-filter-input");
	await expect.element(input).toHaveFocus();
	// Typing goes into the editor, not to the menu's typeahead.
	await input.fill("");
	await commands.keyType("label:bug");
	await expect.element(input).toHaveValue("label:bug");
	await expect.element(input).toHaveFocus();
	// Esc cancels the edit first, then closes the menu.
	await commands.keyPress("Escape");
	await expect
		.poll(() => details.getByTestId("issue-filter-input").elements().length)
		.toBe(0);
	await expect.element(menu).toBeVisible();

	// The instance is a section too, and copies its ID.
	const instance = menu.getByTestId("board-more-instance");
	await expect
		.element(instance.getByTestId("board-more-instance-id"))
		.toHaveText("7F3CA91B");
	await instance.getByRole("menuitem", { name: "Copy instance ID" }).click();
	await expect
		.element(instance.getByTestId("board-more-instance-copy-status"))
		.toHaveText("Copied");
	expect(copied).toEqual(["7F3CA91B"]);
	await commands.keyPress("Escape");
	await expectHidden(menu);

	// Queue's choices are a section; a choice opens its picker.
	await more.click();
	await menu
		.getByTestId("board-more-queue")
		.getByRole("menuitem", { name: "Fix issue" })
		.click();
	const dialog = page.getByTestId("queue-dialog");
	await expect.element(dialog).toHaveAttribute("data-skill", "gh-fix");
	await commands.keyPress("Escape");
	await expectHidden(dialog);

	// Prompt chat opens from the menu, as a drawer on a phone.
	await more.click();
	await expect
		.element(menu.getByRole("menuitemcheckbox", { name: "Prompt chat" }))
		.toHaveAttribute("aria-checked", "false");
	await menu.getByRole("menuitemcheckbox", { name: "Prompt chat" }).click();
	await expect.element(page.getByTestId("project-chat")).toBeVisible();
});

test("Logs and Watch work from the … menu on a phone (#1488)", async () => {
	await page.viewport(360, 800);
	const updates: unknown[] = [];
	setup(undefined, {
		project_update: (args?: Record<string, unknown>) => {
			updates.push(args);
			return { ok: true };
		},
	});
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	const more = page.getByTestId("board-more-menu");
	await more.click();
	await page.getByRole("menuitemcheckbox", { name: "Logs" }).click();
	await expect.element(page.getByTestId("project-logs")).toBeVisible();
	await more.click();
	await expect
		.element(page.getByRole("menuitemcheckbox", { name: "Logs" }))
		.toHaveAttribute("aria-checked", "true");
	await page.getByTestId("board-mode-manual").click();
	await expect
		.poll(() => updates[0])
		.toEqual({ target: "lsegal/aviary", mode: "manual" });
});

for (const width of [360, 390, 430]) {
	test(`at ${width}px a project's header is one line with no title, icon or agent chip (#1570, #1576)`, async () => {
		await page.viewport(width, 800);
		setup();
		await renderApp("/agents/coder/projects/lsegal%2Faviary");
		// Measure once the phone's project row has loaded above the header.
		await expect
			.element(page.getByTestId("mobile-sub-nav").getByText("lsegal/zvidlib"))
			.toBeVisible();
		await expect
			.element(page.getByTestId("settings-current"))
			.toHaveText("lsegal/aviary");
		await expect
			.element(page.getByRole("heading", { name: "lsegal/aviary" }))
			.toBeInTheDocument();
		expect(
			page.getByTestId("settings-title").element().getBoundingClientRect()
				.width,
		).toBe(1);
		await expect
			.poll(() => page.getByTestId("settings-header-icon").elements().length)
			.toBe(0);
		await expect
			.poll(() => page.getByTestId("project-owner").elements().length)
			.toBe(0);
		await expect
			.poll(() => page.getByTestId("board-subtitle").elements().length)
			.toBe(0);
		const row: DOMRect[] = [];
		for (const id of [
			"delivery-status",
			"board-issues",
			"instance-tag",
			"board-info",
		]) {
			const box = await boundingBoxOf(page.getByTestId(id));
			expect(box, id).not.toBeNull();
			row.push(box);
		}
		for (const b of row)
			expect(Math.abs((b?.y ?? 0) - (row[0]?.y ?? 0))).toBeLessThan(6);
		// The chips and the toolbar share one line, chips on the left (#1576).
		await headerSettled();
		await expectHeaderOneLine();
		const titleRow = await boundingBoxOf(
			page.getByTestId("settings-title-row"),
		);
		const more = await boundingBoxOf(page.getByTestId("board-more-menu"));
		expect(Math.abs((more?.y ?? 0) - (row[0]?.y ?? 0))).toBeLessThan(12);
		expect(titleRow?.height ?? 99).toBeLessThanOrEqual(54);
		// "…" holds exactly what the line has no room for.
		const { header, menu } = await headerItemPlaces();
		expect([...header, ...menu].sort()).toEqual(
			[
				"logs-toggle",
				"project-mode-menu",
				"prompt-chat-toggle",
				"schedule-new",
				"queue-menu",
				"board-info",
				"instance-tag",
			].sort(),
		);
		const root = page.getByTestId("settings-header").element();
		const wide = [root, ...root.querySelectorAll("*")]
			.filter((el) => el.getBoundingClientRect().right > innerWidth + 1)
			.map((el) => el.outerHTML.slice(0, 80));
		expect(wide).toEqual([]);
		expect(
			await (() => document.documentElement.scrollWidth <= innerWidth)(),
		).toBe(true);
		// The details popover fits the screen too.
		const info = await openBoardInfo(false);
		const box = await boundingBoxOf(info);
		expect(box?.x ?? -1).toBeGreaterThanOrEqual(0);
		expect((box?.x ?? 0) + (box?.width ?? 0)).toBeLessThanOrEqual(width);
	});
}

test("on desktop a project's header keeps its title, with the icons and toolbar on one row (#1570)", async () => {
	await page.viewport(1280, 800);
	setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	const title = page.getByTestId("settings-title");
	await expect.element(title).toBeVisible();
	await expect
		.poll(() => page.getByTestId("settings-header-icon").elements().length)
		.toBe(0);
	const titleBox = await boundingBoxOf(title);
	const more = await boundingBoxOf(page.getByTestId("board-more-menu"));
	const info = await boundingBoxOf(page.getByTestId("board-info"));
	expect(Math.abs((more?.y ?? 0) - (titleBox?.y ?? 0))).toBeLessThan(12);
	expect(Math.abs((info?.y ?? 0) - (titleBox?.y ?? 0))).toBeLessThan(12);
	// The row starts at the page's padding with no icon tile.
	const main = await boundingBoxOf(page.locator("main"));
	expect((titleBox?.x ?? 0) - (main?.x ?? 0)).toBeLessThanOrEqual(25);
});

test("on a phone the models and quota move into the details popover, which warns while closed (#1580)", async () => {
	await page.viewport(390, 844);
	setup();
	for (const path of [
		"/agents/coder/projects",
		"/agents/coder/projects/lsegal%2Faviary",
	]) {
		await renderApp(path);
		await headerSettled();
		// No providers row: the board's toolbar follows the stats directly.
		await expect
			.poll(
				() =>
					page.getByRole("region", { name: "Models and quota" }).elements()
						.length,
			)
			.toBe(0);
		if (path.endsWith("projects"))
			await expect
				.poll(() => {
					const row = page.getByTestId("board-nav-row").element();
					// biome-ignore lint/performance/useDomNodeTextContent: only rendered text counts, as useInnerText did.
					return (row as HTMLElement).innerText;
				})
				.not.toContain("Models & quota");
		else await expectHidden(page.getByTestId("board-nav-row"));

		// gemini is signed out, so the closed icon carries a described dot.
		const info = page.getByRole("button", {
			name: "Board details",
			exact: true,
		});
		await expect.element(page.getByTestId("board-info-alert")).toBeVisible();
		await expect(info).toHaveAccessibleDescription(
			"CLI provider needs attention: gemini signed out",
		);

		// Enter opens the details with each provider's dot, model and quota.
		(info.element() as HTMLElement).focus();
		await commands.keyPress("Enter");
		const details = page.getByRole("dialog", { name: "Board details" });
		const clis = details.getByRole("region", { name: "Models and quota" });
		const rows = clis.getByTestId("cli-provider");
		await expectTexts(rows, [
			/claude\s*opus\s*38% of 5h quota left/,
			/codex\s*gpt-5.6-terra\s*quota ok/,
			/gemini\s*signed out/,
		]);
		await expect
			.element(rows.nth(0).locator("span").first())
			.toHaveClass(/bg-green-500/);
		await expect
			.element(rows.nth(2).locator("span").first())
			.toHaveClass(/bg-gray-400/);
		// The popover stays inside the screen.
		const box = await boundingBoxOf(details);
		expect((box?.x ?? 0) + (box?.width ?? 0)).toBeLessThanOrEqual(390);
		expect(
			await (() => document.documentElement.scrollWidth <= window.innerWidth)(),
		).toBe(true);
		// Esc closes it and returns focus to the icon.
		await commands.keyPress("Escape");
		await expectHidden(details);
		await expect.element(info).toHaveFocus();
		// An outside tap closes it too.
		await info.click();
		await expect.element(details).toBeVisible();
		await page.getByTestId("delivery-status").click();
		await expectHidden(details);
	}
});

test("with the details in … on a narrow phone, … carries the provider warning (#1580)", async () => {
	await page.viewport(260, 800);
	setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	await headerSettled();
	await expect
		.poll(() => page.getByTestId("board-info").elements().length)
		.toBe(0);
	const more = page.getByTestId("board-more-menu");
	await expect.element(page.getByTestId("board-more-menu-alert")).toBeVisible();
	await expect(more).toHaveAccessibleDescription(
		"CLI provider needs attention: gemini signed out",
	);
	await more.click();
	const clis = page
		.getByTestId("board-more-info")
		.getByRole("region", { name: "Models and quota" });
	await expect
		.poll(() => clis.getByTestId("cli-provider").elements().length)
		.toBe(3);
	await expect.element(clis).toMatchTextContent("38% of 5h quota left");
});

test("on desktop the models and quota stay inline with no warning dot (#1580)", async () => {
	setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	await expect
		.element(page.getByRole("region", { name: "Models and quota" }))
		.toMatchTextContent("signed out");
	await expect.element(page.getByTestId("board-info")).toBeVisible();
	await expect
		.poll(() => page.getByTestId("board-info-alert").elements().length)
		.toBe(0);
	await page.getByTestId("board-info").click();
	await expectHidden(page.getByTestId("board-info-clis"));
});

test("the instance tag and details popovers work by keyboard, tap and hover (#1570)", async () => {
	stubClipboard();
	setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary");

	// Enter opens the tag's popover as a named dialog; Esc closes it and
	// returns focus to the tag.
	const tag = page.getByRole("button", { name: "Instance ID", exact: true });
	await expect.element(tag).toHaveAttribute("aria-haspopup", "dialog");
	await expect.element(tag).toHaveAttribute("aria-expanded", "false");
	(tag.element() as HTMLElement).focus();
	await commands.keyPress("Enter");
	const tagPopover = page.getByRole("dialog", { name: "Instance ID" });
	await expect.element(tagPopover).toBeVisible();
	await expect.element(tag).toHaveAttribute("aria-expanded", "true");
	await expect
		.element(tagPopover.getByTestId("instance-id"))
		.toHaveText("7F3CA91B");
	await tagPopover.getByRole("button", { name: /Copy instance ID/ }).click();
	await expect
		.element(tagPopover.getByTestId("instance-id-copy-status"))
		.toHaveText("Copied");
	expect(copied).toEqual(["7F3CA91B"]);
	await commands.keyPress("Escape");
	await expectHidden(tagPopover);
	await expect.element(tag).toHaveFocus();

	// Space opens the details; a click outside closes them.
	const info = page.getByRole("button", { name: "Board details", exact: true });
	(info.element() as HTMLElement).focus();
	await commands.keyPress("Space");
	const details = page.getByRole("dialog", { name: "Board details" });
	await expect.element(details).toBeVisible();
	await expect
		.element(details)
		.toMatchTextContent("Issues matching assignee:@me");
	await expect.element(details.getByTestId("board-mode")).toHaveText("Watch");
	await expect
		.element(details.getByTestId("board-delivery"))
		.toHaveText("poll");
	await expect
		.element(details.getByTestId("board-interval"))
		.toHaveText("every 30s");
	await commands.mouseMove(600, 600);
	await commands.mouseDown();
	await commands.mouseUp();
	await expectHidden(details);

	// Hovering shows the details as a tooltip, gone when the pointer leaves.
	await info.hover();
	await expect.element(details).toBeVisible();
	await expect.element(info).not.toHaveFocus();
	await commands.mouseMove(600, 600);
	await expectHidden(details);

	// The filter editor works from inside the popover; Esc cancels the edit
	// first, then closes the popover.
	await info.click();
	await details
		.getByRole("button", { name: "Edit issue filter for this project" })
		.click();
	const input = details.getByTestId("issue-filter-input");
	await expect.element(input).toHaveFocus();
	(input.element() as HTMLElement).focus();
	await commands.keyPress("Escape");
	await expect
		.poll(() => details.getByTestId("issue-filter-editor").elements().length)
		.toBe(0);
	await expect.element(details).toBeVisible();
	await commands.keyPress("Escape");
	await expectHidden(details);
});
