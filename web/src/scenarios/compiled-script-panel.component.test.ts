import { beforeEach, expect, test } from "vitest";
import { page } from "vitest/browser";
import { currentURL, reload, renderApp } from "../component-test/app";
import { mockMCP } from "../component-test/server";
import { useCompiledPanelOpen } from "../composables/useTaskCompiles";
import { boundingBoxOf } from "./helpers/box";

interface Task {
	name: string;
	type?: string;
	prompt?: string;
	trigger?: string;
	channel?: string;
	match?: string;
	target?: string;
	project?: string;
	every?: string;
	precompile?: boolean;
	compiled?: string;
	compiled_key?: string;
	script_failures?: number;
	script_last_error?: string;
}

const NOW = new Date().toISOString();
const AGO = new Date(Date.now() - 3_600_000).toISOString();

const SCRIPT = [
	"-- reply with the crash ID",
	'local id = string.match(trigger.message.text, "crash (%d+)")',
	"if not id then",
	'  tool.task_no_reply({reason = "no crash ID"})',
	"  return",
	"end",
	'print("crash " .. id)',
].join("\n");

const COMPILED: Task = {
	name: "triage",
	trigger: "message",
	channel: "Team Slack",
	match: "*crash*",
	prompt: "Reply with the crash ID.",
	target: "reply",
	precompile: true,
	compiled: SCRIPT,
	compiled_key: "abc1234def567890",
};

/**
 * Mocks one agent with the Slack channel "Team Slack" and `tasks`, with
 * compile attempts per task and the jobs job_list returns.
 */
