import { beforeEach, expect, test } from "vitest";
import { type Locator, page } from "vitest/browser";
import { currentURL, renderApp } from "../component-test/app";
import { mockMCP, requestLog } from "../component-test/server";
import { useChannelDialogs } from "../composables/useChannelDialogs";

// The add-channel dialog's open state is module state, which a page load
// starts without; close what an earlier test in the file opened.
beforeEach(() => {
	useChannelDialogs().close();
});

const NOW = Date.now();
const iso = (msAgo: number) => new Date(NOW - msAgo).toISOString();
const LONG =
	"update issue: if there is a workflow on an entry, the chat should show which workflow handled each message, with a link to its run, and the run should link back to the message that triggered it so people can follow the whole thread";

interface Channel {
	type: string;
	id: string;
	name?: string;
	enabled?: boolean;
}

const SLACK: Channel = { type: "slack", id: "team-slack", name: "Team Slack" };
const SIGNAL: Channel = { type: "signal", id: "+15550000000", name: "signal" };
const DISCORD: Channel = {
	type: "discord",
	id: "ops",
	name: "Ops Discord",
	enabled: false,
};

const ACTIVITY: Record<string, unknown> = {
	"Team Slack": {
		last_activity: iso(5 * 60_000),
		messages_24h: 7,
		messages: [
			{
				entry: "C1",
				conversation: "#general",
				sender: "Bob Stone",
				text: "deploying now",
				timestamp: iso(30 * 60_000),
			},
			{
				entry: "C1",
				conversation: "#general",
				sender: "Alice",
				text: "the app crashed, logs attached",
				timestamp: iso(5 * 60_000),
				attachments: ["image", "image", "file"],
				workflows: ["triage"],
			},
		],
	},
	signal: {
		last_activity: iso(2 * 3_600_000),
		messages_24h: 2,
		messages: [
			{
				entry: "+15551112222",
				sender: "Loren Segal",
				text: LONG,
				timestamp: iso(3 * 3_600_000),
			},
			{
				entry: "group-1",
				text: "hello from someone nobody knows",
				timestamp: iso(2 * 3_600_000),
			},
		],
	},
	"Ops Discord": { messages_24h: 0, messages: [] },
};

