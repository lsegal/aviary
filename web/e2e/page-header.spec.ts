import { expect, test } from "@playwright/test";
import { mockAviary, PAGES } from "./helpers/pageHeaderPages";
import { expectSettingsHeader } from "./helpers/settingsHeader";

// Every routed page shows the one shared page header (#1147): the Scheduled
// Tasks task page's header from before #1067, a breadcrumb bar with an arrow
// back to the parent, then a title row with the section's icon tile, the
// title and the page's actions. No page keeps a header of its own.

// Test IDs of the headers pages drew before #1147.
const OLD_HEADERS = [
	"page-header",
	"page-breadcrumb",
	"task-header",
	"job-detail-header",
	"channels-overview-header",
];

test.beforeEach(async ({ page }) => {
	await mockAviary(page);
});

for (const p of PAGES) {
	test(`${p.url} renders the shared page header and no other`, async ({
		page,
	}) => {
		{
			await page.goto(p.url);
			const header = page.getByTestId("settings-header");
			await expect(page.getByTestId("settings-title")).toHaveText(p.title);
			await expectSettingsHeader(page, p.back !== false, p.compact);
			// The header opens the page, and it is the page's only header.
			expect(
				await header.evaluate(
					(el) =>
						el.parentElement?.tagName === "MAIN" && !el.previousElementSibling,
				),
			).toBe(true);
			await expect(page.locator("main header:visible")).toHaveCount(0);
			await expect(page.locator("main h1:visible")).toHaveCount(0);
			await expect(
				page.getByRole("navigation", { name: "Breadcrumb" }),
			).toHaveCount(1);
			await expect(page.locator("main h2.text-xl:visible")).toHaveCount(0);
			for (const id of OLD_HEADERS)
				await expect(page.getByTestId(id)).toHaveCount(0);
			// The current crumb is not a link; every parent crumb is.
			const current = header.locator('[aria-current="page"]');
			await expect(current).toHaveCount(1);
			await expect(current.locator("a")).toHaveCount(0);
		}
	});

	test(`on a phone ${p.url}'s header collapses to its parent and page without scrolling sideways`, async ({
		page,
	}) => {
		await page.setViewportSize({ width: 375, height: 740 });
		{
			await page.goto(p.url);
			await expect(page.getByTestId("settings-title")).toHaveText(p.title);
			const crumbs = page
				.getByTestId("settings-breadcrumb")
				.getByRole("listitem");
			expect(await crumbs.count()).toBeLessThanOrEqual(2);
			await expect(crumbs.last().locator('[aria-current="page"]')).toHaveCount(
				1,
			);
			const overflow = await page
				.getByTestId("settings-header")
				.evaluate((el) =>
					[el, ...el.querySelectorAll("*")].some(
						(e) =>
							e.getBoundingClientRect().right > window.innerWidth + 1 &&
							getComputedStyle(e).display !== "none",
					),
				);
			expect(overflow).toBe(false);
		}
	});
}
