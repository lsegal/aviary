import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";
import { chooseOption } from "./helpers/select";

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
	precompile_note?: string;
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
const LOG_ALL: Task = {
	name: "log-all",
	trigger: "message",
	channel: "Team Slack",
	prompt: "Log it.",
};
const ARCHIVE: Task = {
	name: "archive",
	enabled: false,
	trigger: "message",
	channel: "Team Slack",
	prompt: "Archive it.",
};

const TRIGGER_CONTEXT = {
	channel_type: "slack",
	configured_id: "team-slack",
	channel: "Team Slack",
	entry: "C1",
	session_id: "channel:slack:C1",
	recorded_at: NOW,
	message: {
		sender: "Alice",
		text: "the app crashed",
		attachments: ["crash.log (/tmp/crash.log)"],
		timestamp: NOW,
	},
	before: [{ sender: "Bob", text: "deploying now", timestamp: AGO }],
	after: [{ from_agent: true, text: "Looking into it", timestamp: NOW }],
};

/** Mocks one agent with the Slack channel "Team Slack" and `tasks`. */
async function mockWorkflows(
	page: Page,
	tasks: Task[],
	jobs: Record<string, unknown>[] = [],
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
			// A workflow run also carries its triggering message, so it costs
			// more than the same scheduled task.
			const workflow = args?.trigger === "message";
			const model = {
				available: true,
				min_tokens: workflow ? 5_000 : 4_000,
				max_tokens: workflow ? 9_000 : 7_000,
				min_cost_usd: 0.01,
				max_cost_usd: 0.03,
				...(workflow
					? { min_context_tokens: 55, max_context_tokens: 1240 }
					: {}),
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
		task_delete: (args?: Record<string, unknown>) => {
			record("task_delete", args);
			config.agents[0].tasks = config.agents[0].tasks.filter(
				(t) => t !== find(args?.name),
			);
			return "deleted";
		},
	});
	return { config, calls };
}

test("the Workflows tab adds a workflow with an entry, a match and a preview", async ({
	page,
}) => {
	const { calls } = await mockWorkflows(page, [TRIAGE]);
	await page.goto("/agents/assistant/channels/Team%20Slack/settings");

	await page.getByTestId("channel-tab-workflows").click();
	await expect(page).toHaveURL(/tab=workflows/);
	await expect(page.getByTestId("workflow-row")).toHaveCount(1);
	await expect(page.getByTestId("workflow-row")).toContainText(
		"messages matching *crash* on C1",
	);

	await page.getByTestId("workflow-add").click();
	// An empty match takes over every message; the form says so.
	await expect(page.getByTestId("workflow-takeover-hint")).toContainText(
		"the agent won't reply on its own",
	);
	await expect(page.getByTestId("workflow-preview-message")).toHaveCount(2);
	await expect(page.getByTestId("workflow-preview-count")).toHaveText(
		"2 of 2 recent messages",
	);

	await page.getByTestId("workflow-name").fill("crashes");
	await page.getByTestId("workflow-match").fill("*crash*");
	await expect(page.getByTestId("workflow-takeover-hint")).toHaveCount(0);
	await expect(page.getByTestId("workflow-preview-count")).toHaveText(
		"1 of 2 recent messages",
	);
	await expect(
		page.locator("[data-testid=workflow-preview-message][data-matches=true]"),
	).toContainText("the app crashed");

	await chooseOption(page.getByTestId("workflow-entry"), "#general (C1)");
	await page.getByTestId("workflow-content").fill("Reply with next steps.");
	await page.getByTestId("workflow-save").click();

	await expect(page.getByTestId("workflow-row")).toHaveCount(2);
	expect(calls.task_schedule?.[0]).toMatchObject({
		agent: "assistant",
		name: "crashes",
		trigger: "message",
		channel: "Team Slack",
		entry: "C1",
		match: "*crash*",
		target: "reply",
		content: "Reply with next steps.",
	});
});

test("the Workflows tab pauses and deletes a workflow", async ({ page }) => {
	const { calls } = await mockWorkflows(page, [TRIAGE]);
	await page.goto(
		"/agents/assistant/channels/Team%20Slack/settings?tab=workflows",
	);
	await page.getByTestId("task-enabled").click();
	await expect
		.poll(() => calls.task_update?.[0])
		.toMatchObject({
			name: "assistant/triage",
			enabled: false,
		});
	await expect(page.getByTestId("workflow-row")).toContainText("Paused");

	page.once("dialog", (d) => d.accept());
	await page.getByTestId("workflow-delete").click();
	await expect(page.getByTestId("workflows-empty")).toBeVisible();
	expect(calls.task_delete?.[0]).toMatchObject({ name: "assistant/triage" });
});

