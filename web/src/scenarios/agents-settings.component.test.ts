// Agent, channel and system settings scenarios, ported from
// web/e2e/agents.spec.ts (#1636). Task and file scenarios live in
// agents-tasks.component.test.ts.
import { beforeEach, expect, test } from "vitest";
import { page } from "vitest/browser";
import { currentURL, renderApp } from "../component-test/app";
import { mockMCP } from "../component-test/server";
import {
	type Args,
	ASSISTANT,
	CONFIG,
	count,
	expectHidden,
	mockDefault,
	TOOLS,
	texts,
} from "./agents.fixtures";
import {
	agentPageLink,
	openAgentPage,
	openChannelSettings,
	openGearMenuItem,
	openTaskSettings,
	systemList,
} from "./helpers/sidebar";
// The pages this file renders (see component-test/routes.ts).
import "../views/ChannelView.vue";
import "../views/OverviewView.vue";
import "../views/SettingsView.vue";

beforeEach(() => {
	mockDefault();
});

test("agents and tasks tab shows configured entries", async () => {
	await openAgentPage("settings");

	await expect
		.element(
			page.getByRole("button", { name: "Remove Agent", exact: true }).first(),
		)
		.toBeVisible();
	// General subtab (default): agent name field is always visible in the header
	await expect
		.element(page.locator('input[placeholder="assistant"]').first())
		.toHaveValue("assistant");

	// Tasks subtab
	await agentPageLink("Scheduled Tasks").click();
	// The root page is the overview; a task's Settings opens it for editing.
	await openTaskSettings();
	await expect
		.element(page.locator('input[placeholder="daily-briefing"]').first())
		.toHaveValue("daily-briefing");
	await expect.element(page.getByTestId("paused-chip").first()).toBeVisible();
	await expect
		.poll(() => count(page.getByText("disabled", { exact: true })))
		.toBe(0);
	await expect
		.element(page.getByRole("switch", { name: "Toggle task enabled" }).first())
		.toBeVisible();
	await expect
		.element(
			page.getByRole("heading", { name: "Scheduled Tasks", exact: true }),
		)
		.toBeVisible();

	// The agent's Settings page stacks General and Permissions; each channel
	// has its own settings page.
	await agentPageLink("Agent Settings").click();
	await expect.poll(currentURL).toMatch(/\/agents\/assistant\/settings$/);
	await expect
		.poll(() =>
			count(page.getByRole("heading", { name: "Channels", exact: true })),
		)
		.toBe(0);
	await openChannelSettings("+15551234567");
	await expect
		.element(page.locator('input[placeholder="e.g. +15551234567"]').first())
		.toHaveValue("+15551234567");
	await expect
		.element(page.locator('input[placeholder="*, +15551234567"]').first())
		.toHaveValue("*");
});

test("tab switching does not blank content", async () => {
	await renderApp("/system/settings");

	const system = await systemList();
	for (let i = 0; i < 3; i += 1) {
		await openGearMenuItem("Settings");
		await expect
			.element(page.getByRole("heading", { name: "Server", exact: true }))
			.toBeVisible();

		// With one agent its pages are the sidebar's top level.
		await agentPageLink("Agent Settings").click();
		await expect
			.element(page.getByRole("button", { name: "Remove Agent", exact: true }))
			.toBeVisible();

		await system.getByRole("link", { name: "Sessions", exact: true }).click();
		await expect
			.element(page.getByRole("button", { name: "Refresh Sessions" }))
			.toBeVisible();

		await openGearMenuItem("Settings");
		await page.getByTestId("settings-tab-providers").click();
		await expect
			.element(page.getByRole("heading", { name: "Providers", exact: true }))
			.toBeVisible();
	}
});

