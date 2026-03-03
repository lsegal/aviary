import { expect, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

const AGENTS = [
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
		model: "openai/gpt-4o",
		fallbacks: [],
		state: "running",
	},
];

const JOBS = [
	{
		id: "j1",
		task_id: "nightly",
		agent_name: "assistant",
		status: "completed",
		attempts: 1,
		created_at: "",
		updated_at: "",
	},
	{
		id: "j2",
		task_id: "build",
		agent_name: "coder",
		status: "in_progress",
		attempts: 1,
		created_at: "",
		updated_at: "",
	},
];

test.beforeEach(async ({ page }) => {
	await setAuthToken(page);
});

test("shows agent and job counts", async ({ page }) => {
	await mockMCP(page, {
		agent_list: AGENTS,
		job_list: JOBS,
		config_validate: [],
	});
	await page.goto("/overview");

	await expect(page.getByText("2").first()).toBeVisible(); // 2 agents
	// scope to the Jobs card to avoid matching the agent state badge
	const jobsCard = page.locator("a", { hasText: "Jobs" });
	await expect(jobsCard.getByText("1 running")).toBeVisible();
});

test("health card is green when no config issues", async ({ page }) => {
	await mockMCP(page, {
		agent_list: AGENTS,
		job_list: [],
		config_validate: [],
	});
	await page.goto("/overview");

	await expect(page.getByText("Healthy")).toBeVisible();
	await expect(page.getByText("No issues found")).toBeVisible();
});

test("health card is red and shows errors", async ({ page }) => {
	const issues = [
		{
			level: "ERROR",
			field: "agents[0].model",
			message:
				"credential \"anthropic:default\" not found in auth store — run 'aviary auth set anthropic:default <your-api-key>'",
		},
		{
			level: "WARN",
			field: "agents[0].channels[0].allowFrom",
			message:
				"empty allowFrom list will silently reject all incoming messages",
		},
	];
	await mockMCP(page, {
		agent_list: AGENTS,
		job_list: [],
		config_validate: issues,
	});
	await page.goto("/overview");

	await expect(page.getByText("Errors")).toBeVisible();
	await expect(page.getByText("1 error, 1 warning")).toBeVisible();
	await expect(page.getByText("agents[0].model")).toBeVisible();
	await expect(page.getByText("agents[0].channels[0].allowFrom")).toBeVisible();
});

test("health card is yellow when warnings only", async ({ page }) => {
	const issues = [
		{
			level: "WARN",
			field: "agents[0].model",
			message: "no model configured; agent will not respond to prompts",
		},
	];
	await mockMCP(page, {
		agent_list: [],
		job_list: [],
		config_validate: issues,
	});
	await page.goto("/overview");

	await expect(page.getByText("Warnings")).toBeVisible();
	await expect(page.getByText(/^1 warning$/)).toBeVisible();
});

test("re-check button refreshes doctor status", async ({ page }) => {
	const issues = [
		{
			level: "ERROR",
			field: "agents[0].model",
			message: "credential not found",
		},
	];
	await mockMCP(page, {
		agent_list: [],
		job_list: [],
		config_validate: issues,
	});
	await page.goto("/overview");
	await expect(page.getByText("Errors")).toBeVisible();

	// Fix the issue, re-intercept with clean result.
	await page.unroute("/mcp");
	await mockMCP(page, { agent_list: [], job_list: [], config_validate: [] });
	await page.getByRole("button", { name: "Re-check" }).click();

	await expect(page.getByText("No issues found")).toBeVisible();
});

test("stat cards link to their detail views", async ({ page }) => {
	await mockMCP(page, { agent_list: [], job_list: [], config_validate: [] });
	await page.goto("/overview");

	await page.getByText("Agents").first().click();
	await expect(page).toHaveURL("/settings?tab=agents");
});
