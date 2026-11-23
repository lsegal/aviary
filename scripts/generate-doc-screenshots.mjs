// Renders the docs site's screenshots (docs/site/public/screenshots) from the
// web UI against the fixtures below, with every MCP call mocked. Run it from
// the repository root with the web dev server up:
//
//   node scripts/generate-doc-screenshots.mjs [name-prefix...]
//
// DOCS_SCREENSHOT_BASE_URL overrides the dev server's URL. See
// "Regenerating Docs Screenshots" in CONTRIBUTING.md.
import { chromium } from "@playwright/test";
import { mkdir } from "node:fs/promises";
import path from "node:path";
import process from "node:process";

const repoRoot = process.cwd();
const outputDir = path.join(
	repoRoot,
	"docs",
	"site",
	"public",
	"screenshots",
);
const baseURL =
	process.env.DOCS_SCREENSHOT_BASE_URL ?? "https://127.0.0.1:5173";
const themes = ["light", "dark"];
// Optional name prefixes passed on the command line (for example `projects`)
// limit the run to matching screenshots.
const only = process.argv.slice(2);

function wanted(outputName) {
	return only.length === 0 || only.some((name) => outputName.startsWith(name));
}

function themedOutputName(outputName, theme) {
	const extensionIndex = outputName.lastIndexOf(".");
	if (extensionIndex === -1) return `${outputName}-${theme}`;
	return `${outputName.slice(0, extensionIndex)}-${theme}${outputName.slice(extensionIndex)}`;
}

function iso(daysAgo, hour, minute = 0) {
	const date = new Date();
	date.setDate(date.getDate() - daysAgo);
	date.setHours(hour, minute, 0, 0);
	return date.toISOString();
}

function usageRecord({
	daysAgo,
	hour,
	session,
	agent,
	model,
	provider,
	input,
	output,
	cacheRead = 0,
	cacheWrite = 0,
	toolCalls = 0,
	hasError = false,
	hasThrottle = false,
}) {
	return {
		timestamp: iso(daysAgo, hour),
		session_id: session,
		agent_id: agent,
		model,
		provider,
		input_tokens: input,
		output_tokens: output,
		cache_read_tokens: cacheRead,
		cache_write_tokens: cacheWrite,
		tool_calls: toolCalls,
		has_error: hasError,
		has_throttle: hasThrottle,
	};
}

const docsVersion = "0.7.3";

const versionPayload = {
	latestVersion: docsVersion,
	upgradeAvailable: false,
	message: "",
};

const overviewFixtures = {
	agent_list: [
		{
			id: "a1",
			name: "assistant",
			model: "anthropic/claude-sonnet-4-5",
			fallbacks: ["openai/gpt-5-mini"],
			state: "idle",
		},
		{
			id: "a2",
			name: "researcher",
			model: "openai/gpt-5",
			fallbacks: [],
			state: "running",
		},
		{
			id: "a3",
			name: "ops",
			model: "google/gemini-2.5-pro",
			fallbacks: [],
			state: "idle",
		},
	],
	job_list: [
		{
			id: "job-1",
			task_id: "daily-inbox-triage",
			agent_id: "assistant",
			status: "completed",
			attempts: 1,
			max_retries: 3,
			created_at: iso(0, 7, 30),
			updated_at: iso(0, 7, 31),
		},
		{
			id: "job-2",
			task_id: "calendar-prep",
			agent_id: "assistant",
			status: "in_progress",
			attempts: 1,
			max_retries: 3,
			created_at: iso(0, 8, 15),
			updated_at: iso(0, 8, 16),
		},
		{
			id: "job-3",
			task_id: "competitor-watch",
			agent_id: "researcher",
			status: "failed",
			attempts: 2,
			max_retries: 3,
			created_at: iso(0, 6, 0),
			updated_at: iso(0, 6, 2),
		},
	],
	config_validate: [],
};

const chatSessions = [
	{
		id: "sess-main",
		agent_id: "agent_assistant",
		name: "main",
		created_at: iso(1, 10),
		is_processing: false,
	},
	{
		id: "sess-triage",
		agent_id: "agent_assistant",
		name: "morning planning",
		created_at: iso(0, 8),
		is_processing: false,
	},
];

const chatFixtures = {
	agent_list: [
		{
			id: "a1",
			name: "assistant",
			model: "anthropic/claude-sonnet-4-5",
			fallbacks: ["openai/gpt-5-mini"],
			state: "idle",
		},
		{
			id: "a2",
			name: "researcher",
			model: "openai/gpt-5",
			fallbacks: [],
			state: "idle",
		},
	],
	session_messages: [
		{
			id: "m1",
			role: "user",
			content:
				"Plan my morning: search for today’s OpenAI docs updates, check my calendar, and summarize unread release email.",
			timestamp: iso(0, 8, 10),
		},
		{
			id: "m2",
			role: "tool",
			content: JSON.stringify({
				name: "web_search",
				args: { query: "OpenAI docs updates March 2026", count: 3 },
				result:
					"1. GPT-5.4 prompt upgrade guide\n2. Responses API updates\n3. Docs changelog entry for tool calling",
			}),
			timestamp: iso(0, 8, 10),
		},
		{
			id: "m3",
			role: "tool",
			content: JSON.stringify({
				name: "calendar_events_today",
				args: { date: "2026-03-29" },
				result:
					"09:00 Docs review\n11:30 Customer sync\n14:00 Launch checklist\n16:30 Inbox zero block",
			}),
			timestamp: iso(0, 8, 11),
		},
		{
			id: "m4",
			role: "tool",
			content: JSON.stringify({
				name: "email_inbox_list",
				args: { mailbox: "inbox", unread_only: true, max_results: 5 },
				result:
					"3 unread: landing page screenshots, release notes draft, calendar hold from product",
			}),
			timestamp: iso(0, 8, 11),
		},
		{
			id: "m5",
			role: "assistant",
			content:
				"I took a quick pass through everything.\n\nThe docs updates look pretty minor, mostly prompt and tool-calling changes, so nothing feels urgent there. Your first meeting is the docs review at 9, which gives you a clean block right now if you want to knock out the screenshot work first. In email, the thread that matters most is the one about landing page screenshots, then the release-notes draft after that.\n\nIf you want, I can turn this into a short morning brief and sketch the screenshot to-do list next.",
			timestamp: iso(0, 8, 12),
			model: "anthropic/claude-sonnet-4-5",
		},
	],
};

