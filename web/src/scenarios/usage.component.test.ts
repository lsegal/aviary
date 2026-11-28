import { expect, test } from "vitest";
import { page } from "vitest/browser";
import { renderApp } from "../component-test/app";
import { mockMCP } from "../component-test/server";
import { chooseOption } from "./helpers/select";
// The pages this file renders (see component-test/routes.ts).
import "../views/SettingsView.vue";
import "../views/UsageView.vue";

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

const breakdown = (title: string) =>
	page.locator("div.rounded-xl").filter({
		has: page.getByRole("heading", { name: title, exact: true }),
	});

const breakdownRow = (title: string, name: string) =>
	breakdown(title)
		.locator("div.mb-3")
		.filter({ has: page.getByText(name, { exact: true }) });

const count = (loc: ReturnType<typeof page.getByTestId>) =>
	expect.poll(() => loc.elements().length);

/** Playwright's toHaveText with an array: every match's whole text, in order. */
const texts = (loc: ReturnType<typeof page.getByTestId>, innerText = false) =>
	expect.poll(() =>
		loc.elements().map((el) =>
			(innerText
				? // biome-ignore lint/performance/useDomNodeTextContent: Playwright's useInnerText, which renders the gap between +N and −N.
					(el as HTMLElement).innerText
				: (el.textContent ?? "")
			)
				.replace(/\s+/g, " ")
				.trim(),
		),
	);

test("shows CLI-reported cost for mixed records", async () => {
	mockMCP({
		agent_list: [],
		job_list: [],
		config_validate: [],
		usage_query: MIXED_RECORDS,
	});
	await renderApp("/system/usage");

	await expect
		.element(page.getByTestId("usage-total-cost"))
		.toHaveText("$1.75");
	await expect
		.element(
			page
				.locator("div.rounded-xl")
				.filter({ hasText: "Cost" })
				.getByText("$1.75", { exact: true }),
		)
		.toBeVisible();

	for (const title of ["Top Models", "Top Agents"]) {
		const name = title === "Top Models" ? "cli/claude/sonnet" : "claude-agent";
		const other = title === "Top Models" ? "cli/codex/gpt-5" : "codex-agent";
		await expect
			.element(breakdownRow(title, name).getByTestId("usage-item-cost"))
			.toHaveText("$1.75");
		await expect.element(breakdownRow(title, other)).toBeVisible();
		await count(breakdownRow(title, other).getByTestId("usage-item-cost")).toBe(
			0,
		);
		await expect
			.element(breakdownRow(title, other))
			.not.toMatchTextContent("$");
	}
});

test("hides cost when no record reports one", async () => {
	mockMCP({
		agent_list: [],
		job_list: [],
		config_validate: [],
		usage_query: MIXED_RECORDS.filter((r) => !("cost_usd" in r)),
	});
	await renderApp("/system/usage");

	await expect
		.element(breakdownRow("Top Models", "cli/codex/gpt-5"))
		.toBeVisible();
	await count(page.getByTestId("usage-total-cost")).toBe(0);
	await count(page.getByTestId("usage-item-cost")).toBe(0);
	await count(page.getByText("$", { exact: false })).toBe(0);
});

const sessionRow = (over: Record<string, unknown>) => ({
	session_id: "s",
	agent_id: "claude-agent",
	model: "cli/claude/sonnet",
	provider: "cli",
	input: 100,
	output: 50,
	cache_read: 0,
	cache_write: 0,
	tool_calls: 2,
	messages: 1,
	has_error: false,
	has_throttle: false,
	first_ts: now,
	last_ts: now,
	name: "Session",
	kind: "chat",
	...over,
});

const SESSIONS_PAGE = {
	total: 696,
	offset: 0,
	limit: 50,
	agents: ["claude-agent", "projects"],
	models: ["cli/claude/sonnet", "opus"],
	totals: {},
	sessions: [
		sessionRow({ session_id: "s1", name: "Main Chat" }),
		sessionRow({
			session_id: "s2",
			name: "Trip planning",
			deleted: true,
			deleted_at: "2026-11-20T15:04:00Z",
		}),
		sessionRow({
			session_id: "s3",
			name: "Unknown session (deleted)",
			deleted: true,
		}),
		sessionRow({
			session_id: "task-session",
			agent_id: "assistant",
			name: "triage",
			kind: "workflow",
			owner: "Team Slack",
			task: "assistant/triage",
			channel: "Team Slack",
		}),
		sessionRow({
			session_id: "o/r#7",
			agent_id: "projects",
			model: "opus",
			name: "Fix the parser",
			kind: "job",
			owner: "o/r#7",
			has_error: true,
		}),
	],
};

function usageTools(
	sessions: (args?: Record<string, unknown>) => unknown = () => SESSIONS_PAGE,
	records: (args?: Record<string, unknown>) => unknown = () => MIXED_RECORDS,
) {
	return {
		agent_list: [],
		job_list: [],
		config_validate: [],
		usage_query: records,
		usage_sessions: sessions,
	};
}

