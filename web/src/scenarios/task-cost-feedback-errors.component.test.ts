import { expect, test } from "vitest";
import { type Locator, page } from "vitest/browser";
import { renderApp } from "../component-test/app";
import { mockMCP } from "../component-test/server";

// Fixtures follow task_analyze's result (internal/mcp/task_analyze.go).
//
// The error and in-flight scenarios of task-cost-feedback, which all type
// "Check https://example.com". TaskCostFeedback keeps analyses in a
// module-level cache that outlives a test, so they get their own page, and
// run in an order where no test sees an analysis an earlier one cached:
// only the last one ever answers that input successfully.

type Analysis = Record<string, unknown>;

const DETERMINISTIC: Analysis = {
	verdict: "deterministic",
	will_precompile: true,
	reason: "fixed URL check",
	estimate: { available: true, min_tokens: 0, max_tokens: 0 },
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

test("a superseded analysis does not show the error state", async () => {
	mockAnalyzer(async (args) => {
		if (args.content === "Check https://example.com") {
			await new Promise((resolve) => setTimeout(resolve, 1500));
			throw new Error("analysis superseded by a newer request");
		}
		return DETERMINISTIC;
	});
	const dialog = await openAddTask();
	await dialog.getByLabelText("Task name").fill("uptime");
	const prompt = dialog.getByLabelText("Prompt");
	await prompt.fill("Check https://example.com");
	const feedback = dialog.getByTestId("task-cost-feedback");
	await sleep(1400);
	await prompt.fill("Check https://example.com/status");
	await expect
		.element(feedback.getByTestId("task-cost-verdict"))
		.toHaveText("This task can run deterministically");
	await sleep(500);
	await expect.element(feedback).toHaveAttribute("data-state", "ready");
});

test("saving is not blocked by an analysis in flight", async () => {
	const calls = mockAnalyzer(() => new Promise<Analysis>(() => {}));
	const dialog = await openAddTask();
	await dialog.getByLabelText("Task name").fill("pending");
	await dialog.getByLabelText("Prompt").fill("Check https://example.com");
	await expect
		.element(dialog.getByTestId("task-cost-feedback"))
		.toHaveAttribute("data-state", "checking");
	await expect.poll(() => calls.length).toBe(1);
	await dialog.getByRole("button", { name: "Create" }).click();
	await expect.element(dialog).not.toBeInTheDocument();
});

test("an analyzer failure shows Couldn't analyze, and Retry re-runs the check", async () => {
	let fail = true;
	const calls = mockAnalyzer(() => {
		if (fail) throw new Error("provider unavailable");
		return DETERMINISTIC;
	});
	const dialog = await openAddTask();
	await dialog.getByLabelText("Task name").fill("uptime");
	await dialog.getByLabelText("Prompt").fill("Check https://example.com");
	const feedback = dialog.getByTestId("task-cost-feedback");
	await expect.element(feedback).toHaveAttribute("data-state", "error");
	await expect
		.element(feedback.getByTestId("task-cost-verdict"))
		.toHaveText("Couldn't analyze this task.");
	await expect
		.element(feedback.getByTestId("task-cost-error"))
		.toHaveText("provider unavailable");
	const retry = feedback.getByRole("button", { name: "Retry analysis" });
	await expect.element(retry).toBeVisible();
	// The message and the Retry control are separate, so they never read as
	// one phrase such as "Couldn't analyze Retry".
	await expect.element(feedback).not.toHaveTextContent(/analyze\s*Retry/i);
	expect(feedback.locator("p").filter({ has: retry }).elements()).toHaveLength(
		0,
	);

	fail = false;
	await feedback.getByTestId("task-cost-retry").click();
	await expect
		.element(feedback.getByTestId("task-cost-verdict"))
		.toHaveText("This task can run deterministically");
	expect(calls).toHaveLength(2);
	expect(calls[1]).toMatchObject({ content: "Check https://example.com" });
});
