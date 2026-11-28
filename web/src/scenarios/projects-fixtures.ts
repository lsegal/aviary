// Shared fixtures for the projects-*.component.test.ts scenarios, ported
// from web/e2e/projects.spec.ts (#1636). Fixtures follow the payloads
// internal/projects' Manager returns for project_status, project_job_list
// and project_job_get.
import { page } from "vitest/browser";
import { mockMCP, type ToolFixtures } from "../component-test/server";

export const minutesAgo = (m: number) =>
	new Date(Date.now() - m * 60_000).toISOString();

export const STATUS = {
	identity: "7F3CA91B",
	running: true,
	targets: ["lsegal/aviary", "lsegal/zvidlib"],
	issue_counts: { "lsegal/aviary": 6, "lsegal/zvidlib": 0 },
	jobs: {
		running: 2,
		queued: 0,
		paused: 1,
		completed: 7,
		merged_24h: 3,
		failed: 1,
	},
	concurrency: 4,
	delivery: "poll",
	poll_interval: "30s",
	issue_filter: "assignee:@me",
	allowed_commenters: ["lsegal", "octocat"],
	quotas: {
		claude: "38% of 5h quota left",
		codex: "quota ok",
		gemini: "signed out",
	},
	agents: ["claude/opus", "codex", "gemini"],
	clis: [
		{ name: "claude", models: ["opus"], quota: "38% of 5h quota left" },
		{ name: "codex", models: ["gpt-5.6-terra"], quota: "quota ok" },
		{ name: "gemini", models: [], quota: "signed out" },
	],
	recent_logs:
		"12:03:58 checked lsegal/aviary: 6 ready issues\n12:04:11 claimed #221, cloning into ~/.aviary/clones/aviary-221",
	log_offset: 2,
	last_check: minutesAgo(0.1),
};

export const actions = (status: string) => ({
	retry: status !== "queued",
	pause: status === "active" || status === "waiting",
	resume: status === "paused",
	stop: ["active", "waiting", "paused"].includes(status),
});

// The board state core.JobSnapshot.BoardState reports for each status of a
// job whose clone exists.
export const STATE_OF: Record<string, string> = {
	queued: "starting",
	active: "running",
	stopping: "running",
	waiting: "paused",
	paused: "paused",
	complete: "done",
	failed: "failed",
};

export const job = (fields: {
	number: number;
	title: string;
	status: string;
	agent: string;
	model: string;
	started: number;
	pr?: number;
	prRepo?: string;
	progress?: [number, number];
	waitReason?: string;
	target?: string;
	/** Overrides the board state, such as a running job with no clone yet. */
	state?: string;
	prState?: "draft" | "open" | "merged";
}) => ({
	key: `${fields.target ?? "lsegal/aviary"}#${fields.number}`,
	target: fields.target ?? "lsegal/aviary",
	number: fields.number,
	title: fields.title,
	status: fields.status,
	state: fields.state ?? STATE_OF[fields.status],
	kind: "issue",
	agent: fields.agent,
	model: fields.model,
	actions: actions(fields.status),
	repository: "lsegal/aviary",
	issue_url: `https://github.com/lsegal/aviary/issues/${fields.number}`,
	...(fields.pr
		? {
				pull_request: fields.pr,
				pull_request_url: `https://github.com/${fields.prRepo ?? "lsegal/aviary"}/pull/${fields.pr}`,
				pull_request_state: fields.prState ?? "open",
			}
		: {}),
	...(fields.progress
		? {
				progress: {
					done: fields.progress[0],
					total: fields.progress[1],
					percent: Math.round((fields.progress[0] / fields.progress[1]) * 100),
				},
			}
		: {}),
	started: minutesAgo(fields.started),
	...(fields.waitReason ? { wait_reason: fields.waitReason } : {}),
});

/** The clone each job's detail reports. */
export const CLONES: Record<number, Record<string, unknown>> = {
	218: {
		checkout_directory: "/tmp/aviary-gh-fix-200",
		checkout_state: "present",
	},
	221: { checkout_directory: "", checkout_state: "none" },
	209: {
		checkout_directory: "/tmp/aviary-gh-fix-209",
		checkout_state: "removed",
	},
	200: {
		checkout_directory: "/tmp/aviary-gh-fix-200",
		checkout_state: "reused",
		checkout_reused_by: 218,
	},
};

