import { expect, test } from "vitest";
import { page } from "vitest/browser";
import { goto, renderApp } from "../component-test/app";
import { declarePages } from "../component-test/routes";
import { route } from "../component-test/server";
import {
	expectNoHorizontalOverflow,
	mockSettings,
} from "./settings-mobile-overflow.fixtures";

// The pages this file renders (see component-test/routes.ts).
declarePages(() => import("../views/SettingsView.vue"));

/** An attribute of the page's root element, where the display prefs show. */
const htmlAttr = (name: string) => document.documentElement.getAttribute(name);

// Settings pages fit a phone at the largest text size (#1093). Its own file
// because the display preferences are read once per page (module state):
// this page must load the server's xlarge choice fresh.

test("settings pages fit a phone at the largest text size (#1093)", async () => {
	await page.viewport(375, 800);
	localStorage.setItem(
		"aviary.display.prefs",
		JSON.stringify({ textSize: "xlarge" }),
	);
	mockSettings();
	// The server holds the same choice, as it does once the user saved it.
	route("/api/preferences/display", (r) =>
		r.fulfill({ json: { textSize: "xlarge" } }),
	);
	let first = true;
	for (const url of [
		"/agents/Aviary/channels/signal/settings",
		"/agents/Aviary/tasks?task=release-aviary",
		"/agents/Aviary/settings",
		"/projects/settings",
		"/system/settings",
		"/system/providers",
		"/system/display",
	]) {
		if (first) await renderApp(url);
		else await goto(url);
		first = false;
		await expect.poll(() => htmlAttr("data-text-size")).toBe("xlarge");
		await expectNoHorizontalOverflow(url);
	}
	// The tab bar scrolls sideways, keeping the open tab in view.
	await expect
		.element(page.getByTestId("settings-tab-display"))
		.toBeInViewport({ ratio: 0.95 });
	// The header's links give way rather than run under its status icons.
	const status = page
		.getByTestId("mobile-header-status")
		.element()
		.getBoundingClientRect();
	// The status group's own version link (#1405) is not one of those links.
	const links = page
		.locator('header a[aria-label]:not([data-testid="mobile-header-status"] a)')
		.elements();
	expect(links.length).toBeGreaterThan(0);
	for (const link of links) {
		const box = link.getBoundingClientRect();
		const style = getComputedStyle(link);
		const visible =
			box.width > 0 &&
			box.height > 0 &&
			style.visibility !== "hidden" &&
			style.display !== "none";
		if (!visible) continue;
		const p = (link.parentElement as HTMLElement).getBoundingClientRect();
		const clipped = box.top >= p.bottom;
		if (!clipped) expect(box.x + box.width).toBeLessThanOrEqual(status.x);
	}
});
