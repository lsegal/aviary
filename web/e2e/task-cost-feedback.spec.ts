import { expect, type Page, test } from "@playwright/test";
import {
	analysisCache,
	sawChecking,
	watchForChecking,
} from "./helpers/analysisCache";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

// The other task cost feedback scenarios are component tests now
// (web/src/scenarios/task-cost-feedback*.component.test.ts). This one stays
// here because it needs real page reloads and a second browser: the editor
// keeps analyses in a module-level cache that only a new page drops, and the
// scenario checks that reopening after a reload, in another browser, and
// after the server's cache is invalidated goes to the server's cache.

// Fixtures follow task_analyze's result (internal/mcp/task_analyze.go).

type Analysis = Record<string, unknown>;

const PARTIAL: Analysis = {
	verdict: "partial",
	will_precompile: false,
	reason: "summarizing needs judgment",
	model_steps: ["summarizing the results"],
	estimate: {
		available: true,
		min_tokens: 4600,
		max_tokens: 12400,
		min_cost_usd: 0.014,
		max_cost_usd: 0.05,
	},
};

const NONDETERMINISTIC: Analysis = {
	verdict: "nondeterministic",
	will_precompile: false,
	reason: "open-ended search",
	estimate: { available: true, min_tokens: 2300, max_tokens: 2300 },
};

interface Task {
	name: string;
	type?: string;
	prompt?: string;
	[key: string]: unknown;
}

/**
 * Mocks one agent with `tasks` and an analyzer answering through `analyze`,
 * recording every task_analyze call that analyzes. Cache lookups
 * (cached_only) are answered by `lookup`, a miss by default.
 */
async function mockAnalyzer(
	page: Page,
	analyze: (args: Record<string, unknown>) => Analysis | Promise<Analysis>,
	tasks: Task[] = [],
	lookup: (args: Record<string, unknown>) => Analysis = () => ({
		miss: true,
	}),
) {
	const config = {
		agents: [{ name: "assistant", model: "x/y", tasks }],
	};
	const calls: Record<string, unknown>[] = [];
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: [
			{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
		],
		config_get: () => config,
		config_save: () => "ok",
		config_validate: [],
		config_task_convert_to_script: () => "task_compile_1",
		task_preview: (args?: Record<string, unknown>) => taskPreview(args ?? {}),
		task_analyze: (args?: Record<string, unknown>) => {
			if (args?.cached_only) return lookup(args);
			calls.push(args ?? {});
			return analyze(args ?? {});
		},
		job_list: [],
		session_list: [],
		auth_list: [],
		tool_list: [],
		agent_file_list: [],
	});
	return calls;
}

/**
 * task_preview's answer for args, with runs_per_30_days as the server gives
 * it: hourly interval and cron triggers fire 720 times, daily ones 30 times,
 * and one-time triggers have no rate.
 */
function taskPreview(args: Record<string, unknown>) {
	const out = { description: "preview", next: [] as string[] };
	if (args.every === "1h" || args.schedule === "0 * * * *")
		return { ...out, runs_per_30_days: 720 };
	if (args.trigger === "daily" && args.at)
		return { ...out, runs_per_30_days: 30 };
	return out;
}

test("the task editor shows a cached analysis on open and re-analyzes only on change or Re-check", async ({
	page,
	browser,
}) => {
	const cache = analysisCache((args) =>
		args.content === "summarize the news" ? PARTIAL : NONDETERMINISTIC,
	);
	const tasks = [{ name: "manual", prompt: "summarize the news" }];
	// An earlier session analyzed the saved prompt two hours ago.
	cache.seed(
		{ agent: "assistant", content: "summarize the news", type: "prompt" },
		new Date(Date.now() - 2 * 3600_000).toISOString(),
	);
	const open = async (p: Page) => {
		await watchForChecking(p);
		await mockAnalyzer(
			p,
			(a) => cache.handler(a),
			tasks,
			(a) => cache.handler(a),
		);
		await p.goto("/agents/assistant/tasks?task=manual");
		const feedback = p.getByTestId("task-cost-feedback");
		await expect(feedback.getByTestId("task-cost-verdict")).toHaveText(
			"Partially deterministic",
		);
		await expect(feedback.getByTestId("task-cost-cached")).toHaveText(
			"Cached · checked 2h ago",
		);
		await expect(feedback.getByTestId("task-cost-estimate")).toHaveText(
			"≈ 4.6k–12k tokens/run ($0.014–$0.050)",
		);
		expect(await sawChecking(p)).toBe(false);
		return feedback;
	};

	// Opening, reloading, and another browser all use the cache.
	const feedback = await open(page);
	expect(cache.analyses).toHaveLength(0);
	await page.reload();
	await expect(feedback.getByTestId("task-cost-cached")).toHaveText(
		"Cached · checked 2h ago",
	);
	expect(await sawChecking(page)).toBe(false);
	const other = await browser.newContext();
	try {
		await open(await other.newPage());
	} finally {
		await other.close();
	}
	expect(cache.analyses).toHaveLength(0);

	// Editing analyzes once; reverting shows the cached result again.
	const prompt = page.getByLabel("Prompt");
	await prompt.fill("summarize the news today");
	await expect(feedback.getByTestId("task-cost-verdict")).toHaveText(
		"Not deterministic",
	);
	expect(cache.analyses).toHaveLength(1);
	await expect(feedback.getByTestId("task-cost-cached")).toHaveCount(0);
	await prompt.fill("summarize the news");
	await expect(feedback.getByTestId("task-cost-cached")).toHaveText(
		"Cached · checked 2h ago",
	);
	await page.waitForTimeout(1500);
	expect(cache.analyses).toHaveLength(1);

	// Re-check bypasses the cache and refreshes it.
	await feedback.getByTestId("task-cost-recheck").click();
	await expect.poll(() => cache.analyses.length).toBe(2);
	expect(cache.analyses[1]).toMatchObject({
		content: "summarize the news",
		force: true,
	});
	await expect(feedback.getByTestId("task-cost-cached")).toHaveCount(0);
	await page.reload();
	await expect(feedback.getByTestId("task-cost-cached")).toHaveText(
		/^Cached · checked (just now|\d+s ago)$/,
	);
	expect(cache.analyses).toHaveLength(2);

	// A compiler version bump invalidates the cache, so reopening analyzes.
	cache.invalidate();
	await page.reload();
	await expect(feedback.getByTestId("task-cost-verdict")).toHaveText(
		"Partially deterministic",
	);
	expect(cache.analyses).toHaveLength(3);
	await expect(feedback.getByTestId("task-cost-cached")).toHaveCount(0);
});