test("the channel chat shows workflow cards, active first, with a remembered collapse and paused filter", async ({
	page,
}) => {
	await mockWorkflows(
		page,
		[ARCHIVE, TRIAGE, LOG_ALL],
		[
			{
				id: "job-1",
				task_id: "assistant/triage",
				agent_id: "assistant",
				status: "in_progress",
				trigger: "message",
				prompt: "",
				attempts: 1,
				max_retries: 0,
				created_at: NOW,
				updated_at: NOW,
			},
			{
				id: "job-0",
				task_id: "assistant/log-all",
				agent_id: "assistant",
				status: "pending",
				trigger: "message",
				prompt: "",
				attempts: 0,
				max_retries: 0,
				created_at: NOW,
				updated_at: NOW,
			},
		],
	);
	await page.goto("/agents/assistant/channels/Team%20Slack");

	const cards = page.getByTestId("workflow-card");
	await expect(cards).toHaveCount(3);
	await expect(cards.nth(0)).toHaveAttribute("data-workflow", "log-all");
	await expect(cards.nth(1)).toHaveAttribute("data-workflow", "triage");
	await expect(cards.nth(2)).toHaveAttribute("data-workflow", "archive");
	await expect(cards.nth(2)).toHaveAttribute("data-paused", "true");

	// Triggering and running cards carry the traveling border, in different colors.
	await expect(cards.nth(0)).toHaveAttribute("data-activity", "triggering");
	await expect(cards.nth(1)).toHaveAttribute("data-activity", "running");
	const colors = await cards.evaluateAll((els) =>
		els
			.slice(0, 2)
			.map((el) => (el as HTMLElement).style.getPropertyValue("--snake")),
	);
	expect(colors[0]).not.toBe(colors[1]);
	const animation = await cards
		.nth(1)
		.evaluate((el) => getComputedStyle(el, "::before").animationName);
	expect(animation).toContain("workflow-snake");

	await page.getByTestId("workflow-strip-hide-paused").check();
	await expect(cards).toHaveCount(2);

	await page.getByTestId("workflow-strip-toggle").click();
	await expect(page.getByTestId("workflow-strip")).toHaveAttribute(
		"data-collapsed",
		"true",
	);
	await page.reload();
	await expect(page.getByTestId("workflow-strip")).toHaveAttribute(
		"data-collapsed",
		"true",
	);
	await expect(cards).toHaveCount(2);

	// The message the workflow handled says so, and links to its run.
	const handled = page.getByTestId("chat-message-workflows");
	await expect(handled).toContainText("Handled by workflow");
	await expect(page.getByTestId("chat-message-workflow-link")).toHaveAttribute(
		"href",
		"/agents/assistant/tasks/triage/runs/job-1",
	);
});

test("the traveling border does not travel under reduced motion", async ({
	page,
}) => {
	await page.emulateMedia({ reducedMotion: "reduce" });
	await mockWorkflows(
		page,
		[TRIAGE],
		[
			{
				id: "job-1",
				task_id: "assistant/triage",
				agent_id: "assistant",
				status: "in_progress",
				prompt: "",
				attempts: 1,
				max_retries: 0,
				created_at: NOW,
				updated_at: NOW,
			},
		],
	);
	await page.goto("/agents/assistant/channels/Team%20Slack");
	const card = page.getByTestId("workflow-card");
	await expect(card).toHaveAttribute("data-activity", "running");
	const animation = await card.evaluate(
		(el) => getComputedStyle(el, "::before").animationName,
	);
	expect(animation).toBe("none");
});

test("a workflow card opens its detail view with the run's trigger context", async ({
	page,
}) => {
	await mockWorkflows(
		page,
		[TRIAGE],
		[
			{
				id: "job-1",
				task_id: "assistant/triage",
				agent_id: "assistant",
				status: "completed",
				trigger: "message",
				trigger_context: TRIGGER_CONTEXT,
				prompt: "",
				attempts: 1,
				max_retries: 0,
				created_at: AGO,
				updated_at: NOW,
			},
		],
	);
	await page.goto("/agents/assistant/channels/Team%20Slack");
	await page.getByTestId("workflow-card").click();
	await expect(page).toHaveURL(/\/agents\/assistant\/tasks\/triage$/);

	await expect(page.getByTestId("task-schedule")).toContainText("Workflow");
	await expect(page.getByTestId("task-run-now")).toBeVisible();
	await expect(page.getByTestId("task-toggle-enabled")).toHaveText(/Pause/);
	const ctx = page.getByTestId("trigger-context");
	await expect(ctx).toContainText("Team Slack · C1");
	await expect(page.getByTestId("trigger-context-before")).toContainText(
		"Bob: deploying now",
	);
	await expect(page.getByTestId("trigger-context-message")).toContainText(
		"Alice: the app crashed",
	);
	await expect(page.getByTestId("trigger-context-attachment")).toContainText(
		"crash.log",
	);
	await expect(page.getByTestId("trigger-context-after")).toContainText(
		"Agent: Looking into it",
	);

	// Settings opens the channel's Workflows tab on this workflow.
	await page.getByTestId("task-settings-link").click();
	await expect(page.getByTestId("workflow-form")).toBeVisible();
	await expect(page.getByTestId("workflow-name")).toHaveValue("triage");
});

