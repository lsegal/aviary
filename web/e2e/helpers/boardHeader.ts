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
 * Checks that the compact Code Loop header lines its chips up on the right
 * with the toolbar on a phone (#1572): beside it when they share a row, or
 * ending where it ends on the row above it.
 */
export async function expectHeaderRightAligned(page: Page, lastChip: string) {
	const box = async (id: string) => {
		const b = await page.getByTestId(id).boundingBox();
		if (!b) throw new Error(`no ${id}`);
		return { ...b, right: b.x + b.width, mid: b.y + b.height / 2 };
	};
	const row = await box("settings-title-row");
	const actions = await box("settings-actions");
	const chip = await box(lastChip);
	// The toolbar ends at the row's right padding.
	expect(row.right - actions.right).toBeGreaterThanOrEqual(15);
	expect(row.right - actions.right).toBeLessThanOrEqual(17);
	if (Math.abs(chip.mid - actions.mid) < 6) {
		// One row: the chips sit just before the toolbar.
		expect(chip.right).toBeLessThanOrEqual(actions.x);
		expect(actions.x - chip.right).toBeLessThanOrEqual(12);
	} else {
		// Two rows: the chips end where the toolbar ends.
		expect(chip.mid).toBeLessThan(actions.mid);
		expect(Math.abs(actions.right - chip.right)).toBeLessThanOrEqual(2);
	}
}
