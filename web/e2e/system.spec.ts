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
			{
				name: "task_run",
				description: "Run a scheduled task immediately.",
				inputSchema: {
					type: "object",
					required: ["name"],
					properties: {
						name: {
							type: "string",
							description: "Task name to run immediately.",
						},
						force: {
							type: "boolean",
							description:
								"Force execution even if the task is already active.",
						},
					},
				},
			},
			{ name: "config_get", description: "Read the active configuration." },
		],
		task_run: async (args: any) => {
			await new Promise((resolve) => setTimeout(resolve, 150));
			return {
				ok: true,
				args,
			};
		},
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

test("system tools can run a tool from the menu and show output", async ({
	page,
}) => {
	await page.goto("/system/tools");

	await page.getByTestId("run-tool-task_run").click();

	await expect(page.getByRole("heading", { name: "Run Tool" })).toBeVisible();
	await expect(
		page.getByRole("paragraph").filter({ hasText: "task_run" }),
	).toBeVisible();
	await expect(
		page.getByPlaceholder("Task name to run immediately."),
	).toBeVisible();

	await page.getByPlaceholder("Task name to run immediately.").fill("nightly");
	await page.getByRole("combobox").nth(0).selectOption("true");
	await page.getByRole("button", { name: "Run Tool" }).click();

	await expect(page.getByTestId("tool-run-output")).toContainText('"ok":true');
	await expect(page.getByTestId("tool-run-output")).toContainText(
		'"name":"nightly"',
	);
	await expect(page.getByTestId("tool-run-output")).toContainText(
		'"force":true',
	);

	await page.getByRole("button", { name: "Close" }).click();
	await page.getByTestId("run-tool-task_run").click();

	await expect(
		page.getByPlaceholder("Task name to run immediately."),
	).toHaveValue("nightly");
	await expect(page.getByRole("combobox").nth(0)).toHaveValue("true");
	await expect(page.getByTestId("tool-run-output")).toContainText('"ok":true');

	await page.getByPlaceholder("Task name to run immediately.").fill("weekly");
	await page.getByRole("button", { name: "Run Tool" }).click();
	await expect(page.getByTestId("tool-run-output")).toHaveClass(
		/text-gray-400/,
	);
	await expect(page.getByTestId("tool-run-output")).toContainText('"ok":true');
	await expect(page.getByTestId("tool-run-output")).toContainText(
		'"name":"weekly"',
	);
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

test("settings leaves server and cdp ports unset until the user enters them", async ({
	page,
}) => {
	let savedConfig: Record<string, unknown> | null = null;
	await setAuthToken(page);
	await mockMCP(page, {
		config_get: {
			server: {
				tls: { cert: "", key: "" },
				external_access: false,
				no_tls: false,
			},
			agents: [],
			models: { providers: {}, defaults: { model: "", fallbacks: [] } },
			browser: { binary: "" },
			scheduler: { concurrency: "" },
			skills: {},
		},
		config_save: (args: any) => {
			savedConfig = JSON.parse(String(args?.config ?? "{}")) as Record<
				string,
				unknown
			>;
			return {};
		},
		skills_list: [],
		tool_list: [],
	});

	await page.goto("/settings");

	const portInput = page.getByPlaceholder("16677");
	const cdpPortInput = page.getByPlaceholder("9222");

	await expect(portInput).toHaveValue("");
	await expect(cdpPortInput).toHaveValue("");

	await portInput.fill("12a3");
	await cdpPortInput.fill("9b2c2d");
	await expect(portInput).toHaveValue("123");
	await expect(cdpPortInput).toHaveValue("922");

	await portInput.clear();
	await cdpPortInput.clear();
	await page
		.getByRole("checkbox", { name: /Expose service externally/i })
		.check();
	await page.getByRole("button", { name: "Save Changes" }).click();

	expect(savedConfig).not.toBeNull();
	expect(savedConfig).toMatchObject({
		server: {
			port: 0,
			external_access: true,
		},
		browser: {
			cdp_port: 0,
		},
	});
});
