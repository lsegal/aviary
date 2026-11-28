import { expect, type Page, test } from "@playwright/test";
import {
	analysisCache,
	sawChecking,
	watchForChecking,
} from "./helpers/analysisCache";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

// The other workflow scenarios are component tests now
// (web/src/scenarios/workflows-*.component.test.ts). This one stays here
// because it needs real page reloads and a second browser: the editor keeps
// analyses in a module-level cache that only a new page drops, and the
// scenario checks that reopening after a reload, in another browser, and
// after the server's cache is invalidated goes to the server's cache.

interface Task {
	name: string;
	enabled?: boolean;
	type?: string;
	prompt?: string;
	trigger?: string;
	channel?: string;
	entry?: string;
	match?: string;
	target?: string;
	every?: string;
	model?: string;
	precompile?: boolean;
	compiled?: string;
	compiled_key?: string;
	script_failures?: number;
	script_last_error?: string;
}

const NOW = new Date().toISOString();
const AGO = new Date(Date.now() - 60_000).toISOString();

const TRIAGE: Task = {
	name: "triage",
	trigger: "message",
	channel: "Team Slack",
	entry: "C1",
	match: "*crash*",
	prompt: "Triage the crash.",
	target: "reply",
};

/**
 * task_analyze's answer for args. A workflow run also carries its triggering
 * message, so it costs more than the same scheduled task.
 */
function workflowAnalysis(args?: Record<string, unknown>) {
	const workflow = args?.trigger === "message";
	const model = {
		available: true,
		min_tokens: workflow ? 5_000 : 4_000,
		max_tokens: workflow ? 9_000 : 7_000,
		min_cost_usd: 0.01,
		max_cost_usd: 0.03,
		...(workflow ? { min_context_tokens: 55, max_context_tokens: 1240 } : {}),
	};
	const deterministic = String(args?.content).startsWith("Reply");
	return {
		verdict: deterministic ? "deterministic" : "nondeterministic",
		reason: deterministic ? "" : "needs judgment",
		will_precompile: deterministic && args?.precompile === true,
		estimate:
			deterministic && args?.precompile === true
				? { available: true, min_tokens: 0, max_tokens: 0 }
				: model,
		...(deterministic && args?.precompile === true
			? { without_precompile: model }
			: {}),
	};
}

/**
 * Mocks one agent with the Slack channel "Team Slack" and `tasks`, answering
 * task_analyze with `analyze`.
 */
async function mockWorkflows(
	page: Page,
	tasks: Task[],
	jobs: Record<string, unknown>[] = [],
	analyze: (args: Record<string, unknown>) => unknown = workflowAnalysis,
) {
	const config = {
		agents: [
			{
				name: "assistant",
				model: "x/y",
				channels: [
					{
						type: "slack",
						id: "team-slack",
						name: "Team Slack",
						deliver_to: "C1",
					},
				],
				// Copies, so one test's changes never leak into the next.
				tasks: tasks.map((t) => ({ ...t })),
			},
		],
	};
	const calls: Record<string, Record<string, unknown>[]> = {};
	/** Tool failures to report, by tool name. */
	const failures: Record<string, string> = {};
	const record = (tool: string, args?: Record<string, unknown>) => {
		calls[tool] ??= [];
		calls[tool].push(args ?? {});
	};
	const find = (id: unknown) =>
		config.agents[0].tasks.find(
			(t) => `assistant/${t.name}` === String(id) || t.name === String(id),
		);
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: [
			{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
		],
		config_get: () => config,
		config_validate: [],
		auth_list: [],
		tool_list: [],
		session_list: [
			{ id: "main", agent_id: "assistant", name: "main", created_at: NOW },
		],
		channel_sessions: [
			{
				id: "channel:slack:C1",
				agent_id: "assistant",
				name: "slack:C1",
				created_at: NOW,
				own: false,
			},
		],
		session_messages: (args?: Record<string, unknown>) =>
			args?.session_id === "channel:slack:C1"
				? [
						{
							id: "m1",
							role: "user",
							content: "the app crashed",
							timestamp: NOW,
							workflows: [
								{
									task_id: "assistant/triage",
									name: "triage",
									job_id: "job-1",
								},
							],
						},
					]
				: [],
		workflow_entries: [{ entry: "C1", title: "#general" }],
		workflow_preview: (args?: Record<string, unknown>) => {
			record("workflow_preview", args);
			const pattern = String(args?.match ?? "")
				.replace(/[.+^${}()|[\]\\]/g, "\\$&")
				.replace(/\*/g, ".*")
				.replace(/\?/g, ".");
			const re = new RegExp(`^${pattern || ".*"}$`, "i");
			return ["the app crashed", "lunch?"].map((text, i) => ({
				entry: "C1",
				sender: i ? "Bob" : "Alice",
				text,
				timestamp: NOW,
				matches: re.test(text),
			}));
		},
		task_analyze: (args?: Record<string, unknown>) => {
			record("task_analyze", args);
			return analyze(args ?? {});
		},
		task_compile_query: (args?: Record<string, unknown>) =>
			args?.task === "triage"
				? [
						{
							id: "c2",
							agent_id: "assistant",
							task_name: "triage",
							status: "succeeded",
							created_at: NOW,
							updated_at: NOW,
						},
						{
							id: "c1",
							agent_id: "assistant",
							task_name: "triage",
							status: "skipped",
							created_at: AGO,
							updated_at: AGO,
						},
					]
				: [],
		job_query: jobs,
		job_list: (args?: Record<string, unknown>) =>
			jobs.filter((j) => j.task_id === args?.task),
		job_logs: "status: working",
		task_session: { id: "task-session", agent_id: "assistant" },
		task_list: () =>
			config.agents[0].tasks.map((t) => ({
				id: `assistant/${t.name}`,
				agent_name: "assistant",
				name: t.name,
				enabled: t.enabled !== false,
				trigger_type: "message",
				description: `messages on ${t.channel}`,
				history: [],
			})),
		task_schedule: (args?: Record<string, unknown>) => {
			record("task_schedule", args);
			config.agents[0].tasks.push({
				name: String(args?.name),
				trigger: "message",
				channel: String(args?.channel),
				entry: String(args?.entry ?? ""),
				match: String(args?.match ?? ""),
				prompt: String(args?.content),
				target: String(args?.target ?? ""),
				...(typeof args?.precompile === "boolean"
					? { precompile: args.precompile }
					: {}),
			});
			return "created";
		},
		task_update: (args?: Record<string, unknown>) => {
			record("task_update", args);
			const t = find(args?.name);
			if (t && typeof args?.enabled === "boolean") t.enabled = args.enabled;
			if (t && typeof args?.match === "string") t.match = args.match;
			if (t && typeof args?.precompile === "boolean")
				t.precompile = args.precompile;
			return "updated";
		},
		task_stop: (args?: Record<string, unknown>) => {
			record("task_stop", args);
			return "stopped";
		},
		task_delete: (args?: Record<string, unknown>) => {
			record("task_delete", args);
			if (failures.task_delete) throw new Error(failures.task_delete);
			config.agents[0].tasks = config.agents[0].tasks.filter(
				(t) => t !== find(args?.name),
			);
			return "deleted";
		},
	});
	return { config, calls, failures };
}

