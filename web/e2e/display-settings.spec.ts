import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

// Settings → Display (#1093): text size and theme apply at once, persist on
// the server with a localStorage cache applied before the first paint, and
// share the tab with the sidebar editor and one Reset to defaults.

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

/** The page's background, as the body paints it. */
function bodyBackground(page: Page): Promise<string> {
	return page.evaluate(() => getComputedStyle(document.body).backgroundColor);
}

/** The root font size in pixels. */
function rootFontSize(page: Page): Promise<number> {
	return page.evaluate(() =>
		Number.parseFloat(getComputedStyle(document.documentElement).fontSize),
	);
}

function option(page: Page, group: string, value: string) {
	return page.getByTestId(group).locator(`[data-value="${value}"]`);
}

test.use({ viewport: { width: 1280, height: 900 } });

test("Display is a Settings tab, under the shared header", async ({ page }) => {
	await mockDisplay(page);
	await page.goto("/system/settings");
	const tabs = page.getByTestId("settings-tabs").getByRole("tab");
	await expect(tabs).toHaveText([
		"General",
		"Providers & Authentication",
		"Display",
		"Notifications",
	]);
	await tabs.nth(2).click();
	await expect(page).toHaveURL("/system/display");
	await expect(page.getByTestId("settings-display")).toBeVisible();
	await expect(page.getByTestId("settings-title")).toHaveText("Settings");
	await expect(page.getByTestId("settings-breadcrumb")).toContainText("System");
	// Changes apply at once, so there's no Save Changes.
	await expect(page.getByRole("button", { name: "Save Changes" })).toHaveCount(
		0,
	);
});

test("Light and Dark switch the theme; System follows the OS live", async ({
	page,
}) => {
	await page.emulateMedia({ colorScheme: "light" });
	await mockDisplay(page);
	await page.goto("/system/display");
	await expect(html(page)).toHaveAttribute("data-theme", "light");
	const light = await bodyBackground(page);

	await option(page, "display-theme", "dark").click();
	await expect(option(page, "display-theme", "dark")).toHaveAttribute(
		"aria-checked",
		"true",
	);
	await expect(html(page)).toHaveAttribute("data-theme", "dark");
	await expect.poll(() => bodyBackground(page)).not.toBe(light);

	// Dark stays dark whatever the OS says.
	await page.emulateMedia({ colorScheme: "light" });
	await expect(html(page)).toHaveAttribute("data-theme", "dark");

	await option(page, "display-theme", "light").click();
	await expect(html(page)).toHaveAttribute("data-theme", "light");
	await page.emulateMedia({ colorScheme: "dark" });
	await expect(html(page)).toHaveAttribute("data-theme", "light");

	// System tracks the OS as it changes, without a reload.
	await option(page, "display-theme", "system").click();
	await expect(html(page)).toHaveAttribute("data-theme", "dark");
	await page.emulateMedia({ colorScheme: "light" });
	await expect(html(page)).toHaveAttribute("data-theme", "light");
	await page.emulateMedia({ colorScheme: "dark" });
	await expect(html(page)).toHaveAttribute("data-theme", "dark");
});

test("text size rescales the UI and persists across reloads and devices", async ({
	page,
}) => {
	await mockDisplay(page);
	await page.goto("/system/display");
	expect(await rootFontSize(page)).toBe(16);

	await option(page, "display-text-size", "xlarge").click();
	await expect(html(page)).toHaveAttribute("data-text-size", "xlarge");
	await expect.poll(() => rootFontSize(page)).toBe(20);
	await option(page, "display-text-size", "small").click();
	await expect.poll(() => rootFontSize(page)).toBe(14);
	await option(page, "display-text-size", "large").click();
	await expect.poll(() => rootFontSize(page)).toBe(18);

	await page.reload();
	await expect(html(page)).toHaveAttribute("data-text-size", "large");
	await expect(option(page, "display-text-size", "large")).toHaveAttribute(
		"aria-checked",
		"true",
	);

	// Another device has no local copy and reads the server's.
	await page.evaluate(() => localStorage.removeItem("aviary.display.prefs"));
	await page.reload();
	await expect(html(page)).toHaveAttribute("data-text-size", "large");
	await expect.poll(() => rootFontSize(page)).toBe(18);
});

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

test("the sidebar editor works from the Display tab and the gear menu opens it", async ({
	page,
}) => {
	await mockDisplay(page);
	await page.goto("/agents/assistant/chat");
	await page.getByTestId("sidebar").getByTestId("gear-menu-button").click();
	await page.getByTestId("gear-menu-edit-nav").click();
	await expect(page).toHaveURL("/system/display#sidebar");
	await expect(page.getByTestId("display-sidebar")).toBeInViewport();

	const sidebar = page.getByTestId("sidebar");
	await expect(sidebar.getByTestId("sidebar-row-files")).toBeVisible();
	await page
		.getByTestId("nav-edit-agentPages")
		.locator('[data-key="files"]')
		.getByTestId("nav-edit-visibility")
		.click();
	await expect(sidebar.getByTestId("sidebar-row-files")).toHaveCount(0);
});

test("Reset to defaults restores the text size, theme and sidebar", async ({
	page,
}) => {
	await page.emulateMedia({ colorScheme: "light" });
	await mockDisplay(page);
	await page.goto("/system/display");
	const reset = page.getByTestId("display-reset");
	await expect(reset).toBeDisabled();

	await option(page, "display-theme", "dark").click();
	await option(page, "display-text-size", "xlarge").click();
	await page
		.getByTestId("nav-edit-agentPages")
		.locator('[data-key="files"]')
		.getByTestId("nav-edit-visibility")
		.click();
	await expect(
		page.getByTestId("sidebar").getByTestId("sidebar-row-files"),
	).toHaveCount(0);

	await reset.click();
	await expect(html(page)).toHaveAttribute("data-theme", "light");
	await expect(html(page)).toHaveAttribute("data-text-size", "default");
	await expect(option(page, "display-theme", "system")).toHaveAttribute(
		"aria-checked",
		"true",
	);
	await expect(
		page.getByTestId("sidebar").getByTestId("sidebar-row-files"),
	).toBeVisible();
	await expect(reset).toBeDisabled();

	// The server's copy was reset too.
	await page.evaluate(() => localStorage.removeItem("aviary.display.prefs"));
	await page.reload();
	await expect(html(page)).toHaveAttribute("data-text-size", "default");
	await expect(html(page)).toHaveAttribute("data-theme", "light");
});
