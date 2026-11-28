import { beforeEach, expect, test } from "vitest";
import { type Locator, page } from "vitest/browser";
import { renderApp } from "../component-test/app";
import { mockAviary, PAGES } from "./helpers/pageHeaderPages";
import { expectSettingsHeader } from "./helpers/settingsHeader";
// The pages this file renders (see component-test/routes.ts).
import "../components/projects/ProjectJobDetail.vue";
import "../components/tasks/dashboard/TaskRunPage.vue";
import "../views/ChannelView.vue";
import "../views/DaemonsView.vue";
import "../views/LogsView.vue";
import "../views/OverviewView.vue";
import "../views/ProjectsView.vue";
import "../views/SettingsView.vue";
import "../views/SystemModelsView.vue";
import "../views/SystemToolsView.vue";
import "../views/TaskView.vue";
import "../views/UsageView.vue";

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

/** How many elements match `selector` and are visible, as Playwright's
 * `:visible`. */
function visibleCount(selector: string): number {
	return [...document.querySelectorAll<HTMLElement>(selector)].filter((el) =>
		el.checkVisibility(),
	).length;
}

function count(loc: Locator): number {
	return loc.elements().length;
}

beforeEach(() => {
	mockAviary();
});

for (const p of PAGES) {
	test(`${p.url} renders the shared page header and no other`, async () => {
		await renderApp(p.url);
		const header = page.getByTestId("settings-header");
		await expect
			.element(page.getByTestId("settings-title"))
			.toHaveText(p.title);
		await expectSettingsHeader(p.back !== false, p.compact);
		// The header opens the page, and it is the page's only header.
		const el = header.element();
		expect(
			el.parentElement?.tagName === "MAIN" && !el.previousElementSibling,
		).toBe(true);
		await expect.poll(() => visibleCount("main header")).toBe(0);
		await expect.poll(() => visibleCount("main h1")).toBe(0);
		await expect
			.poll(() => count(page.getByRole("navigation", { name: "Breadcrumb" })))
			.toBe(1);
		await expect.poll(() => visibleCount("main h2.text-xl")).toBe(0);
		for (const id of OLD_HEADERS)
			await expect.poll(() => count(page.getByTestId(id))).toBe(0);
		// The current crumb is not a link; every parent crumb is.
		const current = header.locator('[aria-current="page"]');
		await expect.poll(() => count(current)).toBe(1);
		await expect.poll(() => count(current.locator("a"))).toBe(0);
	});

	test(`on a phone ${p.url}'s header collapses to its parent and page without scrolling sideways`, async () => {
		await page.viewport(375, 740);
		await renderApp(p.url);
		await expect
			.element(page.getByTestId("settings-title"))
			.toHaveText(p.title);
		const crumbs = page
			.getByTestId("settings-breadcrumb")
			.getByRole("listitem");
		expect(count(crumbs)).toBeLessThanOrEqual(2);
		await expect
			.poll(() => count(crumbs.last().locator('[aria-current="page"]')))
			.toBe(1);
		const el = page.getByTestId("settings-header").element();
		const overflow = [el, ...el.querySelectorAll("*")].some(
			(e) =>
				e.getBoundingClientRect().right > window.innerWidth + 1 &&
				getComputedStyle(e).display !== "none",
		);
		expect(overflow).toBe(false);
	});
}
