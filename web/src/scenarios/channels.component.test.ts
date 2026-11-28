import { expect, test } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { currentURL, goto, renderApp } from "../component-test/app";
import {
	type Channel,
	type Config,
	mockChannels,
	TEAM,
} from "./helpers/channelsServer";
import {
	chooseOption,
	chooseValue,
	expectValue,
	optionLabels,
} from "./helpers/select";
// The pages this file renders (see component-test/routes.ts).
import "../views/ChannelView.vue";
import "../views/ChatView.vue";
import "../views/SettingsView.vue";

/** Each matching element's whitespace-normalized text, in order, as
 * Playwright's toHaveText([...]) compares them. */
function texts(locator: Locator): string[] {
	return locator
		.elements()
		.map((el) => (el.textContent ?? "").replace(/\s+/g, " ").trim());
}

function sidebarRows() {
	return page.locator(
		"[data-testid=sidebar-sole-agent] > div[data-testid^=sidebar-row-]",
	);
}

test("the Channels section sits directly below Code Loop", async () => {
	mockChannels([TEAM]);
	await renderApp("/agents/assistant/chat");
	await expect.element(page.getByTestId("sidebar-row-channels")).toBeVisible();

	const keys = sidebarRows()
		.elements()
		.map((r) => r.getAttribute("data-testid"));
	const projects = keys.indexOf("sidebar-row-projects");
	expect(projects).toBeGreaterThanOrEqual(0);
	expect(keys[projects + 1]).toBe("sidebar-row-channels");
	await expect
		.poll(() => texts(page.getByTestId("channel-row")))
		.toEqual(["Team Slack"]);

	// It lists the channels, not their sessions, which stay out of Chats.
	await expect
		.element(page.getByTestId("sidebar-chats"))
		.not.toMatchTextContent("channel:slack");
});

test("+ adds a channel, creating its session, and opens its settings", async () => {
	const { calls } = mockChannels([]);
	await renderApp("/agents/assistant/channels");
	await expect
		.element(page.getByTestId("channels-empty"))
		.toMatchTextContent("No channels yet");

	await page.getByTestId("sidebar-row-channels").hover();
	await page.getByRole("button", { name: "Add channel" }).first().click();
	const dialog = page.getByTestId("channel-dialog");
	await dialog.getByLabelText("Channel name").fill("Ops Discord");
	await chooseValue(dialog.getByRole("combobox", { name: "Type" }), "discord");
	await dialog.getByRole("button", { name: "Create" }).click();

	await expect
		.poll(() => calls.channel_create)
		.toEqual([{ agent: "assistant", name: "Ops Discord", type: "discord" }]);
	await expect
		.poll(currentURL)
		.toMatch(/\/agents\/assistant\/channels\/Ops%20Discord\/settings$/);
	await expect.element(page.getByTestId("channel-settings")).toBeVisible();
	await expect
		.poll(() =>
			texts(page.getByTestId("channel-row").getByTestId("sidebar-item-link")),
		)
		.toEqual(["Ops Discord"]);

	// Its conversation opens on the channel's own session.
	await page.getByTestId("channel-open-conversation").click();
	await expect
		.poll(currentURL)
		.toMatch(/\/agents\/assistant\/channels\/Ops%20Discord$/);
	await expect
		.poll(() => calls.channel_sessions?.length ?? 0)
		.toBeGreaterThan(0);
});

test("a channel's view shows its conversation and its gear opens its settings", async () => {
	mockChannels([TEAM]);
	await renderApp("/agents/assistant/chat");
	await page
		.getByTestId("channel-row")
		.getByTestId("sidebar-item-link")
		.click();

	await expect
		.poll(currentURL)
		.toMatch(/\/agents\/assistant\/channels\/Team%20Slack$/);
	await expect
		.element(page.getByTestId("settings-title"))
		.toHaveText("Team Slack");
	await expect
		.element(page.getByTestId("settings-header"))
		.toMatchTextContent("delivers to C123");
	await expect.element(page.getByText("Daily report: all green")).toBeVisible();

	await page.getByTestId("channel-settings-link").click();
	await expect
		.poll(currentURL)
		.toMatch(/\/agents\/assistant\/channels\/Team%20Slack\/settings$/);
	await expect
		.element(page.getByTestId("channel-deliver-to"))
		.toHaveValue("C123");
	await expect
		.element(page.getByTestId("channel-owner"))
		.toHaveText("assistant");
});