export const JOBS = [
	job({
		number: 218,
		title: "Cap scheduler retry backoff at 10 minutes",
		status: "active",
		agent: "claude",
		model: "opus",
		started: 14,
		pr: 233,
		prState: "draft",
		progress: [2, 5],
	}),
	job({
		number: 221,
		title: "Export a session as Markdown from the chat menu",
		status: "active",
		agent: "codex",
		model: "gpt-5.5",
		started: 3,
		state: "starting",
	}),
	job({
		number: 209,
		title: "Scoop install fails when ~/.local/bin is missing",
		status: "failed",
		agent: "claude",
		model: "opus",
		started: 60,
		pr: 227,
	}),
	job({
		number: 214,
		title: "Show token usage per scheduled task",
		status: "paused",
		agent: "claude",
		model: "opus",
		started: 41,
		pr: 229,
		prRepo: "someone/aviary",
		progress: [5, 6],
		waitReason: "Stacked on PR #226, waiting for it to merge",
	}),
	job({
		number: 200,
		title: "Add a dark theme toggle",
		status: "complete",
		agent: "claude",
		model: "opus",
		started: 300,
		pr: 205,
		prState: "merged",
	}),
];

export const at = (m: number) => minutesAgo(m);

export const EVENTS_218 = [
	{ kind: "system", time: at(14), text: "Claimed #218" },
	{ kind: "system", time: at(13), text: "Opened draft PR #233" },
	{
		kind: "message",
		time: at(12),
		text: "I can reproduce it: retries back off **without a cap**. I'll clamp the delay at 10 minutes.",
	},
	{
		kind: "reasoning",
		time: at(12),
		text: "The backoff doubles each attempt.",
	},
	{
		kind: "plan",
		time: at(11),
		items: [
			{ text: "Reproduce the unbounded backoff", status: "completed" },
			{ text: "Clamp the delay", status: "in_progress" },
			{ text: "Add a test", status: "pending" },
		],
	},
	{
		kind: "tool_call",
		time: at(11),
		call_id: "c1",
		name: "Read",
		input: { file_path: "internal/scheduler/queue.go" },
	},
	{
		kind: "tool_result",
		time: at(11),
		call_id: "c1",
		output: "package scheduler\n…",
	},
	{
		kind: "file_change",
		time: at(10),
		path: "internal/scheduler/queue.go",
		diff: "@@ -40,3 +40,4 @@\n delay := base << attempt\n-return delay\n+return min(delay, 10*time.Minute)\n+// capped",
	},
	{
		kind: "command",
		time: at(9),
		call_id: "b1",
		command: "go test ./internal/scheduler/...",
		status: "running",
	},
	{
		kind: "command",
		time: at(9),
		call_id: "b1",
		output: "--- FAIL: TestBackoffCap (0.01s)",
		exit_code: 1,
		is_error: true,
		status: "done",
	},
	{
		kind: "comment_relay",
		time: at(8),
		author: "lsegal",
		text: "Keep the jitter, please.",
		url: "https://github.com/lsegal/aviary/issues/218#issuecomment-1",
	},
	{
		kind: "command",
		time: at(7),
		call_id: "b2",
		command: "go test ./internal/scheduler/...",
		status: "running",
	},
	{
		kind: "command",
		time: at(7),
		call_id: "b2",
		output: "ok  internal/scheduler 2.41s",
		exit_code: 0,
		status: "done",
	},
	{
		kind: "usage",
		time: at(6),
		usage: { input_tokens: 182000, output_tokens: 9100, cost_usd: 1.42 },
	},
];

// The typed events and raw stdout project_job_get returns per job; tests may
// grow them to stand in for a running job.
export const EVENTS: Record<number, object[]> = { 218: EVENTS_218 };
export const RAW_OUTPUT: Record<number, string> = {
	218: '{"type":"assistant","message":{"content":[{"type":"text","text":"I can reproduce it"}]}}\n',
};

// project_job_list's project filter: the board passes targets, one or more.
// The board's toggle chip for one job state.
export const stateChip = (label: string) =>
	page
		.getByRole("group", { name: "Job filter" })
		.getByRole("button", { name: new RegExp(`^${label}`) });

export const inTargets = (
	args: Record<string, unknown> | undefined,
	target: string,
) => !Array.isArray(args?.targets) || args.targets.includes(target);

export interface Calls {
	[tool: string]: Record<string, unknown>[];
}

