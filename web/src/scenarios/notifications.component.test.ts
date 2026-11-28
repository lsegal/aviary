import { afterEach, beforeEach, expect, test } from "vitest";
import { page } from "vitest/browser";
import { currentURL, goto, renderApp } from "../component-test/app";
import {
	note,
	removeSystemNotifications,
	restoreSystemNotifications,
	setup,
	stubSystemNotifications,
} from "./helpers/notificationServer";

// Notifications (#1215): pushed over /api/ws, shown as a banner, a toast or
// a system notification with a sound, kept in the bell, and controlled from
// Settings → Notifications. useNotifications loads the saved settings once
// per page, so the tests that need them loaded fresh (the Settings tab with
// sound off, the master switch off, sound) have their own files:
// notifications-{settings,master-switch,sound}.component.test.ts.

beforeEach(async () => {
	await page.viewport(1280, 900);
});

afterEach(() => {
	restoreSystemNotifications();
});

test("the Notifications tab fits a phone-width screen", async () => {
	await page.viewport(375, 812);
	setup();
	await renderApp("/system/notifications");
	await expect
		.element(page.getByTestId("settings-notifications"))
		.toBeVisible();
	const el = page.getByTestId("settings-scroll").element();
	expect(el.scrollWidth - el.clientWidth).toBeLessThanOrEqual(0);
	await expect
		.element(page.getByTestId("notification-bell").first())
		.toBeVisible();
});

test("a small notification is a toast and a big one a banner; clicking opens the linked page", async () => {
	const server = setup();
	await renderApp("/system/overview");

	await server.push(
		note({
			title: "bot replied",
			body: "in Main Chat",
			delivery: "small",
			link: "/system/usage",
		}),
	);
	const toast = page.getByTestId("notification-toast");
	await expect.element(toast).toHaveTextContent("bot replied");
	await expect
		.element(page.getByTestId("notification-unread").first())
		.toHaveText("1");
	await toast.getByRole("button", { name: /bot replied/ }).click();
	await expect.poll(currentURL).toBe("/system/usage");
	await expect.poll(() => server.state.reads.length).toBeGreaterThan(0);

	await server.push(
		note({
			title: "Failed: acme/app#7",
			delivery: "big",
			link: "/system/notifications",
		}),
	);
	const banner = page.getByTestId("notification-banner");
	await expect.element(banner).toHaveTextContent("Failed: acme/app#7");
	await banner.getByRole("button", { name: /Failed/ }).click();
	await expect.poll(currentURL).toBe("/system/notifications");
	await expect.poll(() => banner.elements().length).toBe(0);
});

test("no notification is shown for the page being viewed", async () => {
	const server = setup();
	await renderApp("/system/notifications");
	await server.push(
		note({
			title: "Here already",
			link: "/system/notifications",
			delivery: "big",
		}),
	);
	await expect
		.poll(() => server.state.reads.flat())
		.toContain(server.state.history[0].id);
	expect(page.getByTestId("notification-banner").elements()).toHaveLength(0);
	expect(page.getByTestId("notification-toast").elements()).toHaveLength(0);
});

test("system delivery uses the browser when allowed", async () => {
	const server = setup();
	const shown = stubSystemNotifications("granted");
	await renderApp("/system/notifications");
	await expect
		.element(page.getByTestId("notifications-permission"))
		.toHaveAttribute("data-state", "granted");
	await goto("/system/overview");
	await server.push(
		note({ title: "Workflow bot/triage failed", delivery: "system" }),
	);
	await expect
		.poll(() => shown)
		.toEqual([
			{
				title: "Workflow bot/triage failed",
				body: "agent exited with status 1",
			},
		]);
	expect(page.getByTestId("notification-toast").elements()).toHaveLength(0);
});

test("when the browser denies permission the settings say so and in-app delivery still works", async () => {
	const server = setup();
	stubSystemNotifications("denied");
	await renderApp("/system/notifications");
	const permission = page.getByTestId("notifications-permission");
	await expect.element(permission).toHaveAttribute("data-state", "denied");
	await expect
		.element(permission)
		.toHaveTextContent("Blocked in this browser");
	expect(page.getByTestId("notifications-allow").elements()).toHaveLength(0);

	await goto("/system/overview");
	await server.push(
		note({ title: "Falls back to a toast", delivery: "system" }),
	);
	await expect
		.element(page.getByTestId("notification-toast"))
		.toHaveTextContent("Falls back to a toast");
});

