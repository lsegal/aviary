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

test("summarizes project outcomes and wall time apart from token metrics", async ({
	page,
}) => {
	const project = (
		issue: string,
		extra: {
			outcome?: string;
			duration_ms?: number;
			additions?: number;
			deletions?: number;
		},
		timestamp = now,
	) => ({
		timestamp,
		session_id: issue,
		agent_id: "projects",
		model: "",
		provider: "",
		input_tokens: 0,
		output_tokens: 0,
		project: { issue, ...extra },
	});
	const earlier = new Date(Date.now() - 60_000).toISOString();
	await mockMCP(page, {
		agent_list: [],
		job_list: [],
		config_validate: [],
		usage_query: [
			...MIXED_RECORDS,
			project("o/r#1", { duration_ms: 90 * 60_000 }),
			project("o/r#1", { outcome: "merged", additions: 1200, deletions: 30 }),
			project("o/r#2", { duration_ms: 30 * 60_000 }),
			project("o/r#2", { outcome: "failed" }),
			// Failed, then retried and merged: counts as merged only.
			project("o/r#3", { duration_ms: 5 * 60_000 }),
			project("o/r#3", { outcome: "failed" }, earlier),
			project("o/r#3", { outcome: "merged", additions: 40, deletions: 15 }),
			// A second project; its failed job opened no pull request.
			project("a/b#4", { duration_ms: 10 * 60_000 }),
			project("a/b#4", { outcome: "failed" }),
		],
	});
	await page.goto("/system/usage");

	await expect(page.getByTestId("usage-projects-issues")).toHaveText("4");
	await expect(page.getByTestId("usage-projects-merged")).toHaveText("2");
	await expect(page.getByTestId("usage-projects-failed")).toHaveText("2");
	await expect(page.getByTestId("usage-projects-wall-time")).toHaveText(
		"2h 15m",
	);
	await expect(page.getByTestId("usage-projects-additions")).toHaveText(
		"+1,240",
	);
	await expect(page.getByTestId("usage-projects-deletions")).toHaveText(
		"−45",
	);
	// Each project totals its own merged pull requests' lines.
	await expect(page.getByTestId("usage-project-name")).toHaveText([
		"a/b",
		"o/r",
	]);
	await expect(page.getByTestId("usage-project-lines")).toHaveText(
		["+0 −0", "+1,240 −45"],
		{ useInnerText: true },
	);
	await expect(page.getByTestId("usage-project-row").nth(1)).toContainText(
		"2h 5m",
	);
	// Project records are not token calls.
	await expect(
		page
			.locator("div.rounded-xl", { hasText: "Messages" })
			.getByText("3", { exact: true }),
	).toBeVisible();
});