// A Code Loop record reports an issue's job wall time or its outcome instead
// of token use.
function projectUsage(issue, daysAgo, hour, project) {
	return {
		timestamp: iso(daysAgo, hour),
		session_id: issue,
		agent_id: "coder",
		model: "",
		provider: "",
		input_tokens: 0,
		output_tokens: 0,
		project: { issue, ...project },
	};
}

const usageFixtures = {
	usage_query: [
		projectUsage("lsegal/aviary#209", 1, 10, { duration_ms: 38 * 60_000 }),
		projectUsage("lsegal/aviary#209", 1, 11, {
			outcome: "merged",
			additions: 84,
			deletions: 12,
		}),
		projectUsage("lsegal/aviary#200", 3, 14, { duration_ms: 52 * 60_000 }),
		projectUsage("lsegal/aviary#200", 3, 15, {
			outcome: "merged",
			additions: 312,
			deletions: 41,
		}),
		projectUsage("lsegal/forgo#80", 0, 9, { duration_ms: 64 * 60_000 }),
		projectUsage("lsegal/forgo#80", 0, 10, { outcome: "failed" }),
		usageRecord({
			daysAgo: 0,
			hour: 8,
			session: "sess-main",
			agent: "assistant",
			model: "anthropic/claude-sonnet-4-5",
			provider: "anthropic",
			input: 1800,
			output: 2200,
			cacheRead: 300,
			toolCalls: 3,
		}),
		usageRecord({
			daysAgo: 0,
			hour: 9,
			session: "sess-triage",
			agent: "assistant",
			model: "anthropic/claude-sonnet-4-5",
			provider: "anthropic",
			input: 1400,
			output: 1700,
			cacheRead: 250,
			toolCalls: 2,
		}),
		usageRecord({
			daysAgo: 0,
			hour: 10,
			session: "sess-research",
			agent: "researcher",
			model: "openai/gpt-5",
			provider: "openai",
			input: 2500,
			output: 2100,
			cacheRead: 600,
			cacheWrite: 120,
			toolCalls: 5,
		}),
		usageRecord({
			daysAgo: 1,
			hour: 11,
			session: "sess-ops",
			agent: "ops",
			model: "google/gemini-2.5-pro",
			provider: "google",
			input: 900,
			output: 600,
			toolCalls: 1,
		}),
		usageRecord({
			daysAgo: 1,
			hour: 13,
			session: "sess-ops",
			agent: "ops",
			model: "google/gemini-2.5-pro",
			provider: "google",
			input: 1100,
			output: 850,
			cacheRead: 180,
			toolCalls: 2,
			hasThrottle: true,
		}),
		usageRecord({
			daysAgo: 2,
			hour: 7,
			session: "sess-main",
			agent: "assistant",
			model: "anthropic/claude-sonnet-4-5",
			provider: "anthropic",
			input: 1500,
			output: 1300,
			cacheRead: 220,
			toolCalls: 2,
		}),
		usageRecord({
			daysAgo: 2,
			hour: 15,
			session: "sess-research-2",
			agent: "researcher",
			model: "openai/gpt-5",
			provider: "openai",
			input: 3200,
			output: 2800,
			cacheRead: 700,
			cacheWrite: 150,
			toolCalls: 6,
			hasError: true,
		}),
		usageRecord({
			daysAgo: 3,
			hour: 9,
			session: "sess-main",
			agent: "assistant",
			model: "anthropic/claude-sonnet-4-5",
			provider: "anthropic",
			input: 1000,
			output: 1200,
			cacheRead: 200,
			toolCalls: 2,
		}),
		usageRecord({
			daysAgo: 4,
			hour: 14,
			session: "sess-support",
			agent: "assistant",
			model: "anthropic/claude-sonnet-4-5",
			provider: "anthropic",
			input: 1600,
			output: 1500,
			cacheRead: 240,
			toolCalls: 4,
		}),
		usageRecord({
			daysAgo: 5,
			hour: 16,
			session: "sess-ops-2",
			agent: "ops",
			model: "google/gemini-2.5-pro",
			provider: "google",
			input: 800,
			output: 950,
			cacheRead: 100,
			toolCalls: 1,
		}),
		usageRecord({
			daysAgo: 6,
			hour: 12,
			session: "sess-research-3",
			agent: "researcher",
			model: "openai/gpt-5",
			provider: "openai",
			input: 2900,
			output: 2500,
			cacheRead: 640,
			cacheWrite: 140,
			toolCalls: 5,
		}),
	],
};

