import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

// Settings → Display (#1093): the localStorage cache is applied by
// index.html before the app's script runs, so a load never flashes the wrong
// theme or size. These stay Playwright specs because they need a real page
// load (index.html's inline script, an init script observing the load); the
// rest of Display is in web/src/scenarios/display-settings*.component.test.ts.

async function mockDisplay(page: Page) {
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: [],
		job_list: [],
		config_validate: [],
		session_list: [],
		auth_list: [],
		config_get: {
			agents: [{ name: "assistant", model: "x/y", fallbacks: [] }],
		},
	});
}

const html = (page: Page) => page.locator("html");

function option(page: Page, group: string, value: string) {
	return page.getByTestId(group).locator(`[data-value="${value}"]`);
}

test.use({ viewport: { width: 1280, height: 900 } });

test("the cached theme and size apply before the app loads", async ({
	page,
}) => {
	await page.emulateMedia({ colorScheme: "light" });
	await page.addInitScript(() =>
		localStorage.setItem(
			"aviary.display.prefs",
			JSON.stringify({ theme: "dark", textSize: "small" }),
		),
	);
	await mockDisplay(page);
	// Replace the app's entry, in dev or in a build, with nothing: only
	// index.html's own script runs.
	await page.route(/\/(src\/main\.ts|assets\/index-[^/]*\.js)(\?|$)/, (route) =>
		route.fulfill({ contentType: "text/javascript", body: "" }),
	);
	await page.goto("/system/display", { waitUntil: "domcontentloaded" });
	await expect(page.getByTestId("settings-display")).toHaveCount(0);
	await expect(html(page)).toHaveAttribute("data-theme", "dark");
	await expect(html(page)).toHaveAttribute("data-text-size", "small");
});

test("a load with a cached theme is never shown in the other theme", async ({
	page,
}) => {
	await page.emulateMedia({ colorScheme: "light" });
	await page.addInitScript(() =>
		localStorage.setItem(
			"aviary.display.prefs",
			JSON.stringify({ theme: "dark" }),
		),
	);
	// Record every theme the page is ever shown in.
	await page.addInitScript(() => {
		const seen: string[] = [];
		(window as unknown as { seenThemes: string[] }).seenThemes = seen;
		new MutationObserver((records) => {
			for (const r of records) {
				const theme = (r.target as HTMLElement).dataset.theme;
				if (theme && seen.at(-1) !== theme) seen.push(theme);
			}
		}).observe(document, {
			subtree: true,
			attributes: true,
			attributeFilter: ["data-theme"],
		});
	});
	await mockDisplay(page);
	// The server holds the same choice, as it does once the user saved it.
	await page.route("/api/preferences/display", (route) =>
		route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify({ theme: "dark" }),
		}),
	);
	await page.goto("/system/display");
	await expect(page.getByTestId("settings-display")).toBeVisible();
	await expect(option(page, "display-theme", "dark")).toHaveAttribute(
		"aria-checked",
		"true",
	);
	expect(
		await page.evaluate(
			() => (window as unknown as { seenThemes: string[] }).seenThemes,
		),
	).toEqual(["dark"]);
});
