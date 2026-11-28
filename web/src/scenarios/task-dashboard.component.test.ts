import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { nextTick } from "vue";
import { currentURL, goto, reload, renderApp } from "../component-test/app";
import { mockMCP } from "../component-test/server";
import {
	TASK_CHAT_DEFAULT_HEIGHT,
	useTaskChatPanel,
} from "../composables/useTaskChatPanel";

// A task's dashboard: its schedule, live run log, run history and chat
// panel. Each test starts with the default layout (chat on, default height),
// as the spec's init script made it.

interface MockJob {
	id: string;
	task_id: string;
	agent_id: string;
	prompt: string;
	status: "pending" | "in_progress" | "completed" | "failed" | "canceled";
	attempts: number;
	max_retries: number;
	task_type?: string;
	trigger?: string;
	error?: string;
	error_output?: string;
	bug_report_url?: string;
	output?: string;
	script_error?: string;
	created_at: string;
	updated_at: string;
}

const TASK_ID = "assistant/report";

function minutesAgo(m: number): string {
	return new Date(Date.now() - m * 60_000).toISOString();
}

function job(
	id: string,
	status: MockJob["status"],
	startedMinutesAgo: number,
	extra: Partial<MockJob> = {},
): MockJob {
	return {
		id,
		task_id: TASK_ID,
		agent_id: "assistant",
		prompt: "Report",
		status,
		attempts: 1,
		max_retries: 0,
		trigger: "schedule",
		created_at: minutesAgo(startedMinutesAgo),
		updated_at: minutesAgo(startedMinutesAgo - 1),
		...extra,
	};
}

/**
 * Mocks one agent with a "report" task whose runs are `jobs`, which the
 * tests may change, and records the actions the page takes.
 */