test("the workflow editor shows a cached analysis on open and re-analyzes only on change or Re-check", async ({
	page,
	browser,
}) => {
	const cache = analysisCache(workflowAnalysis);
	const openTriage = async (p: Page) => {
		await p.goto(
			"/agents/assistant/channels/Team%20Slack/settings?tab=workflows",
		);
		await p
			.locator("[data-testid=workflow-row][data-workflow=triage]")
			.getByTestId("workflow-edit")
			.click();
		return p.getByTestId("workflow-form").getByTestId("task-cost-feedback");
	};
	// Only the workflow's own analyses count; the agent settings page
	// behind the tab analyzes its own selected task.
	const triageAnalyses = () =>
		cache.analyses.filter((a) => a.trigger === "message").length;

	// The first open has nothing cached and analyzes once.
	await mockWorkflows(page, [TRIAGE], [], (a) => cache.handler(a));
	let feedback = await openTriage(page);
	await expect(feedback.getByTestId("task-cost-verdict")).toHaveText(
		"Not deterministic",
	);
	expect(triageAnalyses()).toBe(1);

	// Reopening after a reload, or in another browser, uses the cache.
	await watchForChecking(
		page,
		"[data-testid=workflow-form] [data-testid=task-cost-feedback]",
	);
	await page.reload();
	feedback = await openTriage(page);
	await expect(feedback.getByTestId("task-cost-cached")).toHaveText(
		/^Cached · checked (just now|\d+s ago)$/,
	);
	await expect(feedback.getByTestId("task-cost-estimate")).toHaveText(
		"≈ 5k–9k tokens/run ($0.010–$0.030)",
	);
	expect(triageAnalyses()).toBe(1);
	expect(await sawChecking(page)).toBe(false);

	const other = await browser.newContext();
	try {
		const otherPage = await other.newPage();
		await mockWorkflows(otherPage, [TRIAGE], [], (a) => cache.handler(a));
		await expect(
			(await openTriage(otherPage)).getByTestId("task-cost-cached"),
		).toBeVisible();
		expect(triageAnalyses()).toBe(1);
	} finally {
		await other.close();
	}

	// Editing analyzes once; reverting shows the cached result again.
	const content = page.getByTestId("workflow-content");
	await content.fill("Reply with the crash ID");
	await expect(feedback.getByTestId("task-cost-verdict")).toHaveText(
		"This task can run deterministically",
	);
	expect(triageAnalyses()).toBe(2);
	await content.fill("Triage the crash.");
	await expect(feedback.getByTestId("task-cost-verdict")).toHaveText(
		"Not deterministic",
	);
	await expect(feedback.getByTestId("task-cost-cached")).toBeVisible();
	await page.waitForTimeout(1500);
	expect(triageAnalyses()).toBe(2);

	// Re-check bypasses the cache.
	await feedback.getByTestId("task-cost-recheck").click();
	await expect.poll(triageAnalyses).toBe(3);
	expect(cache.analyses.at(-1)).toMatchObject({ force: true });
	await expect(feedback.getByTestId("task-cost-cached")).toHaveCount(0);

	// A compiler version bump invalidates the cache, so reopening analyzes.
	cache.invalidate();
	await page.reload();
	feedback = await openTriage(page);
	await expect(feedback.getByTestId("task-cost-verdict")).toHaveText(
		"Not deterministic",
	);
	expect(triageAnalyses()).toBe(4);
});