test("… Rename, Settings and Delete act on a channel", async () => {
	const { calls } = mockChannels([
		TEAM,
		{ type: "signal", id: "+15551234567", name: "Phone" },
	]);
	await renderApp("/agents/assistant/channels/Team%20Slack");
	const row = (name: string) =>
		page.locator(`[data-testid=channel-row][data-channel="${name}"]`);
	// "…" shows on hover, like a pointer reaching it (#985).
	const openMenu = async (name: string) => {
		await row(name).hover();
		await row(name).getByTestId("channel-menu-button").click();
	};
	const menu = async (name: string, item: string) => {
		await openMenu(name);
		await page.getByRole("menuitem", { name: item }).click();
	};

	await openMenu("Team Slack");
	await expect
		.poll(() => texts(page.getByRole("menuitem")))
		.toEqual(["Disable", "Rename", "Settings", "Delete"]);
	await commands.keyPress("Escape");
	await expect.poll(() => page.getByRole("menuitem").elements().length).toBe(0);

	await menu("Team Slack", "Settings");
	await expect
		.poll(currentURL)
		.toMatch(/\/agents\/assistant\/channels\/Team%20Slack\/settings$/);

	// Rename keeps the channel's session: the view follows the new name.
	await goto("/agents/assistant/channels/Team%20Slack");
	await menu("Team Slack", "Rename");
	const input = page.getByTestId("channel-rename-input");
	await input.fill("Ops Slack");
	await commands.keyPress("Enter");
	await expect
		.poll(() => calls.channel_rename)
		.toEqual([
			{ agent: "assistant", channel: "Team Slack", new_name: "Ops Slack" },
		]);
	await expect
		.poll(currentURL)
		.toMatch(/\/agents\/assistant\/channels\/Ops%20Slack$/);
	await expect
		.element(page.getByTestId("settings-title"))
		.toHaveText("Ops Slack");

	// Delete confirms and offers to delete the sessions, checked by default.
	await menu("Ops Slack", "Delete");
	const confirm = page.getByTestId("channel-remove-dialog");
	await expect.element(confirm).toMatchTextContent("Remove channel?");
	await expect
		.element(confirm.getByTestId("channel-remove-data-checkbox"))
		.toBeChecked();
	await confirm.getByRole("button", { name: "Cancel" }).click();
	await expect.poll(() => confirm.elements().length).toBe(0);
	expect(calls.channel_remove).toBeUndefined();

	await menu("Ops Slack", "Delete");
	await confirm.getByTestId("channel-remove-data-checkbox").click();
	await expect
		.element(confirm.getByTestId("channel-remove-data-checkbox"))
		.not.toBeChecked();
	await confirm.getByRole("button", { name: "Remove" }).click();
	await expect
		.poll(() => calls.channel_remove)
		.toEqual([
			{ agent: "assistant", channel: "Ops Slack", delete_data: false },
		]);
	await expect.poll(currentURL).toMatch(/\/agents\/assistant\/channels$/);
	await expect
		.poll(() => texts(page.getByTestId("channel-row")))
		.toEqual(["Phone"]);
});

test("a scheduled task delivers to a channel picked by name", async () => {
	const { calls } = mockChannels([TEAM], {
		tasks: [{ name: "daily-report", schedule: "0 9 * * *", prompt: "report" }],
	});
	await renderApp("/agents/assistant/tasks?task=daily-report");
	const sendVia = page.getByTestId("task-send-via");
	expect(await optionLabels(sendVia)).toEqual([
		"silent",
		"Session",
		"Code Loop job",
		"Notify",
		"Notify on failure only",
		"Channel: Team Slack",
	]);
	await chooseOption(sendVia, "Channel: Team Slack");
	await page.getByRole("button", { name: "Save Changes" }).click();

	await expect.poll(() => calls.config_save?.length ?? 0).toBe(1);
	const saved = JSON.parse(String(calls.config_save[0].config)) as Config;
	expect(saved.agents[0].tasks?.[0].target).toBe("channel:Team Slack");
});

test("a scheduled task can send its result as a notification, on failure only (#1215)", async () => {
	const { calls } = mockChannels([TEAM], {
		tasks: [{ name: "daily-report", schedule: "0 9 * * *", prompt: "report" }],
	});
	await renderApp("/agents/assistant/tasks?task=daily-report");
	const sendVia = page.getByTestId("task-send-via");
	await chooseOption(sendVia, "Notify on failure only");
	await expectValue(sendVia, "notify?failures");
	await page.getByRole("button", { name: "Save Changes" }).click();

	await expect.poll(() => calls.config_save?.length ?? 0).toBe(1);
	const saved = JSON.parse(String(calls.config_save[0].config)) as Config;
	expect(saved.agents[0].tasks?.[0].target).toBe("notify?failures");
});

test("Agent Settings has no Channels section and its old address redirects", async () => {
	mockChannels([TEAM]);
	await renderApp("/agents/assistant/settings");
	await expect
		.element(page.getByTestId("settings-title"))
		.toMatchTextContent("Agent Settings");
	expect(page.getByTestId("channel-settings").elements()).toHaveLength(0);
	expect(
		page.getByRole("heading", { name: "Channels", exact: true }).elements(),
	).toHaveLength(0);

	await goto("/agents/assistant/settings/channels");
	await expect.poll(currentURL).toMatch(/\/agents\/assistant\/channels$/);
});

// Disabled channels: muted sidebar rows and mobile pills, and Enable/Disable
// in the row's "…" menu (#981).

