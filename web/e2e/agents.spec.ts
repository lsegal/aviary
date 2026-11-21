import { expect, test } from "@playwright/test";
import type { AgentFileArgs } from "./helpers/mockMCP";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";
import {
	agentPageLink,
	openAgentPage,
	openChannelSettings,
} from "./helpers/sidebar";

const CONFIG = {
	server: { port: 16677, tls: { cert: "", key: "" } },
	agents: [
		{
			name: "assistant",
			model: "anthropic/claude-sonnet-4-5",
			memory: "",
			fallbacks: [],
			channels: [
				{
					type: "signal",
					id: "+15551234567",
					primary: "+15551234567",
					disabled_tools: ["task_run"],
					allow_from: [
						{
							from: "*",
							respond_to_mentions: true,
							restrict_tools: ["task_run", "browser_open", "usage_query"],
						},
					],
				},
			],
			tasks: [
				{
					enabled: false,
					name: "daily-briefing",
					schedule: "0 * * * * *",
					prompt: "Summarize updates",
					target: "channel:+15551234567",
				},
			],
			permissions: {
				preset: "minimal",
				tools: ["task_run", "auth_set", "browser_open"],
			},
		},
	],
	models: {
		providers: {
			anthropic: { auth: "auth:anthropic:default" },
			openai: { auth: "auth:openai:default" },
		},
		defaults: { model: "anthropic/claude-sonnet-4-5", fallbacks: [] },
	},
	browser: { binary: "", cdp_port: 9222, reuse_tabs: true },
	search: { web: { brave_api_key: "auth:brave_api_key" } },
	scheduler: { concurrency: "auto" },
};

test.beforeEach(async ({ page }) => {
	await setAuthToken(page);
	const agentFiles = new Map<string, string>([
		["AGENTS.md", "# Agents"],
		["RULES.md", "# Rules"],
		["MEMORY.md", "Remembered note"],
		["IDENTITY.md", "# Identity"],
	]);
	await mockMCP(page, {
		config_get: CONFIG,
		auth_list: ["anthropic:default", "brave_api_key"],
		session_list: [],
		agent_list: [
			{
				id: "a1",
				name: "assistant",
				model: "anthropic/claude-sonnet-4-5",
				fallbacks: [],
				state: "idle",
			},
		],
		tool_list: [
			{ name: "task_run", description: "Run a task immediately" },
			{ name: "auth_set", description: "Store a secret" },
			{ name: "browser_open", description: "Open a browser tab" },
			{ name: "usage_query", description: "Read usage metrics" },
		],
		agent_file_list: () => Array.from(agentFiles.keys()).sort(),
		agent_file_read: (args) => agentFiles.get(String(args?.file ?? "")) ?? "",
		agent_file_write: (args) => {
			agentFiles.set(String(args?.file ?? ""), String(args?.content ?? ""));
			return "ok";
		},
		agent_file_delete: (args) => {
			agentFiles.delete(String(args?.file ?? ""));
			return "ok";
		},
	});
});

test("agents and tasks tab shows configured entries", async ({ page }) => {
	await openAgentPage(page, "settings");

	await expect(page.getByTestId("sidebar-add-agent")).toBeVisible();
	await expect(
		page.getByRole("button", { name: "Remove Agent", exact: true }).first(),
	).toBeVisible();
	// General subtab (default): agent name field is always visible in the header
	await expect(
		page.locator('input[placeholder="assistant"]').first(),
	).toHaveValue("assistant");

	// Tasks subtab
	await agentPageLink(page, "Scheduled Tasks").click();
	await expect(
		page.locator('input[placeholder="daily-briefing"]').first(),
	).toHaveValue("daily-briefing");
	await expect(
		page.getByText("disabled", { exact: true }).first(),
	).toBeVisible();
	await expect(
		page.getByRole("switch", { name: "Toggle task enabled" }).first(),
	).toBeVisible();
	await expect(
		page.getByRole("heading", { name: "Scheduled Tasks", exact: true }),
	).toBeVisible();

	// The agent's Settings page stacks General and Permissions; each channel
	// has its own settings page.
	await agentPageLink(page, "Agent Settings").click();
	await expect(page).toHaveURL(/\/agents\/assistant\/settings$/);
	await expect(
		page.getByRole("heading", { name: "Channels", exact: true }),
	).toHaveCount(0);
	await openChannelSettings(page, "+15551234567");
	await expect(
		page.locator('input[placeholder="e.g. +15551234567"]').first(),
	).toHaveValue("+15551234567");
	await expect(
		page.locator('input[placeholder="*, +15551234567"]').first(),
	).toHaveValue("*");
});

test("tab switching does not blank content", async ({ page }) => {
	await page.goto("/system/settings");

	const system = page.getByTestId("sidebar-system-list");
	await page.getByTestId("sidebar-system-more").click();
	for (let i = 0; i < 3; i += 1) {
		await system.getByRole("link", { name: "Settings", exact: true }).click();
		await expect(
			page.getByRole("heading", { name: "Server", exact: true }),
		).toBeVisible();

		// With one agent its pages are the sidebar's top level.
		await agentPageLink(page, "Agent Settings").click();
		await expect(
			page.getByRole("button", { name: "Remove Agent", exact: true }),
		).toBeVisible();

		await system.getByRole("link", { name: "Sessions", exact: true }).click();
		await expect(
			page.getByRole("button", { name: "Refresh Sessions" }),
		).toBeVisible();

		await page
			.getByRole("link", { name: "Providers & Auth", exact: true })
			.click();
		await expect(
			page.getByRole("heading", { name: "Providers", exact: true }),
		).toBeVisible();
	}
});

