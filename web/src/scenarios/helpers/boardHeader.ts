// Component-test helper, ported from the Playwright helper of the same name (#1636).
import { expect } from "vitest";
import { commands, type Locator, page } from "vitest/browser";

/** Playwright's isVisible: attached, with a box, and not hidden. */
function isVisible(locator: Locator): boolean {
	const el = locator.query();
	if (!el) return false;
	return (
		el.getClientRects().length > 0 &&
		getComputedStyle(el).visibility !== "hidden"
	);
}

/**
 * A touch tap, as Playwright's locator.tap with hasTouch: touch pointer
 * events (so hover handlers that only answer the mouse stay out of it),
 * then the click a tap fires.
 */
async function touchTap(locator: Locator) {
	await expect.element(locator).toBeVisible();
	const el = locator.element() as HTMLElement;
	const r = el.getBoundingClientRect();
	const init: PointerEventInit = {
		bubbles: true,
		cancelable: true,
		composed: true,
		pointerType: "touch",
		isPrimary: true,
		pointerId: 1,
		clientX: r.left + r.width / 2,
		clientY: r.top + r.height / 2,
	};
	el.dispatchEvent(new PointerEvent("pointerdown", init));
	el.dispatchEvent(new PointerEvent("pointerup", init));
	el.click();
}

/**
 * Opens the Code Loop board's details popover (#1570), which holds the issue
 * filter and its editor, the mode, delivery, the last check and the poll
 * interval, unless it is already open, and returns it.
 */
export async function openBoardInfo(tap = false) {
	const popover = page.getByTestId("board-info-popover");
	if (!isVisible(popover)) {
		const trigger = page.getByTestId("board-info");
		await (tap ? touchTap(trigger) : trigger.click());
	}
	await expect.element(popover).toBeVisible();
	return popover;
}

/** Opens the board's instance ID popover (#1570) and returns it. */
export async function openInstanceTag() {
	const popover = page.getByTestId("instance-tag-popover");
	if (!isVisible(popover)) await page.getByTestId("instance-tag").click();
	await expect.element(popover).toBeVisible();
	return popover;
}

/**
 * Waits for the Code Loop board's header to load its status and for its
 * "…" overflow (#1576) to settle after the chips render.
 */
export async function headerSettled() {
	await expect.element(page.getByTestId("board-more-menu")).toBeVisible();
	await expect.element(page.getByTestId("delivery-status")).toHaveText("live");
	await new Promise((done) =>
		requestAnimationFrame(() => requestAnimationFrame(done)),
	);
}

/**
 * The Code Loop board header's items that move into its "…" menu on a
 * narrow phone (#1488, #1576), in the order they move, each with the test
 * ID of its button or icon in the header and of its entry in the menu.
 */
export const COLLAPSIBLE_HEADER_ITEMS = [
	{ header: "logs-toggle", menu: "board-logs" },
	{ header: "project-mode-menu", menu: "board-mode-watch" },
	{ header: "prompt-chat-toggle", menu: "board-more-chat" },
	{ header: "schedule-new", menu: "board-more-schedule" },
	{ header: "queue-menu", menu: "board-more-queue" },
	{ header: "board-add-project", menu: "board-more-add-project" },
	{ header: "board-info", menu: "board-more-info" },
	{ header: "instance-tag", menu: "board-more-instance" },
] as const;

/**
 * Opens the board header's "…" menu and returns which collapsible items show
 * in the header and which in the menu, then closes the menu.
 */
export async function headerItemPlaces() {
	const header: string[] = [];
	const menu: string[] = [];
	const row = page.getByTestId("settings-title-row");
	for (const { header: id } of COLLAPSIBLE_HEADER_ITEMS)
		if (isVisible(row.getByTestId(id))) header.push(id);
	await page.getByTestId("board-more-menu").click();
	const items = page.getByTestId("board-more-items");
	await expect.element(items).toBeVisible();
	for (const { header: id, menu: entry } of COLLAPSIBLE_HEADER_ITEMS)
		if (items.getByTestId(entry).elements().length) menu.push(id);
	await commands.keyPress("Escape");
	await expect.poll(() => isVisible(items)).toBe(false);
	return { header, menu };
}