test("lists the server's page of sessions by name, marking deleted ones", async () => {
	mockMCP(usageTools());
	await renderApp("/system/usage");

	await expect
		.element(page.getByTestId("usage-session-total"))
		.toHaveText("696 sessions");
	await expect
		.element(page.getByTestId("usage-session-range"))
		.toHaveText("1–5 of 696");
	const rows = page.getByTestId("usage-session-row");
	await texts(page.getByTestId("usage-session-name")).toEqual([
		"Main Chat",
		"Trip planning",
		"Unknown session (deleted)",
		"triage",
		"Fix the parser",
	]);
	await texts(page.getByTestId("usage-session-kind")).toEqual([
		"Chat",
		"Chat",
		"Chat",
		"Workflow · Team Slack",
		"Code Loop job · o/r#7",
	]);

	// A live chat links to it; deleted sessions and job runs link nowhere.
	await expect
		.element(rows.nth(0).getByRole("link", { name: "Main Chat" }))
		.toHaveAttribute("href", "/agents/claude-agent/chat/s1");
	for (const i of [1, 2, 3, 4]) {
		await count(rows.nth(i).getByRole("link")).toBe(0);
	}

	// Deleted sessions keep their name, struck through, with a deletion tooltip.
	const deleted = rows.nth(1).getByTestId("usage-session-name");
	await expect
		.element(deleted)
		.toHaveCSS("text-decoration-line", "line-through");
	await expect
		.element(deleted)
		.toHaveAttribute(
			"title",
			expect.stringMatching(/^Session deleted on Nov 20, 2026/),
		);
	await expect
		.element(rows.nth(2).getByTestId("usage-session-name"))
		.toHaveAttribute("title", "Session deleted");
	await count(rows.nth(1).locator("svg")).toBe(1);
	await count(rows.nth(0).locator("svg")).toBe(0);
	await expect
		.element(rows.nth(0).getByTestId("usage-session-name"))
		.toHaveCSS("text-decoration-line", "none");
});

test("pages and sorts the sessions on the server", async () => {
	const calls: Record<string, unknown>[] = [];
	mockMCP(
		usageTools((args) => {
			calls.push(args ?? {});
			return { ...SESSIONS_PAGE, offset: Number(args?.offset ?? 0) };
		}),
	);
	await renderApp("/system/usage");
	await expect
		.element(page.getByTestId("usage-page"))
		.toHaveText("Page 1 of 14");
	expect(calls.at(-1)).toMatchObject({
		sort: "last",
		order: "desc",
		limit: 50,
		offset: 0,
	});
	await expect.element(page.getByTestId("usage-page-prev")).toBeDisabled();

	await page.getByTestId("usage-page-next").click();
	await expect
		.element(page.getByTestId("usage-page"))
		.toHaveText("Page 2 of 14");
	expect(calls.at(-1)).toMatchObject({ offset: 50 });
	await expect
		.element(page.getByTestId("usage-session-range"))
		.toHaveText("51–55 of 696");

	// Sorting starts over at the first page; a second click flips the order.
	await page.getByTestId("usage-sort-tokens").click();
	await expect
		.element(page.getByTestId("usage-page"))
		.toHaveText("Page 1 of 14");
	expect(calls.at(-1)).toMatchObject({
		sort: "tokens",
		order: "desc",
		offset: 0,
	});
	await expect
		.element(page.getByRole("columnheader", { name: "Tokens" }))
		.toHaveAttribute("aria-sort", "descending");
	await page.getByTestId("usage-sort-tokens").click();
	await expect
		.element(page.getByRole("columnheader", { name: "Tokens" }))
		.toHaveAttribute("aria-sort", "ascending");
	await expect
		.poll(() => calls.at(-1))
		.toMatchObject({ sort: "tokens", order: "asc" });
	await page.getByTestId("usage-sort-name").click();
	await expect
		.element(page.getByRole("columnheader", { name: "Session" }))
		.toHaveAttribute("aria-sort", "ascending");
});

test("filters narrow the sessions and the totals together", async () => {
	const sessionCalls: Record<string, unknown>[] = [];
	const queryCalls: Record<string, unknown>[] = [];
	mockMCP(
		usageTools(
			(args) => {
				sessionCalls.push(args ?? {});
				return args?.agent
					? {
							...SESSIONS_PAGE,
							total: 1,
							sessions: [SESSIONS_PAGE.sessions[4]],
						}
					: SESSIONS_PAGE;
			},
			(args) => {
				queryCalls.push(args ?? {});
				return args?.agent
					? MIXED_RECORDS.filter((r) => r.agent_id === "codex-agent")
					: MIXED_RECORDS;
			},
		),
	);
	await renderApp("/system/usage");
	await expect
		.element(page.getByTestId("usage-session-total"))
		.toHaveText("696 sessions");

	await chooseOption(page.getByTestId("usage-filter-agent"), "projects");
	await expect
		.element(page.getByTestId("usage-session-total"))
		.toHaveText("1 session");
	expect(sessionCalls.at(-1)).toMatchObject({ agent: "projects", offset: 0 });
	await expect
		.poll(() => queryCalls.at(-1))
		.toMatchObject({ agent: "projects" });
	// The page totals come from the filtered records.
	await count(page.getByTestId("usage-total-cost")).toBe(0);

	await chooseOption(page.getByTestId("usage-filter-model"), "opus");
	await expect
		.poll(() => sessionCalls.at(-1))
		.toMatchObject({
			agent: "projects",
			model: "opus",
		});
	await chooseOption(page.getByTestId("usage-filter-status"), "Error");
	await expect.poll(() => queryCalls.at(-1)).toMatchObject({ status: "error" });
	await chooseOption(
		page.getByTestId("usage-filter-deleted"),
		"Without deleted",
	);
	await expect
		.poll(() => sessionCalls.at(-1))
		.toMatchObject({
			deleted: "exclude",
			status: "error",
		});
	await chooseOption(page.getByTestId("usage-filter-deleted"), "With deleted");
	await expect.poll(() => sessionCalls.at(-1)).not.toHaveProperty("deleted");
});

