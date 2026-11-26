import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";
import {
	chooseOption,
	chooseValue,
	expectValue,
	optionLabels,
} from "./helpers/select";

interface Channel {
	type: string;
	id: string;
	name?: string;
	enabled?: boolean;
	deliver_to?: string;
}

interface Task {
	name: string;
	schedule?: string;
	prompt?: string;
	target?: string;
}

interface Config {
	agents: Array<{
		name: string;
		model: string;
		channels?: Channel[];
		tasks?: Task[];
	}>;
}

const NOW = new Date().toISOString();

/**
 * Mocks one agent with `channels`, keeping them as the channel tools change
 * them. Each channel's own session is named after its type and ID, as the
 * server names it, and lands in `sessions`.
 */
async function mockChannels(
	page: Page,
	channels: Channel[],
	opts: { tasks?: Task[]; agents?: string[] } = {},
) {
	const config: Config = {
		agents: [
			{
				name: "assistant",
				model: "x/y",
				channels,
				tasks: opts.tasks ?? [],
			},
			...(opts.agents ?? []).map((name) => ({ name, model: "x/y" })),
		],
	};
	const calls: Record<string, Record<string, unknown>[]> = {};
	// While fail is set, channel_update fails.
	const failUpdates = { fail: false };
	const record = (tool: string, args?: Record<string, unknown>) => {
		calls[tool] ??= [];
		calls[tool].push(args ?? {});
	};
	const sessionOf = (c: Channel) => ({
		id: `channel:${c.type}:${c.id}`,
		agent_id: "assistant",
		name: `channel:${c.type}:${c.id}`,
		created_at: NOW,
		own: true,
	});
	const find = (name: unknown) => {
		const ch = config.agents[0].channels ?? [];
		const i = ch.findIndex(
			(c) =>
				(c.name || c.id).toLowerCase() === String(name ?? "").toLowerCase(),
		);
		if (i < 0)
			throw new Error(`agent "assistant" has no channel named ${name}`);
		return i;
	};
	const messages: Record<string, unknown[]> = {
		"channel:slack:team-slack": [
			{
				role: "assistant",
				content: "Daily report: all green",
				timestamp: NOW,
			},
		],
	};
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: opts.agents?.length
			? [
					{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
					...opts.agents.map((name) => ({
						id: name,
						name,
						model: "x/y",
						state: "idle",
					})),
				]
			: [{ id: "assistant", name: "assistant", model: "x/y", state: "idle" }],
		config_get: () => config,
		config_save: (args) => {
			record("config_save", { ...args });
			Object.assign(config, JSON.parse(String(args?.config ?? "{}")));
			return "ok";
		},
		config_validate: [],
		job_list: [],
		auth_list: [],
		tool_list: [],
		session_list: [
			{ id: "main", agent_id: "assistant", name: "main", created_at: NOW },
		],
		session_messages: (args?: Record<string, unknown>) =>
			messages[String(args?.session_id)] ?? [],
		channel_sessions: (args?: Record<string, unknown>) => {
			record("channel_sessions", args);
			return [
				sessionOf((config.agents[0].channels ?? [])[find(args?.channel)]),
			];
		},
		channel_create: (args?: Record<string, unknown>) => {
			record("channel_create", args);
			const name = String(args?.name);
			const ch: Channel = {
				type: String(args?.type),
				id: name.toLowerCase().replace(/[^a-z0-9]+/g, "-"),
				name,
				enabled: false,
			};
			config.agents[0].channels = [...(config.agents[0].channels ?? []), ch];
			return { channel: ch, session_id: sessionOf(ch).id };
		},
		channel_rename: (args?: Record<string, unknown>) => {
			record("channel_rename", args);
			const ch = config.agents[0].channels ?? [];
			ch[find(args?.channel)].name = String(args?.new_name);
			return "renamed";
		},
		channel_update: (args?: Record<string, unknown>) => {
			record("channel_update", args);
			if (failUpdates.fail) throw new Error("disk is read-only");
			const ch = config.agents[0].channels ?? [];
			ch[find(args?.channel)].enabled = Boolean(args?.enabled);
			return "updated";
		},
		channel_remove: (args?: Record<string, unknown>) => {
			record("channel_remove", args);
			const i = find(args?.channel);
			config.agents[0].channels = (config.agents[0].channels ?? []).filter(
				(_, j) => j !== i,
			);
			return "removed";
		},
	});
	return { config, calls, failUpdates };
}

