import { expect, type Page } from "@playwright/test";

/**
 * Opens the Code Loop board's details popover (#1570), which holds the issue
 * filter and its editor, the mode, delivery, the last check and the poll
 * interval, unless it is already open, and returns it.
 */
export async function openBoardInfo(page: Page, tap = false) {
	const popover = page.getByTestId("board-info-popover");
	if (!(await popover.isVisible())) {
		const trigger = page.getByTestId("board-info");
		await (tap ? trigger.tap() : trigger.click());
	}
	await expect(popover).toBeVisible();
	return popover;
}

/** Opens the board's instance ID popover (#1570) and returns it. */
export async function openInstanceTag(page: Page) {
	const popover = page.getByTestId("instance-tag-popover");
	if (!(await popover.isVisible()))
		await page.getByTestId("instance-tag").click();
	await expect(popover).toBeVisible();
	return popover;
}

/**
 * Waits for the Code Loop board's header to load its status and for its
 * "…" overflow (#1576) to settle after the chips render.
 */
export async function headerSettled(page: Page) {
	await expect(page.getByTestId("board-more-menu")).toBeVisible();
	await expect(page.getByTestId("delivery-status")).toHaveText("live");
	await page.evaluate(
		() =>
			new Promise((done) =>
				requestAnimationFrame(() => requestAnimationFrame(done)),
			),
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
export async function headerItemPlaces(page: Page) {
	const header: string[] = [];
	const menu: string[] = [];
	const row = page.getByTestId("settings-title-row");
	for (const { header: id } of COLLAPSIBLE_HEADER_ITEMS)
		if (await row.getByTestId(id).isVisible()) header.push(id);
	await page.getByTestId("board-more-menu").click();
	const items = page.getByTestId("board-more-items");
	await expect(items).toBeVisible();
	for (const { header: id, menu: entry } of COLLAPSIBLE_HEADER_ITEMS)
		if (await items.getByTestId(entry).count()) menu.push(id);
	await page.keyboard.press("Escape");
	await expect(items).toBeHidden();
	return { header, menu };
}

/**
 * Checks that the compact Code Loop header is a single line on a phone
 * (#1576): the chips start at the row's left padding, the toolbar ends at
 * its right padding, everything shares one row, and nothing sticks out.
 */
export async function expectHeaderOneLine(page: Page) {
	const row = page.getByTestId("settings-title-row");
	const layout = await row.evaluate((el) => {
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
		return {
			height: box.height,
			paddingY:
				Number.parseFloat(style.paddingTop) +
				Number.parseFloat(style.paddingBottom),
			mids: kids.map((b) => b.top + b.height / 2),
			tallest: Math.max(...kids.map((b) => b.height)),
			left:
				(status?.left ?? 0) - box.left - Number.parseFloat(style.paddingLeft),
			right:
				box.right -
				(actions?.right ?? 0) -
				Number.parseFloat(style.paddingRight),
			gap: (actions?.left ?? 0) - (status?.right ?? 0),
			wide: el.scrollWidth > el.clientWidth + 1,
			page: document.documentElement.scrollWidth <= innerWidth,
		};
	});
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