test("unsupported system notifications are reported", async () => {
	setup();
	removeSystemNotifications();
	await renderApp("/system/notifications");
	await expect
		.element(page.getByTestId("notifications-permission"))
		.toHaveAttribute("data-state", "unsupported");
});

test("the bell shows the unread count and history, with mark-read and clear", async () => {
	const history = [
		note({ title: "Newest", link: "/system/usage" }),
		note({ title: "Older", read: true, count: 3 }),
		note({ title: "Oldest" }),
	];
	const server = setup({ history });
	await renderApp("/system/overview");
	const bell = page
		.getByTestId("sidebar-footer")
		.getByTestId("notification-bell");
	await expect.element(bell.getByTestId("notification-unread")).toHaveText("2");
	await expect
		.element(bell)
		.toHaveAccessibleName("Notifications, 2 unread");

	await bell.click();
	const center = page.getByTestId("notification-center");
	const items = center.getByTestId("notification-item");
	await expect.poll(() => items.elements().length).toBe(3);
	await expect.element(items.nth(1)).toHaveTextContent("×3");
	await center.getByTestId("notification-mark-all-read").click();
	await expect
		.poll(() => bell.getByTestId("notification-unread").elements().length)
		.toBe(0);
	await expect.poll(() => server.state.reads.at(-1)).toEqual([]);

	await items.first().click();
	await expect.poll(currentURL).toBe("/system/usage");

	await bell.click();
	await center.getByTestId("notification-clear").click();
	await expect
		.element(center.getByTestId("notification-empty"))
		.toBeVisible();
});

test("quiet notifications reach the bell only", async () => {
	const server = setup();
	await renderApp("/system/overview");
	await server.push(note({ title: "At night", quiet: true, delivery: "big" }));
	await expect
		.element(page.getByTestId("notification-unread").first())
		.toHaveText("1");
	expect(page.getByTestId("notification-banner").elements()).toHaveLength(0);
});

test("a task failing many times in a row offers Pause in its banner and the bell", async () => {
	const paused: Record<string, unknown>[] = [];
	const server = setup({
		tools: {
			task_update: (args: Record<string, unknown>) => {
				paused.push(args);
				return { ok: true };
			},
		},
	});
	await renderApp("/system/overview");
	await server.push(
		note({
			title: 'Scheduled prompt "assistant/release" has failed 5 times in a row',
			delivery: "big",
			pauseTask: "assistant/release",
		}),
	);
	const banner = page.getByTestId("notification-banner");
	await banner.getByTestId("notification-banner-pause").click();
	await expect
		.poll(() => paused)
		.toEqual([{ name: "assistant/release", enabled: false }]);
	await expect.poll(() => banner.elements().length).toBe(0);
	await expect
		.element(page.getByTestId("notification-paused"))
		.toHaveTextContent("Paused assistant/release");

	const bell = page
		.getByTestId("sidebar-footer")
		.getByTestId("notification-bell");
	await bell.click();
	const center = page.getByTestId("notification-center");
	await expect
		.element(center.getByTestId("notification-pause"))
		.toHaveText("Pause assistant/release");
	await center.getByTestId("notification-pause").click();
	await expect.poll(() => paused.length).toBe(2);
});

// Last: it changes the settings the other tests deliver with.
test("repeated failures are suppressed by default and configurable", async () => {
	const { state } = setup();
	await renderApp("/system/notifications");
	const repeats = page.getByTestId("notifications-repeats");
	await expect
		.element(repeats.getByTestId("notifications-repeat-suppress"))
		.toHaveAttribute("aria-checked", "true");
	await expect
		.element(repeats.getByTestId("notifications-streak"))
		.toHaveAttribute("aria-checked", "true");
	await expect
		.element(repeats.getByTestId("notifications-repeat-window"))
		.toHaveAttribute("data-value", "360");
	await expect
		.element(repeats.getByTestId("notifications-streak-count"))
		.toHaveAttribute("data-value", "5");

	await repeats.getByTestId("notifications-repeat-window").click();
	await page.getByRole("option", { name: "1 hour" }).click();
	await expect
		.poll(
			() =>
				(state.puts.at(-1)?.repeats as { windowMinutes?: number })
					?.windowMinutes,
		)
		.toBe(60);
	await repeats.getByTestId("notifications-streak").click();
	await expect
		.poll(() => (state.puts.at(-1)?.repeats as { streak?: boolean })?.streak)
		.toBe(false);
	await expect
		.element(repeats.getByTestId("notifications-streak-count"))
		.toBeDisabled();
});