const TEAM: Channel = {
	type: "slack",
	id: "team-slack",
	name: "Team Slack",
	deliver_to: "C123",
};

function sidebarRows(page: Page) {
	return page.locator(
		"[data-testid=sidebar-sole-agent] > div[data-testid^=sidebar-row-]",
	);
}

test("the Channels section sits directly below Code Loop", async ({ page }) => {
	await mockChannels(page, [TEAM]);
	await page.goto("/agents/assistant/chat");
	await expect(page.getByTestId("sidebar-row-channels")).toBeVisible();

	const keys = await sidebarRows(page).evaluateAll((rows) =>
		rows.map((r) => r.getAttribute("data-testid")),
	);
	const projects = keys.indexOf("sidebar-row-projects");
	expect(projects).toBeGreaterThanOrEqual(0);
	expect(keys[projects + 1]).toBe("sidebar-row-channels");
	await expect(page.getByTestId("channel-row")).toHaveText(["Team Slack"]);

	// It lists the channels, not their sessions, which stay out of Chats.
	await expect(page.getByTestId("sidebar-chats")).not.toContainText(
		"channel:slack",
	);
});

test("the Channels section works in the collapsed rail and the mobile nav", async ({
	page,
}) => {
	await mockChannels(page, [TEAM]);
	await page.goto("/agents/assistant/chat");
	await page.getByTestId("sidebar-collapse").click();
	const rail = page.getByTestId("sidebar-rail-channels");
	await expect(rail).toBeVisible();
	await rail.click();
	await expect(page).toHaveURL(/\/agents\/assistant\/channels$/);

	await page.setViewportSize({ width: 390, height: 800 });
	await page.goto("/agents/assistant/channels");
	await expect(page.getByTestId("mobile-row-action")).toHaveAccessibleName(
		"Add channel",
	);
	await expect(
		page.getByRole("link", { name: "Team Slack", exact: true }).first(),
	).toBeVisible();
});

test("+ adds a channel, creating its session, and opens its settings", async ({
	page,
}) => {
	const { calls } = await mockChannels(page, []);
	await page.goto("/agents/assistant/channels");
	await expect(page.getByTestId("channels-empty")).toContainText(
		"No channels yet",
	);

	await page.getByTestId("sidebar-row-channels").hover();
	await page.getByRole("button", { name: "Add channel" }).first().click();
	const dialog = page.getByTestId("channel-dialog");
	await dialog.getByLabel("Channel name").fill("Ops Discord");
	await chooseValue(dialog.getByRole("combobox", { name: "Type" }), "discord");
	await dialog.getByRole("button", { name: "Create" }).click();

	expect(calls.channel_create).toEqual([
		{ agent: "assistant", name: "Ops Discord", type: "discord" },
	]);
	await expect(page).toHaveURL(
		/\/agents\/assistant\/channels\/Ops%20Discord\/settings$/,
	);
	await expect(page.getByTestId("channel-settings")).toBeVisible();
	await expect(
		page.getByTestId("channel-row").getByTestId("sidebar-item-link"),
	).toHaveText(["Ops Discord"]);

	// Its conversation opens on the channel's own session.
	await page.getByTestId("channel-open-conversation").click();
	await expect(page).toHaveURL(/\/agents\/assistant\/channels\/Ops%20Discord$/);
	await expect
		.poll(() => calls.channel_sessions?.length ?? 0)
		.toBeGreaterThan(0);
});

test("a channel's view shows its conversation and its gear opens its settings", async ({
	page,
}) => {
	await mockChannels(page, [TEAM]);
	await page.goto("/agents/assistant/chat");
	await page
		.getByTestId("channel-row")
		.getByTestId("sidebar-item-link")
		.click();

	await expect(page).toHaveURL(/\/agents\/assistant\/channels\/Team%20Slack$/);
	await expect(page.getByTestId("settings-title")).toHaveText("Team Slack");
	await expect(page.getByTestId("settings-header")).toContainText(
		"delivers to C123",
	);
	await expect(page.getByText("Daily report: all green")).toBeVisible();

	await page.getByTestId("channel-settings-link").click();
	await expect(page).toHaveURL(
		/\/agents\/assistant\/channels\/Team%20Slack\/settings$/,
	);
	await expect(page.getByTestId("channel-deliver-to")).toHaveValue("C123");
	await expect(page.getByTestId("channel-owner")).toHaveText("assistant");
});