/** Mocks the server for the Code Loop boards and returns the tool calls. */
export function setup(
	status: () => object = () => STATUS,
	extra: ToolFixtures = {},
) {
	const calls: Calls = {};
	const record = (tool: string, args?: Record<string, unknown>) => {
		calls[tool] ??= [];
		calls[tool].push(args ?? {});
	};
	let tick = 0;
	const scrollback = (number: number) => {
		tick++;
		return [
			`▸ read internal/scheduler/queue.go (#${number})`,
			"$ go test ./internal/scheduler/...",
			`ok  internal/scheduler tick ${tick}`,
		].join("\n");
	};
	const ok = (tool: string) => (args?: Record<string, unknown>) => {
		record(tool, args);
		return { ok: true };
	};
	mockMCP({
		agent_list: [],
		// coder owns both watched projects, so their boards sit under it.
		config_get: {
			agents: [
				{
					name: "coder",
					model: "cli/claude/opus",
					projects: [{ target: "lsegal/aviary" }, { target: "lsegal/zvidlib" }],
				},
			],
		},
		// Like the Manager, project_status counts the jobs of the projects
		// it is passed in each board state.
		project_status: (args?: Record<string, unknown>) => {
			record("project_status", args);
			const states: Record<string, number> = {
				starting: 0,
				paused: 0,
				running: 0,
				done: 0,
				failed: 0,
			};
			for (const j of JOBS) if (inTargets(args, j.target)) states[j.state]++;
			return { states, ...status() };
		},
		project_job_list: (args?: Record<string, unknown>) => {
			record("project_job_list", args);
			const jobs = JOBS.filter(
				(j) =>
					inTargets(args, j.target) &&
					(!args?.status || j.status === args.status) &&
					(!args?.states || (args.states as string[]).includes(j.state)),
			);
			return { jobs, total: jobs.length, offset: 0, limit: args?.limit ?? 50 };
		},
		project_job_get: (args?: Record<string, unknown>) => {
			record("project_job_get", args);
			const found = JOBS.find(
				(j) => j.target === args?.target && j.number === args?.number,
			);
			if (!found) throw new Error("no such job");
			return {
				...found,
				...CLONES[found.number],
				session_id: "7f3c…a9",
				scrollback: scrollback(found.number),
				events: EVENTS[found.number] ?? [],
				raw_output: RAW_OUTPUT[found.number] ?? "",
			};
		},
		project_job_retry: ok("project_job_retry"),
		project_job_stop: ok("project_job_stop"),
		project_job_clear: ok("project_job_clear"),
		project_refresh: ok("project_refresh"),
		project_github_signin: ok("project_github_signin"),
		project_github_use_polling: ok("project_github_use_polling"),
		project_github_import: ok("project_github_import"),
		...extra,
	});
	return calls;
}

export const jobGetWithEvents =
	(events: () => unknown[]) => (args?: Record<string, unknown>) => {
		const found = JOBS.find(
			(j) => j.target === args?.target && j.number === args?.number,
		);
		if (!found) throw new Error("no such job");
		return {
			...found,
			...CLONES[found.number],
			scrollback: `$ go test ./... (#${found.number})`,
			events: found.number === 218 ? events() : [],
			raw_output: "",
		};
	};

export const SIGNED_OUT = {
	signed_out: true,
	signing_in: false,
	url: "https://github.com/lsegal/aviary/issues?q=is%3Aissue+state%3Aopen+label%3Aready",
	status: 404,
	since: minutesAgo(2),
};
export const browserStatus = (github: object) => ({
	...STATUS,
	issue_filter: "",
	delivery: "browser",
	poll_interval: "20s",
	github,
});

export const PNG =
	"data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==";

/** Fixtures for the built-in Code Loop agent behind the prompt chat. */
export function chatFixtures(calls: Calls): ToolFixtures {
	const record = (tool: string, args?: Record<string, unknown>) => {
		calls[tool] ??= [];
		calls[tool].push(args ?? {});
	};
	return {
		agent_list: [
			{
				id: "projects",
				name: "projects",
				model: "anthropic/claude-sonnet-5-5",
				state: "idle",
			},
		],
		session_list: (args?: Record<string, unknown>) => {
			record("session_list", args);
			return [
				{ id: "main", agent_id: "projects", name: "main", created_at: at(60) },
			];
		},
		session_messages: (args?: Record<string, unknown>) => {
			record("session_messages", args);
			return chatLog(calls);
		},
		agent_run: (args?: Record<string, unknown>) => {
			record("agent_run", args);
			return { stream: ["#218 is clamping the retry delay."], result: "" };
		},
	};
}

/** The prompt chat's persisted log: two seeded messages, then every prompt. */
export function chatLog(calls: Calls) {
	return [
		{
			id: "m1",
			session_id: "projects-chat",
			role: "user",
			content: "What does the board look like?",
			media_urls: [PNG],
			timestamp: at(5),
		},
		{
			id: "m2",
			session_id: "projects-chat",
			role: "assistant",
			content: "Here is the failing run.",
			media_urls: [PNG],
			timestamp: at(4),
		},
		// What the server persists once a prompt has run.
		...(calls.agent_run ?? []).flatMap((run, i) => [
			{
				id: `r${i}`,
				session_id: "projects-chat",
				role: "user",
				content: run.message,
				timestamp: at(0),
			},
			{
				id: `a${i}`,
				session_id: "projects-chat",
				role: "assistant",
				content: "#218 is clamping the retry delay.",
				timestamp: at(0),
			},
		]),
	];
}
