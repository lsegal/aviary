import { expect, type Locator, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

// Fixtures follow task_analyze's result (internal/mcp/task_analyze.go).

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

/**
 * Mocks one agent with `tasks` and an analyzer answering through `analyze`,
 * recording every task_analyze call.
 */
async function mockAnalyzer(
	page: Page,
	analyze: (args: Record<string, unknown>) => Analysis | Promise<Analysis>,
	tasks: Task[] = [],
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
		task_preview: () => ({ description: "preview", next: [] }),
		task_analyze: (args?: Record<string, unknown>) => {
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

async function openAddTask(page: Page): Promise<Locator> {
	await page.goto("/agents/assistant/tasks");
	const row = page
		.getByTestId("sidebar-sole-agent")
		.getByTestId("sidebar-row-tasks");
	await row.hover();
	await row.getByRole("button", { name: "Add task" }).click();
	const dialog = page.getByRole("dialog", { name: "Add task" });
	await expect(dialog).toBeVisible();
	return dialog;
}

test("typing analyzes the prompt once after a pause and shows a deterministic verdict", async ({
	page,
}) => {
	// A slow analyzer keeps Checking… on screen long enough to see.
	const calls = await mockAnalyzer(
		page,
		() => new Promise<Analysis>((r) => setTimeout(() => r(DETERMINISTIC), 800)),
	);
	const dialog = await openAddTask(page);
	const feedback = dialog.getByTestId("task-cost-feedback");
	await expect(feedback).toHaveCount(0);

	await dialog
		.getByLabel("Prompt")
		.pressSequentially("Check https://example.com", { delay: 30 });
	await expect(feedback).toHaveAttribute("data-state", "checking");
	await expect(feedback).toContainText("Checking…");
	await expect(feedback).toHaveAttribute("data-state", "ready");
	await expect(feedback.getByTestId("task-cost-verdict")).toHaveText(
		"This task can run deterministically",
	);
	await expect(feedback.getByTestId("task-cost-estimate")).toHaveText(
		"≈ 0 tokens/run",
	);
	await expect(feedback).toContainText("✔");
	// No request per keystroke: one analysis of the final text.
	expect(calls).toHaveLength(1);
	expect(calls[0]).toMatchObject({
		agent: "assistant",
		content: "Check https://example.com",
		type: "prompt",
		precompile: true,
	});

	// Turning precompute off re-runs the check for the new input.
	await dialog.getByLabel("Try to precompute task").uncheck();
	await expect.poll(() => calls.length).toBe(2);
	expect(calls[1]).toMatchObject({ precompile: false });
});

test("a stale in-flight analysis never replaces a newer one, and analyzed text is cached", async ({
	page,
}) => {
	let releaseFirst: () => void = () => {};
	const firstDone = new Promise<void>((r) => {
		releaseFirst = r;
	});
	const calls = await mockAnalyzer(page, async (args) => {
		if (args.content === "first prompt") {
			await firstDone;
			return NONDETERMINISTIC;
		}
		return PARTIAL;
	});
	const dialog = await openAddTask(page);
	const prompt = dialog.getByLabel("Prompt");
	const feedback = dialog.getByTestId("task-cost-feedback");

	await prompt.fill("first prompt");
	await expect.poll(() => calls.length).toBe(1);
	await prompt.fill("second prompt");
	await expect(feedback.getByTestId("task-cost-verdict")).toHaveText(
		"Partially deterministic",
	);
	releaseFirst();
	// The older result arrives late and is discarded.
	await page.waitForTimeout(300);
	await expect(feedback.getByTestId("task-cost-verdict")).toHaveText(
		"Partially deterministic",
	);
	expect(calls).toHaveLength(2);

	// Re-entering analyzed text shows its verdict without a new request.
	await prompt.fill("first prompt");
	await expect(feedback.getByTestId("task-cost-verdict")).toHaveText(
		"Not deterministic",
	);
	await prompt.fill("second prompt");
	await expect(feedback.getByTestId("task-cost-verdict")).toHaveText(
		"Partially deterministic",
	);
	await page.waitForTimeout(1500);
	expect(calls).toHaveLength(2);
});

test("each verdict shows its icon, text and token estimate", async ({
	page,
}) => {
	const verdicts: Record<string, Analysis> = {
		partial: PARTIAL,
		non: NONDETERMINISTIC,
		unbounded: UNAVAILABLE,
	};
	await mockAnalyzer(page, (args) => verdicts[String(args.content)]);
	const dialog = await openAddTask(page);
	const prompt = dialog.getByLabel("Prompt");
	const feedback = dialog.getByTestId("task-cost-feedback");

	await prompt.fill("partial");
	await expect(feedback.getByTestId("task-cost-verdict")).toHaveText(
		"Partially deterministic",
	);
	await expect(feedback).toContainText("◐");
	await expect(feedback.getByTestId("task-cost-estimate")).toHaveText(
		"≈ 4.6k–12k tokens/run ($0.014–$0.050)",
	);
	await expect(feedback.getByTestId("task-cost-note")).toHaveText(
		"Summarizing the results needs the model, so the task is not precompiled and runs through the model.",
	);

	await prompt.fill("non");
	await expect(feedback.getByTestId("task-cost-verdict")).toHaveText(
		"Not deterministic",
	);
	await expect(feedback).toContainText("✖");
	await expect(feedback.getByTestId("task-cost-estimate")).toHaveText(
		"≈ 2.3k tokens/run",
	);
	await expect(feedback.getByTestId("task-cost-note")).toHaveText(
		"Won't be precompiled: open-ended search",
	);

	await prompt.fill("unbounded");
	await expect(feedback.getByTestId("task-cost-estimate")).toHaveText(
		"Estimate unavailable: depends on the size of fetched data",
	);
});

test("an analyzer failure shows Couldn't analyze, and Retry re-runs the check", async ({
	page,
}) => {
	let fail = true;
	const calls = await mockAnalyzer(page, () => {
		if (fail) throw new Error("provider unavailable");
		return DETERMINISTIC;
	});
	const dialog = await openAddTask(page);
	await dialog.getByLabel("Task name").fill("uptime");
	await dialog.getByLabel("Prompt").fill("Check https://example.com");
	const feedback = dialog.getByTestId("task-cost-feedback");
	await expect(feedback).toHaveAttribute("data-state", "error");
	await expect(feedback.getByTestId("task-cost-verdict")).toHaveText(
		"Couldn't analyze",
	);

	fail = false;
	await feedback.getByTestId("task-cost-retry").click();
	await expect(feedback.getByTestId("task-cost-verdict")).toHaveText(
		"This task can run deterministically",
	);
	expect(calls).toHaveLength(2);
	expect(calls[1]).toMatchObject({ content: "Check https://example.com" });
});

test("saving is not blocked by an analysis in flight", async ({ page }) => {
	const calls = await mockAnalyzer(page, () => new Promise<Analysis>(() => {}));
	const dialog = await openAddTask(page);
	await dialog.getByLabel("Task name").fill("pending");
	await dialog.getByLabel("Prompt").fill("Check https://example.com");
	await expect(dialog.getByTestId("task-cost-feedback")).toHaveAttribute(
		"data-state",
		"checking",
	);
	await expect.poll(() => calls.length).toBe(1);
	await dialog.getByRole("button", { name: "Create" }).click();
	await expect(dialog).toBeHidden();
});

test("the task editor shows script and project tasks without calling the analyzer", async ({
	page,
}) => {
	const calls = await mockAnalyzer(page, () => DETERMINISTIC, [
		{ name: "lua-job", type: "script", prompt: "print('hi')" },
		{ name: "chatty", type: "prompt", prompt: "Say hello" },
	]);
	await page.goto("/agents/assistant/tasks?task=lua-job");
	const feedback = page.getByTestId("task-cost-feedback");
	await expect(feedback).toHaveAttribute("data-state", "script");
	await expect(feedback).toContainText("Runs a script · 0 tokens/run");
	await page.waitForTimeout(1500);
	expect(calls).toHaveLength(0);

	// Changing the type re-runs the check.
	await taskType(page).selectOption("project");
	await expect(feedback).toHaveAttribute("data-state", "project");
	await expect(feedback.getByTestId("task-cost-verdict")).toHaveText(
		"Project tasks are never precomputed",
	);
	await taskType(page).selectOption("prompt");
	await expect(feedback.getByTestId("task-cost-verdict")).toHaveText(
		"This task can run deterministically",
	);
	expect(calls).toHaveLength(1);
	expect(calls[0]).toMatchObject({ type: "prompt", precompile: false });
});

function taskType(page: Page): Locator {
	return page
		.locator("select")
		.filter({ has: page.locator('option[value="script"]') });
}