test("… Rename, Settings and Delete act on a channel", async ({ page }) => {
	const { calls } = await mockChannels(page, [
		TEAM,
		{ type: "signal", id: "+15551234567", name: "Phone" },
	]);
	await page.goto("/agents/assistant/channels/Team%20Slack");
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
	await expect(page.getByRole("menuitem")).toHaveText([
		"Disable",
		"Rename",
		"Settings",
		"Delete",
	]);
	await page.keyboard.press("Escape");
	await expect(page.getByRole("menuitem")).toHaveCount(0);

	await menu("Team Slack", "Settings");
	await expect(page).toHaveURL(
		/\/agents\/assistant\/channels\/Team%20Slack\/settings$/,
	);

	// Rename keeps the channel's session: the view follows the new name.
	await page.goto("/agents/assistant/channels/Team%20Slack");
	await menu("Team Slack", "Rename");
	const input = page.getByTestId("channel-rename-input");
	await input.fill("Ops Slack");
	await input.press("Enter");
	expect(calls.channel_rename).toEqual([
		{ agent: "assistant", channel: "Team Slack", new_name: "Ops Slack" },
	]);
	await expect(page).toHaveURL(/\/agents\/assistant\/channels\/Ops%20Slack$/);
	await expect(page.getByTestId("settings-title")).toHaveText("Ops Slack");

	// Delete confirms and offers to delete the sessions, checked by default.
	await menu("Ops Slack", "Delete");
	const confirm = page.getByTestId("channel-remove-dialog");
	await expect(confirm).toContainText("Remove channel?");
	await expect(
		confirm.getByTestId("channel-remove-data-checkbox"),
	).toBeChecked();
	await confirm.getByRole("button", { name: "Cancel" }).click();
	await expect(confirm).toHaveCount(0);
	expect(calls.channel_remove).toBeUndefined();

	await menu("Ops Slack", "Delete");
	await confirm.getByTestId("channel-remove-data-checkbox").uncheck();
	await confirm.getByRole("button", { name: "Remove" }).click();
	expect(calls.channel_remove).toEqual([
		{ agent: "assistant", channel: "Ops Slack", delete_data: false },
	]);
	await expect(page).toHaveURL(/\/agents\/assistant\/channels$/);
	await expect(page.getByTestId("channel-row")).toHaveText(["Phone"]);
});