// The configuration every page loads: the sidebar lists its agents and their
// sessions, tasks and channels.
const docsConfig = {
	server: {
		port: 16677,
		tls: { cert: "", key: "" },
		external_access: false,
		no_tls: false,
	},
	agents: [
		{
			name: "assistant",
			model: "anthropic/claude-sonnet-4-5",
			fallbacks: ["openai/gpt-5-mini"],
			working_dir: "/workspace/aviary",
			verbose: true,
			permissions: {
				preset: "minimal",
				tools: [
					"web_search",
					"calendar_events_today",
					"email_inbox_list",
					"session_list",
					"task_run",
				],
				disabled_tools: ["browser_open"],
				filesystem: {
					allowed_paths: ["./docs/**", "!./docs/private/**", "./README.md"],
				},
				exec: {
					allowed_commands: ["git status", "pnpm lint", "!rm *", "!git push *"],
					shell_interpolate: false,
					shell: "/bin/bash -lc",
				},
			},
			channels: [
				{
					enabled: true,
					type: "slack",
					id: "design-ops-bot",
					primary: "U123PRIMARY",
					url: "xapp-1-live-example",
					token: "xoxb-1-live-example",
					model: "openai/gpt-5-mini",
					fallbacks: ["anthropic/claude-sonnet-4-5"],
					disabled_tools: ["browser_open"],
					show_typing: true,
					reply_to_replies: true,
					react_to_emoji: true,
					send_read_receipts: false,
					group_chat_history: 30,
					allow_from: [
						{
							enabled: true,
							from: "U123PRIMARY, U456PM",
							allowed_groups: "CENG, CDOCS",
							mention_prefixes: ["aviary", "assistant"],
							exclude_prefixes: ["!"],
							respond_to_mentions: true,
							mention_prefix_group_only: true,
							model: "anthropic/claude-sonnet-4-5",
							fallbacks: ["openai/gpt-5-mini"],
							restrict_tools: [
								"web_search",
								"calendar_events_today",
								"email_inbox_list",
							],
						},
						{
							enabled: true,
							from: "*",
							allowed_groups: "CDOCS",
							mention_prefixes: ["aviary"],
							exclude_prefixes: ["/"],
							respond_to_mentions: false,
							mention_prefix_group_only: false,
							restrict_tools: ["web_search"],
						},
					],
				},
			],
			tasks: [
				{
					name: "daily-inbox-triage",
					type: "script",
					trigger: "weekly",
					days: "mon,tue,wed,thu,fri",
					at: "07:00",
					prompt:
						"Check unread email, compare against today’s meetings, and draft a morning brief.",
				},
				{
					name: "calendar-prep",
					trigger: "daily",
					at: "08:30",
					prompt: "Summarize today’s meetings and prep notes.",
				},
				{
					name: "weekly-review",
					trigger: "weekly",
					days: "fri",
					at: "16:00",
					enabled: false,
					prompt: "Draft a weekly review from closed tasks and sent email.",
				},
			],
		},
		{
			name: "researcher",
			model: "openai/gpt-5",
			tasks: [
				{
					name: "competitor-watch",
					every: "4h",
					prompt: "Track competitor launches and pricing changes.",
				},
			],
		},
		{ name: "ops", model: "google/gemini-2.5-pro" },
	],
	models: {
		providers: {},
		defaults: {
			model: "anthropic/claude-sonnet-4-5",
			fallbacks: ["openai/gpt-5-mini"],
		},
	},
	browser: { binary: "", cdp_port: 9222, headless: true },
	search: { web: { brave_api_key: "" } },
	scheduler: { concurrency: "", precompute_tasks: true },
	skills: {
		calendar: { enabled: true },
		email: { enabled: true },
	},
};

const settingsToolList = [
	{
		name: "web_search",
		description: "Search the web and return the top matching pages.",
		inputSchema: {
			type: "object",
			required: ["query"],
			properties: {
				query: { type: "string" },
				count: { type: "integer", default: 3 },
			},
		},
	},
	{
		name: "calendar_events_today",
		description: "Return the user’s events for the current day.",
		inputSchema: {
			type: "object",
			properties: {
				date: { type: "string" },
			},
		},
	},
	{
		name: "email_inbox_list",
		description: "List the newest inbox messages for a mailbox.",
		inputSchema: {
			type: "object",
			properties: {
				mailbox: { type: "string" },
				unread_only: { type: "boolean" },
			},
		},
	},
	{
		name: "browser_open",
		description: "Open a page in the shared browser session.",
		inputSchema: {
			type: "object",
			required: ["url"],
			properties: {
				url: { type: "string" },
			},
		},
	},
	{
		name: "session_list",
		description: "List sessions for an agent.",
		inputSchema: {
			type: "object",
			properties: {
				agent: { type: "string" },
			},
		},
	},
	{
		name: "task_run",
		description: "Run a scheduled task immediately.",
		inputSchema: {
			type: "object",
			properties: {
				name: { type: "string" },
			},
		},
	},
	{
		name: "config_get",
		description: "Read the active Aviary configuration.",
		inputSchema: { type: "object", properties: {} },
	},
	{
		name: "config_save",
		description: "Persist the active Aviary configuration.",
		inputSchema: {
			type: "object",
			properties: {
				config: { type: "string" },
			},
		},
	},
];

