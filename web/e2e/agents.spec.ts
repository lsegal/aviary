import { expect, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

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
					disabledTools: ["task_run"],
					allowFrom: [
						{
							from: "*",
							respondToMentions: true,
							restrictTools: ["task_run", "browser_open", "usage_query"],
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
					target: "signal:+15551234567:+15551234567",
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
	await page.goto("/settings");
	await page.getByRole("link", { name: "Agents & Tasks", exact: true }).click();

	await expect(page.getByRole("button", { name: "Add Agent" })).toBeVisible();
	await expect(
		page.getByRole("button", { name: "Remove Agent", exact: true }).first(),
	).toBeVisible();
	// General subtab (default): agent name field is always visible in the header
	await expect(
		page.locator('input[placeholder="assistant"]').first(),
	).toHaveValue("assistant");

	// Tasks subtab
	await page
		.getByRole("button", { name: "Tasks", exact: true })
		.first()
		.click();
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
		page.getByRole("heading", { name: "Tasks", exact: true }),
	).toBeVisible();

	// Channels subtab
	await page
		.getByRole("button", { name: "Channels", exact: true })
		.first()
		.click();
	await expect(
		page.locator('input[placeholder="e.g. +15551234567 or user ID"]').first(),
	).toHaveValue("+15551234567");
});

test("tab switching does not blank content", async ({ page }) => {
	await page.goto("/settings");

	for (let i = 0; i < 3; i += 1) {
		await page.getByRole("link", { name: "General", exact: true }).click();
		await expect(
			page.getByRole("heading", { name: "Server", exact: true }),
		).toBeVisible();

		await page
			.getByRole("link", { name: "Agents & Tasks", exact: true })
			.click();
		await expect(page.getByRole("button", { name: "Add Agent" })).toBeVisible();

		await page.getByRole("link", { name: "Sessions", exact: true }).click();
		await expect(
			page.getByRole("button", { name: "Refresh Sessions" }),
		).toBeVisible();

		await page
			.getByRole("link", { name: "Providers & Auth", exact: true })
			.click();
		await expect(
			page.getByRole("heading", { name: "Credentials", exact: true }),
		).toBeVisible();
	}
});

test("general tab shows web search settings", async ({ page }) => {
	await page.goto("/settings");

	await expect(
		page.getByRole("heading", { name: "Web Search", exact: true }),
	).toBeVisible();
	await expect(
		page.getByText("auth:brave_api_key", { exact: true }),
	).toBeVisible();
});

test("model dropdown hides models from unauthenticated providers", async ({
	page,
}) => {
	await page.goto("/settings");

	await page.locator('input[placeholder="Select a model…"]').first().click();

	await expect(
		page.getByText("anthropic/claude-3-5-haiku-latest", { exact: true }),
	).toBeVisible();
	await expect(page.getByText("openai/gpt-4o", { exact: true })).toHaveCount(0);
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

	await page.goto("/settings");
	await page.locator(".cursor-text").first().click();

	await expect(
		page.getByText("google-gemini/gemini-2.5-flash", { exact: true }),
	).toBeVisible();
});

test("providers auth tab shows credential controls", async ({ page }) => {
	await page.goto("/settings");
	await page
		.getByRole("link", { name: "Providers & Auth", exact: true })
		.click();

	await expect(
		page.getByRole("heading", { name: "Credentials", exact: true }),
	).toBeVisible();
	await expect(
		page.getByRole("heading", { name: "Extra Secrets", exact: true }),
	).toBeVisible();
});