test("the Channels page shows every workflow and recent messages by channel", async ({
	page,
}) => {
	await mockWorkflows(page, [ARCHIVE, TRIAGE]);
	await page.goto("/agents/assistant/channels");

	const overview = page.getByTestId("channels-overview");
	await expect(overview).toBeVisible();
	const cards = overview.getByTestId("workflow-card");
	await expect(cards).toHaveCount(2);
	await expect(cards.nth(0)).toHaveAttribute("data-workflow", "triage");
	await expect(cards.nth(1)).toHaveAttribute("data-paused", "true");

	await expect(page.getByTestId("channels-overview-message")).toHaveCount(2);
	await page.getByTestId("channels-overview-channel-link").click();
	await expect(page).toHaveURL(/\/agents\/assistant\/channels\/Team%20Slack$/);
});

test("workflows stay out of the Scheduled Tasks list", async ({ page }) => {
	await mockWorkflows(page, [
		TRIAGE,
		{ name: "hourly", every: "1h", prompt: "p" },
	]);
	await page.goto("/agents/assistant/tasks");
	await expect(page.getByTestId("task-card")).toHaveCount(1);
});

test("the workflow editor analyzes the prompt as you type, with the trigger message in the estimate", async ({
	page,
}) => {
	const { calls } = await mockWorkflows(page, [TRIAGE]);
	await page.goto(
		"/agents/assistant/channels/Team%20Slack/settings?tab=workflows",
	);
	await page.getByTestId("workflow-add").click();
	const form = page.getByTestId("workflow-form");
	const feedback = form.getByTestId("task-cost-feedback");
	await expect(feedback).toHaveCount(0);
	// The same section as the task editors: prompt, analysis, precompile, model.
	await expect(form.getByTestId("workflow-prompt-section")).toBeVisible();
	await expect(form.getByTestId("workflow-model")).toBeVisible();
	const precompile = form.getByLabel("Try to precompile");
	await expect(precompile).toBeChecked();
	await expect(form.getByTestId("workflow-precompile-help")).toContainText(
		"runs on each matching message without the model",
	);

	await page
		.getByTestId("workflow-content")
		.pressSequentially("Summarize the crash", { delay: 20 });
	await expect(feedback).toHaveAttribute("data-state", "ready");
	// Rapid typing is analyzed once, as a workflow. (The agent settings
	// page behind the tab analyzes its own selected task.)
	const typed = (calls.task_analyze ?? []).filter((c) =>
		String(c.content).startsWith("Summ"),
	);
	expect(typed).toHaveLength(1);
	expect(typed[0]).toMatchObject({
		agent: "assistant",
		content: "Summarize the crash",
		type: "prompt",
		trigger: "message",
		precompile: true,
	});
	expect(String(typed[0].editor)).toMatch(/^task-editor-/);
	await expect(feedback.getByTestId("task-cost-verdict")).toHaveText(
		"Not deterministic",
	);
	await expect(feedback.getByTestId("task-cost-estimate")).toHaveText(
		"≈ 5k–9k tokens/run ($0.010–$0.030)",
	);
	await expect(feedback.getByTestId("task-cost-context")).toHaveText(
		"Includes the matched message and the messages before it, ≈ 55–1.2k tokens each model call.",
	);

	// A deterministic prompt is precompiled, with the model's cost beside it.
	await page.getByTestId("workflow-content").fill("Reply with the crash ID");
	await expect(feedback.getByTestId("task-cost-verdict")).toHaveText(
		"This task can run deterministically",
	);
	await expect(feedback.getByTestId("task-cost-estimate")).toHaveText(
		"≈ 0 tokens/run",
	);
	await expect(feedback.getByTestId("task-cost-without-precompile")).toHaveText(
		"≈ 5k–9k tokens/run ($0.010–$0.030) without precompile",
	);
	await expect(feedback.getByTestId("task-cost-note")).toContainText(
		"runs on each matching message with no model calls; a run whose script fails falls back to the model",
	);

	// A script workflow is already a script.
	await chooseOption(form.getByRole("combobox", { name: "Type" }), "script");
	await expect(precompile).toBeDisabled();
	await expect(feedback.getByTestId("task-cost-estimate")).toHaveText(
		"0 tokens/run",
	);
});

