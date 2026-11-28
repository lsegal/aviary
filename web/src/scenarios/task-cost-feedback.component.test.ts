import { afterEach, expect, test } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { goto, renderApp } from "../component-test/app";
import { mockMCP } from "../component-test/server";
import { useAgentItemDialogs } from "../composables/useAgentItemDialogs";

// Fixtures follow task_analyze's result (internal/mcp/task_analyze.go).
//
// TaskCostFeedback keeps analyses in a module-level cache that outlives a
// test, so tests in this file never reuse another test's input with a
// different analyzer answer. The error-path scenarios that reuse
// "Check https://example.com" live in task-cost-feedback-errors, and the
// cached-analysis scenario, which needs a real page reload to drop that
// cache, stays in web/e2e/task-cost-feedback.spec.ts.

// The Add task dialog's open state is module state, which outlives a test's
// app; close it after each test, as a new page would start with it closed.
afterEach(() => {
	useAgentItemDialogs().close();
});

type Analysis = Record<string, unknown>;

const DETERMINISTIC: Analysis = {
	verdict: "deterministic",
	will_precompile: true,
	reason: "fixed URL check",
	estimate: { available: true, min_tokens: 0, max_tokens: 0 },
};

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

const UNAVAILABLE: Analysis = {
	verdict: "nondeterministic",
	will_precompile: false,
	reason: "summaries need judgment",
	estimate: {
		available: false,
		reason: "depends on the size of fetched data",
		min_tokens: 0,
		max_tokens: 0,
	},
};