test("switching agents preserves subtab and loads that agent file state", async ({
	page,
}) => {
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
	await mockMCP(page, {
		config_get: twoAgentConfig,
		auth_list: ["anthropic:default", "brave_api_key"],
		session_list: [],
		agent_list: [
			{
				id: "a1",
				name: "assistant",
				model: "anthropic/claude-sonnet-4-5",
				fallbacks: [],
				state: "idle",
			},
			{
				id: "a2",
				name: "coder",
				model: "anthropic/claude-sonnet-4-5",
				fallbacks: [],
				state: "idle",
			},
		],
		tool_list: [
			{ name: "task_run", description: "Run a task immediately" },
			{ name: "auth_set", description: "Store a secret" },
			{ name: "browser_open", description: "Open a browser tab" },
			{ name: "usage_query", description: "Read usage metrics" },
		],
		agent_file_list: (args?: AgentFileArgs) =>
			Array.from(
				agentFiles.get(String(args?.agent ?? ""))?.keys() ?? [],
			).sort(),
		agent_file_read: (args?: AgentFileArgs) =>
			agentFiles
				.get(String(args?.agent ?? ""))
				?.get(String(args?.file ?? "")) ?? "",
	});

	await page.goto("/agents/assistant/tasks");

	await expect(
		page.locator('input[placeholder="daily-briefing"]').first(),
	).toHaveValue("daily-briefing");

	await page
		.getByTestId("sidebar-agent-list")
		.getByRole("link", { name: "coder", exact: true })
		.click();
	await expect(page).toHaveURL(/\/agents\/coder\/tasks$/);
	await expect(
		page.locator('input[placeholder="daily-briefing"]').first(),
	).toHaveValue("repo-check");

	await agentPageLink(page, "Files").click();
	await expect(page).toHaveURL(/\/agents\/coder\/files$/);
	await expect(
		page.getByTestId("sidebar-files").getByTestId("file-row"),
	).toHaveText(["AGENTS.md", "IDENTITY.md"]);
	await expect(
		page.locator(
			'textarea[placeholder="Pick a file in the sidebar, or add one with its +."]',
		),
	).toHaveValue("# Coder Agent");
});

test("general tab shows web search settings", async ({ page }) => {
	await page.goto("/system/settings");

	await expect(
		page.getByRole("heading", { name: "Web Search", exact: true }),
	).toBeVisible();
	await expect(page.getByTestId("web-search-secret")).toBeVisible();
	await expect(page.getByTestId("web-search-secret")).toContainText(
		"brave_api_key",
	);
	await expect(page.getByTestId("web-search-secret")).toContainText("Secret");
});

test("model dropdown hides models from unauthenticated providers", async ({
	page,
}) => {
	await page.goto("/system/settings");

	await page.getByTestId("default-model-selector").locator("input").click();

	await expect(
		page.getByText("anthropic/claude-sonnet-4-6", { exact: true }),
	).toBeVisible();
	await expect(page.getByText("openai/gpt-5.5", { exact: true })).toHaveCount(
		0,
	);
	await expect(
		page.getByText("google/gemini-2.5-flash", { exact: true }),
	).toHaveCount(0);
});

test("model dropdown shows Gemini models when gemini auth is present", async ({
	page,
}) => {
	await setAuthToken(page);
	await mockMCP(page, {
		config_get: CONFIG,
		auth_list: ["anthropic:default", "gemini:oauth"],
		session_list: [],
		agent_list: [
			{
				id: "a1",
				name: "assistant",
				model: "anthropic/claude-sonnet-4-5",
				fallbacks: [],
				state: "idle",
			},
		],
		tool_list: [
			{ name: "task_run", description: "Run a task immediately" },
			{ name: "auth_set", description: "Store a secret" },
			{ name: "browser_open", description: "Open a browser tab" },
			{ name: "usage_query", description: "Read usage metrics" },
		],
	});

	await page.goto("/system/settings");
	await page.locator(".cursor-text").first().click();

	await expect(
		page.getByText("google-gemini/gemini-2.5-flash", { exact: true }),
	).toBeVisible();
});

test("providers auth tab shows credential controls", async ({ page }) => {
	await page.goto("/system/settings");
	await page
		.getByRole("link", { name: "Providers & Auth", exact: true })
		.click();

	await expect(
		page.getByRole("heading", { name: "Providers", exact: true }),
	).toBeVisible();
	await expect(
		page.getByRole("heading", { name: "Extra Secrets", exact: true }),
	).toBeVisible();
});

