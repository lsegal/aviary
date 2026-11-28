import { afterEach, beforeEach, expect, test } from "vitest";
import { commands, page } from "vitest/browser";
import { currentURL, reload, renderApp } from "../component-test/app";
import { mockMCP } from "../component-test/server";
import { useDisplayPrefs } from "../composables/useDisplayPrefs";
import { useNavPrefs } from "../composables/useNavPrefs";
import {
	defaultDisplayPrefs,
	normalizeDisplayPrefs,
} from "../lib/displayPrefs";
// The pages this file renders (see component-test/routes.ts).
import "../views/ChatView.vue";
import "../views/SettingsView.vue";

// Settings → Display (#1093): text size and theme apply at once, persist on
// the server with a localStorage cache applied before the first paint, and
// share the tab with the sidebar editor and one Reset to defaults.
//
// The display and navigation preferences live in module state, which
// outlasts a test (and reload()), and load the server's copy only once per
// file; each test starts from the defaults, and a page load reading the
// server's copy is covered in display-settings-server-copy. Loads before the
// app's script runs (index.html's cached theme) stay in
// web/e2e/display-settings.spec.ts.

function mockDisplay() {
	mockMCP({
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

/** The server's copy of the display preferences. */
async function serverDisplay() {
	const res = await fetch("/api/preferences/display");
	return normalizeDisplayPrefs(await res.json());
}

/** An attribute of the page's root element, where the display prefs show. */
const htmlAttr = (name: string) => document.documentElement.getAttribute(name);

/** The page's background, as the body paints it. */
function bodyBackground(): string {
	return getComputedStyle(document.body).backgroundColor;
}

/** The root font size in pixels. */
function rootFontSize(): number {
	return Number.parseFloat(getComputedStyle(document.documentElement).fontSize);
}

function option(group: string, value: string) {
	return page.getByTestId(group).locator(`[data-value="${value}"]`);
}

beforeEach(async () => {
	await page.viewport(1280, 900);
	mockDisplay();
});

afterEach(() => {
	useDisplayPrefs().reset();
	const nav = useNavPrefs();
	nav.reset();
	nav.showHidden.value = false;
});

test("Display is a Settings tab, under the shared header", async () => {
	await renderApp("/system/settings");
	const tabs = page.getByTestId("settings-tabs").getByRole("tab");
	await expect
		.poll(() => tabs.elements().map((el) => el.textContent?.trim()))
		.toEqual([
			"General",
			"Providers & Authentication",
			"Display",
			"Notifications",
		]);
	await tabs.nth(2).click();
	await expect.poll(currentURL).toBe("/system/display");
	await expect.element(page.getByTestId("settings-display")).toBeVisible();
	await expect
		.element(page.getByTestId("settings-title"))
		.toHaveText("Settings");
	await expect
		.element(page.getByTestId("settings-breadcrumb"))
		.toMatchTextContent("System");
	// Changes apply at once, so there's no Save Changes.
	expect(
		page.getByRole("button", { name: "Save Changes" }).elements(),
	).toHaveLength(0);
});

test("Light and Dark switch the theme; System follows the OS live", async () => {
	await commands.emulateMedia({ colorScheme: "light" });
	await renderApp("/system/display");
	await expect.poll(() => htmlAttr("data-theme")).toBe("light");
	const light = bodyBackground();

	await option("display-theme", "dark").click();
	await expect
		.element(option("display-theme", "dark"))
		.toHaveAttribute("aria-checked", "true");
	await expect.poll(() => htmlAttr("data-theme")).toBe("dark");
	await expect.poll(bodyBackground).not.toBe(light);

	// Dark stays dark whatever the OS says.
	await commands.emulateMedia({ colorScheme: "light" });
	await expect.poll(() => htmlAttr("data-theme")).toBe("dark");

	await option("display-theme", "light").click();
	await expect.poll(() => htmlAttr("data-theme")).toBe("light");
	await commands.emulateMedia({ colorScheme: "dark" });
	await expect.poll(() => htmlAttr("data-theme")).toBe("light");

	// System tracks the OS as it changes, without a reload.
	await option("display-theme", "system").click();
	await expect.poll(() => htmlAttr("data-theme")).toBe("dark");
	await commands.emulateMedia({ colorScheme: "light" });
	await expect.poll(() => htmlAttr("data-theme")).toBe("light");
	await commands.emulateMedia({ colorScheme: "dark" });
	await expect.poll(() => htmlAttr("data-theme")).toBe("dark");
});

test("text size rescales the UI and persists across reloads and devices", async () => {
	await renderApp("/system/display");
	await expect.element(page.getByTestId("settings-display")).toBeVisible();
	expect(rootFontSize()).toBe(16);

	await option("display-text-size", "xlarge").click();
	await expect.poll(() => htmlAttr("data-text-size")).toBe("xlarge");
	await expect.poll(rootFontSize).toBe(20);
	await option("display-text-size", "small").click();
	await expect.poll(rootFontSize).toBe(14);
	await option("display-text-size", "large").click();
	await expect.poll(rootFontSize).toBe(18);

	// The browser's copy holds the choice, for index.html to apply.
	expect(
		normalizeDisplayPrefs(
			JSON.parse(localStorage.getItem("aviary.display.prefs") ?? "null"),
		).textSize,
	).toBe("large");
	await reload();
	await expect.poll(() => htmlAttr("data-text-size")).toBe("large");
	await expect
		.element(option("display-text-size", "large"))
		.toHaveAttribute("aria-checked", "true");

	// Another device has no local copy and reads the server's, which holds
	// the choice (display-settings-server-copy shows a load applying it).
	await expect.poll(async () => (await serverDisplay()).textSize).toBe("large");
	localStorage.removeItem("aviary.display.prefs");
	await reload();
	await expect.poll(() => htmlAttr("data-text-size")).toBe("large");
	await expect.poll(rootFontSize).toBe(18);
});

test("the sidebar editor works from the Display tab and the gear menu opens it", async () => {
	await renderApp("/agents/assistant/chat");
	await page.getByTestId("sidebar").getByTestId("gear-menu-button").click();
	await page.getByTestId("gear-menu-edit-nav").click();
	await expect.poll(currentURL).toBe("/system/display#sidebar");
	await expect.element(page.getByTestId("display-sidebar")).toBeInViewport();

	const sidebar = page.getByTestId("sidebar");
	await expect.element(sidebar.getByTestId("sidebar-row-files")).toBeVisible();
	await page
		.getByTestId("nav-edit-agentPages")
		.locator('[data-key="files"]')
		.getByTestId("nav-edit-visibility")
		.click();
	await expect
		.element(sidebar.getByTestId("sidebar-row-files"))
		.not.toBeInTheDocument();
});

test("Reset to defaults restores the text size, theme and sidebar", async () => {
	await commands.emulateMedia({ colorScheme: "light" });
	await renderApp("/system/display");
	const reset = page.getByTestId("display-reset");
	await expect.element(reset).toBeDisabled();

	await option("display-theme", "dark").click();
	await option("display-text-size", "xlarge").click();
	await page
		.getByTestId("nav-edit-agentPages")
		.locator('[data-key="files"]')
		.getByTestId("nav-edit-visibility")
		.click();
	await expect
		.element(page.getByTestId("sidebar").getByTestId("sidebar-row-files"))
		.not.toBeInTheDocument();
	await expect
		.poll(async () => (await serverDisplay()).textSize)
		.toBe("xlarge");

	await reset.click();
	await expect.poll(() => htmlAttr("data-theme")).toBe("light");
	await expect.poll(() => htmlAttr("data-text-size")).toBe("default");
	await expect
		.element(option("display-theme", "system"))
		.toHaveAttribute("aria-checked", "true");
	await expect
		.element(page.getByTestId("sidebar").getByTestId("sidebar-row-files"))
		.toBeVisible();
	await expect.element(reset).toBeDisabled();

	// The server's copy was reset too.
	await expect.poll(serverDisplay).toEqual(defaultDisplayPrefs());
	localStorage.removeItem("aviary.display.prefs");
	await reload();
	await expect.poll(() => htmlAttr("data-text-size")).toBe("default");
	await expect.poll(() => htmlAttr("data-theme")).toBe("light");
});
