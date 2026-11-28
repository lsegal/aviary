import { beforeEach, expect, test } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import {
	currentURL,
	renderApp,
	setAuthToken,
	unmountApp,
} from "../component-test/app";
import { mockMCP, routeWebSocket } from "../component-test/server";
import { boundingBoxOf } from "./helpers/box";
import { openGearMenuItem } from "./helpers/sidebar";
// The pages this file renders (see component-test/routes.ts).
import "../views/OverviewView.vue";
import "../views/SettingsView.vue";

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

function mockServer(upgradeAvailable = false) {
	mockMCP({
		agent_list: [],
		job_list: [],
		config_validate: [],
		server_version_check: versionCheck(upgradeAvailable),
	});
}

function connectHealthy() {
	routeWebSocket(/\/api\/ws/, (ws) => {
		ws.send(health);
	});
}

const count = (loc: Locator) => () => loc.elements().length;

beforeEach(() => {
	setAuthToken();
});

test("footer is one row: version on the left, bell, connection then gear right-aligned (#838, #1215)", async () => {
	connectHealthy();
	mockServer();
	await renderApp("/system/overview");

	const footer = page.getByTestId("sidebar-footer");
	const version = footer.getByTestId("server-version");
	await expect.element(version).toHaveText("31ee10e");
	await expect.poll(count(footer.getByTestId("update-icon"))).toBe(0);

	const actions = footer.getByTestId("sidebar-footer-actions");
	const buttons = actions.getByRole("button");
	await expect.poll(count(buttons)).toBe(3);
	await expect.element(buttons.nth(0)).toHaveAccessibleName("Notifications");
	await expect.element(buttons.nth(1)).toHaveAccessibleName("Connected");
	await expect.element(buttons.nth(2)).toHaveAccessibleName("Settings menu");
	// Restart and Log out moved into the gear menu (#822).
	await expect
		.poll(count(footer.getByRole("button", { name: "Restart" })))
		.toBe(0);
	await expect
		.poll(count(footer.getByRole("button", { name: "Log out" })))
		.toBe(0);

	const gear = actions.getByTestId("gear-menu-button");
	const connection = actions.getByTestId("connection-status");
	const row = await boundingBoxOf(footer);
	const gearBox = await boundingBoxOf(gear);
	const connectionBox = await boundingBoxOf(connection);
	const versionBox = await boundingBoxOf(version);
	const actionsBox = await boundingBoxOf(actions);
	// One row: everything shares the row's vertical band.
	expect(row.height).toBeLessThan(40);
	expect(versionBox.x - row.x).toBeLessThan(12);
	expect(versionBox.x).toBeLessThan(actionsBox.x);
	// The gear sits immediately right of the connection state (#838).
	expect(connectionBox.x + connectionBox.width).toBeLessThanOrEqual(gearBox.x);
	expect(gearBox.x - (connectionBox.x + connectionBox.width)).toBeLessThan(12);
	expect(row.x + row.width - (gearBox.x + gearBox.width)).toBeLessThan(12);

	// The old pills are gone.
	await expect.poll(count(page.getByText(/^Version /))).toBe(0);
	await expect
		.poll(count(page.getByText("Connected", { exact: true })))
		.toBe(0);
});

test("the gear opens a Settings, Restart and Logout menu rather than navigating", async () => {
	connectHealthy();
	mockServer();
	await renderApp("/system/overview");

	await page
		.getByTestId("sidebar-footer")
		.getByTestId("gear-menu-button")
		.click();
	await expect.poll(currentURL).toBe("/system/overview");
	const items = page.getByTestId("gear-menu").getByRole("menuitem");
	await expect
		.poll(() =>
			items.elements().map((el) => el.textContent?.replace(/\s+/g, " ").trim()),
		)
		.toEqual(["Settings", "Edit sidebar", "Restart", "Logout"]);
	await commands.keyPress("Escape");

	await openGearMenuItem("Settings");
	await expect.poll(currentURL).toBe("/system/settings");
	await expect
		.element(page.getByTestId("settings-title"))
		.toHaveText("Settings");
});