test("Slack browse channels shows visible workspace channels", async ({
	page,
}) => {
	await setAuthToken(page);
	await mockMCP(page, {
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
							allow_from: [
								{
									from: "*",
									respond_to_mentions: true,
								},
							],
						},
					],
				},
			],
		},
		auth_list: ["anthropic:default", "brave_api_key"],
		session_list: [],
		agent_list: [
			{
				id: "a1",
				name: "assistant",
				model: "anthropic/claude-sonnet-4-5",
				fallbacks: [],
				state: "idle",
			},
		],
		tool_list: [
			{ name: "task_run", description: "Run a task immediately" },
			{ name: "auth_set", description: "Store a secret" },
			{ name: "browser_open", description: "Open a browser tab" },
			{ name: "usage_query", description: "Read usage metrics" },
		],
		slack_channels_list: {
			team_name: "Aviary",
			channels: [
				{
					id: "C123",
					name: "alerts",
					is_member: true,
					num_members: 8,
				},
				{
					id: "C456",
					name: "ops-private",
					is_private: true,
					num_members: 3,
				},
			],
		},
	});

	await openChannelSettings(page, "workspace-bot");
	await page.getByRole("button", { name: "Browse Channels" }).click();

	await expect(
		page.getByText("Connected to Aviary with 2 visible channels.", {
			exact: true,
		}),
	).toBeVisible();
	await expect(page.getByText("#alerts", { exact: true })).toBeVisible();
	await expect(page.getByText("C123", { exact: true })).toBeVisible();
	await expect(page.getByText("#ops-private", { exact: true })).toBeVisible();
	await expect(page.getByText("Private", { exact: true })).toBeVisible();
	await expect(page.getByText("Joined", { exact: true })).toBeVisible();
});

test("each channel has its own settings page", async ({ page }) => {
	await setAuthToken(page);
	await mockMCP(page, {
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
		agent_list: [
			{
				id: "a1",
				name: "assistant",
				model: "anthropic/claude-sonnet-4-5",
				fallbacks: [],
				state: "idle",
			},
		],
		tool_list: [
			{ name: "task_run", description: "Run a task immediately" },
			{ name: "auth_set", description: "Store a secret" },
		],
	});

	await openChannelSettings(page, "+15551234567");

	// One channel, with no list of the others and no delete button: a
	// channel is removed from its sidebar entry.
	await expect(page.getByTestId("channel-settings")).toBeVisible();
	await expect(page.getByLabel("Delete channel")).toHaveCount(0);
	await expect(page.getByLabel("Remove channel")).toHaveCount(0);
	await expect(page.getByTestId("channel-owner")).toHaveText("assistant");
	await expect(
		page.locator('input[placeholder="+15551234567"]').first(),
	).toBeVisible();
	await expect(
		page.getByTestId("channel-secret-assistant-1-token"),
	).toBeHidden();

	await openChannelSettings(page, "server-bot");

	await expect(
		page.getByTestId("channel-secret-assistant-1-token"),
	).toBeVisible();
	await expect(
		page.locator('input[placeholder="+15551234567"]').first(),
	).toBeHidden();
	await expect(
		page.locator('input[placeholder="server-bot"]').first(),
	).toHaveValue("server-bot");
	await expect(
		page.getByTestId("channel-secret-assistant-1-token"),
	).toContainText("discord_bot_token");
	await expect(
		page.getByTestId("channel-secret-assistant-1-token"),
	).toContainText("Secret");
	await expect(
		page.getByText("auth:discord_bot_token", { exact: true }),
	).toHaveCount(0);

	await openChannelSettings(page, "workspace-bot");
	await expect(
		page.getByTestId("channel-secret-assistant-2-url"),
	).toContainText("slack_app_token");
	await expect(
		page.getByTestId("channel-secret-assistant-2-token"),
	).toContainText("slack_bot_token");
	await expect(
		page.getByTestId("channel-secret-assistant-2-url"),
	).toContainText("Secret");
	await expect(
		page.getByTestId("channel-secret-assistant-2-token"),
	).toContainText("Secret");
	await expect(
		page.getByText("auth:slack_app_token", { exact: true }),
	).toHaveCount(0);
	await expect(
		page.getByText("auth:slack_bot_token", { exact: true }),
	).toHaveCount(0);
});

test("raw configured channel tokens are masked in dropdowns", async ({
	page,
}) => {
	await setAuthToken(page);
	await mockMCP(page, {
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
		agent_list: [
			{
				id: "a1",
				name: "assistant",
				model: "anthropic/claude-sonnet-4-5",
				fallbacks: [],
				state: "idle",
			},
		],
		tool_list: [
			{ name: "task_run", description: "Run a task immediately" },
			{ name: "auth_set", description: "Store a secret" },
		],
	});

	await openChannelSettings(page, "server-bot");
	await expect(
		page.getByTestId("channel-secret-assistant-0-token"),
	).toContainText("discord****");
	await expect(
		page.getByTestId("channel-secret-assistant-0-token"),
	).toContainText("Inline");
	await expect(
		page.getByText("discord-plaintext-token", { exact: true }),
	).toHaveCount(0);

	await openChannelSettings(page, "workspace-bot");
	await expect(
		page.getByTestId("channel-secret-assistant-1-url"),
	).toContainText("xapp-pl****");
	await expect(
		page.getByTestId("channel-secret-assistant-1-token"),
	).toContainText("xoxb-pl****");
	await expect(
		page.getByTestId("channel-secret-assistant-1-url"),
	).toContainText("Inline");
	await expect(
		page.getByTestId("channel-secret-assistant-1-token"),
	).toContainText("Inline");
	await expect(
		page.getByText("xapp-plaintext-token", { exact: true }),
	).toHaveCount(0);
	await expect(
		page.getByText("xoxb-plaintext-token", { exact: true }),
	).toHaveCount(0);
});

