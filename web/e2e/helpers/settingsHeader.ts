import { expect, type Page } from "@playwright/test";

/**
 * Checks the page's header is the Settings page header every page shares
 * (#986, #1125): a sticky, bordered, blurred bar below the page's padding,
 * with the "Back to …" link above a breadcrumb set as the page's title.
 */
export async function expectSettingsHeader(page: Page, back = true) {
	const header = page.getByTestId("settings-header");
	await expect(header).toHaveCount(1);
	const look = await header.evaluate((el) => {
		const s = getComputedStyle(el);
		const main = el.closest("main")?.getBoundingClientRect();
		const title = el.querySelector('[data-testid="settings-title"]');
		const t = title ? getComputedStyle(title) : null;
		return {
			position: s.position,
			blur: s.backdropFilter,
			border: s.borderBottomWidth,
			padding: s.paddingTop,
			gap: main ? el.getBoundingClientRect().top - main.top : -1,
			titleSize: t?.fontSize,
			titleWeight: t?.fontWeight,
		};
	});
	expect(look).toEqual({
		position: "sticky",
		blur: expect.stringContaining("blur"),
		border: "1px",
		padding: "16px",
		gap: 24,
		titleSize: "20px",
		titleWeight: "700",
	});
	const link = header.getByTestId("settings-back");
	if (!back) {
		await expect(link).toHaveCount(0);
		return;
	}
	await expect(link).toHaveText(/^Back to /);
	const title = header.getByTestId("settings-title");
	const [linkBox, titleBox] = [
		await link.boundingBox(),
		await title.boundingBox(),
	];
	expect(linkBox && titleBox && linkBox.y + linkBox.height <= titleBox.y).toBe(
		true,
	);
}