test("a scheduled task delivers to a channel picked by name", async ({
	page,
}) => {
	const { calls } = await mockChannels(page, [TEAM], {
		tasks: [{ name: "daily-report", schedule: "0 9 * * *", prompt: "report" }],
	});
	await page.goto("/agents/assistant/tasks?task=daily-report");
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

test("a scheduled task can send its result as a notification, on failure only (#1215)", async ({
	page,
}) => {
	const { calls } = await mockChannels(page, [TEAM], {
		tasks: [{ name: "daily-report", schedule: "0 9 * * *", prompt: "report" }],
	});
	await page.goto("/agents/assistant/tasks?task=daily-report");
	const sendVia = page.getByTestId("task-send-via");
	await chooseOption(sendVia, "Notify on failure only");
	await expectValue(sendVia, "notify?failures");
	await page.getByRole("button", { name: "Save Changes" }).click();

	await expect.poll(() => calls.config_save?.length ?? 0).toBe(1);
	const saved = JSON.parse(String(calls.config_save[0].config)) as Config;
	expect(saved.agents[0].tasks?.[0].target).toBe("notify?failures");
});

test("Agent Settings has no Channels section and its old address redirects", async ({
	page,
}) => {
	await mockChannels(page, [TEAM]);
	await page.goto("/agents/assistant/settings");
	await expect(page.getByTestId("settings-title")).toContainText(
		"Agent Settings",
	);
	await expect(page.getByTestId("channel-settings")).toHaveCount(0);
	await expect(
		page.getByRole("heading", { name: "Channels", exact: true }),
	).toHaveCount(0);

	await page.goto("/agents/assistant/settings/channels");
	await expect(page).toHaveURL(/\/agents\/assistant\/channels$/);
});

// Disabled channels: muted sidebar rows and mobile pills, and Enable/Disable
// in the row's "…" menu (#981).

const OFF: Channel = {
	type: "signal",
	id: "+15551234567",
	name: "Phone",
	enabled: false,
};

const channelRow = (page: Page, name: string) =>
	page
		.getByTestId("sidebar")
		.locator(`[data-testid=channel-row][data-channel="${name}"]`);

async function openChannelMenu(page: Page, name: string) {
	const row = channelRow(page, name);
	await row.hover();
	await row.getByTestId("channel-menu-button").click();
}

test("a disabled channel's sidebar row is muted, marked and sorted last", async ({
	page,
}) => {
	await mockChannels(page, [OFF, TEAM]);
	await page.goto("/agents/assistant/channels");

	const rows = page.getByTestId("sidebar").getByTestId("channel-row");
	await expect(rows.getByTestId("sidebar-item-link")).toHaveText([
		"Team Slack",
		"Phone",
	]);

	const off = channelRow(page, "Phone");
	await expect(off).toHaveAttribute("data-disabled", "true");
	await expect(off.getByTestId("channel-row-disabled")).toBeVisible();
	const link = off.getByTestId("sidebar-item-link");
	await expect(link).toHaveAttribute("title", "Disabled");
	await expect(link).toHaveClass(/italic/);

	const on = channelRow(page, "Team Slack");
	await expect(on).not.toHaveAttribute("data-disabled", "true");
	await expect(on.getByTestId("channel-row-disabled")).toHaveCount(0);
	await expect(on.getByTestId("sidebar-item-link")).toHaveAttribute(
		"title",
		"Team Slack",
	);
	await expect(on.getByTestId("sidebar-item-link")).not.toHaveClass(/italic/);
});

test("the mobile nav marks disabled channels too", async ({ page }) => {
	await mockChannels(page, [OFF, TEAM]);
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto("/agents/assistant/channels");

	const pill = page.locator('a[data-disabled="true"]');
	await expect(pill).toHaveCount(1);
	await expect(pill).toHaveText("Phone");
	await expect(pill).toHaveAttribute("title", "Disabled");
	await expect(pill).toHaveClass(/italic/);
	await expect(pill.getByTestId("mobile-channel-disabled")).toBeVisible();
	const enabled = page.getByRole("link", { name: "Team Slack", exact: true });
	await expect(enabled.first()).not.toHaveAttribute("title", "Disabled");
});

test("the sidebar menu disables and enables a channel without navigating", async ({
	page,
}) => {
	const { calls } = await mockChannels(page, [OFF, TEAM]);
	await page.goto("/agents/assistant/channels/Team%20Slack");
	const url = page.url();

	await openChannelMenu(page, "Phone");
	await expect(page.getByRole("menuitem")).toHaveText([
		"Enable",
		"Rename",
		"Settings",
		"Delete",
	]);
	await page.keyboard.press("Escape");

	await openChannelMenu(page, "Team Slack");
	await page.getByRole("menuitem", { name: "Disable" }).click();
	await expect(channelRow(page, "Team Slack")).toHaveAttribute(
		"data-disabled",
		"true",
	);
	expect(page.url()).toBe(url);
	expect(calls.channel_update).toEqual([
		{ agent: "assistant", channel: "Team Slack", enabled: false },
	]);

	await openChannelMenu(page, "Phone");
	await page.getByRole("menuitem", { name: "Enable" }).click();
	await expect(channelRow(page, "Phone")).not.toHaveAttribute(
		"data-disabled",
		"true",
	);
	await expect(
		page
			.getByTestId("sidebar")
			.getByTestId("channel-row")
			.getByTestId("sidebar-item-link"),
	).toHaveText(["Phone", "Team Slack"]);
	expect(calls.channel_update.at(-1)).toEqual({
		agent: "assistant",
		channel: "Phone",
		enabled: true,
	});
	expect(page.url()).toBe(url);
});

test("a failed channel toggle rolls back and shows an error toast", async ({
	page,
}) => {
	const { failUpdates } = await mockChannels(page, [OFF, TEAM]);
	await page.goto("/agents/assistant/channels");
	failUpdates.fail = true;

	await openChannelMenu(page, "Team Slack");
	await page.getByRole("menuitem", { name: "Disable" }).click();
	const toast = page.getByTestId("toast");
	await expect(toast).toContainText("Couldn't disable Team Slack");
	await expect(toast).toContainText("disk is read-only");
	await expect(toast).toHaveAttribute("data-tone", "error");
	await expect(channelRow(page, "Team Slack")).not.toHaveAttribute(
		"data-disabled",
		"true",
	);

	await openChannelMenu(page, "Phone");
	await page.getByRole("menuitem", { name: "Enable" }).click();
	await expect(toast.last()).toContainText("Couldn't enable Phone");
	await expect(channelRow(page, "Phone")).toHaveAttribute(
		"data-disabled",
		"true",
	);
});
