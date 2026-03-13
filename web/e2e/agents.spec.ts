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
					name: "daily-briefing",
					schedule: "0 * * * * *",
					prompt: "Summarize updates",
					channel: "route:signal:0:+15551234567",
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
	browser: { binary: "", cdp_port: 9222 },
	search: { web: { brave_api_key: "auth:brave_api_key" } },
	scheduler: { concurrency: "auto" },
};

test.beforeEach(async ({ page }) => {
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
	});
});

test("agents and tasks tab shows configured entries", async ({ page }) => {
	await page.goto("/settings");
	await page.getByRole("link", { name: "Agents & Tasks", exact: true }).click();

	await expect(page.getByRole("button", { name: "+ Add Agent" })).toBeVisible();
	await expect(
		page.locator('input[placeholder="assistant"]').first(),
	).toHaveValue("assistant");
	await expect(
		page.locator('input[placeholder="daily-briefing"]').first(),
	).toHaveValue("daily-briefing");
	await expect(
		page.locator('input[placeholder="Phone number or group ID"]').first(),
	).toHaveValue("+15551234567");
	await expect(
		page.getByRole("heading", { name: "Tasks", exact: true }),
	).toBeVisible();
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
		await expect(
			page.getByRole("button", { name: "+ Add Agent" }),
		).toBeVisible();

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

	await page.getByTestId("agent-tool-permissions-inspect-assistant").click();
	await expect(
		page.getByRole("heading", { name: "Inspect Tool Permissions" }),
	).toBeVisible();
	await expect(
		page.getByTestId("tool-permissions-inspector-output"),
	).toContainText('"finalTools": [\n    "task_run"\n  ]');
	await page.getByRole("button", { name: "Close" }).click();

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

	const phoneInput = page.locator('input[placeholder="+15551234567"]').first();

	await expect(page.getByLabel("Show typing indicator")).toBeChecked();
	await expect(page.getByLabel("Reply to replies")).toBeChecked();
	await expect(page.getByLabel("React to emojis")).toBeChecked();
	await expect(page.getByLabel("Send read receipts")).toBeChecked();

	await phoneInput.fill("+12132957731");
	await page.getByRole("button", { name: "Save Changes" }).click();

	await expect(page.getByText("Settings saved successfully.")).toBeVisible();
	await expect(page.getByLabel("Show typing indicator")).toBeChecked();
	await expect(page.getByLabel("Reply to replies")).toBeChecked();
	await expect(page.getByLabel("React to emojis")).toBeChecked();
	await expect(page.getByLabel("Send read receipts")).toBeChecked();
});
