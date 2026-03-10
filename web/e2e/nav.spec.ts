import { expect, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

test.beforeEach(async ({ page }) => {
	await setAuthToken(page);
	await mockMCP(page, { agent_list: [], job_list: [], config_validate: [] });
});

test("root redirects to overview", async ({ page }) => {
	await page.goto("/");
	await expect(page).toHaveURL("/overview");
});

test("sidebar contains all nav links", async ({ page }) => {
	await page.goto("/overview");

	const nav = page.locator("nav");
	for (const label of ["Overview", "Chat", "Settings", "System"]) {
		await expect(
			nav.getByRole("link", { name: label, exact: true }),
		).toBeVisible();
	}
});

test("unauthenticated user is redirected to login", async ({ page }) => {
	// Clear token so auth guard fires.
	await page.addInitScript(() => localStorage.removeItem("aviary_token"));
	await page.goto("/overview");
	await expect(page).toHaveURL("/login");
});