test("missing auth secrets are highlighted in secret dropdowns", async ({
	page,
}) => {
	await setAuthToken(page);
	await mockMCP(page, {
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
		agent_list: [
			{
				id: "a1",
				name: "assistant",
				model: "anthropic/claude-sonnet-4-5",
				fallbacks: [],
				state: "idle",
			},
		],
		tool_list: [
			{ name: "task_run", description: "Run a task immediately" },
			{ name: "auth_set", description: "Store a secret" },
		],
	});

	await page.goto("/system/settings");
	await expect(page.getByTestId("web-search-secret")).toContainText(
		"missing_brave_key",
	);
	await expect(page.getByTestId("web-search-secret")).toContainText(
		"Missing Secret",
	);
	await expect(page.getByTestId("web-search-secret")).toHaveClass(
		/border-red-300/,
	);

	await openChannelSettings(page, "server-bot");
	await expect(
		page.getByTestId("channel-secret-assistant-0-token"),
	).toContainText("missing_discord_token");
	await expect(
		page.getByTestId("channel-secret-assistant-0-token"),
	).toContainText("Missing Secret");
	await expect(
		page.getByTestId("channel-secret-assistant-0-token"),
	).toHaveClass(/border-red-300/);
});

test("permissions preset disables inaccessible tool groups and tools", async ({
	page,
}) => {
	await openAgentPage(page, "settings/permissions");

	const presetTrigger = page.locator("#tool-preset-assistant");
	await expect(presetTrigger).toContainText("Minimal");
	await presetTrigger.click();
	await expect(page.getByRole("option", { name: /Minimal/ })).toBeVisible();
	await expect(
		page.getByTestId("agent-tool-group-checkbox-assistant-auth"),
	).toBeDisabled();
	await expect(
		page.getByTestId("agent-tool-group-checkbox-assistant-auth"),
	).not.toBeChecked();
	await expect(
		page.getByTestId("agent-tool-checkbox-assistant-auth_set"),
	).toBeDisabled();
	await expect(
		page.getByTestId("agent-tool-checkbox-assistant-auth_set"),
	).not.toBeChecked();
	await expect(
		page.getByTestId("agent-tool-checkbox-assistant-browser_open"),
	).toBeDisabled();
	await expect(
		page.getByTestId("agent-tool-checkbox-assistant-browser_open"),
	).not.toBeChecked();
	await expect(
		page.getByTestId("agent-tool-checkbox-assistant-task_run"),
	).toBeEnabled();
	await expect(
		page.getByTestId("agent-tool-checkbox-assistant-task_run"),
	).toBeChecked();
});

test("tool permissions inspector shows resolved final tool set", async ({
	page,
}) => {
	await openAgentPage(page, "settings/permissions");

	await page.getByTestId("agent-tool-permissions-inspect-assistant").click();
	await expect(
		page.getByRole("heading", { name: "Inspect Tool Permissions" }),
	).toBeVisible();
	await expect(
		page.getByTestId("tool-permissions-inspector-output"),
	).toContainText('"finalTools": [\n    "task_run"\n  ]');
	await page.getByRole("button", { name: "Close" }).click();

	// A channel's allow-from entries inspect from the channel's settings.
	await openChannelSettings(page, "+15551234567");
	await page
		.getByTestId("entry-tool-permissions-inspect-assistant-0-0")
		.click();
	await expect(
		page.getByTestId("tool-permissions-inspector-output"),
	).toContainText('"restrictionSource": "override"');
	await expect(
		page.getByTestId("tool-permissions-inspector-output"),
	).toContainText('"effectiveDisabledTools": [\n    "task_run"\n  ]');
	await expect(
		page.getByTestId("tool-permissions-inspector-output"),
	).toContainText('"finalTools": []');
});

test("saving settings preserves default-on signal channel checkboxes", async ({
	page,
}) => {
	await openChannelSettings(page, "+15551234567");

	const phoneInput = page.locator('input[placeholder="+15551234567"]').first();

	await expect(page.getByLabel("Show typing indicator")).toBeChecked();
	await expect(page.getByLabel("Reply to replies")).toBeChecked();
	await expect(page.getByLabel("React to emojis")).toBeChecked();
	await expect(page.getByLabel("Send read receipts")).toBeChecked();

	await phoneInput.fill("+12132957731");
	// The channel is named by its ID, so the address follows the edit.
	await expect(page).toHaveURL(/\/channels\/%2B12132957731\/settings$/);
	await page.getByRole("button", { name: "Save Changes" }).click();

	await expect(page.getByTitle("Settings saved")).toBeVisible();
	await expect(page.getByLabel("Show typing indicator")).toBeChecked();
	await expect(page.getByLabel("Reply to replies")).toBeChecked();
	await expect(page.getByLabel("React to emojis")).toBeChecked();
	await expect(page.getByLabel("Send read receipts")).toBeChecked();
});