// Mocks every page needs, which a capture's own fixtures override.
const commonFixtures = {
	config_get: docsConfig,
	config_validate: [],
	server_version_check: {
		currentVersion: docsVersion,
		latestVersion: docsVersion,
		upgradeAvailable: false,
		upgradeSupported: true,
		restart: { phase: "idle", jobs: [], held: false },
	},
	session_list: (args) => (args.agent === "assistant" ? chatSessions : []),
	// Called with a saved task's agent/name, or with an edited task's fields.
	task_preview: (args) => {
		const [agent, name] = String(args.name ?? "").split("/");
		const task =
			docsConfig.agents
				.find((a) => a.name === agent)
				?.tasks?.find((t) => t.name === name) ?? args;
		return {
			description: task.every
				? `every ${task.every}`
				: task.trigger === "weekly"
					? `weekly on ${task.days} at ${task.at}`
					: `daily at ${task.at}`,
			next: task.enabled === false ? [] : [iso(-1, 7)],
		};
	},
};

const settingsFixtures = {
	auth_list: ["anthropic:default", "openai:default", "google:default"],
	tool_list: settingsToolList,
};

const systemSkillsList = [
	{
		name: "calendar",
		description: "Read upcoming events and create short day plans.",
		path: "skills/calendar/SKILL.md",
		source: "builtin",
		enabled: true,
	},
	{
		name: "email",
		description: "Triage inboxes and summarize unread threads into action items.",
		path: "skills/email/SKILL.md",
		source: "builtin",
		enabled: true,
	},
	{
		name: "notion",
		description: "Look up notes, docs, and project pages from your workspace.",
		path: "skills/notion/SKILL.md",
		source: "builtin",
		enabled: false,
	},
	{
		name: "gogcli",
		description: "Search GOG libraries and metadata for game-related workflows.",
		path: "skills/gogcli/SKILL.md",
		source: "disk",
		enabled: false,
	},
];

const systemToolsFixtures = {
	tool_list: settingsToolList,
	skills_list: systemSkillsList,
	calendar_events_today:
		"09:00 Docs review\n11:30 Customer sync\n14:00 Launch checklist\n16:30 Inbox zero block",
	web_search:
		"1. aviary.bot landing page draft\n2. docs screenshot checklist\n3. customer feedback thread about docs images",
};

const systemSkillsFixtures = {
	skills_list: systemSkillsList,
	config_get: {
		server: {
			port: 16677,
			tls: { cert: "", key: "" },
			external_access: false,
			no_tls: false,
		},
		agents: [],
		models: { providers: {}, defaults: { model: "", fallbacks: [] } },
		browser: { binary: "", cdp_port: 9222, headless: true },
		search: { web: { brave_api_key: "" } },
		scheduler: { concurrency: "", precompute_tasks: true },
		skills: {
			calendar: { enabled: true },
			email: { enabled: true },
			notion: { enabled: false },
			gogcli: { enabled: false },
		},
	},
};

const compileDetail = {
	id: "compile-1",
	agent_id: "assistant",
	task_name: "daily-inbox-triage",
	requested_task_type: "prompt",
	result_task_type: "script",
	trigger: "cron",
	target: "email+calendar",
	prompt:
		"Check unread email, compare against today’s meetings, and draft a morning brief.",
	validated: true,
	deterministic_steps: 4,
	status: "succeeded",
	reason: "Workflow is deterministic and safe to compile into a script.",
	created_at: iso(0, 7, 0),
	updated_at: iso(0, 7, 1),
	stages: [
		{
			name: "Discovery",
			status: "succeeded",
			system_prompt: "Analyze the task for deterministic steps.",
			user_prompt: "Check unread email and calendar to prepare a morning brief.",
			response:
				"Calendar lookup, inbox listing, and templated summary are deterministic.",
			started_at: iso(0, 7, 0),
		},
		{
			name: "Compilation",
			status: "succeeded",
			system_prompt: "Generate a runnable script.",
			user_prompt: "Produce the final script.",
			response: "Compiled script produced successfully.",
			started_at: iso(0, 7, 1),
		},
	],
	script:
		"events = calendar_events_today()\nmail = email_inbox_list(unread_only=true)\nprint(render_morning_brief(events, mail))",
};

function taskRun(n, task, status, daysAgo, hour, seconds, extra = {}) {
	const created = new Date(iso(daysAgo, hour));
	return {
		id: `job_${String(n).padStart(8, "0")}`,
		task_id: `assistant/${task}`,
		agent_id: "assistant",
		prompt: "",
		status,
		attempts: status === "failed" ? 2 : 1,
		max_retries: 3,
		scheduled_for: created.toISOString(),
		created_at: created.toISOString(),
		updated_at: new Date(created.getTime() + seconds * 1000).toISOString(),
		...extra,
	};
}

const jobsFixtures = {
	job_query: [
		taskRun(12, "calendar-prep", "in_progress", 0, 8, 30),
		taskRun(11, "daily-inbox-triage", "completed", 0, 7, 41),
		taskRun(10, "calendar-prep", "completed", 1, 8, 52),
		taskRun(9, "daily-inbox-triage", "completed", 1, 7, 38),
		taskRun(8, "calendar-prep", "failed", 2, 8, 95, {
			error: "calendar_events_today: provider returned 503",
		}),
		taskRun(7, "daily-inbox-triage", "completed", 2, 7, 44),
		taskRun(6, "calendar-prep", "completed", 3, 8, 47),
		taskRun(5, "daily-inbox-triage", "completed", 3, 7, 36),
		taskRun(4, "weekly-review", "completed", 4, 16, 120),
		taskRun(15, "daily-inbox-triage", "completed", 4, 7, 39),
		taskRun(14, "calendar-prep", "completed", 4, 8, 51),
		taskRun(13, "calendar-prep", "completed", 5, 8, 46),
		taskRun(3, "daily-inbox-triage", "completed", 6, 7, 40),
		taskRun(2, "calendar-prep", "completed", 6, 8, 49),
	],
	task_compile_query: [compileDetail],
	task_compile_get: compileDetail,
};