function mockCompiled(
	tasks: Task[],
	opts: {
		attempts?: Record<string, Record<string, unknown>[]>;
		jobs?: Record<string, unknown>[];
	} = {},
) {
	const config = {
		agents: [
			{
				name: "assistant",
				model: "x/y",
				projects: [{ target: "acme/app" }],
				channels: [
					{
						type: "slack",
						id: "team-slack",
						name: "Team Slack",
						deliver_to: "C1",
					},
				],
				tasks: tasks.map((t) => ({ ...t })),
			},
		],
	};
	const attempts = opts.attempts ?? {
		triage: [
			{
				id: "c1",
				agent_id: "assistant",
				task_name: "triage",
				status: "succeeded",
				prompt: "Reply with the crash ID.",
				script: SCRIPT,
				reason: "Every step is deterministic.",
				created_at: AGO,
				updated_at: AGO,
			},
		],
	};
	const calls: Record<string, Record<string, unknown>[]> = {};
	const record = (tool: string, args?: Record<string, unknown>) => {
		calls[tool] ??= [];
		calls[tool].push(args ?? {});
	};
	const find = (name: unknown) =>
		config.agents[0].tasks.find(
			(t) => t.name === String(name) || `assistant/${t.name}` === String(name),
		);
	mockMCP({
		agent_list: [
			{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
		],
		config_get: () => config,
		config_validate: [],
		auth_list: [],
		tool_list: [],
		session_list: [],
		session_messages: [],
		channel_sessions: [],
		workflow_entries: [],
		workflow_preview: [],
		task_session: { id: "task-session", agent_id: "assistant" },
		task_preview: () => ({ description: "preview", next: [] }),
		task_list: () =>
			config.agents[0].tasks.map((t) => ({
				id: `assistant/${t.name}`,
				agent_name: "assistant",
				name: t.name,
				enabled: true,
				trigger_type: t.trigger === "message" ? "message" : "interval",
				history: [],
			})),
		task_analyze: (args?: Record<string, unknown>) => {
			const deterministic = String(args?.content).startsWith("Reply");
			return {
				verdict: deterministic ? "deterministic" : "nondeterministic",
				reason: deterministic ? "" : "needs judgment",
				will_precompile: deterministic && args?.precompile === true,
				estimate: { available: true, min_tokens: 0, max_tokens: 0 },
			};
		},
		task_compile_query: (args?: Record<string, unknown>) =>
			attempts[String(args?.task)] ?? [],
		job_query: opts.jobs ?? [],
		job_list: (args?: Record<string, unknown>) =>
			(opts.jobs ?? []).filter((j) => j.task_id === args?.task),
		job_logs: "",
		config_task_convert_to_script: (args?: Record<string, unknown>) => {
			record("config_task_convert_to_script", args);
			const t = find(args?.task);
			if (t) {
				t.compiled = 'print("fresh")';
				t.compiled_key = "fff0000aaaa11111";
				t.prompt = t.prompt ?? "";
			}
			attempts[String(args?.task)] = [
				{
					id: "c2",
					agent_id: "assistant",
					task_name: String(args?.task),
					status: "succeeded",
					prompt: t?.prompt,
					script: 'print("fresh")',
					reason: "Recompiled.",
					created_at: NOW,
					updated_at: NOW,
				},
				...(attempts[String(args?.task)] ?? []),
			];
			return "recompiling";
		},
		task_update: (args?: Record<string, unknown>) => {
			record("task_update", args);
			const t = find(args?.name);
			if (t && typeof args?.precompile === "boolean")
				t.precompile = args.precompile;
			return "updated";
		},
	});
	return { config, calls };
}

// The panel's open state is module state read from localStorage on load;
// start each test as a fresh page would, with it closed.
beforeEach(() => {
	useCompiledPanelOpen().value = false;
});

const WORKFLOWS_URL =
	"/agents/assistant/channels/Team%20Slack/settings?tab=workflows";

async function openWorkflowEditor(name = "triage", fresh = true) {
	if (fresh) await renderApp(WORKFLOWS_URL);
	await page
		.locator(`[data-testid=workflow-row][data-workflow=${name}]`)
		.getByTestId("workflow-edit")
		.click();
	return page.getByTestId("workflow-form");
}

const count = (loc: { elements(): Element[] }) => () => loc.elements().length;

test("workflow rows show each compiled state in the shared chip", async () => {
	mockCompiled([
		COMPILED,
		{ ...COMPILED, name: "vague", compiled: "", compiled_key: "k2" },
		{ ...COMPILED, name: "fresh", compiled: "", compiled_key: "" },
		{
			...COMPILED,
			name: "flaky",
			script_failures: 2,
			script_last_error: "gh 404",
		},
		{ ...COMPILED, name: "off", precompile: false, compiled: "" },
	]);
	await renderApp(WORKFLOWS_URL);
	const chip = (name: string) =>
		page
			.locator(`[data-testid=workflow-row][data-workflow=${name}]`)
			.getByTestId("compiled-status-chip");
	await expect
		.element(chip("triage"))
		.toHaveAttribute("data-state", "compiled");
	await expect.element(chip("triage")).toHaveText("Compiled");
	await expect
		.element(chip("vague"))
		.toHaveAttribute("data-state", "not-compiled");
	await expect.element(chip("vague")).toHaveText("Not compiled");
	await expect
		.element(chip("fresh"))
		.toHaveAttribute("data-state", "compiling");
	await expect.element(chip("flaky")).toHaveAttribute("data-state", "failing");
	await expect.element(chip("flaky")).toHaveText("Script failing");
	await expect
		.element(chip("flaky"))
		.toHaveAttribute("title", expect.stringMatching(/last error: gh 404/));
	await expect.poll(count(chip("off"))).toBe(0);
});

test("a failed compile shows Compile failed in the editor", async () => {
	mockCompiled([{ ...COMPILED, compiled: "", compiled_key: "" }], {
		attempts: {
			triage: [
				{
					id: "c1",
					status: "failed",
					reason: "model unavailable",
					created_at: AGO,
				},
			],
		},
	});
	const form = await openWorkflowEditor();
	const chip = form.getByTestId("compiled-status-chip");
	await expect.element(chip).toHaveAttribute("data-state", "failed");
	await expect.element(chip).toHaveText("Compile failed");
	await form.getByTestId("workflow-compiled-toggle").click();
	await expect
		.element(form.getByTestId("compiled-panel-empty"))
		.toMatchTextContent("last compile attempt failed");
	await expect
		.element(form.getByTestId("compiled-panel-notes"))
		.toMatchTextContent("model unavailable");
});

test("the compiled panel shows the script beside the prompt, goes stale on edit, and Recompile updates it", async () => {
	await page.viewport(1400, 1000);
	const { calls } = mockCompiled([COMPILED], {
		jobs: [
			{
				id: "job-9",
				task_id: "assistant/triage",
				agent_id: "assistant",
				status: "completed",
				script_error: "<string>:2: attempt to index a nil value",
				prompt: "",
				attempts: 1,
				max_retries: 0,
				created_at: AGO,
				updated_at: AGO,
			},
		],
	});
	const form = await openWorkflowEditor();
	const chip = form.getByTestId("compiled-status-chip").first();
	await expect.element(chip).toHaveAttribute("data-state", "compiled");
	await expect.element(chip).toMatchTextContent("Compiled · vabc1234");

	const toggle = form.getByTestId("workflow-compiled-toggle");
	await expect.element(toggle).toHaveText(/Show compiled/);
	await toggle.click();
	await expect.element(toggle).toHaveText(/Hide compiled/);
	const panel = form.getByTestId("compiled-panel");
	await expect.element(panel).toBeVisible();

	// Beside the prompt on a wide screen.
	const promptBox = await boundingBoxOf(form.getByTestId("workflow-content"));
	const panelBox = await boundingBoxOf(panel);
	expect(panelBox.x).toBeGreaterThan(promptBox.x + promptBox.width - 1);

	// The current script, numbered, with the compiler's notes.
	const lines = panel.getByTestId("compiled-panel-line");
	await expect.poll(count(lines)).toBe(7);
	await expect.element(lines.nth(1)).toHaveAttribute("data-line", "2");
	await expect
		.element(lines.nth(1))
		.toMatchTextContent("2local id = string.match");
	await expect.element(lines.nth(1)).toHaveClass(/bg-red-50/);
	await expect
		.element(panel.getByTestId("compiled-panel-notes"))
		.toMatchTextContent("Every step is deterministic.");
	await expect.element(panel.getByTestId("compiled-panel-copy")).toBeVisible();
	await expect
		.element(panel.getByTestId("compiled-panel-last-failed"))
		.toHaveAttribute("href", "/agents/assistant/tasks/triage/runs/job-9");
	await expect.poll(count(panel.getByTestId("compiled-panel-stale"))).toBe(0);

	// Editing the prompt marks the chip and the panel stale.
	await form.getByTestId("workflow-content").fill("Summarize the crash.");
	await expect.element(chip).toHaveAttribute("data-state", "stale");
	await expect
		.element(chip)
		.toMatchTextContent("Compiled (stale: prompt changed)");
	const stale = panel.getByTestId("compiled-panel-stale");
	await expect.element(stale).toBeVisible();
	await expect
		.element(stale.getByTestId("compiled-panel-source-version"))
		.toHaveText("vabc1234");
	await expect
		.element(stale.getByTestId("compiled-panel-source-prompt"))
		.toHaveText("Reply with the crash ID.");
	// Live analysis of the edited prompt predicts it no longer compiles.
	await expect
		.element(stale.getByTestId("compiled-panel-prediction"))
		.toMatchTextContent("would not compile (nondeterministic: needs judgment)");

	// Back to the saved prompt, then Recompile updates the chip and the panel.
	await form.getByTestId("workflow-content").fill("Reply with the crash ID.");
	await expect.element(chip).toHaveAttribute("data-state", "compiled");
	await panel.getByTestId("compiled-panel-recompile").click();
	await expect.poll(count(lines)).toBe(1);
	await expect.element(lines.first()).toMatchTextContent('print("fresh")');
	await expect.element(chip).toMatchTextContent("vfff0000");
	await expect
		.element(panel.getByTestId("compiled-panel-notes"))
		.toMatchTextContent("Recompiled.");
	expect(calls.config_task_convert_to_script).toEqual([
		{ agent: "assistant", task: "triage" },
	]);

	// The panel stays open across a reload: the reloaded page reads it back
	// from storage, as a fresh page load would.
	useCompiledPanelOpen().value =
		localStorage.getItem("aviary.compiledPanelOpen") === "true";
	await reload();
	const again = await openWorkflowEditor("triage", false);
	await expect.element(again.getByTestId("compiled-panel")).toBeVisible();

	// Disable precompile turns it off and unchecks the form's choice.
	await again.getByTestId("compiled-panel-disable").click();
	await expect
		.element(again.getByLabelText("Try to precompile"))
		.not.toBeChecked();
	expect(calls.task_update?.at(-1)).toMatchObject({
		name: "assistant/triage",
		precompile: false,
	});
});

test("on a phone the compiled panel stacks below the prompt without overflow", async () => {
	await page.viewport(390, 844);
	mockCompiled([
		{
			...COMPILED,
			compiled: `${SCRIPT}\nprint("${"a very long line ".repeat(20)}")`,
		},
	]);
	const form = await openWorkflowEditor();
	await form.getByTestId("workflow-compiled-toggle").click();
	const panel = form.getByTestId("compiled-panel");
	await expect.element(panel).toBeVisible();
	const promptBox = await boundingBoxOf(form.getByTestId("workflow-content"));
	const panelBox = await boundingBoxOf(panel);
	expect(panelBox.y).toBeGreaterThan(promptBox.y + promptBox.height - 1);
	const overflow = document.documentElement.scrollWidth - window.innerWidth;
	expect(overflow).toBeLessThanOrEqual(0);
});

test("the task editor and task page header use the same compiled chip and panel", async () => {
	mockCompiled(
		[
			{
				name: "report",
				type: "project",
				project: "acme/app",
				every: "1h",
				prompt: "Reply with the open issue count.",
				precompile: true,
				compiled: 'print("3")',
				compiled_key: "1234567abcdef000",
			},
		],
		{ attempts: {} },
	);
	await renderApp("/agents/assistant/tasks/report");
	const header = page.getByTestId("settings-header");
	const chip = header.getByTestId("compiled-status-chip");
	await expect.element(chip).toHaveAttribute("data-state", "compiled");
	await expect.element(chip).toMatchTextContent("v1234567");
	// The chip opens the task's editor with its compiled script shown.
	await chip.click();
	await expect
		.poll(currentURL)
		.toMatch(/\/agents\/assistant\/tasks\?task=report/);
	const panel = page.getByTestId("compiled-panel");
	await expect.element(panel).toBeVisible();
	const lines = panel.getByTestId("compiled-panel-line");
	await expect
		.poll(() =>
			lines.elements().map((el) => el.textContent?.replace(/\s+/g, " ").trim()),
		)
		.toEqual(['1print("3")']);
	await expect
		.element(page.getByTestId("settings-task-compiled-toggle"))
		.toHaveAttribute("aria-pressed", "true");
});