test("a workflow's precompile choice is saved and only sent again when it changes", async ({
	page,
}) => {
	const { calls } = await mockWorkflows(page, [
		{ ...TRIAGE, precompile: true, compiled: 'print("ok")', compiled_key: "k" },
	]);
	await page.goto(
		"/agents/assistant/channels/Team%20Slack/settings?tab=workflows",
	);
	await expect(
		page.getByTestId("workflow-row").getByTestId("task-precompile-badge"),
	).toHaveText("Compiled");

	// A new workflow precompiles by default.
	await page.getByTestId("workflow-add").click();
	await page.getByTestId("workflow-name").fill("crash-ids");
	await page.getByTestId("workflow-content").fill("Reply with the crash ID");
	await page.getByTestId("workflow-save").click();
	await expect(page.getByTestId("workflow-row")).toHaveCount(2);
	expect(calls.task_schedule?.[0]).toMatchObject({
		name: "crash-ids",
		type: "prompt",
		precompile: true,
	});

	// Editing a compiled workflow says so, and leaves an unchanged choice alone.
	await page
		.locator("[data-testid=workflow-row][data-workflow=triage]")
		.getByTestId("workflow-edit")
		.click();
	const precompile = page
		.getByTestId("workflow-form")
		.getByLabel("Try to precompile");
	await expect(precompile).toBeChecked();
	await expect(page.getByTestId("workflow-precompile-help")).toContainText(
		"Precompiled: each matching message runs its script",
	);
	await page.getByTestId("workflow-save").click();
	await expect(page.getByTestId("workflow-form")).toHaveCount(0);
	expect(calls.task_update?.[0]).not.toHaveProperty("precompile");

	await page
		.locator("[data-testid=workflow-row][data-workflow=triage]")
		.getByTestId("workflow-edit")
		.click();
	await precompile.uncheck();
	await page.getByTestId("workflow-save").click();
	await expect(page.getByTestId("workflow-form")).toHaveCount(0);
	expect(calls.task_update?.[1]).toMatchObject({
		name: "assistant/triage",
		precompile: false,
	});
});

test("workflow cards and the detail view show the compiled state and fallback runs", async ({
	page,
}) => {
	await mockWorkflows(
		page,
		[
			{
				...TRIAGE,
				precompile: true,
				compiled: 'print("ok")',
				compiled_key: "k",
				script_failures: 1,
				precompile_note:
					"The compiled script failed on its first run (lookup failed), so it was set aside and the task runs on the model.",
			},
		],
		[
			{
				id: "job-1",
				task_id: "assistant/triage",
				agent_id: "assistant",
				status: "completed",
				trigger: "message",
				trigger_context: TRIGGER_CONTEXT,
				script_error: "lookup failed",
				prompt: "",
				attempts: 1,
				max_retries: 0,
				created_at: AGO,
				updated_at: NOW,
			},
		],
	);
	await page.goto("/agents/assistant/channels/Team%20Slack");
	const card = page.getByTestId("workflow-card");
	await expect(card.getByTestId("task-precompile-badge")).toHaveText(
		"Compiled",
	);
	await expect(card.getByTestId("task-precompile-badge")).toHaveAttribute(
		"title",
		/failed its last run/,
	);
	await expect(card.getByTestId("workflow-card-last")).toHaveAttribute(
		"data-last",
		"fallback",
	);
	await expect(card.getByTestId("workflow-card-last")).toHaveText(
		"last run fell back",
	);

	await card.click();
	await expect(page).toHaveURL(/\/agents\/assistant\/tasks\/triage$/);
	await expect(
		page.getByTestId("task-header").getByTestId("task-precompile-badge"),
	).toHaveText("Compiled · 1 failed");
	await expect(page.getByTestId("task-precompile-note")).toContainText(
		"failed on its first run",
	);
	const stats = page.getByTestId("stat-row");
	await expect(stats).toContainText("2compile attempts · last succeeded");
	await expect(stats.getByTitle("Succeeded via fallback: 1")).toBeVisible();
});