test("switching agents preserves subtab and loads that agent file state", async () => {
	const twoAgentConfig = {
		...CONFIG,
		agents: [
			CONFIG.agents[0],
			{
				name: "coder",
				model: "anthropic/claude-sonnet-4-5",
				memory: "",
				fallbacks: [],
				channels: [],
				tasks: [
					{
						name: "repo-check",
						schedule: "0 */5 * * * *",
						prompt: "Check repository health",
					},
				],
			},
		],
	};
	const agentFiles = new Map<string, Map<string, string>>([
		[
			"assistant",
			new Map<string, string>([
				["AGENTS.md", "# Agents"],
				["RULES.md", "# Rules"],
			]),
		],
		[
			"coder",
			new Map<string, string>([
				["AGENTS.md", "# Coder Agent"],
				["IDENTITY.md", "# Coder Identity"],
			]),
		],
	]);
	mockMCP({
		config_get: twoAgentConfig,
		auth_list: ["anthropic:default", "brave_api_key"],
		session_list: [],
		agent_list: [ASSISTANT, { ...ASSISTANT, id: "a2", name: "coder" }],
		tool_list: TOOLS,
		agent_file_list: (args?: Args) =>
			Array.from(
				agentFiles.get(String(args?.agent ?? ""))?.keys() ?? [],
			).sort(),
		agent_file_read: (args?: Args) =>
			agentFiles
				.get(String(args?.agent ?? ""))
				?.get(String(args?.file ?? "")) ?? "",
	});

	await renderApp("/agents/assistant/tasks?task=daily-briefing");

	await expect
		.element(page.locator('input[placeholder="daily-briefing"]').first())
		.toHaveValue("daily-briefing");

	// Switching agents keeps the subtab, opening the other agent's overview.
	await page
		.getByTestId("sidebar-agent-list")
		.getByRole("link", { name: "coder", exact: true })
		.click();
	await expect.poll(currentURL).toMatch(/\/agents\/coder\/tasks$/);
	await expect.poll(() => texts(page.getByTestId("task-card")).length).toBe(1);
	await expect
		.element(page.getByTestId("task-card"))
		.toMatchTextContent(/repo-check/);

	await agentPageLink("Files").click();
	await expect.poll(currentURL).toMatch(/\/agents\/coder\/files$/);
	await expect
		.poll(() =>
			texts(page.getByTestId("sidebar-files").getByTestId("file-row")),
		)
		.toEqual(["AGENTS.md", "IDENTITY.md"]);
	await expect
		.element(
			page.locator(
				'textarea[placeholder="Pick a file in the sidebar, or add one with its +."]',
			),
		)
		.toHaveValue("# Coder Agent");
});

test("general tab shows web search settings", async () => {
	await renderApp("/system/settings");

	await expect
		.element(page.getByRole("heading", { name: "Web Search", exact: true }))
		.toBeVisible();
	const secret = page.getByTestId("web-search-secret");
	await expect.element(secret).toBeVisible();
	await expect.element(secret).toMatchTextContent("brave_api_key");
	await expect.element(secret).toMatchTextContent("Secret");
});

test("model dropdown hides models from unauthenticated providers", async () => {
	await renderApp("/system/settings");

	await page.getByTestId("default-model-selector").locator("input").click();

	await expect
		.element(page.getByText("anthropic/claude-sonnet-4-6", { exact: true }))
		.toBeVisible();
	await expect
		.poll(() => count(page.getByText("openai/gpt-5.5", { exact: true })))
		.toBe(0);
	await expect
		.poll(() =>
			count(page.getByText("google/gemini-2.5-flash", { exact: true })),
		)
		.toBe(0);
});

test("model dropdown shows Gemini models when gemini auth is present", async () => {
	mockMCP({
		config_get: CONFIG,
		auth_list: ["anthropic:default", "gemini:oauth"],
		session_list: [],
		agent_list: [ASSISTANT],
		tool_list: TOOLS,
	});

	await renderApp("/system/settings");
	await page.locator(".cursor-text").first().click();

	await expect
		.element(page.getByText("google-gemini/gemini-2.5-flash", { exact: true }))
		.toBeVisible();
});

test("providers auth tab shows credential controls", async () => {
	await renderApp("/system/settings");
	await page
		.getByRole("tab", { name: "Providers & Authentication", exact: true })
		.click();

	await expect
		.element(page.getByRole("heading", { name: "Providers", exact: true }))
		.toBeVisible();
	await expect
		.element(page.getByRole("heading", { name: "Extra Secrets", exact: true }))
		.toBeVisible();
});

test("Slack browse channels shows visible workspace channels", async () => {
	mockMCP({
		config_get: {
			...CONFIG,
			agents: [
				{
					...CONFIG.agents[0],
					channels: [
						{
							type: "slack",
							id: "workspace-bot",
							url: "xapp-test-token",
							token: "xoxb-test-token",
							allow_from: [{ from: "*", respond_to_mentions: true }],
						},
					],
				},
			],
		},
		auth_list: ["anthropic:default", "brave_api_key"],
		session_list: [],
		agent_list: [ASSISTANT],
		tool_list: TOOLS,
		slack_channels_list: {
			team_name: "Aviary",
			channels: [
				{ id: "C123", name: "alerts", is_member: true, num_members: 8 },
				{
					id: "C456",
					name: "ops-private",
					is_private: true,
					num_members: 3,
				},
			],
		},
	});

	await openChannelSettings("workspace-bot");
	await page.getByRole("button", { name: "Browse Channels" }).click();

	await expect
		.element(
			page.getByText("Connected to Aviary with 2 visible channels.", {
				exact: true,
			}),
		)
		.toBeVisible();
	await expect
		.element(page.getByText("#alerts", { exact: true }))
		.toBeVisible();
	await expect.element(page.getByText("C123", { exact: true })).toBeVisible();
	await expect
		.element(page.getByText("#ops-private", { exact: true }))
		.toBeVisible();
	await expect
		.element(page.getByText("Private", { exact: true }))
		.toBeVisible();
	await expect.element(page.getByText("Joined", { exact: true })).toBeVisible();
});