/** Mocks the agent "assistant" with channels and message workflows. */
function mockOverview(
	channels: Channel[],
	tasks: Record<string, unknown>[] = [],
) {
	const config = {
		agents: [{ name: "assistant", model: "x/y", channels, tasks }],
	};
	const calls: Record<string, Record<string, unknown>[]> = {};
	const record = (tool: string, args?: Record<string, unknown>) => {
		calls[tool] ??= [];
		calls[tool].push(args ?? {});
	};
	mockMCP({
		agent_list: [
			{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
		],
		config_get: () => config,
		config_validate: [],
		auth_list: [],
		tool_list: [],
		session_list: [
			{
				id: "main",
				agent_id: "assistant",
				name: "main",
				created_at: iso(0),
			},
		],
		channel_sessions: [],
		channel_activity: (args?: Record<string, unknown>) => {
			record("channel_activity", args);
			return (
				ACTIVITY[String(args?.channel)] ?? { messages_24h: 0, messages: [] }
			);
		},
		channel_update: (args?: Record<string, unknown>) => {
			record("channel_update", args);
			const c = channels.find((ch) => ch.name === args?.channel);
			if (c && typeof args?.enabled === "boolean") c.enabled = args.enabled;
			return "updated";
		},
		workflow_entries: [],
		workflow_preview: [],
		job_query: [
			{
				id: "job-1",
				task_id: "assistant/triage",
				status: "in_progress",
				created_at: iso(10_000),
			},
		],
		job_list: [],
		task_list: [],
	});
	return { calls };
}

/** Waits until the app has made no requests for a while. Any re-render of
 * the overview collapses an expanded message, so a click on one waits for
 * the reloads that follow toggling a channel to land. */
async function settled() {
	let last = -1;
	while (requestLog().length !== last) {
		last = requestLog().length;
		await new Promise((r) => setTimeout(r, 500));
	}
}

/** Each element's whitespace-normalized text, as Playwright's toHaveText
 * with an array compares them. */
function texts(loc: Locator): string[] {
	return loc
		.elements()
		.map((el) => (el.textContent ?? "").replace(/\s+/g, " ").trim());
}

/** The texts of the paragraphs within loc, in order: what the specs'
 * aria snapshots of paragraphs checked. */
function paragraphs(loc: Locator): string[] {
	return texts(loc.locator("p"));
}

const TASKS = [
	{
		name: "triage",
		trigger: "message",
		channel: "Team Slack",
		match: "*crash*",
		prompt: "Triage it.",
	},
	{
		name: "archive",
		enabled: false,
		trigger: "message",
		channel: "signal",
		prompt: "Archive it.",
	},
];

test("the populated Channels overview summarizes channels, workflows and recent messages", async () => {
	const { calls } = mockOverview(
		[{ ...SLACK }, { ...SIGNAL }, { ...DISCORD }],
		TASKS,
	);
	await renderApp("/agents/assistant/channels");

	const overview = page.getByTestId("channels-overview");
	await expect
		.element(page.getByTestId("settings-title"))
		.toHaveText("Channels");
	await expect
		.poll(() => paragraphs(overview.getByTestId("channels-stats")))
		.toEqual([
			"Channels",
			"3",
			"2 enabled",
			"Messages 24h",
			"9",
			"last 5m ago",
			"Workflows",
			"2",
			"1 active",
			"Running",
			"1",
			"workflow running",
		]);

	// A card per channel, with its status, traffic and workflows.
	const cards = overview.getByTestId("channel-card");
	await expect.poll(() => cards.elements().length).toBe(3);
	const slack = cards.filter({ has: page.getByText("Team Slack") });
	await expect
		.element(slack.getByTestId("channel-card-status"))
		.toHaveText("Enabled");
	await expect
		.element(slack.getByTestId("channel-card-last-activity"))
		.toHaveText("5m ago");
	await expect
		.element(slack.getByTestId("channel-card-messages"))
		.toHaveText("7");
	await expect
		.element(slack.getByTestId("channel-card-workflows"))
		.toHaveText("1");
	const discord = cards.filter({ has: page.getByText("Ops Discord") });
	await expect
		.element(discord.getByTestId("channel-card-status"))
		.toHaveText("Disabled");
	await expect
		.element(discord.getByTestId("channel-card-last-activity"))
		.toHaveText("None yet");
	await expect
		.element(slack.getByTestId("channel-card-settings"))
		.toHaveAttribute(
			"href",
			"/agents/assistant/channels/Team%20Slack/settings",
		);
	await discord.getByTestId("channel-card-enabled").click();
	await expect
		.poll(() => calls.channel_update)
		.toEqual([{ agent: "assistant", channel: "Ops Discord", enabled: true }]);
	await expect.element(overview.getByTestId("channel-card-add")).toBeVisible();

	// The workflows as the shared status cards: the running one animates.
	const workflows = overview.getByTestId("workflow-card");
	await expect.poll(() => workflows.elements().length).toBe(2);
	await expect
		.element(workflows.nth(0))
		.toHaveAttribute("data-workflow", "triage");
	await expect
		.element(workflows.nth(0))
		.toHaveAttribute("data-activity", "running");
	await expect.element(workflows.nth(1)).toHaveAttribute("data-paused", "true");

	// Recent messages: a card per channel, newest first, by sender name.
	const groups = overview.getByTestId("channels-overview-channel");
	await expect.poll(() => groups.elements().length).toBe(3);
	const slackMessages = groups.filter({ hasText: "Team Slack" });
	await expect
		.poll(() => texts(slackMessages.getByTestId("message-sender")))
		.toEqual(["Alice", "Bob Stone"]);
	await expect
		.poll(() => texts(slackMessages.getByTestId("message-avatar")))
		.toEqual(["A", "BS"]);
	await expect
		.poll(() => texts(slackMessages.getByTestId("message-attachment")))
		.toEqual(["2 images", "File"]);
	await expect
		.element(slackMessages.getByTestId("message-workflow"))
		.toHaveText("triage");
	const signalMessages = groups.filter({ hasText: "signal" });
	await expect
		.poll(() => texts(signalMessages.getByTestId("message-sender")))
		.toEqual(["Unknown sender", "Loren Segal"]);
	expect(page.getByText("+15551112222").elements()).toHaveLength(0);
	await expect
		.element(groups.filter({ hasText: "Ops Discord" }))
		.toMatchTextContent("No messages yet.");

	// Long text fits one line, with the rest in a tooltip and on click.
	const long = signalMessages.getByTestId("message-text").nth(1);
	await expect.element(long).toHaveAttribute("title", LONG);
	await settled();
	const oneLine = long.element().getBoundingClientRect().height;
	await long.click();
	await expect.element(long).toHaveAttribute("aria-expanded", "true");
	expect(long.element().getBoundingClientRect().height).toBeGreaterThan(
		oneLine,
	);

	// Searching filters the messages across channels.
	await overview.getByTestId("channels-overview-search").fill("crash");
	await expect
		.poll(
			() => overview.getByTestId("channels-overview-message").elements().length,
		)
		.toBe(1);
	await expect.element(signalMessages).toMatchTextContent("No messages match.");
	await overview.getByTestId("channels-overview-search").fill("");

	expect(calls.channel_activity).toContainEqual({
		agent: "assistant",
		channel: "Team Slack",
		limit: 5,
	});

	// The heading opens the channel's chat.
	await slackMessages.getByTestId("channels-overview-channel-link").click();
	await expect
		.poll(currentURL)
		.toMatch(/\/agents\/assistant\/channels\/Team%20Slack$/);
});

test("Add workflow picks a channel and opens its Workflows tab", async () => {
	mockOverview([{ ...SLACK }, { ...SIGNAL }], TASKS);
	await renderApp("/agents/assistant/channels");

	await page.getByTestId("add-workflow").first().click();
	const menu = page.getByTestId("add-workflow-menu");
	await expect
		.poll(() => texts(menu.getByTestId("add-workflow-channel")))
		.toEqual(["Team Slack", "signal"]);
	await menu.getByText("signal").click();
	await expect
		.poll(currentURL)
		.toMatch(/\/agents\/assistant\/channels\/signal\/settings\?tab=workflows$/);
});

test("the empty Channels overview offers to add a channel and a workflow", async () => {
	mockOverview([]);
	await renderApp("/agents/assistant/channels");

	const overview = page.getByTestId("channels-overview");
	const empty = overview.getByTestId("channels-empty");
	await expect
		.poll(() => paragraphs(empty))
		.toEqual([
			"No channels yet",
			"A channel connects this agent to Slack, Discord or Signal, and keeps its conversation here.",
		]);
	await expect
		.element(empty.getByRole("button", { name: "Add channel", exact: true }))
		.toBeVisible();
	await expect
		.element(overview.getByTestId("workflows-empty"))
		.toMatchTextContent("No workflows yet");
	// A workflow watches a channel, so there is none to pick yet.
	await expect
		.element(
			overview.getByTestId("workflows-empty").getByTestId("add-workflow"),
		)
		.toBeDisabled();
	expect(
		overview.getByTestId("channels-overview-recent").elements(),
	).toHaveLength(0);

	await overview.getByTestId("channels-empty-add").click();
	await expect.element(page.getByTestId("channel-dialog")).toBeVisible();
});

test("with one channel and no workflows, Add workflow opens that channel's Workflows tab", async () => {
	mockOverview([{ ...SLACK }]);
	await renderApp("/agents/assistant/channels");

	const empty = page.getByTestId("workflows-empty");
	await expect.element(empty).toMatchTextContent("No workflows yet");
	await empty.getByTestId("add-workflow").click();
	await expect
		.poll(currentURL)
		.toMatch(
			/\/agents\/assistant\/channels\/Team%20Slack\/settings\?tab=workflows$/,
		);
});

for (const [label, width] of [
	["desktop", 1280],
	["tablet", 820],
	["phone", 390],
] as const) {
	test(`nothing on the Channels overview overflows sideways at ${label} width`, async () => {
		await page.viewport(width, 900);
		mockOverview([{ ...SLACK }, { ...SIGNAL }, { ...DISCORD }], TASKS);
		await renderApp("/agents/assistant/channels");

		const overview = page.getByTestId("channels-overview");
		await expect
			.poll(
				() =>
					overview.getByTestId("channels-overview-message").elements().length,
			)
			.toBe(4);
		const el = overview.element();
		const box = el.getBoundingClientRect();
		const wide = [...el.querySelectorAll<HTMLElement>("*")]
			.filter((n) => n.getBoundingClientRect().right > box.right + 1)
			.map((n) => n.dataset.testid || n.tagName);
		expect({ scroll: el.scrollWidth - el.clientWidth, wide }).toEqual({
			scroll: 0,
			wide: [],
		});
	});
}
