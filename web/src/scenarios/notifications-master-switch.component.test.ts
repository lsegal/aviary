import { afterEach, beforeEach, expect, test } from "vitest";
import { page } from "vitest/browser";
import { goto, renderApp } from "../component-test/app";
import {
	note,
	restoreSystemNotifications,
	setup,
	stubSystemNotifications,
} from "./helpers/notificationServer";

// Its own file (#1636): useNotifications loads the saved settings once per
// page, and this test needs the page to load them (the switch off) fresh.

beforeEach(async () => {
	await page.viewport(1280, 900);
});

afterEach(() => {
	restoreSystemNotifications();
});

test("with the master switch off nothing is shown", async () => {
	const server = setup({ prefs: { enabled: false } });
	const shown = stubSystemNotifications("granted");
	await renderApp("/system/notifications");
	await expect
		.element(page.getByTestId("notifications-enabled"))
		.toHaveAttribute("aria-checked", "false");
	await expect.element(page.getByTestId("notifications-test")).toBeDisabled();
	await goto("/system/overview");
	for (const delivery of ["small", "big", "system"] as const)
		await server.push(note({ delivery, link: "/system/usage" }));
	await expect
		.element(page.getByTestId("notification-unread").first())
		.toHaveText("3");
	expect(page.getByTestId("notification-toast").elements()).toHaveLength(0);
	expect(page.getByTestId("notification-banner").elements()).toHaveLength(0);
	expect(shown).toHaveLength(0);
});