function minutesAgo(minutes) {
	return new Date(Date.now() - minutes * 60_000).toISOString();
}

// The Code Loop fixtures follow the payloads internal/projects' Manager
// returns for project_status, project_job_list and project_job_get (see
// web/e2e/projects.spec.ts). Keep them to public repositories.
const projectTargets = ["lsegal/aviary", "lsegal/forgo"];

// The board state core.JobSnapshot.BoardState reports for each job status.
const projectStateOf = {
	queued: "starting",
	active: "running",
	waiting: "paused",
	paused: "paused",
	complete: "done",
	failed: "failed",
};

function projectJob({
	target = "lsegal/aviary",
	number,
	title,
	status,
	state,
	agent,
	model,
	started,
	pr,
	prState = "open",
	progress,
	waitReason,
}) {
	return {
		key: `${target}#${number}`,
		target,
		number,
		title,
		status,
		state: state ?? projectStateOf[status],
		kind: "issue",
		agent,
		model,
		actions: {
			retry: status !== "queued",
			pause: status === "active" || status === "waiting",
			resume: status === "paused",
			stop: ["active", "waiting", "paused"].includes(status),
		},
		repository: target,
		issue_url: `https://github.com/${target}/issues/${number}`,
		...(pr
			? {
					pull_request: pr,
					pull_request_url: `https://github.com/${target}/pull/${pr}`,
					pull_request_state: prState,
				}
			: {}),
		...(progress
			? {
					progress: {
						done: progress[0],
						total: progress[1],
						percent: Math.round((progress[0] / progress[1]) * 100),
					},
				}
			: {}),
		started: minutesAgo(started),
		...(waitReason ? { wait_reason: waitReason } : {}),
	};
}