test("shows sessions as cards without horizontal overflow on a phone", async () => {
	await page.viewport(375, 800);
	mockMCP(usageTools());
	await renderApp("/system/usage");
	await count(page.getByTestId("usage-session-card")).toBe(5);
	await expect
		.element(page.getByTestId("usage-session-row").first())
		.not.toBeVisible();
	let node: Element | null = page.getByTestId("usage-sessions").element();
	let worst = 0;
	while (node) {
		worst = Math.max(worst, node.scrollWidth - node.clientWidth);
		node = node.parentElement;
	}
	expect(worst).toBeLessThanOrEqual(0);
});

test("summarizes project outcomes and wall time apart from token metrics", async () => {
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
	mockMCP({
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
	await renderApp("/system/usage");

	await expect
		.element(page.getByTestId("usage-projects-issues"))
		.toHaveText("4");
	await expect
		.element(page.getByTestId("usage-projects-merged"))
		.toHaveText("2");
	await expect
		.element(page.getByTestId("usage-projects-failed"))
		.toHaveText("2");
	await expect
		.element(page.getByTestId("usage-projects-wall-time"))
		.toHaveText("2h 15m");
	await expect
		.element(page.getByTestId("usage-projects-additions"))
		.toHaveText("+1,240");
	await expect
		.element(page.getByTestId("usage-projects-deletions"))
		.toHaveText("−45");
	// Each project totals its own merged pull requests' lines.
	await texts(page.getByTestId("usage-project-name")).toEqual(["a/b", "o/r"]);
	await texts(page.getByTestId("usage-project-lines"), true).toEqual([
		"+0 −0",
		"+1,240 −45",
	]);
	await expect
		.element(page.getByTestId("usage-project-row").nth(1))
		.toMatchTextContent("2h 5m");
	// Project records are not token calls.
	await expect
		.element(
			page
				.locator("div.rounded-xl")
				.filter({ hasText: "Messages" })
				.getByText("3", { exact: true }),
		)
		.toBeVisible();
});

// #1470: records that spent no tokens left nameless "0" rows in the
// breakdown cards, and a bare model split from its provider/model form.
test("breakdowns merge model name forms and skip tokenless records", async () => {
	mockMCP({
		agent_list: [],
		job_list: [],
		config_validate: [],
		usage_query: [
			{
				timestamp: now,
				session_id: "o/r#7",
				agent_id: "projects",
				model: "opus",
				provider: "claude",
				input_tokens: 600,
				output_tokens: 100,
			},
			{
				timestamp: now,
				session_id: "s1",
				agent_id: "assistant",
				model: "claude/opus",
				provider: "claude",
				input_tokens: 200,
				output_tokens: 100,
			},
			{
				timestamp: now,
				session_id: "task-editor",
				agent_id: "assistant",
				model: "",
				provider: "",
				input_tokens: 0,
				output_tokens: 0,
				has_error: true,
			},
		],
	});
	await renderApp("/system/usage");

	for (const [title, name] of [
		["Top Models", "claude/opus"],
		["Top Providers", "claude"],
	]) {
		const rows = breakdown(title).locator("div.mb-3");
		await count(rows).toBe(1);
		await expect.element(rows.first()).toMatchTextContent(name);
		await expect.element(rows.first()).toMatchTextContent("1.0K");
	}
});

test("breakdowns show the empty state when no record spent tokens", async () => {
	mockMCP({
		agent_list: [],
		job_list: [],
		config_validate: [],
		usage_query: [
			{
				timestamp: now,
				session_id: "task-editor",
				agent_id: "assistant",
				model: "",
				provider: "",
				input_tokens: 0,
				output_tokens: 0,
				has_error: true,
			},
		],
	});
	await renderApp("/system/usage");

	for (const title of ["Top Models", "Top Providers", "Top Agents"]) {
		await expect.element(breakdown(title).getByText("No data")).toBeVisible();
		await count(breakdown(title).locator("div.mb-3")).toBe(0);
	}
});