test("saving settings preserves task prompt newlines", async ({ page }) => {
	let savedConfig: unknown = null;

	await setAuthToken(page);
	const agentFiles = new Map<string, string>([
		["AGENTS.md", "# Agents"],
		["RULES.md", "# Rules"],
		["MEMORY.md", "Remembered note"],
		["IDENTITY.md", "# Identity"],
	]);
	await mockMCP(page, {
		config_get: CONFIG,
		config_save: (args) => {
			savedConfig = JSON.parse(String(args?.config ?? "{}"));
			return "ok";
		},
		auth_list: ["anthropic:default", "brave_api_key"],
		session_list: [],
		agent_list: [
			{
				id: "a1",
				name: "assistant",
				model: "anthropic/claude-sonnet-4-5",
				fallbacks: [],
				state: "idle",
			},
		],
		tool_list: [
			{ name: "task_run", description: "Run a task immediately" },
			{ name: "auth_set", description: "Store a secret" },
			{ name: "browser_open", description: "Open a browser tab" },
			{ name: "usage_query", description: "Read usage metrics" },
		],
		agent_file_list: () => Array.from(agentFiles.keys()).sort(),
		agent_file_read: (args) => agentFiles.get(String(args?.file ?? "")) ?? "",
		agent_file_write: (args) => {
			agentFiles.set(String(args?.file ?? ""), String(args?.content ?? ""));
			return "ok";
		},
		agent_file_delete: (args) => {
			agentFiles.delete(String(args?.file ?? ""));
			return "ok";
		},
	});

	await openAgentPage(page, "settings");
	await agentPageLink(page, "Scheduled Tasks").click();

	const prompt = page.getByPlaceholder("Task prompt...").first();
	await prompt.fill("line 1\n\nline 3\n");
	await page.getByRole("button", { name: "Save Changes" }).click();

	await expect(page.getByTitle("Settings saved")).toBeVisible();
	expect(savedConfig).toMatchObject({
		agents: [
			{
				tasks: [
					{
						prompt: "line 1\n\nline 3\n",
					},
				],
			},
		],
	});
});

test("tasks can be enabled from the settings UI", async ({ page }) => {
	await openAgentPage(page, "settings");
	await agentPageLink(page, "Scheduled Tasks").click();

	await expect(page.getByText("disabled", { exact: true })).toBeVisible();
	const toggle = page
		.getByRole("switch", { name: "Toggle task enabled" })
		.first();
	await toggle.click();
	await expect(toggle).toHaveAttribute("aria-checked", "true");
});

test("task header wraps cleanly on mobile", async ({ page }) => {
	await page.setViewportSize({ width: 390, height: 844 });
	await openAgentPage(page, "tasks", "assistant");

	const definedIn = page.getByText("Defined in: aviary.yaml", {
		exact: true,
	});
	const convertButton = page.getByRole("button", { name: "Convert to Script" });
	const enabledSwitch = page.getByRole("switch", {
		name: "Toggle task enabled",
	});

	await expect(definedIn).toBeVisible();
	await expect(convertButton).toBeVisible();
	await expect(enabledSwitch).toBeVisible();

	const definedInBox = await definedIn.boundingBox();
	const convertButtonBox = await convertButton.boundingBox();
	const enabledSwitchBox = await enabledSwitch.boundingBox();

	expect(definedInBox).not.toBeNull();
	expect(convertButtonBox).not.toBeNull();
	expect(enabledSwitchBox).not.toBeNull();

	if (definedInBox && convertButtonBox && enabledSwitchBox) {
		expect(definedInBox.x).toBeGreaterThanOrEqual(0);
		expect(definedInBox.x + definedInBox.width).toBeLessThanOrEqual(390);
		expect(convertButtonBox.x).toBeGreaterThanOrEqual(0);
		expect(convertButtonBox.x + convertButtonBox.width).toBeLessThanOrEqual(
			390,
		);
		expect(enabledSwitchBox.x).toBeGreaterThanOrEqual(0);
		expect(enabledSwitchBox.x + enabledSwitchBox.width).toBeLessThanOrEqual(
			390,
		);
	}

	const pageWidths = await page.evaluate(() => ({
		clientWidth: document.documentElement.clientWidth,
		scrollWidth: document.documentElement.scrollWidth,
	}));
	expect(pageWidths.scrollWidth).toBeLessThanOrEqual(pageWidths.clientWidth);
});