const projectJobs = [
	projectJob({
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
	projectJob({
		target: "lsegal/forgo",
		number: 87,
		title: "Hot reload drops methods added to generic types",
		status: "active",
		agent: "codex",
		model: "gpt-5.5",
		started: 22,
		pr: 91,
		prState: "draft",
		progress: [3, 6],
	}),
	projectJob({
		number: 221,
		title: "Export a session as Markdown from the chat menu",
		status: "active",
		state: "starting",
		agent: "codex",
		model: "gpt-5.5",
		started: 1,
	}),
	projectJob({
		number: 214,
		title: "Show token usage per scheduled task",
		status: "paused",
		agent: "claude",
		model: "opus",
		started: 41,
		pr: 229,
		progress: [5, 6],
		waitReason: "Stacked on PR #226, waiting for it to merge",
	}),
	projectJob({
		target: "lsegal/forgo",
		number: 80,
		title: "cmd/go build cache ignores -tags from GOFLAGS",
		status: "failed",
		agent: "claude",
		model: "opus",
		started: 70,
		pr: 84,
		progress: [4, 6],
	}),
	projectJob({
		number: 209,
		title: "Scoop install fails when ~/.local/bin is missing",
		status: "complete",
		agent: "claude",
		model: "opus",
		started: 95,
		pr: 227,
		prState: "merged",
		progress: [6, 6],
	}),
	projectJob({
		number: 200,
		title: "Add a dark theme toggle",
		status: "complete",
		agent: "codex",
		model: "gpt-5.5",
		started: 300,
		pr: 205,
		prState: "merged",
		progress: [5, 5],
	}),
];

const projectRunEvents = [
	{ kind: "system", time: minutesAgo(14), text: "Claimed #218" },
	{ kind: "system", time: minutesAgo(13), text: "Opened draft PR #233" },
	{
		kind: "message",
		time: minutesAgo(12),
		text: "I can reproduce it: retries back off **without a cap**. I'll clamp the delay at 10 minutes and keep the jitter.",
	},
	{
		kind: "plan",
		time: minutesAgo(11),
		items: [
			{ text: "Reproduce the unbounded backoff", status: "completed" },
			{ text: "Clamp the delay", status: "completed" },
			{ text: "Add a test", status: "in_progress" },
			{ text: "Repair CI", status: "pending" },
			{ text: "Merge", status: "pending" },
		],
	},
	{
		kind: "tool_call",
		time: minutesAgo(11),
		call_id: "c1",
		name: "Read",
		input: { file_path: "internal/scheduler/queue.go" },
	},
	{
		kind: "tool_result",
		time: minutesAgo(11),
		call_id: "c1",
		output: "package scheduler\n…",
	},
	{
		kind: "file_change",
		time: minutesAgo(10),
		path: "internal/scheduler/queue.go",
		diff: "@@ -40,3 +40,3 @@\n delay := base << attempt\n-return delay\n+return min(delay, 10*time.Minute)",
	},
	{
		kind: "comment_relay",
		time: minutesAgo(9),
		author: "lsegal",
		text: "Keep the jitter, please.",
		url: "https://github.com/lsegal/aviary/issues/218#issuecomment-1",
	},
	{
		kind: "command",
		time: minutesAgo(8),
		call_id: "b1",
		command: "go test ./internal/scheduler/...",
		status: "running",
	},
	{
		kind: "command",
		time: minutesAgo(8),
		call_id: "b1",
		output: "--- FAIL: TestBackoffCap (0.01s)",
		exit_code: 1,
		is_error: true,
		status: "done",
	},
	{
		kind: "file_change",
		time: minutesAgo(7),
		path: "internal/scheduler/queue_test.go",
		diff: "@@ -88,2 +88,3 @@\n-want := 20 * time.Minute\n+want := 10 * time.Minute\n+// jitter stays within 10%",
	},
	{
		kind: "command",
		time: minutesAgo(6),
		call_id: "b2",
		command: "go test ./internal/scheduler/...",
		status: "running",
	},
	{
		kind: "command",
		time: minutesAgo(6),
		call_id: "b2",
		output: "ok  internal/scheduler 2.41s",
		exit_code: 0,
		status: "done",
	},
	{
		kind: "usage",
		time: minutesAgo(5),
		usage: { input_tokens: 182000, output_tokens: 9100, cost_usd: 1.42 },
	},
];

// The latest few events of every other job, which its card's output shows.
function projectCommand(id, minutes, command, output, failed = false) {
	return [
		{ kind: "command", time: minutesAgo(minutes), call_id: id, command, status: "running" },
		{
			kind: "command",
			time: minutesAgo(minutes),
			call_id: id,
			output,
			exit_code: failed ? 1 : 0,
			is_error: failed,
			status: "done",
		},
	];
}

const projectEvents = {
	218: projectRunEvents,
	87: [
		{
			kind: "file_change",
			time: minutesAgo(9),
			path: "src/cmd/hotreload/methods.go",
			diff: "@@ -12,2 +12,6 @@\n+for _, inst := range named.Instances() {\n+\tpatch(inst)\n+}",
		},
		...projectCommand("f1", 6, "go test ./cmd/hotreload/...", "ok  cmd/hotreload 4.87s"),
		{
			kind: "message",
			time: minutesAgo(2),
			text: "Generic instances now pick up new methods. Running the runtime tests next.",
		},
	],
	221: [
		{ kind: "system", time: minutesAgo(1), text: "Claimed #221" },
		{ kind: "system", time: minutesAgo(0.5), text: "Cloning lsegal/aviary" },
	],
	214: [
		...projectCommand("p1", 30, "pnpm test:e2e usage", "12 passed (18.4s)"),
		{ kind: "system", time: minutesAgo(28), text: "Marked PR #229 ready for review" },
		{ kind: "system", time: minutesAgo(27), text: "Paused: stacked on PR #226" },
	],
	80: [
		{
			kind: "file_change",
			time: minutesAgo(50),
			path: "src/cmd/go/internal/work/buildid.go",
			diff: "@@ -201,1 +201,2 @@\n+h.Write(cfg.BuildTags)",
		},
		...projectCommand("g1", 44, "go test cmd/go -run Script/build_cache", "--- FAIL: TestScript/build_cache_tags", true),
		{ kind: "system", time: minutesAgo(40), text: "CI failed on PR #84" },
	],
	209: [
		...projectCommand("s1", 60, "go test ./installer/...", "ok  installer 0.92s"),
		{ kind: "system", time: minutesAgo(52), text: "CI passed on PR #227" },
		{ kind: "system", time: minutesAgo(50), text: "Merged PR #227" },
	],
	200: [
		...projectCommand("d1", 260, "pnpm lint", "Checked 412 files. No fixes applied."),
		{ kind: "system", time: minutesAgo(250), text: "Merged PR #205" },
	],
};

function projectInTargets(args, target) {
	return !Array.isArray(args.targets) || args.targets.includes(target);
}

const projectsFixtures = {
	agent_list: [],
	config_get: {
		agents: [
			{
				name: "coder",
				model: "cli/claude/opus",
				projects: projectTargets.map((target) => ({ target })),
			},
		],
	},
	// Like the Manager, project_status counts the jobs of the projects it is
	// passed in each board state.
	project_status: (args) => {
		const states = { starting: 0, paused: 0, running: 0, done: 0, failed: 0 };
		for (const job of projectJobs) {
			if (projectInTargets(args, job.target)) states[job.state]++;
		}
		return {
			identity: "7F3CA91B",
			running: true,
			targets: projectTargets,
			issue_counts: { "lsegal/aviary": 6, "lsegal/forgo": 3 },
			states,
			jobs: {
				running: 3,
				queued: 0,
				paused: 1,
				completed: 12,
				merged_24h: 4,
				failed: 1,
			},
			concurrency: 4,
			delivery: "browser",
			poll_interval: "20s",
			issue_filter: "assignee:@me",
			allowed_commenters: ["lsegal"],
			quotas: { claude: "62% of 5h quota left", codex: "quota ok" },
			agents: ["claude/opus", "codex"],
			clis: [
				{ name: "claude", models: ["opus"], quota: "62% of 5h quota left" },
				{ name: "codex", models: ["gpt-5.5"], quota: "quota ok" },
			],
			recent_logs:
				"12:03:58 checked lsegal/aviary: 6 ready issues\n12:04:11 claimed #221, cloning lsegal/aviary",
			log_offset: 2,
			last_check: minutesAgo(0.1),
		};
	},
	project_job_list: (args) => {
		const jobs = projectJobs.filter(
			(job) =>
				projectInTargets(args, job.target) &&
				(!args.status || job.status === args.status) &&
				(!Array.isArray(args.states) || args.states.includes(job.state)),
		);
		return { jobs, total: jobs.length, offset: 0, limit: args.limit ?? 50 };
	},
	project_job_get: (args) => {
		const job =
			projectJobs.find(
				(j) => j.target === args.target && j.number === args.number,
			) ?? projectJobs[0];
		return {
			...job,
			checkout_directory: `~/.aviary/clones/${job.target.split("/")[1]}-${job.number}`,
			checkout_state: job.state === "done" ? "removed" : "present",
			session_id: "7f3c…a9",
			scrollback: "",
			events: projectEvents[job.number] ?? [],
			raw_output: "",
		};
	},
};

// waitForServer loads url in the browser until it answers 2xx. The browser
// accepts the dev server's self-signed cert.
async function waitForServer(browser, url, timeoutMs = 30_000) {
	const started = Date.now();
	const page = await browser.newPage();
	try {
		for (;;) {
			try {
				const response = await page.goto(url);
				if (response?.ok()) return;
			} catch {
				// Keep waiting.
			}
			if (Date.now() - started > timeoutMs) {
				throw new Error(`Timed out waiting for ${url}`);
			}
			await new Promise((resolve) => setTimeout(resolve, 500));
		}
	} finally {
		await page.close();
	}
}

async function configurePage(page, fixtures) {
	fixtures = { ...commonFixtures, ...fixtures };
	await page.addInitScript((version) => {
		localStorage.setItem("aviary_token", "docs-shot-token");
		// The mocked server has no providers, so skip the setup wizard.
		sessionStorage.setItem("aviary_onboarding_skipped", "1");

		class MockWebSocket {
			static CONNECTING = 0;
			static OPEN = 1;
			static CLOSING = 2;
			static CLOSED = 3;

			constructor(url) {
				this.url = url;
				this.readyState = MockWebSocket.CONNECTING;
				this.onopen = null;
				this.onmessage = null;
				this.onerror = null;
				this.onclose = null;
				setTimeout(() => {
					this.readyState = MockWebSocket.OPEN;
					this.onopen?.({ type: "open", target: this });
					this.onmessage?.({
						type: "message",
						data: JSON.stringify({
							type: "health",
							ok: true,
							version,
							goos: "linux",
						}),
						target: this,
					});
				}, 20);
			}

			addEventListener() {}
			removeEventListener() {}
			send() {}
			close() {
				this.readyState = MockWebSocket.CLOSED;
				this.onclose?.({ type: "close", target: this });
			}
		}

		window.WebSocket = MockWebSocket;
	}, docsVersion);

	await page.route("**/api/version", async (route) => {
		await route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify(versionPayload),
		});
	});

	await page.route("**/api/login", async (route) => {
		await route.fulfill({
			status: 200,
			contentType: "application/json",
			body: "{}",
		});
	});

	await page.route("**/mcp", async (route) => {
		const body = route.request().postDataJSON();

		if (body.method === "initialize") {
			await route.fulfill({
				status: 200,
				headers: {
					"Content-Type": "application/json",
					"Mcp-Session-Id": "docs-shot-session",
				},
				body: JSON.stringify({
					jsonrpc: "2.0",
					id: body.id,
					result: {
						protocolVersion: "2024-11-05",
						capabilities: {},
						serverInfo: { name: "aviary-docs-shot", version: docsVersion },
					},
				}),
			});
			return;
		}

		if (body.method === "notifications/initialized") {
			await route.fulfill({ status: 200, body: "{}" });
			return;
		}

		if (body.method === "tools/list") {
			await route.fulfill({
				status: 200,
				contentType: "application/json",
				body: JSON.stringify({
					jsonrpc: "2.0",
					id: body.id,
					result: { tools: fixtures.tool_list ?? [] },
				}),
			});
			return;
		}

		if (body.method === "tools/call") {
			const name = body.params?.name ?? "";
			if (process.env.DOCS_SCREENSHOT_DEBUG && !(name in fixtures)) console.warn("UNMOCKED", page.url(), name, JSON.stringify(body.params?.arguments));
			const fixture = name in fixtures ? fixtures[name] : [];
			// A function fixture computes its result from the call's arguments.
			const result =
				typeof fixture === "function"
					? fixture(body.params?.arguments ?? {})
					: fixture;
			await route.fulfill({
				status: 200,
				contentType: "application/json",
				body: JSON.stringify({
					jsonrpc: "2.0",
					id: body.id,
					result: {
						content: [
							{
								type: "text",
								text:
									typeof result === "string" ? result : JSON.stringify(result),
							},
						],
					},
				}),
			});
			return;
		}

		await route.fulfill({ status: 200, body: "{}" });
	});
}