test("permissions preset disables inaccessible tool groups and tools", async ({
	page,
}) => {
	await page.goto("/settings");
	await page.getByRole("link", { name: "Agents & Tasks", exact: true }).click();
	await page
		.getByRole("button", { name: "Permissions", exact: true })
		.first()
		.click();

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
	await page.goto("/settings");
	await page.getByRole("link", { name: "Agents & Tasks", exact: true }).click();
	await page
		.getByRole("button", { name: "Permissions", exact: true })
		.first()
		.click();

	await page.getByTestId("agent-tool-permissions-inspect-assistant").click();
	await expect(
		page.getByRole("heading", { name: "Inspect Tool Permissions" }),
	).toBeVisible();
	await expect(
		page.getByTestId("tool-permissions-inspector-output"),
	).toContainText('"finalTools": [\n    "task_run"\n  ]');
	await page.getByRole("button", { name: "Close" }).click();

	await page
		.getByRole("button", { name: "Channels", exact: true })
		.first()
		.click();
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
	await page.goto("/settings");
	await page.getByRole("link", { name: "Agents & Tasks", exact: true }).click();
	await page
		.getByRole("button", { name: "Channels", exact: true })
		.first()
		.click();

	const phoneInput = page.locator('input[placeholder="+15551234567"]').first();

	await expect(page.getByLabel("Show typing indicator")).toBeChecked();
	await expect(page.getByLabel("Reply to replies")).toBeChecked();
	await expect(page.getByLabel("React to emojis")).toBeChecked();
	await expect(page.getByLabel("Send read receipts")).toBeChecked();

	await phoneInput.fill("+12132957731");
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

	await page.goto("/settings");
	await page.getByRole("link", { name: "Agents & Tasks", exact: true }).click();
	await page
		.getByRole("button", { name: "Tasks", exact: true })
		.first()
		.click();

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
	await page.goto("/settings");
	await page.getByRole("link", { name: "Agents & Tasks", exact: true }).click();
	await page
		.getByRole("button", { name: "Tasks", exact: true })
		.first()
		.click();

	await expect(page.getByText("disabled", { exact: true })).toBeVisible();
	const toggle = page
		.getByRole("switch", { name: "Toggle task enabled" })
		.first();
	await toggle.click();
	await expect(toggle).toHaveAttribute("aria-checked", "true");
});

test("task header wraps cleanly on mobile", async ({ page }) => {
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto("/settings");
	await page.getByRole("link", { name: "Agents & Tasks", exact: true }).click();
	await page
		.getByRole("button", { name: "Tasks", exact: true })
		.first()
		.click();

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

	await page.goto("/settings");
	await page.getByRole("link", { name: "Agents & Tasks", exact: true }).click();
	await page
		.getByRole("button", { name: "Tasks", exact: true })
		.first()
		.click();

	await expect(page.getByRole("button", { name: "file-task" })).toBeVisible();
	await page.getByLabel("Delete task").click();
	await page.getByRole("button", { name: "Delete" }).click();

	await expect(page.getByRole("button", { name: "file-task" })).toHaveCount(0);
	await expect(page.getByTitle("Settings saved")).toBeVisible();
	expect(savedConfig).toMatchObject({
		agents: [
			{
				tasks: [],
			},
		],
	});
});

test("agent files editor lists root markdown files and protects built-ins", async ({
	page,
}) => {
	await page.goto("/settings");
	await page.getByRole("link", { name: "Agents & Tasks", exact: true }).click();

	await expect(page.getByRole("button", { name: "IDENTITY.md" })).toBeVisible();
	await expect(page.getByRole("button", { name: "AGENTS.md" })).toBeVisible();
	await expect(page.getByRole("button", { name: "MEMORY.md" })).toBeVisible();
	await expect(page.getByRole("button", { name: "RULES.md" })).toBeVisible();
	await page.getByRole("button", { name: "AGENTS.md" }).click();
	await expect(page.getByRole("button", { name: "Delete" })).toBeDisabled();
	await page.getByRole("button", { name: "RULES.md" }).click();
	await expect(page.getByRole("button", { name: "Delete" })).toBeDisabled();

	await page.getByRole("button", { name: "IDENTITY.md" }).click();
	await expect(page.getByRole("button", { name: "Delete" })).toBeEnabled();

	await page.getByPlaceholder("IDENTITY.md").fill("PROFILE");
	await page.getByRole("button", { name: "+", exact: true }).click();
	await expect(page.getByRole("button", { name: "PROFILE.md" })).toBeVisible();
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

	await page.goto("/settings");
	await page.getByRole("link", { name: "Agents & Tasks", exact: true }).click();
	await page.getByRole("button", { name: "Refresh" }).first().click();

	await expect(page.getByRole("button", { name: "AGENTS.md" })).toBeVisible();
	await expect(page.getByRole("button", { name: "RULES.md" })).toBeVisible();
	await expect(page.getByRole("button", { name: "MEMORY.md" })).toBeVisible();
	await expect(
		page.getByText("No root markdown files yet. Refresh or add one."),
	).toHaveCount(0);
});