test("deleting a task from settings persists immediately", async ({ page }) => {
	let savedConfig: unknown = null;

	await setAuthToken(page);
	const agentFiles = new Map<string, string>([
		["AGENTS.md", "# Agents"],
		["RULES.md", "# Rules"],
		["MEMORY.md", "Remembered note"],
		["IDENTITY.md", "# Identity"],
	]);
	await mockMCP(page, {
		config_get: {
			...CONFIG,
			agents: [
				{
					...CONFIG.agents[0],
					tasks: [
						{
							enabled: true,
							name: "file-task",
							prompt: "From file",
							from_file: true,
							file: "tasks/file-task.md",
						},
					],
				},
			],
		},
		config_save: (args) => {
			savedConfig = JSON.parse(String(args?.config ?? "{}"));
			return "ok";
		},
		auth_list: ["anthropic:default", "brave_api_key"],
		session_list: [],
		agent_list: [
			{
				id: "a1",
				name: "assistant",
				model: "anthropic/claude-sonnet-4-5",
				fallbacks: [],
				state: "idle",
			},
		],
		tool_list: [
			{ name: "task_run", description: "Run a task immediately" },
			{ name: "auth_set", description: "Store a secret" },
			{ name: "browser_open", description: "Open a browser tab" },
			{ name: "usage_query", description: "Read usage metrics" },
		],
		agent_file_list: () => Array.from(agentFiles.keys()).sort(),
		agent_file_read: (args) => agentFiles.get(String(args?.file ?? "")) ?? "",
		agent_file_write: (args) => {
			agentFiles.set(String(args?.file ?? ""), String(args?.content ?? ""));
			return "ok";
		},
		agent_file_delete: (args) => {
			agentFiles.delete(String(args?.file ?? ""));
			return "ok";
		},
	});

	await openAgentPage(page, "settings");
	await agentPageLink(page, "Scheduled Tasks").click();

	const row = page.getByTestId("task-row").filter({ hasText: "file-task" });
	await expect(row).toBeVisible();
	await row.hover();
	await row.getByTestId("task-menu-button").click();
	await page.getByRole("menuitem", { name: "Delete" }).click();
	await page.getByRole("button", { name: "Delete", exact: true }).click();

	await expect(row).toHaveCount(0);
	await expect(
		page.getByText("No scheduled tasks configured for this agent."),
	).toBeVisible();
	expect(savedConfig).toMatchObject({
		agents: [
			{
				tasks: [],
			},
		],
	});
});

test("agent files editor opens the file picked in the sidebar", async ({
	page,
}) => {
	await openAgentPage(page, "files");

	const files = page.getByTestId("sidebar-files").getByTestId("file-row");
	await expect(files).toHaveText([
		"AGENTS.md",
		"IDENTITY.md",
		"MEMORY.md",
		"RULES.md",
	]);
	const editor = page.getByPlaceholder(
		"Pick a file in the sidebar, or add one with its +.",
	);
	// Without a ?file the page opens the first file.
	await expect(editor).toHaveValue("# Agents");
	await expect(page.getByTestId("agent-file-name")).toHaveText("AGENTS.md");
	await expect(page.getByTestId("agent-file-builtin")).toBeVisible();

	await files.filter({ hasText: "IDENTITY.md" }).getByRole("link").click();
	await expect(page).toHaveURL(
		/\/agents\/assistant\/files\?file=IDENTITY\.md$/,
	);
	await expect(editor).toHaveValue("# Identity");
	await expect(page.getByTestId("agent-file-name")).toHaveText("IDENTITY.md");
	await expect(page.getByTestId("agent-file-builtin")).toHaveCount(0);
});

test("agent files editor auto-syncs templates when an older agent has no root files", async ({
	page,
}) => {
	const syncedFiles = new Map<string, string>([
		["AGENTS.md", "# Agents"],
		["RULES.md", "# Rules"],
		["MEMORY.md", "Remembered note"],
	]);
	let synced = false;

	await setAuthToken(page);
	await mockMCP(page, {
		config_get: CONFIG,
		auth_list: ["anthropic:default", "brave_api_key"],
		session_list: [],
		agent_list: [
			{
				id: "a1",
				name: "assistant",
				model: "anthropic/claude-sonnet-4-5",
				fallbacks: [],
				state: "idle",
			},
		],
		tool_list: [
			{ name: "task_run", description: "Run a task immediately" },
			{ name: "auth_set", description: "Store a secret" },
			{ name: "browser_open", description: "Open a browser tab" },
			{ name: "usage_query", description: "Read usage metrics" },
		],
		agent_file_list: () =>
			synced ? Array.from(syncedFiles.keys()).sort() : [],
		agent_file_read: (args) => syncedFiles.get(String(args?.file ?? "")) ?? "",
		agent_template_sync: () => {
			synced = true;
			return "ok";
		},
	});

	await openAgentPage(page, "files");
	await page.getByTestId("agent-file-menu").click();
	await page.getByRole("menuitem", { name: "Refresh" }).click();

	await expect(
		page.getByTestId("sidebar-files").getByTestId("file-row"),
	).toHaveText(["AGENTS.md", "MEMORY.md", "RULES.md"]);
	await expect(page.getByText("No files yet")).toHaveCount(0);
});