test("connection icon follows connecting, connected and disconnected", async () => {
	// Real timers: the back-off resets on each open, so the four failures
	// that read as disconnected take about three seconds.
	let connections = 0;
	let healthy = false;
	routeWebSocket(/\/api\/ws/, (ws) => {
		connections += 1;
		if (healthy) ws.send(health);
		else ws.close();
	});
	mockServer();
	await renderApp("/system/overview");

	const icon = page
		.getByTestId("sidebar-footer")
		.getByTestId("connection-status");
	await expect.element(icon).toHaveAttribute("data-state", "connecting");
	await expect.element(icon).toHaveAccessibleName("Connecting…");
	await expect.element(icon).toHaveAttribute("title", "Connecting…");
	await expect.element(icon).toHaveClass(/text-orange-600/);

	// The fourth failure reads as disconnected.
	await expect
		.poll(() => icon.element().getAttribute("data-state"), { timeout: 20_000 })
		.toBe("disconnected");
	await expect.element(icon).toHaveAccessibleName("Disconnected");
	await expect
		.element(icon)
		.toHaveAttribute("title", "Disconnected — click to retry");
	await expect.element(icon).toHaveClass(/text-red-600/);

	// Clicking while disconnected retries right away.
	healthy = true;
	const before = connections;
	await icon.click();
	await expect.poll(() => connections).toBe(before + 1);
	await expect.element(icon).toHaveAttribute("data-state", "connected");
	await expect.element(icon).toHaveAccessibleName("Connected");
	await expect.element(icon).toHaveAttribute("title", "Connected");
	await expect.element(icon).toHaveClass(/text-green-700/);
});

test("update icon shows only with an update and opens the upgrade dialog", async () => {
	connectHealthy();
	mockServer(true);
	await renderApp("/system/overview");

	const footer = page.getByTestId("sidebar-footer");
	await expect
		.element(footer.getByTestId("server-version"))
		.toHaveText("31ee10e");
	const update = footer.getByRole("button", {
		name: "Update available: v0.7.0",
	});
	await expect
		.element(update)
		.toHaveAttribute("title", "Update available: v0.7.0");
	// The notice text stays in its box above the row.
	await expect
		.element(page.getByText("New version available").last())
		.toBeVisible();
	await expect.element(footer).not.toMatchTextContent("New version available");

	await update.click();
	await expect.element(page.getByRole("dialog")).toMatchTextContent("v0.7.0");
});

test("mobile header shows the compact version and connection icon on one row", async () => {
	await page.viewport(390, 844);
	connectHealthy();
	mockServer(true);
	await renderApp("/system/overview");

	const status = page.getByTestId("mobile-header-status");
	await expect
		.element(status.getByTestId("server-version"))
		.toHaveText("31ee10e");
	await expect
		.element(status.getByTestId("connection-status"))
		.toHaveAccessibleName("Connected");
	const update = status.getByRole("button", {
		name: "Update available: v0.7.0",
	});
	await expect.element(update).toBeVisible();

	const logo = await boundingBoxOf(page.getByAltText("Aviary logo").first());
	const box = await boundingBoxOf(status);
	// Same row as the logo, and not wrapped.
	expect(
		Math.abs(box.y + box.height / 2 - (logo.y + logo.height / 2)),
	).toBeLessThan(4);
	// One row of 40px controls (#1317); a second row would double it.
	expect(box.height).toBeLessThan(60);
	await expect.poll(count(page.getByText(/^Version /))).toBe(0);

	await update.click();
	await expect.element(page.getByRole("dialog")).toMatchTextContent("v0.7.0");
});

test("mobile header reads version, bell, connection then gear, with the gear right-most (#1315)", async () => {
	await page.viewport(390, 844);
	connectHealthy();
	mockServer();
	await renderApp("/system/overview");

	const status = page.getByTestId("mobile-header-status");
	const version = status.getByTestId("server-version");
	await expect.element(version).toHaveText("31ee10e");
	const buttons = status.getByRole("button");
	await expect.poll(count(buttons)).toBe(3);
	await expect.element(buttons.nth(0)).toHaveAccessibleName("Notifications");
	await expect.element(buttons.nth(1)).toHaveAccessibleName("Connected");
	await expect.element(buttons.nth(2)).toHaveAccessibleName("Settings menu");

	const row = await boundingBoxOf(status);
	const versionBox = await boundingBoxOf(version);
	const bellBox = await boundingBoxOf(buttons.nth(0));
	const gearBox = await boundingBoxOf(status.getByTestId("gear-menu-button"));
	expect(versionBox.x + versionBox.width).toBeLessThanOrEqual(bellBox.x);
	// The gear ends the row, flush with the header's right edge.
	expect(row.x + row.width - (gearBox.x + gearBox.width)).toBeLessThan(2);
	expect(gearBox.x + gearBox.width).toBeLessThanOrEqual(390);
});

