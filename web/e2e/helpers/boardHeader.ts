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