test("each channel has its own settings page", async () => {
	mockMCP({
		config_get: {
			...CONFIG,
			agents: [
				{
					...CONFIG.agents[0],
					channels: [
						{
							type: "signal",
							id: "+15551234567",
							url: "127.0.0.1:7583",
							allow_from: [{ from: "*" }],
						},
						{
							type: "discord",
							id: "server-bot",
							token: "auth:discord_bot_token",
							allow_from: [{ from: "user-1" }],
						},
						{
							type: "slack",
							id: "workspace-bot",
							url: "auth:slack_app_token",
							token: "auth:slack_bot_token",
							allow_from: [{ from: "*" }],
						},
					],
				},
			],
		},
		auth_list: [
			"anthropic:default",
			"brave_api_key",
			"discord_bot_token",
			"slack_app_token",
			"slack_bot_token",
		],
		session_list: [],
		agent_list: [ASSISTANT],
		tool_list: TOOLS.slice(0, 2),
	});

	await openChannelSettings("+15551234567");

	// One channel, with no list of the others and no delete button: a
	// channel is removed from its sidebar entry.
	await expect.element(page.getByTestId("channel-settings")).toBeVisible();
	await expect.poll(() => count(page.getByLabelText("Delete channel"))).toBe(0);
	await expect.poll(() => count(page.getByLabelText("Remove channel"))).toBe(0);
	await expect
		.element(page.getByTestId("channel-owner"))
		.toHaveText("assistant");
	await expect
		.element(page.locator('input[placeholder="+15551234567"]').first())
		.toBeVisible();
	await expectHidden(page.getByTestId("channel-secret-assistant-1-token"));

	await openChannelSettings("server-bot");

	const discordToken = page.getByTestId("channel-secret-assistant-1-token");
	await expect.element(discordToken).toBeVisible();
	await expectHidden(page.locator('input[placeholder="+15551234567"]').first());
	await expect
		.element(page.locator('input[placeholder="server-bot"]').first())
		.toHaveValue("server-bot");
	await expect.element(discordToken).toMatchTextContent("discord_bot_token");
	await expect.element(discordToken).toMatchTextContent("Secret");
	await expect
		.poll(() =>
			count(page.getByText("auth:discord_bot_token", { exact: true })),
		)
		.toBe(0);

	await openChannelSettings("workspace-bot");
	const slackURL = page.getByTestId("channel-secret-assistant-2-url");
	const slackToken = page.getByTestId("channel-secret-assistant-2-token");
	await expect.element(slackURL).toMatchTextContent("slack_app_token");
	await expect.element(slackToken).toMatchTextContent("slack_bot_token");
	await expect.element(slackURL).toMatchTextContent("Secret");
	await expect.element(slackToken).toMatchTextContent("Secret");
	await expect
		.poll(() => count(page.getByText("auth:slack_app_token", { exact: true })))
		.toBe(0);
	await expect
		.poll(() => count(page.getByText("auth:slack_bot_token", { exact: true })))
		.toBe(0);
});

