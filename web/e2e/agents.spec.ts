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
			channels: [],
			tasks: [
				{
					name: "daily-briefing",
					schedule: "0 * * * * *",
					prompt: "Summarize updates",
					channel: "last",
				},
			],
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
		auth_list: ["anthropic:default", "openai:default", "brave_api_key"],
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
