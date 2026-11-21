import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";
import { openGearMenuItem } from "./helpers/sidebar";

const health = JSON.stringify({
	type: "health",
	ok: true,
	version: "31ee10eaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	goos: "darwin",
});

function versionCheck(upgradeAvailable: boolean) {
	return {
		currentVersion: "31ee10eaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		latestVersion: upgradeAvailable ? "v0.7.0" : "",
		upgradeAvailable,
		upgradeSupported: true,
		restart: { phase: "idle", jobs: [], held: false },
	};
}

async function mockServer(page: Page, upgradeAvailable = false) {
	await mockMCP(page, {
		agent_list: [],
		job_list: [],
		config_validate: [],
		server_version_check: versionCheck(upgradeAvailable),
	});
}

function connectHealthy(page: Page) {
	return page.routeWebSocket(/\/api\/ws/, (ws) => {
		ws.send(health);
	});
}

test.beforeEach(async ({ page }) => {
	await setAuthToken(page);
});

test("footer is one row: gear and version on the left, connection right-aligned", async ({
	page,
}) => {
	await connectHealthy(page);
	await mockServer(page);
	await page.goto("/system/overview");

	const footer = page.getByTestId("sidebar-footer");
	const version = footer.getByTestId("server-version");
	await expect(version).toHaveText("31ee10e");
	await expect(footer.getByTestId("update-icon")).toHaveCount(0);

	const actions = footer.getByTestId("sidebar-footer-actions");
	const buttons = actions.getByRole("button");
	await expect(buttons).toHaveCount(1);
	await expect(buttons.nth(0)).toHaveAccessibleName("Connected");
	// Restart and Log out moved into the gear menu (#822).
	await expect(footer.getByRole("button", { name: "Restart" })).toHaveCount(0);
	await expect(footer.getByRole("button", { name: "Log out" })).toHaveCount(0);

	const gear = footer.getByTestId("gear-menu-button");
	const row = await footer.boundingBox();
	const gearBox = await gear.boundingBox();
	const versionBox = await version.boundingBox();
	const actionsBox = await actions.boundingBox();
	if (!row || !gearBox || !versionBox || !actionsBox)
		throw new Error("footer not laid out");
	// One row: everything shares the row's vertical band.
	expect(row.height).toBeLessThan(40);
	expect(gearBox.x - row.x).toBeLessThan(12);
	expect(gearBox.x).toBeLessThan(versionBox.x);
	expect(versionBox.x).toBeLessThan(actionsBox.x);
	expect(row.x + row.width - (actionsBox.x + actionsBox.width)).toBeLessThan(
		12,
	);

	// The old pills are gone.
	await expect(page.getByText(/^Version /)).toHaveCount(0);
	await expect(page.getByText("Connected", { exact: true })).toHaveCount(0);
});

test("the gear opens a Settings, Restart and Logout menu rather than navigating", async ({
	page,
}) => {
	await connectHealthy(page);
	await mockServer(page);
	await page.goto("/system/overview");

	await page
		.getByTestId("sidebar-footer")
		.getByTestId("gear-menu-button")
		.click();
	await expect(page).toHaveURL("/system/overview");
	await expect(page.getByTestId("gear-menu").getByRole("menuitem")).toHaveText([
		"Settings",
		"Restart",
		"Logout",
	]);
	await page.keyboard.press("Escape");

	await openGearMenuItem(page, "Settings");
	await expect(page).toHaveURL("/system/settings");
	await expect(page.getByTestId("settings-title")).toHaveText("Settings");
});

test("connection icon follows connecting, connected and disconnected", async ({
	page,
}) => {
	await page.clock.install();
	let connections = 0;
	let healthy = false;
	await page.routeWebSocket(/\/api\/ws/, (ws) => {
		connections += 1;
		if (healthy) ws.send(health);
		else ws.close();
	});
	await mockServer(page);
	await page.goto("/system/overview");

	const icon = page
		.getByTestId("sidebar-footer")
		.getByTestId("connection-status");
	await expect(icon).toHaveAttribute("data-state", "connecting");
	await expect(icon).toHaveAccessibleName("Connecting…");
	await expect(icon).toHaveAttribute("title", "Connecting…");
	await expect(icon).toHaveClass(/text-orange-600/);

	// Back-off is 1 s, 2 s, 4 s; the fourth failure reads as disconnected.
	await expect
		.poll(async () => {
			await page.clock.runFor(1_000);
			return icon.getAttribute("data-state");
		})
		.toBe("disconnected");
	await expect(icon).toHaveAccessibleName("Disconnected");
	await expect(icon).toHaveAttribute("title", "Disconnected — click to retry");
	await expect(icon).toHaveClass(/text-red-600/);

	// Clicking while disconnected retries right away.
	healthy = true;
	const before = connections;
	await icon.click();
	await expect.poll(() => connections).toBe(before + 1);
	await expect(icon).toHaveAttribute("data-state", "connected");
	await expect(icon).toHaveAccessibleName("Connected");
	await expect(icon).toHaveAttribute("title", "Connected");
	await expect(icon).toHaveClass(/text-green-700/);
});

test("update icon shows only with an update and opens the upgrade dialog", async ({
	page,
}) => {
	await connectHealthy(page);
	await mockServer(page, true);
	await page.goto("/system/overview");

	const footer = page.getByTestId("sidebar-footer");
	await expect(footer.getByTestId("server-version")).toHaveText("31ee10e");
	const update = footer.getByRole("button", {
		name: "Update available: v0.7.0",
	});
	await expect(update).toHaveAttribute("title", "Update available: v0.7.0");
	// The notice text stays in its box above the row.
	await expect(page.getByText("New version available").last()).toBeVisible();
	await expect(footer).not.toContainText("New version available");

	await update.click();
	await expect(page.getByRole("dialog")).toContainText("v0.7.0");
});

test("mobile header shows the compact version and connection icon on one row", async ({
	page,
}) => {
	await page.setViewportSize({ width: 390, height: 844 });
	await connectHealthy(page);
	await mockServer(page, true);
	await page.goto("/system/overview");

	const status = page.getByTestId("mobile-header-status");
	await expect(status.getByTestId("server-version")).toHaveText("31ee10e");
	await expect(status.getByTestId("connection-status")).toHaveAccessibleName(
		"Connected",
	);
	const update = status.getByRole("button", {
		name: "Update available: v0.7.0",
	});
	await expect(update).toBeVisible();

	const logo = await page.getByAltText("Aviary logo").first().boundingBox();
	const box = await status.boundingBox();
	if (!logo || !box) throw new Error("header not laid out");
	// Same row as the logo, and not wrapped.
	expect(
		Math.abs(box.y + box.height / 2 - (logo.y + logo.height / 2)),
	).toBeLessThan(4);
	expect(box.height).toBeLessThan(40);
	await expect(page.getByText(/^Version /)).toHaveCount(0);

	await update.click();
	await expect(page.getByRole("dialog")).toContainText("v0.7.0");
});
