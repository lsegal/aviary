import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

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
async function mockOverview(
	page: Page,
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
	await setAuthToken(page);
	await mockMCP(page, {
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

test("the populated Channels overview summarizes channels, workflows and recent messages", async ({
	page,
}) => {
	const { calls } = await mockOverview(page, [SLACK, SIGNAL, DISCORD], TASKS);
	await page.goto("/agents/assistant/channels");

	const overview = page.getByTestId("channels-overview");
	await expect(page.getByTestId("page-title")).toHaveText("Channels");
	await expect(overview.getByTestId("channels-stats")).toMatchAriaSnapshot(`
		- paragraph: Channels
		- paragraph: "3"
		- paragraph: 2 enabled
		- paragraph: Messages 24h
		- paragraph: "9"
		- paragraph: last 5m ago
		- paragraph: Workflows
		- paragraph: "2"
		- paragraph: 1 active
		- paragraph: Running
		- paragraph: "1"
		- paragraph: workflow running
	`);

	// A card per channel, with its status, traffic and workflows.
	const cards = overview.getByTestId("channel-card");
	await expect(cards).toHaveCount(3);
	const slack = cards.filter({ has: page.getByText("Team Slack") });
	await expect(slack.getByTestId("channel-card-status")).toHaveText("Enabled");
	await expect(slack.getByTestId("channel-card-last-activity")).toHaveText(
		"5m ago",
	);
	await expect(slack.getByTestId("channel-card-messages")).toHaveText("7");
	await expect(slack.getByTestId("channel-card-workflows")).toHaveText("1");
	const discord = cards.filter({ has: page.getByText("Ops Discord") });
	await expect(discord.getByTestId("channel-card-status")).toHaveText(
		"Disabled",
	);
	await expect(discord.getByTestId("channel-card-last-activity")).toHaveText(
		"None yet",
	);
	await expect(slack.getByTestId("channel-card-settings")).toHaveAttribute(
		"href",
		"/agents/assistant/channels/Team%20Slack/settings",
	);
	await discord.getByTestId("channel-card-enabled").click();
	await expect
		.poll(() => calls.channel_update)
		.toEqual([{ agent: "assistant", channel: "Ops Discord", enabled: true }]);
	await expect(overview.getByTestId("channel-card-add")).toBeVisible();

	// The workflows as the shared status cards: the running one animates.
	const workflows = overview.getByTestId("workflow-card");
	await expect(workflows).toHaveCount(2);
	await expect(workflows.nth(0)).toHaveAttribute("data-workflow", "triage");
	await expect(workflows.nth(0)).toHaveAttribute("data-activity", "running");
	await expect(workflows.nth(1)).toHaveAttribute("data-paused", "true");

	// Recent messages: a card per channel, newest first, by sender name.
	const groups = overview.getByTestId("channels-overview-channel");
	await expect(groups).toHaveCount(3);
	const slackMessages = groups.filter({ hasText: "Team Slack" });
	await expect(slackMessages.getByTestId("message-sender")).toHaveText([
		"Alice",
		"Bob Stone",
	]);
	await expect(slackMessages.getByTestId("message-avatar")).toHaveText([
		"A",
		"BS",
	]);
	await expect(slackMessages.getByTestId("message-attachment")).toHaveText([
		"2 images",
		"File",
	]);
	await expect(slackMessages.getByTestId("message-workflow")).toHaveText(
		"triage",
	);
	const signalMessages = groups.filter({ hasText: "signal" });
	await expect(signalMessages.getByTestId("message-sender")).toHaveText([
		"Unknown sender",
		"Loren Segal",
	]);
	await expect(page.getByText("+15551112222")).toHaveCount(0);
	await expect(groups.filter({ hasText: "Ops Discord" })).toContainText(
		"No messages yet.",
	);

	// Long text fits one line, with the rest in a tooltip and on click.
	const long = signalMessages.getByTestId("message-text").nth(1);
	await expect(long).toHaveAttribute("title", LONG);
	const oneLine = (await long.boundingBox())?.height ?? 0;
	await long.click();
	await expect(long).toHaveAttribute("aria-expanded", "true");
	expect((await long.boundingBox())?.height ?? 0).toBeGreaterThan(oneLine);

	// Searching filters the messages across channels.
	await overview.getByTestId("channels-overview-search").fill("crash");
	await expect(overview.getByTestId("channels-overview-message")).toHaveCount(
		1,
	);
	await expect(signalMessages).toContainText("No messages match.");
	await overview.getByTestId("channels-overview-search").fill("");

	expect(calls.channel_activity).toContainEqual({
		agent: "assistant",
		channel: "Team Slack",
		limit: 5,
	});

	// The heading opens the channel's chat.
	await slackMessages.getByTestId("channels-overview-channel-link").click();
	await expect(page).toHaveURL(/\/agents\/assistant\/channels\/Team%20Slack$/);
});

test("Add workflow picks a channel and opens its Workflows tab", async ({
	page,
}) => {
	await mockOverview(page, [SLACK, SIGNAL], TASKS);
	await page.goto("/agents/assistant/channels");

	await page.getByTestId("add-workflow").first().click();
	const menu = page.getByTestId("add-workflow-menu");
	await expect(menu.getByTestId("add-workflow-channel")).toHaveText([
		"Team Slack",
		"signal",
	]);
	await menu.getByText("signal").click();
	await expect(page).toHaveURL(
		/\/agents\/assistant\/channels\/signal\/settings\?tab=workflows$/,
	);
});

test("the empty Channels overview offers to add a channel and a workflow", async ({
	page,
}) => {
	await mockOverview(page, []);
	await page.goto("/agents/assistant/channels");

	const overview = page.getByTestId("channels-overview");
	await expect(overview.getByTestId("channels-empty")).toMatchAriaSnapshot(`
		- paragraph: No channels yet
		- paragraph: A channel connects this agent to Slack, Discord or Signal, and keeps its conversation here.
		- button "Add channel"
	`);
	await expect(overview.getByTestId("workflows-empty")).toContainText(
		"No workflows yet",
	);
	// A workflow watches a channel, so there is none to pick yet.
	await expect(
		overview.getByTestId("workflows-empty").getByTestId("add-workflow"),
	).toBeDisabled();
	await expect(overview.getByTestId("channels-overview-recent")).toHaveCount(0);

	await overview.getByTestId("channels-empty-add").click();
	await expect(page.getByTestId("channel-dialog")).toBeVisible();
});

test("with one channel and no workflows, Add workflow opens that channel's Workflows tab", async ({
	page,
}) => {
	await mockOverview(page, [SLACK]);
	await page.goto("/agents/assistant/channels");

	const empty = page.getByTestId("workflows-empty");
	await expect(empty).toContainText("No workflows yet");
	await empty.getByTestId("add-workflow").click();
	await expect(page).toHaveURL(
		/\/agents\/assistant\/channels\/Team%20Slack\/settings\?tab=workflows$/,
	);
});

for (const [label, width] of [
	["desktop", 1280],
	["tablet", 820],
	["phone", 390],
] as const) {
	test(`nothing on the Channels overview overflows sideways at ${label} width`, async ({
		page,
	}) => {
		await page.setViewportSize({ width, height: 900 });
		await mockOverview(page, [SLACK, SIGNAL, DISCORD], TASKS);
		await page.goto("/agents/assistant/channels");

		const overview = page.getByTestId("channels-overview");
		await expect(overview.getByTestId("channels-overview-message")).toHaveCount(
			4,
		);
		const overflow = await overview.evaluate((el) => {
			const box = el.getBoundingClientRect();
			const wide = [...el.querySelectorAll<HTMLElement>("*")]
				.filter((n) => n.getBoundingClientRect().right > box.right + 1)
				.map((n) => n.dataset.testid || n.tagName);
			return { scroll: el.scrollWidth - el.clientWidth, wide };
		});
		expect(overflow).toEqual({ scroll: 0, wide: [] });
	});
}