function mockTask(
	jobs: MockJob[],
	opts: { enabled?: boolean; history?: object[]; task?: object } = {},
) {
	const state: { jobs: MockJob[]; enabled: boolean; task?: object } = {
		jobs,
		enabled: opts.enabled ?? true,
		task: opts.task,
	};
	const calls: { tool: string; args: Record<string, unknown> }[] = [];
	const logs = new Map<string, string>();
	const record = (tool: string, args?: Record<string, unknown>) =>
		calls.push({ tool, args: args ?? {} });
	mockMCP({
		agent_list: [
			{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
		],
		config_get: () => ({
			agents: [
				{
					name: "assistant",
					model: "x/y",
					tasks: [
						{
							name: "report",
							every: "15m",
							prompt: "Report",
							enabled: state.enabled,
							...state.task,
						},
					],
				},
			],
		}),
		config_validate: [],
		task_list: () => [
			{
				id: TASK_ID,
				agent_name: "assistant",
				name: "report",
				enabled: state.enabled,
				description: "every 15m0s (Local)",
				next_run: new Date(Date.now() + 14 * 60_000 + 5_000).toISOString(),
				history: opts.history ?? [],
			},
		],
		task_session: (args?: Record<string, unknown>) => ({
			id: "task-report",
			agent_id: "assistant",
			name: String(args?.name),
		}),
		session_messages: [],
		session_list: [],
		auth_list: [],
		tool_list: [],
		job_list: (args?: Record<string, unknown>) => {
			record("job_list", args);
			return state.jobs;
		},
		job_logs: (args?: Record<string, unknown>) =>
			logs.get(String(args?.id)) ?? `logs of ${String(args?.id)}`,
		job_stop: (args?: Record<string, unknown>) => {
			record("job_stop", args);
			for (const j of state.jobs) if (j.id === args?.id) j.status = "canceled";
			return "stopped";
		},
		task_run: (args?: Record<string, unknown>) => {
			record("task_run", args);
			const started = job(`run-${state.jobs.length}`, "in_progress", 0, {
				trigger: "manual",
			});
			state.jobs = [started, ...state.jobs];
			return started;
		},
		config_task_convert_to_script: (args?: Record<string, unknown>) => {
			record("config_task_convert_to_script", args);
			state.task = {
				...state.task,
				precompile: true,
				script_failures: undefined,
				script_last_error: undefined,
				compiled: 'print("fresh")',
				compiled_key: "fresh",
			};
			return `recompiling task "${String(args?.task)}"`;
		},
		task_update: (args?: Record<string, unknown>) => {
			record("task_update", args);
			state.enabled = Boolean(args?.enabled);
			return "updated";
		},
	});
	return { state, calls, logs };
}

const count = (loc: Locator) => loc.elements().length;
const box = (loc: Locator) => loc.element().getBoundingClientRect();
const heightOf = (loc: Locator) =>
	loc.elements()[0]?.getBoundingClientRect().height;

beforeEach(async () => {
	// The chat layout lives in module state, which outlasts a test, so reset
	// it there as well as in storage.
	const chat = useTaskChatPanel();
	chat.open.value = true;
	chat.setHeight(TASK_CHAT_DEFAULT_HEIGHT);
	await nextTick();
	localStorage.clear();
});

afterEach(() => {
	vi.restoreAllMocks();
});

test("a task with no runs shows empty states and its schedule", async () => {
	const { calls } = mockTask([], { enabled: false });
	await renderApp("/agents/assistant/tasks/report");

	await expect.element(page.getByTestId("settings-title")).toHaveText("report");
	await expect.element(page.getByTestId("task-state")).toHaveText("Paused");
	await expect
		.element(page.getByTestId("task-schedule"))
		.toMatchTextContent("every 15m0s (Local) · paused");
	await expect.element(page.getByTestId("task-run-log-empty")).toBeVisible();
	await expect
		.element(page.getByTestId("task-run-history-empty"))
		.toMatchTextContent("No runs yet");
	expect(calls.find((c) => c.tool === "job_list")?.args).toEqual({
		task: TASK_ID,
	});

	await page.getByTestId("task-toggle-enabled").click();
	await expect.element(page.getByTestId("task-state")).toHaveText("Enabled");
	await expect
		.element(page.getByTestId("task-schedule"))
		.toMatchTextContent("every 15m0s (Local) · next in 14m");
	expect(calls.find((c) => c.tool === "task_update")?.args).toEqual({
		name: TASK_ID,
		enabled: true,
	});
});

test("a running task streams its logs, and copy and stop work", async () => {
	// The clipboard as the page writes it (the spec read it back with
	// clipboard permissions granted).
	let clipboard = "";
	vi.spyOn(navigator.clipboard, "writeText").mockImplementation(
		async (text) => {
			clipboard = text;
		},
	);
	const running = job("live", "in_progress", 2);
	const { state, calls, logs } = mockTask([
		running,
		job("done", "completed", 30),
	]);
	const lines = Array.from({ length: 80 }, (_, i) => `line ${i + 1}`);
	logs.set("live", lines.join("\n"));
	await renderApp("/agents/assistant/tasks/report");

	const log = page.getByTestId("task-run-log");
	await expect
		.element(log.getByTestId("task-run-log-title"))
		.toHaveText("Current run");
	await expect
		.element(log.getByTestId("task-run-result"))
		.toHaveText("Running");
	await expect
		.element(log.getByTestId("task-run-log-elapsed"))
		.toMatchTextContent("elapsed");
	const output = log.getByTestId("task-run-log-output");
	await expect.element(output).toMatchTextContent("line 80");
	// It follows the newest line.
	await expect
		.poll(() => {
			const el = output.element();
			return el.scrollHeight - el.scrollTop - el.clientHeight;
		})
		.toBeLessThan(30);

	// Scrolling up pauses following while new lines stream in.
	{
		const el = output.element();
		el.scrollTop = 0;
		el.dispatchEvent(new Event("scroll"));
	}
	await expect.element(log.getByTestId("task-run-log-follow")).toBeVisible();
	logs.set("live", [...lines, "line 81"].join("\n"));
	await expect.element(output).toMatchTextContent("line 81");
	expect(output.element().scrollTop).toBe(0);
	await log.getByTestId("task-run-log-follow").click();
	await expect
		.poll(() => count(log.getByTestId("task-run-log-follow")))
		.toBe(0);

	await log.getByTestId("task-run-log-copy").click();
	await expect.poll(() => clipboard).toContain("line 81");

	await log.getByTestId("task-run-log-stop").click();
	await expect
		.poll(() => calls.find((c) => c.tool === "job_stop")?.args)
		.toEqual({ id: "live" });
	expect(state.jobs[0].status).toBe("canceled");
	// With nothing running, the stopped run is the last run.
	await expect
		.element(log.getByTestId("task-run-log-title"))
		.toHaveText("Last run");
	await expect
		.element(log.getByTestId("task-run-result"))
		.toHaveText("Canceled");
});

test("an idle task shows its last run, and Run now starts one", async () => {
	const { calls } = mockTask([
		job("older", "completed", 60),
		job("newest", "failed", 20, { error: "exit status 1: boom" }),
	]);
	await renderApp("/agents/assistant/tasks/report");

	const log = page.getByTestId("task-run-log");
	await expect
		.element(log.getByTestId("task-run-log-title"))
		.toHaveText("Last run");
	await expect.element(log).toHaveAttribute("data-run", "newest");
	await expect.element(log.getByTestId("task-run-result")).toHaveText("Failed");
	await expect
		.element(log.getByTestId("task-run-log-error"))
		.toHaveText("exit status 1: boom");
	await expect
		.element(log.getByTestId("task-run-log-output"))
		.toHaveText("logs of newest");

	await page.getByTestId("task-run-now").click();
	await expect
		.poll(() => calls.find((c) => c.tool === "task_run")?.args)
		.toEqual({ name: TASK_ID });
	await expect
		.element(log.getByTestId("task-run-log-title"))
		.toHaveText("Current run");
	await expect.element(page.getByTestId("task-stop")).toBeVisible();
	await expect
		.element(
			page.getByTestId("task-run-row").first().getByTestId("task-run-trigger"),
		)
		.toHaveText("manual");
});

test("a failed last run shows a short error, its output, and a Report bug link", async () => {
	const report = "https://github.com/o/r/issues/new?body=%23%23+Context";
	const long = `agent failed: ${"x".repeat(600)}`;
	mockTask([
		job("newest", "failed", 20, {
			error: "agent process didn't exit cleanly: WaitDelay expired",
			error_output: "Scanning tests\nFiled issues",
			bug_report_url: report,
		}),
		job("older", "failed", 60, { error: long }),
	]);
	await renderApp("/agents/assistant/tasks/report");

	const log = page.getByTestId("task-run-log");
	await expect
		.element(log.getByTestId("task-run-log-title"))
		.toHaveText("Last run");
	expect(count(page.getByTestId("task-run-log-empty"))).toBe(0);
	await expect
		.element(log.getByTestId("task-run-log-error-message"))
		.toHaveText("agent process didn't exit cleanly: WaitDelay expired");
	await expect
		.element(log.getByTestId("task-run-log-error"))
		.not.toMatchTextContent("https://");
	await expect
		.element(log.getByTestId("task-run-log-report-bug"))
		.toHaveAttribute("href", report);
	const output = log.getByTestId("task-run-log-error-output");
	await output.locator("summary").click();
	await expect
		.element(output.locator("pre"))
		.toHaveText("Scanning tests\nFiled issues");

	const rows = page.getByTestId("task-run-row");
	await expect
		.element(rows.first().getByTestId("task-run-report-bug"))
		.toHaveAttribute("href", report);
	expect(count(rows.nth(1).getByTestId("task-run-report-bug"))).toBe(0);
	// A long error wraps and clamps inside its row instead of widening it.
	await expect.element(rows.nth(1).getByTestId("task-run-error")).toBeVisible();
	const row = box(rows.nth(1));
	const error = box(rows.nth(1).getByTestId("task-run-error"));
	expect(error.width <= row.width).toBe(true);
	expect(error.height < 60).toBe(true);
});

test("many runs paginate, and results filter from the chips and pills", async () => {
	const jobs: MockJob[] = [];
	for (let i = 0; i < 45; i++) {
		const failed = i % 3 === 0;
		jobs.push(
			job(`j${i}`, failed ? "failed" : "completed", 15 * (i + 1), {
				error: failed ? `failure ${i}` : undefined,
				trigger: i === 1 ? "missed" : "schedule",
			}),
		);
	}
	mockTask(jobs, {
		history: [
			{
				fire_time: minutesAgo(3),
				reason: "schedule",
				status: "skipped",
				summary: "previous run still active",
			},
		],
	});
	await renderApp("/agents/assistant/tasks/report");

	const rows = page.getByTestId("task-run-row");
	await expect.poll(() => count(rows)).toBe(20);
	// Newest first: the skipped fire, then j0.
	await expect.element(rows.first()).toHaveAttribute("data-result", "skipped");
	await expect
		.element(rows.first().getByTestId("task-run-error"))
		.toHaveText("previous run still active");
	await expect
		.element(rows.nth(1).getByTestId("task-run-error"))
		.toHaveText("failure 0");
	await expect
		.element(rows.nth(2).getByTestId("task-run-trigger"))
		.toHaveText("catch-up");
	await page.getByTestId("task-run-history-more").click();
	await expect.poll(() => count(rows)).toBe(40);
	await page.getByTestId("task-run-history-more").click();
	await expect.poll(() => count(rows)).toBe(46);
	await expect
		.poll(() => count(page.getByTestId("task-run-history-more")))
		.toBe(0);

	await page.getByTestId("task-run-filter-failed").click();
	await expect.poll(() => count(rows)).toBe(15);
	for (const el of rows.elements())
		expect(el.getAttribute("data-result")).toBe("failed");

	const chips = page.getByTestId("stat-chip");
	await chips.filter({ hasText: "succeeded" }).click();
	await expect
		.element(page.getByTestId("task-run-filter-succeeded"))
		.toHaveAttribute("aria-pressed", "true");
	await expect.poll(() => count(rows)).toBe(20);
	const rate = chips.filter({ hasText: /%\s*success$/ });
	await expect.element(rate).toMatchTextContent("67%");
	await rate.click();
	await expect
		.element(page.getByTestId("task-run-filter-all"))
		.toHaveAttribute("aria-pressed", "true");
	await expect
		.element(chips.filter({ hasText: "next run" }))
		.toMatchTextContent("in 14m");
});

test("a run opens at its own URL that survives a reload", async () => {
	mockTask([
		job("good", "completed", 10),
		job("bad", "failed", 40, { error: "connection refused" }),
	]);
	await renderApp("/agents/assistant/tasks/report");

	await page
		.getByTestId("task-run-row")
		.filter({ hasText: "Failed" })
		.getByTestId("task-run-open")
		.click();
	await expect
		.poll(currentURL)
		.toMatch(/\/agents\/assistant\/tasks\/report\/runs\/bad$/);
	const runPage = page.getByTestId("task-run-page");
	await expect
		.element(runPage.getByTestId("task-run-log-error"))
		.toHaveText("connection refused");
	await expect
		.element(runPage.getByTestId("task-run-log-output"))
		.toHaveText("logs of bad");
	// The sidebar still marks the task open.
	await expect.element(page.getByTestId("task-chat-panel")).toBeVisible();

	await reload();
	await expect
		.element(runPage.getByTestId("task-run-log-title"))
		.toHaveText("Run bad");
	expect(count(runPage.getByTestId("task-run-missing"))).toBe(0);
	await expect
		.element(runPage.getByTestId("task-run-log-error"))
		.toHaveText("connection refused");

	await page.getByTestId("settings-back").click();
	await expect.poll(currentURL).toMatch(/\/agents\/assistant\/tasks\/report$/);
	await expect.element(page.getByTestId("task-run-history")).toBeVisible();

	await goto("/agents/assistant/tasks/report/runs/nope");
	await expect.element(page.getByTestId("task-run-missing")).toBeVisible();
});

test("a project task's run opens at its URL without a missing-run note", async () => {
	mockTask(
		[
			job("job_20261124_004934_825653000", "completed", 10, {
				task_type: "project",
				trigger: "manual",
				output: "Triggered release.yml",
			}),
		],
		{ task: { type: "project", project: "o/r", precompile: true } },
	);
	await renderApp(
		"/agents/assistant/tasks/report/runs/job_20261124_004934_825653000",
	);

	const runPage = page.getByTestId("task-run-page");
	await expect
		.element(runPage.getByTestId("task-run-log-title"))
		.toHaveText("Run job_20261124_004934_825653000");
	expect(count(runPage.getByTestId("task-run-missing"))).toBe(0);
});

test("the chat panel toggles and resizes, and both persist", async () => {
	mockTask([]);
	await renderApp("/agents/assistant/tasks/report");

	const toggle = page.getByTestId("task-chat-toggle");
	const panel = page.getByTestId("task-chat-panel");
	await expect.element(toggle).toHaveAttribute("aria-pressed", "true");
	await expect.element(panel).toHaveAttribute("data-layout", "panel");
	await expect.element(panel.getByTestId("task-chat")).toBeVisible();
	expect(heightOf(panel)).toBe(320);

	// Dragging the top edge up makes the panel taller.
	const handle = page.getByTestId("task-chat-resize");
	const h = box(handle);
	const x = h.x + h.width / 2;
	const y = h.y + h.height / 2;
	await commands.mouseMove(x, y);
	await commands.mouseDown();
	await commands.mouseMove(x, y - 80, 4);
	await commands.mouseUp();
	await expect.poll(() => heightOf(panel)).toBe(400);

	// It stops at its bounds.
	(handle.element() as HTMLElement).focus();
	await commands.keyPress("Home");
	await expect.poll(() => heightOf(panel)).toBe(160);
	await commands.keyPress("ArrowUp");
	await expect.poll(() => heightOf(panel)).toBe(176);

	await reload();
	await expect.poll(() => heightOf(panel)).toBe(176);

	const dashboard = page.getByTestId("task-dashboard");
	const withChat = heightOf(dashboard) ?? 0;
	await toggle.click();
	await expect.poll(() => count(panel)).toBe(0);
	await expect.element(toggle).toHaveAttribute("aria-pressed", "false");
	await expect
		.poll(() => heightOf(dashboard) ?? 0)
		.toBeGreaterThan(withChat + 150);

	await reload();
	await expect.element(page.getByTestId("settings-title")).toHaveText("report");
	expect(count(panel)).toBe(0);
	await expect.element(toggle).toHaveAttribute("aria-pressed", "false");
});

test("on a phone the chat opens as a bottom sheet", async () => {
	await page.viewport(390, 844);
	mockTask([job("done", "completed", 30)]);
	await renderApp("/agents/assistant/tasks/report");

	const panel = page.getByTestId("task-chat-panel");
	await expect.element(panel).toHaveAttribute("data-layout", "sheet");
	await page.getByTestId("task-chat-close").click();
	await expect.poll(() => count(panel)).toBe(0);
	await expect.element(page.getByTestId("task-run-history")).toBeVisible();

	await page.getByTestId("task-chat-toggle").click();
	await expect.element(panel).toBeVisible();
	await expect
		.poll(() => {
			const b = box(panel);
			return Math.round(b.y + b.height);
		})
		.toBe(844);
	await page
		.getByTestId("task-chat-backdrop")
		.click({ position: { x: 10, y: 10 } });
	await expect.poll(() => count(panel)).toBe(0);
});

test("a precompiled project task's fallback runs are marked and counted", async () => {
	const { logs } = mockTask(
		[
			job("fallback", "completed", 5, {
				output:
					"Started the Release workflow because main moved past v0.7.4\n\n_Precompiled script failed (gh 404), fell back to the model; details in the run logs._",
				script_error:
					"<string>:52: failed to read main SHA: gh: Not Found (HTTP 404)\nstack traceback:\n    [G]: in function 'error'",
			}),
			job("script", "completed", 10),
			job("broken", "failed", 15, {
				error: "agent exited 1",
				script_error: "<string>:3: boom",
			}),
		],
		{
			task: {
				type: "project",
				project: "acme/app",
				precompile: true,
				compiled: 'print("release")',
				compiled_key: "k",
				script_failures: 2,
				script_last_error: "gh 404",
			},
		},
	);
	logs.set(
		"fallback",
		"Precompiled script failed, so the model ran the task:\n<string>:52: failed to read main SHA: gh: Not Found (HTTP 404)\nstack traceback:\n\nStarted the Release workflow",
	);
	await renderApp("/agents/assistant/tasks/report");

	await expect
		.element(page.getByTestId("task-script-failing"))
		.toMatchTextContent("failed its last 2 runs (last error: gh 404)");
	const log = page.getByTestId("task-run-log");
	await expect.element(log).toHaveAttribute("data-run", "fallback");
	const pill = log.getByTestId("task-run-result");
	await expect.element(pill).toHaveText("Succeeded via fallback");
	await expect.element(pill).toHaveAttribute("data-result", "fallback");
	await expect.element(pill).toHaveClass(/amber/);
	// The traceback is in the run's logs.
	await expect
		.element(log.getByTestId("task-run-log-output"))
		.toMatchTextContent("stack traceback");

	const chips = page.getByTestId("stat-chip");
	const fallbackChip = chips.filter({ hasText: /^1\s*via fallback$/ });
	await expect.poll(() => count(fallbackChip)).toBe(1);
	await expect
		.poll(() => count(chips.filter({ hasText: /^1\s*succeeded$/ })))
		.toBe(1);
	await expect
		.element(chips.filter({ hasText: "script failures" }))
		.toMatchTextContent("2");
	// A fallback that succeeded still counts toward the success rate.
	await expect
		.element(chips.filter({ hasText: /%\s*success$/ }))
		.toMatchTextContent("67%");

	await fallbackChip.click();
	const rows = page.getByTestId("task-run-row");
	await expect.poll(() => count(rows)).toBe(1);
	await expect.element(rows.first()).toHaveAttribute("data-result", "fallback");
});

test("a failing compiled script keeps precompile on and offers Recompile", async () => {
	const { calls } = mockTask([job("done", "completed", 30)], {
		task: {
			type: "project",
			project: "acme/app",
			precompile: true,
			compiled: 'print("release")',
			compiled_key: "k",
			script_verified: true,
			script_failures: 3,
			script_last_error: "gh 404",
		},
	});
	await renderApp("/agents/assistant/tasks/report");

	const badge = page.getByTestId("compiled-status-chip");
	await expect.element(badge).toHaveAttribute("data-state", "failing");
	await expect.element(badge).toMatchTextContent("Script failing");
	await expect
		.element(badge)
		.toHaveAttribute("title", expect.stringMatching(/last error: gh 404/));
	const banner = page.getByTestId("task-script-failing");
	await expect.element(banner).toMatchTextContent("failed its last 3 runs");
	await expect
		.element(banner.getByTestId("task-script-last-error"))
		.toHaveText("gh 404");
	await expect.element(banner).toMatchTextContent("Precompile stays on");
	expect(count(page.getByText(/turned off/i))).toBe(0);

	await banner.getByTestId("task-script-failing-recompile").click();
	await expect
		.poll(() => count(page.getByTestId("task-script-failing")))
		.toBe(0);
	await expect.element(badge).toHaveAttribute("data-state", "compiled");
	expect(
		calls.filter((c) => c.tool === "config_task_convert_to_script"),
	).toEqual([
		{
			tool: "config_task_convert_to_script",
			args: { agent: "assistant", task: "report" },
		},
	]);
});

test("a task that does not precompile has no Recompile action", async () => {
	mockTask([job("done", "completed", 30)]);
	await renderApp("/agents/assistant/tasks/report");
	await expect.element(page.getByTestId("settings-title")).toHaveText("report");
	expect(count(page.getByTestId("task-recompile"))).toBe(0);
});
