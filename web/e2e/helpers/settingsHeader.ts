import { expect, type Page } from "@playwright/test";

/**
 * Checks the page's header is the one every routed page shares (#1147), the
 * Scheduled Tasks task page's header from before #1067: a bordered
 * breadcrumb bar at the top of the page, with an icon-only arrow back to the
 * parent, then a bordered title row with the section's icon tile and the
 * page's bold title.
 */
export async function expectSettingsHeader(page: Page, back = true) {
	const header = page.getByTestId("settings-header");
	await expect(header).toHaveCount(1);
	await expect(header.getByTestId("settings-breadcrumb")).toBeVisible();
	await expect(header.getByTestId("settings-header-icon")).toBeVisible();
	const look = await header.evaluate((el) => {
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
		return {
			top: main && bar ? bar.top - main.top : -1,
			barBorder: border("settings-crumb-bar"),
			rowBorder: border("settings-title-row"),
			rowBelowBar: !!bar && !!row && row.top >= bar.bottom - 1,
			icon: icon ? [icon.width, icon.height] : [],
			titleSize: t?.fontSize,
			titleWeight: t?.fontWeight,
		};
	});
	expect(look).toEqual({
		top: 0,
		barBorder: "1px",
		rowBorder: "1px",
		rowBelowBar: true,
		icon: [28, 28],
		titleSize: "18px",
		titleWeight: "700",
	});
	const link = header.getByTestId("settings-back");
	if (!back) {
		await expect(link).toHaveCount(0);
		await expect(header.getByTestId("settings-back-none")).toHaveCount(1);
		return;
	}
	await expect(link).toHaveAccessibleName(/^Back to /);
	await expect(link).toHaveText("");
	await expect(link.locator("svg")).toHaveCount(1);
}