test("mobile header controls have 40px tap targets (#1317)", async () => {
	await page.viewport(390, 844);
	connectHealthy();
	mockServer(true);
	await renderApp("/system/overview");

	const status = page.getByTestId("mobile-header-status");
	await expect
		.element(status.getByTestId("server-version"))
		.toHaveText("31ee10e");
	const buttons = status.getByRole("button");
	await expect.poll(count(buttons)).toBe(4);
	await expect
		.element(buttons.nth(0))
		.toHaveAccessibleName("Update available: v0.7.0");
	for (const button of buttons.elements()) {
		const box = button.getBoundingClientRect();
		const name = button.getAttribute("aria-label") ?? "";
		expect(box.width, name).toBeGreaterThanOrEqual(40);
		expect(box.height, name).toBeGreaterThanOrEqual(40);
	}
	// The icons themselves keep their size.
	await expect
		.element(status.locator("svg").first())
		.toHaveCSS("width", "16px");

	// The desktop sidebar footer keeps its 32px controls.
	await page.viewport(1280, 800);
	const bell = page
		.getByTestId("sidebar-footer-actions")
		.getByRole("button", { name: "Notifications" });
	await expect.element(bell).toBeVisible();
	expect((await boundingBoxOf(bell)).height).toBe(32);
});

test("mobile header fits a 320px phone with an update pending (#1317)", async () => {
	await page.viewport(320, 640);
	connectHealthy();
	mockServer(true);
	await renderApp("/system/overview");

	const status = page.getByTestId("mobile-header-status");
	await expect.poll(count(status.getByRole("button"))).toBe(4);
	const title = await boundingBoxOf(
		page.locator("header").getByText("Aviary", { exact: true }),
	);
	const box = await boundingBoxOf(status);
	const gear = await boundingBoxOf(status.getByTestId("gear-menu-button"));
	// The version text truncates instead of running under the title.
	expect(box.x).toBeGreaterThanOrEqual(title.x + title.width);
	expect(gear.x + gear.width).toBeLessThanOrEqual(320);
	for (const button of status.getByRole("button").elements()) {
		expect(button.getBoundingClientRect().width).toBeGreaterThanOrEqual(40);
	}
	expect(document.documentElement.scrollWidth - window.innerWidth).toBe(0);
});

const labelStyle = [
	"color",
	"text-decoration-line",
	"font-family",
	"font-size",
];

async function versionLabelStyle(version: string) {
	routeWebSocket(/\/api\/ws/, (ws) => {
		ws.send(
			JSON.stringify({ type: "health", ok: true, version, goos: "darwin" }),
		);
	});
	mockServer();
	await renderApp("/system/overview");
	const label = page
		.getByTestId("sidebar-footer")
		.getByTestId("server-version");
	await expect.element(label).toHaveText(version);
	const style = () => {
		const s = getComputedStyle(label.element());
		return labelStyle.map((p) => s.getPropertyValue(p));
	};
	const plain = style();
	await label.hover();
	return { label, plain, hovered: style() };
}

test("a release build's version label links to its release notes with no visual change (#1405)", async () => {
	const { label, plain, hovered } = await versionLabelStyle("v0.7.56");
	expect(label.element().tagName).toBe("A");
	await expect
		.element(label)
		.toHaveAttribute(
			"href",
			"https://github.com/lsegal/aviary/releases/tag/v0.7.56",
		);
	await expect.element(label).toHaveAttribute("target", "_blank");
	await expect.element(label).toHaveAttribute("rel", "noopener noreferrer");
	await expect
		.element(label)
		.toHaveAttribute("title", "Release notes for v0.7.56");
	await expect.element(label).toHaveAccessibleName("Release notes for v0.7.56");
	expect(hovered).toEqual(plain);

	// Keyboard focus shows a ring: tab back onto it from the bell. (Checked
	// before the dev build below, which remounts the app in place of the
	// spec's second browser context.)
	await expect.element(label).toHaveCSS("outline-style", "none");
	(
		page
			.getByTestId("sidebar-footer")
			.getByRole("button", { name: "Notifications" })
			.element() as HTMLElement
	).focus();
	await commands.keyPress("Shift+Tab");
	await expect.element(label).toHaveFocus();
	await expect.element(label).toHaveCSS("outline-style", "solid");
	await expect.element(label).not.toHaveCSS("outline-width", "0px");

	// The same label in a dev build: plain text, not a link, styled the same.
	unmountApp();
	const dev = await versionLabelStyle("dev");
	expect(dev.label.element().tagName).toBe("SPAN");
	expect(dev.label.element().hasAttribute("href")).toBe(false);
	expect(plain).toEqual(dev.plain);
});

test("dev and SHA builds keep the version label as plain text (#1405)", async () => {
	connectHealthy();
	mockServer();
	await renderApp("/system/overview");
	const label = page
		.getByTestId("sidebar-footer")
		.getByTestId("server-version");
	await expect.element(label).toHaveText("31ee10e");
	expect(label.element().tagName).toBe("SPAN");
	await expect.poll(count(page.locator('a[href*="/releases/tag/"]'))).toBe(0);
});