async function openPage(page, pathname, readySelector) {
	await page.goto(`${baseURL}${pathname}`);
	await page.waitForLoadState("networkidle");
	await page.waitForFunction(() => document.fonts?.status === "loaded");
	try { await page.locator(readySelector).waitFor({timeout: 8000}); } catch (e) { await page.screenshot({path: `/tmp/946dbg/${pathname.replaceAll("/","_")}.png`}); throw e; }
}

async function captureSimple({
	browser,
	fixtures,
	pathname,
	readySelector,
	outputName,
	viewport,
	theme,
	storage = {},
}) {
	if (!wanted(outputName)) return;
	const page = await browser.newPage({ viewport, colorScheme: theme });
	try {
		await configurePage(page, fixtures);
		// UI preferences the page reads from localStorage on load.
		await page.addInitScript((entries) => {
			for (const [key, value] of Object.entries(entries)) {
				localStorage.setItem(key, value);
			}
		}, storage);
		await openPage(page, pathname, readySelector);
		await page.screenshot({
			path: path.join(outputDir, themedOutputName(outputName, theme)),
			fullPage: false,
		});
	} finally {
		await page.close();
	}
}

async function captureTools(browser, theme) {
	if (!wanted("system-tools.png")) return;
	const page = await browser.newPage({
		viewport: { width: 1040, height: 1120 },
		colorScheme: theme,
	});
	try {
		await configurePage(page, systemToolsFixtures);
		await openPage(page, "/system/tools", 'h2:has-text("System Tools")');
		await page.getByTestId("run-tool-calendar_events_today").click();
		await page.getByRole("button", { name: "Run Tool" }).click();
		await page.getByTestId("tool-run-output").getByText("Docs review").waitFor();
		await page.screenshot({
			path: path.join(outputDir, themedOutputName("system-tools.png", theme)),
			fullPage: false,
		});
	} finally {
		await page.close();
	}
}

