import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

// Choosing Settings from the gear menu routes at once and shows the page
// shell without waiting on the server (#897).
const BUDGET_MS = 500;

/**
 * Mocks the server and resolves once the Settings chunk has loaded, so the
 * timings measure routing rather than a first download.
 */
async function mockServer(
	page: Page,
	configGet: () => Promise<unknown>,
	slowFixtures: Record<string, () => Promise<unknown>> = {},
) {
	await mockMCP(page, {
		agent_list: [{ id: "a1", name: "bot", state: "idle" }],
		job_list: [],
		config_validate: [],
		session_list: [],
		config_get: configGet,
		...slowFixtures,
	});
	let settingsChunk = false;
	page.on("requestfinished", (r) => {
		if (/SettingsView/.test(r.url())) settingsChunk = true;
	});
	return () => expect.poll(() => settingsChunk).toBe(true);
}

/** Holds every API request open until the test ends. */
async function stallServer(page: Page) {
	const never = new Promise<void>(() => {});
	await page.route("/mcp", () => never);
	await page.route(/\/api\/(?!ws)/, () => never);
}

/**
 * Clicks Settings in the gear menu and returns the ms, timed in the page,
 * from pressing it until the URL changed and the Settings heading rendered.
 */
async function clickSettings(page: Page) {
	await page
		.getByTestId("sidebar-footer")
		.getByTestId("gear-menu-button")
		.click();
	const item = page
		.getByTestId("gear-menu")
		.getByRole("menuitem", { name: "Settings", exact: true });
	await expect(item).toBeVisible();
	// Installed before the click, so the press is always seen.
	await page.evaluate(() => {
		const w = window as unknown as { settingsNav?: Promise<number> };
		w.settingsNav = new Promise<number>((resolve) => {
			document.addEventListener(
				"pointerdown",
				() => {
					const start = performance.now();
					const shown = () =>
						location.pathname === "/system/settings" &&
						document.querySelector("[data-testid=settings-title]");
					const check = () => {
						if (shown()) resolve(performance.now() - start);
						else requestAnimationFrame(check);
					};
					check();
				},
				{ capture: true, once: true },
			);
		});
	});
	await item.click();
	await expect(page).toHaveURL("/system/settings");
	await expect(page.getByTestId("settings-title")).toBeVisible();
	return page.evaluate(
		() => (window as unknown as { settingsNav: Promise<number> }).settingsNav,
	);
}

test("Settings in the gear menu routes and shows its shell while data loads (#897)", async ({
	page,
}) => {
	await setAuthToken(page);
	let releaseConfig = () => {};
	const configLoaded = new Promise<void>((resolve) => {
		releaseConfig = resolve;
	});
	const never = new Promise<never>(() => {});
	const chunkLoaded = await mockServer(
		page,
		async () => {
			await configLoaded;
			return {
				agents: [{ name: "bot", model: "anthropic/claude-sonnet-4-6" }],
			};
		},
		// Requests the forms do not need never answer, and must not hold
		// them up: installed skills and provider credentials.
		{ skills_list: () => never, auth_list: () => never },
	);
	// Nor does the tool list.
	await page.route("/mcp", (route) =>
		route.request().postDataJSON()?.method === "tools/list"
			? never
			: route.fallback(),
	);
	await page.goto("/system/overview");
	await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible();
	// The chunk is prefetched before the first click.
	await chunkLoaded();

	expect(await clickSettings(page)).toBeLessThan(BUDGET_MS);
	// The config is still pending: the data-dependent sections show a
	// loading state rather than holding up the page.
	await expect(page.getByTestId("settings-loading")).toBeVisible();

	releaseConfig();
	await expect(page.getByTestId("settings-loading")).toHaveCount(0);
	await expect(page.getByRole("button", { name: "Reload" })).toBeEnabled();
});

test("no route guard waits on the server once the app has loaded (#897)", async ({
	page,
}) => {
	// Leave the onboarding gate active so its first check really runs.
	await setAuthToken(page, "test-token", { onboarding: true });
	const chunkLoaded = await mockServer(page, async () => ({ agents: [] }));
	await page.goto("/system/overview");
	await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible();
	await chunkLoaded();

	await stallServer(page);
	expect(await clickSettings(page)).toBeLessThan(BUDGET_MS);
});
