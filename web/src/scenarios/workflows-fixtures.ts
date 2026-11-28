// Fixtures shared by the workflows component tests (#1636), ported from
// web/e2e/workflows.spec.ts: one agent with the Slack channel "Team Slack",
// its workflows (message-triggered tasks) and their runs.
import { vi } from "vitest";
import { mockMCP } from "../component-test/server";

export interface Task {
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

export const NOW = new Date().toISOString();
export const AGO = new Date(Date.now() - 60_000).toISOString();

export const TRIAGE: Task = {
	name: "triage",
	trigger: "message",
	channel: "Team Slack",
	entry: "C1",
	match: "*crash*",
	prompt: "Triage the crash.",
	target: "reply",
};
export const LOG_ALL: Task = {
	name: "log-all",
	trigger: "message",
	channel: "Team Slack",
	prompt: "Log it.",
};
export const ARCHIVE: Task = {
	name: "archive",
	enabled: false,
	trigger: "message",
	channel: "Team Slack",
	prompt: "Archive it.",
};

export const TRIGGER_CONTEXT = {
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

/**
 * task_analyze's answer for args. A workflow run also carries its triggering
 * message, so it costs more than the same scheduled task.
 */
export function workflowAnalysis(args?: Record<string, unknown>) {
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
export function mockWorkflows(
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
	mockMCP({
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


/**
 * Records every native alert, confirm or prompt, dismissing each, which the
 * caller expects to stay empty.
 */
export function nativeDialogs(): string[] {
	const seen: string[] = [];
	vi.spyOn(window, "alert").mockImplementation((m?: unknown) => {
		seen.push(`alert: ${String(m ?? "")}`);
	});
	vi.spyOn(window, "confirm").mockImplementation((m?: string) => {
		seen.push(`confirm: ${m ?? ""}`);
		return false;
	});
	vi.spyOn(window, "prompt").mockImplementation((m?: string) => {
		seen.push(`prompt: ${m ?? ""}`);
		return null;
	});
	return seen;
}