async function captureJobs(browser, theme) {
	if (!wanted("system-jobs.png")) return;
	const page = await browser.newPage({
		viewport: { width: 1040, height: 1120 },
		colorScheme: theme,
	});
	try {
		await configurePage(page, jobsFixtures);
		await openPage(page, "/agents/assistant/tasks", '[data-testid="stat-runs"]');
		await page.getByTestId("more-analytics-toggle").click();
		await page
			.locator("tr")
			.filter({ hasText: "Workflow is deterministic and safe to compile" })
			.first()
			.click();
		await page.getByText("Generated Script").waitFor();
		// The drawer slides in from the right; wait until it is flush with the
		// viewport's edge.
		await page.waitForFunction(() => {
			const panel = document.querySelector('[data-testid="details-panel"]');
			return (
				Math.round(panel?.getBoundingClientRect().right ?? 0) ===
				window.innerWidth
			);
		});
		await page.screenshot({
			path: path.join(outputDir, themedOutputName("system-jobs.png", theme)),
			fullPage: false,
		});
	} finally {
		await page.close();
	}
}

async function captureSettingsPage({
	browser,
	pathname,
	outputName,
	readySelector,
	theme,
}) {
	if (!wanted(outputName)) return;
	const page = await browser.newPage({
		viewport: { width: 1040, height: 1120 },
		colorScheme: theme,
	});
	try {
		await configurePage(page, settingsFixtures);
		await openPage(page, pathname, readySelector);
		await page.screenshot({
			path: path.join(outputDir, themedOutputName(outputName, theme)),
			fullPage: false,
		});
	} finally {
		await page.close();
	}
}

async function main() {
	await mkdir(outputDir, { recursive: true });
	const browser = await chromium.launch({
		args: ["--ignore-certificate-errors"],
	});
	try {
		await waitForServer(browser, baseURL);
		for (const theme of themes) {
			await captureSimple({
				browser,
				fixtures: overviewFixtures,
				pathname: "/system/overview",
				readySelector: 'h2:has-text("Overview")',
				outputName: "control-panel-overview.png",
				viewport: { width: 1040, height: 980 },
				theme,
			});

			await captureSimple({
				browser,
				fixtures: chatFixtures,
				pathname: "/agents/assistant/chat/sess-triage",
				readySelector: "text=I took a quick pass through everything.",
				outputName: "chat-workspace.png",
				viewport: { width: 1040, height: 980 },
				theme,
			});

			await captureSimple({
				browser,
				fixtures: usageFixtures,
				pathname: "/system/usage",
				readySelector: 'h2:has-text("Usage")',
				outputName: "usage-analytics.png",
				viewport: { width: 1040, height: 1120 },
				theme,
			});

			await captureSettingsPage({
				browser,
				pathname: "/agents/assistant/channels/design-ops-bot/settings",
				outputName: "configure-everything.png",
				readySelector: "text=Allow From",
				theme,
			});

			await captureSettingsPage({
				browser,
				pathname: "/agents/assistant/settings/permissions",
				outputName: "security-minded.png",
				readySelector: 'label:has-text("Filesystem Allowed Paths")',
				theme,
			});

			await captureSimple({
				browser,
				fixtures: systemSkillsFixtures,
				pathname: "/system/skills",
				readySelector: 'h2:has-text("Skills")',
				outputName: "system-skills.png",
				viewport: { width: 1040, height: 1120 },
				theme,
			});

			await captureSimple({
				browser,
				fixtures: {},
				pathname: "/system/models",
				readySelector: 'h2:has-text("Supported Models")',
				outputName: "system-models.png",
				viewport: { width: 1040, height: 1120 },
				theme,
			});

			await captureSimple({
				browser,
				fixtures: projectsFixtures,
				pathname: "/agents/coder/projects",
				readySelector: "text=Cap scheduler retry backoff at 10 minutes",
				outputName: "projects-board.png",
				viewport: { width: 1040, height: 980 },
				theme,
				// Show the job counts as the compact row of chips.
				storage: { "aviary.projects.statsCompact": "1" },
			});

			await captureSimple({
				browser,
				fixtures: projectsFixtures,
				pathname: "/agents/coder/projects/lsegal%2Faviary/jobs/218",
				readySelector: '[data-event="usage"]',
				outputName: "projects-run.png",
				viewport: { width: 1440, height: 980 },
				theme,
			});

			await captureTools(browser, theme);
			await captureJobs(browser, theme);
		}
	} finally {
		await browser.close();
	}
}

main().catch((error) => {
	console.error(error);
	process.exitCode = 1;
});