const OFF: Channel = {
	type: "signal",
	id: "+15551234567",
	name: "Phone",
	enabled: false,
};

const channelRow = (name: string) =>
	page
		.getByTestId("sidebar")
		.locator(`[data-testid=channel-row][data-channel="${name}"]`);

async function openChannelMenu(name: string) {
	const row = channelRow(name);
	await row.hover();
	await row.getByTestId("channel-menu-button").click();
}

test("a disabled channel's sidebar row is muted, marked and sorted last", async () => {
	mockChannels([OFF, TEAM]);
	await renderApp("/agents/assistant/channels");

	const rows = page.getByTestId("sidebar").getByTestId("channel-row");
	await expect
		.poll(() => texts(rows.getByTestId("sidebar-item-link")))
		.toEqual(["Team Slack", "Phone"]);

	const off = channelRow("Phone");
	await expect.element(off).toHaveAttribute("data-disabled", "true");
	await expect.element(off.getByTestId("channel-row-disabled")).toBeVisible();
	const link = off.getByTestId("sidebar-item-link");
	await expect.element(link).toHaveAttribute("title", "Disabled");
	await expect.element(link).toHaveClass(/italic/);

	const on = channelRow("Team Slack");
	await expect.element(on).not.toHaveAttribute("data-disabled", "true");
	expect(on.getByTestId("channel-row-disabled").elements()).toHaveLength(0);
	await expect
		.element(on.getByTestId("sidebar-item-link"))
		.toHaveAttribute("title", "Team Slack");
	await expect
		.element(on.getByTestId("sidebar-item-link"))
		.not.toHaveClass(/italic/);
});

test("the mobile nav marks disabled channels too", async () => {
	mockChannels([OFF, TEAM]);
	await page.viewport(390, 844);
	await renderApp("/agents/assistant/channels");

	const pill = page.locator('a[data-disabled="true"]');
	await expect.poll(() => pill.elements().length).toBe(1);
	await expect.element(pill).toHaveText("Phone");
	await expect.element(pill).toHaveAttribute("title", "Disabled");
	await expect.element(pill).toHaveClass(/italic/);
	await expect
		.element(pill.getByTestId("mobile-channel-disabled"))
		.toBeVisible();
	const enabled = page.getByRole("link", { name: "Team Slack", exact: true });
	await expect
		.element(enabled.first())
		.not.toHaveAttribute("title", "Disabled");
});

test("the sidebar menu disables and enables a channel without navigating", async () => {
	const { calls } = mockChannels([OFF, TEAM]);
	await renderApp("/agents/assistant/channels/Team%20Slack");
	const url = currentURL();

	await openChannelMenu("Phone");
	await expect
		.poll(() => texts(page.getByRole("menuitem")))
		.toEqual(["Enable", "Rename", "Settings", "Delete"]);
	await commands.keyPress("Escape");

	await openChannelMenu("Team Slack");
	await page.getByRole("menuitem", { name: "Disable" }).click();
	await expect
		.element(channelRow("Team Slack"))
		.toHaveAttribute("data-disabled", "true");
	expect(currentURL()).toBe(url);
	expect(calls.channel_update).toEqual([
		{ agent: "assistant", channel: "Team Slack", enabled: false },
	]);

	await openChannelMenu("Phone");
	await page.getByRole("menuitem", { name: "Enable" }).click();
	await expect
		.element(channelRow("Phone"))
		.not.toHaveAttribute("data-disabled", "true");
	await expect
		.poll(() =>
			texts(
				page
					.getByTestId("sidebar")
					.getByTestId("channel-row")
					.getByTestId("sidebar-item-link"),
			),
		)
		.toEqual(["Phone", "Team Slack"]);
	expect(calls.channel_update.at(-1)).toEqual({
		agent: "assistant",
		channel: "Phone",
		enabled: true,
	});
	expect(currentURL()).toBe(url);
});

test("a failed channel toggle rolls back and shows an error toast", async () => {
	const { failUpdates } = mockChannels([OFF, TEAM]);
	await renderApp("/agents/assistant/channels");
	failUpdates.fail = true;

	await openChannelMenu("Team Slack");
	await page.getByRole("menuitem", { name: "Disable" }).click();
	const toast = page.getByTestId("toast");
	await expect
		.element(toast.first())
		.toMatchTextContent("Couldn't disable Team Slack");
	await expect.element(toast.first()).toMatchTextContent("disk is read-only");
	await expect.element(toast.first()).toHaveAttribute("data-tone", "error");
	await expect
		.element(channelRow("Team Slack"))
		.not.toHaveAttribute("data-disabled", "true");

	await openChannelMenu("Phone");
	await page.getByRole("menuitem", { name: "Enable" }).click();
	await expect
		.element(toast.last())
		.toMatchTextContent("Couldn't enable Phone");
	await expect
		.element(channelRow("Phone"))
		.toHaveAttribute("data-disabled", "true");
});