interface Task {
	name: string;
	type?: string;
	prompt?: string;
	[key: string]: unknown;
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

/**
 * Mocks one agent with `tasks` and an analyzer answering through `analyze`,
 * recording every task_analyze call that analyzes. Cache lookups
 * (cached_only) are a miss.
 */
function mockAnalyzer(
	analyze: (args: Record<string, unknown>) => Analysis | Promise<Analysis>,
	tasks: Task[] = [],
) {
	const config = {
		agents: [{ name: "assistant", model: "x/y", tasks }],
	};
	const calls: Record<string, unknown>[] = [];
	mockMCP({
		agent_list: [
			{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
		],
		config_get: () => config,
		config_save: () => "ok",
		config_validate: [],
		config_task_convert_to_script: () => "task_compile_1",
		task_preview: (args?: Record<string, unknown>) => taskPreview(args ?? {}),
		task_analyze: (args?: Record<string, unknown>) => {
			if (args?.cached_only) return { miss: true };
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

async function openAddTask(): Promise<Locator> {
	await renderApp("/agents/assistant/tasks");
	const row = page
		.getByTestId("sidebar-sole-agent")
		.getByTestId("sidebar-row-tasks");
	await row.hover();
	await row.getByRole("button", { name: "Add task" }).click();
	const dialog = page.getByRole("dialog", { name: "Add task" });
	await expect.element(dialog).toBeVisible();
	return dialog;
}

function taskType(): Locator {
	return page
		.locator("select")
		.filter({ has: page.locator('option[value="script"]') });
}

test("typing analyzes the prompt once after a pause and shows a deterministic verdict", async () => {
	// A slow analyzer keeps Checking… on screen long enough to see.
	const calls = mockAnalyzer(
		() => new Promise<Analysis>((r) => setTimeout(() => r(DETERMINISTIC), 800)),
	);
	const dialog = await openAddTask();
	const feedback = dialog.getByTestId("task-cost-feedback");
	expect(feedback.elements()).toHaveLength(0);

	// pressSequentially with a 30ms delay between keys.
	await dialog.getByLabelText("Prompt").click();
	for (const ch of "Check https://example.com") {
		await commands.keyType(ch);
		await sleep(30);
	}
	await expect.element(feedback).toHaveAttribute("data-state", "checking");
	await expect.element(feedback).toMatchTextContent("Checking…");
	await expect.element(feedback).toHaveAttribute("data-state", "ready");
	await expect
		.element(feedback.getByTestId("task-cost-verdict"))
		.toHaveText("This task can run deterministically");
	await expect
		.element(feedback.getByTestId("task-cost-estimate"))
		.toHaveText("≈ 0 tokens/run");
	await expect.element(feedback).toMatchTextContent("✔");
	// No request per keystroke: one analysis of the final text.
	expect(calls).toHaveLength(1);
	expect(calls[0]).toMatchObject({
		agent: "assistant",
		content: "Check https://example.com",
		type: "prompt",
		precompile: true,
	});

	// Turning precompute off re-runs the check for the new input.
	const precompile = dialog.getByLabelText("Try to precompile");
	await expect.element(precompile).toBeChecked();
	await precompile.click();
	await expect.element(precompile).not.toBeChecked();
	await expect.poll(() => calls.length).toBe(2);
	expect(calls[1]).toMatchObject({ precompile: false });
});

test("a stale in-flight analysis never replaces a newer one, and analyzed text is cached", async () => {
	let releaseFirst: () => void = () => {};
	const firstDone = new Promise<void>((r) => {
		releaseFirst = r;
	});
	const calls = mockAnalyzer(async (args) => {
		if (args.content === "first prompt") {
			await firstDone;
			return NONDETERMINISTIC;
		}
		return PARTIAL;
	});
	const dialog = await openAddTask();
	const prompt = dialog.getByLabelText("Prompt");
	const feedback = dialog.getByTestId("task-cost-feedback");

	await prompt.fill("first prompt");
	await expect.poll(() => calls.length).toBe(1);
	await prompt.fill("second prompt");
	await expect
		.element(feedback.getByTestId("task-cost-verdict"))
		.toHaveText("Partially deterministic");
	releaseFirst();
	// The older result arrives late and is discarded.
	await sleep(300);
	await expect
		.element(feedback.getByTestId("task-cost-verdict"))
		.toHaveText("Partially deterministic");
	expect(calls).toHaveLength(2);

	// Re-entering analyzed text shows its verdict without a new request.
	await prompt.fill("first prompt");
	await expect
		.element(feedback.getByTestId("task-cost-verdict"))
		.toHaveText("Not deterministic");
	await prompt.fill("second prompt");
	await expect
		.element(feedback.getByTestId("task-cost-verdict"))
		.toHaveText("Partially deterministic");
	await sleep(1500);
	expect(calls).toHaveLength(2);
});

test("estimates that round to the same value show one value", async () => {
	mockAnalyzer(() => ({
		verdict: "nondeterministic",
		will_precompile: false,
		reason: "open-ended search",
		estimate: {
			available: true,
			min_tokens: 10100,
			max_tokens: 10400,
			min_cost_usd: 0.0301,
			max_cost_usd: 0.0304,
		},
	}));
	const dialog = await openAddTask();
	await dialog.getByLabelText("Prompt").fill("say hello");
	await expect
		.element(
			dialog
				.getByTestId("task-cost-feedback")
				.getByTestId("task-cost-estimate"),
		)
		.toHaveText("≈ 10k tokens/run ($0.030)");
});

test("each verdict shows its icon, text and token estimate", async () => {
	const verdicts: Record<string, Analysis> = {
		partial: PARTIAL,
		non: NONDETERMINISTIC,
		unbounded: UNAVAILABLE,
	};
	mockAnalyzer((args) => verdicts[String(args.content)]);
	const dialog = await openAddTask();
	const prompt = dialog.getByLabelText("Prompt");
	const feedback = dialog.getByTestId("task-cost-feedback");

	await prompt.fill("partial");
	await expect
		.element(feedback.getByTestId("task-cost-verdict"))
		.toHaveText("Partially deterministic");
	await expect.element(feedback).toMatchTextContent("◐");
	await expect
		.element(feedback.getByTestId("task-cost-estimate"))
		.toHaveText("≈ 4.6k–12k tokens/run ($0.014–$0.050)");
	await expect
		.element(feedback.getByTestId("task-cost-note"))
		.toHaveText(
			"Summarizing the results needs the model, so the task is not precompiled and runs through the model.",
		);

	await prompt.fill("non");
	await expect
		.element(feedback.getByTestId("task-cost-verdict"))
		.toHaveText("Not deterministic");
	await expect.element(feedback).toMatchTextContent("✖");
	await expect
		.element(feedback.getByTestId("task-cost-estimate"))
		.toHaveText("≈ 2.3k tokens/run");
	await expect
		.element(feedback.getByTestId("task-cost-note"))
		.toHaveText("Can't precompile: open-ended search");

	await prompt.fill("unbounded");
	await expect
		.element(feedback.getByTestId("task-cost-estimate"))
		.toHaveText("Estimate unavailable: depends on the size of fetched data");
});

test("the task editor shows script tasks without calling the analyzer", async () => {
	const calls = mockAnalyzer(
		() => DETERMINISTIC,
		[
			{ name: "lua-job", type: "script", prompt: "print('hi')" },
			{ name: "chatty", type: "prompt", prompt: "Say hello" },
		],
	);
	await renderApp("/agents/assistant/tasks?task=lua-job");
	const feedback = page.getByTestId("task-cost-feedback");
	await expect.element(feedback).toHaveAttribute("data-state", "script");
	await expect
		.element(feedback)
		.toMatchTextContent("Runs a script · 0 tokens/run");
	await sleep(1500);
	expect(calls).toHaveLength(0);

	// Changing the type re-runs the check.
	await taskType().selectOptions("prompt");
	await expect
		.element(feedback.getByTestId("task-cost-verdict"))
		.toHaveText("This task can run deterministically");
	expect(calls).toHaveLength(1);
	expect(calls[0]).toMatchObject({ type: "prompt", precompile: false });
});

test("the task editor analyzes a project task as its script runs, in the project's clone", async () => {
	const calls = mockAnalyzer(
		() => DETERMINISTIC,
		[
			{
				name: "stale",
				type: "project",
				project: "o/r",
				prompt: "Label stale issues",
				precompile: true,
			},
		],
	);
	await renderApp("/agents/assistant/tasks?task=stale");
	const feedback = page.getByTestId("task-cost-feedback");
	await expect
		.element(feedback.getByTestId("task-cost-verdict"))
		.toHaveText("This task can run deterministically");
	await expect
		.element(feedback.getByTestId("task-cost-estimate"))
		.toHaveText("≈ 0 tokens/run");
	await expect
		.element(feedback.getByTestId("task-cost-note"))
		.toMatchTextContent("runs in a clone of the project with no model calls");
	expect(calls).toHaveLength(1);
	expect(calls[0]).toMatchObject({
		type: "project",
		project: "o/r",
		precompile: true,
	});
});

test("a recurring trigger adds a monthly projection to the per-run estimate", async () => {
	mockAnalyzer(() => PARTIAL);
	const dialog = await openAddTask();
	const feedback = dialog.getByTestId("task-cost-feedback");
	const monthly = feedback.getByTestId("task-cost-monthly");
	const editor = dialog.getByTestId("trigger-editor");

	await dialog.getByLabelText("Prompt").fill("summarize the news");
	await expect
		.element(feedback.getByTestId("task-cost-estimate"))
		.toHaveText("≈ 4.6k–12k tokens/run ($0.014–$0.050)");
	// No trigger: nothing to project.
	expect(monthly.elements()).toHaveLength(0);

	await editor.getByRole("radio", { name: "Interval" }).click();
	await editor.getByLabelText("Every").fill("1h");
	await expect
		.element(monthly)
		.toHaveText("≈ 3.3M–8.9M tokens/month ($10.08–$36.00)");

	await editor.getByRole("radio", { name: "Daily" }).click();
	await editor.getByLabelText("At", { exact: true }).fill("09:00");
	await expect
		.element(monthly)
		.toHaveText("≈ 138k–372k tokens/month ($0.420–$1.50)");

	await editor.getByRole("radio", { name: "Cron" }).click();
	await editor.getByLabelText("Cron expression").fill("0 * * * *");
	await expect
		.element(monthly)
		.toHaveText("≈ 3.3M–8.9M tokens/month ($10.08–$36.00)");

	// A one-time task has no monthly rate.
	await editor.getByRole("radio", { name: "Once" }).click();
	await expect.poll(() => monthly.elements().length).toBe(0);
	await expect
		.element(feedback.getByTestId("task-cost-estimate"))
		.toBeVisible();
});

test("the monthly projection is omitted for 0-token and unavailable estimates, and has no USD without pricing", async () => {
	const verdicts: Record<string, Analysis> = {
		check: DETERMINISTIC,
		unbounded: UNAVAILABLE,
		non: NONDETERMINISTIC,
	};
	mockAnalyzer((args) => verdicts[String(args.content)]);
	const dialog = await openAddTask();
	const prompt = dialog.getByLabelText("Prompt");
	const feedback = dialog.getByTestId("task-cost-feedback");
	const monthly = feedback.getByTestId("task-cost-monthly");
	const editor = dialog.getByTestId("trigger-editor");
	await editor.getByRole("radio", { name: "Interval" }).click();
	await editor.getByLabelText("Every").fill("1h");

	await prompt.fill("non");
	await expect.element(monthly).toHaveText("≈ 1.7M tokens/month");

	await prompt.fill("check");
	await expect
		.element(feedback.getByTestId("task-cost-verdict"))
		.toHaveText("This task can run deterministically");
	await expect.poll(() => monthly.elements().length).toBe(0);

	await prompt.fill("unbounded");
	await expect
		.element(feedback.getByTestId("task-cost-estimate"))
		.toHaveText("Estimate unavailable: depends on the size of fetched data");
	await expect.poll(() => monthly.elements().length).toBe(0);
});

test("the settings task editor projects a saved task's trigger", async () => {
	mockAnalyzer(
		() => PARTIAL,
		[
			{ name: "hourly", prompt: "summarize the news", every: "1h" },
			{ name: "manual", prompt: "summarize the news" },
		],
	);
	await renderApp("/agents/assistant/tasks?task=hourly");
	const feedback = page.getByTestId("task-cost-feedback");
	await expect
		.element(feedback.getByTestId("task-cost-monthly"))
		.toHaveText("≈ 3.3M–8.9M tokens/month ($10.08–$36.00)");

	await goto("/agents/assistant/tasks?task=manual");
	await expect
		.element(feedback.getByTestId("task-cost-estimate"))
		.toHaveText("≈ 4.6k–12k tokens/run ($0.014–$0.050)");
	await expect
		.poll(() => feedback.getByTestId("task-cost-monthly").elements().length)
		.toBe(0);
});