/**
 * Checks that the compact Code Loop header is a single line (#1576, #1584):
 * the title or chips start at the row's left padding, the toolbar ends at
 * its right padding, everything shares one row, and nothing sticks out.
 */
export async function expectHeaderOneLine() {
	const row = page.getByTestId("settings-title-row");
	await expect.element(row).toBeInTheDocument();
	const el = row.element();
	const box = el.getBoundingClientRect();
	const style = getComputedStyle(el);
	const kids = [
		...el.querySelectorAll<HTMLElement>(
			'[data-testid="settings-status"] > *, [data-testid="settings-actions"] > *',
		),
	]
		.map((k) => k.getBoundingClientRect())
		.filter((b) => b.width > 0);
	const status = el
		.querySelector('[data-testid="settings-status"]')
		?.getBoundingClientRect();
	const actions = el
		.querySelector('[data-testid="settings-actions"]')
		?.getBoundingClientRect();
	// A title shown beside the chips starts the line (#1584).
	const title = el
		.querySelector('[data-testid="settings-title"]')
		?.getBoundingClientRect();
	const first = title && title.width > 1 ? title : status;
	const layout = {
		height: box.height,
		paddingY:
			Number.parseFloat(style.paddingTop) +
			Number.parseFloat(style.paddingBottom),
		mids: kids.map((b) => b.top + b.height / 2),
		tallest: Math.max(...kids.map((b) => b.height)),
		left: (first?.left ?? 0) - box.left - Number.parseFloat(style.paddingLeft),
		right:
			box.right - (actions?.right ?? 0) - Number.parseFloat(style.paddingRight),
		gap: (actions?.left ?? 0) - (status?.right ?? 0),
		wide: el.scrollWidth > el.clientWidth + 1,
		page: document.documentElement.scrollWidth <= innerWidth,
	};
	// One row: every chip and button shares a midline, and the row is one
	// control tall plus its padding.
	for (const mid of layout.mids)
		expect(Math.abs(mid - layout.mids[0])).toBeLessThan(4);
	expect(layout.height).toBeLessThanOrEqual(
		layout.tallest + layout.paddingY + 1,
	);
	// Chips on the left, the toolbar on the right, at the row's padding.
	expect(Math.abs(layout.left)).toBeLessThanOrEqual(1);
	expect(Math.abs(layout.right)).toBeLessThanOrEqual(1);
	expect(layout.gap).toBeGreaterThanOrEqual(7);
	// Nothing scrolls sideways (#997, #1209).
	expect(layout.wide).toBe(false);
	expect(layout.page).toBe(true);
}

/**
 * How the compact Code Loop header filled its line (#1584): whether the
 * title, instance badge and details summary show, the room left between
 * the chips and the toolbar, and what the title and badge would need.
 */
export async function headerFit() {
	const row = page.getByTestId("settings-title-row");
	await expect.element(row).toBeInTheDocument();
	const el = row.element();
	const q = (id: string) =>
		el.querySelector<HTMLElement>(`[data-testid="${id}"]`);
	const w = (id: string) => q(id)?.getBoundingClientRect().width ?? 0;
	const title = q("settings-title");
	const status = q("settings-status")?.getBoundingClientRect();
	const actions = q("settings-actions")?.getBoundingClientRect();
	return {
		title: (title?.getBoundingClientRect().width ?? 0) > 1,
		titleWidth: title?.scrollWidth ?? 0,
		badge: !!q("instance-badge"),
		tag: !!q("instance-tag") && !q("instance-badge"),
		summary: !!q("board-meta"),
		badgeSaves: w("instance-badge-measure") - w("header-icon-measure"),
		// The room between the chips and the toolbar, less their 8px gap.
		free: (actions?.left ?? 0) - (status?.right ?? 0) - 8,
	};
}