test("raw configured channel tokens are masked in dropdowns", async () => {
	mockMCP({
		config_get: {
			...CONFIG,
			agents: [
				{
					...CONFIG.agents[0],
					channels: [
						{
							type: "discord",
							id: "server-bot",
							token: "discord-plaintext-token",
							allow_from: [{ from: "*" }],
						},
						{
							type: "slack",
							id: "workspace-bot",
							url: "xapp-plaintext-token",
							token: "xoxb-plaintext-token",
							allow_from: [{ from: "*" }],
						},
					],
				},
			],
		},
		auth_list: ["anthropic:default", "brave_api_key"],
		session_list: [],
		agent_list: [ASSISTANT],
		tool_list: TOOLS.slice(0, 2),
	});

	await openChannelSettings("server-bot");
	const discordToken = page.getByTestId("channel-secret-assistant-0-token");
	await expect.element(discordToken).toMatchTextContent("discord****");
	await expect.element(discordToken).toMatchTextContent("Inline");
	await expect
		.poll(() =>
			count(page.getByText("discord-plaintext-token", { exact: true })),
		)
		.toBe(0);

	await openChannelSettings("workspace-bot");
	const slackURL = page.getByTestId("channel-secret-assistant-1-url");
	const slackToken = page.getByTestId("channel-secret-assistant-1-token");
	await expect.element(slackURL).toMatchTextContent("xapp-pl****");
	await expect.element(slackToken).toMatchTextContent("xoxb-pl****");
	await expect.element(slackURL).toMatchTextContent("Inline");
	await expect.element(slackToken).toMatchTextContent("Inline");
	await expect
		.poll(() => count(page.getByText("xapp-plaintext-token", { exact: true })))
		.toBe(0);
	await expect
		.poll(() => count(page.getByText("xoxb-plaintext-token", { exact: true })))
		.toBe(0);
});

test("missing auth secrets are highlighted in secret dropdowns", async () => {
	mockMCP({
		config_get: {
			...CONFIG,
			search: { web: { brave_api_key: "auth:missing_brave_key" } },
			agents: [
				{
					...CONFIG.agents[0],
					channels: [
						{
							type: "discord",
							id: "server-bot",
							token: "auth:missing_discord_token",
							allow_from: [{ from: "*" }],
						},
					],
				},
			],
		},
		auth_list: ["anthropic:default", "brave_api_key"],
		session_list: [],
		agent_list: [ASSISTANT],
		tool_list: TOOLS.slice(0, 2),
	});

	await renderApp("/system/settings");
	const webSearch = page.getByTestId("web-search-secret");
	await expect.element(webSearch).toMatchTextContent("missing_brave_key");
	await expect.element(webSearch).toMatchTextContent("Missing Secret");
	await expect.element(webSearch).toHaveClass(/border-red-300/);

	await openChannelSettings("server-bot");
	const token = page.getByTestId("channel-secret-assistant-0-token");
	await expect.element(token).toMatchTextContent("missing_discord_token");
	await expect.element(token).toMatchTextContent("Missing Secret");
	await expect.element(token).toHaveClass(/border-red-300/);
});

test("permissions preset disables inaccessible tool groups and tools", async () => {
	await openAgentPage("settings/permissions");

	const presetTrigger = page.locator("#tool-preset-assistant");
	await expect.element(presetTrigger).toMatchTextContent("Minimal");
	await presetTrigger.click();
	await expect
		.element(page.getByRole("option", { name: /Minimal/ }))
		.toBeVisible();
	const authGroup = page.getByTestId(
		"agent-tool-group-checkbox-assistant-auth",
	);
	await expect.element(authGroup).toBeDisabled();
	await expect.element(authGroup).not.toBeChecked();
	const authSet = page.getByTestId("agent-tool-checkbox-assistant-auth_set");
	await expect.element(authSet).toBeDisabled();
	await expect.element(authSet).not.toBeChecked();
	const browserOpen = page.getByTestId(
		"agent-tool-checkbox-assistant-browser_open",
	);
	await expect.element(browserOpen).toBeDisabled();
	await expect.element(browserOpen).not.toBeChecked();
	const taskRun = page.getByTestId("agent-tool-checkbox-assistant-task_run");
	await expect.element(taskRun).toBeEnabled();
	await expect.element(taskRun).toBeChecked();
});

test("tool permissions inspector shows resolved final tool set", async () => {
	await openAgentPage("settings/permissions");

	await page.getByTestId("agent-tool-permissions-inspect-assistant").click();
	await expect
		.element(page.getByRole("heading", { name: "Inspect Tool Permissions" }))
		.toBeVisible();
	const output = page.getByTestId("tool-permissions-inspector-output");
	await expect
		.element(output)
		.toMatchTextContent('"finalTools": [ "task_run" ]');
	await page.getByRole("button", { name: "Close" }).click();

	// A channel's allow-from entries inspect from the channel's settings.
	await openChannelSettings("+15551234567");
	await page
		.getByTestId("entry-tool-permissions-inspect-assistant-0-0")
		.click();
	await expect
		.element(output)
		.toMatchTextContent('"restrictionSource": "override"');
	await expect
		.element(output)
		.toMatchTextContent('"effectiveDisabledTools": [ "task_run" ]');
	await expect.element(output).toMatchTextContent('"finalTools": []');
});

