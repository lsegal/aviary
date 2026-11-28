// Mirrors web/e2e/helpers/settingsHeader.ts for component tests (#1636).
import { expect } from "vitest";
import { page } from "vitest/browser";

/**
 * Checks the page's header is the one every routed page shares (#1147), the
 * Scheduled Tasks task page's header from before #1067: a bordered
 * breadcrumb bar at the top of the page, with an icon-only arrow back to the
 * parent, then a bordered title row with the section's icon tile and the
 * page's bold title. A Code Loop board's compact header (#1570) has no icon
 * tile.
 */
export async function expectSettingsHeader(back = true, compact = false) {
	const header = page.getByTestId("settings-header");
	await expect.poll(() => header.elements().length).toBe(1);
	await expect.element(header.getByTestId("settings-breadcrumb")).toBeVisible();
	await expect
		.poll(() => header.getByTestId("settings-header-icon").elements().length)
		.toBe(compact ? 0 : 1);
	const el = header.element();
	const q = (id: string) => el.querySelector(`[data-testid="${id}"]`);
	const box = (id: string) => q(id)?.getBoundingClientRect();
	const main = el.closest("main")?.getBoundingClientRect();
	const bar = box("settings-crumb-bar");
	const row = box("settings-title-row");
	const icon = box("settings-header-icon");
	const title = q("settings-title");
	const t = title ? getComputedStyle(title) : null;
	const border = (id: string) => {
		const e = q(id);
		return e ? getComputedStyle(e).borderBottomWidth : "";
	};
	const look = {
		top: main && bar ? bar.top - main.top : -1,
		barBorder: border("settings-crumb-bar"),
		rowBorder: border("settings-title-row"),
		rowBelowBar: !!bar && !!row && row.top >= bar.bottom - 1,
		icon: icon ? [icon.width, icon.height] : [],
		titleSize: t?.fontSize,
		titleWeight: t?.fontWeight,
	};
	expect(look).toEqual({
		top: 0,
		barBorder: "1px",
		rowBorder: "1px",
		rowBelowBar: true,
		icon: compact ? [] : [28, 28],
		titleSize: "18px",
		titleWeight: "700",
	});
	const link = header.getByTestId("settings-back");
	if (!back) {
		await expect.poll(() => link.elements().length).toBe(0);
		await expect
			.poll(() => header.getByTestId("settings-back-none").elements().length)
			.toBe(1);
		return;
	}
	await expect.element(link).toHaveAccessibleName(/^Back to /);
	await expect.element(link).toHaveText("");
	await expect.poll(() => link.locator("svg").elements().length).toBe(1);
}