test("a task can target one of its agent's projects and keeps it when saved", async ({
	page,
}) => {
	let savedConfig: unknown = null;
	const config = {
		...CONFIG,
		agents: [
			{
				...CONFIG.agents[0],
				fallbacks: ["cli/claude/opus"],
				tasks: [
					...CONFIG.agents[0].tasks,
					{
						name: "nightly-triage",
						type: "project",
						project: "o/gone",
						model: "cli/codex",
						schedule: "0 3 * * *",
						prompt: "Triage new issues",
					},
				],
				// A project task runs against a project its own agent owns.
				projects: [{ target: "o/r" }, { target: "o/other" }],
			},
			{
				name: "elsewhere",
				model: "anthropic/claude-sonnet-4-5",
				projects: [{ target: "o/not-mine" }],
			},
		],
	};
	await mockMCP(page, {
		config_get: config,
		config_save: (args) => {
			savedConfig = JSON.parse(String(args?.config ?? "{}"));
			return "ok";
		},
		auth_list: ["anthropic:default", "brave_api_key"],
		session_list: [],
		agent_list: [
			{
				id: "a1",
				name: "assistant",
				model: "anthropic/claude-sonnet-4-5",
				fallbacks: [],
				state: "idle",
			},
		],
		agent_file_list: [],
	});

	await page.goto("/agents/assistant/tasks");
	await page
		.getByTestId("sidebar-tasks")
		.getByRole("link", { name: "nightly-triage" })
		.click();

	// A project that is no longer watched stays selected instead of clearing.
	const project = page.getByRole("combobox", { name: "Project" });
	await expect(project).toHaveValue("o/gone");
	await expect(project.locator("option")).toHaveText([
		"Select a watched project…",
		"o/r",
		"o/other",
		"o/gone",
	]);
	const model = page.getByTestId("task-model");
	await expect(model).toContainText("cli/codex");
	await expect(page.getByPlaceholder("./docs/**/*.md")).toHaveCount(0);

	await project.selectOption("o/r");
	await model.getByRole("button").click();
	await model.getByRole("textbox").fill("cli/claude/opus");
	await model.getByRole("textbox").press("Enter");
	await expect(model).toContainText("cli/claude/opus");

	// Turning the first task into a project task offers the watched projects.
	await page
		.getByTestId("sidebar-tasks")
		.getByRole("link", { name: "daily-briefing" })
		.click();
	await page
		.locator("select")
		.filter({ has: page.locator('option[value="project"]') })
		.selectOption("project");
	await page.getByRole("combobox", { name: "Project" }).selectOption("o/other");

	await page.getByRole("button", { name: "Save Changes" }).click();
	await expect(page.getByTitle("Settings saved")).toBeVisible();
	expect(savedConfig).toMatchObject({
		agents: [
			{
				tasks: [
					{
						name: "daily-briefing",
						type: "project",
						project: "o/other",
					},
					{
						name: "nightly-triage",
						type: "project",
						project: "o/r",
						model: "cli/claude/opus",
					},
				],
				projects: [{ target: "o/r" }, { target: "o/other" }],
			},
			{ name: "elsewhere", projects: [{ target: "o/not-mine" }] },
		],
	});
});

test("a task's session target is picked by name and saved by ID", async ({
	page,
}) => {
	let savedConfig: unknown = null;
	const config = {
		...CONFIG,
		agents: [
			{
				...CONFIG.agents[0],
				tasks: [
					{
						name: "daily-briefing",
						schedule: "0 9 * * *",
						prompt: "Summarize updates",
						target: "session:sess_gone",
					},
				],
			},
		],
	};
	await mockMCP(page, {
		config_get: config,
		config_save: (args) => {
			savedConfig = JSON.parse(String(args?.config ?? "{}"));
			return "ok";
		},
		auth_list: ["anthropic:default", "brave_api_key"],
		session_list: [
			{
				id: "sess_planning",
				agent_id: "assistant",
				name: "sess_planning",
				title: "Trip planning",
				created_at: "",
			},
			{ id: "sess_main", agent_id: "assistant", name: "main", created_at: "" },
		],
		agent_list: [
			{
				id: "a1",
				name: "assistant",
				model: "anthropic/claude-sonnet-4-5",
				fallbacks: [],
				state: "idle",
			},
		],
		agent_file_list: [],
	});

	await page.goto("/agents/assistant/tasks");
	await page
		.getByTestId("sidebar-tasks")
		.getByRole("link", { name: "daily-briefing" })
		.click();

	// A saved session that no longer exists is shown as gone, not blank.
	const session = page.getByRole("combobox", { name: "Session" });
	await expect(session.locator("option")).toHaveText([
		"Choose a session…",
		"Main Chat",
		"Trip planning",
		"Deleted session (no longer exists)",
	]);
	await expect(session).toHaveValue("sess_gone");
	await expect(page.locator('input[placeholder="Target ID"]')).toHaveCount(0);

	await session.selectOption({ label: "Main Chat" });
	await page.getByRole("button", { name: "Save Changes" }).click();
	await expect(page.getByTitle("Settings saved")).toBeVisible();
	expect(savedConfig).toMatchObject({
		agents: [{ tasks: [{ target: "session:sess_main" }] }],
	});
});

test("a task saved with a session name selects that session", async ({
	page,
}) => {
	const config = {
		...CONFIG,
		agents: [
			{
				...CONFIG.agents[0],
				tasks: [
					{
						name: "daily-briefing",
						schedule: "0 9 * * *",
						prompt: "Summarize updates",
						target: "session:main",
					},
				],
			},
		],
	};
	await mockMCP(page, {
		config_get: config,
		auth_list: [],
		session_list: [
			{ id: "sess_main", agent_id: "assistant", name: "main", created_at: "" },
		],
		agent_list: [{ id: "a1", name: "assistant", state: "idle" }],
		agent_file_list: [],
	});

	await page.goto("/agents/assistant/tasks");
	await page
		.getByTestId("sidebar-tasks")
		.getByRole("link", { name: "daily-briefing" })
		.click();
	const session = page.getByRole("combobox", { name: "Session" });
	await expect(session).toHaveValue("sess_main");
	await expect(session.locator("option:checked")).toHaveText("Main Chat");
});