test("saving settings preserves default-on signal channel checkboxes", async () => {
	await openChannelSettings("+15551234567");

	const phoneInput = page.locator('input[placeholder="+15551234567"]').first();
	const boxes = [
		"Show typing indicator",
		"Reply to replies",
		"React to emojis",
		"Send read receipts",
	];

	for (const label of boxes)
		await expect.element(page.getByLabelText(label)).toBeChecked();

	await phoneInput.fill("+12132957731");
	// The channel is named by its ID, so the address follows the edit.
	await expect
		.poll(currentURL)
		.toMatch(/\/channels\/%2B12132957731\/settings$/);
	await page.getByRole("button", { name: "Save Changes" }).click();

	await expect.element(page.getByTitle("Settings saved")).toBeVisible();
	for (const label of boxes)
		await expect.element(page.getByLabelText(label)).toBeChecked();
});

test("settings deep links scroll to their section", async () => {
	await page.viewport(1280, 720);
	// The section headings in the page, not the header's title crumb.
	const heading = (name: string) =>
		page
			.getByTestId("settings-scroll")
			.getByRole("heading", { name, exact: true });

	await openAgentPage("settings/permissions");
	await expect.element(heading("Permissions")).toBeInViewport();
	await expect.element(heading("General")).not.toBeInViewport();

	// The old tab query params land on the same sections.
	// page.goto loaded the page afresh, so this mounts the app anew too.
	await renderApp("/agents/assistant/settings?tab=permissions");
	await expect
		.poll(currentURL)
		.toMatch(/\/agents\/assistant\/settings\/permissions$/);
	await expect.element(heading("Permissions")).toBeInViewport();

	// Channels moved to their own sidebar section; the old address goes there.
	await renderApp("/agents/assistant/settings/channels");
	await expect.poll(currentURL).toMatch(/\/agents\/assistant\/channels$/);
	await expect.element(page.getByTestId("channels-overview")).toBeVisible();
	await expect.poll(() => count(page.getByTestId("channel-settings"))).toBe(0);

	await agentPageLink("Agent Settings").click();
	await expect.poll(currentURL).toMatch(/\/agents\/assistant\/settings$/);
	await expect.element(heading("General")).toBeInViewport();
});

test("Save Changes saves every settings section at once", async () => {
	let savedConfig: unknown = null;
	mockMCP({
		config_get: CONFIG,
		config_save: (args?: Args) => {
			savedConfig = JSON.parse(String(args?.config ?? "{}"));
			return "ok";
		},
		auth_list: ["anthropic:default", "brave_api_key"],
		session_list: [],
		agent_list: [{ id: "a1", name: "assistant", state: "idle" }],
		tool_list: TOOLS.slice(0, 1),
		agent_file_list: [],
	});
	await openAgentPage("settings");

	await page
		.getByPlaceholder("Default: process working directory", { exact: false })
		.fill("/srv/assistant");
	await page.getByRole("button", { name: "Save Changes" }).click();

	await expect.element(page.getByTitle("Settings saved")).toBeVisible();
	expect(savedConfig).toMatchObject({
		agents: [{ working_dir: "/srv/assistant" }],
	});

	// A channel's settings save the same way, from its own page.
	await openChannelSettings("+15551234567");
	await page
		.locator('input[placeholder="+15551234567"]')
		.first()
		.fill("+15550000000");
	await page.getByRole("button", { name: "Save Changes" }).click();
	await expect.element(page.getByTitle("Settings saved")).toBeVisible();
	await expect
		.poll(() => savedConfig)
		.toMatchObject({
			agents: [{ channels: [{ id: "+15550000000" }] }],
		});
});

test("Remove Agent sits in the danger zone and asks for confirmation", async () => {
	await openAgentPage("settings");
	const dangerZone = page.getByTestId("agent-danger-zone");
	await dangerZone
		.getByRole("button", { name: "Remove Agent", exact: true })
		.click();
	const dialog = page.getByRole("alertdialog");
	await expect.element(dialog).toMatchTextContent("Remove agent?");
	const checkbox = page.getByTestId("remove-agent-data-checkbox");
	await expect.element(checkbox).toBeChecked();
	await checkbox.click();
	await expect.element(checkbox).not.toBeChecked();
	await expect
		.element(page.getByTestId("remove-agent-data-summary"))
		.toMatchTextContent("Only removes the agent from the configuration");
	await dialog.getByRole("button", { name: "Cancel" }).click();
	await expect.poll(() => count(dialog)).toBe(0);
	await expect
		.element(page.getByTestId("settings-title"))
		.toHaveText("Agent Settings");
});
