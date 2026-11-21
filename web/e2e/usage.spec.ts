import { expect, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

const now = new Date().toISOString();

const MIXED_RECORDS = [
	{
		timestamp: now,
		session_id: "s1",
		agent_id: "claude-agent",
		model: "cli/claude/sonnet",
		provider: "cli",
		input_tokens: 100,
		output_tokens: 50,
		cost_usd: 1.25,
	},
	{
		timestamp: now,
		session_id: "s1",
		agent_id: "claude-agent",
		model: "cli/claude/sonnet",
		provider: "cli",
		input_tokens: 10,
		output_tokens: 5,
		cost_usd: 0.5,
	},
	{
		timestamp: now,
		session_id: "s2",
		agent_id: "codex-agent",
		model: "cli/codex/gpt-5",
		provider: "cli",
		input_tokens: 2000,
		output_tokens: 1000,
	},
];

const breakdown = (page: import("@playwright/test").Page, title: string) =>
	page.locator("div.rounded-xl", {
		has: page.getByRole("heading", { name: title, exact: true }),
	});

const breakdownRow = (
	page: import("@playwright/test").Page,
	title: string,
	name: string,
) =>
	breakdown(page, title)
		.locator("div.mb-3")
		.filter({ has: page.getByText(name, { exact: true }) });

test.beforeEach(async ({ page }) => {
	await setAuthToken(page);
});

test("shows CLI-reported cost for mixed records", async ({ page }) => {
	await mockMCP(page, {
		agent_list: [],
		job_list: [],
		config_validate: [],
		usage_query: MIXED_RECORDS,
	});
	await page.goto("/system/usage");

	await expect(page.getByTestId("usage-total-cost")).toHaveText("$1.75");
	await expect(
		page
			.locator("div.rounded-xl", { hasText: "Cost" })
			.getByText("$1.75", { exact: true }),
	).toBeVisible();

	for (const title of ["Top Models", "Top Agents"]) {
		const name = title === "Top Models" ? "cli/claude/sonnet" : "claude-agent";
		const other = title === "Top Models" ? "cli/codex/gpt-5" : "codex-agent";
		await expect(
			breakdownRow(page, title, name).getByTestId("usage-item-cost"),
		).toHaveText("$1.75");
		await expect(breakdownRow(page, title, other)).toBeVisible();
		await expect(
			breakdownRow(page, title, other).getByTestId("usage-item-cost"),
		).toHaveCount(0);
		await expect(breakdownRow(page, title, other)).not.toContainText("$");
	}
});

test("hides cost when no record reports one", async ({ page }) => {
	await mockMCP(page, {
		agent_list: [],
		job_list: [],
		config_validate: [],
		usage_query: MIXED_RECORDS.filter((r) => !("cost_usd" in r)),
	});
	await page.goto("/system/usage");

	await expect(
		breakdownRow(page, "Top Models", "cli/codex/gpt-5"),
	).toBeVisible();
	await expect(page.getByTestId("usage-total-cost")).toHaveCount(0);
	await expect(page.getByTestId("usage-item-cost")).toHaveCount(0);
	await expect(page.getByText("$", { exact: false })).toHaveCount(0);
});

test("lists sessions by name instead of by ID", async ({ page }) => {
	await mockMCP(page, {
		agent_list: [],
		job_list: [],
		config_validate: [],
		usage_query: MIXED_RECORDS,
		session_list: (args) =>
			args?.agent === "claude-agent"
				? [{ id: "s1", agent_id: "claude-agent", name: "main", created_at: "" }]
				: [],
	});
	await page.goto("/system/usage");

	const names = page.getByTestId("usage-session-name");
	await expect(names).toHaveText(["Main Chat", "Deleted session"], {
		useInnerText: true,
	});
});
