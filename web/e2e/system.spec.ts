import { expect, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

test.beforeEach(async ({ page }) => {
	await setAuthToken(page);
	await mockMCP(page, {
		config_get: {
			server: {
				port: 16677,
				tls: { cert: "", key: "" },
				external_access: false,
				no_tls: false,
			},
			agents: [],
			models: { providers: {}, defaults: { model: "", fallbacks: [] } },
			browser: { binary: "", cdp_port: 0 },
			scheduler: { concurrency: "" },
			skills: {
				gogcli: { enabled: true },
			},
		},
		config_save: {},
		skills_list: [
			{
				name: "gogcli",
				description: "Control GOG Galaxy tasks.",
				path: "skills/gogcli/SKILL.md",
				source: "builtin",
				enabled: true,
			},
			{
				name: "deploy",
				description: "Deployment checklist helpers.",
				path: "C:/skills/deploy/SKILL.md",
				source: "disk",
				enabled: false,
			},
		],
		tool_list: [
			{ name: "agent_list", description: "List configured agents." },
			{ name: "task_run", description: "Run a scheduled task immediately." },
			{ name: "config_get", description: "Read the active configuration." },
		],
	});
});

test("system tools shows grouped MCP tools and active skills", async ({
	page,
}) => {
	await page.goto("/system/tools");

	await expect(
		page.getByRole("heading", { name: "System Tools" }),
	).toBeVisible();
	await expect(page.getByText("agent_list")).toBeVisible();
	await expect(page.getByText("task_run")).toBeVisible();
	await expect(
		page.getByRole("heading", { name: "Activated Skills" }),
	).toBeVisible();
	await expect(page.getByText("gogcli", { exact: true })).toBeVisible();
});

test("system skills can disable and enable installed skills", async ({
	page,
}) => {
	await page.goto("/system/skills");

	await expect(
		page.getByRole("heading", { name: "Skill Marketplace" }),
	).toBeVisible();
	const disableButton = page
		.locator("article")
		.filter({ hasText: "gogcli" })
		.getByRole("button", { name: "Disable" });
	await expect(disableButton).toBeVisible();
	await disableButton.click();
	await expect(
		page
			.locator("article")
			.filter({ hasText: "gogcli" })
			.getByRole("button", { name: "Enable" }),
	).toBeVisible();

	await page.getByRole("button", { name: "Disabled" }).click();
	await expect(
		page.locator("article").filter({ hasText: "deploy" }),
	).toBeVisible();
});
