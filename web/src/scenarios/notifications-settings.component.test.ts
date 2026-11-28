import { beforeEach, expect, test } from "vitest";
import { page } from "vitest/browser";
import {
	currentURL,
	reload,
	renderApp,
	setAuthToken,
} from "../component-test/app";
import { setup } from "./helpers/notificationServer";
// The pages this file renders (see component-test/routes.ts).
import "../views/SettingsView.vue";

// Its own file (#1636): useNotifications loads the saved settings once per
// page, and this test needs them to start as the defaults.

beforeEach(async () => {
	await page.viewport(1280, 900);
});

test("Notifications is a Settings tab after Display, under the shared header, sound off by default", async () => {
	const { state } = setup();
	await renderApp("/system/settings");
	const tabs = page.getByTestId("settings-tabs").getByRole("tab");
	await expect
		.poll(() => tabs.elements().map((t) => t.textContent?.trim()))
		.toEqual([
			"General",
			"Providers & Authentication",
			"Display",
			"Notifications",
		]);
	await tabs.last().click();
	await expect.poll(currentURL).toBe("/system/notifications");
	await expect
		.element(page.getByTestId("settings-notifications"))
		.toBeVisible();
	await expect
		.element(page.getByTestId("settings-title"))
		.toHaveText("Settings");
	expect(
		page.getByRole("button", { name: "Save Changes" }).elements(),
	).toHaveLength(0);

	await expect
		.element(page.getByTestId("notifications-enabled"))
		.toHaveAttribute("aria-checked", "true");
	await expect
		.element(page.getByTestId("notifications-sound"))
		.toHaveAttribute("aria-checked", "false");

	// Each change saves on the server at once.
	await page.getByTestId("notifications-sound").click();
	await expect
		.poll(() => (state.puts.at(-1)?.sound as { enabled?: boolean })?.enabled)
		.toBe(true);
	await page
		.getByTestId("notification-event-chat.reply")
		.getByTestId("notification-event-delivery")
		.locator('[data-value="big"]')
		.click();
	await expect
		.poll(
			() =>
				(state.puts.at(-1)?.events as Record<string, { delivery: string }>)?.[
					"chat.reply"
				]?.delivery,
		)
		.toBe("big");

	// Saved settings load on another device. (The settings loaded with the
	// page stay in module state across the remount; the master switch test
	// covers a page loading them from the server.)
	localStorage.clear();
	setAuthToken(); // as the spec's init script signed each load in
	await reload();
	await expect
		.element(page.getByTestId("notifications-sound"))
		.toHaveAttribute("aria-checked", "true");

	await page.getByTestId("notifications-test").click();
	await expect.poll(() => state.tests).toBe(1);
});