test("settings deep links scroll to their section", async ({ page }) => {
	await page.setViewportSize({ width: 1280, height: 720 });
	const heading = (name: string) =>
		page.getByRole("heading", { name, exact: true });

	await openAgentPage(page, "settings/permissions");
	await expect(heading("Permissions")).toBeInViewport();
	await expect(heading("General")).not.toBeInViewport();

	// The old tab query params land on the same sections.
	await page.goto("/agents/assistant/settings?tab=permissions");
	await expect(page).toHaveURL(/\/agents\/assistant\/settings\/permissions$/);
	await expect(heading("Permissions")).toBeInViewport();

	// Channels moved to their own sidebar section; the old address goes there.
	await page.goto("/agents/assistant/settings/channels");
	await expect(page).toHaveURL(/\/agents\/assistant\/channels$/);
	await expect(heading("Channels")).toHaveCount(0);

	await agentPageLink(page, "Agent Settings").click();
	await expect(page).toHaveURL(/\/agents\/assistant\/settings$/);
	await expect(heading("General")).toBeInViewport();
});

test("Save Changes saves every settings section at once", async ({ page }) => {
	let savedConfig: unknown = null;
	await mockMCP(page, {
		config_get: CONFIG,
		config_save: (args) => {
			savedConfig = JSON.parse(String(args?.config ?? "{}"));
			return "ok";
		},
		auth_list: ["anthropic:default", "brave_api_key"],
		session_list: [],
		agent_list: [{ id: "a1", name: "assistant", state: "idle" }],
		tool_list: [{ name: "task_run", description: "Run a task immediately" }],
		agent_file_list: [],
	});
	await openAgentPage(page, "settings");

	await page
		.getByPlaceholder("Default: process working directory", { exact: false })
		.fill("/srv/assistant");
	await page.getByRole("button", { name: "Save Changes" }).click();

	await expect(page.getByTitle("Settings saved")).toBeVisible();
	expect(savedConfig).toMatchObject({
		agents: [{ working_dir: "/srv/assistant" }],
	});

	// A channel's settings save the same way, from its own page.
	await openChannelSettings(page, "+15551234567");
	await page
		.locator('input[placeholder="+15551234567"]')
		.first()
		.fill("+15550000000");
	await page.getByRole("button", { name: "Save Changes" }).click();
	await expect(page.getByTitle("Settings saved")).toBeVisible();
	expect(savedConfig).toMatchObject({
		agents: [{ channels: [{ id: "+15550000000" }] }],
	});
});

test("Remove Agent sits in the danger zone and asks for confirmation", async ({
	page,
}) => {
	await openAgentPage(page, "settings");
	const dangerZone = page.getByTestId("agent-danger-zone");
	await dangerZone
		.getByRole("button", { name: "Remove Agent", exact: true })
		.click();
	const dialog = page.getByRole("alertdialog");
	await expect(dialog).toContainText("Remove agent?");
	await dialog.getByRole("button", { name: "Cancel" }).click();
	await expect(dialog).toHaveCount(0);
	await expect(page.getByTestId("settings-title")).toHaveText(
		"assistant · Agent Settings",
	);
});

test("tasks use the shared trigger editor and save a daily trigger without cron", async ({
	page,
}) => {
	let savedConfig: unknown = null;
	const previews: Record<string, unknown>[] = [];
	await mockMCP(page, {
		config_get: CONFIG,
		config_save: (args) => {
			savedConfig = JSON.parse(String(args?.config ?? "{}"));
			return "ok";
		},
		auth_list: ["anthropic:default", "brave_api_key"],
		session_list: [],
		agent_list: [
			{
				id: "a1",
				name: "assistant",
				model: "anthropic/claude-sonnet-4-5",
				fallbacks: [],
				state: "idle",
			},
		],
		agent_file_list: [],
		task_preview: (args?: Record<string, unknown>) => {
			previews.push(args ?? {});
			return {
				description: "preview",
				next: [
					"2026-11-22T17:00:00Z",
					"2026-11-23T17:00:00Z",
					"2026-11-24T17:00:00Z",
				],
			};
		},
	});

	await page.goto("/agents/assistant/tasks");
	const editor = page.getByTestId("trigger-editor");

	// A hand-written cron expression opens on the Cron tab, unchanged.
	await expect(editor.getByRole("radio", { name: "Cron" })).toHaveAttribute(
		"aria-checked",
		"true",
	);
	await expect(editor.getByLabel("Cron expression")).toHaveValue("0 * * * * *");
	await expect(editor.getByTestId("trigger-preview").locator("li")).toHaveCount(
		3,
	);

	await editor.getByRole("radio", { name: "Daily" }).click();
	await editor.getByLabel("At", { exact: true }).fill("09:00");
	await editor.getByLabel("Timezone").fill("America/Los_Angeles");
	await editor
		.getByLabel("If the last run is still going")
		.selectOption("skip");
	await expect
		.poll(() => previews.at(-1))
		.toMatchObject({
			trigger: "daily",
			at: "09:00",
			timezone: "America/Los_Angeles",
		});

	await page.getByRole("button", { name: "Save Changes" }).click();
	await expect(page.getByTitle("Settings saved")).toBeVisible();
	const task = (
		savedConfig as { agents: { tasks: Record<string, unknown>[] }[] }
	).agents[0].tasks[0];
	expect(task).toMatchObject({
		trigger: "daily",
		at: "09:00",
		timezone: "America/Los_Angeles",
		overlap: "skip",
	});
	expect(task.schedule || undefined).toBeUndefined();
});
